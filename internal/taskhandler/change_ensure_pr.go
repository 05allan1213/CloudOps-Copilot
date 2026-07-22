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

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

const (
	changeEnsurePayloadSchema = 1
	changeEnsureMaxAttempts   = 5
)

type ChangeEnsurePRTaskStore interface {
	EnqueueIn(context.Context, asyncjob.DBTX, asyncjob.NewTask) (*asyncjob.Task, error)
}

type ChangeEnsurePRConfig struct {
	DB                *sql.DB
	Tasks             ChangeEnsurePRTaskStore
	Writer            remediation.PhasedGitHubWriter
	Git               remediation.ExactGitReader
	ClaimPolicy       agent.ClaimPolicy
	CurrentPolicyHash string
	Now               func() time.Time
}

func NewChangeEnsurePR(config ChangeEnsurePRConfig) (Operation, error) {
	if config.DB == nil || config.Tasks == nil || config.Writer == nil || config.Git == nil {
		return nil, errors.New("change.ensure_pr requires MySQL, task store, exact Git reader, and phased GitHub writer")
	}
	if !validSHA256Text(config.CurrentPolicyHash) {
		return nil, errors.New("change.ensure_pr requires the current lowercase policy hash")
	}
	if _, err := agent.EvaluateSufficiency(agent.SufficiencyInput{IncidentID: "change.ensure_pr", CycleNo: 1, Policy: config.ClaimPolicy}); err != nil {
		return nil, fmt.Errorf("change.ensure_pr requires a valid current claim policy: %w", err)
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	operation := &changeEnsurePROperation{
		cfg: config,
		store: &mysqlChangeEnsurePRStore{
			db: config.DB, tasks: config.Tasks,
			git: config.Git, claimPolicy: config.ClaimPolicy,
			currentPolicyHash: config.CurrentPolicyHash, now: config.Now,
		},
	}
	return operation.handle, nil
}

type changeEnsurePROperation struct {
	cfg             ChangeEnsurePRConfig
	store           changeEnsurePRStore
	externalContext func(context.Context) (context.Context, context.CancelFunc, error)
}

type changeEnsurePRStore interface {
	LoadApprovedPlan(context.Context, asyncjob.Task, time.Time, string) (changePlanSnapshot, error)
	SupersedeApprovedPlanIn(context.Context, asyncjob.DBTX, asyncjob.Task, string) error
	CreateChangeRequestIn(context.Context, asyncjob.DBTX, asyncjob.Task, changePlanSnapshot) error
	LoadChange(context.Context, asyncjob.Task) (changeSnapshot, error)
	ValidateChangePreflight(context.Context, changeSnapshot, time.Time, string) error
	MarkWriteIntent(context.Context, asyncjob.Task, changeSnapshot, string) error
	ApplyObservationIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, remediation.WriteObservation) error
	BlockReconciliationIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, string) error
	InvalidateIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, string, bool) error
}

type changePlanSnapshot struct {
	Plan                  remediation.RemediationPlan
	Decision              remediation.Approval
	LegacyPlanStatus      string
	IncidentStatus        string
	IncidentVersion       uint64
	Request               remediation.PhasedDeliveryRequest
	ChangeRequestPublicID string
	LogicalOperationKey   string
}

type changeSnapshot struct {
	PlanSnapshot         changePlanSnapshot
	ChangeRequestID      uint64
	ChangePublicID       string
	ChangeVersion        uint64
	ChangeStatus         string
	WritePhase           remediation.WritePhase
	LogicalOperation     string
	ExternalMarker       string
	ExternalWriteStarted bool
	PreflightRejected    bool
}

type changeEnsurePayload struct {
	PlanID          string                 `json:"plan_id"`
	ChangeRequestID string                 `json:"change_request_id,omitempty"`
	WritePhase      remediation.WritePhase `json:"write_phase,omitempty"`
}

type changeReconciliationAction uint8

type changeInvalidationDecision struct {
	Terminal bool
	V3Status string
	Event    string
}

const (
	changeReconciliationInvalid changeReconciliationAction = iota
	changeReconciliationAdvance
	changeReconciliationAbsent
	changeReconciliationPending
)

func (o *changeEnsurePROperation) handle(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	key := dispatchKey(task)
	if (key != changeEnsurePlanPRKey && key != changeEnsureRequestPRKey) || task.SubjectID == 0 ||
		task.CycleNo == 0 || task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != changeEnsurePayloadSchema ||
		execution.Lease.TaskID != task.ID || execution.Lease.ExpectedSubjectVersion != task.ExpectedSubjectVersion {
		return asyncjob.Dead("invalid_task_subject", "change.ensure_pr task identity is invalid", nil)
	}
	payload, err := decodeChangeEnsurePayload(task)
	if err != nil {
		return asyncjob.Dead("invalid_change_payload", boundChange(err.Error(), 2048), nil)
	}
	now := o.cfg.Now().UTC()
	if key == changeEnsurePlanPRKey {
		snapshot, loadErr := o.store.LoadApprovedPlan(ctx, task, now, o.cfg.CurrentPolicyHash)
		if loadErr != nil {
			if isTerminalChangePreflight(loadErr) {
				return asyncjob.Dead("change_preflight_rejected", boundChange(loadErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
					return o.store.SupersedeApprovedPlanIn(ctx, tx, task, "change_preflight_rejected")
				})
			}
			return changeLoadFailure(loadErr)
		}
		if payload.PlanID != snapshot.Plan.PublicID || payload.ChangeRequestID != "" || payload.WritePhase != "" {
			return asyncjob.Dead("invalid_change_payload", "plan task payload does not match its subject", nil)
		}
		return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.CreateChangeRequestIn(ctx, tx, task, snapshot)
		})
	}

	snapshot, err := o.store.LoadChange(ctx, task)
	if err != nil {
		if isTerminalChangePreflight(err) && snapshot.ChangeRequestID != 0 && snapshot.PlanSnapshot.Plan.ID != 0 {
			return asyncjob.Dead("change_preflight_rejected", boundChange(err.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, task, snapshot, "change_preflight_rejected", false)
			})
		}
		return changeLoadFailure(err)
	}
	if payload.PlanID != snapshot.PlanSnapshot.Plan.PublicID || payload.ChangeRequestID != snapshot.ChangePublicID || payload.WritePhase != snapshot.WritePhase {
		return asyncjob.Dead("invalid_change_payload", "change task payload does not match its durable subject", nil)
	}
	if snapshot.ExternalMarker != "" {
		if snapshot.PlanSnapshot.Plan.Status == remediation.PlanInvalidated {
			snapshot.PreflightRejected = true
			return o.reconcileRejected(ctx, execution, snapshot)
		}
		if err := o.store.ValidateChangePreflight(ctx, snapshot, now, o.cfg.CurrentPolicyHash); err != nil {
			if isTerminalChangePreflight(err) {
				if errors.Is(err, errChangeApprovalExpired) {
					return o.reconcileExpiredApproval(ctx, execution, snapshot)
				}
				snapshot.PreflightRejected = true
				return o.reconcileRejected(ctx, execution, snapshot)
			}
			return changeLoadFailure(err)
		}
		if err := o.store.MarkWriteIntent(ctx, task, snapshot, snapshot.ExternalMarker); err != nil {
			if isTerminalChangePreflight(err) {
				if errors.Is(err, errChangeApprovalExpired) {
					return o.reconcileExpiredApproval(ctx, execution, snapshot)
				}
				snapshot.PreflightRejected = true
				return o.reconcileRejected(ctx, execution, snapshot)
			}
			return changeLoadFailure(err)
		}
		return o.executePhaseWriter(ctx, task, snapshot)
	}
	if err := o.store.ValidateChangePreflight(ctx, snapshot, now, o.cfg.CurrentPolicyHash); err != nil {
		if isTerminalChangePreflight(err) {
			return asyncjob.Dead("change_preflight_rejected", boundChange(err.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, task, snapshot, "change_preflight_rejected", false)
			})
		}
		return changeLoadFailure(err)
	}
	marker := expectedExternalWriteMarker(snapshot, task.ExpectedSubjectVersion)
	if err := o.store.MarkWriteIntent(ctx, task, snapshot, marker); err != nil {
		if isTerminalChangePreflight(err) {
			return asyncjob.Dead("change_preflight_rejected", boundChange(err.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, task, snapshot, "change_preflight_rejected", false)
			})
		}
		return changeLoadFailure(err)
	}
	snapshot.ExternalMarker = marker
	snapshot.ExternalWriteStarted = true

	return o.executePhaseWriter(ctx, task, snapshot)
}

func expectedExternalWriteMarker(snapshot changeSnapshot, subjectVersion uint64) string {
	return hashCanonical("external-write", snapshot.LogicalOperation, string(snapshot.WritePhase), fmt.Sprint(subjectVersion))
}

func (o *changeEnsurePROperation) reconcileExpiredApproval(ctx context.Context, execution asyncjob.Execution, snapshot changeSnapshot) asyncjob.Result {
	externalCtx, cancel, err := o.newExternalContext(ctx)
	if err != nil {
		return asyncjob.RetryAfter(0, "external_deadline_missing", "GitHub external-call deadline is unavailable", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.BlockReconciliationIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_unavailable")
		})
	}
	observation, callErr := o.callReconciler(externalCtx, snapshot)
	cancel()
	if callErr != nil {
		if isTerminalChangePreflight(callErr) {
			return asyncjob.Dead("github_reconciliation_rejected", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_rejected", false)
			})
		}
		return asyncjob.RetryAfter(0, "github_reconciliation_unavailable", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.BlockReconciliationIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_unavailable")
		})
	}
	switch classifyChangeReconciliation(snapshot.WritePhase, observation) {
	case changeReconciliationInvalid:
		return asyncjob.Dead("github_reconciliation_invalid", "GitHub reconciliation returned incomplete state", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_invalid", false)
		})
	case changeReconciliationAbsent:
		return asyncjob.Dead("github_write_absent", "reconciliation proved that no external branch, commit, or PR remains", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_write_absent", true)
		})
	case changeReconciliationAdvance:
		if err := validateChangeWriteObservation(snapshot, observation); err != nil {
			return asyncjob.Dead("github_reconciliation_invalid", boundChange(err.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_invalid", false)
			})
		}
		if observation.Phase != remediation.WritePhaseComplete {
			snapshot.PreflightRejected = true
		}
		return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.ApplyObservationIn(ctx, tx, execution.Task, snapshot, observation)
		})
	default:
		return asyncjob.RetryAfter(0, "github_reconciliation_pending", "Approval expired before a complete Draft PR was observed", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.BlockReconciliationIn(ctx, tx, execution.Task, snapshot, "change_approval_expired")
		})
	}
}

func (o *changeEnsurePROperation) executePhaseWriter(ctx context.Context, task asyncjob.Task, snapshot changeSnapshot) asyncjob.Result {
	externalCtx, cancel, err := o.newExternalContext(ctx)
	if err != nil {
		return asyncjob.RetryAfter(0, "external_deadline_missing", "GitHub external-call deadline is unavailable", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.BlockReconciliationIn(ctx, tx, task, snapshot, "github_reconciliation_unavailable")
		})
	}
	observation, callErr := o.callPhaseWriter(externalCtx, snapshot)
	cancel()
	if callErr != nil {
		if isTerminalChangePreflight(callErr) {
			return asyncjob.Dead("github_write_rejected", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, task, snapshot, "github_write_rejected", false)
			})
		}
		return asyncjob.RetryAfter(0, "github_write_unavailable", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.BlockReconciliationIn(ctx, tx, task, snapshot, "github_reconciliation_unavailable")
		})
	}
	if err := validateChangeWriteObservation(snapshot, observation); err != nil {
		return asyncjob.Dead("github_write_phase_mismatch", boundChange(err.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, task, snapshot, "github_write_phase_mismatch", false)
		})
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.store.ApplyObservationIn(ctx, tx, task, snapshot, observation)
	})
}

func (o *changeEnsurePROperation) reconcileRejected(ctx context.Context, execution asyncjob.Execution, snapshot changeSnapshot) asyncjob.Result {
	externalCtx, cancel, err := o.newExternalContext(ctx)
	if err != nil {
		return asyncjob.RetryAfter(0, "external_deadline_missing", "GitHub external-call deadline is unavailable", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "change_preflight_rejected", false)
		})
	}
	observation, callErr := o.callReconciler(externalCtx, snapshot)
	cancel()
	if callErr != nil {
		if isTerminalChangePreflight(callErr) {
			return asyncjob.Dead("github_reconciliation_rejected", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_rejected", false)
			})
		}
		return asyncjob.RetryAfter(0, "github_reconciliation_unavailable", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "change_preflight_rejected", false)
		})
	}
	switch classifyChangeReconciliation(snapshot.WritePhase, observation) {
	case changeReconciliationInvalid:
		return asyncjob.Dead("github_reconciliation_invalid", "GitHub reconciliation returned incomplete state", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_invalid", false)
		})
	case changeReconciliationAbsent:
		return asyncjob.Dead("github_write_absent", "reconciliation proved that no external branch, commit, or PR remains", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_write_absent", true)
		})
	case changeReconciliationAdvance:
		if err := validateChangeWriteObservation(snapshot, observation); err != nil {
			return asyncjob.Dead("github_reconciliation_invalid", boundChange(err.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "github_reconciliation_invalid", false)
			})
		}
		return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.ApplyObservationIn(ctx, tx, execution.Task, snapshot, observation)
		})
	default:
		return asyncjob.RetryAfter(0, "github_reconciliation_pending", "external GitHub state remains present or ambiguous; reconciliation only", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, execution.Task, snapshot, "change_preflight_rejected", false)
		})
	}
}

func (o *changeEnsurePROperation) newExternalContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if o.externalContext != nil {
		return o.externalContext(ctx)
	}
	return asyncjob.ExternalCallContext(ctx)
}

func classifyChangeReconciliation(current remediation.WritePhase, observation remediation.WriteObservation) changeReconciliationAction {
	if !observation.Reconciled || observation.Phase == "" || !validWritePhase(observation.Phase) {
		return changeReconciliationInvalid
	}
	if observation.Phase == remediation.WritePhaseEnsureBranch {
		return changeReconciliationAbsent
	}
	if validWriteAdvance(current, observation.Phase) {
		return changeReconciliationAdvance
	}
	return changeReconciliationPending
}

func classifyChangeInvalidation(externalStarted, safeTerminal bool) changeInvalidationDecision {
	if safeTerminal && externalStarted {
		return changeInvalidationDecision{Terminal: true, V3Status: "failed", Event: "external_write_reconciled_absent"}
	}
	if !externalStarted {
		return changeInvalidationDecision{Terminal: true, V3Status: "superseded", Event: "change_preflight_superseded"}
	}
	return changeInvalidationDecision{}
}

func validWritePhase(phase remediation.WritePhase) bool {
	switch phase {
	case remediation.WritePhaseEnsureBranch, remediation.WritePhaseEnsureCommit, remediation.WritePhaseEnsureDraftPR, remediation.WritePhaseComplete:
		return true
	default:
		return false
	}
}

func (o *changeEnsurePROperation) callReconciler(ctx context.Context, snapshot changeSnapshot) (remediation.WriteObservation, error) {
	return o.cfg.Writer.ReconcileDraftPR(ctx, snapshot.PlanSnapshot.Request)
}

func (o *changeEnsurePROperation) callPhaseWriter(ctx context.Context, snapshot changeSnapshot) (remediation.WriteObservation, error) {
	switch snapshot.WritePhase {
	case remediation.WritePhaseEnsureBranch:
		return o.cfg.Writer.EnsureBranch(ctx, snapshot.PlanSnapshot.Request)
	case remediation.WritePhaseEnsureCommit:
		return o.cfg.Writer.EnsureCommit(ctx, snapshot.PlanSnapshot.Request)
	case remediation.WritePhaseEnsureDraftPR:
		return o.cfg.Writer.EnsureDraftPR(ctx, snapshot.PlanSnapshot.Request)
	default:
		return remediation.WriteObservation{}, remediation.ErrInvalidArgument
	}
}

func decodeChangeEnsurePayload(task asyncjob.Task) (changeEnsurePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(task.Payload)))
	decoder.DisallowUnknownFields()
	var payload changeEnsurePayload
	if err := decoder.Decode(&payload); err != nil || strings.TrimSpace(payload.PlanID) == "" {
		return changeEnsurePayload{}, errors.New("change.ensure_pr payload is malformed")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return changeEnsurePayload{}, errors.New("change.ensure_pr payload has multiple JSON values")
	}
	return payload, nil
}

func validWriteAdvance(current, next remediation.WritePhase) bool {
	switch current {
	case remediation.WritePhaseEnsureBranch:
		return next == remediation.WritePhaseEnsureCommit || next == remediation.WritePhaseEnsureDraftPR || next == remediation.WritePhaseComplete
	case remediation.WritePhaseEnsureCommit:
		return next == remediation.WritePhaseEnsureDraftPR || next == remediation.WritePhaseComplete
	case remediation.WritePhaseEnsureDraftPR:
		return next == remediation.WritePhaseComplete
	default:
		return false
	}
}

func validateChangeWriteObservation(snapshot changeSnapshot, observation remediation.WriteObservation) error {
	plan := snapshot.PlanSnapshot.Plan
	if !validWriteAdvance(snapshot.WritePhase, observation.Phase) || observation.BaseSHA != plan.TargetBaseRevision {
		return fmt.Errorf("%w: GitHub observation does not advance the approved base", remediation.ErrDrift)
	}
	switch observation.Phase {
	case remediation.WritePhaseEnsureCommit:
		if observation.BranchSHA != plan.TargetBaseRevision || observation.CommitSHA != "" || observation.TreeSHA != "" || observation.PRNumber != 0 || observation.PRURL != "" {
			return fmt.Errorf("%w: branch observation is not bound to the approved base", remediation.ErrDrift)
		}
	case remediation.WritePhaseEnsureDraftPR:
		if observation.CommitSHA == "" || observation.BranchSHA != observation.CommitSHA || observation.TreeSHA != plan.ExpectedTreeHash || observation.PRNumber != 0 || observation.PRURL != "" {
			return fmt.Errorf("%w: commit observation is not bound to the approved tree", remediation.ErrDrift)
		}
	case remediation.WritePhaseComplete:
		if observation.CommitSHA == "" || observation.BranchSHA != observation.CommitSHA || observation.TreeSHA != plan.ExpectedTreeHash || observation.PRNumber <= 0 || strings.TrimSpace(observation.PRURL) == "" {
			return fmt.Errorf("%w: Draft PR observation is not bound to the approved commit and tree", remediation.ErrDrift)
		}
	default:
		return fmt.Errorf("%w: GitHub observation has an invalid next phase", remediation.ErrDrift)
	}
	return nil
}

func changeLoadFailure(err error) asyncjob.Result {
	switch {
	case errors.Is(err, asyncjob.ErrSubjectVersionMismatch):
		return asyncjob.Dead("subject_version_mismatch", "change subject version or Incident cycle no longer matches", nil)
	case errors.Is(err, asyncjob.ErrPolicyViolation), errors.Is(err, remediation.ErrApprovalMismatch), errors.Is(err, remediation.ErrDrift):
		return asyncjob.Dead("change_preflight_rejected", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, asyncjob.ErrInvalidMutation):
		return asyncjob.Dead("change_mutation_rejected", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, sql.ErrNoRows):
		return asyncjob.Dead("change_subject_missing", "change.ensure_pr subject no longer exists", nil)
	default:
		return asyncjob.RetryAfter(0, "change_store_unavailable", boundChange(err.Error(), 2048), nil)
	}
}

type mysqlChangeEnsurePRStore struct {
	db                *sql.DB
	tasks             ChangeEnsurePRTaskStore
	git               remediation.ExactGitReader
	claimPolicy       agent.ClaimPolicy
	currentPolicyHash string
	now               func() time.Time
}

type changeQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *mysqlChangeEnsurePRStore) LoadApprovedPlan(ctx context.Context, task asyncjob.Task, now time.Time, policyHash string) (changePlanSnapshot, error) {
	snapshot, err := s.loadPlan(ctx, s.db, task.SubjectID, false)
	if err != nil {
		return changePlanSnapshot{}, err
	}
	if snapshot.Plan.CycleNo != uint64(task.CycleNo) || snapshot.Plan.RowVersion != task.ExpectedSubjectVersion || snapshot.Plan.IncidentID != task.IncidentID ||
		snapshot.Plan.MigratedLegacy != task.MigratedLegacy || snapshot.Plan.MigratedLegacyContext != task.MigratedLegacyContext {
		return changePlanSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	if err := s.validateDurablePreflight(ctx, s.db, snapshot, now, policyHash, false, remediation.PlanApproved, "awaiting_approval"); err != nil {
		return changePlanSnapshot{}, err
	}
	if err := s.validateGitPreflight(ctx, snapshot.Plan); err != nil {
		return changePlanSnapshot{}, err
	}
	snapshot.ChangeRequestPublicID = uuid.NewString()
	snapshot.LogicalOperationKey = hashCanonical("change.ensure_pr", snapshot.Plan.PublicID, snapshot.Plan.CanonicalPlanHash)
	snapshot.Request = buildPhasedDeliveryRequest(snapshot.Plan, snapshot.LogicalOperationKey)
	return snapshot, nil
}

func (s *mysqlChangeEnsurePRStore) LoadChange(ctx context.Context, task asyncjob.Task) (changeSnapshot, error) {
	var snapshot changeSnapshot
	var writePhase, repository, baseRevision, headBranch, idempotencyKey string
	var expectedSubjectVersion uint64
	var migratedLegacy, migratedLegacyContext bool
	if err := s.db.QueryRowContext(ctx, `
SELECT id, public_id, plan_id, row_version, COALESCE(v3_status,''), COALESCE(write_phase,''),
       COALESCE(logical_operation_key,''), COALESCE(external_write_marker,''),
       repository, base_revision, head_branch, idempotency_key, expected_subject_version,
       migrated_legacy, migrated_legacy_context,
       EXISTS (
           SELECT 1 FROM change_request_events e
           WHERE e.change_request_id = change_requests.id
             AND e.incident_id = change_requests.incident_id
             AND e.cycle_no = change_requests.cycle_no
             AND e.domain_schema_version = 3
             AND e.external_write_started = TRUE
       )
FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		task.SubjectID, task.IncidentID, task.CycleNo).
		Scan(&snapshot.ChangeRequestID, &snapshot.ChangePublicID, &snapshot.PlanSnapshot.Plan.ID,
			&snapshot.ChangeVersion, &snapshot.ChangeStatus, &writePhase, &snapshot.LogicalOperation,
			&snapshot.ExternalMarker, &repository, &baseRevision, &headBranch, &idempotencyKey,
			&expectedSubjectVersion, &migratedLegacy, &migratedLegacyContext, &snapshot.ExternalWriteStarted); err != nil {
		return changeSnapshot{}, err
	}
	if snapshot.ChangeVersion != task.ExpectedSubjectVersion || migratedLegacy != task.MigratedLegacy ||
		migratedLegacyContext != task.MigratedLegacyContext {
		return changeSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	snapshot.WritePhase = remediation.WritePhase(writePhase)
	plan, err := s.loadPlan(ctx, s.db, snapshot.PlanSnapshot.Plan.ID, false)
	snapshot.PlanSnapshot = plan
	if err != nil {
		return snapshot, err
	}
	if snapshot.PlanSnapshot.Plan.IncidentID != task.IncidentID || snapshot.PlanSnapshot.Plan.CycleNo != uint64(task.CycleNo) ||
		snapshot.PlanSnapshot.Plan.MigratedLegacy != migratedLegacy ||
		snapshot.PlanSnapshot.Plan.MigratedLegacyContext != migratedLegacyContext ||
		snapshot.ChangeStatus != "pending" || expectedSubjectVersion != snapshot.ChangeVersion ||
		!validWritePhase(snapshot.WritePhase) || snapshot.WritePhase == remediation.WritePhaseComplete {
		return changeSnapshot{}, fmt.Errorf("%w: ChangeRequest is not writable", asyncjob.ErrInvalidMutation)
	}
	expectedLogicalOperation := hashCanonical("change.ensure_pr", snapshot.PlanSnapshot.Plan.PublicID, snapshot.PlanSnapshot.Plan.CanonicalPlanHash)
	expectedRequest := buildPhasedDeliveryRequest(snapshot.PlanSnapshot.Plan, expectedLogicalOperation)
	expectedMarker := expectedExternalWriteMarker(changeSnapshot{LogicalOperation: expectedLogicalOperation, WritePhase: snapshot.WritePhase}, task.ExpectedSubjectVersion)
	if _, err := uuid.Parse(snapshot.ChangePublicID); err != nil || snapshot.LogicalOperation != expectedLogicalOperation ||
		idempotencyKey != expectedLogicalOperation || repository != snapshot.PlanSnapshot.Plan.TargetRepository ||
		baseRevision != snapshot.PlanSnapshot.Plan.TargetBaseRevision || headBranch != expectedRequest.Branch ||
		(snapshot.ExternalMarker != "" && (snapshot.ExternalMarker != expectedMarker || !validSHA256Text(snapshot.ExternalMarker))) {
		return snapshot, fmt.Errorf("%w: ChangeRequest immutable delivery binding is invalid", asyncjob.ErrPolicyViolation)
	}
	if snapshot.ExternalMarker != "" {
		snapshot.ExternalWriteStarted = true
	}
	if snapshot.PlanSnapshot.IncidentStatus != "delivering" ||
		snapshot.PlanSnapshot.LegacyPlanStatus != string(remediation.PlanApproved) ||
		(snapshot.PlanSnapshot.Plan.Status != remediation.PlanConsumed && snapshot.PlanSnapshot.Plan.Status != remediation.PlanInvalidated) {
		return changeSnapshot{}, fmt.Errorf("%w: ChangeRequest no longer belongs to active delivery", asyncjob.ErrInvalidMutation)
	}
	snapshot.PlanSnapshot.LogicalOperationKey = expectedLogicalOperation
	snapshot.PlanSnapshot.Request = expectedRequest
	return snapshot, nil
}

func (s *mysqlChangeEnsurePRStore) ValidateChangePreflight(ctx context.Context, snapshot changeSnapshot, now time.Time, policyHash string) error {
	if snapshot.PlanSnapshot.Plan.Status != remediation.PlanConsumed {
		return fmt.Errorf("%w: ChangeRequest no longer has a writable approved Plan", asyncjob.ErrInvalidMutation)
	}
	if err := s.validateDurablePreflight(ctx, s.db, snapshot.PlanSnapshot, now, policyHash, false, remediation.PlanConsumed, "delivering"); err != nil {
		return err
	}
	return s.validateGitPreflight(ctx, snapshot.PlanSnapshot.Plan)
}

func (s *mysqlChangeEnsurePRStore) SupersedeApprovedPlanIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, reason string) error {
	var cycleNo, rowVersion uint64
	var publicID, status string
	var migratedLegacy, migratedLegacyContext bool
	if err := tx.QueryRowContext(ctx, `SELECT public_id, cycle_no, row_version, v3_status, migrated_legacy, migrated_legacy_context
FROM remediation_plans
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 FOR UPDATE`, task.SubjectID, task.IncidentID).
		Scan(&publicID, &cycleNo, &rowVersion, &status, &migratedLegacy, &migratedLegacyContext); err != nil {
		return err
	}
	if cycleNo != uint64(task.CycleNo) || rowVersion != task.ExpectedSubjectVersion || status != string(remediation.PlanApproved) ||
		migratedLegacy != task.MigratedLegacy || migratedLegacyContext != task.MigratedLegacyContext {
		return asyncjob.ErrSubjectVersionMismatch
	}
	result, err := tx.ExecContext(ctx, `UPDATE remediation_plans
SET status = 'superseded', v3_status = 'superseded', row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND row_version = ?
  AND domain_schema_version = 3 AND v3_status = 'approved'`,
		task.SubjectID, task.IncidentID, task.CycleNo, task.ExpectedSubjectVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	incidentVersion, err := s.returnIncidentToInvestigating(ctx, tx, task, reason)
	if err != nil {
		return err
	}
	if err := appendChangeIncidentEvent(ctx, tx, task, "remediation_plan_superseded", publicID, reason); err != nil {
		return err
	}
	return s.enqueueChangeInvestigation(ctx, tx, task, incidentVersion, reason)
}

func (s *mysqlChangeEnsurePRStore) CreateChangeRequestIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot changePlanSnapshot) error {
	var cycle, planVersion uint64
	var planStatus string
	var planMigratedLegacy, planMigratedLegacyContext bool
	if err := tx.QueryRowContext(ctx, `
SELECT cycle_no, row_version, v3_status, migrated_legacy, migrated_legacy_context FROM remediation_plans
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.SubjectID, task.IncidentID).Scan(&cycle, &planVersion, &planStatus, &planMigratedLegacy, &planMigratedLegacyContext); err != nil {
		return err
	}
	if cycle != uint64(task.CycleNo) || planVersion != task.ExpectedSubjectVersion || planStatus != "approved" ||
		planMigratedLegacy != task.MigratedLegacy || planMigratedLegacyContext != task.MigratedLegacyContext {
		return asyncjob.ErrSubjectVersionMismatch
	}
	var incidentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT v3_status FROM incidents WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`, task.IncidentID, task.CycleNo).Scan(&incidentStatus); err != nil {
		return err
	}
	if incidentStatus != "awaiting_approval" {
		return asyncjob.ErrInvalidMutation
	}
	current, err := s.loadPlan(ctx, tx, snapshot.Plan.ID, true)
	if err != nil {
		if isTerminalChangePreflight(err) {
			return s.SupersedeApprovedPlanIn(ctx, tx, task, "change_preflight_rejected")
		}
		return err
	}
	if err := s.validateDurablePreflight(ctx, tx, current, s.now().UTC(), s.currentPolicyHash, true, remediation.PlanApproved, "awaiting_approval"); err != nil {
		if isTerminalChangePreflight(err) {
			return s.SupersedeApprovedPlanIn(ctx, tx, task, "change_preflight_rejected")
		}
		return err
	}
	current.ChangeRequestPublicID = snapshot.ChangeRequestPublicID
	current.LogicalOperationKey = hashCanonical("change.ensure_pr", current.Plan.PublicID, current.Plan.CanonicalPlanHash)
	current.Request = buildPhasedDeliveryRequest(current.Plan, current.LogicalOperationKey)
	snapshot = current
	result, err := tx.ExecContext(ctx, `
INSERT INTO change_requests
  (public_id, plan_id, repository, base_revision, head_branch, status, ci_status,
   idempotency_key, row_version, domain_schema_version, incident_id, cycle_no,
   v3_status, migrated_legacy, migrated_legacy_context, write_phase, expected_subject_version, logical_operation_key,
   commit_sha, pr_number, pr_url, lease_owner, attempts, failure_code)
VALUES (?, ?, ?, ?, ?, 'pending', 'pending', ?, 1, 3, ?, ?, 'pending', ?, ?, 'ensure_branch', 1, ?, '', 0, '', '', 0, '')
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		snapshot.ChangeRequestPublicID, snapshot.Plan.ID, snapshot.Plan.TargetRepository,
		snapshot.Plan.TargetBaseRevision, snapshot.Request.Branch, snapshot.LogicalOperationKey,
		snapshot.Plan.IncidentID, snapshot.Plan.CycleNo, snapshot.Plan.MigratedLegacy,
		snapshot.Plan.MigratedLegacyContext, snapshot.LogicalOperationKey)
	if err != nil {
		return err
	}
	changeID, err := result.LastInsertId()
	if err != nil || changeID <= 0 {
		return fmt.Errorf("read ChangeRequest id: %w", err)
	}
	planUpdate, err := tx.ExecContext(ctx, `
UPDATE remediation_plans SET v3_status = 'consumed', row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND row_version = ? AND v3_status = 'approved'`, snapshot.Plan.ID, task.ExpectedSubjectVersion)
	if err != nil {
		return err
	}
	if affected, _ := planUpdate.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	incidentUpdate, err := tx.ExecContext(ctx, `
UPDATE incidents SET v3_status = 'delivering', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status = 'awaiting_approval'`, task.IncidentID, task.CycleNo)
	if err != nil {
		return err
	}
	if affected, _ := incidentUpdate.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if err := appendChangeEvent(ctx, tx, uint64(changeID), task.IncidentID, task.CycleNo, 1, "change_request_created", "worker", remediation.WritePhaseEnsureBranch, false, "", map[string]any{
		"change_request_id": snapshot.ChangeRequestPublicID, "plan_id": snapshot.Plan.PublicID,
		"logical_operation_key": snapshot.LogicalOperationKey,
	}); err != nil {
		return err
	}
	payload, _ := json.Marshal(changeEnsurePayload{PlanID: snapshot.Plan.PublicID, ChangeRequestID: snapshot.ChangeRequestPublicID, WritePhase: remediation.WritePhaseEnsureBranch})
	_, err = s.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskChangeEnsurePR,
		SubjectType: "change_request", SubjectID: uint64(changeID), Transition: "change.ensure_pr",
		ExpectedSubjectVersion: 1, PayloadSchemaVersion: changeEnsurePayloadSchema, Payload: payload,
		DedupeKey:           hashCanonical("change.ensure_pr", fmt.Sprint(changeID), string(remediation.WritePhaseEnsureBranch), "1"),
		LogicalOperationKey: snapshot.LogicalOperationKey, MigratedLegacy: snapshot.Plan.MigratedLegacy,
		MigratedLegacyContext: snapshot.Plan.MigratedLegacyContext, Priority: 80, MaxAttempts: changeEnsureMaxAttempts,
	})
	return err
}

func (s *mysqlChangeEnsurePRStore) MarkWriteIntent(ctx context.Context, task asyncjob.Task, snapshot changeSnapshot, marker string) error {
	if marker != expectedExternalWriteMarker(snapshot, task.ExpectedSubjectVersion) {
		return fmt.Errorf("%w: external write marker is not bound to the current phase", asyncjob.ErrPolicyViolation)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version uint64
	var phase, existingMarker string
	if err := tx.QueryRowContext(ctx, `
SELECT row_version, write_phase, COALESCE(external_write_marker,'')
FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status = 'pending'
FOR UPDATE`, task.SubjectID, task.IncidentID, task.CycleNo).Scan(&version, &phase, &existingMarker); err != nil {
		return err
	}
	if version != task.ExpectedSubjectVersion || remediation.WritePhase(phase) != snapshot.WritePhase {
		return asyncjob.ErrSubjectVersionMismatch
	}
	current, err := s.loadPlan(ctx, tx, snapshot.PlanSnapshot.Plan.ID, true)
	if err != nil {
		return err
	}
	if err := s.validateDurablePreflight(ctx, tx, current, s.now().UTC(), s.currentPolicyHash, true, remediation.PlanConsumed, "delivering"); err != nil {
		return err
	}
	if current.Plan.CanonicalPlanHash != snapshot.PlanSnapshot.Plan.CanonicalPlanHash ||
		current.Plan.EvidenceSetHash != snapshot.PlanSnapshot.Plan.EvidenceSetHash ||
		current.Plan.ExpectedTreeHash != snapshot.PlanSnapshot.Plan.ExpectedTreeHash ||
		current.Plan.ExpectedPostImageHash != snapshot.PlanSnapshot.Plan.ExpectedPostImageHash {
		return fmt.Errorf("%w: approved Plan changed before the external write marker", asyncjob.ErrPolicyViolation)
	}
	if existingMarker != "" {
		if existingMarker != marker {
			return fmt.Errorf("%w: external write marker does not match the current phase", asyncjob.ErrPolicyViolation)
		}
		return tx.Commit()
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE change_requests
SET external_write_started_at = NOW(6), external_write_marker = ?, updated_at = NOW(6)
WHERE id = ? AND row_version = ? AND external_write_marker IS NULL`, marker, task.SubjectID, task.ExpectedSubjectVersion)
	if err != nil {
		return err
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	sequence, err := nextChangeSequence(ctx, tx, task.SubjectID)
	if err != nil {
		return err
	}
	if err := appendChangeEvent(ctx, tx, task.SubjectID, task.IncidentID, task.CycleNo, sequence, "external_write_started", "worker", snapshot.WritePhase, true, marker, map[string]any{
		"phase": snapshot.WritePhase, "logical_operation_key": snapshot.LogicalOperation,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *mysqlChangeEnsurePRStore) ApplyObservationIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot changeSnapshot, observation remediation.WriteObservation) error {
	if err := validateChangeWriteObservation(snapshot, observation); err != nil {
		return err
	}
	var version uint64
	var marker, phase string
	if err := tx.QueryRowContext(ctx, `
SELECT row_version, write_phase, COALESCE(external_write_marker,'') FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.SubjectID, task.IncidentID, task.CycleNo).Scan(&version, &phase, &marker); err != nil {
		return err
	}
	if version != task.ExpectedSubjectVersion || remediation.WritePhase(phase) != snapshot.WritePhase || marker == "" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	var planStatus string
	if err := tx.QueryRowContext(ctx, `SELECT v3_status FROM remediation_plans
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		snapshot.PlanSnapshot.Plan.ID, task.IncidentID, task.CycleNo).Scan(&planStatus); err != nil {
		return err
	}
	if planStatus != string(remediation.PlanConsumed) && planStatus != string(remediation.PlanInvalidated) {
		return asyncjob.ErrInvalidMutation
	}
	if snapshot.PreflightRejected && planStatus == string(remediation.PlanConsumed) {
		result, err := tx.ExecContext(ctx, `UPDATE remediation_plans
SET v3_status = 'invalidated', row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status = 'consumed'`,
			snapshot.PlanSnapshot.Plan.ID, task.IncidentID, task.CycleNo)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		planStatus = string(remediation.PlanInvalidated)
	}
	if planStatus == string(remediation.PlanInvalidated) {
		result, err := tx.ExecContext(ctx, `UPDATE change_requests
SET expected_subject_version = ?, commit_sha = ?, pr_number = ?, pr_url = ?,
    failure_code = 'change_preflight_rejected', failure_reason = 'change_preflight_rejected',
    external_write_started_at = NULL, external_write_marker = NULL,
    row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND row_version = ? AND external_write_marker = ?`,
			version+1, observation.CommitSHA, observation.PRNumber, observation.PRURL,
			task.SubjectID, version, marker)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		sequence, err := nextChangeSequence(ctx, tx, task.SubjectID)
		if err != nil {
			return err
		}
		if err := appendChangeEvent(ctx, tx, task.SubjectID, task.IncidentID, task.CycleNo, sequence, "external_write_observed_after_invalidation", "github", snapshot.WritePhase, true, marker, observation); err != nil {
			return err
		}
		var incidentVersion uint64
		if err := tx.QueryRowContext(ctx, `SELECT version FROM incidents
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status = 'delivering' FOR UPDATE`,
			task.IncidentID, task.CycleNo).Scan(&incidentVersion); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `UPDATE incidents
SET needs_attention = TRUE, blocking_reason_code = 'approved_change_invalidated',
    blocked_at = COALESCE(blocked_at, NOW(6)), version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND version = ? AND v3_status = 'delivering'`,
			task.IncidentID, task.CycleNo, incidentVersion)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		return appendChangeIncidentEvent(ctx, tx, task, "approved_change_invalidated", snapshot.ChangePublicID, "change_preflight_rejected")
	}
	nextPhase := observation.Phase
	updates := `write_phase = ?, expected_subject_version = ?, commit_sha = ?, pr_number = ?, pr_url = ?,
external_write_started_at = NULL, external_write_marker = NULL, row_version = row_version + 1, updated_at = NOW(6)`
	writePhase := string(nextPhase)
	status, v3Status := "pending", "pending"
	if nextPhase == remediation.WritePhaseComplete {
		writePhase, status, v3Status = "observe", "pr_created", "pr_open"
	}
	result, err := tx.ExecContext(ctx, `UPDATE change_requests SET status = ?, v3_status = ?, `+updates+`
WHERE id = ? AND row_version = ? AND external_write_marker = ?`,
		status, v3Status, writePhase, version+1, observation.CommitSHA, observation.PRNumber,
		observation.PRURL, task.SubjectID, version, marker)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	sequence, err := nextChangeSequence(ctx, tx, task.SubjectID)
	if err != nil {
		return err
	}
	if err := appendChangeEvent(ctx, tx, task.SubjectID, task.IncidentID, task.CycleNo, sequence, "external_write_observed", "github", snapshot.WritePhase, true, marker, observation); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `UPDATE incidents
SET needs_attention = FALSE, blocking_reason_code = NULL, blocked_at = NULL, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status = 'delivering'
  AND blocking_reason_code IN ('github_reconciliation_pending','github_reconciliation_unavailable')`,
		task.IncidentID, task.CycleNo)
	if nextPhase == remediation.WritePhaseComplete {
		payload, _ := json.Marshal(map[string]any{"change_request_id": snapshot.ChangePublicID, "phase": "observe"})
		_, err = s.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
			IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskDeliveryObserve,
			SubjectType: "change_request", SubjectID: task.SubjectID, Transition: "delivery.observe",
			ExpectedSubjectVersion: version + 1, PayloadSchemaVersion: 1, Payload: payload,
			DedupeKey:           hashCanonical("delivery.observe", fmt.Sprint(task.SubjectID), fmt.Sprint(version+1)),
			LogicalOperationKey: snapshot.LogicalOperation, MigratedLegacy: task.MigratedLegacy,
			MigratedLegacyContext: task.MigratedLegacyContext, Priority: 70, MaxAttempts: changeEnsureMaxAttempts,
		})
		return err
	}
	payload, _ := json.Marshal(changeEnsurePayload{PlanID: snapshot.PlanSnapshot.Plan.PublicID, ChangeRequestID: snapshot.ChangePublicID, WritePhase: nextPhase})
	_, err = s.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskChangeEnsurePR,
		SubjectType: "change_request", SubjectID: task.SubjectID, Transition: "change.ensure_pr",
		ExpectedSubjectVersion: version + 1, PayloadSchemaVersion: changeEnsurePayloadSchema, Payload: payload,
		DedupeKey:           hashCanonical("change.ensure_pr", fmt.Sprint(task.SubjectID), string(nextPhase), fmt.Sprint(version+1)),
		LogicalOperationKey: snapshot.LogicalOperation, MigratedLegacy: task.MigratedLegacy,
		MigratedLegacyContext: task.MigratedLegacyContext, Priority: 80, MaxAttempts: changeEnsureMaxAttempts,
	})
	return err
}

func (s *mysqlChangeEnsurePRStore) BlockReconciliationIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot changeSnapshot, reason string) error {
	var version uint64
	var marker string
	if err := tx.QueryRowContext(ctx, `SELECT row_version, COALESCE(external_write_marker,'')
FROM change_requests WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.SubjectID, task.IncidentID, task.CycleNo).Scan(&version, &marker); err != nil {
		return err
	}
	if version != task.ExpectedSubjectVersion || marker == "" || marker != snapshot.ExternalMarker {
		return asyncjob.ErrSubjectVersionMismatch
	}
	var incidentVersion uint64
	var incidentStatus, blockingReason string
	var needsAttention bool
	if err := tx.QueryRowContext(ctx, `SELECT version, v3_status, needs_attention, COALESCE(blocking_reason_code,'')
FROM incidents WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.IncidentID, task.CycleNo).Scan(&incidentVersion, &incidentStatus, &needsAttention, &blockingReason); err != nil {
		return err
	}
	if incidentStatus != "delivering" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if !needsAttention || blockingReason != reason {
		result, err := tx.ExecContext(ctx, `UPDATE incidents
SET needs_attention = TRUE, blocking_reason_code = ?, blocked_at = COALESCE(blocked_at, NOW(6)),
    version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND version = ? AND v3_status = 'delivering'`,
			reason, task.IncidentID, task.CycleNo, incidentVersion)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
	}
	return appendChangeIncidentEvent(ctx, tx, task, "change_reconciliation_blocked", snapshot.ChangePublicID, reason)
}

func (s *mysqlChangeEnsurePRStore) InvalidateIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot changeSnapshot, reason string, safeTerminal bool) error {
	var version, planID uint64
	var changeStatus, phase, marker string
	var externalStarted bool
	if err := tx.QueryRowContext(ctx, `SELECT row_version, plan_id, v3_status, COALESCE(write_phase,''),
       COALESCE(external_write_marker,''),
       EXISTS (
           SELECT 1 FROM change_request_events e
           WHERE e.change_request_id = change_requests.id
             AND e.incident_id = change_requests.incident_id
             AND e.cycle_no = change_requests.cycle_no
             AND e.domain_schema_version = 3
             AND e.external_write_started = TRUE
       )
FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.SubjectID, task.IncidentID, task.CycleNo).
		Scan(&version, &planID, &changeStatus, &phase, &marker, &externalStarted); err != nil {
		return err
	}
	if version != task.ExpectedSubjectVersion || planID != snapshot.PlanSnapshot.Plan.ID || changeStatus != "pending" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	externalStarted = externalStarted || marker != ""
	var planStatus string
	if err := tx.QueryRowContext(ctx, `SELECT v3_status FROM remediation_plans
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		planID, task.IncidentID, task.CycleNo).Scan(&planStatus); err != nil {
		return err
	}
	if planStatus != string(remediation.PlanConsumed) && planStatus != string(remediation.PlanInvalidated) {
		return asyncjob.ErrInvalidMutation
	}
	if planStatus != string(remediation.PlanInvalidated) {
		result, err := tx.ExecContext(ctx, `UPDATE remediation_plans
SET v3_status = 'invalidated', row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND v3_status = 'consumed' AND domain_schema_version = 3`, planID)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
	}

	decision := classifyChangeInvalidation(externalStarted, safeTerminal)
	if decision.Terminal {
		result, err := tx.ExecContext(ctx, `UPDATE change_requests
SET status = 'failed', v3_status = ?, failure_code = ?, failure_reason = ?,
    external_write_started_at = NULL, external_write_marker = NULL,
    row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND row_version = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
			decision.V3Status, reason, reason, task.SubjectID, version, task.IncidentID, task.CycleNo)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		sequence, err := nextChangeSequence(ctx, tx, task.SubjectID)
		if err != nil {
			return err
		}
		externalEvent := externalStarted && marker != ""
		if err := appendChangeEvent(ctx, tx, task.SubjectID, task.IncidentID, task.CycleNo, sequence, decision.Event, "worker", remediation.WritePhase(phase), externalEvent, marker, map[string]any{
			"reason": reason, "external_state_safe": safeTerminal,
		}); err != nil {
			return err
		}
		incidentVersion, err := s.returnIncidentToInvestigating(ctx, tx, task, reason)
		if err != nil {
			return err
		}
		return s.enqueueChangeInvestigation(ctx, tx, task, incidentVersion, reason)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE change_requests
SET failure_code = ?, failure_reason = ?, updated_at = NOW(6)
WHERE id = ? AND row_version = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		reason, reason, task.SubjectID, version, task.IncidentID, task.CycleNo); err != nil {
		return err
	}
	var incidentVersion uint64
	var incidentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT version, v3_status FROM incidents
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`, task.IncidentID, task.CycleNo).
		Scan(&incidentVersion, &incidentStatus); err != nil {
		return err
	}
	if incidentStatus != "delivering" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	result, err := tx.ExecContext(ctx, `UPDATE incidents
SET needs_attention = TRUE, blocking_reason_code = ?, blocked_at = COALESCE(blocked_at, NOW(6)),
    version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND version = ? AND v3_status = 'delivering'`,
		reason, task.IncidentID, task.CycleNo, incidentVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	return appendChangeIncidentEvent(ctx, tx, task, "approved_change_invalidated", snapshot.ChangePublicID, reason)
}

func (s *mysqlChangeEnsurePRStore) returnIncidentToInvestigating(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, reason string) (uint64, error) {
	var version uint64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT version, v3_status FROM incidents
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`, task.IncidentID, task.CycleNo).
		Scan(&version, &status); err != nil {
		return 0, err
	}
	if status != "awaiting_approval" && status != "delivering" {
		return 0, asyncjob.ErrSubjectVersionMismatch
	}
	result, err := tx.ExecContext(ctx, `UPDATE incidents
SET status = 'DIAGNOSING', v3_status = 'investigating',
    version = version + 1, needs_attention = FALSE, blocking_reason_code = NULL,
    blocked_at = NULL, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND version = ? AND v3_status = ?`,
		task.IncidentID, task.CycleNo, version, status)
	if err != nil {
		return 0, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return 0, asyncjob.ErrSubjectVersionMismatch
	}
	if err := appendChangeIncidentEvent(ctx, tx, task, "incident_returned_to_investigating", fmt.Sprint(task.SubjectID), reason); err != nil {
		return 0, err
	}
	return version + 1, nil
}

func (s *mysqlChangeEnsurePRStore) enqueueChangeInvestigation(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, incidentVersion uint64, reason string) error {
	budget, err := businessbudget.GuardAutomatic(ctx, tx, businessbudget.KindAgentRun, task.IncidentID, task.CycleNo)
	if err != nil {
		return err
	}
	if budget.IncidentVersion != incidentVersion {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if !budget.Allowed() {
		return businessbudget.MarkExhausted(ctx, tx, budget, task.IncidentID, task.CycleNo, "change.ensure_pr")
	}
	payload, _ := json.Marshal(map[string]any{
		"mode": "start", "cycle_no": task.CycleNo,
		"migrated_legacy_context": task.MigratedLegacyContext,
	})
	_, err = s.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskInvestigationAdvance,
		SubjectType: "incident", SubjectID: task.IncidentID, Transition: "investigation.start",
		ExpectedSubjectVersion: incidentVersion, PayloadSchemaVersion: 1, Payload: payload,
		DedupeKey: hashCanonical("change.ensure_pr", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo),
			"investigation.start", fmt.Sprint(incidentVersion), reason),
		MigratedLegacyContext: task.MigratedLegacyContext,
		Priority:              80, MaxAttempts: 3,
	})
	return err
}

func appendChangeIncidentEvent(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, eventType, subject, reason string) error {
	metadata, err := json.Marshal(map[string]any{
		"source": "change.ensure_pr", "subject": subject, "reason": reason,
	})
	if err != nil || len(metadata) > 8192 {
		return asyncjob.ErrInvalidMutation
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO incident_events
 (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
  event_type, idempotency_key, migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
 VALUES (?, ?, 3, ?, 1, ?, ?, ?, ?, 'system', 'change.ensure_pr', ?, ?, NOW(6), NOW(6))
 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		uuid.NewString(), task.IncidentID, task.CycleNo, eventType,
		hashCanonical("change.ensure_pr.event", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo), eventType, subject, reason),
		task.MigratedLegacyContext, task.MigratedLegacy,
		boundChange(reason, 2048), metadata)
	return err
}

func buildPhasedDeliveryRequest(plan remediation.RemediationPlan, logicalOperationKey string) remediation.PhasedDeliveryRequest {
	marker := "<!-- cloudops-remediation:" + plan.PublicID + ":" + plan.CanonicalPlanHash + " -->"
	evidence := make([]string, 0, len(plan.EvidenceBindings))
	for _, binding := range plan.EvidenceBindings {
		evidence = append(evidence, "- /api/v3/incidents/"+plan.IncidentPublicID+"/evidence/"+binding.ID)
	}
	body := strings.Join([]string{
		marker,
		"Incident: " + plan.IncidentPublicID,
		"Plan: " + plan.PublicID,
		"Canonical plan hash: " + plan.CanonicalPlanHash,
		"Evidence:", strings.Join(evidence, "\n"),
	}, "\n\n")
	return remediation.PhasedDeliveryRequest{
		DeliveryRequest: remediation.DeliveryRequest{
			Repository: plan.TargetRepository, BaseRevision: plan.TargetBaseRevision,
			BaseBranch: plan.TargetBaseBranch, Path: plan.TargetPath, Content: append([]byte(nil), plan.PostImage...),
			Branch:      "cloudops/incident-" + plan.IncidentPublicID + "/remediation-" + plan.PublicID,
			CommitTitle: "cloudops: approved remediation " + plan.PublicID,
			PRTitle:     "[Draft] restore required environment", PRBody: body, Marker: marker,
		},
		BaseBlobSHA: plan.BaseBlobSHA, ExpectedBeforeHash: plan.ExpectedBeforeHash,
		ExpectedPostImageHash: plan.ExpectedPostImageHash, ExpectedTreeHash: plan.ExpectedTreeHash,
		LogicalOperationKey: logicalOperationKey,
	}
}

func nextChangeSequence(ctx context.Context, tx asyncjob.DBTX, changeID uint64) (uint64, error) {
	var sequence uint64
	err := tx.QueryRowContext(ctx, `SELECT sequence_no + 1 FROM change_request_events WHERE change_request_id = ? ORDER BY sequence_no DESC LIMIT 1 FOR UPDATE`, changeID).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return sequence, nil
}

func appendChangeEvent(ctx context.Context, tx asyncjob.DBTX, changeID, incidentID uint64, cycleNo uint32, sequence uint64, eventType, source string, phase remediation.WritePhase, external bool, marker string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > 8192 {
		return asyncjob.ErrInvalidMutation
	}
	contentHash := hashCanonical("change-event", fmt.Sprint(changeID), fmt.Sprint(sequence), eventType, string(encoded))
	var phaseValue any
	if phase != "" && phase != remediation.WritePhaseComplete {
		phaseValue = string(phase)
	}
	var markerValue any
	if marker != "" {
		markerValue = marker
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO change_request_events
  (public_id, domain_schema_version, event_schema_version, incident_id, cycle_no,
   change_request_id, sequence_no, event_type, source_system, write_phase,
   external_write_started, external_write_marker, payload_json, content_hash, occurred_at)
VALUES (?, 3, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6))`,
		uuid.NewString(), incidentID, cycleNo, changeID, sequence, eventType, source,
		phaseValue, external, markerValue, encoded, contentHash)
	return err
}

func boundChange(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
