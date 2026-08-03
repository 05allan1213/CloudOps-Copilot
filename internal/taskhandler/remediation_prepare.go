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
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/google/uuid"
)

const remediationPreparePayloadSchema = 1

type RemediationPrepareInput struct {
	AgentRunID            uint64
	PlanPublicID          string
	SourceType            remediation.PlanSourceType
	Baseline              RemediationPrepareBaselineFence
	Request               remediation.RestoreEnvCompileRequest
	LocalRequest          remediation.LocalScenarioCompileRequest
	MigratedLegacy        bool
	MigratedLegacyContext bool
}

type RemediationPrepareBaselineFence struct {
	ID              uint64
	PublicID        string
	RowVersion      uint64
	GitOpsRevision  string
	ConfigHash      string
	ObservationID   uint64
	ObservationHash string
}

type RemediationPrepareLoader interface {
	Load(context.Context, asyncjob.Task) (RemediationPrepareInput, error)
}

type RemediationPrepareStore interface {
	PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, RemediationPrepareInput, *remediation.RemediationPlan) error
}

type RemediationPrepareConfig struct {
	Loader RemediationPrepareLoader
	Store  RemediationPrepareStore
	Now    func() time.Time
}

func NewRemediationPrepare(config RemediationPrepareConfig) (Operation, error) {
	if config.Loader == nil || config.Store == nil {
		return nil, errors.New("remediation.prepare requires a bounded loader and persistence store")
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
	if payload.AgentRunID == "" || payload.CycleNo != uint64(task.CycleNo) {
		return asyncjob.Dead("invalid_remediation_payload", "remediation payload cycle does not match its subject", nil)
	}
	input, err := o.cfg.Loader.Load(ctx, task)
	if err != nil {
		return remediationPrepareLoadFailure(err)
	}
	sourceType := input.SourceType
	if sourceType == "" {
		sourceType = remediation.PlanSourceGitOps
	}
	requestIncidentID, requestCycleNo, createdByAgentRunID := input.Request.IncidentID, input.Request.CycleNo, input.Request.CreatedByAgentRunID
	if sourceType == remediation.PlanSourceLocalScenario {
		requestIncidentID, requestCycleNo, createdByAgentRunID = input.LocalRequest.IncidentID, input.LocalRequest.CycleNo, input.LocalRequest.CreatedByAgentRunID
	}
	if input.AgentRunID != task.SubjectID || requestIncidentID != task.IncidentID || requestCycleNo != uint64(task.CycleNo) ||
		input.MigratedLegacy != task.MigratedLegacy || input.MigratedLegacyContext != task.MigratedLegacyContext ||
		createdByAgentRunID == "" || payload.AgentRunID != createdByAgentRunID {
		return asyncjob.Dead("remediation_input_mismatch", "compiled remediation input is outside the task Incident/cycle", nil)
	}
	if sourceType == remediation.PlanSourceGitOps && (input.Baseline.ID == 0 || input.Baseline.RowVersion == 0 ||
		input.Baseline.GitOpsRevision != input.Request.LastKnownGoodRevision || input.Baseline.ConfigHash == "" ||
		input.Baseline.ObservationID == 0 || input.Baseline.ObservationHash == "") {
		return asyncjob.Dead("remediation_input_mismatch", "compiled GitOps remediation input has no DeploymentBaseline fence", nil)
	}
	if _, err := uuid.Parse(input.PlanPublicID); err != nil {
		return asyncjob.Dead("remediation_input_mismatch", "compiled remediation input has no deterministic Plan identity", nil)
	}
	var plan remediation.RemediationPlan
	switch sourceType {
	case remediation.PlanSourceGitOps:
		plan, err = remediation.CompileRestoreRequiredEnv(input.Request)
	case remediation.PlanSourceLocalScenario:
		plan, err = remediation.CompileLocalScenarioRestoreRequiredEnv(input.LocalRequest)
	default:
		err = remediation.ErrInvalidArgument
	}
	if err != nil {
		return asyncjob.Dead("remediation_compile_rejected", boundChange(err.Error(), 2048), nil)
	}
	plan.PublicID = input.PlanPublicID
	plan.MigratedLegacy = input.MigratedLegacy
	plan.MigratedLegacyContext = input.MigratedLegacyContext
	if err := remediation.ValidatePlan(plan); err != nil {
		return asyncjob.Dead("remediation_plan_invalid", boundChange(err.Error(), 2048), nil)
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.cfg.Store.PersistIn(ctx, tx, task, input, &plan)
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
	repository remediation.Repository
}

func NewMySQLRemediationPrepareStore(repository remediation.Repository) (RemediationPrepareStore, error) {
	if repository == nil {
		return nil, errors.New("remediation repository is required")
	}
	return &mysqlRemediationPrepareStore{repository: repository}, nil
}

func (s *mysqlRemediationPrepareStore) PersistIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, input RemediationPrepareInput, plan *remediation.RemediationPlan) error {
	sourceType := input.SourceType
	if sourceType == "" {
		sourceType = remediation.PlanSourceGitOps
	}
	createdByAgentRunID := input.Request.CreatedByAgentRunID
	if sourceType == remediation.PlanSourceLocalScenario {
		createdByAgentRunID = input.LocalRequest.CreatedByAgentRunID
	}
	if plan == nil || input.AgentRunID != task.SubjectID || plan.IncidentID != task.IncidentID || plan.CycleNo != uint64(task.CycleNo) ||
		plan.PublicID != input.PlanPublicID || plan.SourceType != sourceType || plan.CreatedByAgentRunID != createdByAgentRunID {
		return asyncjob.ErrInvalidMutation
	}
	var runPublicID, runStatus string
	var runVersion, expectedIncidentVersion uint64
	var runMigratedLegacy, runMigratedLegacyContext bool
	if err := tx.QueryRowContext(ctx, `
SELECT public_id, row_version, status, expected_incident_version, migrated_legacy, migrated_legacy_context
FROM agent_runs
WHERE id = ? AND incident_id = ? AND cycle_no = ?
FOR SHARE`, task.SubjectID, task.IncidentID, task.CycleNo).
		Scan(&runPublicID, &runVersion, &runStatus, &expectedIncidentVersion, &runMigratedLegacy, &runMigratedLegacyContext); err != nil {
		return err
	}
	if runPublicID != plan.CreatedByAgentRunID || runVersion != task.ExpectedSubjectVersion || runStatus != "completed" ||
		expectedIncidentVersion != plan.IncidentVersion || runMigratedLegacy != task.MigratedLegacy ||
		runMigratedLegacyContext != task.MigratedLegacyContext || plan.MigratedLegacy != task.MigratedLegacy ||
		plan.MigratedLegacyContext != task.MigratedLegacyContext {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if plan.SourceType == remediation.PlanSourceGitOps {
		var baselinePublicID, baselineStatus, baselineRevision, configHash string
		var baselineVersion uint64
		if err := tx.QueryRowContext(ctx, `
SELECT public_id, row_version, status, gitops_revision, config_hash
FROM deployment_baselines
WHERE id = ?
FOR SHARE`, input.Baseline.ID).
			Scan(&baselinePublicID, &baselineVersion, &baselineStatus, &baselineRevision, &configHash); err != nil {
			return err
		}
		if baselinePublicID != input.Baseline.PublicID || baselineVersion != input.Baseline.RowVersion || baselineStatus != "active" ||
			baselineRevision != input.Baseline.GitOpsRevision || configHash != input.Baseline.ConfigHash {
			return asyncjob.ErrPolicyViolation
		}
		var observationBaselineID uint64
		var observationHash string
		if err := tx.QueryRowContext(ctx, `
SELECT baseline_id, content_hash
FROM baseline_observations
WHERE id = ? AND observation_type = 'config_blob'
FOR SHARE`, input.Baseline.ObservationID).Scan(&observationBaselineID, &observationHash); err != nil {
			return err
		}
		if observationBaselineID != input.Baseline.ID || observationHash != input.Baseline.ObservationHash || observationHash != input.Baseline.ConfigHash {
			return asyncjob.ErrPolicyViolation
		}
	}
	var existingPlanID uint64
	var existingAuthorization sql.NullInt64
	existingErr := tx.QueryRowContext(ctx, `SELECT id, business_budget_authorization_id
FROM remediation_plans
WHERE public_id = ? AND incident_id = ? AND cycle_no = ?
FOR UPDATE`, plan.PublicID, plan.IncidentID, plan.CycleNo).Scan(&existingPlanID, &existingAuthorization)
	if existingErr == nil {
		if existingPlanID == 0 {
			return asyncjob.ErrInvalidMutation
		}
		if existingAuthorization.Valid {
			plan.BusinessBudgetAuthorizationID = uint64(existingAuthorization.Int64)
		}
		return s.repository.CreatePlanIn(ctx, tx, plan)
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return existingErr
	}
	budget, err := businessbudget.GuardChild(ctx, tx, businessbudget.KindRemediationPlan, task.IncidentID, task.CycleNo, task.SubjectID)
	if err != nil {
		return fmt.Errorf("%w: remediation Plan authorization rejected: %v", asyncjob.ErrPolicyViolation, err)
	}
	if budget.IncidentVersion != plan.IncidentVersion {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if !budget.Allowed() {
		return businessbudget.MarkExhausted(ctx, tx, budget, task.IncidentID, task.CycleNo, "remediation.prepare")
	}
	plan.BusinessBudgetAuthorizationID = budget.AuthorizationID
	if err := s.repository.CreatePlanIn(ctx, tx, plan); err != nil {
		return err
	}
	var version uint64
	var status string
	if err := tx.QueryRowContext(ctx, `
SELECT version, status FROM incidents
WHERE id = ? AND cycle_no = ? FOR UPDATE`, plan.IncidentID, plan.CycleNo).Scan(&version, &status); err != nil {
		return err
	}
	switch status {
	case "investigating":
		result, err := tx.ExecContext(ctx, `
UPDATE incidents
SET status = 'awaiting_approval', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND version = ? AND status = 'investigating'`,
			plan.IncidentID, plan.CycleNo, version)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		return appendPrepareIncidentEvent(ctx, tx, plan, "remediation_plan_created", map[string]any{
			"plan_id": plan.PublicID, "plan_hash": plan.CanonicalPlanHash, "operation": plan.OperationType,
			"business_budget_authorization_id": budget.AuthorizationPublicID,
			"authorization_slot":               budget.AuthorizationSlot,
		})
	case "awaiting_approval":
		// A task replay after a committed plan must be idempotent. The
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
 (public_id, incident_id, cycle_no, event_schema_version,
  event_type, idempotency_key, migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, ?, 1, ?, ?, ?, ?, 'agent', ?, ?, ?, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		uuid.NewString(), plan.IncidentID, plan.CycleNo, eventType, idempotency,
		plan.MigratedLegacyContext, plan.MigratedLegacy, plan.CreatedByAgentRunID,
		"Evidence-backed remediation plan awaits human approval", payload)
	return err
}

// Func adapters keep the operation easy to exercise without replacing the
// production task-fenced path with a fixture implementation.
type RemediationPrepareLoaderFunc func(context.Context, asyncjob.Task) (RemediationPrepareInput, error)

func (f RemediationPrepareLoaderFunc) Load(ctx context.Context, task asyncjob.Task) (RemediationPrepareInput, error) {
	return f(ctx, task)
}
