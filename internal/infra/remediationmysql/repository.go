// Package remediationmysql owns the narrow MySQL adapter for immutable V3
// remediation Plans and Decisions. It is intentionally independent from the
// legacy incidentmysql repository bundle so cloudops-worker can import it.
package remediationmysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

const v3PlanSelect = `SELECT
    p.id, p.public_id, p.incident_id, i.public_id, p.domain_schema_version,
    p.cycle_no, p.incident_version, ar.public_id, p.diagnosis_hash,
    p.plan_version, p.plan_hash, p.status, p.v3_status, p.operation_type,
    p.target_repository, p.target_base_revision, p.target_base_branch,
    p.last_known_good_sha, p.base_blob_sha, p.file_mode, p.target_path,
    p.target_resource_json, p.target_field_ref, p.parameters_json,
    p.evidence_references_json, p.risk_level, p.policy_snapshot_hash,
    p.expected_before_hash, p.expected_post_image_hash, p.expected_tree_hash,
    p.proposed_patch_hash, p.canonical_change_manifest_json, p.bounded_diff,
    p.post_image, p.patch_summary, p.rollback_plan, p.validation_plan,
    p.policy_version, p.policy_snapshot_json, p.verification_plan_json,
    p.verification_plan_hash, p.evidence_bindings_json, p.evidence_set_hash,
    p.hash_schema_version, p.canonical_plan_hash, p.plan_content_schema_version,
    p.row_version, p.created_at, p.updated_at, p.expires_at
FROM remediation_plans p
JOIN incidents i ON i.id = p.incident_id
JOIN agent_runs ar ON ar.id = p.created_by_agent_run_id`

type V3RemediationRepository struct {
	db *sql.DB
}

var _ remediation.V3Repository = (*V3RemediationRepository)(nil)

func NewV3RemediationRepository(db *sql.DB) (*V3RemediationRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: V3 remediation database required", remediation.ErrInvalidArgument)
	}
	return &V3RemediationRepository{db: db}, nil
}

func (r *V3RemediationRepository) CreatePlan(ctx context.Context, plan *remediation.RemediationPlan) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("%w: V3 remediation repository is not initialized", remediation.ErrInvalidArgument)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin V3 plan transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.CreatePlanIn(ctx, tx, plan); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit V3 plan transaction: %w", err)
	}
	return nil
}

func (r *V3RemediationRepository) CreatePlanIn(ctx context.Context, executor remediation.PersistenceTX, plan *remediation.RemediationPlan) error {
	if executor == nil || plan == nil || plan.Status != remediation.PlanAwaitingApproval {
		return remediation.ErrInvalidArgument
	}
	if err := remediation.ValidateV3Plan(*plan); err != nil {
		return err
	}
	incident, err := lockV3RemediationIncident(ctx, executor, plan.IncidentID)
	if err != nil {
		return err
	}
	if incident.publicID != plan.IncidentPublicID || incident.cycleNo != plan.CycleNo {
		return fmt.Errorf("%w: plan does not belong to the current Incident cycle", remediation.ErrConflict)
	}

	existing, existingErr := findV3PlanByIdentity(ctx, executor, plan, true)
	if existingErr == nil {
		if sameV3PlanContent(*existing, *plan) {
			*plan = *existing
			return nil
		}
		return fmt.Errorf("%w: V3 plan identity already has different content", remediation.ErrConflict)
	}
	if !errors.Is(existingErr, remediation.ErrNotFound) {
		return existingErr
	}
	if incident.version != plan.IncidentVersion || incident.status != "investigating" || !incident.databaseNow.Before(plan.ExpiresAt) {
		return fmt.Errorf("%w: stale Incident version or phase for V3 plan", remediation.ErrConflict)
	}

	agentRun, err := resolveV3AgentRun(ctx, executor, plan.CreatedByAgentRunID, plan.IncidentID, plan.CycleNo, true)
	if err != nil {
		return err
	}
	if agentRun.status != "completed" || agentRun.expectedIncidentVersion != plan.IncidentVersion || agentRun.diagnosisHash != plan.DiagnosisHash {
		return fmt.Errorf("%w: V3 plan is not bound to the completed diagnosis", remediation.ErrConflict)
	}
	if err := validateV3PlanEvidence(ctx, executor, plan); err != nil {
		return err
	}

	parametersJSON, err := json.Marshal(plan.Parameters)
	if err != nil || len(parametersJSON) > remediation.MaxPlanJSONBytes {
		return remediation.ErrInvalidArgument
	}
	targetJSON, err := json.Marshal(plan.Parameters.Target)
	if err != nil || len(targetJSON) > 4096 {
		return remediation.ErrInvalidArgument
	}
	evidenceReferencesJSON, err := json.Marshal(plan.EvidenceReferences)
	if err != nil || len(evidenceReferencesJSON) > 16*1024 {
		return remediation.ErrInvalidArgument
	}
	evidenceBindingsJSON, err := json.Marshal(plan.EvidenceBindings)
	if err != nil || len(evidenceBindingsJSON) > 16*1024 {
		return remediation.ErrInvalidArgument
	}

	result, err := executor.ExecContext(ctx, `INSERT INTO remediation_plans (
    public_id, incident_id, domain_schema_version, cycle_no, incident_version,
    created_by_agent_run_id, diagnosis_hash, plan_version, plan_hash, status,
    v3_status, operation_type, target_repository, target_base_revision,
    target_base_branch, last_known_good_sha, base_blob_sha, file_mode, target_path,
    target_resource_json, target_field_ref, parameters_json, evidence_references_json,
    risk_level, policy_snapshot_hash, expected_before_hash,
    expected_post_image_hash, expected_tree_hash, proposed_patch_hash,
    canonical_change_manifest_json, bounded_diff, post_image, patch_summary,
    rollback_plan, validation_plan, policy_version, policy_snapshot_json,
    verification_plan_json, verification_plan_hash, evidence_bindings_json,
    evidence_set_hash, hash_schema_version, canonical_plan_hash,
    plan_content_schema_version, row_version, created_at, updated_at, expires_at
) VALUES (
    ?, ?, 3, ?, ?, ?, ?, ?, ?, 'awaiting_approval', 'awaiting_approval',
    'restore_required_env', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'low', ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)`,
		plan.PublicID, plan.IncidentID, plan.CycleNo, plan.IncidentVersion,
		agentRun.id, plan.DiagnosisHash, plan.PlanVersion, plan.PlanHash,
		plan.TargetRepository, plan.TargetBaseRevision, plan.TargetBaseBranch,
		plan.LastKnownGoodRevision, plan.BaseBlobSHA, plan.FileMode, plan.TargetPath,
		targetJSON, plan.TargetFieldRef, parametersJSON, evidenceReferencesJSON,
		plan.PolicySnapshotHash, plan.ExpectedBeforeHash, plan.ExpectedPostImageHash,
		plan.ExpectedTreeHash, plan.ProposedPatchHash, []byte(plan.CanonicalChangeManifest),
		plan.BoundedDiff, plan.PostImage, plan.PatchSummary, plan.RollbackPlan,
		plan.ValidationPlan, plan.PolicyVersion, []byte(plan.PolicySnapshot),
		[]byte(plan.VerificationPlan), plan.VerificationPlanHash, evidenceBindingsJSON,
		plan.EvidenceSetHash, plan.HashSchemaVersion, plan.CanonicalPlanHash,
		plan.PlanContentSchemaVersion, plan.RowVersion, plan.CreatedAt.UTC(),
		plan.UpdatedAt.UTC(), plan.ExpiresAt.UTC(),
	)
	if err != nil {
		if !isMySQLDuplicate(err) {
			return classifyV3RemediationError(err)
		}
		existing, loadErr := findV3PlanByIdentity(ctx, executor, plan, true)
		if loadErr == nil && sameV3PlanContent(*existing, *plan) {
			*plan = *existing
			return nil
		}
		if loadErr != nil && !errors.Is(loadErr, remediation.ErrNotFound) {
			return loadErr
		}
		return fmt.Errorf("%w: duplicate V3 plan identity", remediation.ErrConflict)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return fmt.Errorf("read V3 plan ID: %w", err)
	}
	plan.ID = uint64(id)
	return nil
}

func (r *V3RemediationRepository) GetPlan(ctx context.Context, publicID string) (*remediation.RemediationPlan, error) {
	if r == nil || r.db == nil {
		return nil, remediation.ErrInvalidArgument
	}
	publicID = strings.TrimSpace(publicID)
	if _, err := uuid.Parse(publicID); err != nil {
		return nil, remediation.ErrInvalidArgument
	}
	return loadV3Plan(ctx, r.db, "p.public_id = ?", false, publicID)
}

// LockPlanIn loads the immutable V3 Plan through an owning transaction and
// holds its row lock until that transaction completes. Command workflows use
// it to bind the human Decision to the exact version/hash they inspected.
func (r *V3RemediationRepository) LockPlanIn(ctx context.Context, executor remediation.PersistenceTX, publicID string) (*remediation.RemediationPlan, error) {
	if r == nil || r.db == nil || executor == nil {
		return nil, remediation.ErrInvalidArgument
	}
	publicID = strings.TrimSpace(publicID)
	if _, err := uuid.Parse(publicID); err != nil {
		return nil, remediation.ErrInvalidArgument
	}
	owner, err := loadV3Plan(ctx, executor, "p.public_id = ?", false, publicID)
	if err != nil {
		return nil, err
	}
	if _, err := lockV3RemediationIncident(ctx, executor, owner.IncidentID); err != nil {
		return nil, err
	}
	return loadV3Plan(ctx, executor, "p.public_id = ?", true, publicID)
}

func (r *V3RemediationRepository) ResolveAgentRunID(ctx context.Context, executor remediation.PersistenceTX, publicID string, incidentID, cycleNo uint64) (uint64, error) {
	publicID = strings.TrimSpace(publicID)
	if executor == nil || incidentID == 0 || cycleNo == 0 {
		return 0, remediation.ErrInvalidArgument
	}
	if _, err := uuid.Parse(publicID); err != nil {
		return 0, remediation.ErrInvalidArgument
	}
	run, err := resolveV3AgentRun(ctx, executor, publicID, incidentID, cycleNo, true)
	if err != nil {
		return 0, err
	}
	return run.id, nil
}

func (r *V3RemediationRepository) RecordDecision(ctx context.Context, planPublicID string, expectedRowVersion uint64, decision *remediation.Approval) error {
	if r == nil || r.db == nil {
		return remediation.ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin V3 decision transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.RecordDecisionIn(ctx, tx, planPublicID, expectedRowVersion, decision); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit V3 decision transaction: %w", err)
	}
	return nil
}

func (r *V3RemediationRepository) RecordDecisionIn(ctx context.Context, executor remediation.PersistenceTX, planPublicID string, expectedRowVersion uint64, decision *remediation.Approval) error {
	planPublicID = strings.TrimSpace(planPublicID)
	if executor == nil || decision == nil || expectedRowVersion == 0 {
		return remediation.ErrInvalidArgument
	}
	if _, err := uuid.Parse(planPublicID); err != nil {
		return remediation.ErrInvalidArgument
	}
	owner, err := loadV3Plan(ctx, executor, "p.public_id = ?", false, planPublicID)
	if err != nil {
		return err
	}
	incident, err := lockV3RemediationIncident(ctx, executor, owner.IncidentID)
	if err != nil {
		return err
	}
	plan, err := loadV3Plan(ctx, executor, "p.public_id = ?", true, planPublicID)
	if err != nil {
		return err
	}
	if incident.cycleNo != plan.CycleNo || incident.publicID != plan.IncidentPublicID {
		return fmt.Errorf("%w: Decision does not belong to the current Incident cycle", remediation.ErrConflict)
	}
	if plan.Status != remediation.PlanAwaitingApproval {
		existing, existingErr := loadV3Decision(ctx, executor, plan.ID, true)
		if existingErr == nil && sameV3Decision(*existing, *decision) {
			*decision = *existing
			return nil
		}
		if existingErr != nil && !errors.Is(existingErr, remediation.ErrNotFound) {
			return existingErr
		}
		return fmt.Errorf("%w: V3 Plan already has a terminal Decision", remediation.ErrConflict)
	}
	if plan.RowVersion != expectedRowVersion || incident.status != "awaiting_approval" || incident.version != plan.IncidentVersion+1 {
		return fmt.Errorf("%w: stale V3 Plan or Incident phase", remediation.ErrConflict)
	}
	var databaseNow time.Time
	if err := executor.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
		return fmt.Errorf("read Decision database time: %w", err)
	}
	if err := remediation.ValidateV3DecisionBinding(*plan, *decision, databaseNow.UTC()); err != nil {
		return err
	}

	result, err := executor.ExecContext(ctx, `INSERT INTO remediation_decisions (
    public_id, domain_schema_version, decision_schema_version, incident_id,
    cycle_no, plan_id, plan_version, decision, actor_provider, actor_login,
    actor_role, reason, request_id, request_authenticated_at, expires_at,
    approved_hash_schema_version, approved_plan_hash, approved_base_sha,
    approved_post_image_hash, approved_tree_hash, approved_patch_hash,
    approved_policy_hash, approved_verification_hash, approved_evidence_set_hash,
    created_at
) VALUES (?, 3, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.PublicID, decision.DecisionSchemaVersion, decision.IncidentID,
		decision.CycleNo, decision.PlanID, decision.PlanVersion, decision.Decision,
		decision.ActorProvider, decision.Actor, decision.Role, decision.Reason,
		decision.RequestID, decision.RequestAuthenticatedAt.UTC(), decision.ExpiresAt.UTC(),
		decision.ApprovedHashSchemaVersion, decision.ApprovedPlanHash,
		decision.ApprovedBaseSHA, decision.ApprovedPostImageHash,
		decision.ApprovedTreeHash, decision.ApprovedPatchHash,
		decision.ApprovedPolicyHash, decision.ApprovedVerificationHash,
		decision.ApprovedEvidenceSetHash, decision.CreatedAt.UTC(),
	)
	if err != nil {
		if isMySQLDuplicate(err) {
			existing, loadErr := loadV3Decision(ctx, executor, plan.ID, true)
			if loadErr == nil && sameV3Decision(*existing, *decision) {
				*decision = *existing
				return nil
			}
			if loadErr != nil && !errors.Is(loadErr, remediation.ErrNotFound) {
				return loadErr
			}
			return fmt.Errorf("%w: duplicate V3 Decision identity", remediation.ErrConflict)
		}
		return classifyV3RemediationError(err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return fmt.Errorf("read V3 Decision ID: %w", err)
	}
	decision.ID = uint64(id)
	nextStatus := remediation.PlanApproved
	if decision.Decision == remediation.DecisionRejected {
		nextStatus = remediation.PlanRejected
	}
	updated, err := executor.ExecContext(ctx, `UPDATE remediation_plans
SET status = ?, v3_status = ?, row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ?
  AND row_version = ? AND v3_status = 'awaiting_approval'`,
		nextStatus, nextStatus, plan.ID, plan.CycleNo, expectedRowVersion)
	if err != nil {
		return classifyV3RemediationError(err)
	}
	affected, err := updated.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: stale V3 Plan Decision write", remediation.ErrConflict)
	}
	return nil
}

func (r *V3RemediationRepository) GetDecision(ctx context.Context, planPublicID string) (*remediation.Approval, error) {
	if r == nil || r.db == nil {
		return nil, remediation.ErrInvalidArgument
	}
	planPublicID = strings.TrimSpace(planPublicID)
	if _, err := uuid.Parse(planPublicID); err != nil {
		return nil, remediation.ErrInvalidArgument
	}
	plan, err := r.GetPlan(ctx, planPublicID)
	if err != nil {
		return nil, err
	}
	decision, err := loadV3Decision(ctx, r.db, plan.ID, false)
	if err != nil {
		return nil, err
	}
	if err := remediation.ValidateV3DecisionBinding(*plan, *decision, decision.CreatedAt); err != nil {
		return nil, err
	}
	return decision, nil
}

type v3IncidentFence struct {
	publicID    string
	cycleNo     uint64
	version     uint64
	status      string
	databaseNow time.Time
}

func lockV3RemediationIncident(ctx context.Context, executor remediation.PersistenceTX, incidentID uint64) (v3IncidentFence, error) {
	var result v3IncidentFence
	err := executor.QueryRowContext(ctx, `SELECT public_id, cycle_no, version, v3_status, NOW(6)
FROM incidents WHERE id = ? AND domain_schema_version = 3 FOR UPDATE`, incidentID).
		Scan(&result.publicID, &result.cycleNo, &result.version, &result.status, &result.databaseNow)
	if err != nil {
		return result, classifyV3RemediationError(err)
	}
	return result, nil
}

type v3AgentRunFence struct {
	id                      uint64
	status                  string
	expectedIncidentVersion uint64
	diagnosisHash           string
}

func resolveV3AgentRun(ctx context.Context, executor remediation.PersistenceTX, publicID string, incidentID, cycleNo uint64, lock bool) (v3AgentRunFence, error) {
	query := `SELECT id, v3_status, expected_incident_version, final_diagnosis
FROM agent_runs
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`
	if lock {
		query += " FOR UPDATE"
	}
	var result v3AgentRunFence
	var diagnosisJSON []byte
	err := executor.QueryRowContext(ctx, query, publicID, incidentID, cycleNo).
		Scan(&result.id, &result.status, &result.expectedIncidentVersion, &diagnosisJSON)
	if err != nil {
		return result, classifyV3RemediationError(err)
	}
	var diagnosis struct {
		DiagnosisHash string `json:"diagnosis_hash"`
	}
	if len(diagnosisJSON) > 32*1024 || json.Unmarshal(diagnosisJSON, &diagnosis) != nil || len(diagnosis.DiagnosisHash) != 64 {
		return result, fmt.Errorf("%w: AgentRun has no valid final diagnosis", remediation.ErrConflict)
	}
	result.diagnosisHash = diagnosis.DiagnosisHash
	return result, nil
}

func validateV3PlanEvidence(ctx context.Context, executor remediation.PersistenceTX, plan *remediation.RemediationPlan) error {
	for _, binding := range plan.EvidenceBindings {
		var contentHash sql.NullString
		var valid, truncated bool
		err := executor.QueryRowContext(ctx, `SELECT content_hash, valid, truncated
FROM evidence_items
WHERE public_id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
FOR SHARE`, binding.ID, plan.IncidentID, plan.CycleNo).Scan(&contentHash, &valid, &truncated)
		if err != nil {
			return classifyV3RemediationError(err)
		}
		if !contentHash.Valid || contentHash.String != binding.ContentHash || !valid || truncated {
			return fmt.Errorf("%w: Evidence ownership or content hash mismatch", remediation.ErrConflict)
		}
	}
	return nil
}

func findV3PlanByIdentity(ctx context.Context, executor remediation.PersistenceTX, plan *remediation.RemediationPlan, lock bool) (*remediation.RemediationPlan, error) {
	return loadV3Plan(ctx, executor, `(p.public_id = ? OR
    (p.incident_id = ? AND p.plan_version = ?) OR
    (p.incident_id = ? AND p.cycle_no = ? AND p.v3_status IN ('awaiting_approval','approved')))`,
		lock, plan.PublicID, plan.IncidentID, plan.PlanVersion, plan.IncidentID, plan.CycleNo)
}

func loadV3Plan(ctx context.Context, executor remediation.PersistenceTX, predicate string, lock bool, args ...any) (*remediation.RemediationPlan, error) {
	query := v3PlanSelect + `
WHERE p.domain_schema_version = 3 AND p.plan_content_schema_version IS NOT NULL AND ` + predicate + `
ORDER BY (p.public_id = ?) DESC, p.id DESC LIMIT 1`
	publicID := ""
	if len(args) > 0 {
		publicID, _ = args[0].(string)
	}
	args = append(args, publicID)
	if lock {
		query += " FOR UPDATE"
	}
	var plan remediation.RemediationPlan
	var legacyStatus, v3Status string
	var targetJSON, parametersJSON, evidenceReferencesJSON []byte
	var manifestJSON, policyJSON, verificationJSON, evidenceBindingsJSON []byte
	err := executor.QueryRowContext(ctx, query, args...).Scan(
		&plan.ID, &plan.PublicID, &plan.IncidentID, &plan.IncidentPublicID,
		&plan.DomainSchemaVersion, &plan.CycleNo, &plan.IncidentVersion,
		&plan.CreatedByAgentRunID, &plan.DiagnosisHash, &plan.PlanVersion,
		&plan.PlanHash, &legacyStatus, &v3Status, &plan.OperationType,
		&plan.TargetRepository, &plan.TargetBaseRevision, &plan.TargetBaseBranch,
		&plan.LastKnownGoodRevision, &plan.BaseBlobSHA, &plan.FileMode,
		&plan.TargetPath, &targetJSON, &plan.TargetFieldRef, &parametersJSON,
		&evidenceReferencesJSON, &plan.RiskLevel, &plan.PolicySnapshotHash,
		&plan.ExpectedBeforeHash, &plan.ExpectedPostImageHash, &plan.ExpectedTreeHash,
		&plan.ProposedPatchHash, &manifestJSON, &plan.BoundedDiff, &plan.PostImage,
		&plan.PatchSummary, &plan.RollbackPlan, &plan.ValidationPlan,
		&plan.PolicyVersion, &policyJSON, &verificationJSON,
		&plan.VerificationPlanHash, &evidenceBindingsJSON, &plan.EvidenceSetHash,
		&plan.HashSchemaVersion, &plan.CanonicalPlanHash,
		&plan.PlanContentSchemaVersion, &plan.RowVersion, &plan.CreatedAt,
		&plan.UpdatedAt, &plan.ExpiresAt,
	)
	if err != nil {
		return nil, classifyV3RemediationError(err)
	}
	plan.Status = remediation.PlanStatus(v3Status)
	plan.CanonicalChangeManifest, err = canonicalizeStoredV3JSON(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: decode persisted V3 change manifest", remediation.ErrInvalidArgument)
	}
	plan.PolicySnapshot, err = canonicalizeStoredV3JSON(policyJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: decode persisted V3 policy", remediation.ErrInvalidArgument)
	}
	plan.VerificationPlan, err = canonicalizeStoredV3JSON(verificationJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: decode persisted V3 verification plan", remediation.ErrInvalidArgument)
	}
	var target remediation.TargetResource
	if json.Unmarshal(targetJSON, &target) != nil || json.Unmarshal(parametersJSON, &plan.Parameters) != nil || plan.Parameters.Target != target || json.Unmarshal(evidenceReferencesJSON, &plan.EvidenceReferences) != nil || json.Unmarshal(evidenceBindingsJSON, &plan.EvidenceBindings) != nil {
		return nil, fmt.Errorf("%w: decode persisted V3 plan JSON", remediation.ErrInvalidArgument)
	}
	if err := remediation.ValidateV3Plan(plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func canonicalizeStoredV3JSON(raw []byte) (json.RawMessage, error) {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil, remediation.ErrInvalidArgument
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(canonical), nil
}

func sameV3PlanContent(left, right remediation.RemediationPlan) bool {
	return left.IncidentID == right.IncidentID && left.IncidentPublicID == right.IncidentPublicID &&
		left.CycleNo == right.CycleNo && left.IncidentVersion == right.IncidentVersion &&
		left.PlanVersion == right.PlanVersion && left.CanonicalPlanHash == right.CanonicalPlanHash &&
		left.ExpectedPostImageHash == right.ExpectedPostImageHash && left.CreatedByAgentRunID == right.CreatedByAgentRunID
}

const v3DecisionSelect = `SELECT id, public_id, domain_schema_version,
    decision_schema_version, incident_id, cycle_no, plan_id, plan_version,
    decision, actor_provider, actor_login, actor_role, reason, request_id,
    request_authenticated_at, expires_at, approved_hash_schema_version,
    approved_plan_hash, approved_base_sha, approved_post_image_hash,
    approved_tree_hash, approved_patch_hash, approved_policy_hash,
    approved_verification_hash, approved_evidence_set_hash, created_at
FROM remediation_decisions WHERE plan_id = ?`

func loadV3Decision(ctx context.Context, executor remediation.PersistenceTX, planID uint64, lock bool) (*remediation.Approval, error) {
	query := v3DecisionSelect
	if lock {
		query += " FOR UPDATE"
	}
	var decision remediation.Approval
	err := executor.QueryRowContext(ctx, query, planID).Scan(
		&decision.ID, &decision.PublicID, &decision.DomainSchemaVersion,
		&decision.DecisionSchemaVersion, &decision.IncidentID, &decision.CycleNo,
		&decision.PlanID, &decision.PlanVersion, &decision.Decision,
		&decision.ActorProvider, &decision.Actor, &decision.Role, &decision.Reason,
		&decision.RequestID, &decision.RequestAuthenticatedAt, &decision.ExpiresAt,
		&decision.ApprovedHashSchemaVersion, &decision.ApprovedPlanHash,
		&decision.ApprovedBaseSHA, &decision.ApprovedPostImageHash,
		&decision.ApprovedTreeHash, &decision.ApprovedPatchHash,
		&decision.ApprovedPolicyHash, &decision.ApprovedVerificationHash,
		&decision.ApprovedEvidenceSetHash, &decision.CreatedAt,
	)
	if err != nil {
		return nil, classifyV3RemediationError(err)
	}
	return &decision, nil
}

func sameV3Decision(left, right remediation.Approval) bool {
	return left.PublicID == right.PublicID && left.DomainSchemaVersion == right.DomainSchemaVersion &&
		left.DecisionSchemaVersion == right.DecisionSchemaVersion && left.IncidentID == right.IncidentID &&
		left.CycleNo == right.CycleNo && left.PlanID == right.PlanID && left.PlanVersion == right.PlanVersion &&
		left.Decision == right.Decision && left.ActorProvider == right.ActorProvider && left.Actor == right.Actor &&
		left.Role == right.Role && left.Reason == right.Reason && left.RequestID == right.RequestID &&
		left.RequestAuthenticatedAt.Equal(right.RequestAuthenticatedAt) && left.ExpiresAt.Equal(right.ExpiresAt) &&
		left.ApprovedHashSchemaVersion == right.ApprovedHashSchemaVersion && left.ApprovedPlanHash == right.ApprovedPlanHash &&
		left.ApprovedBaseSHA == right.ApprovedBaseSHA && left.ApprovedPostImageHash == right.ApprovedPostImageHash &&
		left.ApprovedTreeHash == right.ApprovedTreeHash && left.ApprovedPatchHash == right.ApprovedPatchHash &&
		left.ApprovedPolicyHash == right.ApprovedPolicyHash && left.ApprovedVerificationHash == right.ApprovedVerificationHash &&
		left.ApprovedEvidenceSetHash == right.ApprovedEvidenceSetHash && left.CreatedAt.Equal(right.CreatedAt)
}

func classifyV3RemediationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return remediation.ErrNotFound
	}
	var mysqlErr *drivermysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1062:
			return fmt.Errorf("%w: duplicate V3 remediation identity", remediation.ErrConflict)
		case 1451, 1452:
			return fmt.Errorf("%w: V3 remediation ownership constraint", remediation.ErrConflict)
		case 1406, 3819:
			return fmt.Errorf("%w: V3 remediation persistence constraint", remediation.ErrInvalidArgument)
		}
	}
	return err
}

func isMySQLDuplicate(err error) bool {
	var mysqlErr *drivermysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
