package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

const workspaceRunColumns = `run.id, run.public_id, run.subject_type,
COALESCE(alert.public_id, ''), COALESCE(incident.public_id, ''), COALESCE(consultation.public_id, ''),
COALESCE(revision.public_id, ''), COALESCE(snapshot.public_id, ''),
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(snapshot.filters_json, '$.scenario_id')), ''), run.status, COALESCE(run.outcome, ''),
run.uncertainty, run.objective, COALESCE(run.model_provider, ''), COALESCE(run.actual_model, ''),
	COALESCE(run.final_answer, ''),
run.prompt_version, COALESCE(run.tool_schema_version, ''), run.failure_code, run.failure_summary,
run.cancel_requested_at, run.started_at, run.completed_at, run.created_at, run.updated_at,
(SELECT COUNT(*) FROM agent_evidence_citations AS citation_count WHERE citation_count.agent_run_id=run.id)`

const workspaceRunJoins = ` FROM agent_runs AS run
LEFT JOIN alerts AS alert ON alert.id = run.alert_id
LEFT JOIN incidents AS incident ON incident.id = run.incident_id
LEFT JOIN agent_consultations AS consultation ON consultation.id = run.consultation_id
LEFT JOIN configuration_revisions AS revision ON revision.id = run.configuration_revision_id
LEFT JOIN context_snapshots AS snapshot ON snapshot.id = run.context_snapshot_id`

type WorkspaceRepository struct {
	db         *sql.DB
	tasks      *asyncjob.Repository
	now        func() time.Time
	runbookDir string
}

type workspaceRunRecord struct {
	internalID uint64
	view       WorkspaceRun
}

func NewWorkspaceRepository(db *sql.DB) (*WorkspaceRepository, error) {
	if db == nil {
		return nil, errors.New("agent workspace repository requires MySQL")
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		return nil, fmt.Errorf("initialize Agent Workspace async task repository: %w", err)
	}
	return &WorkspaceRepository{db: db, tasks: tasks, now: time.Now}, nil
}

func (r *WorkspaceRepository) SetRunbookDir(dir string) {
	r.runbookDir = strings.TrimSpace(dir)
}

func (r *WorkspaceRepository) StartAlertInvestigation(ctx context.Context, alertPublicID, idempotencyKey, reason string) (WorkspaceRun, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return WorkspaceRun{}, err
	}
	defer workspaceRollback(tx)
	publicID, err := r.StartAlertInvestigationTx(ctx, tx, alertPublicID, idempotencyKey, reason)
	if err != nil {
		return WorkspaceRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkspaceRun{}, fmt.Errorf("commit Alert Investigation: %w", err)
	}
	return r.WorkspaceRun(ctx, publicID)
}

// StartIncidentInvestigationTx creates an Incident-scoped Workspace Run,
// immutable Context Snapshot, and durable Workspace task in the caller's
// command transaction. The Incident transition and current Run pointer are
// committed with the Workspace identities.
func (r *WorkspaceRepository) StartIncidentInvestigationTx(
	ctx context.Context,
	tx *sql.Tx,
	incidentPublicID, idempotencyKey, reason string,
	expectedVersion, authorizationID uint64,
) (string, error) {
	if tx == nil || expectedVersion == 0 {
		return "", ErrInvalidArgument
	}
	incidentPublicID = strings.TrimSpace(incidentPublicID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if incidentPublicID == "" || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return "", ErrInvalidArgument
	}

	var (
		incidentID, revisionID, scopeID                           uint64
		cycleNo, version                                          uint64
		clusterID, environment, namespace, targetKind, targetName string
		summary, scopePublicID                                    string
		firstSeenAt                                               time.Time
		migratedLegacyContext                                     bool
	)
	err := tx.QueryRowContext(ctx, `SELECT incident.id, incident.cycle_no, incident.version,
incident.cluster, incident.environment, incident.namespace, incident.target_kind, incident.target_name,
incident.summary, incident.first_seen_at,
incident.migrated_legacy_context,
active.configuration_revision_id, scope.id, scope.public_id
FROM incidents AS incident
JOIN active_configuration AS active ON active.singleton_id = 1
JOIN operational_scopes AS scope
  ON scope.configuration_revision_id = active.configuration_revision_id
 AND scope.cluster_id = incident.cluster
WHERE incident.public_id = ?
ORDER BY scope.is_default DESC, scope.id
LIMIT 1 FOR UPDATE`, incidentPublicID).Scan(
		&incidentID, &cycleNo, &version, &clusterID, &environment, &namespace, &targetKind, &targetName,
		&summary, &firstSeenAt, &migratedLegacyContext,
		&revisionID, &scopeID, &scopePublicID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock Incident Workspace subject: %w", err)
	}
	if version != expectedVersion || cycleNo == 0 {
		return "", ErrConflict
	}

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT public_id FROM agent_runs
WHERE run_kind='workspace' AND subject_type='incident' AND incident_id=? AND cycle_no=? AND idempotency_key=?
ORDER BY id DESC LIMIT 1`, incidentID, cycleNo, idempotencyKey).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	scenarioID, err := workspaceIncidentScenarioID(ctx, tx, incidentID, cycleNo)
	if err != nil {
		return "", fmt.Errorf("resolve Incident Scenario identity: %w", err)
	}

	now := r.now().UTC()
	objective := strings.TrimSpace(reason)
	if objective == "" {
		objective = "调查 Incident：" + summary
	}
	var authorization any
	if authorizationID != 0 {
		authorization = authorizationID
	}
	runPublicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, subject_type, incident_id, alert_id, consultation_id, configuration_revision_id,
context_snapshot_id, cycle_no, expected_incident_version, business_budget_authorization_id,
idempotency_key, run_kind, status, objective, model, prompt_version,
max_steps, max_tool_calls, max_model_calls, token_budget, max_evidence_items,
max_runtime_ms, tool_timeout_ms, max_evidence_bytes, max_checkpoint_bytes,
max_step_retries, failure_code, uncertainty, migrated_legacy_context, created_at, updated_at
) VALUES (?, 'incident', ?, NULL, NULL, ?, NULL, ?, ?, ?, ?, 'workspace', 'pending',
	?, 'provider-pending', ?, 12, 8, 3, 12000, 12, 120000, 15000, 16384, 32768, 1, '',
'unknown', ?, ?, ?)`, runPublicID, incidentID, revisionID, cycleNo, expectedVersion+1,
		authorization, idempotencyKey, objective, WorkspacePromptVersion, migratedLegacyContext, now, now)
	if err != nil {
		return "", fmt.Errorf("persist Incident Investigation run: %w", err)
	}
	runIDValue, err := result.LastInsertId()
	if err != nil || runIDValue <= 0 {
		return "", fmt.Errorf("read Incident Investigation run id: %w", err)
	}
	runID := uint64(runIDValue)

	from, to := workspaceInvestigationWindow(firstSeenAt, now)
	resources := []telemetry.ResourceReference{{
		ID:   workspaceKubernetesResourceID(clusterID, targetKind, namespace, targetName),
		Kind: targetKind, Namespace: namespace, Name: targetName,
	}}
	namespacesJSON, _ := json.Marshal([]string{namespace})
	resourcesJSON, _ := json.Marshal(resources)
	filters := map[string]any{
		"incident_id": incidentPublicID, "cycle_no": cycleNo, "operational_scope_id": scopePublicID,
		"subject_summary": summary,
	}
	if scenarioID != "" {
		filters["scenario_id"] = scenarioID
	}
	filtersJSON, _ := json.Marshal(filters)
	canonical, _ := json.Marshal(map[string]any{
		"subject_type": "incident", "incident_id": incidentPublicID, "cycle_no": cycleNo,
		"configuration_revision_id": revisionID, "operational_scope_id": scopeID,
		"cluster_id": clusterID, "environment": environment, "namespaces": []string{namespace},
		"resources": resources, "filters": filters, "from": from, "to": to,
	})
	snapshotPublicID := uuid.NewString()
	snapshotResult, err := tx.ExecContext(ctx, `INSERT INTO context_snapshots (
public_id, consultation_id, agent_run_id, subject_type, configuration_revision_id, cluster_id,
environment, namespaces_json, resource_refs_json, filters_json, range_start, range_end,
query_execution_refs_json, query_definition_refs_json, evidence_refs_json, content_hash,
created_by, created_at
) VALUES (?, NULL, ?, 'incident', ?, ?, ?, ?, ?, ?, ?, ?, JSON_ARRAY(), JSON_ARRAY(), JSON_ARRAY(), ?, 'local-owner', ?)`,
		snapshotPublicID, runID, revisionID, clusterID, environment, namespacesJSON, resourcesJSON,
		filtersJSON, from, to, workspaceSHA256(canonical), now)
	if err != nil {
		return "", fmt.Errorf("persist Incident Context Snapshot: %w", err)
	}
	snapshotID, err := snapshotResult.LastInsertId()
	if err != nil || snapshotID <= 0 {
		return "", fmt.Errorf("read Incident Context Snapshot id: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET context_snapshot_id=? WHERE id=? AND context_snapshot_id IS NULL`, snapshotID, runID); err != nil {
		return "", err
	}
	if err = enqueueWorkspaceTask(ctx, tx, runID, revisionID, now.Add(ownerInvestigationClaimGrace), now); err != nil {
		return "", err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE incidents
SET status='investigating', current_agent_run_id=?, version=version+1, updated_at=?
WHERE id=? AND cycle_no=? AND version=? AND status IN ('detected','investigating')`,
		runID, now, incidentID, cycleNo, expectedVersion)
	if err != nil {
		return "", fmt.Errorf("advance Incident Investigation: %w", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return "", ErrConflict
	}
	if err = insertWorkspaceEvent(ctx, tx, runID, nil, 1, "run.created", map[string]any{
		"run_id": runPublicID, "subject_type": "incident", "incident_id": incidentPublicID,
		"cycle_no": cycleNo, "context_snapshot_id": snapshotPublicID,
		"migrated_legacy_context": migratedLegacyContext,
	}, now); err != nil {
		return "", err
	}
	return runPublicID, nil
}

// StartAlertInvestigationTx creates the immutable Alert snapshot and durable
// Workspace task in the caller's Alert command transaction.
func (r *WorkspaceRepository) StartAlertInvestigationTx(ctx context.Context, tx *sql.Tx, alertPublicID, idempotencyKey, reason string) (string, error) {
	if tx == nil {
		return "", ErrInvalidArgument
	}
	var err error
	var alertID, revisionID uint64
	var clusterID, environment, namespace, targetKind, targetName, summary string
	var labelsJSON json.RawMessage
	var startsAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT alert.id, alert.cluster, alert.environment, alert.namespace,
alert.target_kind, alert.target_name, alert.summary, alert.starts_at,
alert.labels_json, active.configuration_revision_id
FROM alerts AS alert JOIN active_configuration AS active ON active.singleton_id = 1
WHERE alert.public_id = ? FOR UPDATE`, strings.TrimSpace(alertPublicID)).Scan(
		&alertID, &clusterID, &environment, &namespace, &targetKind, &targetName, &summary,
		&startsAt, &labelsJSON, &revisionID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock Alert Investigation subject: %w", err)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT public_id FROM agent_runs
WHERE run_kind='workspace' AND subject_type='alert' AND alert_id=? AND idempotency_key=?
	ORDER BY id DESC LIMIT 1`, alertID, idempotencyKey).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	now := r.now().UTC()
	runPublicID := uuid.NewString()
	objective := strings.TrimSpace(reason)
	if objective == "" {
		objective = "调查 Alert：" + summary
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, subject_type, incident_id, alert_id, consultation_id, configuration_revision_id,
context_snapshot_id, cycle_no, expected_incident_version, idempotency_key, run_kind, status,
objective, model, prompt_version, max_steps, max_tool_calls, max_model_calls, token_budget,
max_evidence_items, max_runtime_ms, tool_timeout_ms, max_evidence_bytes, max_checkpoint_bytes,
max_step_retries, failure_code, uncertainty, created_at, updated_at
) VALUES (?, 'alert', NULL, ?, NULL, ?, NULL, NULL, 0, ?, 'workspace', 'pending',
?, 'provider-pending', ?, 12, 8, 1, 12000, 12, 120000, 15000, 16384, 32768, 1, '', 'unknown', ?, ?)`,
		runPublicID, alertID, revisionID, idempotencyKey, objective, WorkspacePromptVersion, now, now)
	if err != nil {
		return "", fmt.Errorf("persist Alert Investigation run: %w", err)
	}
	runID, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	from, to := workspaceInvestigationWindow(startsAt, now)
	resources := []telemetry.ResourceReference{{
		ID:   workspaceKubernetesResourceID(clusterID, targetKind, namespace, targetName),
		Kind: targetKind, Namespace: namespace, Name: targetName,
	}}
	namespacesJSON, _ := json.Marshal([]string{namespace})
	resourcesJSON, _ := json.Marshal(resources)
	scenarioID := workspaceScenarioID(labelsJSON)
	filters := map[string]string{
		"alert_id":        alertPublicID,
		"alert_name":      workspaceAlertLabel(labelsJSON, "alertname"),
		"subject_summary": summary,
	}
	if scenarioID != "" {
		filters["scenario_id"] = scenarioID
	}
	filtersJSON, _ := json.Marshal(filters)
	canonical, _ := json.Marshal(map[string]any{
		"subject_type": "alert", "alert_id": alertPublicID, "configuration_revision_id": revisionID,
		"cluster_id": clusterID, "environment": environment, "namespaces": []string{namespace},
		"resources": resources, "filters": filters, "from": from, "to": to,
	})
	snapshotPublicID := uuid.NewString()
	snapshotResult, err := tx.ExecContext(ctx, `INSERT INTO context_snapshots (
public_id, consultation_id, agent_run_id, subject_type, configuration_revision_id, cluster_id,
environment, namespaces_json, resource_refs_json, filters_json, range_start, range_end,
query_execution_refs_json, query_definition_refs_json, evidence_refs_json, content_hash,
created_by, created_at
) VALUES (?, NULL, ?, 'alert', ?, ?, ?, ?, ?, ?, ?, ?, JSON_ARRAY(), JSON_ARRAY(), JSON_ARRAY(), ?, 'local-owner', ?)`,
		snapshotPublicID, runID, revisionID, clusterID, environment, namespacesJSON, resourcesJSON,
		filtersJSON, from, to, workspaceSHA256(canonical), now)
	if err != nil {
		return "", fmt.Errorf("persist Alert Context Snapshot: %w", err)
	}
	snapshotID, err := snapshotResult.LastInsertId()
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET context_snapshot_id=? WHERE id=? AND context_snapshot_id IS NULL`, snapshotID, runID); err != nil {
		return "", err
	}
	if err = enqueueWorkspaceTask(ctx, tx, uint64(runID), revisionID, now.Add(ownerInvestigationClaimGrace), now); err != nil {
		return "", err
	}
	if err = insertWorkspaceEvent(ctx, tx, uint64(runID), nil, 1, "run.created", map[string]any{
		"run_id": runPublicID, "subject_type": "alert", "alert_id": alertPublicID,
		"context_snapshot_id": snapshotPublicID,
	}, now); err != nil {
		return "", err
	}
	return runPublicID, nil
}

func workspaceInvestigationWindow(firstSeen, now time.Time) (time.Time, time.Time) {
	to := now.UTC()
	from := firstSeen.UTC().Add(-5 * time.Minute)
	if from.After(to) {
		from = to.Add(-5 * time.Minute)
	}
	if to.Sub(from) > 24*time.Hour {
		from = to.Add(-24 * time.Hour)
	}
	return from, to
}

func workspaceScenarioID(labelsJSON json.RawMessage) string {
	value := workspaceAlertLabel(labelsJSON, "scenario_id")
	if value == "" || !workspaceScenarioIdentity(value) {
		return ""
	}
	return value
}

func workspaceAlertLabel(labelsJSON json.RawMessage, key string) string {
	var labels map[string]string
	if json.Unmarshal(labelsJSON, &labels) != nil {
		return ""
	}
	return strings.TrimSpace(labels[key])
}

func workspaceScenarioIdentity(value string) bool {
	const prefix = "scenario-"
	if len(value) <= len(prefix) || len(value) > 63 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for index := len(prefix); index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	last := value[len(value)-1]
	return (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
}

func workspaceIncidentScenarioID(ctx context.Context, tx *sql.Tx, incidentID, cycleNo uint64) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(alert.labels_json, '$.scenario_id')), '')
FROM alert_incident_links AS relation
JOIN alerts AS alert ON alert.id = relation.alert_id
WHERE relation.incident_id = ? AND relation.incident_cycle_no = ?
ORDER BY 1 LIMIT 2`, incidentID, cycleNo)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	identities := make([]string, 0, 2)
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value != "" && !workspaceScenarioIdentity(value) {
			return "", ErrConflict
		}
		identities = append(identities, value)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(identities) > 1 {
		return "", ErrConflict
	}
	if len(identities) == 1 {
		return identities[0], nil
	}
	return "", nil
}

func (r *WorkspaceRepository) CreateConsultationTurn(ctx context.Context, consultationPublicID string, request SendMessageRequest) (ConsultationMessage, WorkspaceRun, error) {
	consultationPublicID = strings.TrimSpace(consultationPublicID)
	request.Content = strings.TrimSpace(request.Content)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if consultationPublicID == "" || request.Content == "" || utf8.RuneCountInString(request.Content) > 16000 || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 {
		return ConsultationMessage{}, WorkspaceRun{}, ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	defer workspaceRollback(tx)
	var consultationID uint64
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id,status FROM agent_consultations WHERE public_id=? FOR UPDATE`, consultationPublicID).Scan(&consultationID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ConsultationMessage{}, WorkspaceRun{}, ErrNotFound
	}
	if err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	if status != "open" {
		return ConsultationMessage{}, WorkspaceRun{}, ErrConflict
	}
	var existingRunID string
	err = tx.QueryRowContext(ctx, `SELECT public_id FROM agent_runs WHERE run_kind='workspace'
AND subject_type='consultation' AND consultation_id=? AND idempotency_key=? ORDER BY id DESC LIMIT 1`, consultationID, request.IdempotencyKey).Scan(&existingRunID)
	if err == nil {
		var messageID string
		if scanErr := tx.QueryRowContext(ctx, `SELECT public_id FROM agent_consultation_messages
WHERE agent_run_id=(SELECT id FROM agent_runs WHERE public_id=?) AND role='owner' LIMIT 1`, existingRunID).Scan(&messageID); scanErr != nil {
			return ConsultationMessage{}, WorkspaceRun{}, scanErr
		}
		if err = tx.Commit(); err != nil {
			return ConsultationMessage{}, WorkspaceRun{}, err
		}
		message, loadErr := r.ConsultationMessage(ctx, messageID)
		if loadErr != nil {
			return ConsultationMessage{}, WorkspaceRun{}, loadErr
		}
		run, loadErr := r.WorkspaceRun(ctx, existingRunID)
		return message, run, loadErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	var snapshotID, revisionID uint64
	var snapshotPublicID string
	err = tx.QueryRowContext(ctx, `SELECT snapshot.id,snapshot.public_id,snapshot.configuration_revision_id
FROM context_snapshots AS snapshot WHERE snapshot.consultation_id=?
ORDER BY snapshot.created_at DESC,snapshot.id DESC LIMIT 1`, consultationID).Scan(&snapshotID, &snapshotPublicID, &revisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return ConsultationMessage{}, WorkspaceRun{}, ErrConflict
	}
	if err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE consultation_id=? AND run_kind='workspace'
AND status IN ('pending','running')`, consultationID).Scan(&active); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	if active != 0 {
		return ConsultationMessage{}, WorkspaceRun{}, ErrConflict
	}
	now := r.now().UTC()
	runPublicID := uuid.NewString()
	runResult, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, subject_type, incident_id, alert_id, consultation_id, configuration_revision_id,
context_snapshot_id, cycle_no, expected_incident_version, idempotency_key, run_kind, status,
objective, model, prompt_version, max_steps, max_tool_calls, max_model_calls, token_budget,
max_evidence_items, max_runtime_ms, tool_timeout_ms, max_evidence_bytes, max_checkpoint_bytes,
max_step_retries, failure_code, uncertainty, created_at, updated_at
) VALUES (?, 'consultation', NULL, NULL, ?, ?, ?, NULL, 0, ?, 'workspace', 'pending',
?, 'provider-pending', ?, 12, 8, 1, 12000, 12, 120000, 15000, 16384, 32768, 1, '', 'unknown', ?, ?)`,
		runPublicID, consultationID, revisionID, snapshotID, request.IdempotencyKey,
		request.Content, WorkspacePromptVersion, now, now)
	if err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, fmt.Errorf("persist Consultation turn: %w", err)
	}
	runID64, _ := runResult.LastInsertId()
	if err = enqueueWorkspaceTask(ctx, tx, uint64(runID64), revisionID, now, now); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	var sequence uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM agent_consultation_messages WHERE consultation_id=?`, consultationID).Scan(&sequence); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	messagePublicID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_consultation_messages (
public_id,consultation_id,agent_run_id,context_snapshot_id,sequence,role,content,status,created_at,completed_at
) VALUES (?,?,?,?,?,'owner',?,'completed',?,?)`, messagePublicID, consultationID, runID64, snapshotID,
		sequence, request.Content, now, now); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, fmt.Errorf("persist Owner Consultation message: %w", err)
	}
	if err = insertWorkspaceEvent(ctx, tx, uint64(runID64), &consultationID, 1, "run.created", map[string]any{
		"run_id": runPublicID, "consultation_id": consultationPublicID, "context_snapshot_id": snapshotPublicID,
	}, now); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_consultations SET updated_at=? WHERE id=?`, now, consultationID); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	message, err := r.ConsultationMessage(ctx, messagePublicID)
	if err != nil {
		return ConsultationMessage{}, WorkspaceRun{}, err
	}
	run, err := r.WorkspaceRun(ctx, runPublicID)
	return message, run, err
}

func (r *WorkspaceRepository) WorkspaceRun(ctx context.Context, publicID string) (WorkspaceRun, error) {
	record, err := scanWorkspaceRun(r.db.QueryRowContext(ctx, `SELECT `+workspaceRunColumns+workspaceRunJoins+`
WHERE run.public_id=? AND run.run_kind='workspace'`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceRun{}, ErrNotFound
	}
	if err != nil {
		return WorkspaceRun{}, err
	}
	view := record.view
	view.Steps, err = r.workspaceSteps(ctx, record.internalID)
	if err != nil {
		return WorkspaceRun{}, err
	}
	view.Evidence, err = r.evidenceCitations(ctx, record.internalID, 0)
	if err != nil {
		return WorkspaceRun{}, err
	}
	view.Guidance, err = r.guidanceCitations(ctx, record.internalID, 0)
	if err != nil {
		return WorkspaceRun{}, err
	}
	view.ActionCards, err = r.actionCards(ctx, record.internalID)
	if err != nil {
		return WorkspaceRun{}, err
	}
	view.OperationPlans, err = r.operationPlans(ctx, record.internalID)
	return view, err
}

func (r *WorkspaceRepository) WorkspaceRuns(ctx context.Context, limit int) ([]WorkspaceRun, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+workspaceRunColumns+workspaceRunJoins+`
WHERE run.run_kind='workspace' ORDER BY run.created_at DESC,run.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]WorkspaceRun, 0)
	for rows.Next() {
		record, scanErr := scanWorkspaceRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, record.view)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) Consultations(ctx context.Context, limit int) ([]ConsultationSummary, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT consultation.public_id,consultation.title,consultation.status,
snapshot.public_id,revision.public_id,snapshot.cluster_id,snapshot.environment,snapshot.namespaces_json,
(SELECT COUNT(*) FROM agent_consultation_messages message WHERE message.consultation_id=consultation.id),
consultation.created_at,consultation.updated_at
FROM agent_consultations AS consultation
JOIN context_snapshots AS snapshot ON snapshot.id=(SELECT current.id FROM context_snapshots current
  WHERE current.consultation_id=consultation.id ORDER BY current.created_at DESC,current.id DESC LIMIT 1)
JOIN configuration_revisions AS revision ON revision.id=snapshot.configuration_revision_id
ORDER BY consultation.updated_at DESC,consultation.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConsultationSummary, 0)
	for rows.Next() {
		var item ConsultationSummary
		var revisionID string
		var namespacesJSON []byte
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &item.ActiveSnapshotID, &revisionID,
			&item.Scope.ClusterID, &item.Scope.Environment, &namespacesJSON, &item.MessageCount,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(namespacesJSON, &item.Scope.Namespaces); err != nil {
			return nil, err
		}
		item.Scope.RevisionID = revisionID
		item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) Consultation(ctx context.Context, publicID string) (ConsultationDetail, error) {
	items, err := r.Consultations(ctx, 100)
	if err != nil {
		return ConsultationDetail{}, err
	}
	var detail ConsultationDetail
	for _, item := range items {
		if item.ID == publicID {
			detail.ConsultationSummary = item
			break
		}
	}
	if detail.ID == "" {
		return ConsultationDetail{}, ErrNotFound
	}
	var consultationID uint64
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM agent_consultations WHERE public_id=?`, publicID).Scan(&consultationID); err != nil {
		return ConsultationDetail{}, err
	}
	detail.Snapshots, err = r.contextSnapshots(ctx, consultationID)
	if err != nil {
		return ConsultationDetail{}, err
	}
	detail.Messages, err = r.consultationMessages(ctx, consultationID)
	if err != nil {
		return ConsultationDetail{}, err
	}
	var activeRunID string
	activeErr := r.db.QueryRowContext(ctx, `SELECT public_id FROM agent_runs
WHERE consultation_id=? AND run_kind='workspace' ORDER BY created_at DESC,id DESC LIMIT 1`, consultationID).Scan(&activeRunID)
	if activeErr == nil {
		activeRun, loadErr := r.WorkspaceRun(ctx, activeRunID)
		if loadErr != nil {
			return ConsultationDetail{}, loadErr
		}
		detail.ActiveRun = &activeRun
	} else if !errors.Is(activeErr, sql.ErrNoRows) {
		return ConsultationDetail{}, activeErr
	}
	return detail, nil
}

func (r *WorkspaceRepository) ConsultationMessage(ctx context.Context, publicID string) (ConsultationMessage, error) {
	row := r.db.QueryRowContext(ctx, `SELECT message.id,message.public_id,consultation.public_id,
COALESCE(run.public_id,''),snapshot.public_id,message.sequence,message.role,message.content,message.status,
message.created_at,message.completed_at
FROM agent_consultation_messages AS message
JOIN agent_consultations AS consultation ON consultation.id=message.consultation_id
LEFT JOIN agent_runs AS run ON run.id=message.agent_run_id
JOIN context_snapshots AS snapshot ON snapshot.id=message.context_snapshot_id
WHERE message.public_id=?`, publicID)
	message, internalID, err := scanConsultationMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ConsultationMessage{}, ErrNotFound
	}
	if err != nil {
		return ConsultationMessage{}, err
	}
	var runID uint64
	if message.RunID != "" {
		_ = r.db.QueryRowContext(ctx, `SELECT id FROM agent_runs WHERE public_id=?`, message.RunID).Scan(&runID)
	}
	message.Evidence, err = r.evidenceCitations(ctx, runID, internalID)
	if err != nil {
		return ConsultationMessage{}, err
	}
	message.Guidance, err = r.guidanceCitations(ctx, runID, internalID)
	return message, err
}

func (r *WorkspaceRepository) RequestCancel(ctx context.Context, publicID string) (WorkspaceRun, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return WorkspaceRun{}, err
	}
	defer workspaceRollback(tx)
	var runID uint64
	var status string
	var consultationID sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT id,status,consultation_id FROM agent_runs
WHERE public_id=? AND run_kind='workspace' FOR UPDATE`, publicID).Scan(&runID, &status, &consultationID); errors.Is(err, sql.ErrNoRows) {
		return WorkspaceRun{}, ErrNotFound
	} else if err != nil {
		return WorkspaceRun{}, err
	}
	if status != "pending" && status != "running" {
		return WorkspaceRun{}, ErrConflict
	}
	now := r.now().UTC()
	if status == "pending" {
		if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status='cancelled',outcome='cancelled',uncertainty='unknown',
cancel_requested_at=?,completed_at=?,updated_at=?,row_version=row_version+1 WHERE id=? AND status='pending'`, now, now, now, runID); err != nil {
			return WorkspaceRun{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_consultation_messages SET status='cancelled',completed_at=?
WHERE agent_run_id=? AND role='owner'`, now, runID); err != nil {
			return WorkspaceRun{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_tasks SET status='cancelled',cancelled_at=?,updated_at=?
WHERE agent_run_id=? AND status='ready'`, now, now, runID); err != nil {
			return WorkspaceRun{}, err
		}
		if err = insertWorkspaceEvent(ctx, tx, runID, nullableUint64(consultationID), 2, "run.cancelled", map[string]any{"outcome": "cancelled"}, now); err != nil {
			return WorkspaceRun{}, err
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET cancel_requested_at=COALESCE(cancel_requested_at,?),updated_at=?
WHERE id=? AND status='running'`, now, now, runID); err != nil {
		return WorkspaceRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkspaceRun{}, err
	}
	return r.WorkspaceRun(ctx, publicID)
}

func (r *WorkspaceRepository) RequestConsultationCancel(ctx context.Context, consultationPublicID string) (WorkspaceRun, error) {
	var runPublicID string
	err := r.db.QueryRowContext(ctx, `SELECT run.public_id FROM agent_runs run
JOIN agent_consultations consultation ON consultation.id=run.consultation_id
WHERE consultation.public_id=? AND run.run_kind='workspace' AND run.status IN ('pending','running')
ORDER BY run.created_at DESC,run.id DESC LIMIT 1`, strings.TrimSpace(consultationPublicID)).Scan(&runPublicID)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceRun{}, ErrConflict
	}
	if err != nil {
		return WorkspaceRun{}, err
	}
	return r.RequestCancel(ctx, runPublicID)
}

func (r *WorkspaceRepository) StreamEvents(ctx context.Context, consultationPublicID, lastEventID string, limit int) ([]StreamEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	var consultationID, afterID uint64
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM agent_consultations WHERE public_id=?`, consultationPublicID).Scan(&consultationID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if strings.TrimSpace(lastEventID) != "" {
		if err := r.db.QueryRowContext(ctx, `SELECT id FROM agent_stream_events WHERE public_id=? AND consultation_id=?`, lastEventID, consultationID).Scan(&afterID); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidArgument
		} else if err != nil {
			return nil, err
		}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT event.public_id,run.public_id,consultation.public_id,
event.sequence,event.event_type,event.payload_json,event.created_at
FROM agent_stream_events AS event JOIN agent_runs AS run ON run.id=event.agent_run_id
JOIN agent_consultations AS consultation ON consultation.id=event.consultation_id
WHERE event.consultation_id=? AND event.id>? ORDER BY event.id LIMIT ?`, consultationID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]StreamEvent, 0)
	for rows.Next() {
		var item StreamEvent
		if err := rows.Scan(&item.ID, &item.RunID, &item.ConsultationID, &item.Sequence, &item.Type, &item.Payload, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanWorkspaceRun(scanner interface{ Scan(...any) error }) (workspaceRunRecord, error) {
	var result workspaceRunRecord
	var outcome string
	var cancel, started, completed sql.NullTime
	var provider, model, toolVersion string
	err := scanner.Scan(&result.internalID, &result.view.ID, &result.view.SubjectType,
		&result.view.AlertID, &result.view.IncidentID, &result.view.ConsultationID,
		&result.view.ConfigurationRevisionID, &result.view.ContextSnapshotID, &result.view.ScenarioID, &result.view.Status, &outcome,
		&result.view.Uncertainty, &result.view.Objective, &provider, &model, &result.view.Answer, &result.view.PromptVersion,
		&toolVersion, &result.view.FailureCode, &result.view.FailureSummary, &cancel, &started, &completed,
		&result.view.CreatedAt, &result.view.UpdatedAt, &result.view.EvidenceCount)
	if err != nil {
		return workspaceRunRecord{}, err
	}
	result.view.Outcome = WorkspaceOutcome(outcome)
	result.view.ModelProvider, result.view.ActualModel, result.view.ToolSchemaVersion = provider, model, toolVersion
	result.view.CancelRequestedAt = workspaceOptionalTime(cancel)
	result.view.StartedAt = workspaceOptionalTime(started)
	result.view.CompletedAt = workspaceOptionalTime(completed)
	result.view.CreatedAt, result.view.UpdatedAt = result.view.CreatedAt.UTC(), result.view.UpdatedAt.UTC()
	result.view.Steps = []WorkspaceStep{}
	result.view.Evidence = []EvidenceCitation{}
	result.view.Guidance = []GuidanceCitation{}
	result.view.ActionCards = []ActionCard{}
	result.view.OperationPlans = []OperationPlan{}
	return result, nil
}

func (r *WorkspaceRepository) workspaceSteps(ctx context.Context, runID uint64) ([]WorkspaceStep, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT public_id,sequence,step_type,selected_tool,arguments_json,
status,result_summary,evidence_public_id,duration_ms,error_code,started_at,finished_at,created_at
FROM agent_steps WHERE agent_run_id=? ORDER BY sequence,id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]WorkspaceStep, 0)
	for rows.Next() {
		var item WorkspaceStep
		var arguments json.RawMessage
		var started, finished sql.NullTime
		if err := rows.Scan(&item.ID, &item.Sequence, &item.Type, &item.Tool, &arguments, &item.Status,
			&item.ResultSummary, &item.EvidenceID, &item.DurationMS, &item.ErrorCode, &started, &finished, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Scope = arguments
		var target struct {
			Resource string `json:"resource"`
		}
		_ = json.Unmarshal(arguments, &target)
		item.Target = target.Resource
		item.StartedAt, item.FinishedAt = workspaceOptionalTime(started), workspaceOptionalTime(finished)
		item.CreatedAt = item.CreatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) evidenceCitations(ctx context.Context, runID, messageID uint64) ([]EvidenceCitation, error) {
	if runID == 0 {
		return []EvidenceCitation{}, nil
	}
	query := `SELECT citation.public_id,evidence.public_id,citation.citation_use,evidence.source,evidence.summary,
COALESCE(execution.public_id,''),COALESCE(query_revision.public_id,run_revision.public_id,''),
evidence.resource_ref,evidence.time_range_json,evidence.observed_at,evidence.collected_at,evidence.content_hash,
evidence.facts_json,evidence.trust_axes_json
FROM agent_evidence_citations AS citation
JOIN evidence_items AS evidence ON evidence.id=citation.evidence_item_id
LEFT JOIN query_executions AS execution ON execution.id=evidence.query_execution_id
LEFT JOIN configuration_revisions AS query_revision ON query_revision.id=execution.configuration_revision_id
LEFT JOIN agent_runs AS producer_run ON producer_run.id=evidence.agent_run_id
LEFT JOIN configuration_revisions AS run_revision ON run_revision.id=producer_run.configuration_revision_id
WHERE citation.agent_run_id=?`
	args := []any{runID}
	if messageID != 0 {
		query += " AND citation.message_id=?"
		args = append(args, messageID)
	}
	query += " ORDER BY citation.id"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]EvidenceCitation, 0)
	for rows.Next() {
		var item EvidenceCitation
		var observed sql.NullTime
		if err := rows.Scan(&item.ID, &item.EvidenceID, &item.Use, &item.Source, &item.Summary,
			&item.QueryExecutionID, &item.ConfigurationRevisionID, &item.ResourceRef, &item.TimeRange,
			&observed, &item.CollectedAt, &item.ContentHash, &item.Facts, &item.TrustAxes); err != nil {
			return nil, err
		}
		item.ObservedAt = workspaceOptionalTime(observed)
		item.CollectedAt = item.CollectedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) guidanceCitations(ctx context.Context, runID, messageID uint64) ([]GuidanceCitation, error) {
	if runID == 0 {
		return []GuidanceCitation{}, nil
	}
	query := `SELECT citation.public_id,citation.guidance_type,COALESCE(item.public_id,''),
COALESCE(revision.public_id,citation.runbook_revision),COALESCE(revision.revision_no,0),
COALESCE(item.title,citation.runbook_path),COALESCE(revision.source_type,'git'),
COALESCE(revision.created_at,citation.created_at),revision.review_at,revision.expires_at
FROM agent_guidance_citations AS citation
LEFT JOIN knowledge_item_revisions AS revision ON revision.id=citation.knowledge_revision_id
LEFT JOIN knowledge_items AS item ON item.id=revision.knowledge_item_id
WHERE citation.agent_run_id=?`
	args := []any{runID}
	if messageID != 0 {
		query += " AND citation.message_id=?"
		args = append(args, messageID)
	}
	query += " ORDER BY citation.id"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	now := r.now().UTC()
	result := make([]GuidanceCitation, 0)
	for rows.Next() {
		var item GuidanceCitation
		var review, expiry sql.NullTime
		if err := rows.Scan(&item.ID, &item.Type, &item.KnowledgeItemID, &item.RevisionID, &item.Revision,
			&item.Title, &item.Source, &item.CreatedAt, &review, &expiry); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.ReviewAt, item.ExpiresAt = workspaceOptionalTime(review), workspaceOptionalTime(expiry)
		item.AgeSeconds = maxWorkspaceInt64(0, int64(now.Sub(item.CreatedAt).Seconds()))
		item.Stale = item.ExpiresAt != nil && !item.ExpiresAt.After(now) || item.ReviewAt != nil && !item.ReviewAt.After(now)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) contextSnapshots(ctx context.Context, consultationID uint64) ([]WorkspaceContextSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT snapshot.public_id,consultation.public_id,COALESCE(run.public_id,''),
snapshot.subject_type,revision.public_id,snapshot.cluster_id,snapshot.environment,snapshot.namespaces_json,
snapshot.resource_refs_json,snapshot.filters_json,snapshot.range_start,snapshot.range_end,
snapshot.query_definition_refs_json,snapshot.query_execution_refs_json,snapshot.evidence_refs_json,
snapshot.content_hash,snapshot.created_at
FROM context_snapshots AS snapshot
LEFT JOIN agent_consultations AS consultation ON consultation.id=snapshot.consultation_id
LEFT JOIN agent_runs AS run ON run.id=snapshot.agent_run_id
JOIN configuration_revisions AS revision ON revision.id=snapshot.configuration_revision_id
WHERE snapshot.consultation_id=? ORDER BY snapshot.created_at,snapshot.id`, consultationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]WorkspaceContextSnapshot, 0)
	for rows.Next() {
		var item WorkspaceContextSnapshot
		var namespaces, resources, definitions, queries, evidence []byte
		if err := rows.Scan(&item.ID, &item.ConsultationID, &item.RunID, &item.SubjectType,
			&item.ConfigurationRevisionID, &item.Scope.ClusterID, &item.Scope.Environment, &namespaces,
			&resources, &item.Filters, &item.TimeRange.From, &item.TimeRange.To, &definitions, &queries,
			&evidence, &item.ContentHash, &item.CreatedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal(namespaces, &item.Scope.Namespaces) != nil || json.Unmarshal(resources, &item.Resources) != nil ||
			json.Unmarshal(definitions, &item.QueryDefinitionIDs) != nil || json.Unmarshal(queries, &item.QueryExecutionIDs) != nil ||
			json.Unmarshal(evidence, &item.EvidenceIDs) != nil {
			return nil, ErrUnavailable
		}
		item.Scope.RevisionID = item.ConfigurationRevisionID
		item.TimeRange.From, item.TimeRange.To = item.TimeRange.From.UTC(), item.TimeRange.To.UTC()
		item.CreatedAt = item.CreatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) consultationMessages(ctx context.Context, consultationID uint64) ([]ConsultationMessage, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT message.id,message.public_id,consultation.public_id,
COALESCE(run.public_id,''),snapshot.public_id,message.sequence,message.role,message.content,message.status,
message.created_at,message.completed_at
FROM agent_consultation_messages AS message
JOIN agent_consultations AS consultation ON consultation.id=message.consultation_id
LEFT JOIN agent_runs AS run ON run.id=message.agent_run_id
JOIN context_snapshots AS snapshot ON snapshot.id=message.context_snapshot_id
WHERE message.consultation_id=? ORDER BY message.sequence,message.id`, consultationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConsultationMessage, 0)
	for rows.Next() {
		item, internalID, scanErr := scanConsultationMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		var runID uint64
		if item.RunID != "" {
			_ = r.db.QueryRowContext(ctx, `SELECT id FROM agent_runs WHERE public_id=?`, item.RunID).Scan(&runID)
		}
		item.Evidence, scanErr = r.evidenceCitations(ctx, runID, internalID)
		if scanErr != nil {
			return nil, scanErr
		}
		item.Guidance, scanErr = r.guidanceCitations(ctx, runID, internalID)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanConsultationMessage(scanner interface{ Scan(...any) error }) (ConsultationMessage, uint64, error) {
	var item ConsultationMessage
	var internalID uint64
	var completed sql.NullTime
	if err := scanner.Scan(&internalID, &item.ID, &item.ConsultationID, &item.RunID, &item.ContextSnapshotID,
		&item.Sequence, &item.Role, &item.Content, &item.Status, &item.CreatedAt, &completed); err != nil {
		return ConsultationMessage{}, 0, err
	}
	item.CompletedAt = workspaceOptionalTime(completed)
	item.CreatedAt = item.CreatedAt.UTC()
	item.Evidence = []EvidenceCitation{}
	item.Guidance = []GuidanceCitation{}
	return item, internalID, nil
}

func insertWorkspaceEvent(ctx context.Context, tx *sql.Tx, runID uint64, consultationID *uint64, sequence int, eventType string, payload any, at time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > 16384 {
		return ErrInvalidArgument
	}
	var consultation any
	if consultationID != nil {
		consultation = *consultationID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_stream_events
(public_id,agent_run_id,consultation_id,sequence,event_type,payload_json,created_at)
VALUES (?,?,?,?,?,?,?)`, uuid.NewString(), runID, consultation, sequence, eventType, encoded, at.UTC())
	return err
}

func workspaceKubernetesResourceID(clusterID, kind, namespace, name string) string {
	apiVersion := "v1"
	if kind == "Deployment" || kind == "StatefulSet" || kind == "DaemonSet" || kind == "ReplicaSet" {
		apiVersion = "apps/v1"
	}
	identity := strings.Join([]string{clusterID, apiVersion, kind, namespace, name}, "\x00")
	return "k8s_" + base64.RawURLEncoding.EncodeToString([]byte(identity))
}

func workspaceSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func workspaceOptionalTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func nullableUint64(value sql.NullInt64) *uint64 {
	if !value.Valid || value.Int64 <= 0 {
		return nil
	}
	result := uint64(value.Int64)
	return &result
}

func maxWorkspaceInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func workspaceRollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

// Compile-time imports used by the projection types stay explicit here so
// repository call sites cannot accidentally replace Configuration Revision
// and Context Snapshot contracts with untyped maps.
var _ = settings.OperationalScope{}
