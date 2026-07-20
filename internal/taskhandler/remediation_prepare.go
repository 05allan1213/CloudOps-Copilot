package taskhandler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

const remediationPreparePayloadSchema = 1

type RemediationPrepareInput struct {
	Request remediation.RestoreEnvCompileRequest
}

type RemediationPrepareLoader interface {
	Load(context.Context, asyncjob.Task) (RemediationPrepareInput, error)
}

type RemediationPrepareStore interface {
	PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, *remediation.RemediationPlan) error
}

type RemediationPrepareConfig struct {
	Loader RemediationPrepareLoader
	Store  RemediationPrepareStore
	Now    func() time.Time
}

func NewRemediationPrepare(config RemediationPrepareConfig) (Operation, error) {
	if config.Loader == nil || config.Store == nil {
		return nil, errors.New("remediation.prepare requires a bounded loader and V3 persistence store")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	operation := &remediationPrepareOperation{cfg: config}
	return operation.handle, nil
}

type remediationPrepareOperation struct {
	cfg RemediationPrepareConfig
}

type remediationPreparePayload struct {
	AgentRunID string `json:"agent_run_id"`
	CycleNo    uint64 `json:"cycle_no"`
}

func (o *remediationPrepareOperation) handle(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	if dispatchKey(task) != remediationPrepareKey || task.SubjectID == 0 || task.CycleNo == 0 ||
		task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != remediationPreparePayloadSchema ||
		execution.Lease.TaskID != task.ID || execution.Lease.ExpectedSubjectVersion != task.ExpectedSubjectVersion {
		return asyncjob.Dead("invalid_task_subject", "remediation.prepare task identity is invalid", nil)
	}
	payload, err := decodeRemediationPreparePayload(task)
	if err != nil {
		return asyncjob.Dead("invalid_remediation_payload", boundChange(err.Error(), 2048), nil)
	}
	if payload.AgentRunID != "" && payload.CycleNo != uint64(task.CycleNo) {
		return asyncjob.Dead("invalid_remediation_payload", "remediation payload cycle does not match its subject", nil)
	}
	input, err := o.cfg.Loader.Load(ctx, task)
	if err != nil {
		return remediationPrepareLoadFailure(err)
	}
	request := input.Request
	if request.IncidentID != task.IncidentID || request.CycleNo != uint64(task.CycleNo) || request.CreatedByAgentRunID == "" {
		return asyncjob.Dead("remediation_input_mismatch", "compiled remediation input is outside the task Incident/cycle", nil)
	}
	plan, err := remediation.CompileRestoreRequiredEnv(request)
	if err != nil {
		return asyncjob.Dead("remediation_compile_rejected", boundChange(err.Error(), 2048), nil)
	}
	if err := remediation.ValidateV3Plan(plan); err != nil {
		return asyncjob.Dead("remediation_plan_invalid", boundChange(err.Error(), 2048), nil)
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.cfg.Store.PersistIn(ctx, tx, task, &plan)
	})
}

func decodeRemediationPreparePayload(task asyncjob.Task) (remediationPreparePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(task.Payload)))
	decoder.DisallowUnknownFields()
	var payload remediationPreparePayload
	if err := decoder.Decode(&payload); err != nil {
		return remediationPreparePayload{}, errors.New("remediation.prepare payload is malformed")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return remediationPreparePayload{}, errors.New("remediation.prepare payload has multiple JSON values")
	}
	return payload, nil
}

func remediationPrepareLoadFailure(err error) asyncjob.Result {
	switch {
	case errors.Is(err, asyncjob.ErrSubjectVersionMismatch), errors.Is(err, asyncjob.ErrPolicyViolation), errors.Is(err, remediation.ErrDrift), errors.Is(err, remediation.ErrInvalidArgument):
		return asyncjob.Dead("remediation_input_rejected", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, sql.ErrNoRows):
		return asyncjob.Dead("remediation_subject_missing", "remediation source facts no longer exist", nil)
	default:
		return asyncjob.RetryAfter(0, "remediation_source_unavailable", boundChange(err.Error(), 2048), nil)
	}
}

// mysqlRemediationPrepareStore keeps the Plan insert and Incident transition
// in the async task's transaction. It deliberately does not enqueue a follow-up
// task: human approval is the next explicit boundary.
type mysqlRemediationPrepareStore struct {
	repository remediation.V3Repository
}

func NewMySQLRemediationPrepareStore(repository remediation.V3Repository) (RemediationPrepareStore, error) {
	if repository == nil {
		return nil, errors.New("V3 remediation repository is required")
	}
	return &mysqlRemediationPrepareStore{repository: repository}, nil
}

func (s *mysqlRemediationPrepareStore) PersistIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, plan *remediation.RemediationPlan) error {
	if plan == nil || plan.IncidentID != task.IncidentID || plan.CycleNo != uint64(task.CycleNo) {
		return asyncjob.ErrInvalidMutation
	}
	if err := s.repository.CreatePlanIn(ctx, tx, plan); err != nil {
		return err
	}
	var version uint64
	var status string
	if err := tx.QueryRowContext(ctx, `
SELECT version, v3_status FROM incidents
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? FOR UPDATE`, plan.IncidentID, plan.CycleNo).Scan(&version, &status); err != nil {
		return err
	}
	switch status {
	case "investigating":
		result, err := tx.ExecContext(ctx, `
UPDATE incidents
SET v3_status = 'awaiting_approval', status = 'AWAITING_APPROVAL', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ? AND v3_status = 'investigating'`,
			plan.IncidentID, plan.CycleNo, version)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		return appendPrepareIncidentEvent(ctx, tx, plan, "remediation_plan_created", map[string]any{
			"plan_id": plan.PublicID, "plan_hash": plan.CanonicalPlanHash, "operation": plan.OperationType,
		})
	case "awaiting_approval":
		// A task replay after a committed plan must be idempotent. The V3
		// repository already checked that the same immutable Plan is present.
		return nil
	default:
		return fmt.Errorf("%w: Incident status %q cannot enter approval", asyncjob.ErrInvalidMutation, status)
	}
}

func appendPrepareIncidentEvent(ctx context.Context, tx asyncjob.DBTX, plan *remediation.RemediationPlan, eventType string, metadata map[string]any) error {
	payload, err := json.Marshal(metadata)
	if err != nil || len(payload) > 8192 {
		return asyncjob.ErrInvalidMutation
	}
	idempotency := hashCanonical("remediation.prepare", plan.PublicID, eventType)
	_, err = tx.ExecContext(ctx, `
INSERT INTO incident_events
  (incident_id, event_type, idempotency_key, actor_type, actor_id, summary, metadata_json, occurred_at)
VALUES (?, ?, ?, 'agent', ?, ?, ?, NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		plan.IncidentID, eventType, idempotency, plan.CreatedByAgentRunID,
		"Evidence-backed remediation plan awaits human approval", payload)
	return err
}

// Func adapters keep the operation easy to exercise without replacing the
// production task-fenced path with a fixture implementation.
type RemediationPrepareLoaderFunc func(context.Context, asyncjob.Task) (RemediationPrepareInput, error)

func (f RemediationPrepareLoaderFunc) Load(ctx context.Context, task asyncjob.Task) (RemediationPrepareInput, error) {
	return f(ctx, task)
}
