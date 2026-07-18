// Package taskhandler maps the five frozen task types to bounded application
// handlers. It never claims work; asyncjob.Runner is the sole claim owner.
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

type Operation func(context.Context, asyncjob.Execution) asyncjob.Result

// Config injects the one-step application operations owned by later feature
// phases. Phase 2 provides investigation.start itself because it is part of
// Incident/Task convergence.
type Config struct {
	InvestigationStep   Operation
	RemediationPrepare  Operation
	ChangeEnsurePR      Operation
	DeliveryObserve     Operation
	VerificationAdvance Operation
}

// NewRuntime returns the production registry only when every subject-bound
// one-step operation is explicitly provided. Missing operations are a startup
// error; the worker must not claim a task merely to dead-letter it as disabled.
func NewRuntime(config Config) (map[asyncjob.TaskType]asyncjob.Handler, error) {
	missing := make([]string, 0, 5)
	if config.InvestigationStep == nil {
		missing = append(missing, "investigation.step")
	}
	if config.RemediationPrepare == nil {
		missing = append(missing, "remediation.prepare")
	}
	if config.ChangeEnsurePR == nil {
		missing = append(missing, "change.ensure_pr")
	}
	if config.DeliveryObserve == nil {
		missing = append(missing, "delivery.observe")
	}
	if config.VerificationAdvance == nil {
		missing = append(missing, "verification.advance")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("async task operations are not migrated: %s", strings.Join(missing, ", "))
	}
	return New(config), nil
}

// New is the controlled registry used by Incident start integration tests.
// Production code must use NewRuntime. A missing operation returns an invalid
// result, which the Runner deliberately leaves running for lease takeover.
func New(config Config) map[asyncjob.TaskType]asyncjob.Handler {
	notMigrated := func(context.Context, asyncjob.Execution) asyncjob.Result {
		return asyncjob.Result{}
	}
	if config.InvestigationStep == nil {
		config.InvestigationStep = notMigrated
	}
	if config.RemediationPrepare == nil {
		config.RemediationPrepare = notMigrated
	}
	if config.ChangeEnsurePR == nil {
		config.ChangeEnsurePR = notMigrated
	}
	if config.DeliveryObserve == nil {
		config.DeliveryObserve = notMigrated
	}
	if config.VerificationAdvance == nil {
		config.VerificationAdvance = notMigrated
	}
	return map[asyncjob.TaskType]asyncjob.Handler{
		asyncjob.TaskInvestigationAdvance: asyncjob.HandlerFunc(func(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
			if execution.Task.SubjectType == "incident" && execution.Task.Transition == "investigation.start" {
				return investigationStart(execution)
			}
			return config.InvestigationStep(ctx, execution)
		}),
		asyncjob.TaskRemediationPrepare:  asyncjob.HandlerFunc(config.RemediationPrepare),
		asyncjob.TaskChangeEnsurePR:      asyncjob.HandlerFunc(config.ChangeEnsurePR),
		asyncjob.TaskDeliveryObserve:     asyncjob.HandlerFunc(config.DeliveryObserve),
		asyncjob.TaskVerificationAdvance: asyncjob.HandlerFunc(config.VerificationAdvance),
	}
}

func investigationStart(execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	if task.SubjectID != task.IncidentID || task.CycleNo == 0 || task.ExpectedSubjectVersion == 0 {
		return asyncjob.Dead("invalid_task_subject", "investigation.start subject does not match its incident", nil)
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
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
			return fmt.Errorf("incident status %q cannot start investigation", status)
		}

		runPublicID := uuid.NewString()
		runKey := hashCanonical("agent-run", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo), fmt.Sprint(task.ExpectedSubjectVersion))
		result, err := tx.ExecContext(ctx, `
INSERT INTO agent_runs
    (public_id, incident_id, idempotency_key, status, model, prompt_version, max_steps,
     failure_code, row_version, domain_schema_version, v3_status, cycle_no,
     expected_incident_version, created_at, updated_at)
VALUES (?, ?, ?, 'PENDING', 'phase2-compatibility', 'phase2-task-handler-v1', 1,
        '', 1, 3, 'pending', ?, ?, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
			runPublicID, task.IncidentID, runKey, task.CycleNo, task.ExpectedSubjectVersion+1)
		if err != nil {
			return fmt.Errorf("create investigation AgentRun: %w", err)
		}
		runID, err := result.LastInsertId()
		if err != nil || runID <= 0 {
			return fmt.Errorf("read investigation AgentRun id: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "SELECT public_id FROM agent_runs WHERE id = ?", runID).Scan(&runPublicID); err != nil {
			return fmt.Errorf("load investigation AgentRun public id: %w", err)
		}

		updated, err := tx.ExecContext(ctx, `
UPDATE incidents
SET v3_status = 'investigating', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ?
  AND v3_status IN ('detected','investigating')`,
			task.IncidentID, task.CycleNo, task.ExpectedSubjectVersion)
		if err != nil {
			return fmt.Errorf("advance investigation Incident: %w", err)
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}

		eventMetadata, _ := json.Marshal(map[string]any{"agent_run_id": runPublicID, "cycle_no": task.CycleNo})
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

		payload, _ := json.Marshal(map[string]any{"mode": "step", "agent_run_id": runPublicID, "cycle_no": task.CycleNo})
		dedupe := hashCanonical("task", runPublicID, "investigation.step", "1")
		if _, err := tx.ExecContext(ctx, `
INSERT INTO async_tasks
    (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
     transition, expected_subject_version, payload_schema_version, payload_json,
     dedupe_key, replay_generation, status, priority, available_at, attempt,
     max_attempts, lease_generation, created_at, updated_at)
VALUES (?, ?, ?, 'investigate', 'investigation.advance', 'agent_run', ?,
        'investigation.step', 1, 1, ?, ?, 0, 'ready', 50, NOW(6), 0, 5, 0, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
			uuid.NewString(), task.IncidentID, task.CycleNo, runID, payload, dedupe); err != nil {
			return fmt.Errorf("enqueue investigation.step: %w", err)
		}
		return nil
	})
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
