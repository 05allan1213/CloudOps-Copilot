package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type workspaceStepLease struct {
	InternalID    uint64
	PublicID      string
	Sequence      int
	ArgumentsHash string
	StartedAt     time.Time
}

func enqueueWorkspaceTask(ctx context.Context, tx *sql.Tx, runID, revisionID uint64, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_workspace_tasks
(public_id,agent_run_id,configuration_revision_id,task_type,status,priority,available_at,max_attempts,created_at,updated_at)
VALUES (?,?,?,'workspace.run','ready',0,?,2,?,?)`, uuid.NewString(), runID, revisionID, now, now, now)
	if err != nil {
		return fmt.Errorf("enqueue Agent Workspace task: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) WorkspaceTaskReady(ctx context.Context) error {
	var version int64
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		return fmt.Errorf("read Agent Workspace schema version: %w", err)
	}
	if version != 9 {
		return fmt.Errorf("unsupported Agent Workspace schema version %d, want 9", version)
	}
	var tables int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema=DATABASE() AND table_name IN ('agent_workspace_tasks','agent_workspace_task_attempts')`).Scan(&tables); err != nil {
		return err
	}
	if tables != 2 {
		return ErrUnavailable
	}
	return nil
}

func (r *WorkspaceRepository) ClaimWorkspaceTask(ctx context.Context, owner string, leaseDuration time.Duration) (WorkspaceLease, bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 128 || leaseDuration <= 0 {
		return WorkspaceLease{}, false, ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return WorkspaceLease{}, false, err
	}
	defer workspaceRollback(tx)
	now := r.now().UTC()
	if err = reapExhaustedWorkspaceTask(ctx, tx, now); err != nil {
		return WorkspaceLease{}, false, err
	}

	record, claimKind, err := selectWorkspaceClaimCandidate(ctx, tx, now)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.Commit(); err != nil {
			return WorkspaceLease{}, false, err
		}
		return WorkspaceLease{}, false, nil
	}
	if err != nil {
		return WorkspaceLease{}, false, err
	}
	if claimKind == "takeover" {
		if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_task_attempts SET status='lease_expired',finished_at=?,
error_code='LEASE_EXPIRED',error_summary='previous Workspace lease expired before completion'
WHERE task_id=? AND attempt=? AND status='running'`, now, record.lease.TaskID, record.lease.Attempt); err != nil {
			return WorkspaceLease{}, false, err
		}
	}
	nextAttempt := record.lease.Attempt + 1
	nextGeneration := record.lease.Generation + 1
	expiresAt := now.Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `UPDATE agent_workspace_tasks SET status='running',attempt=?,lease_owner=?,
lease_generation=?,lease_expires_at=?,heartbeat_at=?,started_at=COALESCE(started_at,?),updated_at=?
WHERE id=? AND attempt=? AND lease_generation=? AND status=?`, nextAttempt, owner, nextGeneration, expiresAt,
		now, now, now, record.lease.TaskID, record.lease.Attempt, record.lease.Generation, record.taskStatus)
	if err != nil {
		return WorkspaceLease{}, false, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return WorkspaceLease{}, false, ErrLeaseLost
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_workspace_task_attempts
(public_id,task_id,configuration_revision_id,attempt,lease_owner,lease_generation,claim_kind,status,started_at,created_at)
VALUES (?,?,?,?,?,?,?,'running',?,?)`, uuid.NewString(), record.lease.TaskID, record.revisionInternalID,
		nextAttempt, owner, nextGeneration, claimKind, now, now); err != nil {
		return WorkspaceLease{}, false, err
	}
	runResult, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status='running',started_at=COALESCE(started_at,?),updated_at=?,row_version=row_version+1
WHERE id=? AND run_kind='workspace' AND status='pending'`, now, now, record.lease.RunID)
	if err != nil {
		return WorkspaceLease{}, false, err
	}
	if rows, _ := runResult.RowsAffected(); rows == 1 {
		if err = appendWorkspaceEvent(ctx, tx, record.lease.RunID, "run.started", map[string]any{"run_id": record.lease.RunPublicID}, now); err != nil {
			return WorkspaceLease{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return WorkspaceLease{}, false, err
	}
	record.lease.Owner = owner
	record.lease.Attempt = nextAttempt
	record.lease.Generation = nextGeneration
	return record.lease, true, nil
}

type workspaceClaimRecord struct {
	lease              WorkspaceLease
	revisionInternalID uint64
	taskStatus         string
}

func selectWorkspaceClaimCandidate(ctx context.Context, tx *sql.Tx, now time.Time) (workspaceClaimRecord, string, error) {
	var record workspaceClaimRecord
	err := tx.QueryRowContext(ctx, `SELECT task.id,task.public_id,task.agent_run_id,run.public_id,
task.configuration_revision_id,revision.public_id,task.attempt,task.max_attempts,task.lease_generation,task.status
FROM agent_workspace_tasks task JOIN agent_runs run ON run.id=task.agent_run_id
JOIN configuration_revisions revision ON revision.id=task.configuration_revision_id
WHERE task.status='running' AND task.lease_expires_at<=? AND task.attempt<task.max_attempts
ORDER BY task.lease_expires_at,task.id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(
		&record.lease.TaskID, &record.lease.TaskPublicID, &record.lease.RunID, &record.lease.RunPublicID,
		&record.revisionInternalID, &record.lease.ConfigurationRevisionID, &record.lease.Attempt, &record.lease.MaxAttempts,
		&record.lease.Generation, &record.taskStatus)
	if err == nil {
		return record, "takeover", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return workspaceClaimRecord{}, "", err
	}
	err = tx.QueryRowContext(ctx, `SELECT task.id,task.public_id,task.agent_run_id,run.public_id,
task.configuration_revision_id,revision.public_id,task.attempt,task.max_attempts,task.lease_generation,task.status
FROM agent_workspace_tasks task JOIN agent_runs run ON run.id=task.agent_run_id
JOIN configuration_revisions revision ON revision.id=task.configuration_revision_id
WHERE task.status='ready' AND task.available_at<=? AND task.attempt<task.max_attempts
ORDER BY task.priority DESC,task.available_at,task.id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(
		&record.lease.TaskID, &record.lease.TaskPublicID, &record.lease.RunID, &record.lease.RunPublicID,
		&record.revisionInternalID, &record.lease.ConfigurationRevisionID, &record.lease.Attempt, &record.lease.MaxAttempts,
		&record.lease.Generation, &record.taskStatus)
	return record, "ready", err
}

func reapExhaustedWorkspaceTask(ctx context.Context, tx *sql.Tx, now time.Time) error {
	var taskID, runID uint64
	var attempt uint32
	err := tx.QueryRowContext(ctx, `SELECT task.id,task.agent_run_id,task.attempt
FROM agent_workspace_tasks task WHERE task.status='running' AND task.lease_expires_at<=?
AND task.attempt>=task.max_attempts ORDER BY task.lease_expires_at,task.id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).
		Scan(&taskID, &runID, &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_task_attempts SET status='dead',finished_at=?,
error_code='ATTEMPTS_EXHAUSTED',error_summary='Workspace lease expired after the final attempt'
WHERE task_id=? AND attempt=? AND status='running'`, now, taskID, attempt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_tasks SET status='dead',lease_owner=NULL,lease_expires_at=NULL,
dead_at=?,last_error_code='ATTEMPTS_EXHAUSTED',last_error_summary='Workspace lease expired after the final attempt',updated_at=?
WHERE id=?`, now, now, taskID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status='failed',outcome='failed',uncertainty='high',
failure_code='WORKSPACE_ATTEMPTS_EXHAUSTED',failure_summary='Workspace task attempts were exhausted',completed_at=?,updated_at=?,row_version=row_version+1
WHERE id=? AND status IN ('pending','running')`, now, now, runID)
	return err
}

func (r *WorkspaceRepository) HeartbeatWorkspaceTask(ctx context.Context, lease WorkspaceLease, duration time.Duration) error {
	if duration <= 0 || strings.TrimSpace(lease.Owner) == "" {
		return ErrInvalidArgument
	}
	now := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE agent_workspace_tasks SET lease_expires_at=?,heartbeat_at=?,updated_at=?
WHERE id=? AND status='running' AND lease_owner=? AND lease_generation=? AND attempt=? AND lease_expires_at>?`,
		now.Add(duration), now, now, lease.TaskID, lease.Owner, lease.Generation, lease.Attempt, now)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrLeaseLost
	}
	_, err = r.db.ExecContext(ctx, `UPDATE agent_workspace_task_attempts SET last_heartbeat_at=?
WHERE task_id=? AND attempt=? AND lease_owner=? AND lease_generation=? AND status='running'`,
		now, lease.TaskID, lease.Attempt, lease.Owner, lease.Generation)
	return err
}

func (r *WorkspaceRepository) WorkspaceCancellationRequested(ctx context.Context, lease WorkspaceLease) (bool, error) {
	if err := r.guardWorkspaceLease(ctx, r.db, lease); err != nil {
		return false, err
	}
	var requested sql.NullTime
	var status string
	if err := r.db.QueryRowContext(ctx, `SELECT cancel_requested_at,status FROM agent_runs WHERE id=?`, lease.RunID).
		Scan(&requested, &status); err != nil {
		return false, err
	}
	return requested.Valid || status == "cancelled", nil
}

func (r *WorkspaceRepository) CiteWorkspaceSnapshotEvidence(ctx context.Context, lease WorkspaceLease, evidencePublicID string) error {
	evidencePublicID = strings.TrimSpace(evidencePublicID)
	if evidencePublicID == "" {
		return ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	now := r.now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_evidence_citations
(public_id,agent_run_id,message_id,evidence_item_id,citation_use,created_at)
SELECT ?,run.id,NULL,evidence.id,'context',?
FROM agent_runs run JOIN context_snapshots snapshot ON snapshot.id=run.context_snapshot_id
JOIN evidence_items evidence ON evidence.public_id=? AND evidence.valid=1
WHERE run.id=? AND JSON_CONTAINS(snapshot.evidence_refs_json,JSON_QUOTE(?))
AND NOT EXISTS (SELECT 1 FROM agent_evidence_citations citation
WHERE citation.agent_run_id=run.id AND citation.evidence_item_id=evidence.id AND citation.citation_use='context')`,
		uuid.NewString(), now, evidencePublicID, lease.RunID, evidencePublicID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs run
JOIN context_snapshots snapshot ON snapshot.id=run.context_snapshot_id
JOIN evidence_items evidence ON evidence.public_id=? AND evidence.valid=1
WHERE run.id=? AND JSON_CONTAINS(snapshot.evidence_refs_json,JSON_QUOTE(?))`, evidencePublicID, lease.RunID, evidencePublicID).
			Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrNotFound
		}
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) AppendWorkspaceAnswerDelta(ctx context.Context, lease WorkspaceLease, delta string) error {
	if delta == "" || len(delta) > 1024 {
		return ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	var cancelRequested sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT cancel_requested_at FROM agent_runs WHERE id=? FOR UPDATE`, lease.RunID).
		Scan(&cancelRequested); err != nil {
		return err
	}
	if cancelRequested.Valid {
		return ErrCancelled
	}
	if err = appendWorkspaceEvent(ctx, tx, lease.RunID, "answer.delta", map[string]string{"delta": delta}, r.now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) WorkspaceExecution(ctx context.Context, lease WorkspaceLease) (WorkspaceExecutionContext, error) {
	if err := r.guardWorkspaceLease(ctx, r.db, lease); err != nil {
		return WorkspaceExecutionContext{}, err
	}
	run, err := r.WorkspaceRun(ctx, lease.RunPublicID)
	if err != nil {
		return WorkspaceExecutionContext{}, err
	}
	var result WorkspaceExecutionContext
	result.Run = run
	result.Task = WorkspaceTask{
		ID: lease.TaskPublicID, RunID: lease.RunPublicID, ConfigurationRevisionID: lease.ConfigurationRevisionID,
		SubjectType: run.SubjectType, Status: WorkspaceTaskRunning, Attempt: lease.Attempt, MaxAttempts: lease.MaxAttempts,
	}
	var namespaces, resources, definitions, queries, evidence []byte
	var maxRuntimeMS, toolTimeoutMS int64
	err = r.db.QueryRowContext(ctx, `SELECT snapshot.public_id,COALESCE(consultation.public_id,''),COALESCE(owner_message.content,''),
snapshot.subject_type,revision.public_id,snapshot.cluster_id,snapshot.environment,snapshot.namespaces_json,
snapshot.resource_refs_json,snapshot.filters_json,snapshot.range_start,snapshot.range_end,
snapshot.query_definition_refs_json,snapshot.query_execution_refs_json,snapshot.evidence_refs_json,
snapshot.content_hash,snapshot.created_at,run.max_tool_calls,run.max_evidence_items,run.max_runtime_ms,run.tool_timeout_ms,
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(alert.labels_json,'$.alertname')),'')
FROM agent_runs run JOIN context_snapshots snapshot ON snapshot.id=run.context_snapshot_id
JOIN configuration_revisions revision ON revision.id=snapshot.configuration_revision_id
LEFT JOIN agent_consultations consultation ON consultation.id=run.consultation_id
LEFT JOIN agent_consultation_messages owner_message ON owner_message.agent_run_id=run.id AND owner_message.role='owner'
LEFT JOIN alerts alert ON alert.id=run.alert_id WHERE run.id=?`, lease.RunID).Scan(
		&result.Snapshot.ID, &result.Snapshot.ConsultationID, &result.OwnerPrompt, &result.Snapshot.SubjectType,
		&result.Snapshot.ConfigurationRevisionID, &result.Snapshot.Scope.ClusterID, &result.Snapshot.Scope.Environment,
		&namespaces, &resources, &result.Snapshot.Filters, &result.Snapshot.TimeRange.From, &result.Snapshot.TimeRange.To,
		&definitions, &queries, &evidence, &result.Snapshot.ContentHash, &result.Snapshot.CreatedAt,
		&result.Limits.MaxToolCalls, &result.Limits.MaxEvidenceItems, &maxRuntimeMS, &toolTimeoutMS,
		&result.AlertName)
	if err != nil {
		return WorkspaceExecutionContext{}, err
	}
	if json.Unmarshal(namespaces, &result.Snapshot.Scope.Namespaces) != nil || json.Unmarshal(resources, &result.Snapshot.Resources) != nil ||
		json.Unmarshal(definitions, &result.Snapshot.QueryDefinitionIDs) != nil || json.Unmarshal(queries, &result.Snapshot.QueryExecutionIDs) != nil ||
		json.Unmarshal(evidence, &result.Snapshot.EvidenceIDs) != nil {
		return WorkspaceExecutionContext{}, ErrUnavailable
	}
	result.Snapshot.Scope.RevisionID = result.Snapshot.ConfigurationRevisionID
	result.Limits.MaxRuntime = time.Duration(maxRuntimeMS) * time.Millisecond
	result.Limits.ToolTimeout = time.Duration(toolTimeoutMS) * time.Millisecond
	result.Snapshot.RunID = run.ID
	result.Snapshot.TimeRange.From, result.Snapshot.TimeRange.To = result.Snapshot.TimeRange.From.UTC(), result.Snapshot.TimeRange.To.UTC()
	result.Snapshot.CreatedAt = result.Snapshot.CreatedAt.UTC()
	return result, nil
}

func (r *WorkspaceRepository) StartWorkspaceTool(ctx context.Context, lease WorkspaceLease, tool string, arguments json.RawMessage) (workspaceStepLease, error) {
	tool = strings.TrimSpace(tool)
	if tool == "" || len(tool) > 128 || !workspaceJSONObject(arguments) {
		return workspaceStepLease{}, ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return workspaceStepLease{}, err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return workspaceStepLease{}, err
	}
	var cancelRequested sql.NullTime
	var runStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status,cancel_requested_at FROM agent_runs WHERE id=? FOR UPDATE`, lease.RunID).
		Scan(&runStatus, &cancelRequested); err != nil {
		return workspaceStepLease{}, err
	}
	if cancelRequested.Valid || runStatus == "cancelled" {
		return workspaceStepLease{}, ErrCancelled
	}
	now := r.now().UTC()
	if runStatus != "running" {
		return workspaceStepLease{}, ErrConflict
	}
	var sequence int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM agent_steps WHERE agent_run_id=?`, lease.RunID).Scan(&sequence); err != nil {
		return workspaceStepLease{}, err
	}
	step := workspaceStepLease{PublicID: uuid.NewString(), Sequence: sequence, ArgumentsHash: workspaceSHA256(arguments), StartedAt: now}
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_steps
(public_id,agent_run_id,incident_id,cycle_no,sequence,step_type,short_reason,selected_tool,arguments_json,arguments_hash,
result_summary,result_ref,evidence_public_id,status,retry_count,duration_ms,input_tokens,output_tokens,error_code,started_at,created_at)
VALUES (?,?,NULL,NULL,?,'tool','bounded read selected by Workspace policy',?,?,?,'','','','running',0,0,0,0,'',?,?)`,
		step.PublicID, lease.RunID, sequence, tool, arguments, step.ArgumentsHash, now, now)
	if err != nil {
		return workspaceStepLease{}, err
	}
	stepID, err := result.LastInsertId()
	if err != nil {
		return workspaceStepLease{}, err
	}
	step.InternalID = uint64(stepID)
	if err = appendWorkspaceEvent(ctx, tx, lease.RunID, "tool.started", map[string]any{
		"step_id": step.PublicID, "tool": tool, "scope": arguments,
	}, now); err != nil {
		return workspaceStepLease{}, err
	}
	if err = tx.Commit(); err != nil {
		return workspaceStepLease{}, err
	}
	return step, nil
}

func (r *WorkspaceRepository) CompleteWorkspaceTool(ctx context.Context, lease WorkspaceLease, step workspaceStepLease, observation WorkspaceToolObservation) (string, error) {
	if strings.TrimSpace(observation.Tool) == "" || strings.TrimSpace(observation.Source) == "" ||
		strings.TrimSpace(observation.ResourceRef) == "" || strings.TrimSpace(observation.Summary) == "" {
		return "", ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return "", err
	}
	var status, argumentsHash, snapshotHash string
	var timeFrom, timeTo time.Time
	if err = tx.QueryRowContext(ctx, `SELECT step.status,step.arguments_hash,snapshot.content_hash,snapshot.range_start,snapshot.range_end
FROM agent_steps step JOIN agent_runs run ON run.id=step.agent_run_id
JOIN context_snapshots snapshot ON snapshot.id=run.context_snapshot_id
WHERE step.id=? AND step.agent_run_id=? FOR UPDATE`, step.InternalID, lease.RunID).
		Scan(&status, &argumentsHash, &snapshotHash, &timeFrom, &timeTo); err != nil {
		return "", err
	}
	if status != "running" || argumentsHash != step.ArgumentsHash {
		return "", ErrConflict
	}
	facts, err := workspaceFactsEnvelope(observation.Facts, observation.Summary)
	if err != nil {
		return "", err
	}
	provenance := workspaceObjectOrDefault(observation.Provenance, map[string]any{
		"provider": observation.Source, "adapter": "provider-gateway/v1", "tool": observation.Tool,
	})
	trust := workspaceObjectOrDefault(observation.TrustAxes, map[string]any{
		"authority": "runtime_observation", "integrity": "provider_response", "freshness": "captured", "completeness": "bounded",
	})
	now := r.now().UTC()
	if observation.CollectedAt.IsZero() {
		observation.CollectedAt = now
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = observation.CollectedAt
	}
	if observation.ObservedAt.After(observation.CollectedAt) {
		observation.ObservedAt = observation.CollectedAt
	}
	contentMaterial, _ := json.Marshal(map[string]any{
		"facts": json.RawMessage(facts), "source": observation.Source, "tool": observation.Tool,
		"resource": observation.ResourceRef, "observed_at": observation.ObservedAt.UTC(),
	})
	contentHash := workspaceSHA256(contentMaterial)
	provenanceHash := workspaceSHA256(provenance)
	factSchemaHash := workspaceSHA256([]byte("cloudops.agent-workspace/facts/v1"))
	window, _ := json.Marshal(map[string]time.Time{"from": timeFrom.UTC(), "to": timeTo.UTC()})
	groups, _ := json.Marshal([][]string{{observation.Source + "/" + observation.Tool}})
	empty := json.RawMessage(`[]`)
	redaction := json.RawMessage(`{"policy":"agent-workspace-evidence/v1","raw_text":"allowlisted-summary-only"}`)
	redactionCounts := json.RawMessage(`{"secret_fields":0,"raw_payloads_omitted":1}`)
	promptFlags := json.RawMessage(`{"untrusted_content":true,"instruction_text_not_executed":true,"hidden_reasoning_omitted":true}`)
	evidencePublicID := uuid.NewString()
	query := strings.TrimSpace(observation.Query)
	if query == "" {
		query = observation.Tool
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO evidence_items
(public_id,incident_id,evidence_contract_version,cycle_no,agent_run_id,agent_step_id,type,source,
producer_type,producer_id,producer_version,producer_dedupe_key,adapter_version,query_template_id,
query_template_version,scope_snapshot_hash,arguments_hash,tool_name,resource_ref,time_range_json,query_text,
summary,facts_json,fact_schema_version,fact_schema_hash,provenance_json,provenance_hash,trust_axes_json,claim_use,
corroboration_groups_json,input_evidence_ids_json,input_sample_ids_json,input_hashes_json,source_revision,result_hash,
content_hash,raw_ref,safe_raw_reference,redaction_json,redaction_policy_version,redaction_counts_json,prompt_safety_flags_json,
truncated,valid,migrated_legacy,migrated_legacy_context,idempotency_key,collected_at,observed_at,created_at)
VALUES (
?,NULL,1,NULL,?,?,'agent.workspace.observation',?,
'agent_step',?,?,?,'provider-gateway/v1',?,'1',
?,?,?,?,?,?,?,?,1,?,?,?,?,'context',
?,?,?,?,?,?,?,?,?,?,'agent-workspace-evidence/v1',
?,?,?,1,0,0,?,?,?,?)`,
		evidencePublicID, lease.RunID, step.InternalID, observation.Source, step.PublicID, WorkspaceToolVersion,
		contentHash, observation.Tool, snapshotHash, argumentsHash, observation.Tool, observation.ResourceRef, window,
		query, workspaceBound(observation.Summary, 4096), facts, factSchemaHash, provenance, provenanceHash, trust,
		groups, empty, empty, empty, workspaceNullableString(observation.SourceRevision), contentHash, contentHash,
		"agent-workspace-step:"+step.PublicID, "agent-workspace-step:"+step.PublicID, redaction, redactionCounts,
		promptFlags, observation.Truncated || observation.Partial, contentHash, observation.CollectedAt.UTC(),
		observation.ObservedAt.UTC(), now)
	if err != nil {
		return "", fmt.Errorf("persist Agent Workspace Evidence: %w", err)
	}
	evidenceID, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_evidence_citations
(public_id,agent_run_id,message_id,evidence_item_id,citation_use,created_at)
VALUES (?,?,NULL,?,'context',?)`, uuid.NewString(), lease.RunID, evidenceID, now); err != nil {
		return "", err
	}
	duration := now.Sub(step.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_steps SET result_summary=?,result_ref=?,evidence_public_id=?,
status='completed',duration_ms=?,finished_at=? WHERE id=? AND status='running'`, workspaceBound(observation.Summary, 4096),
		"evidence:"+evidencePublicID, evidencePublicID, duration, now, step.InternalID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET used_steps=used_steps+1,used_tool_calls=used_tool_calls+1,
used_evidence_items=used_evidence_items+1,updated_at=?,row_version=row_version+1 WHERE id=?`, now, lease.RunID); err != nil {
		return "", err
	}
	if err = appendWorkspaceEvent(ctx, tx, lease.RunID, "tool.completed", map[string]any{
		"step_id": step.PublicID, "tool": observation.Tool, "evidence_id": evidencePublicID,
		"summary": workspaceBound(observation.Summary, 512), "duration_ms": duration,
	}, now); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return evidencePublicID, nil
}

func (r *WorkspaceRepository) FailWorkspaceTool(ctx context.Context, lease WorkspaceLease, step workspaceStepLease, code, summary string) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	now := r.now().UTC()
	duration := now.Sub(step.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_steps SET status='failed',error_code=?,result_summary=?,duration_ms=?,finished_at=?
WHERE id=? AND agent_run_id=? AND status='running'`, workspaceBound(code, 64), workspaceBound(summary, 4096), duration,
		now, step.InternalID, lease.RunID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET used_steps=used_steps+1,used_tool_calls=used_tool_calls+1,
updated_at=?,row_version=row_version+1 WHERE id=?`, now, lease.RunID); err != nil {
		return err
	}
	if err = appendWorkspaceEvent(ctx, tx, lease.RunID, "tool.failed", map[string]any{
		"step_id": step.PublicID, "error_code": workspaceBound(code, 64), "summary": workspaceBound(summary, 512),
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) CompleteWorkspaceTask(ctx context.Context, lease WorkspaceLease, completion WorkspaceCompletion) error {
	if completion.Outcome == "" || strings.TrimSpace(completion.Uncertainty) == "" || strings.TrimSpace(completion.Answer) == "" {
		return ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	var consultationID, snapshotID sql.NullInt64
	var runStatus string
	var cancelRequested sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT status,consultation_id,context_snapshot_id,cancel_requested_at
FROM agent_runs WHERE id=? FOR UPDATE`, lease.RunID).Scan(&runStatus, &consultationID, &snapshotID, &cancelRequested); err != nil {
		return err
	}
	if runStatus != "pending" && runStatus != "running" {
		return ErrConflict
	}
	now := r.now().UTC()
	if cancelRequested.Valid {
		completion = WorkspaceCompletion{Outcome: WorkspaceOutcomeCancelled, Uncertainty: "unknown", Answer: "本次 Agent 工作已取消。", FailureCode: "OWNER_CANCELLED", FailureSummary: "Owner requested cancellation"}
	}
	terminalStatus, taskStatus, attemptStatus := "completed", "succeeded", "succeeded"
	eventType := "run.completed"
	switch completion.Outcome {
	case WorkspaceOutcomeFailed:
		terminalStatus, taskStatus, attemptStatus, eventType = "failed", "dead", "dead", "run.failed"
	case WorkspaceOutcomeCancelled:
		terminalStatus, taskStatus, attemptStatus, eventType = "cancelled", "cancelled", "cancelled", "run.cancelled"
	}
	var messageID any
	if consultationID.Valid {
		var sequence uint64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM agent_consultation_messages WHERE consultation_id=?`, consultationID.Int64).Scan(&sequence); err != nil {
			return err
		}
		messagePublicID := uuid.NewString()
		result, insertErr := tx.ExecContext(ctx, `INSERT INTO agent_consultation_messages
(public_id,consultation_id,agent_run_id,context_snapshot_id,sequence,role,content,status,created_at,completed_at)
VALUES (?,?,?,?,?,'assistant',?,'completed',?,?)`, messagePublicID, consultationID.Int64, lease.RunID,
			snapshotID.Int64, sequence, workspaceBound(completion.Answer, 16000), now, now)
		if insertErr != nil {
			return insertErr
		}
		messageID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_evidence_citations SET message_id=?
WHERE agent_run_id=? AND message_id IS NULL`, messageID, lease.RunID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_guidance_citations SET message_id=?
WHERE agent_run_id=? AND message_id IS NULL`, messageID, lease.RunID); err != nil {
			return err
		}
		if err = appendWorkspaceEvent(ctx, tx, lease.RunID, "answer.completed", map[string]any{
			"message_id": messagePublicID, "uncertainty": completion.Uncertainty,
		}, now); err != nil {
			return err
		}
	}
	model := "provider-disabled"
	modelProvider, actualModel, promptHash, toolVersion, toolHash := any(nil), any(nil), any(nil), any(nil), any(nil)
	if strings.TrimSpace(completion.ModelProvider) != "" && strings.TrimSpace(completion.ActualModel) != "" {
		model = strings.TrimSpace(completion.ActualModel)
		modelProvider, actualModel = strings.TrimSpace(completion.ModelProvider), model
		promptHash = workspaceSHA256([]byte(WorkspacePromptVersion))
		toolVersion, toolHash = WorkspaceToolVersion, workspaceSHA256([]byte(WorkspaceToolVersion))
	}
	diagnosis, _ := json.Marshal(map[string]string{"answer": workspaceBound(completion.Answer, 16000)})
	if _, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status=?,outcome=?,uncertainty=?,model=?,model_provider=?,actual_model=?,
	final_diagnosis=?,prompt_hash=?,tool_schema_version=?,tool_schema_hash=?,failure_code=?,failure_summary=?,used_model_calls=IF(? IS NULL,0,1),
	input_tokens=?,output_tokens=?,completed_at=?,updated_at=?,row_version=row_version+1
	WHERE id=? AND status IN ('pending','running')`, terminalStatus, completion.Outcome, completion.Uncertainty, model,
		modelProvider, actualModel, diagnosis, promptHash, toolVersion, toolHash, workspaceBound(completion.FailureCode, 128),
		workspaceBound(completion.FailureSummary, 2048), modelProvider, completion.InputTokens, completion.OutputTokens,
		now, now, lease.RunID); err != nil {
		return err
	}
	terminalColumn := "completed_at"
	switch taskStatus {
	case "dead":
		terminalColumn = "dead_at"
	case "cancelled":
		terminalColumn = "cancelled_at"
	}
	query := `UPDATE agent_workspace_tasks SET status=?,lease_owner=NULL,lease_expires_at=NULL,` + terminalColumn + `=?,
last_error_code=?,last_error_summary=?,updated_at=? WHERE id=? AND status='running' AND lease_owner=? AND lease_generation=? AND attempt=?`
	result, err := tx.ExecContext(ctx, query, taskStatus, now, workspaceNullableString(completion.FailureCode),
		workspaceNullableString(completion.FailureSummary), now, lease.TaskID, lease.Owner, lease.Generation, lease.Attempt)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrLeaseLost
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_task_attempts SET status=?,finished_at=?,error_code=?,error_summary=?
WHERE task_id=? AND attempt=? AND lease_owner=? AND lease_generation=? AND status='running'`, attemptStatus, now,
		workspaceNullableString(completion.FailureCode), workspaceNullableString(completion.FailureSummary), lease.TaskID,
		lease.Attempt, lease.Owner, lease.Generation); err != nil {
		return err
	}
	if err = appendWorkspaceEvent(ctx, tx, lease.RunID, eventType, map[string]any{
		"outcome": completion.Outcome, "uncertainty": completion.Uncertainty, "failure_code": completion.FailureCode,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) RetryWorkspaceTask(ctx context.Context, lease WorkspaceLease, code, summary string, delay time.Duration) error {
	if delay < 0 {
		delay = 0
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	now := r.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE agent_workspace_tasks SET status='ready',lease_owner=NULL,lease_expires_at=NULL,
available_at=?,last_error_code=?,last_error_summary=?,updated_at=?
WHERE id=? AND status='running' AND lease_owner=? AND lease_generation=? AND attempt=? AND attempt<max_attempts`,
		now.Add(delay), workspaceBound(code, 64), workspaceBound(summary, 2048), now, lease.TaskID, lease.Owner,
		lease.Generation, lease.Attempt)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrLeaseLost
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_task_attempts SET status='retry',finished_at=?,error_code=?,error_summary=?
WHERE task_id=? AND attempt=? AND lease_owner=? AND lease_generation=? AND status='running'`, now,
		workspaceBound(code, 64), workspaceBound(summary, 2048), lease.TaskID, lease.Attempt, lease.Owner, lease.Generation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) guardWorkspaceLease(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, lease WorkspaceLease) error {
	var found uint64
	err := query.QueryRowContext(ctx, `SELECT id FROM agent_workspace_tasks WHERE id=? AND agent_run_id=? AND status='running'
AND lease_owner=? AND lease_generation=? AND attempt=? AND lease_expires_at>?`, lease.TaskID, lease.RunID, lease.Owner,
		lease.Generation, lease.Attempt, r.now().UTC()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	return err
}

func appendWorkspaceEvent(ctx context.Context, tx *sql.Tx, runID uint64, eventType string, payload any, at time.Time) error {
	var consultationID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT consultation_id FROM agent_runs WHERE id=?`, runID).Scan(&consultationID); err != nil {
		return err
	}
	var sequence int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM agent_stream_events WHERE agent_run_id=?`, runID).Scan(&sequence); err != nil {
		return err
	}
	return insertWorkspaceEvent(ctx, tx, runID, nullableUint64(consultationID), sequence, eventType, payload, at)
}

func workspaceFactsEnvelope(raw json.RawMessage, summary string) (json.RawMessage, error) {
	var facts []any
	if len(raw) > 0 && json.Unmarshal(raw, &facts) != nil {
		return nil, ErrInvalidArgument
	}
	if len(facts) == 0 {
		facts = []any{map[string]any{"kind": "bounded_observation", "summary": workspaceBound(summary, 1024)}}
	}
	if len(facts) > 64 {
		facts = facts[:64]
	}
	encoded, err := json.Marshal(map[string]any{"facts": facts})
	if err != nil || len(encoded) > 16384 {
		return nil, ErrInvalidArgument
	}
	return encoded, nil
}

func workspaceObjectOrDefault(raw json.RawMessage, fallback map[string]any) json.RawMessage {
	if workspaceJSONObject(raw) {
		return append(json.RawMessage(nil), raw...)
	}
	encoded, _ := json.Marshal(fallback)
	return encoded
}

func workspaceJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && value != nil
}

func workspaceBound(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func workspaceNullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
