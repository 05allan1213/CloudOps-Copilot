package incidentv3mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const (
	defaultVerificationRunBudget = businessbudget.DefaultLimit
	hardVerificationRunBudget    = businessbudget.HardLimit
	verificationTaskMaxAttempts  = 8
)

var errNoChangeIdentityUnavailable = errors.New("no-change deployment identity is unavailable")

type noChangePlanState struct {
	ID         uint64
	Status     string
	RowVersion uint64
}

type noChangeRequestState struct {
	ID                   uint64
	PlanID               uint64
	Status               string
	PlanStatus           string
	ExternalWriteStarted bool
	ExternalWriteEvent   bool
}

type noChangeTaskState struct {
	ID          uint64
	TaskType    string
	SubjectType string
	SubjectID   uint64
	Status      string
}

type noChangeWorkflowState struct {
	IncidentStatus     domain.V3Status
	ActiveVerification bool
	Plan               *noChangePlanState
	Change             *noChangeRequestState
	Tasks              []noChangeTaskState
}

type noChangeDecision struct {
	Eligible     bool
	CancelPlan   bool
	CancelChange bool
	ChangeID     uint64
	Reason       string
}

type noChangeCancellation struct {
	AgentRuns, Plans, Changes, Tasks, Attempts int64
}

type noChangeVerificationSnapshot struct {
	Plan                     verification.Plan
	TriggerSignalPublicID    string
	BaselinePublicID         string
	BaselineVerificationHash string
}

func noChangeTriggerSignal(signals []insertedSignal, incident incidentRow) (uint64, bool) {
	var selected insertedSignal
	found := false
	for _, signal := range signals {
		if !signal.new || signal.input.Status != domain.SignalStatusResolved || signal.incidentID != incident.id || signal.cycleNo != incident.cycleNo {
			continue
		}
		if !found || signal.input.OccurredAt.After(selected.input.OccurredAt) ||
			signal.input.OccurredAt.Equal(selected.input.OccurredAt) && signal.id > selected.id {
			selected, found = signal, true
		}
	}
	return selected.id, found
}

func decideNoChangeWorkflow(state noChangeWorkflowState) noChangeDecision {
	if state.ActiveVerification || state.IncidentStatus == domain.V3StatusVerifying {
		return noChangeDecision{Reason: "verification_already_active"}
	}
	if state.IncidentStatus == domain.V3StatusResolved || state.IncidentStatus == domain.V3StatusClosed {
		return noChangeDecision{Reason: "incident_is_terminal"}
	}
	for _, task := range state.Tasks {
		switch task.TaskType {
		case "delivery.observe":
			return noChangeDecision{Reason: "delivery_reconciliation_active"}
		case "verification.advance":
			return noChangeDecision{Reason: "verification_task_active"}
		case "change.ensure_pr":
			if task.SubjectType == "change_request" && task.Status == "running" {
				return noChangeDecision{Reason: "external_write_may_be_in_flight"}
			}
			if task.SubjectType == "change_request" && (state.Change == nil || task.SubjectID != state.Change.ID) {
				return noChangeDecision{Reason: "change_task_without_active_request"}
			}
		}
	}
	if state.Change != nil {
		if state.IncidentStatus != domain.V3StatusDelivering {
			return noChangeDecision{Reason: "active_change_outside_delivery"}
		}
		if state.Change.Status != "pending" || state.Change.PlanStatus != "consumed" ||
			state.Change.ExternalWriteStarted || state.Change.ExternalWriteEvent {
			return noChangeDecision{Reason: "external_write_intent_exists"}
		}
		return noChangeDecision{Eligible: true, CancelChange: true, ChangeID: state.Change.ID, Reason: "cancel_pending_change_before_write"}
	}
	switch state.IncidentStatus {
	case domain.V3StatusDelivering:
		if state.Plan == nil || state.Plan.Status != "approved" {
			return noChangeDecision{Reason: "delivery_state_has_no_cancellable_plan"}
		}
		return noChangeDecision{Eligible: true, CancelPlan: true, Reason: "cancel_approved_plan_before_change_request"}
	case domain.V3StatusAwaitingApproval:
		if state.Plan == nil {
			return noChangeDecision{Reason: "approval_state_has_no_active_plan"}
		}
		return noChangeDecision{Eligible: true, CancelPlan: true, Reason: "cancel_unconsumed_plan"}
	case domain.V3StatusDetected, domain.V3StatusInvestigating:
		return noChangeDecision{Eligible: true, CancelPlan: state.Plan != nil, Reason: "cancel_read_only_work"}
	default:
		return noChangeDecision{Reason: "incident_status_not_eligible"}
	}
}

func startNoChangeVerification(ctx context.Context, tx *sql.Tx, incident *incidentRow, triggerSignalID uint64) (bool, error) {
	triggerSignalPublicID, err := loadNoChangeTriggerSignal(ctx, tx, *incident, triggerSignalID)
	if err != nil {
		return false, err
	}
	state, err := loadNoChangeWorkflowState(ctx, tx, *incident)
	if err != nil {
		return false, err
	}
	decision := decideNoChangeWorkflow(state)
	if !decision.Eligible {
		if decision.Reason != "verification_already_active" && decision.Reason != "incident_is_terminal" {
			if err := appendNoChangeDeferredEvent(ctx, tx, *incident, triggerSignalPublicID, decision.Reason); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	snapshot, identityErr := loadNoChangeVerificationSnapshot(ctx, tx, *incident, triggerSignalPublicID)
	if identityErr != nil && !errors.Is(identityErr, errNoChangeIdentityUnavailable) {
		return false, identityErr
	}
	originatingAgentRunID, err := businessbudget.ActiveAgentRunForCycle(ctx, tx, incident.id, uint32(incident.cycleNo))
	if err != nil {
		return false, err
	}
	cancelled, err := cancelNoChangeWorkflow(ctx, tx, *incident, decision)
	if err != nil {
		return false, err
	}
	if err := ensureNoChangeInvestigating(ctx, tx, incident, triggerSignalPublicID); err != nil {
		return false, err
	}
	if cancelled.AgentRuns+cancelled.Plans+cancelled.Changes+cancelled.Tasks > 0 {
		if err := appendNoChangeCancellationEvent(ctx, tx, *incident, triggerSignalPublicID, decision, cancelled); err != nil {
			return false, err
		}
	}
	if identityErr != nil {
		return false, blockNoChangeVerification(ctx, tx, incident, triggerSignalPublicID, "no_change_identity_unavailable")
	}
	budget, err := businessbudget.GuardChild(ctx, tx, businessbudget.KindVerificationRun, incident.id, uint32(incident.cycleNo), originatingAgentRunID)
	if err != nil {
		return false, fmt.Errorf("no-change Verification authorization rejected: %w", err)
	}
	if !budget.Allowed() {
		if budget.IncidentVersion != incident.version {
			return false, errors.New("incident changed before no-change budget block")
		}
		if err := businessbudget.MarkExhausted(ctx, tx, budget, incident.id, uint32(incident.cycleNo), "v3-ingress.no-change"); err != nil {
			return false, err
		}
		incident.version++
		return false, nil
	}
	return insertNoChangeVerification(ctx, tx, incident, triggerSignalID, budget.Count+1, snapshot, budget)
}

func loadNoChangeWorkflowState(ctx context.Context, tx *sql.Tx, incident incidentRow) (noChangeWorkflowState, error) {
	state := noChangeWorkflowState{IncidentStatus: incident.status}
	rows, err := tx.QueryContext(ctx, `SELECT id
FROM verification_runs
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status IN ('pending','running')
ORDER BY id LIMIT 2 FOR UPDATE`, incident.id, incident.cycleNo)
	if err != nil {
		return noChangeWorkflowState{}, err
	}
	verificationCount := 0
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return noChangeWorkflowState{}, err
		}
		verificationCount++
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return noChangeWorkflowState{}, err
	}
	if err := rows.Close(); err != nil {
		return noChangeWorkflowState{}, err
	}
	if verificationCount > 1 {
		return noChangeWorkflowState{}, errors.New("incident cycle has multiple active VerificationRuns")
	}
	state.ActiveVerification = verificationCount == 1

	plan := &noChangePlanState{}
	err = tx.QueryRowContext(ctx, `SELECT id, v3_status, row_version
FROM remediation_plans
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
  AND v3_status IN ('awaiting_approval','approved')
ORDER BY id LIMIT 1 FOR UPDATE`, incident.id, incident.cycleNo).Scan(&plan.ID, &plan.Status, &plan.RowVersion)
	if err == nil {
		state.Plan = plan
	} else if !errors.Is(err, sql.ErrNoRows) {
		return noChangeWorkflowState{}, err
	}

	change := &noChangeRequestState{}
	var writeAt sql.NullTime
	var writeMarker sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT cr.id, cr.plan_id, cr.v3_status, rp.v3_status,
       cr.external_write_started_at, cr.external_write_marker
FROM change_requests cr
JOIN remediation_plans rp ON rp.id = cr.plan_id
WHERE cr.incident_id = ? AND cr.cycle_no = ? AND cr.domain_schema_version = 3
  AND cr.v3_status IN ('pending','pr_open','merged','syncing','rolling_out')
ORDER BY cr.id LIMIT 1 FOR UPDATE`, incident.id, incident.cycleNo).Scan(
		&change.ID, &change.PlanID, &change.Status, &change.PlanStatus, &writeAt, &writeMarker)
	if err == nil {
		change.ExternalWriteStarted = writeAt.Valid || writeMarker.Valid
		var eventCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
FROM change_request_events
WHERE change_request_id = ? AND incident_id = ? AND cycle_no = ? AND external_write_started = TRUE`,
			change.ID, incident.id, incident.cycleNo).Scan(&eventCount); err != nil {
			return noChangeWorkflowState{}, err
		}
		change.ExternalWriteEvent = eventCount > 0
		state.Change = change
	} else if !errors.Is(err, sql.ErrNoRows) {
		return noChangeWorkflowState{}, err
	}

	rows, err = tx.QueryContext(ctx, `SELECT id, task_type, subject_type, subject_id, status
FROM async_tasks
WHERE incident_id = ? AND cycle_no = ? AND status IN ('ready','running')
  AND task_type IN ('investigation.advance','remediation.prepare','change.ensure_pr','delivery.observe','verification.advance')
ORDER BY id FOR UPDATE`, incident.id, incident.cycleNo)
	if err != nil {
		return noChangeWorkflowState{}, err
	}
	for rows.Next() {
		var task noChangeTaskState
		if err := rows.Scan(&task.ID, &task.TaskType, &task.SubjectType, &task.SubjectID, &task.Status); err != nil {
			_ = rows.Close()
			return noChangeWorkflowState{}, err
		}
		state.Tasks = append(state.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return noChangeWorkflowState{}, err
	}
	if err := rows.Close(); err != nil {
		return noChangeWorkflowState{}, err
	}
	return state, nil
}

func loadNoChangeTriggerSignal(ctx context.Context, tx *sql.Tx, incident incidentRow, triggerSignalID uint64) (string, error) {
	var publicID string
	var triggerIncidentID, triggerCycle uint64
	var triggerStatus string
	if err := tx.QueryRowContext(ctx, `SELECT public_id, incident_id, cycle_no, status
FROM incident_signals WHERE id = ? FOR SHARE`, triggerSignalID).Scan(
		&publicID, &triggerIncidentID, &triggerCycle, &triggerStatus); err != nil {
		return "", err
	}
	if triggerIncidentID != incident.id || triggerCycle != incident.cycleNo || triggerStatus != "resolved" {
		return "", errors.New("no-change trigger Signal escaped the Incident cycle")
	}
	return publicID, nil
}

func loadNoChangeVerificationSnapshot(ctx context.Context, tx *sql.Tx, incident incidentRow, triggerSignalPublicID string) (noChangeVerificationSnapshot, error) {
	trigger := noChangeVerificationSnapshot{TriggerSignalPublicID: triggerSignalPublicID}
	type baselineRow struct {
		publicID, sourceRevision, imageDigest, gitopsRevision string
		repository, verificationHash, argoJSON                string
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.public_id, b.source_revision, b.image_digest,
       b.gitops_revision, b.repository, b.verification_hash, CAST(o.observed_json AS CHAR)
FROM deployment_baselines b
JOIN baseline_observations o ON o.baseline_id = b.id
  AND o.domain_schema_version = 3 AND o.observation_type = 'argocd_revision'
WHERE b.domain_schema_version = 3 AND b.status = 'active'
  AND b.cluster = ? AND b.environment = ? AND b.namespace = ?
  AND b.workload_kind = ? AND b.workload_name = ?
ORDER BY b.verified_at DESC, b.id DESC LIMIT 2 FOR SHARE`,
		incident.cluster, incident.environment, incident.namespace, incident.targetKind, incident.targetName)
	if err != nil {
		return noChangeVerificationSnapshot{}, err
	}
	baselines := make([]baselineRow, 0, 2)
	for rows.Next() {
		var item baselineRow
		if err := rows.Scan(&item.publicID, &item.sourceRevision, &item.imageDigest, &item.gitopsRevision,
			&item.repository, &item.verificationHash, &item.argoJSON); err != nil {
			_ = rows.Close()
			return noChangeVerificationSnapshot{}, err
		}
		baselines = append(baselines, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return noChangeVerificationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return noChangeVerificationSnapshot{}, err
	}
	if len(baselines) != 1 {
		return noChangeVerificationSnapshot{}, fmt.Errorf("%w: found %d active target baselines", errNoChangeIdentityUnavailable, len(baselines))
	}
	baseline := baselines[0]
	var argo struct {
		Application string `json:"application"`
		Project     string `json:"project"`
	}
	if json.Unmarshal([]byte(baseline.argoJSON), &argo) != nil || strings.TrimSpace(argo.Application) == "" || strings.TrimSpace(argo.Project) == "" ||
		len(baseline.verificationHash) != 64 {
		return noChangeVerificationSnapshot{}, errors.New("active DeploymentBaseline has malformed Argo provenance")
	}

	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT category
FROM incident_signals
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
ORDER BY category LIMIT 21 FOR SHARE`, incident.id, incident.cycleNo)
	if err != nil {
		return noChangeVerificationSnapshot{}, err
	}
	alertNames := make([]string, 0, 20)
	for rows.Next() {
		var alertName string
		if err := rows.Scan(&alertName); err != nil {
			_ = rows.Close()
			return noChangeVerificationSnapshot{}, err
		}
		alertName = strings.TrimSpace(alertName)
		if alertName != "" {
			alertNames = append(alertNames, alertName)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return noChangeVerificationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return noChangeVerificationSnapshot{}, err
	}
	if len(alertNames) == 0 || len(alertNames) > 20 {
		return noChangeVerificationSnapshot{}, fmt.Errorf("%w: alert identity count is %d", errNoChangeIdentityUnavailable, len(alertNames))
	}

	plan, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{
		TriggerType: "no_change", Repository: baseline.repository,
		TargetRevision: baseline.gitopsRevision, SourceRevision: baseline.sourceRevision,
		ImageDigest: baseline.imageDigest, GitOpsRevision: baseline.gitopsRevision,
		ArgoApplication: argo.Application, ArgoProject: argo.Project,
		Cluster: incident.cluster, Environment: incident.environment, Namespace: incident.namespace,
		Service: incident.service, WorkloadName: incident.targetName, AlertNames: alertNames,
	})
	if err != nil {
		return noChangeVerificationSnapshot{}, fmt.Errorf("compile no-change VerificationPlan: %w", err)
	}
	trigger.Plan = plan
	trigger.BaselinePublicID = baseline.publicID
	trigger.BaselineVerificationHash = baseline.verificationHash
	return trigger, nil
}

func cancelNoChangeWorkflow(ctx context.Context, tx *sql.Tx, incident incidentRow, decision noChangeDecision) (noChangeCancellation, error) {
	var summary noChangeCancellation
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs
SET v3_status = 'cancelled', status = 'CANCELLED', row_version = row_version + 1,
    cancel_requested_at = NOW(6), completed_at = NOW(6), failure_code = 'resolved_before_diagnosis',
    failure_summary = 'resolved Signal triggered no-change verification', updated_at = NOW(6)
WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ? AND v3_status IN ('pending','running')`,
		incident.id, incident.cycleNo)
	if err != nil {
		return noChangeCancellation{}, err
	}
	summary.AgentRuns, _ = result.RowsAffected()
	if decision.CancelPlan {
		result, err = tx.ExecContext(ctx, `UPDATE remediation_plans
SET v3_status = 'cancelled', status = 'cancelled', row_version = row_version + 1, updated_at = NOW(6)
WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ?
  AND v3_status IN ('awaiting_approval','approved')`, incident.id, incident.cycleNo)
		if err != nil {
			return noChangeCancellation{}, err
		}
		summary.Plans, _ = result.RowsAffected()
	}
	if decision.CancelChange {
		result, err = tx.ExecContext(ctx, `UPDATE change_requests
SET v3_status = 'cancelled', status = 'delivery_cancelled', ci_status = 'cancelled',
    failure_code = 'resolved_before_external_write', failure_reason = 'resolved before external write intent',
    row_version = row_version + 1, updated_at = NOW(6)
WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ? AND v3_status = 'pending'
  AND id = ? AND external_write_started_at IS NULL AND external_write_marker IS NULL`, incident.id, incident.cycleNo, decision.ChangeID)
		if err != nil {
			return noChangeCancellation{}, err
		}
		summary.Changes, _ = result.RowsAffected()
		if summary.Changes != 1 {
			return noChangeCancellation{}, errors.New("pending ChangeRequest changed before no-change cancellation")
		}
		if err := appendNoChangeRequestCancellation(ctx, tx, incident, decision.ChangeID, decision); err != nil {
			return noChangeCancellation{}, err
		}
	}
	result, err = tx.ExecContext(ctx, `UPDATE async_task_attempts attempt
JOIN async_tasks task
  ON task.id = attempt.task_id AND task.attempt = attempt.attempt
 AND task.lease_owner = attempt.lease_owner AND task.lease_generation = attempt.lease_generation
 AND task.expected_subject_version = attempt.expected_subject_version
SET attempt.status = 'cancelled', attempt.finished_at = NOW(6),
    attempt.error_code = 'resolved_signal',
    attempt.error_summary = 'resolved Signal fenced task before no-change verification'
WHERE task.incident_id = ? AND task.cycle_no = ? AND task.status = 'running'
  AND task.task_type IN ('investigation.advance','remediation.prepare','change.ensure_pr')
  AND attempt.status = 'running'`, incident.id, incident.cycleNo)
	if err != nil {
		return noChangeCancellation{}, err
	}
	summary.Attempts, _ = result.RowsAffected()
	result, err = tx.ExecContext(ctx, `UPDATE async_tasks
SET status = 'cancelled', lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL,
    lease_generation = lease_generation + 1, last_error_code = 'resolved_signal',
    last_error_summary = 'resolved Signal triggered no-change verification',
    cancelled_at = NOW(6), updated_at = NOW(6)
WHERE incident_id = ? AND cycle_no = ? AND status IN ('ready','running')
  AND task_type IN ('investigation.advance','remediation.prepare','change.ensure_pr')`, incident.id, incident.cycleNo)
	if err != nil {
		return noChangeCancellation{}, err
	}
	summary.Tasks, _ = result.RowsAffected()
	return summary, nil
}

func ensureNoChangeInvestigating(ctx context.Context, tx *sql.Tx, incident *incidentRow, triggerSignalPublicID string) error {
	if incident.status == domain.V3StatusInvestigating {
		return nil
	}
	previous := incident.status
	result, err := tx.ExecContext(ctx, `UPDATE incidents
SET status = 'DIAGNOSING', v3_status = 'investigating',
    version = version + 1, needs_attention = FALSE, blocking_reason_code = NULL,
    blocked_at = NULL, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ? AND v3_status = ?`,
		incident.id, incident.cycleNo, incident.version, previous)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("incident changed before no-change investigating transition")
	}
	incident.status = domain.V3StatusInvestigating
	incident.version++
	metadata, _ := json.Marshal(map[string]any{"from": previous, "trigger_signal_id": triggerSignalPublicID})
	return appendEvent(ctx, tx, *incident, "incident_returned_to_investigating", "system", "v3-ingress",
		"resolved Signal returned Incident to investigating before no-change verification", time.Time{}, metadata,
		hashCanonical("no-change", incident.publicID, fmt.Sprint(incident.cycleNo), triggerSignalPublicID, "investigating"))
}

func blockNoChangeVerification(ctx context.Context, tx *sql.Tx, incident *incidentRow, triggerSignalPublicID, reason string) error {
	result, err := tx.ExecContext(ctx, `UPDATE incidents
SET version = version + 1, needs_attention = TRUE,
    blocking_reason_code = ?, blocked_at = NOW(6), updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ? AND v3_status = 'investigating'`,
		reason, incident.id, incident.cycleNo, incident.version)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("incident changed before no-change block")
	}
	incident.version++
	metadata, _ := json.Marshal(map[string]any{
		"reason": reason, "trigger_signal_id": triggerSignalPublicID,
		"default_budget": defaultVerificationRunBudget, "hard_budget": hardVerificationRunBudget,
	})
	return appendEvent(ctx, tx, *incident, "no_change_verification_blocked", "system", "v3-ingress",
		"no-change verification could not be created", time.Time{}, metadata,
		hashCanonical("no-change", incident.publicID, fmt.Sprint(incident.cycleNo), triggerSignalPublicID, "blocked", reason))
}

func insertNoChangeVerification(ctx context.Context, tx *sql.Tx, incident *incidentRow, triggerSignalID uint64, attempt int, snapshot noChangeVerificationSnapshot, budget businessbudget.Result) (bool, error) {
	planJSON, err := json.Marshal(snapshot.Plan)
	if err != nil || len(planJSON) > 16*1024 {
		return false, errors.New("no-change VerificationPlan exceeds its durable bound")
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&now); err != nil {
		return false, err
	}
	now = now.UTC()
	var originatingAgentRunValue any
	if budget.OriginatingAgentRunID != 0 {
		originatingAgentRunValue = budget.OriginatingAgentRunID
	}
	var authorizationValue any
	if budget.AuthorizationID != 0 {
		authorizationValue = budget.AuthorizationID
	}
	runPublicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO verification_runs
 (public_id, incident_id, domain_schema_version, cycle_no, originating_agent_run_id,
  business_budget_authorization_id, remediation_plan_id, change_request_id,
  status, v3_status, trigger_type, trigger_signal_id, target_revision, source_revision, image_digest,
  gitops_revision, plan_json, verification_profile_version, verification_profile_hash,
  verification_contract_version, verification_profile_id, common_stability_window_ms,
  deadline_at, attempt, row_version, expected_subject_version, migrated_legacy_context, created_at, updated_at)
VALUES (?, ?, 3, ?, ?, ?, NULL, NULL, 'pending', 'pending', 'no_change_signal', ?, ?, ?, ?, ?, ?, ?, ?,
        1, ?, 60000, ?, ?, 1, 1, ?, ?, ?)`,
		runPublicID, incident.id, incident.cycleNo, originatingAgentRunValue, authorizationValue, triggerSignalID,
		snapshot.Plan.TargetRevision, snapshot.Plan.SourceRevision, snapshot.Plan.ImageDigest, snapshot.Plan.GitOpsRevision,
		planJSON, snapshot.Plan.ProfileVersion, snapshot.Plan.ProfileHash, snapshot.Plan.ProfileID,
		now.Add(snapshot.Plan.Deadline), attempt, incident.migratedLegacyContext, now, now)
	if err != nil {
		return false, err
	}
	runID, err := result.LastInsertId()
	if err != nil || runID <= 0 {
		return false, fmt.Errorf("read no-change VerificationRun id: %w", err)
	}
	for _, spec := range snapshot.Plan.Checks {
		subjectJSON, err := json.Marshal(spec.Subject)
		if err != nil {
			return false, err
		}
		var comparison any
		var threshold any
		if spec.Comparison != "" {
			comparison, threshold = string(spec.Comparison), spec.Threshold
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO verification_checks
 (public_id, verification_run_id, domain_schema_version, incident_id, cycle_no, check_type,
  status, required_check, subject_json, expected_json, source_reference, lookback_ms,
  stability_window_ms, timeout_ms, poll_interval_ms, check_spec_schema_version, profile_id,
  template_id, template_version, comparison, threshold, source_identity, initial_delay_ms,
  min_samples, sample_unit, failure_mode, migrated_legacy_context, created_at, updated_at)
VALUES (?, ?, 3, ?, ?, ?, 'pending', ?, ?, ?, '', ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), runID, incident.id, incident.cycleNo, spec.Type, spec.Required, subjectJSON, spec.Expected,
			spec.Lookback.Milliseconds(), spec.StabilityWindow.Milliseconds(), spec.Timeout.Milliseconds(), spec.PollInterval.Milliseconds(),
			spec.ProfileID, spec.TemplateID, spec.TemplateVersion, comparison, threshold, spec.SourceIdentity,
			spec.InitialDelay.Milliseconds(), spec.MinSamples, spec.SampleUnit, spec.FailureMode,
			incident.migratedLegacyContext, now, now); err != nil {
			return false, err
		}
	}
	updated, err := tx.ExecContext(ctx, `UPDATE incidents
SET status = 'VERIFYING', v3_status = 'verifying',
    version = version + 1, needs_attention = FALSE, blocking_reason_code = NULL,
    blocked_at = NULL, updated_at = ?
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ? AND v3_status = 'investigating'`,
		now, incident.id, incident.cycleNo, incident.version)
	if err != nil {
		return false, err
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return false, errors.New("incident changed before no-change verifying transition")
	}
	incident.status = domain.V3StatusVerifying
	incident.version++
	metadata, _ := json.Marshal(map[string]any{
		"verification_run_id": runPublicID, "trigger_signal_id": snapshot.TriggerSignalPublicID,
		"trigger_type": "no_change_signal", "baseline_id": snapshot.BaselinePublicID,
		"baseline_verification_hash": snapshot.BaselineVerificationHash,
		"profile_id":                 snapshot.Plan.ProfileID, "profile_hash": snapshot.Plan.ProfileHash, "attempt": attempt,
		"business_budget_authorization_id": budget.AuthorizationPublicID,
		"authorization_slot":               budget.AuthorizationSlot,
		"originating_agent_run_id":         budget.OriginatingAgentRunPublicID,
	})
	if err := appendEvent(ctx, tx, *incident, "verification_started", "system", "v3-ingress",
		"resolved Signal started no-change verification", now, metadata,
		hashCanonical("no-change", incident.publicID, fmt.Sprint(incident.cycleNo), fmt.Sprint(triggerSignalID), "verification-started")); err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]any{"verification_run_id": runPublicID, "cycle_no": incident.cycleNo})
	if _, err := tx.ExecContext(ctx, `INSERT INTO async_tasks
 (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
  expected_subject_version, payload_schema_version, payload_json, dedupe_key, replay_generation,
  migrated_legacy_context,status, priority, available_at, attempt, max_attempts, lease_generation, created_at, updated_at)
VALUES (?, ?, ?, 'verify', 'verification.advance', 'verification_run', ?, 'verification.advance',
	    1, 1, ?, ?, 0, ?, 'ready', 60, ?, 0, ?, 0, ?, ?)`, uuid.NewString(), incident.id,
		incident.cycleNo, runID, payload, hashCanonical("verification.advance", fmt.Sprint(runID), "1"),
		incident.migratedLegacyContext, now, verificationTaskMaxAttempts, now, now); err != nil {
		return false, err
	}
	return true, nil
}

func appendNoChangeRequestCancellation(ctx context.Context, tx *sql.Tx, incident incidentRow, changeID uint64, decision noChangeDecision) error {
	if changeID == 0 {
		return errors.New("cancelled ChangeRequest identity is missing")
	}
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_no), 0) + 1
FROM change_request_events WHERE change_request_id = ?`, changeID).Scan(&sequence); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"reason": decision.Reason, "external_write_started": false})
	_, err := tx.ExecContext(ctx, `INSERT INTO change_request_events
 (public_id, domain_schema_version, event_schema_version, incident_id, cycle_no,
  change_request_id, sequence_no, event_type, source_system, write_phase,
  external_write_started, external_write_marker, payload_json, content_hash, occurred_at, created_at)
VALUES (?, 3, 1, ?, ?, ?, ?, 'change_request_cancelled_before_write', 'system', NULL,
        FALSE, NULL, ?, ?, NOW(6), NOW(6))`, uuid.NewString(), incident.id, incident.cycleNo,
		changeID, sequence, payload, hashCanonical("change-request-event", fmt.Sprint(changeID), fmt.Sprint(sequence), string(payload)))
	return err
}

func appendNoChangeCancellationEvent(ctx context.Context, tx *sql.Tx, incident incidentRow, triggerSignalPublicID string, decision noChangeDecision, cancelled noChangeCancellation) error {
	metadata, _ := json.Marshal(map[string]any{
		"trigger_signal_id": triggerSignalPublicID, "reason": decision.Reason,
		"cancelled_agent_runs": cancelled.AgentRuns, "cancelled_plans": cancelled.Plans,
		"cancelled_change_requests": cancelled.Changes, "cancelled_tasks": cancelled.Tasks,
		"cancelled_attempts": cancelled.Attempts,
	})
	return appendEvent(ctx, tx, incident, "no_change_work_cancelled", "system", "v3-ingress",
		"resolved Signal cancelled safe pre-write work", time.Time{}, metadata,
		hashCanonical("no-change", incident.publicID, fmt.Sprint(incident.cycleNo), triggerSignalPublicID, "cancelled"))
}

func appendNoChangeDeferredEvent(ctx context.Context, tx *sql.Tx, incident incidentRow, triggerSignalPublicID, reason string) error {
	metadata, _ := json.Marshal(map[string]any{"trigger_signal_id": triggerSignalPublicID, "reason": reason})
	return appendEvent(ctx, tx, incident, "no_change_verification_deferred", "system", "v3-ingress",
		"resolved Signal was recorded while delivery or verification reconciliation remained active", time.Time{}, metadata,
		hashCanonical("no-change", incident.publicID, fmt.Sprint(incident.cycleNo), triggerSignalPublicID, "deferred", reason))
}
