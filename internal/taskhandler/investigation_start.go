package taskhandler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

const (
	defaultSemanticIterations = 8
	defaultToolCalls          = 8
	defaultModelCalls         = 10
	defaultModelTokens        = 16_000
	defaultEvidenceItems      = 20
	defaultRuntimeMillis      = 180_000
	defaultToolTimeoutMillis  = 40_000
	defaultEvidenceBytes      = 16 * 1024
	defaultCheckpointBytes    = 64 * 1024
	defaultStepRetries        = 1
	defaultStepMaxAttempts    = 5
)

func investigationStart(_ context.Context, execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	if dispatchKey(task) != investigationStartKey || task.SubjectID != task.IncidentID ||
		task.CycleNo == 0 || task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != 1 {
		return asyncjob.Dead("invalid_task_subject", "investigation.start task identity is invalid", nil)
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return startInvestigation(ctx, tx, task)
	})
}

func startInvestigation(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task) error {
	var cycle uint64
	var status string
	var version uint64
	if err := tx.QueryRowContext(ctx, `
SELECT cycle_no, v3_status, version
FROM incidents
WHERE id = ? AND domain_schema_version = 3
FOR UPDATE`, task.IncidentID).Scan(&cycle, &status, &version); err != nil {
		return fmt.Errorf("load investigation.start incident: %w", err)
	}
	if cycle != uint64(task.CycleNo) || version != task.ExpectedSubjectVersion {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if status != "detected" && status != "investigating" {
		return fmt.Errorf("%w: incident status %q cannot start investigation", asyncjob.ErrInvalidMutation, status)
	}

	runPublicID := uuid.NewString()
	runKey := hashCanonical("agent-run", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo), fmt.Sprint(task.ExpectedSubjectVersion))
	result, err := tx.ExecContext(ctx, `
INSERT INTO agent_runs
    (public_id, incident_id, idempotency_key, status, objective, model, prompt_version,
     max_steps, max_tool_calls, max_model_calls, token_budget, max_evidence_items,
     max_runtime_ms, tool_timeout_ms, max_evidence_bytes, max_checkpoint_bytes,
     max_step_retries, failure_code, row_version, domain_schema_version, v3_status,
     cycle_no, expected_incident_version, deadline_at, created_at, updated_at)
VALUES (?, ?, ?, 'PENDING', 'Investigate the current Incident using bounded read-only evidence.',
        'configured', 'incident-agent-v3', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 1, 3,
        'pending', ?, ?, TIMESTAMPADD(MICROSECOND, ?, NOW(6)), NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		runPublicID, task.IncidentID, runKey,
		defaultSemanticIterations, defaultToolCalls, defaultModelCalls, defaultModelTokens,
		defaultEvidenceItems, defaultRuntimeMillis, defaultToolTimeoutMillis,
		defaultEvidenceBytes, defaultCheckpointBytes, defaultStepRetries,
		task.CycleNo, task.ExpectedSubjectVersion+1, int64(defaultRuntimeMillis)*1000)
	if err != nil {
		return fmt.Errorf("create investigation AgentRun: %w", err)
	}
	runID, err := result.LastInsertId()
	if err != nil || runID <= 0 {
		return fmt.Errorf("read investigation AgentRun id: %w", err)
	}

	var runIncidentID, runCycle, runExpectedVersion uint64
	var runKeyFound, runStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT public_id, incident_id, COALESCE(cycle_no, 0), COALESCE(expected_incident_version, 0),
       COALESCE(idempotency_key, ''), COALESCE(v3_status, '')
FROM agent_runs WHERE id = ? FOR UPDATE`, runID).
		Scan(&runPublicID, &runIncidentID, &runCycle, &runExpectedVersion, &runKeyFound, &runStatus); err != nil {
		return fmt.Errorf("load investigation AgentRun identity: %w", err)
	}
	if runIncidentID != task.IncidentID || runCycle != uint64(task.CycleNo) ||
		runExpectedVersion != task.ExpectedSubjectVersion+1 || runKeyFound != runKey || runStatus != "pending" {
		return fmt.Errorf("%w: active AgentRun does not match investigation.start", asyncjob.ErrInvalidMutation)
	}

	updated, err := tx.ExecContext(ctx, `
UPDATE incidents
SET v3_status = 'investigating', current_agent_run_id = ?, version = version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ?
  AND v3_status IN ('detected','investigating')`,
		runID, task.IncidentID, task.CycleNo, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("advance investigation Incident: %w", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}

	eventMetadata, err := json.Marshal(map[string]any{"agent_run_id": runPublicID, "cycle_no": task.CycleNo})
	if err != nil {
		return fmt.Errorf("encode AgentRun event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
    (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
     event_type, idempotency_key, actor_type, actor_id, summary, metadata_json,
     occurred_at, created_at)
VALUES (?, ?, 3, ?, 1, 'agent_run_created', ?, 'system', 'async-task-handler',
        'investigation AgentRun created', ?, NOW(6), NOW(6))`,
		uuid.NewString(), task.IncidentID, task.CycleNo,
		hashCanonical("event", runPublicID, "agent_run_created"), eventMetadata); err != nil {
		return fmt.Errorf("append AgentRun event: %w", err)
	}

	payload, err := json.Marshal(map[string]any{"mode": "step", "agent_run_id": runPublicID, "cycle_no": task.CycleNo})
	if err != nil {
		return fmt.Errorf("encode investigation.step payload: %w", err)
	}
	dedupe := hashCanonical("task", runPublicID, "investigation.step", "1")
	if _, err := tx.ExecContext(ctx, `
INSERT INTO async_tasks
    (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
     transition, expected_subject_version, payload_schema_version, payload_json,
     dedupe_key, replay_generation, status, priority, available_at, attempt,
     max_attempts, lease_generation, created_at, updated_at)
VALUES (?, ?, ?, 'investigate', 'investigation.advance', 'agent_run', ?,
        'investigation.step', 1, 1, ?, ?, 0, 'ready', 50, NOW(6), 0, ?, 0, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		uuid.NewString(), task.IncidentID, task.CycleNo, runID, payload, dedupe, defaultStepMaxAttempts); err != nil {
		return fmt.Errorf("enqueue investigation.step: %w", err)
	}

	var stepIncidentID, stepSubjectID, stepExpectedVersion uint64
	var stepTaskType, stepSubjectType, stepTransition, stepStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT incident_id, task_type, subject_type, subject_id, transition,
       expected_subject_version, status
FROM async_tasks
WHERE dedupe_key = ? AND replay_generation = 0
FOR UPDATE`, dedupe).Scan(
		&stepIncidentID, &stepTaskType, &stepSubjectType, &stepSubjectID,
		&stepTransition, &stepExpectedVersion, &stepStatus,
	); err != nil {
		return fmt.Errorf("load investigation.step identity: %w", err)
	}
	if stepIncidentID != task.IncidentID || stepTaskType != string(asyncjob.TaskInvestigationAdvance) ||
		stepSubjectType != "agent_run" || stepSubjectID != uint64(runID) ||
		stepTransition != "investigation.step" || stepExpectedVersion != 1 || stepStatus != "ready" {
		return fmt.Errorf("%w: existing investigation.step task has a different identity", asyncjob.ErrInvalidMutation)
	}
	return nil
}

func hashCanonical(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(strings.TrimSpace(part)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
