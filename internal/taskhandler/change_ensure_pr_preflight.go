package taskhandler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	changecontract "github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

var errChangeApprovalExpired = errors.New("change.ensure_pr approval expired")

const changeEnsurePlanSelect = `SELECT
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
    p.row_version, p.created_at, p.updated_at, p.expires_at,
    i.v3_status, i.version
FROM remediation_plans p
JOIN incidents i ON i.id = p.incident_id
  AND i.domain_schema_version = 3 AND i.cycle_no = p.cycle_no
JOIN agent_runs ar ON ar.id = p.created_by_agent_run_id
  AND ar.incident_id = p.incident_id AND ar.cycle_no = p.cycle_no
  AND ar.domain_schema_version = 3
WHERE p.id = ? AND p.domain_schema_version = 3 AND p.plan_content_schema_version = 2`

const changeEnsureDecisionSelect = `SELECT
    id, public_id, domain_schema_version, decision_schema_version,
    incident_id, cycle_no, plan_id, plan_version, decision,
    actor_provider, actor_login, actor_role, reason, request_id,
    request_authenticated_at, expires_at, approved_hash_schema_version,
    approved_plan_hash, approved_base_sha, approved_post_image_hash,
    approved_tree_hash, approved_patch_hash, approved_policy_hash,
    approved_verification_hash, approved_evidence_set_hash, created_at
FROM remediation_decisions
WHERE plan_id = ? AND incident_id = ? AND cycle_no = ?`

func (s *mysqlChangeEnsurePRStore) loadPlan(ctx context.Context, queryer changeQueryer, planID uint64, lock bool) (changePlanSnapshot, error) {
	var snapshot changePlanSnapshot
	var v3Status string
	var targetJSON, parametersJSON, evidenceReferencesJSON []byte
	var manifestJSON, policyJSON, verificationJSON, evidenceBindingsJSON []byte
	query := changeEnsurePlanSelect
	if lock {
		query += " FOR UPDATE"
	}
	if err := queryer.QueryRowContext(ctx, query, planID).Scan(
		&snapshot.Plan.ID, &snapshot.Plan.PublicID, &snapshot.Plan.IncidentID, &snapshot.Plan.IncidentPublicID,
		&snapshot.Plan.DomainSchemaVersion, &snapshot.Plan.CycleNo, &snapshot.Plan.IncidentVersion,
		&snapshot.Plan.CreatedByAgentRunID, &snapshot.Plan.DiagnosisHash, &snapshot.Plan.PlanVersion,
		&snapshot.Plan.PlanHash, &snapshot.LegacyPlanStatus, &v3Status, &snapshot.Plan.OperationType,
		&snapshot.Plan.TargetRepository, &snapshot.Plan.TargetBaseRevision, &snapshot.Plan.TargetBaseBranch,
		&snapshot.Plan.LastKnownGoodRevision, &snapshot.Plan.BaseBlobSHA, &snapshot.Plan.FileMode,
		&snapshot.Plan.TargetPath, &targetJSON, &snapshot.Plan.TargetFieldRef, &parametersJSON,
		&evidenceReferencesJSON, &snapshot.Plan.RiskLevel, &snapshot.Plan.PolicySnapshotHash,
		&snapshot.Plan.ExpectedBeforeHash, &snapshot.Plan.ExpectedPostImageHash, &snapshot.Plan.ExpectedTreeHash,
		&snapshot.Plan.ProposedPatchHash, &manifestJSON, &snapshot.Plan.BoundedDiff, &snapshot.Plan.PostImage,
		&snapshot.Plan.PatchSummary, &snapshot.Plan.RollbackPlan, &snapshot.Plan.ValidationPlan,
		&snapshot.Plan.PolicyVersion, &policyJSON, &verificationJSON,
		&snapshot.Plan.VerificationPlanHash, &evidenceBindingsJSON, &snapshot.Plan.EvidenceSetHash,
		&snapshot.Plan.HashSchemaVersion, &snapshot.Plan.CanonicalPlanHash,
		&snapshot.Plan.PlanContentSchemaVersion, &snapshot.Plan.RowVersion, &snapshot.Plan.CreatedAt,
		&snapshot.Plan.UpdatedAt, &snapshot.Plan.ExpiresAt,
		&snapshot.IncidentStatus, &snapshot.IncidentVersion,
	); err != nil {
		return changePlanSnapshot{}, err
	}
	snapshot.Plan.Status = remediation.PlanStatus(v3Status)
	var err error
	if snapshot.Plan.CanonicalChangeManifest, err = canonicalizeChangePlanJSON(manifestJSON); err != nil {
		return snapshot, fmt.Errorf("%w: decode persisted V3 change manifest", remediation.ErrInvalidArgument)
	}
	if snapshot.Plan.PolicySnapshot, err = canonicalizeChangePlanJSON(policyJSON); err != nil {
		return snapshot, fmt.Errorf("%w: decode persisted V3 remediation policy", remediation.ErrInvalidArgument)
	}
	if snapshot.Plan.VerificationPlan, err = canonicalizeChangePlanJSON(verificationJSON); err != nil {
		return snapshot, fmt.Errorf("%w: decode persisted V3 verification plan", remediation.ErrInvalidArgument)
	}
	var target remediation.TargetResource
	if json.Unmarshal(targetJSON, &target) != nil ||
		json.Unmarshal(parametersJSON, &snapshot.Plan.Parameters) != nil || snapshot.Plan.Parameters.Target != target ||
		json.Unmarshal(evidenceReferencesJSON, &snapshot.Plan.EvidenceReferences) != nil ||
		json.Unmarshal(evidenceBindingsJSON, &snapshot.Plan.EvidenceBindings) != nil {
		return snapshot, fmt.Errorf("%w: decode persisted V3 plan JSON", remediation.ErrInvalidArgument)
	}

	decisionQuery := changeEnsureDecisionSelect
	if lock {
		decisionQuery += " FOR UPDATE"
	}
	if err := queryer.QueryRowContext(ctx, decisionQuery, snapshot.Plan.ID, snapshot.Plan.IncidentID, snapshot.Plan.CycleNo).Scan(
		&snapshot.Decision.ID, &snapshot.Decision.PublicID, &snapshot.Decision.DomainSchemaVersion,
		&snapshot.Decision.DecisionSchemaVersion, &snapshot.Decision.IncidentID, &snapshot.Decision.CycleNo,
		&snapshot.Decision.PlanID, &snapshot.Decision.PlanVersion, &snapshot.Decision.Decision,
		&snapshot.Decision.ActorProvider, &snapshot.Decision.Actor, &snapshot.Decision.Role, &snapshot.Decision.Reason,
		&snapshot.Decision.RequestID, &snapshot.Decision.RequestAuthenticatedAt, &snapshot.Decision.ExpiresAt,
		&snapshot.Decision.ApprovedHashSchemaVersion, &snapshot.Decision.ApprovedPlanHash,
		&snapshot.Decision.ApprovedBaseSHA, &snapshot.Decision.ApprovedPostImageHash,
		&snapshot.Decision.ApprovedTreeHash, &snapshot.Decision.ApprovedPatchHash,
		&snapshot.Decision.ApprovedPolicyHash, &snapshot.Decision.ApprovedVerificationHash,
		&snapshot.Decision.ApprovedEvidenceSetHash, &snapshot.Decision.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return snapshot, fmt.Errorf("%w: approved Decision is absent from the current cycle", asyncjob.ErrPolicyViolation)
		}
		return snapshot, err
	}
	if err := remediation.ValidateV3Plan(snapshot.Plan); err != nil {
		return snapshot, err
	}
	// Reconciliation remains approval-bound after expiry. Using the immutable
	// Decision creation time validates every approved hash without requiring
	// the approval to still be live at the time of a read-only reconciliation.
	if err := remediation.ValidateV3ApprovalBinding(snapshot.Plan, snapshot.Decision, snapshot.Decision.CreatedAt.UTC()); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func canonicalizeChangePlanJSON(raw []byte) (json.RawMessage, error) {
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

func (s *mysqlChangeEnsurePRStore) validateDurablePreflight(
	ctx context.Context,
	queryer changeQueryer,
	snapshot changePlanSnapshot,
	now time.Time,
	policyHash string,
	lock bool,
	expectedPlanStatus remediation.PlanStatus,
	expectedIncidentStatus string,
) error {
	plan := snapshot.Plan
	now = now.UTC()
	if snapshot.IncidentStatus != expectedIncidentStatus || plan.Status != expectedPlanStatus ||
		snapshot.LegacyPlanStatus != string(remediation.PlanApproved) ||
		plan.OperationType != remediation.OperationRestoreRequiredEnv ||
		plan.IncidentID == 0 || plan.CycleNo == 0 || snapshot.Decision.Decision != remediation.DecisionApproved {
		return fmt.Errorf("%w: Incident, Plan, or Decision is stale for the current delivery cycle", asyncjob.ErrPolicyViolation)
	}
	expired := !now.Before(plan.ExpiresAt.UTC()) || !now.Before(snapshot.Decision.ExpiresAt.UTC())
	if err := s.validateCurrentDiagnosisEvidence(ctx, queryer, plan, lock); err != nil {
		return err
	}
	if err := validateCurrentChangePolicy(plan, policyHash); err != nil {
		return err
	}
	if err := remediation.ValidateV3Plan(plan); err != nil {
		return err
	}
	bindingTime := now
	if expired {
		bindingTime = snapshot.Decision.CreatedAt.UTC()
	}
	if err := remediation.ValidateV3ApprovalBinding(plan, snapshot.Decision, bindingTime); err != nil {
		return err
	}
	if expired {
		return fmt.Errorf("%w: %w", asyncjob.ErrPolicyViolation, errChangeApprovalExpired)
	}
	return nil
}

func validateCurrentChangePolicy(plan remediation.RemediationPlan, policyHash string) error {
	if plan.PolicySnapshotHash != policyHash {
		return fmt.Errorf("%w: current remediation policy no longer matches the approved Plan", asyncjob.ErrPolicyViolation)
	}
	return nil
}

func (s *mysqlChangeEnsurePRStore) validateCurrentDiagnosisEvidence(ctx context.Context, queryer changeQueryer, plan remediation.RemediationPlan, lock bool) error {
	query := `SELECT id, public_id, status, v3_status, expected_incident_version,
       final_diagnosis, completed_at
FROM agent_runs
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`
	if lock {
		query += " FOR UPDATE"
	}
	var runID, expectedIncidentVersion uint64
	var runPublicID, legacyStatus, runStatus string
	var finalDiagnosis []byte
	var completedAt sql.NullTime
	if err := queryer.QueryRowContext(ctx, query, plan.CreatedByAgentRunID, plan.IncidentID, plan.CycleNo).Scan(
		&runID, &runPublicID, &legacyStatus, &runStatus, &expectedIncidentVersion, &finalDiagnosis, &completedAt,
	); err != nil {
		return err
	}
	if runPublicID != plan.CreatedByAgentRunID || legacyStatus != "COMPLETED" || runStatus != "completed" ||
		!completedAt.Valid || expectedIncidentVersion != plan.IncidentVersion {
		return fmt.Errorf("%w: creating AgentRun is no longer the completed current-cycle Diagnosis", asyncjob.ErrPolicyViolation)
	}
	diagnosis, err := decodeRemediationDiagnosis(finalDiagnosis)
	if err != nil {
		return err
	}
	if diagnosis.DiagnosisHash != plan.DiagnosisHash || !slices.Equal(diagnosis.EvidenceIDs, plan.EvidenceReferences) {
		return fmt.Errorf("%w: approved Plan no longer binds the creating Diagnosis and Evidence set", asyncjob.ErrPolicyViolation)
	}
	bindingByID := make(map[string]remediation.EvidenceBinding, len(plan.EvidenceBindings))
	for _, binding := range plan.EvidenceBindings {
		bindingByID[binding.ID] = binding
	}
	facts := make([]agent.EvidenceFact, 0, len(diagnosis.EvidenceIDs)*2)
	factByID := make(map[string]agent.EvidenceFact)
	for _, evidenceID := range diagnosis.EvidenceIDs {
		binding, ok := bindingByID[evidenceID]
		if !ok {
			return fmt.Errorf("%w: Diagnosis Evidence is absent from the approved Plan", asyncjob.ErrPolicyViolation)
		}
		evidenceQuery := `SELECT content_hash, result_hash, producer_type, agent_run_id,
       facts_json, valid, truncated
FROM evidence_items
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`
		if lock {
			evidenceQuery += " FOR UPDATE"
		}
		var contentHash, resultHash, producerType string
		var agentRunID sql.NullInt64
		var factsJSON []byte
		var valid, truncated bool
		if err := queryer.QueryRowContext(ctx, evidenceQuery, evidenceID, plan.IncidentID, plan.CycleNo).Scan(
			&contentHash, &resultHash, &producerType, &agentRunID, &factsJSON, &valid, &truncated,
		); err != nil {
			return err
		}
		if !agentRunID.Valid || uint64(agentRunID.Int64) != runID ||
			(producerType != "agent_step" && producerType != "system_enrichment") ||
			!valid || truncated || !validSHA256Text(contentHash) || resultHash != contentHash || contentHash != binding.ContentHash {
			return fmt.Errorf("%w: Diagnosis Evidence is no longer a verified current-cycle observation", asyncjob.ErrPolicyViolation)
		}
		var envelope storedEvidenceEnvelope
		if len(factsJSON) == 0 || len(factsJSON) > remediation.MaxV3PostImageBytes ||
			json.Unmarshal(factsJSON, &envelope) != nil || envelope.SchemaVersion != 1 ||
			envelope.ContentHash != contentHash || envelope.Truncated || envelope.Status != agent.CollectionAvailable {
			return fmt.Errorf("%w: Diagnosis Evidence envelope is malformed", asyncjob.ErrPolicyViolation)
		}
		for _, fact := range envelope.Facts {
			if fact.ID == "" || fact.EvidenceID != evidenceID || fact.IncidentID != plan.IncidentPublicID || fact.CycleNo != plan.CycleNo {
				return fmt.Errorf("%w: Diagnosis Evidence fact ownership is invalid", asyncjob.ErrPolicyViolation)
			}
			if _, duplicate := factByID[fact.ID]; duplicate {
				return fmt.Errorf("%w: Diagnosis Evidence contains duplicate fact identity", asyncjob.ErrPolicyViolation)
			}
			factByID[fact.ID] = fact
			facts = append(facts, fact)
		}
	}
	for _, fact := range facts {
		for _, parent := range fact.DerivedFrom {
			input, ok := factByID[parent]
			if !ok || input.CollectionStatus != agent.CollectionAvailable || input.Integrity != "verified" ||
				input.Freshness != "fresh" || input.Completeness != "complete" || input.ClaimUse == "forbidden" || input.Truncated {
				return fmt.Errorf("%w: derived Diagnosis fact has no current verified input", asyncjob.ErrPolicyViolation)
			}
		}
	}
	if lock {
		if err := validateApprovedEvidenceCurrentForUpdate(ctx, queryer, plan.IncidentID, plan.CycleNo, plan.EvidenceBindings); err != nil {
			return err
		}
	} else if err := validateApprovedEvidenceCurrent(ctx, queryer, plan.IncidentID, plan.CycleNo, plan.EvidenceBindings); err != nil {
		return err
	}
	return validateCurrentChangeDiagnosis(diagnosis, plan.IncidentPublicID, uint32(plan.CycleNo), facts, s.claimPolicy)
}

func validateCurrentChangeDiagnosis(stored agent.DiagnosisRecord, incidentPublicID string, cycleNo uint32, facts []agent.EvidenceFact, policy agent.ClaimPolicy) error {
	sufficiency, err := agent.EvaluateSufficiency(agent.SufficiencyInput{
		IncidentID: incidentPublicID, CycleNo: uint64(cycleNo), Facts: facts, Policy: policy,
	})
	if err != nil || sufficiency.Outcome != agent.SufficiencyReady {
		return fmt.Errorf("%w: current Evidence no longer satisfies the configured ClaimPolicy", asyncjob.ErrPolicyViolation)
	}
	validated, err := validateV3Diagnosis(stored.Candidate, investigationSnapshot{
		IncidentPublicID: incidentPublicID, Task: asyncjob.Task{CycleNo: cycleNo}, Facts: facts,
	}, policy, sufficiency)
	if err != nil {
		return fmt.Errorf("%w: final Diagnosis no longer validates: %v", asyncjob.ErrPolicyViolation, err)
	}
	storedJSON, storedErr := json.Marshal(stored)
	validatedJSON, validatedErr := json.Marshal(validated)
	if storedErr != nil || validatedErr != nil || !bytes.Equal(storedJSON, validatedJSON) {
		return fmt.Errorf("%w: final Diagnosis content/hash diverges from current Evidence or ClaimPolicy", asyncjob.ErrPolicyViolation)
	}
	return nil
}

func (s *mysqlChangeEnsurePRStore) validateGitPreflight(ctx context.Context, plan remediation.RemediationPlan) error {
	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return err
	}
	facts, err := s.git.ReadRestoreFacts(externalCtx, remediation.ExactGitRestoreQuery{
		Repository: plan.TargetRepository, BaseBranch: plan.TargetBaseBranch,
		TargetPath: plan.TargetPath, BaselineRevision: plan.LastKnownGoodRevision,
	})
	cancel()
	if err != nil {
		return err
	}
	return validateExactGitChangePreflight(plan, facts)
}

func validateExactGitChangePreflight(plan remediation.RemediationPlan, facts remediation.ExactGitRestoreFacts) error {
	if err := remediation.ValidateExactGitRestoreFacts(facts); err != nil {
		return err
	}
	if facts.Repository != plan.TargetRepository || facts.BaseBranch != plan.TargetBaseBranch ||
		facts.TargetPath != plan.TargetPath || facts.BaselineRevision != plan.LastKnownGoodRevision ||
		facts.BaseRevision != plan.TargetBaseRevision || facts.BaseBlobSHA != plan.BaseBlobSHA ||
		facts.FileMode != plan.FileMode || remediation.HashBytes(facts.CurrentContent) != plan.ExpectedBeforeHash {
		return fmt.Errorf("%w: current Git base ref/blob no longer matches the approved Plan", remediation.ErrDrift)
	}
	expectedTree, err := remediation.ExpectedGitTreeHash(facts, plan.PostImage)
	if err != nil {
		return err
	}
	if expectedTree != plan.ExpectedTreeHash {
		return fmt.Errorf("%w: current base tree plus approved post-image no longer matches the approved tree", remediation.ErrDrift)
	}
	return nil
}

func isTerminalChangePreflight(err error) bool {
	return errors.Is(err, asyncjob.ErrPolicyViolation) ||
		errors.Is(err, remediation.ErrApprovalMismatch) ||
		errors.Is(err, remediation.ErrDrift) ||
		errors.Is(err, remediation.ErrConflict) ||
		errors.Is(err, remediation.ErrForbidden) ||
		errors.Is(err, remediation.ErrInvalidArgument) ||
		errors.Is(err, remediation.ErrNotFound) ||
		errors.Is(err, changecontract.ErrConflict) ||
		errors.Is(err, changecontract.ErrPermission) ||
		errors.Is(err, changecontract.ErrNotAllowed) ||
		errors.Is(err, changecontract.ErrInvalidArgument) ||
		errors.Is(err, changecontract.ErrNotFound)
}
