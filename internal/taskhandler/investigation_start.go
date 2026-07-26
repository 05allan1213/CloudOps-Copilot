package taskhandler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
)

const (
	// DefaultAgentRunBudget and HardAgentRunBudget retain the public taskhandler
	// names while sharing the frozen budget contract with every child kind.
	DefaultAgentRunBudget     = businessbudget.DefaultLimit
	HardAgentRunBudget        = businessbudget.HardLimit
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

func investigationStart(identity agent.RunModelIdentity) Operation {
	identityErr := identity.Validate()
	return func(_ context.Context, execution asyncjob.Execution) asyncjob.Result {
		task := execution.Task
		if dispatchKey(task) != investigationStartKey || task.SubjectID != task.IncidentID ||
			task.CycleNo == 0 || task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != 1 {
			return asyncjob.Dead("invalid_task_subject", "investigation.start task identity is invalid", nil)
		}
		if _, err := decodeInvestigationStartPayload(task); err != nil {
			return asyncjob.Dead("invalid_task_payload", err.Error(), nil)
		}
		if identityErr != nil {
			return asyncjob.Dead("invalid_agent_run_identity", identityErr.Error(), nil)
		}
		return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
			return startInvestigation(ctx, tx, task, identity)
		})
	}
}

func startInvestigation(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, identity agent.RunModelIdentity) error {
	if err := identity.Validate(); err != nil {
		return fmt.Errorf("%w: %v", asyncjob.ErrInvalidMutation, err)
	}
	startPayload, err := decodeInvestigationStartPayload(task)
	if err != nil {
		return fmt.Errorf("%w: %v", asyncjob.ErrInvalidMutation, err)
	}
	var cycle uint64
	var status string
	var version uint64
	if err := tx.QueryRowContext(ctx, `
SELECT cycle_no, status, version
FROM incidents
WHERE id = ?
FOR UPDATE`, task.IncidentID).Scan(&cycle, &status, &version); err != nil {
		return fmt.Errorf("load investigation.start incident: %w", err)
	}
	if cycle != uint64(task.CycleNo) || version != task.ExpectedSubjectVersion {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if status != "detected" && status != "investigating" {
		return fmt.Errorf("%w: incident status %q cannot start investigation", asyncjob.ErrInvalidMutation, status)
	}
	budget, err := businessbudget.GuardAgentRun(ctx, tx, task.IncidentID, task.CycleNo, startPayload.AuthorizationPublicID)
	if err != nil {
		return fmt.Errorf("%w: investigation.start authorization rejected: %v", asyncjob.ErrPolicyViolation, err)
	}
	if !budget.Allowed() {
		return businessbudget.MarkExhausted(ctx, tx, budget, task.IncidentID, task.CycleNo, "investigation.start")
	}

	runPublicID := uuid.NewString()
	runKey := hashCanonical("agent-run", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo), fmt.Sprint(task.ExpectedSubjectVersion))
	var authorizationValue any
	if budget.AuthorizationID != 0 {
		authorizationValue = budget.AuthorizationID
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO agent_runs
    (public_id, incident_id, idempotency_key, status, objective, model, model_provider, actual_model,
     prompt_version, prompt_hash, tool_schema_version, tool_schema_hash,
     max_steps, max_tool_calls, max_model_calls, token_budget, max_evidence_items,
     max_runtime_ms, tool_timeout_ms, max_evidence_bytes, max_checkpoint_bytes,
     max_step_retries, failure_code, row_version,
	     cycle_no, expected_incident_version, business_budget_authorization_id, migrated_legacy_context, deadline_at, created_at, updated_at)
VALUES (?, ?, ?, 'pending', 'Investigate the current Incident using bounded read-only evidence.',
	        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 1,
	        ?, ?, ?, ?, TIMESTAMPADD(MICROSECOND, ?, NOW(6)), NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		runPublicID, task.IncidentID, runKey,
		identity.ActualModel, identity.Provider, identity.ActualModel, identity.PromptVersion, identity.PromptHash,
		identity.ToolSchemaVersion, identity.ToolSchemaHash,
		defaultSemanticIterations, defaultToolCalls, defaultModelCalls, defaultModelTokens,
		defaultEvidenceItems, defaultRuntimeMillis, defaultToolTimeoutMillis,
		defaultEvidenceBytes, defaultCheckpointBytes, defaultStepRetries,
		task.CycleNo, task.ExpectedSubjectVersion+1, authorizationValue, task.MigratedLegacyContext,
		int64(defaultRuntimeMillis)*1000)
	if err != nil {
		return fmt.Errorf("create investigation AgentRun: %w", err)
	}
	runID, err := result.LastInsertId()
	if err != nil || runID <= 0 {
		return fmt.Errorf("read investigation AgentRun id: %w", err)
	}

	var runIncidentID, runCycle, runExpectedVersion uint64
	var runKeyFound, runStatus string
	var runIdentity agent.RunModelIdentity
	var runAuthorization sql.NullInt64
	var runMigratedLegacyContext bool
	if err := tx.QueryRowContext(ctx, `
SELECT public_id, incident_id, COALESCE(cycle_no, 0), COALESCE(expected_incident_version, 0),
	       COALESCE(idempotency_key, ''), status, business_budget_authorization_id,
       COALESCE(model_provider, ''), COALESCE(actual_model, ''), COALESCE(prompt_version, ''),
	       COALESCE(prompt_hash, ''), COALESCE(tool_schema_version, ''), COALESCE(tool_schema_hash, ''),
	       migrated_legacy_context
FROM agent_runs WHERE id = ? FOR UPDATE`, runID).
		Scan(&runPublicID, &runIncidentID, &runCycle, &runExpectedVersion, &runKeyFound, &runStatus, &runAuthorization,
			&runIdentity.Provider, &runIdentity.ActualModel, &runIdentity.PromptVersion, &runIdentity.PromptHash,
			&runIdentity.ToolSchemaVersion, &runIdentity.ToolSchemaHash, &runMigratedLegacyContext); err != nil {
		return fmt.Errorf("load investigation AgentRun identity: %w", err)
	}
	if runIncidentID != task.IncidentID || runCycle != uint64(task.CycleNo) ||
		runExpectedVersion != task.ExpectedSubjectVersion+1 || runKeyFound != runKey || runStatus != "pending" ||
		runIdentity != identity || runMigratedLegacyContext != task.MigratedLegacyContext {
		return fmt.Errorf("%w: active AgentRun does not match investigation.start", asyncjob.ErrInvalidMutation)
	}
	if (budget.AuthorizationID == 0 && runAuthorization.Valid) ||
		(budget.AuthorizationID != 0 && (!runAuthorization.Valid || uint64(runAuthorization.Int64) != budget.AuthorizationID)) {
		return fmt.Errorf("%w: AgentRun authorization lineage does not match investigation.start", asyncjob.ErrInvalidMutation)
	}

	updated, err := tx.ExecContext(ctx, `
UPDATE incidents
SET status = 'investigating', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND version = ?
  AND status IN ('detected','investigating')`,
		task.IncidentID, task.CycleNo, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("advance investigation Incident: %w", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}

	eventValues := map[string]any{"agent_run_id": runPublicID, "cycle_no": task.CycleNo}
	if budget.AuthorizationPublicID != "" {
		eventValues["business_budget_authorization_id"] = budget.AuthorizationPublicID
		eventValues["authorization_slot"] = budget.AuthorizationSlot
	}
	eventMetadata, err := json.Marshal(eventValues)
	if err != nil {
		return fmt.Errorf("encode AgentRun event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
    (public_id, incident_id, cycle_no, event_schema_version,
     event_type, idempotency_key, migrated_legacy_context, actor_type, actor_id, summary, metadata_json,
     occurred_at, created_at)
VALUES (?, ?, ?, 1, 'agent_run_created', ?, ?, 'system', 'async-task-handler',
        'investigation AgentRun created', ?, NOW(6), NOW(6))`,
		uuid.NewString(), task.IncidentID, task.CycleNo,
		hashCanonical("event", runPublicID, "agent_run_created"), task.MigratedLegacyContext, eventMetadata); err != nil {
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
     dedupe_key, replay_generation, migrated_legacy, migrated_legacy_context, status, priority, available_at, attempt,
     max_attempts, lease_generation, created_at, updated_at)
VALUES (?, ?, ?, 'investigate', 'investigation.advance', 'agent_run', ?,
        'investigation.step', 1, 1, ?, ?, 0, FALSE, ?, 'ready', 50, NOW(6), 0, ?, 0, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		uuid.NewString(), task.IncidentID, task.CycleNo, runID, payload, dedupe, task.MigratedLegacyContext, defaultStepMaxAttempts); err != nil {
		return fmt.Errorf("enqueue investigation.step: %w", err)
	}

	var stepIncidentID, stepSubjectID, stepExpectedVersion uint64
	var stepTaskType, stepSubjectType, stepTransition, stepStatus string
	var stepMigratedLegacy, stepMigratedLegacyContext bool
	if err := tx.QueryRowContext(ctx, `
SELECT incident_id, task_type, subject_type, subject_id, transition,
	       expected_subject_version, migrated_legacy, migrated_legacy_context, status
FROM async_tasks
WHERE dedupe_key = ? AND replay_generation = 0
FOR UPDATE`, dedupe).Scan(
		&stepIncidentID, &stepTaskType, &stepSubjectType, &stepSubjectID,
		&stepTransition, &stepExpectedVersion, &stepMigratedLegacy, &stepMigratedLegacyContext, &stepStatus,
	); err != nil {
		return fmt.Errorf("load investigation.step identity: %w", err)
	}
	if stepIncidentID != task.IncidentID || stepTaskType != string(asyncjob.TaskInvestigationAdvance) ||
		stepSubjectType != "agent_run" || stepSubjectID != uint64(runID) ||
		stepTransition != "investigation.step" || stepExpectedVersion != 1 || stepMigratedLegacy ||
		stepMigratedLegacyContext != task.MigratedLegacyContext || stepStatus != "ready" {
		return fmt.Errorf("%w: existing investigation.step task has a different identity", asyncjob.ErrInvalidMutation)
	}
	return nil
}

type investigationStartPayload struct {
	Mode                  string `json:"mode"`
	IncidentID            string `json:"incident_id,omitempty"`
	IncidentPublicID      string `json:"incident_public_id,omitempty"`
	CycleNo               uint32 `json:"cycle_no"`
	AuthorizationPublicID string `json:"business_budget_authorization_id,omitempty"`
	MigratedLegacyContext bool   `json:"migrated_legacy_context,omitempty"`
}

func decodeInvestigationStartPayload(task asyncjob.Task) (investigationStartPayload, error) {
	if len(task.Payload) == 0 || len(task.Payload) > 8192 {
		return investigationStartPayload{}, errors.New("investigation.start payload is empty or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(string(task.Payload)))
	decoder.DisallowUnknownFields()
	var payload investigationStartPayload
	if err := decoder.Decode(&payload); err != nil {
		return investigationStartPayload{}, errors.New("investigation.start payload is malformed")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return investigationStartPayload{}, errors.New("investigation.start payload has multiple JSON values")
	}
	if payload.Mode != "start" || payload.CycleNo != 0 && payload.CycleNo != task.CycleNo ||
		len(payload.IncidentID) > 64 || len(payload.IncidentPublicID) > 64 ||
		payload.AuthorizationPublicID != "" && len(payload.AuthorizationPublicID) > 64 ||
		payload.MigratedLegacyContext != task.MigratedLegacyContext {
		return investigationStartPayload{}, errors.New("investigation.start payload identity is invalid")
	}
	payload.AuthorizationPublicID = strings.TrimSpace(payload.AuthorizationPublicID)
	return payload, nil
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
