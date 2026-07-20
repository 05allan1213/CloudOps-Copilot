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

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
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
	CurrentPolicyHash string
	Now               func() time.Time
}

func NewChangeEnsurePR(config ChangeEnsurePRConfig) (Operation, error) {
	if config.DB == nil || config.Tasks == nil || config.Writer == nil {
		return nil, errors.New("change.ensure_pr requires MySQL, task store, and phased GitHub writer")
	}
	if len(config.CurrentPolicyHash) != 64 || strings.ToLower(config.CurrentPolicyHash) != config.CurrentPolicyHash {
		return nil, errors.New("change.ensure_pr requires the current lowercase policy hash")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	operation := &changeEnsurePROperation{
		cfg:   config,
		store: &mysqlChangeEnsurePRStore{db: config.DB, tasks: config.Tasks},
	}
	return operation.handle, nil
}

type changeEnsurePROperation struct {
	cfg   ChangeEnsurePRConfig
	store changeEnsurePRStore
}

type changeEnsurePRStore interface {
	LoadApprovedPlan(context.Context, asyncjob.Task, time.Time, string) (changePlanSnapshot, error)
	CreateChangeRequestIn(context.Context, asyncjob.DBTX, asyncjob.Task, changePlanSnapshot) error
	LoadChange(context.Context, asyncjob.Task, time.Time, string) (changeSnapshot, error)
	MarkWriteIntent(context.Context, asyncjob.Task, changeSnapshot, string) error
	ApplyObservationIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, remediation.WriteObservation) error
	InvalidateIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, string) error
}

type changePlanSnapshot struct {
	Plan                  remediation.RemediationPlan
	Decision              remediation.Approval
	IncidentStatus        string
	IncidentVersion       uint64
	Request               remediation.PhasedDeliveryRequest
	ChangeRequestPublicID string
	LogicalOperationKey   string
}

type changeSnapshot struct {
	PlanSnapshot     changePlanSnapshot
	ChangeRequestID  uint64
	ChangePublicID   string
	ChangeVersion    uint64
	ChangeStatus     string
	WritePhase       remediation.WritePhase
	LogicalOperation string
	ExternalMarker   string
}

type changeEnsurePayload struct {
	PlanID          string                 `json:"plan_id"`
	ChangeRequestID string                 `json:"change_request_id,omitempty"`
	WritePhase      remediation.WritePhase `json:"write_phase,omitempty"`
}

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
			return changeLoadFailure(loadErr)
		}
		if payload.PlanID != snapshot.Plan.PublicID || payload.ChangeRequestID != "" || payload.WritePhase != "" {
			return asyncjob.Dead("invalid_change_payload", "plan task payload does not match its subject", nil)
		}
		return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.CreateChangeRequestIn(ctx, tx, task, snapshot)
		})
	}

	snapshot, err := o.store.LoadChange(ctx, task, now, o.cfg.CurrentPolicyHash)
	if err != nil {
		return changeLoadFailure(err)
	}
	if payload.PlanID != snapshot.PlanSnapshot.Plan.PublicID || payload.ChangeRequestID != snapshot.ChangePublicID || payload.WritePhase != snapshot.WritePhase {
		return asyncjob.Dead("invalid_change_payload", "change task payload does not match its durable subject", nil)
	}
	marker := hashCanonical("external-write", snapshot.LogicalOperation, string(snapshot.WritePhase), fmt.Sprint(task.ExpectedSubjectVersion))
	if err := o.store.MarkWriteIntent(ctx, task, snapshot, marker); err != nil {
		return changeLoadFailure(err)
	}

	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return asyncjob.RetryAfter(0, "external_deadline_missing", "GitHub external-call deadline is unavailable", nil)
	}
	observation, callErr := o.callWriter(externalCtx, snapshot)
	cancel()
	if callErr != nil {
		if errors.Is(callErr, remediation.ErrDrift) || errors.Is(callErr, remediation.ErrConflict) ||
			errors.Is(callErr, remediation.ErrForbidden) || errors.Is(callErr, remediation.ErrApprovalMismatch) {
			return asyncjob.Dead("github_write_rejected", boundChange(callErr.Error(), 2048), func(ctx context.Context, tx asyncjob.DBTX) error {
				return o.store.InvalidateIn(ctx, tx, task, snapshot, "github_write_rejected")
			})
		}
		return asyncjob.RetryAfter(0, "github_write_unavailable", boundChange(callErr.Error(), 2048), nil)
	}
	if !validWriteAdvance(snapshot.WritePhase, observation.Phase) {
		return asyncjob.Dead("github_write_phase_mismatch", "GitHub reconciliation returned an invalid phase transition", func(ctx context.Context, tx asyncjob.DBTX) error {
			return o.store.InvalidateIn(ctx, tx, task, snapshot, "github_write_phase_mismatch")
		})
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.store.ApplyObservationIn(ctx, tx, task, snapshot, observation)
	})
}

func (o *changeEnsurePROperation) callWriter(ctx context.Context, snapshot changeSnapshot) (remediation.WriteObservation, error) {
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
	db    *sql.DB
	tasks ChangeEnsurePRTaskStore
}

type changeQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *mysqlChangeEnsurePRStore) LoadApprovedPlan(ctx context.Context, task asyncjob.Task, now time.Time, policyHash string) (changePlanSnapshot, error) {
	snapshot, err := s.loadPlan(ctx, s.db, task.SubjectID)
	if err != nil {
		return changePlanSnapshot{}, err
	}
	if snapshot.Plan.CycleNo != uint64(task.CycleNo) || snapshot.Plan.RowVersion != task.ExpectedSubjectVersion || snapshot.Plan.IncidentID != task.IncidentID {
		return changePlanSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	if err := s.validatePreflight(ctx, s.db, snapshot, now, policyHash); err != nil {
		return changePlanSnapshot{}, err
	}
	snapshot.ChangeRequestPublicID = uuid.NewString()
	snapshot.LogicalOperationKey = hashCanonical("change.ensure_pr", snapshot.Plan.PublicID, snapshot.Plan.CanonicalPlanHash)
	snapshot.Request = buildPhasedDeliveryRequest(snapshot.Plan, snapshot.LogicalOperationKey)
	return snapshot, nil
}

func (s *mysqlChangeEnsurePRStore) LoadChange(ctx context.Context, task asyncjob.Task, now time.Time, policyHash string) (changeSnapshot, error) {
	var snapshot changeSnapshot
	var writePhase string
	if err := s.db.QueryRowContext(ctx, `
SELECT id, public_id, plan_id, row_version, COALESCE(v3_status,''), COALESCE(write_phase,''),
       COALESCE(logical_operation_key,''), COALESCE(external_write_marker,'')
FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		task.SubjectID, task.IncidentID, task.CycleNo).
		Scan(&snapshot.ChangeRequestID, &snapshot.ChangePublicID, &snapshot.PlanSnapshot.Plan.ID,
			&snapshot.ChangeVersion, &snapshot.ChangeStatus, &writePhase, &snapshot.LogicalOperation, &snapshot.ExternalMarker); err != nil {
		return changeSnapshot{}, err
	}
	if snapshot.ChangeVersion != task.ExpectedSubjectVersion {
		return changeSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	snapshot.WritePhase = remediation.WritePhase(writePhase)
	plan, err := s.loadPlan(ctx, s.db, snapshot.PlanSnapshot.Plan.ID)
	if err != nil {
		return changeSnapshot{}, err
	}
	snapshot.PlanSnapshot = plan
	if err := s.validatePreflight(ctx, s.db, plan, now, policyHash); err != nil {
		return changeSnapshot{}, err
	}
	if snapshot.ChangeStatus != "pending" || snapshot.LogicalOperation == "" || snapshot.WritePhase == "" {
		return changeSnapshot{}, fmt.Errorf("%w: ChangeRequest is not writable", asyncjob.ErrInvalidMutation)
	}
	snapshot.PlanSnapshot.LogicalOperationKey = snapshot.LogicalOperation
	snapshot.PlanSnapshot.Request = buildPhasedDeliveryRequest(snapshot.PlanSnapshot.Plan, snapshot.LogicalOperation)
	return snapshot, nil
}

func (s *mysqlChangeEnsurePRStore) loadPlan(ctx context.Context, queryer changeQueryer, planID uint64) (changePlanSnapshot, error) {
	var snapshot changePlanSnapshot
	var evidenceJSON []byte
	var decision, operation, planStatus string
	const query = `
SELECT p.id, p.public_id, p.incident_id, i.public_id, i.v3_status, i.version,
       p.cycle_no, p.incident_version, p.plan_version, p.row_version, p.v3_status,
       p.operation_type, p.target_repository, p.target_base_branch, p.target_base_revision,
       p.target_path, p.base_blob_sha, p.expected_before_hash, p.expected_post_image_hash,
       p.expected_tree_hash, p.post_image, p.canonical_plan_hash, p.hash_schema_version,
       p.plan_content_schema_version, p.proposed_patch_hash, p.policy_snapshot_hash,
       p.verification_plan_hash, p.evidence_set_hash, p.evidence_bindings_json, p.expires_at,
       d.id, d.public_id, d.domain_schema_version, d.decision_schema_version,
       d.plan_version, d.decision, d.actor_provider, d.actor_login, d.actor_role, d.reason,
       d.request_id, d.request_authenticated_at, d.expires_at, d.approved_hash_schema_version,
       d.approved_plan_hash, d.approved_base_sha, d.approved_post_image_hash,
       d.approved_tree_hash, d.approved_patch_hash, d.approved_policy_hash,
       d.approved_verification_hash, d.approved_evidence_set_hash, d.created_at
FROM remediation_plans p
JOIN incidents i ON i.id = p.incident_id AND i.domain_schema_version = 3 AND i.cycle_no = p.cycle_no
JOIN remediation_decisions d ON d.plan_id = p.id AND d.incident_id = p.incident_id AND d.cycle_no = p.cycle_no
WHERE p.id = ? AND p.domain_schema_version = 3 AND p.plan_content_schema_version = 2`
	if err := queryer.QueryRowContext(ctx, query, planID).Scan(
		&snapshot.Plan.ID, &snapshot.Plan.PublicID, &snapshot.Plan.IncidentID, &snapshot.Plan.IncidentPublicID,
		&snapshot.IncidentStatus, &snapshot.IncidentVersion, &snapshot.Plan.CycleNo, &snapshot.Plan.IncidentVersion,
		&snapshot.Plan.PlanVersion, &snapshot.Plan.RowVersion, &planStatus, &operation,
		&snapshot.Plan.TargetRepository, &snapshot.Plan.TargetBaseBranch, &snapshot.Plan.TargetBaseRevision,
		&snapshot.Plan.TargetPath, &snapshot.Plan.BaseBlobSHA, &snapshot.Plan.ExpectedBeforeHash,
		&snapshot.Plan.ExpectedPostImageHash, &snapshot.Plan.ExpectedTreeHash, &snapshot.Plan.PostImage,
		&snapshot.Plan.CanonicalPlanHash, &snapshot.Plan.HashSchemaVersion, &snapshot.Plan.PlanContentSchemaVersion,
		&snapshot.Plan.ProposedPatchHash, &snapshot.Plan.PolicySnapshotHash, &snapshot.Plan.VerificationPlanHash,
		&snapshot.Plan.EvidenceSetHash, &evidenceJSON, &snapshot.Plan.ExpiresAt,
		&snapshot.Decision.ID, &snapshot.Decision.PublicID, &snapshot.Decision.DomainSchemaVersion,
		&snapshot.Decision.DecisionSchemaVersion, &snapshot.Decision.PlanVersion, &decision,
		&snapshot.Decision.ActorProvider, &snapshot.Decision.Actor, &snapshot.Decision.Role, &snapshot.Decision.Reason,
		&snapshot.Decision.RequestID, &snapshot.Decision.RequestAuthenticatedAt, &snapshot.Decision.ExpiresAt,
		&snapshot.Decision.ApprovedHashSchemaVersion, &snapshot.Decision.ApprovedPlanHash,
		&snapshot.Decision.ApprovedBaseSHA, &snapshot.Decision.ApprovedPostImageHash,
		&snapshot.Decision.ApprovedTreeHash, &snapshot.Decision.ApprovedPatchHash,
		&snapshot.Decision.ApprovedPolicyHash, &snapshot.Decision.ApprovedVerificationHash,
		&snapshot.Decision.ApprovedEvidenceSetHash, &snapshot.Decision.CreatedAt,
	); err != nil {
		return changePlanSnapshot{}, err
	}
	snapshot.Decision.PlanID = snapshot.Plan.ID
	snapshot.Decision.IncidentID = snapshot.Plan.IncidentID
	snapshot.Decision.CycleNo = snapshot.Plan.CycleNo
	snapshot.Decision.Decision = remediation.Decision(decision)
	snapshot.Plan.Status = remediation.PlanStatus(planStatus)
	snapshot.Plan.OperationType = remediation.OperationType(operation)
	if err := json.Unmarshal(evidenceJSON, &snapshot.Plan.EvidenceBindings); err != nil {
		return changePlanSnapshot{}, fmt.Errorf("decode plan evidence bindings: %w", err)
	}
	return snapshot, nil
}

func (s *mysqlChangeEnsurePRStore) validatePreflight(ctx context.Context, queryer changeQueryer, snapshot changePlanSnapshot, now time.Time, policyHash string) error {
	plan := snapshot.Plan
	if snapshot.IncidentStatus != "awaiting_approval" && snapshot.IncidentStatus != "delivering" {
		return fmt.Errorf("%w: Incident is outside approval/delivery", asyncjob.ErrPolicyViolation)
	}
	if plan.OperationType != remediation.OperationRestoreRequiredEnv ||
		(plan.Status != remediation.PlanApproved && plan.Status != remediation.PlanStatus("consumed")) ||
		!now.Before(plan.ExpiresAt.UTC()) || !now.Before(snapshot.Decision.ExpiresAt.UTC()) ||
		plan.PolicySnapshotHash != policyHash {
		return fmt.Errorf("%w: Plan is expired, stale, or policy-drifted", asyncjob.ErrPolicyViolation)
	}
	if err := remediation.ValidateV3ApprovalBinding(plan, snapshot.Decision, now); err != nil {
		return err
	}
	if len(plan.PostImage) == 0 || remediation.HashBytes(plan.PostImage) != plan.ExpectedPostImageHash {
		return fmt.Errorf("%w: persisted post-image does not match the approved hash", remediation.ErrDrift)
	}
	for _, binding := range plan.EvidenceBindings {
		var contentHash string
		var valid, truncated bool
		if err := queryer.QueryRowContext(ctx, `
SELECT content_hash, valid, truncated
FROM evidence_items
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
			binding.ID, plan.IncidentID, plan.CycleNo).Scan(&contentHash, &valid, &truncated); err != nil {
			return err
		}
		if contentHash != binding.ContentHash || !valid || truncated {
			return fmt.Errorf("%w: approved Evidence is stale or unusable", asyncjob.ErrPolicyViolation)
		}
	}
	return nil
}

func (s *mysqlChangeEnsurePRStore) CreateChangeRequestIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot changePlanSnapshot) error {
	var cycle, planVersion uint64
	var planStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT cycle_no, row_version, v3_status FROM remediation_plans
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.SubjectID, task.IncidentID).Scan(&cycle, &planVersion, &planStatus); err != nil {
		return err
	}
	if cycle != uint64(task.CycleNo) || planVersion != task.ExpectedSubjectVersion || planStatus != "approved" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	var incidentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT v3_status FROM incidents WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`, task.IncidentID, task.CycleNo).Scan(&incidentStatus); err != nil {
		return err
	}
	if incidentStatus != "awaiting_approval" {
		return asyncjob.ErrInvalidMutation
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO change_requests
  (public_id, plan_id, repository, base_revision, head_branch, status, ci_status,
   idempotency_key, row_version, domain_schema_version, incident_id, cycle_no,
   v3_status, write_phase, expected_subject_version, logical_operation_key,
   commit_sha, pr_number, pr_url, lease_owner, attempts, failure_code)
VALUES (?, ?, ?, ?, ?, 'pending', 'pending', ?, 1, 3, ?, ?, 'pending', 'ensure_branch', 1, ?, '', 0, '', '', 0, '')
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		snapshot.ChangeRequestPublicID, snapshot.Plan.ID, snapshot.Plan.TargetRepository,
		snapshot.Plan.TargetBaseRevision, snapshot.Request.Branch, snapshot.LogicalOperationKey,
		snapshot.Plan.IncidentID, snapshot.Plan.CycleNo, snapshot.LogicalOperationKey)
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
		LogicalOperationKey: snapshot.LogicalOperationKey, Priority: 80, MaxAttempts: changeEnsureMaxAttempts,
	})
	return err
}

func (s *mysqlChangeEnsurePRStore) MarkWriteIntent(ctx context.Context, task asyncjob.Task, snapshot changeSnapshot, marker string) error {
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
	if existingMarker != "" {
		if existingMarker != marker {
			return asyncjob.ErrInvalidMutation
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
	var version uint64
	var marker string
	if err := tx.QueryRowContext(ctx, `
SELECT row_version, COALESCE(external_write_marker,'') FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`,
		task.SubjectID, task.IncidentID, task.CycleNo).Scan(&version, &marker); err != nil {
		return err
	}
	if version != task.ExpectedSubjectVersion || marker == "" {
		return asyncjob.ErrSubjectVersionMismatch
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
	if nextPhase == remediation.WritePhaseComplete {
		payload, _ := json.Marshal(map[string]any{"change_request_id": snapshot.ChangePublicID, "phase": "observe"})
		_, err = s.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
			IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskDeliveryObserve,
			SubjectType: "change_request", SubjectID: task.SubjectID, Transition: "delivery.observe",
			ExpectedSubjectVersion: version + 1, PayloadSchemaVersion: 1, Payload: payload,
			DedupeKey:           hashCanonical("delivery.observe", fmt.Sprint(task.SubjectID), fmt.Sprint(version+1)),
			LogicalOperationKey: snapshot.LogicalOperation, Priority: 70, MaxAttempts: changeEnsureMaxAttempts,
		})
		return err
	}
	payload, _ := json.Marshal(changeEnsurePayload{PlanID: snapshot.PlanSnapshot.Plan.PublicID, ChangeRequestID: snapshot.ChangePublicID, WritePhase: nextPhase})
	_, err = s.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskChangeEnsurePR,
		SubjectType: "change_request", SubjectID: task.SubjectID, Transition: "change.ensure_pr",
		ExpectedSubjectVersion: version + 1, PayloadSchemaVersion: changeEnsurePayloadSchema, Payload: payload,
		DedupeKey:           hashCanonical("change.ensure_pr", fmt.Sprint(task.SubjectID), string(nextPhase), fmt.Sprint(version+1)),
		LogicalOperationKey: snapshot.LogicalOperation, Priority: 80, MaxAttempts: changeEnsureMaxAttempts,
	})
	return err
}

func (s *mysqlChangeEnsurePRStore) InvalidateIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot changeSnapshot, reason string) error {
	result, err := tx.ExecContext(ctx, `
UPDATE change_requests
SET status = 'failed', v3_status = 'failed', failure_code = ?, failure_reason = ?,
    row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND row_version = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		reason, reason, task.SubjectID, task.ExpectedSubjectVersion, task.IncidentID, task.CycleNo)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	_, _ = tx.ExecContext(ctx, `UPDATE remediation_plans SET v3_status = 'invalidated', row_version = row_version + 1, updated_at = NOW(6) WHERE id = ? AND domain_schema_version = 3`, snapshot.PlanSnapshot.Plan.ID)
	_, _ = tx.ExecContext(ctx, `UPDATE incidents SET needs_attention = TRUE, blocking_reason_code = ?, blocked_at = NOW(6), updated_at = NOW(6) WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3`, reason, task.IncidentID, task.CycleNo)
	return nil
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
