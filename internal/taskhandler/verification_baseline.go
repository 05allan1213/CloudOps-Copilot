package taskhandler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const postDeliveryBaselineObservationSchema = 1

type baselinePromotionContract struct {
	PlanPublicID         string
	PlanStatus           string
	PlanV3Status         string
	Operation            string
	Repository           string
	BaseBranch           string
	LastKnownGood        string
	TargetPath           string
	TargetResource       remediation.TargetResource
	ExpectedConfigHash   string
	PostImageBytes       int
	VerificationPlan     json.RawMessage
	VerificationPlanHash string
	CanonicalPlanHash    string

	ChangePublicID       string
	ChangeRepository     string
	PRNumber             int64
	ChangeStatus         string
	ChangeV3Status       string
	CIStatus             string
	PRState              string
	MergedRevision       string
	TargetRevision       string
	DetectedRevision     string
	ArgoApplication      string
	ArgoProject          string
	ArgoSyncStatus       string
	ArgoOperationPhase   string
	ArgoHealthStatus     string
	ResourceHealth       json.RawMessage
	Cluster              string
	Environment          string
	Namespace            string
	WorkloadKind         string
	WorkloadName         string
	DeploymentGeneration int64
	ObservedGeneration   int64
	RolloutRevision      string
	DesiredReplicas      int
	UpdatedReplicas      int
	AvailableReplicas    int
	UnavailableReplicas  int
	DeliveryCompletedAt  time.Time

	DeliveryEvidence map[DeliveryObservationKind]baselineDeliveryEvidenceProof
}

type baselineDeliveryEvidenceProof struct {
	ID          string                  `json:"id"`
	Kind        DeliveryObservationKind `json:"kind"`
	ContentHash string                  `json:"content_hash"`
	CollectedAt time.Time               `json:"collected_at"`
}

type baselineVerificationSampleProof struct {
	CheckID           string                 `json:"check_id"`
	CheckType         verification.CheckType `json:"check_type"`
	SourceIdentity    string                 `json:"source_identity"`
	SampleID          string                 `json:"sample_id"`
	SampleSequence    int                    `json:"sample_sequence"`
	SampleContentHash string                 `json:"sample_content_hash"`
	Observed          json.RawMessage        `json:"observed"`
	SourceReference   string                 `json:"source_reference"`
	ReasonCode        string                 `json:"reason_code"`
	WindowStartAt     time.Time              `json:"window_start_at"`
	WindowEndAt       time.Time              `json:"window_end_at"`
	SampledAt         time.Time              `json:"sampled_at"`
}

type baselinePromotionObservationEnvelope struct {
	SchemaVersion     int                               `json:"schema_version"`
	VerificationRunID string                            `json:"verification_run_id"`
	ProfileID         string                            `json:"profile_id"`
	ProfileHash       string                            `json:"profile_hash"`
	Category          string                            `json:"category"`
	Checks            []baselineVerificationSampleProof `json:"checks,omitempty"`
	DeliveryEvidence  []baselineDeliveryEvidenceProof   `json:"delivery_evidence,omitempty"`
	Details           any                               `json:"details"`
}

func promotePassingDeploymentBaseline(
	ctx context.Context,
	tx asyncjob.DBTX,
	store VerificationBaselineStore,
	task asyncjob.Task,
	snapshot VerificationAdvanceSnapshot,
	commonStart *time.Time,
	verifiedAt time.Time,
) error {
	if snapshot.TriggerType == "no_change_signal" {
		if snapshot.RemediationPlanID != 0 || snapshot.ChangeRequestID != 0 ||
			snapshot.ProfileID != verification.NoChangeProfileID || snapshot.Run.Plan.TriggerType != "no_change" {
			return fmt.Errorf("%w: no-change VerificationRun has remediation identity", asyncjob.ErrInvalidMutation)
		}
		return nil
	}
	if snapshot.TriggerType != "post_delivery" || snapshot.Run.Plan.TriggerType != "post_delivery" ||
		snapshot.RemediationPlanID == 0 || snapshot.ChangeRequestID == 0 ||
		snapshot.ProfileID != verification.GoldenRequiredEnvProfileID || store == nil || tx == nil {
		return fmt.Errorf("%w: post-delivery baseline promotion identity is incomplete", asyncjob.ErrInvalidMutation)
	}
	if snapshot.ProfileHash != snapshot.Run.Plan.ProfileHash || snapshot.ProfileID != snapshot.Run.Plan.ProfileID ||
		snapshot.SourceRevision != snapshot.Run.Plan.SourceRevision || snapshot.ImageDigest != snapshot.Run.Plan.ImageDigest ||
		snapshot.GitOpsRevision != snapshot.Run.Plan.GitOpsRevision || snapshot.Run.TargetRevision != snapshot.GitOpsRevision {
		return fmt.Errorf("%w: post-delivery baseline promotion differs from the frozen VerificationRun", asyncjob.ErrInvalidMutation)
	}
	if commonStart == nil || commonStart.IsZero() || verifiedAt.IsZero() ||
		verifiedAt.UTC().Sub(commonStart.UTC()) < verification.V3CommonStabilityWindow {
		return fmt.Errorf("%w: post-delivery baseline lacks the passing common window", asyncjob.ErrInvalidMutation)
	}

	contract, err := loadBaselinePromotionContract(ctx, tx, task, snapshot, verifiedAt)
	if err != nil {
		return err
	}
	target, err := lockBaselinePromotionTarget(ctx, tx, snapshot, contract)
	if err != nil {
		return err
	}
	proofs, err := loadBaselineVerificationProofs(ctx, tx, task, snapshot, commonStart, verifiedAt)
	if err != nil {
		return err
	}
	promotion, err := buildPromotedBaselineSnapshot(snapshot, contract, target, proofs, verifiedAt)
	if err != nil {
		return err
	}
	if err := promotion.Finalize(); err != nil {
		return fmt.Errorf("%w: promoted DeploymentBaseline snapshot: %v", asyncjob.ErrInvalidMutation, err)
	}
	if _, err := store.ActivateIn(ctx, tx, promotion); err != nil {
		if errors.Is(err, baseline.ErrConflict) || errors.Is(err, baseline.ErrSuperseded) {
			return fmt.Errorf("%w: promoted DeploymentBaseline conflicts with immutable history: %v", asyncjob.ErrInvalidMutation, err)
		}
		return err
	}
	return nil
}

func loadBaselinePromotionContract(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, verifiedAt time.Time) (baselinePromotionContract, error) {
	var (
		contract                  baselinePromotionContract
		targetResource, postImage []byte
		verificationPlan          []byte
		resourceHealth            []byte
		deliveryCompleted         sql.NullTime
	)
	err := tx.QueryRowContext(ctx, `
SELECT p.public_id, p.status, p.v3_status, p.operation_type, p.target_repository,
       p.target_base_branch, p.last_known_good_sha, p.target_path,
       CAST(p.target_resource_json AS CHAR), p.expected_post_image_hash, p.post_image,
       CAST(p.verification_plan_json AS CHAR), p.verification_plan_hash, p.canonical_plan_hash,
       cr.public_id, cr.repository, cr.pr_number, cr.status, cr.v3_status, cr.ci_status, cr.pr_state,
       cr.merged_commit_sha, cr.target_revision, cr.detected_revision,
       cr.argocd_application, cr.argocd_project, cr.argocd_sync_status,
       cr.argocd_operation_phase, cr.argocd_health_status,
       CAST(cr.resource_health_json AS CHAR), cr.cluster, cr.environment, cr.namespace,
       cr.workload_kind, cr.workload_name, cr.deployment_generation,
       cr.observed_generation, cr.rollout_revision, cr.desired_replicas,
       cr.updated_replicas, cr.available_replicas, cr.unavailable_replicas,
       cr.delivery_completed_at
FROM remediation_plans p
JOIN change_requests cr
  ON cr.plan_id = p.id AND cr.incident_id = p.incident_id
 AND cr.cycle_no = p.cycle_no AND cr.domain_schema_version = 3
WHERE p.id = ? AND cr.id = ? AND p.incident_id = ? AND p.cycle_no = ?
  AND p.domain_schema_version = 3
FOR SHARE`, snapshot.RemediationPlanID, snapshot.ChangeRequestID, task.IncidentID, task.CycleNo).Scan(
		&contract.PlanPublicID, &contract.PlanStatus, &contract.PlanV3Status, &contract.Operation,
		&contract.Repository, &contract.BaseBranch, &contract.LastKnownGood, &contract.TargetPath,
		&targetResource, &contract.ExpectedConfigHash, &postImage, &verificationPlan,
		&contract.VerificationPlanHash, &contract.CanonicalPlanHash, &contract.ChangePublicID,
		&contract.ChangeRepository, &contract.PRNumber, &contract.ChangeStatus, &contract.ChangeV3Status, &contract.CIStatus,
		&contract.PRState, &contract.MergedRevision, &contract.TargetRevision,
		&contract.DetectedRevision, &contract.ArgoApplication, &contract.ArgoProject,
		&contract.ArgoSyncStatus, &contract.ArgoOperationPhase, &contract.ArgoHealthStatus,
		&resourceHealth, &contract.Cluster, &contract.Environment, &contract.Namespace,
		&contract.WorkloadKind, &contract.WorkloadName, &contract.DeploymentGeneration,
		&contract.ObservedGeneration, &contract.RolloutRevision, &contract.DesiredReplicas,
		&contract.UpdatedReplicas, &contract.AvailableReplicas, &contract.UnavailableReplicas,
		&deliveryCompleted,
	)
	if err != nil {
		return baselinePromotionContract{}, err
	}
	if _, err := uuid.Parse(contract.PlanPublicID); err != nil {
		return baselinePromotionContract{}, fmt.Errorf("%w: remediation Plan public identity is invalid", asyncjob.ErrInvalidMutation)
	}
	if _, err := uuid.Parse(contract.ChangePublicID); err != nil {
		return baselinePromotionContract{}, fmt.Errorf("%w: ChangeRequest public identity is invalid", asyncjob.ErrInvalidMutation)
	}
	if json.Unmarshal(targetResource, &contract.TargetResource) != nil || !json.Valid(resourceHealth) ||
		!json.Valid(verificationPlan) || len(postImage) == 0 || len(postImage) > remediation.MaxV3PostImageBytes {
		return baselinePromotionContract{}, fmt.Errorf("%w: post-delivery Plan or delivery payload is malformed", asyncjob.ErrInvalidMutation)
	}
	canonicalVerificationPlan, err := canonicalBaselineJSON(verificationPlan)
	if err != nil {
		return baselinePromotionContract{}, fmt.Errorf("%w: persisted remediation VerificationPlan is malformed", asyncjob.ErrInvalidMutation)
	}
	contract.ResourceHealth = append(json.RawMessage(nil), resourceHealth...)
	contract.VerificationPlan = append(json.RawMessage(nil), canonicalVerificationPlan...)
	contract.PostImageBytes = len(postImage)
	if contract.PlanV3Status != "consumed" || (contract.PlanStatus != "approved" && contract.PlanStatus != "consumed") ||
		contract.Operation != string(remediation.OperationRestoreRequiredEnv) || !validSHA256Text(contract.ExpectedConfigHash) ||
		sha256Hex(postImage) != contract.ExpectedConfigHash || !validSHA256Text(contract.VerificationPlanHash) ||
		sha256Hex(canonicalVerificationPlan) != contract.VerificationPlanHash || !validSHA256Text(contract.CanonicalPlanHash) {
		return baselinePromotionContract{}, fmt.Errorf("%w: immutable remediation Plan binding is invalid", asyncjob.ErrInvalidMutation)
	}
	frozenPlan, err := json.Marshal(snapshot.Run.Plan)
	if err != nil || !jsonVerificationEqual(canonicalVerificationPlan, frozenPlan) {
		return baselinePromotionContract{}, fmt.Errorf("%w: VerificationRun differs from its remediation Plan", asyncjob.ErrInvalidMutation)
	}
	if contract.ChangeStatus != "delivered" || contract.ChangeV3Status != "delivered" ||
		contract.CIStatus != "passing" || (contract.PRState != "closed" && contract.PRState != "merged") ||
		contract.PRNumber <= 0 || contract.MergedRevision != contract.TargetRevision ||
		contract.TargetRevision != contract.DetectedRevision || contract.TargetRevision != snapshot.GitOpsRevision ||
		!strings.EqualFold(contract.ArgoSyncStatus, "Synced") || !strings.EqualFold(contract.ArgoOperationPhase, "Succeeded") ||
		!strings.EqualFold(contract.ArgoHealthStatus, "Healthy") || contract.DeploymentGeneration <= 0 ||
		contract.ObservedGeneration < contract.DeploymentGeneration || contract.DesiredReplicas <= 0 ||
		contract.UpdatedReplicas != contract.DesiredReplicas || contract.AvailableReplicas != contract.DesiredReplicas ||
		contract.UnavailableReplicas != 0 || strings.TrimSpace(contract.RolloutRevision) == "" ||
		!deliveryCompleted.Valid || deliveryCompleted.Time.After(verifiedAt) {
		return baselinePromotionContract{}, fmt.Errorf("%w: delivered ChangeRequest projection is incomplete", asyncjob.ErrInvalidMutation)
	}
	contract.DeliveryCompletedAt = deliveryCompleted.Time.UTC().Truncate(time.Microsecond)
	if len(snapshot.Run.Plan.Checks) == 0 {
		return baselinePromotionContract{}, fmt.Errorf("%w: frozen VerificationPlan has no target", asyncjob.ErrInvalidMutation)
	}
	subject := snapshot.Run.Plan.Checks[0].Subject
	if contract.Repository != subject.Repository || contract.ChangeRepository != contract.Repository || contract.PRNumber != subject.PullRequest ||
		contract.ArgoApplication != subject.ArgoApplication || contract.ArgoProject != subject.ArgoProject ||
		contract.Cluster != subject.Cluster || contract.Environment != subject.Environment ||
		contract.Namespace != subject.Namespace || !strings.EqualFold(contract.WorkloadKind, subject.WorkloadKind) ||
		contract.WorkloadName != subject.WorkloadName || contract.TargetResource.Kind != "Deployment" ||
		contract.TargetResource.Name != contract.WorkloadName || contract.TargetResource.Container == "" ||
		(contract.TargetResource.Namespace != "" && contract.TargetResource.Namespace != contract.Namespace) {
		return baselinePromotionContract{}, fmt.Errorf("%w: remediation, delivery, and Verification target identities differ", asyncjob.ErrInvalidMutation)
	}
	evidence, err := loadBaselineDeliveryEvidence(ctx, tx, task, snapshot, contract.ChangePublicID)
	if err != nil {
		return baselinePromotionContract{}, err
	}
	contract.DeliveryEvidence = evidence
	return contract, nil
}

func loadBaselineDeliveryEvidence(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, changePublicID string) (returnProofs map[DeliveryObservationKind]baselineDeliveryEvidenceProof, retErr error) {
	rows, err := tx.QueryContext(ctx, `
SELECT public_id, CAST(facts_json AS CHAR), content_hash, producer_dedupe_key, collected_at
FROM evidence_items
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
  AND producer_type IN ('delivery.observe','delivery_observation') AND valid = TRUE
ORDER BY collected_at, id
LIMIT 257`, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close baseline delivery evidence rows")
	latest := make(map[DeliveryObservationKind]baselineDeliveryEvidenceProof, 4)
	count := 0
	for rows.Next() {
		count++
		var id, contentHash, producerKey string
		var facts []byte
		var collectedAt time.Time
		if err := rows.Scan(&id, &facts, &contentHash, &producerKey, &collectedAt); err != nil {
			return nil, err
		}
		var envelope struct {
			Kind                 DeliveryObservationKind `json:"kind"`
			SourceRevision       string                  `json:"source_revision"`
			ImageDigest          string                  `json:"image_digest"`
			TargetGitOpsRevision string                  `json:"target_gitops_revision"`
			FailureCode          string                  `json:"failure_code"`
		}
		canonicalFacts, canonicalErr := canonicalBaselineJSON(facts)
		if canonicalErr != nil || json.Unmarshal(canonicalFacts, &envelope) != nil || !validDeliveryObservationKind(envelope.Kind) ||
			sha256Hex(canonicalFacts) != contentHash || producerKey != hashCanonical("delivery.observe", changePublicID, string(envelope.Kind), contentHash) {
			continue
		}
		if envelope.SourceRevision != snapshot.SourceRevision || envelope.ImageDigest != snapshot.ImageDigest ||
			envelope.TargetGitOpsRevision != snapshot.GitOpsRevision || envelope.FailureCode != "" {
			return nil, fmt.Errorf("%w: delivery Evidence identity differs from the passing VerificationRun", asyncjob.ErrInvalidMutation)
		}
		if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("%w: delivery Evidence public identity is invalid", asyncjob.ErrInvalidMutation)
		}
		latest[envelope.Kind] = baselineDeliveryEvidenceProof{
			ID: id, Kind: envelope.Kind, ContentHash: contentHash,
			CollectedAt: collectedAt.UTC().Truncate(time.Microsecond),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count > 256 {
		return nil, fmt.Errorf("%w: delivery Evidence set exceeds the promotion bound", asyncjob.ErrInvalidMutation)
	}
	for _, kind := range []DeliveryObservationKind{DeliveryObservePullRequest, DeliveryObserveCI, DeliveryObserveArgo, DeliveryObserveRollout} {
		if _, ok := latest[kind]; !ok {
			return nil, fmt.Errorf("%w: current ChangeRequest lacks %s delivery Evidence", asyncjob.ErrInvalidMutation, kind)
		}
	}
	return latest, nil
}

func lockBaselinePromotionTarget(ctx context.Context, tx asyncjob.DBTX, snapshot VerificationAdvanceSnapshot, contract baselinePromotionContract) (baseline.Target, error) {
	target := baseline.Target{
		Cluster: contract.Cluster, Environment: contract.Environment, Namespace: contract.Namespace,
		WorkloadKind: "Deployment", WorkloadName: contract.WorkloadName,
		ContainerName: contract.TargetResource.Container, Repository: contract.Repository,
		BaseBranch: contract.BaseBranch, TargetPath: contract.TargetPath,
	}.Normalized()
	targetHash, err := target.IdentityHash()
	if err != nil {
		return baseline.Target{}, fmt.Errorf("%w: promoted baseline target: %v", asyncjob.ErrInvalidMutation, err)
	}
	var (
		storedTargetHash, sourceRevision, imageDigest, gitopsRevision, configHash string
		domainVersion, schemaVersion                                              uint16
		rowVersion                                                                uint64
		verifiedAt                                                                time.Time
	)
	err = tx.QueryRowContext(ctx, `
SELECT domain_schema_version, baseline_schema_version, row_version,
       target_identity_hash, source_revision, image_digest, gitops_revision,
       config_hash, verified_at
FROM deployment_baselines
WHERE status = 'active' AND cluster = ? AND environment = ? AND namespace = ?
  AND workload_kind = 'Deployment' AND workload_name = ? AND container_name = ?
  AND repository = ? AND base_branch = ? AND target_path = ?
LIMIT 1 FOR UPDATE`, target.Cluster, target.Environment, target.Namespace,
		target.WorkloadName, target.ContainerName, target.Repository, target.BaseBranch,
		target.TargetPath).Scan(&domainVersion, &schemaVersion, &rowVersion, &storedTargetHash,
		&sourceRevision, &imageDigest, &gitopsRevision, &configHash, &verifiedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return baseline.Target{}, fmt.Errorf("%w: post-delivery promotion requires one active DeploymentBaseline", asyncjob.ErrInvalidMutation)
		}
		return baseline.Target{}, err
	}
	if domainVersion != baseline.DomainSchemaVersion || schemaVersion != baseline.BaselineSchemaVersion || rowVersion == 0 ||
		storedTargetHash != targetHash || verifiedAt.IsZero() || sourceRevision != snapshot.SourceRevision ||
		imageDigest != snapshot.ImageDigest {
		return baseline.Target{}, fmt.Errorf("%w: active DeploymentBaseline target or immutable image identity differs", asyncjob.ErrInvalidMutation)
	}
	oldBaseline := gitopsRevision == contract.LastKnownGood
	idempotentReplay := gitopsRevision == snapshot.GitOpsRevision && configHash == contract.ExpectedConfigHash
	if !oldBaseline && !idempotentReplay {
		return baseline.Target{}, fmt.Errorf("%w: active DeploymentBaseline is neither the approved restore source nor this promotion", asyncjob.ErrInvalidMutation)
	}
	return target, nil
}

func loadBaselineVerificationProofs(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, commonStart *time.Time, verifiedAt time.Time) (returnProofs map[verification.CheckType]baselineVerificationSampleProof, retErr error) {
	rows, err := tx.QueryContext(ctx, `
SELECT c.public_id, c.check_type, c.status, c.required_check, c.source_identity,
       COALESCE(CAST(c.observed_json AS CHAR), '{}'), c.source_reference,
       c.failure_reason, c.attempt_count, c.consecutive_success_since,
       s.public_id, s.sample_sequence, s.status, CAST(s.observed_json AS CHAR),
       s.source_reference, s.reason_code, s.window_start_at, s.window_end_at,
       s.sampled_at, s.content_hash
FROM verification_checks c
JOIN verification_samples s
  ON s.verification_check_id = c.id AND s.verification_run_id = c.verification_run_id
 AND s.incident_id = c.incident_id AND s.cycle_no = c.cycle_no
 AND s.sample_sequence = (
       SELECT MAX(s2.sample_sequence) FROM verification_samples s2
       WHERE s2.verification_check_id = c.id AND s2.verification_run_id = c.verification_run_id
         AND s2.incident_id = c.incident_id AND s2.cycle_no = c.cycle_no)
WHERE c.verification_run_id = ? AND c.incident_id = ? AND c.cycle_no = ?
ORDER BY c.id`, task.SubjectID, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close baseline verification proof rows")
	expected := make(map[verification.CheckType]string, len(snapshot.Run.Plan.Checks))
	for _, spec := range snapshot.Run.Plan.Checks {
		expected[spec.Type] = spec.SourceIdentity
	}
	proofs := make(map[verification.CheckType]baselineVerificationSampleProof, len(expected))
	var latestSuccess time.Time
	for rows.Next() {
		var (
			checkID, checkType, checkStatus, sourceIdentity, checkSource, checkReason string
			checkObserved                                                             []byte
			required                                                                  bool
			attempt, sequence                                                         int
			successSince, windowStart, windowEnd                                      sql.NullTime
			sampleID, sampleStatus, sampleSource, sampleReason, contentHash           string
			sampleObserved                                                            []byte
			sampledAt                                                                 time.Time
		)
		if err := rows.Scan(&checkID, &checkType, &checkStatus, &required, &sourceIdentity,
			&checkObserved, &checkSource, &checkReason, &attempt, &successSince,
			&sampleID, &sequence, &sampleStatus, &sampleObserved, &sampleSource,
			&sampleReason, &windowStart, &windowEnd, &sampledAt, &contentHash); err != nil {
			return nil, err
		}
		typ := verification.CheckType(checkType)
		expectedSource, ok := expected[typ]
		if !ok {
			return nil, fmt.Errorf("%w: passing VerificationRun contains unexpected check %s", asyncjob.ErrInvalidMutation, typ)
		}
		if _, duplicate := proofs[typ]; duplicate {
			return nil, fmt.Errorf("%w: passing VerificationRun contains duplicate check %s", asyncjob.ErrInvalidMutation, typ)
		}
		if _, err := uuid.Parse(checkID); err != nil {
			return nil, fmt.Errorf("%w: VerificationCheck public identity is invalid", asyncjob.ErrInvalidMutation)
		}
		if _, err := uuid.Parse(sampleID); err != nil {
			return nil, fmt.Errorf("%w: VerificationSample public identity is invalid", asyncjob.ErrInvalidMutation)
		}
		canonicalCheckObserved, checkObservedErr := canonicalVerificationObservation(checkObserved)
		canonicalSampleObserved, sampleObservedErr := canonicalVerificationObservation(sampleObserved)
		if !required || checkStatus != string(verification.CheckPassed) || sampleStatus != string(verification.SamplePassed) {
			return nil, invalidBaselineProof(typ, "check or latest sample is not required and passed")
		}
		if attempt <= 0 || attempt != sequence {
			return nil, invalidBaselineProof(typ, "latest sample sequence differs from check attempt count")
		}
		if strings.TrimSpace(sourceIdentity) == "" || sourceIdentity != expectedSource || checkSource != sampleSource ||
			strings.TrimSpace(sampleReason) == "" || (checkReason != "" && checkReason != sampleReason) {
			return nil, invalidBaselineProof(typ, "check source or reason differs from its latest sample")
		}
		if checkObservedErr != nil || sampleObservedErr != nil || !jsonVerificationEqual(canonicalCheckObserved, canonicalSampleObserved) {
			return nil, invalidBaselineProof(typ, "check observation differs from its latest sample")
		}
		if !successSince.Valid || !windowStart.Valid || !windowEnd.Valid {
			return nil, invalidBaselineProof(typ, "successful sample window is missing")
		}
		successAt := successSince.Time.UTC().Truncate(time.Microsecond)
		windowStartedAt := windowStart.Time.UTC().Truncate(time.Microsecond)
		windowEndedAt := windowEnd.Time.UTC().Truncate(time.Microsecond)
		sampled := sampledAt.UTC().Truncate(time.Microsecond)
		if !successAt.Equal(windowStartedAt) || windowStartedAt.After(windowEndedAt) || windowEndedAt.After(sampled) ||
			sampled.After(verifiedAt.UTC().Truncate(time.Microsecond)) || successAt.After(commonStart.UTC().Truncate(time.Microsecond)) {
			return nil, invalidBaselineProof(typ, "successful sample window timestamps are inconsistent")
		}
		if windowEndedAt.Sub(windowStartedAt) < verification.V3CommonStabilityWindow {
			return nil, invalidBaselineProof(typ, "successful sample window is shorter than the frozen stability window")
		}
		if !validSHA256Text(contentHash) {
			return nil, invalidBaselineProof(typ, "latest sample content hash is invalid")
		}
		sample := verification.Sample{Status: verification.SampleStatus(sampleStatus), Observed: canonicalSampleObserved, SourceReference: sampleSource, ReasonCode: sampleReason}
		if hashVerificationSample(sample, verification.Check{PublicID: checkID}, sequence) != contentHash {
			return nil, fmt.Errorf("%w: VerificationSample %s content hash differs", asyncjob.ErrInvalidMutation, sampleID)
		}
		proofs[typ] = baselineVerificationSampleProof{
			CheckID: checkID, CheckType: typ, SourceIdentity: sourceIdentity,
			SampleID: sampleID, SampleSequence: sequence, SampleContentHash: contentHash,
			Observed: append(json.RawMessage(nil), canonicalSampleObserved...), SourceReference: sampleSource,
			ReasonCode: sampleReason, WindowStartAt: windowStart.Time.UTC().Truncate(time.Microsecond),
			WindowEndAt: windowEnd.Time.UTC().Truncate(time.Microsecond),
			SampledAt:   sampledAt.UTC().Truncate(time.Microsecond),
		}
		if successAt.After(latestSuccess) {
			latestSuccess = successAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(proofs) != len(expected) {
		return nil, fmt.Errorf("%w: passing VerificationRun has %d immutable check samples, want %d", asyncjob.ErrInvalidMutation, len(proofs), len(expected))
	}
	if !latestSuccess.Equal(commonStart.UTC().Truncate(time.Microsecond)) {
		return nil, fmt.Errorf("%w: persisted VerificationCheck windows do not reproduce the common window", asyncjob.ErrInvalidMutation)
	}
	return proofs, nil
}

func invalidBaselineProof(typ verification.CheckType, reason string) error {
	return fmt.Errorf("%w: passing VerificationCheck %s %s", asyncjob.ErrInvalidMutation, typ, reason)
}

func buildPromotedBaselineSnapshot(snapshot VerificationAdvanceSnapshot, contract baselinePromotionContract, target baseline.Target, proofs map[verification.CheckType]baselineVerificationSampleProof, verifiedAt time.Time) (baseline.Snapshot, error) {
	argoChecks := selectBaselineProofs(proofs, verification.CheckArgoExactRevision, verification.CheckArgoSyncSucceeded)
	kubernetesChecks := selectBaselineProofs(proofs, verification.CheckDeploymentObserved, verification.CheckDeploymentRolloutV3, verification.CheckWorkloadReady)
	alertChecks := selectBaselineProofs(proofs, verification.CheckIncidentAlertsResolved)
	metricChecks := selectBaselineProofs(proofs, verification.CheckMetricErrorRateBelow, verification.CheckMetricAvailabilityAbove)
	logChecks := selectBaselineProofs(proofs, verification.CheckLogRequiredEnvAbsent)
	traceChecks := selectBaselineProofs(proofs, verification.CheckTraceErrorRateBelow)

	argoDelivery := []baselineDeliveryEvidenceProof{contract.DeliveryEvidence[DeliveryObserveArgo]}
	kubernetesDelivery := []baselineDeliveryEvidenceProof{contract.DeliveryEvidence[DeliveryObserveRollout]}
	configDelivery := []baselineDeliveryEvidenceProof{
		contract.DeliveryEvidence[DeliveryObservePullRequest], contract.DeliveryEvidence[DeliveryObserveCI],
	}
	argoJSON, err := marshalBaselinePromotionObservation(snapshot, "argocd_revision", argoChecks, argoDelivery, map[string]any{
		"application": contract.ArgoApplication, "project": contract.ArgoProject,
		"target_revision": contract.TargetRevision, "detected_revision": contract.DetectedRevision,
		"sync_status": contract.ArgoSyncStatus, "operation_phase": contract.ArgoOperationPhase,
		"health_status": contract.ArgoHealthStatus, "resource_health": contract.ResourceHealth,
	})
	if err != nil {
		return baseline.Snapshot{}, err
	}
	kubernetesJSON, err := marshalBaselinePromotionObservation(snapshot, "kubernetes_readiness", kubernetesChecks, kubernetesDelivery, map[string]any{
		"cluster": contract.Cluster, "namespace": contract.Namespace, "workload_kind": contract.WorkloadKind,
		"workload_name": contract.WorkloadName, "generation": contract.DeploymentGeneration,
		"observed_generation": contract.ObservedGeneration, "rollout_revision": contract.RolloutRevision,
		"desired_replicas": contract.DesiredReplicas, "updated_replicas": contract.UpdatedReplicas,
		"available_replicas": contract.AvailableReplicas, "unavailable_replicas": contract.UnavailableReplicas,
		"source_revision": snapshot.SourceRevision, "image_digest": snapshot.ImageDigest,
	})
	if err != nil {
		return baseline.Snapshot{}, err
	}
	alertJSON, err := marshalBaselinePromotionObservation(snapshot, "alert_state", alertChecks, nil, map[string]any{"resolved": true})
	if err != nil {
		return baseline.Snapshot{}, err
	}
	metricJSON, err := marshalBaselinePromotionObservation(snapshot, "metric", metricChecks, nil, map[string]any{"golden_thresholds_satisfied": true})
	if err != nil {
		return baseline.Snapshot{}, err
	}
	logJSON, err := marshalBaselinePromotionObservation(snapshot, "log", logChecks, nil, map[string]any{"required_env_error_absent": true})
	if err != nil {
		return baseline.Snapshot{}, err
	}
	traceJSON, err := marshalBaselinePromotionObservation(snapshot, "trace", traceChecks, nil, map[string]any{"error_rate_below_threshold": true})
	if err != nil {
		return baseline.Snapshot{}, err
	}
	configJSON, err := marshalBaselinePromotionObservation(snapshot, "config_blob", nil, configDelivery, map[string]any{
		"remediation_plan_id": contract.PlanPublicID, "change_request_id": contract.ChangePublicID,
		"repository": contract.Repository, "base_branch": contract.BaseBranch, "path": contract.TargetPath,
		"revision": snapshot.GitOpsRevision, "content_hash": contract.ExpectedConfigHash,
		"bytes": contract.PostImageBytes, "canonical_plan_hash": contract.CanonicalPlanHash,
		"verification_plan_hash": contract.VerificationPlanHash,
	})
	if err != nil {
		return baseline.Snapshot{}, err
	}

	configObservation := baseline.Observation{
		Type: baseline.ObservationConfigBlob, SourceIdentity: "remediation-plan/" + contract.PlanPublicID,
		ObservedJSON: configJSON, ContentHash: contract.ExpectedConfigHash,
		ObservedAt: contract.DeliveryCompletedAt,
	}
	return baseline.Snapshot{
		Target: target, SourceRevision: snapshot.SourceRevision, ImageDigest: snapshot.ImageDigest,
		GitOpsRevision: snapshot.GitOpsRevision, ConfigHash: contract.ExpectedConfigHash,
		VerificationPolicyVersion: baseline.PostDeliveryPolicyVersion,
		VerifiedAt:                verifiedAt.UTC().Truncate(time.Microsecond),
		Observations: []baseline.Observation{
			baselinePromotionObservation(baseline.ObservationArgoRevision, snapshot.Run.PublicID, "argocd", argoJSON, latestBaselineProofTime(argoChecks, argoDelivery)),
			baselinePromotionObservation(baseline.ObservationKubernetesReadiness, snapshot.Run.PublicID, "kubernetes", kubernetesJSON, latestBaselineProofTime(kubernetesChecks, kubernetesDelivery)),
			baselinePromotionObservation(baseline.ObservationAlertState, snapshot.Run.PublicID, "alerts", alertJSON, latestBaselineProofTime(alertChecks, nil)),
			baselinePromotionObservation(baseline.ObservationMetric, snapshot.Run.PublicID, "metrics", metricJSON, latestBaselineProofTime(metricChecks, nil)),
			baselinePromotionObservation(baseline.ObservationLog, snapshot.Run.PublicID, "logs", logJSON, latestBaselineProofTime(logChecks, nil)),
			baselinePromotionObservation(baseline.ObservationTrace, snapshot.Run.PublicID, "traces", traceJSON, latestBaselineProofTime(traceChecks, nil)),
			configObservation,
		},
	}, nil
}

func selectBaselineProofs(proofs map[verification.CheckType]baselineVerificationSampleProof, types ...verification.CheckType) []baselineVerificationSampleProof {
	result := make([]baselineVerificationSampleProof, 0, len(types))
	for _, typ := range types {
		result = append(result, proofs[typ])
	}
	return result
}

func marshalBaselinePromotionObservation(snapshot VerificationAdvanceSnapshot, category string, checks []baselineVerificationSampleProof, delivery []baselineDeliveryEvidenceProof, details any) (json.RawMessage, error) {
	payload, err := json.Marshal(baselinePromotionObservationEnvelope{
		SchemaVersion: postDeliveryBaselineObservationSchema, VerificationRunID: snapshot.Run.PublicID,
		ProfileID: snapshot.ProfileID, ProfileHash: snapshot.ProfileHash, Category: category,
		Checks: checks, DeliveryEvidence: delivery, Details: details,
	})
	if err != nil || len(payload) == 0 || len(payload) > baseline.MaxObservationBytes || !json.Valid(payload) {
		return nil, fmt.Errorf("%w: promoted baseline %s observation is unbounded", asyncjob.ErrInvalidMutation, category)
	}
	return payload, nil
}

func baselinePromotionObservation(typ baseline.ObservationType, runPublicID, source string, observed json.RawMessage, observedAt time.Time) baseline.Observation {
	return baseline.Observation{
		Type: typ, SourceIdentity: "verification/" + runPublicID + "/" + source,
		ObservedJSON: observed, ObservedAt: observedAt.UTC().Truncate(time.Microsecond),
	}
}

func latestBaselineProofTime(checks []baselineVerificationSampleProof, delivery []baselineDeliveryEvidenceProof) time.Time {
	var latest time.Time
	for _, proof := range checks {
		if proof.SampledAt.After(latest) {
			latest = proof.SampledAt
		}
	}
	for _, proof := range delivery {
		if proof.CollectedAt.After(latest) {
			latest = proof.CollectedAt
		}
	}
	return latest.UTC().Truncate(time.Microsecond)
}

func canonicalBaselineJSON(raw []byte) ([]byte, error) {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, errors.New("invalid JSON")
	}
	return json.Marshal(value)
}

func canonicalVerificationObservation(raw []byte) ([]byte, error) {
	var observation verification.Observation
	if len(raw) == 0 || json.Unmarshal(raw, &observation) != nil {
		return nil, errors.New("invalid Verification observation")
	}
	canonical, err := json.Marshal(observation)
	if err != nil || !jsonVerificationEqual(raw, canonical) {
		return nil, errors.New("verification observation has unknown fields")
	}
	return canonical, nil
}
