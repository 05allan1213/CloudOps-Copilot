// Package recovery owns the native Incident recovery decision and attempt
// lifecycle. It intentionally has no GitHub, Argo CD, or delivery dependency.
package recovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const (
	DecisionVerifyRecovery = "verify_recovery"
	verificationContract   = 1
	recoveryTaskAttempts   = 5
)

var (
	ErrInvalid           = errors.New("invalid recovery decision")
	ErrNotFound          = errors.New("recovery Incident was not found")
	ErrStaleVersion      = errors.New("recovery Incident version is stale")
	ErrInvalidTransition = errors.New("recovery decision is not allowed in the current state")
	ErrConflict          = errors.New("recovery decision conflicts with current durable state")
)

type taskWriter interface {
	EnqueueIn(context.Context, asyncjob.DBTX, asyncjob.NewTask) (*asyncjob.Task, error)
}

type Coordinator struct {
	tasks taskWriter
	now   func() time.Time
}

func NewCoordinator(db *sql.DB) (*Coordinator, error) {
	if db == nil {
		return nil, errors.New("recovery coordinator requires MySQL")
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		return nil, err
	}
	return &Coordinator{tasks: tasks, now: func() time.Time { return time.Now().UTC() }}, nil
}

type DecisionInput struct {
	IncidentID      string
	ExpectedVersion uint64
	Decision        string
	Reason          string
	ActorProvider   string
	ActorLogin      string
	ActorRole       string
	RequestID       string
	IdempotencyKey  string
}

type DecisionResult struct {
	IncidentID        string
	DecisionID        string
	InvestigationID   string
	VerificationRunID string
	Status            string
	Version           uint64
	Cycle             uint64
}

type incidentState struct {
	ID                    uint64
	PublicID              string
	Cycle                 uint32
	Version               uint64
	Status                string
	Cluster               string
	Environment           string
	Namespace             string
	Service               string
	TargetKind            string
	TargetName            string
	Summary               string
	FirstSeenAt           time.Time
	MigratedLegacy        bool
	MigratedLegacyContext bool
}

type investigationState struct {
	ID          uint64
	PublicID    string
	Status      string
	CompletedAt time.Time
}

type scopeState struct {
	ConfigurationID       uint64
	ConfigurationPublicID string
	ScopeID               uint64
	ScopePublicID         string
}

type decisionState struct {
	ID              uint64
	PublicID        string
	InvestigationID uint64
	ConfigurationID uint64
	ScopeID         uint64
}

func (c *Coordinator) DecideIn(ctx context.Context, tx *sql.Tx, input DecisionInput) (DecisionResult, error) {
	if c == nil || c.tasks == nil || tx == nil || !validDecisionInput(input) {
		return DecisionResult{}, ErrInvalid
	}
	incident, err := loadIncidentByPublicID(ctx, tx, input.IncidentID)
	if err != nil {
		return DecisionResult{}, err
	}
	if incident.Version != input.ExpectedVersion {
		return DecisionResult{}, ErrStaleVersion
	}
	if incident.Status != "investigating" {
		return DecisionResult{}, ErrInvalidTransition
	}
	investigation, err := loadTerminalInvestigation(ctx, tx, incident)
	if err != nil {
		return DecisionResult{}, err
	}
	if err := requireAttributableEvidence(ctx, tx, incident); err != nil {
		return DecisionResult{}, err
	}
	scope, err := loadActiveScope(ctx, tx, incident)
	if err != nil {
		return DecisionResult{}, err
	}
	decision, err := insertDecisionEvent(ctx, tx, incident, investigation, scope, input)
	if err != nil {
		return DecisionResult{}, err
	}
	attempt, err := c.insertAttempt(ctx, tx, &incident, investigation.ID, decision, scope, "owner_decision")
	if err != nil {
		return DecisionResult{}, err
	}
	return DecisionResult{
		IncidentID: incident.PublicID, DecisionID: decision.PublicID,
		InvestigationID: investigation.PublicID, VerificationRunID: attempt,
		Status: incident.Status, Version: incident.Version, Cycle: uint64(incident.Cycle),
	}, nil
}

// RestartForResolvedAlertIn starts the next attempt only after every Alert
// relation in the current Incident Cycle is resolved. Alert lifecycle remains
// independent: this hook can start verification, but can never resolve an
// Incident itself.
func (c *Coordinator) RestartForResolvedAlertIn(ctx context.Context, tx *sql.Tx, alertID uint64) error {
	if c == nil || c.tasks == nil || tx == nil || alertID == 0 {
		return ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT incident.id
FROM alert_incident_links relation
JOIN incidents incident ON incident.id = relation.incident_id
WHERE relation.alert_id = ? AND relation.incident_cycle_no = incident.cycle_no
  AND incident.status = 'investigating'
ORDER BY incident.id`, alertID)
	if err != nil {
		return err
	}
	ids := make([]uint64, 0, 4)
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.restartIncident(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) restartIncident(ctx context.Context, tx *sql.Tx, incidentID uint64) error {
	incident, err := loadIncidentByID(ctx, tx, incidentID)
	if err != nil {
		return err
	}
	if incident.Status != "investigating" {
		return nil
	}
	var total, firing, activeRuns, activeInvestigations int
	if err := tx.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM alert_incident_links relation
   WHERE relation.incident_id = ? AND relation.incident_cycle_no = ?),
  (SELECT COUNT(*) FROM alert_incident_links relation
   JOIN alerts alert ON alert.id = relation.alert_id
   WHERE relation.incident_id = ? AND relation.incident_cycle_no = ? AND alert.status <> 'resolved'),
  (SELECT COUNT(*) FROM verification_runs run
   WHERE run.incident_id = ? AND run.cycle_no = ? AND run.status IN ('pending','running')),
  (SELECT COUNT(*) FROM agent_runs run
   WHERE run.incident_id = ? AND run.cycle_no = ? AND run.subject_type = 'incident'
     AND run.run_kind = 'workspace' AND run.status IN ('pending','running'))`,
		incident.ID, incident.Cycle, incident.ID, incident.Cycle,
		incident.ID, incident.Cycle, incident.ID, incident.Cycle,
	).Scan(&total, &firing, &activeRuns, &activeInvestigations); err != nil {
		return err
	}
	if total == 0 || firing != 0 || activeRuns != 0 || activeInvestigations != 0 {
		return nil
	}
	var latestStatus string
	var decision decisionState
	err = tx.QueryRowContext(ctx, `SELECT run.status, decision.id, decision.public_id,
       run.originating_agent_run_id, run.configuration_revision_id, run.operational_scope_id
FROM verification_runs run
JOIN incident_events decision
  ON decision.id = run.decision_event_id AND decision.incident_id = run.incident_id
 AND decision.cycle_no = run.cycle_no AND decision.event_type = 'incident_recovery_decided'
WHERE run.incident_id = ? AND run.cycle_no = ? AND run.trigger_type = 'operational_recovery'
ORDER BY run.attempt DESC, run.id DESC LIMIT 1 FOR UPDATE`, incident.ID, incident.Cycle).Scan(
		&latestStatus, &decision.ID, &decision.PublicID, &decision.InvestigationID,
		&decision.ConfigurationID, &decision.ScopeID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if latestStatus != "failed" && latestStatus != "timed_out" && latestStatus != "inconclusive" {
		return nil
	}
	var scope scopeState
	err = tx.QueryRowContext(ctx, `SELECT active.configuration_revision_id, revision.public_id,
       scope.id, scope.public_id
FROM active_configuration active
JOIN configuration_revisions revision ON revision.id = active.configuration_revision_id
JOIN operational_scopes scope ON scope.configuration_revision_id = active.configuration_revision_id
WHERE active.singleton_id = 1 AND active.configuration_revision_id = ? AND scope.id = ?
  AND scope.cluster_id = ? AND scope.environment = ?
  AND JSON_CONTAINS(scope.namespaces_json, JSON_QUOTE(?))`,
		decision.ConfigurationID, decision.ScopeID, incident.Cluster, incident.Environment, incident.Namespace,
	).Scan(&scope.ConfigurationID, &scope.ConfigurationPublicID, &scope.ScopeID, &scope.ScopePublicID)
	if errors.Is(err, sql.ErrNoRows) {
		return markAttention(ctx, tx, &incident, "recovery_scope_changed")
	}
	if err != nil {
		return err
	}
	_, err = c.insertAttempt(ctx, tx, &incident, decision.InvestigationID, decision, scope, "all_alert_relations_resolved")
	if errors.Is(err, businessbudget.ErrInvalidAuthorization) || errors.Is(err, ErrInvalidTransition) {
		return markAttention(ctx, tx, &incident, "recovery_attempt_budget_exhausted")
	}
	return err
}

func (c *Coordinator) insertAttempt(ctx context.Context, tx asyncjob.DBTX, incident *incidentState, investigationID uint64, decision decisionState, scope scopeState, reason string) (string, error) {
	if incident == nil || investigationID == 0 || decision.ID == 0 || scope.ConfigurationID == 0 || scope.ScopeID == 0 {
		return "", ErrInvalid
	}
	plan, err := verification.CompilePlan(verification.CompileInput{
		TriggerType: "operational_recovery", Cluster: incident.Cluster, Environment: incident.Environment,
		Namespace: incident.Namespace, Service: incident.Service, WorkloadKind: incident.TargetKind,
		WorkloadName: incident.TargetName,
	})
	if err != nil {
		return "", err
	}
	planJSON, err := json.Marshal(plan)
	if err != nil || len(planJSON) > 16*1024 {
		return "", ErrInvalid
	}
	// Recovery attempts are automatic children of one explicit Owner decision.
	// They consume the normal per-cycle VerificationRun budget, but must not
	// reuse a slot-four/five AgentRun authorization that is unique to one child.
	budget, err := businessbudget.GuardAutomatic(ctx, tx, businessbudget.KindVerificationRun, incident.ID, incident.Cycle)
	if err != nil {
		return "", err
	}
	if !budget.Allowed() || budget.IncidentVersion != incident.Version {
		return "", ErrInvalidTransition
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) + 1 FROM verification_runs
WHERE incident_id = ? AND cycle_no = ? AND decision_event_id = ?`, incident.ID, incident.Cycle, decision.ID).Scan(&attempt); err != nil {
		return "", err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&now); err != nil {
		now = c.now().UTC()
	}
	now = now.UTC()
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO verification_runs
 (public_id, incident_id, cycle_no, originating_agent_run_id, business_budget_authorization_id,
  remediation_plan_id, change_request_id, trigger_signal_id, configuration_revision_id,
  operational_scope_id, decision_event_id, status, trigger_type, target_revision,
  source_revision, image_digest, gitops_revision, plan_json, verification_profile_version,
  verification_profile_hash, verification_contract_version, verification_profile_id,
  common_stability_window_ms, deadline_at, attempt, row_version, expected_subject_version,
  migrated_legacy, migrated_legacy_context, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?, ?, 'pending', 'operational_recovery',
        NULL, NULL, NULL, NULL, ?, ?, ?, ?, ?, 60000, ?, ?, 1, 1, ?, ?, ?, ?)`,
		publicID, incident.ID, incident.Cycle, investigationID, nullableID(budget.AuthorizationID),
		scope.ConfigurationID, scope.ScopeID, decision.ID, planJSON, plan.ProfileVersion,
		plan.ProfileHash, verificationContract, plan.ProfileID, now.Add(plan.Deadline), attempt,
		incident.MigratedLegacy, incident.MigratedLegacyContext, now, now)
	if err != nil {
		return "", err
	}
	runID, err := result.LastInsertId()
	if err != nil || runID <= 0 {
		return "", fmt.Errorf("read operational recovery run id: %w", err)
	}
	for _, spec := range plan.Checks {
		subjectJSON, marshalErr := json.Marshal(spec.Subject)
		if marshalErr != nil {
			return "", marshalErr
		}
		var comparison, threshold any
		if spec.Comparison != "" {
			comparison, threshold = string(spec.Comparison), spec.Threshold
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO verification_checks
 (public_id, verification_run_id, incident_id, cycle_no, check_type, status,
  required_check, subject_json, expected_json, source_reference, lookback_ms,
  stability_window_ms, timeout_ms, poll_interval_ms, check_spec_schema_version,
  profile_id, template_id, template_version, comparison, threshold, source_identity,
  initial_delay_ms, min_samples, sample_unit, failure_mode, migrated_legacy,
  migrated_legacy_context, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?, '', ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), runID, incident.ID, incident.Cycle, spec.Type, spec.Required,
			subjectJSON, spec.Expected, spec.Lookback.Milliseconds(), spec.StabilityWindow.Milliseconds(),
			spec.Timeout.Milliseconds(), spec.PollInterval.Milliseconds(), spec.ProfileID,
			spec.TemplateID, spec.TemplateVersion, comparison, threshold, spec.SourceIdentity,
			spec.InitialDelay.Milliseconds(), spec.MinSamples, spec.SampleUnit, spec.FailureMode,
			incident.MigratedLegacy, incident.MigratedLegacyContext, now, now); err != nil {
			return "", err
		}
	}
	result, err = tx.ExecContext(ctx, `UPDATE incidents
SET status = 'verifying', version = version + 1, needs_attention = FALSE,
    blocking_reason_code = NULL, blocked_at = NULL, updated_at = ?
WHERE id = ? AND cycle_no = ? AND version = ? AND status = 'investigating'`,
		now, incident.ID, incident.Cycle, incident.Version)
	if err != nil {
		return "", err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return "", ErrStaleVersion
	}
	incident.Status = "verifying"
	incident.Version++
	metadata, _ := json.Marshal(map[string]any{
		"verification_run_id": publicID, "decision_id": decision.PublicID,
		"profile_id": plan.ProfileID, "profile_hash": plan.ProfileHash,
		"attempt": attempt, "reason": reason,
		"configuration_revision_id": scope.ConfigurationPublicID,
		"operational_scope_id":      scope.ScopePublicID,
	})
	if err := appendEvent(ctx, tx, *incident, "verification_started", "investigating", "verifying",
		"operational recovery verification started", metadata,
		hash("recovery-verification-started", incident.PublicID, fmt.Sprint(incident.Cycle), decision.PublicID, fmt.Sprint(attempt))); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"verification_run_id": publicID, "cycle_no": incident.Cycle})
	_, err = c.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: incident.ID, CycleNo: incident.Cycle, Type: asyncjob.TaskRecoveryVerify,
		SubjectType: "verification_run", SubjectID: uint64(runID), Transition: "recovery.verify",
		ExpectedSubjectVersion: 1, PayloadSchemaVersion: 1, Payload: payload,
		DedupeKey: hash("recovery.verify", publicID, "1"), Priority: 70,
		MigratedLegacy: incident.MigratedLegacy, MigratedLegacyContext: incident.MigratedLegacyContext,
		AvailableAt: &now, MaxAttempts: recoveryTaskAttempts,
	})
	if err != nil {
		return "", err
	}
	return publicID, nil
}

func validDecisionInput(input DecisionInput) bool {
	return strings.TrimSpace(input.IncidentID) != "" && input.ExpectedVersion > 0 &&
		input.Decision == DecisionVerifyRecovery && len(strings.TrimSpace(input.Reason)) >= 1 &&
		len(strings.TrimSpace(input.Reason)) <= 1024 && input.ActorProvider == "local" &&
		input.ActorLogin == "owner" && input.ActorRole == "owner" &&
		strings.TrimSpace(input.RequestID) != "" && len(input.RequestID) <= 128 &&
		strings.TrimSpace(input.IdempotencyKey) != "" && len(input.IdempotencyKey) <= 128
}

func loadIncidentByPublicID(ctx context.Context, tx asyncjob.DBTX, publicID string) (incidentState, error) {
	return scanIncident(tx.QueryRowContext(ctx, `SELECT id, public_id, cycle_no, version, status,
       cluster, environment, namespace, service_name, target_kind, target_name, summary,
       first_seen_at, migrated_legacy, migrated_legacy_context
FROM incidents WHERE public_id = ? FOR UPDATE`, publicID))
}

func loadIncidentByID(ctx context.Context, tx asyncjob.DBTX, id uint64) (incidentState, error) {
	return scanIncident(tx.QueryRowContext(ctx, `SELECT id, public_id, cycle_no, version, status,
       cluster, environment, namespace, service_name, target_kind, target_name, summary,
       first_seen_at, migrated_legacy, migrated_legacy_context
FROM incidents WHERE id = ? FOR UPDATE`, id))
}

func scanIncident(row *sql.Row) (incidentState, error) {
	var incident incidentState
	err := row.Scan(&incident.ID, &incident.PublicID, &incident.Cycle, &incident.Version,
		&incident.Status, &incident.Cluster, &incident.Environment, &incident.Namespace,
		&incident.Service, &incident.TargetKind, &incident.TargetName, &incident.Summary,
		&incident.FirstSeenAt, &incident.MigratedLegacy, &incident.MigratedLegacyContext)
	if errors.Is(err, sql.ErrNoRows) {
		return incidentState{}, ErrNotFound
	}
	return incident, err
}

func loadTerminalInvestigation(ctx context.Context, tx asyncjob.DBTX, incident incidentState) (investigationState, error) {
	var result investigationState
	var completed sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id, public_id, status, completed_at
FROM agent_runs
WHERE incident_id = ? AND cycle_no = ? AND subject_type = 'incident' AND run_kind = 'workspace'
ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE`, incident.ID, incident.Cycle).Scan(
		&result.ID, &result.PublicID, &result.Status, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return investigationState{}, ErrInvalidTransition
	}
	if err != nil {
		return investigationState{}, err
	}
	if (result.Status != "completed" && result.Status != "failed" && result.Status != "cancelled") || !completed.Valid {
		return investigationState{}, ErrInvalidTransition
	}
	result.CompletedAt = completed.Time.UTC()
	return result, nil
}

func requireAttributableEvidence(ctx context.Context, tx asyncjob.DBTX, incident incidentState) error {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
FROM evidence_items evidence
WHERE evidence.incident_id = ? AND evidence.cycle_no = ? AND evidence.valid = TRUE
  AND evidence.migrated_legacy = FALSE AND evidence.evidence_contract_version = 1
  AND evidence.producer_type IS NOT NULL AND evidence.producer_id IS NOT NULL
  AND evidence.provenance_json IS NOT NULL AND evidence.content_hash IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM evidence_supersessions supersession
    WHERE supersession.incident_id = evidence.incident_id
      AND supersession.cycle_no = evidence.cycle_no
      AND supersession.superseded_evidence_id = evidence.id
  )`, incident.ID, incident.Cycle).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrInvalidTransition
	}
	return nil
}

func loadActiveScope(ctx context.Context, tx asyncjob.DBTX, incident incidentState) (scopeState, error) {
	var scope scopeState
	err := tx.QueryRowContext(ctx, `SELECT active.configuration_revision_id, revision.public_id,
       scope.id, scope.public_id
FROM active_configuration active
JOIN configuration_revisions revision ON revision.id = active.configuration_revision_id
JOIN operational_scopes scope ON scope.configuration_revision_id = active.configuration_revision_id
WHERE active.singleton_id = 1 AND scope.cluster_id = ? AND scope.environment = ?
  AND JSON_CONTAINS(scope.namespaces_json, JSON_QUOTE(?))
LIMIT 1 FOR SHARE`, incident.Cluster, incident.Environment, incident.Namespace).Scan(
		&scope.ConfigurationID, &scope.ConfigurationPublicID, &scope.ScopeID, &scope.ScopePublicID)
	if errors.Is(err, sql.ErrNoRows) {
		return scopeState{}, ErrConflict
	}
	return scope, err
}

func insertDecisionEvent(ctx context.Context, tx asyncjob.DBTX, incident incidentState, investigation investigationState, scope scopeState, input DecisionInput) (decisionState, error) {
	publicID := uuid.NewString()
	metadata, _ := json.Marshal(map[string]any{
		"decision": input.Decision, "reason": strings.TrimSpace(input.Reason),
		"investigation_id":          investigation.PublicID,
		"configuration_revision_id": scope.ConfigurationPublicID,
		"operational_scope_id":      scope.ScopePublicID, "request_id": input.RequestID,
	})
	result, err := tx.ExecContext(ctx, `INSERT INTO incident_events
 (public_id, incident_id, cycle_no, event_schema_version, event_type,
  source_status, target_status, reason_code, idempotency_key,
  migrated_legacy_context, migrated_legacy, actor_type, actor_id,
  summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, ?, 1, 'incident_recovery_decided', 'investigating', 'verifying',
        'owner_verified_recovery', ?, ?, ?, 'owner', ?, ?, ?, NOW(6), NOW(6))`,
		publicID, incident.ID, incident.Cycle,
		hash("incident-recovery-decision", incident.PublicID, fmt.Sprint(incident.Cycle), input.IdempotencyKey),
		incident.MigratedLegacyContext, incident.MigratedLegacy,
		input.ActorProvider+":"+input.ActorLogin, strings.TrimSpace(input.Reason), metadata)
	if err != nil {
		var mysqlErr *drivermysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return decisionState{}, ErrConflict
		}
		return decisionState{}, fmt.Errorf("persist recovery decision: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return decisionState{}, fmt.Errorf("read recovery decision id: %w", err)
	}
	return decisionState{
		ID: uint64(id), PublicID: publicID, InvestigationID: investigation.ID,
		ConfigurationID: scope.ConfigurationID, ScopeID: scope.ScopeID,
	}, nil
}

func appendEvent(ctx context.Context, tx asyncjob.DBTX, incident incidentState, eventType, source, target, summary string, metadata []byte, idempotency string) error {
	_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO incident_events
 (public_id, incident_id, cycle_no, event_schema_version, event_type,
  source_status, target_status, idempotency_key, migrated_legacy_context,
  migrated_legacy, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, 'system', 'recovery-coordinator', ?, ?, NOW(6), NOW(6))`,
		uuid.NewString(), incident.ID, incident.Cycle, eventType, source, target, idempotency,
		incident.MigratedLegacyContext, incident.MigratedLegacy, summary, metadata)
	return err
}

func markAttention(ctx context.Context, tx asyncjob.DBTX, incident *incidentState, reason string) error {
	result, err := tx.ExecContext(ctx, `UPDATE incidents
SET needs_attention = TRUE, blocking_reason_code = ?, blocked_at = NOW(6),
    version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND version = ? AND status = 'investigating'`,
		reason, incident.ID, incident.Cycle, incident.Version)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrStaleVersion
	}
	incident.Version++
	metadata, _ := json.Marshal(map[string]any{"reason": reason})
	return appendEvent(ctx, tx, *incident, "recovery_verification_blocked", "investigating", "investigating",
		"operational recovery verification requires Owner attention", metadata,
		hash("recovery-blocked", incident.PublicID, fmt.Sprint(incident.Cycle), reason, fmt.Sprint(incident.Version)))
}

func nullableID(value uint64) any {
	if value == 0 {
		return nil
	}
	return value
}

func hash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}
