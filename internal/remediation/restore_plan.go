package remediation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	HashSchemaVersion               = 1
	PlanContentSchemaVersion        = 2
	DecisionSchemaVersion           = 1
	RestoreRequiredEnvPolicyVersion = "restore-required-env-policy/v1"
	MaxPlanDiffBytes                = 64 * 1024
	MaxPostImageBytes               = 256 * 1024
	MaxPolicyBytes                  = 16 * 1024
	MaxEvidenceBindings             = 40
)

var (
	gitObjectIDPattern = regexp.MustCompile(`^[a-f0-9]{40}(?:[a-f0-9]{24})?$`)
	envKeyPattern      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	repositoryPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// RemediationHint is the only remediation authority the model may return.
// In particular, it has no repository, branch, patch, or environment value.
type RemediationHint struct {
	OperationHint         OperationType `json:"operation_hint"`
	TargetFieldRef        string        `json:"target_field_ref"`
	LastKnownGoodEvidence string        `json:"last_known_good_evidence_id"`
	SupportingEvidenceIDs []string      `json:"supporting_evidence_ids"`
}

func DecodeRemediationHint(payload []byte) (RemediationHint, error) {
	if len(payload) == 0 || len(payload) > MaxPlannerJSONBytes {
		return RemediationHint{}, fmt.Errorf("%w: remediation hint size", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var hint RemediationHint
	if err := decoder.Decode(&hint); err != nil {
		return RemediationHint{}, fmt.Errorf("%w: remediation hint schema: %v", ErrInvalidArgument, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return RemediationHint{}, fmt.Errorf("%w: remediation hint has multiple JSON values", ErrInvalidArgument)
	}
	if hint.OperationHint != OperationRestoreRequiredEnv || strings.TrimSpace(hint.TargetFieldRef) == "" || len(hint.SupportingEvidenceIDs) == 0 {
		return RemediationHint{}, fmt.Errorf("%w: only restore_required_env with bounded references is allowed", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(hint.LastKnownGoodEvidence); err != nil {
		return RemediationHint{}, fmt.Errorf("%w: invalid baseline evidence ID", ErrInvalidArgument)
	}
	seen := map[string]struct{}{}
	for _, id := range append([]string{hint.LastKnownGoodEvidence}, hint.SupportingEvidenceIDs...) {
		if _, err := uuid.Parse(id); err != nil {
			return RemediationHint{}, fmt.Errorf("%w: invalid evidence ID", ErrInvalidArgument)
		}
		if _, ok := seen[id]; ok {
			return RemediationHint{}, fmt.Errorf("%w: duplicate evidence ID", ErrInvalidArgument)
		}
		seen[id] = struct{}{}
	}
	return hint, nil
}

type RestoreEnvPolicy struct {
	Version             string `json:"version"`
	Repository          string `json:"repository"`
	BaseBranch          string `json:"base_branch"`
	AllowedPath         string `json:"allowed_path"`
	APIVersion          string `json:"api_version"`
	Namespace           string `json:"namespace"`
	Workload            string `json:"workload"`
	Container           string `json:"container"`
	EnvKey              string `json:"env_key"`
	MaxDiffBytes        int    `json:"max_diff_bytes"`
	MaxPostImageBytes   int    `json:"max_post_image_bytes"`
	VerificationVersion string `json:"verification_version"`
}

type RestoreEnvCompileRequest struct {
	IncidentPublicID      string
	IncidentID            uint64
	CycleNo               uint64
	IncidentVersion       uint64
	CreatedByAgentRunID   string
	DiagnosisHash         string
	Repository            string
	BaseBranch            string
	BaseRevision          string
	LastKnownGoodRevision string
	TargetPath            string
	BaseBlobSHA           string
	ExpectedTreeHash      string
	FileMode              string
	Target                TargetResource
	EnvKey                string
	CurrentContent        []byte
	BaselineContent       []byte
	Policy                RestoreEnvPolicy
	VerificationPlan      json.RawMessage
	Evidence              []EvidenceBinding
	BaselineIsAncestor    bool
	CreatedAt             time.Time
	ExpiresAt             time.Time
	PlanVersion           int
}

// CompileRestoreRequiredEnv performs all deterministic checks and produces a
// complete immutable plan. The model never supplies the copied env node.
func CompileRestoreRequiredEnv(request RestoreEnvCompileRequest) (RemediationPlan, error) {
	if err := validateRestoreRequest(request); err != nil {
		return RemediationPlan{}, err
	}
	patch, err := RenderRestoreRequiredEnv(request.CurrentContent, request.BaselineContent, request.Target, request.EnvKey)
	if err != nil {
		return RemediationPlan{}, err
	}
	if len(patch.Diff) > request.Policy.MaxDiffBytes || len(patch.Content) > request.Policy.MaxPostImageBytes {
		return RemediationPlan{}, fmt.Errorf("%w: compiled patch exceeds policy bounds", ErrInvalidArgument)
	}
	policyBytes, err := canonicalJSON(request.Policy)
	if err != nil || len(policyBytes) > MaxPolicyBytes {
		return RemediationPlan{}, fmt.Errorf("%w: policy snapshot size", ErrInvalidArgument)
	}
	verificationBytes, err := canonicalJSON(request.VerificationPlan)
	if err != nil || len(verificationBytes) > 16*1024 {
		return RemediationPlan{}, fmt.Errorf("%w: verification plan size", ErrInvalidArgument)
	}
	manifest := canonicalChangeManifest{
		Path: request.TargetPath, BaseBlobSHA: strings.ToLower(request.BaseBlobSHA), FileMode: request.FileMode,
		PostImageHash: patch.PostImageHash,
	}
	manifestBytes, err := canonicalJSON(manifest)
	if err != nil {
		return RemediationPlan{}, err
	}
	patchHash := hashBytes(manifestBytes)
	evidence := append([]EvidenceBinding(nil), request.Evidence...)
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].ID == evidence[j].ID {
			return evidence[i].ContentHash < evidence[j].ContentHash
		}
		return evidence[i].ID < evidence[j].ID
	})
	evidenceSetHash, err := lengthPrefixedHash(evidenceBytes(evidence))
	if err != nil {
		return RemediationPlan{}, err
	}
	policyHash := hashBytes(policyBytes)
	verificationHash := hashBytes(verificationBytes)
	createdAt := normalizeTime(request.CreatedAt)
	expiresAt := normalizeTime(request.ExpiresAt)
	plan := RemediationPlan{
		PublicID: uuid.NewString(), IncidentID: request.IncidentID, IncidentPublicID: request.IncidentPublicID,
		PlanVersion: request.PlanVersion, PlanHash: "", Status: PlanAwaitingApproval,
		OperationType: OperationRestoreRequiredEnv, TargetRepository: request.Repository,
		TargetBaseRevision: strings.ToLower(request.BaseRevision), TargetPath: request.TargetPath,
		Parameters:         Parameters{Target: request.Target, ProposedValue: ProposedValue{}},
		EvidenceReferences: evidenceIDs(evidence), RiskLevel: RiskLow,
		PolicySnapshotHash: policyHash, ExpectedBeforeHash: patch.BeforeHash, ProposedPatchHash: patchHash,
		PatchSummary: patch.Summary, RollbackPlan: "No rollback operation is permitted; submit a new reviewed plan.",
		ValidationPlan: "Run golden-required-env/v1 deterministic checks after exact merged revision is observed.",
		RowVersion:     1, CreatedAt: createdAt, UpdatedAt: createdAt,
		CycleNo: request.CycleNo, IncidentVersion: request.IncidentVersion, CreatedByAgentRunID: request.CreatedByAgentRunID, DiagnosisHash: strings.ToLower(request.DiagnosisHash),
		HashSchemaVersion: HashSchemaVersion, PlanContentSchemaVersion: PlanContentSchemaVersion,
		LastKnownGoodRevision: strings.ToLower(request.LastKnownGoodRevision), TargetBaseBranch: request.BaseBranch,
		BaseBlobSHA: strings.ToLower(request.BaseBlobSHA), FileMode: request.FileMode,
		TargetFieldRef:        "spec.template.spec.containers[name=" + request.Target.Container + "].env[name=" + request.EnvKey + "]",
		ExpectedPostImageHash: patch.PostImageHash, ExpectedTreeHash: strings.ToLower(request.ExpectedTreeHash),
		CanonicalChangeManifest: manifestBytes, BoundedDiff: patch.Diff, PostImage: patch.Content,
		PolicyVersion: request.Policy.Version, PolicySnapshot: policyBytes, VerificationPlan: verificationBytes,
		VerificationPlanHash: verificationHash, EvidenceBindings: evidence, EvidenceSetHash: evidenceSetHash,
		ExpiresAt: expiresAt,
	}
	canonicalHash, err := CanonicalPlanHash(plan)
	if err != nil {
		return RemediationPlan{}, err
	}
	plan.CanonicalPlanHash, plan.PlanHash = canonicalHash, canonicalHash
	return plan, nil
}

type canonicalChangeManifest struct {
	Path          string `json:"path"`
	BaseBlobSHA   string `json:"base_blob_sha"`
	FileMode      string `json:"file_mode"`
	PostImageHash string `json:"post_image_hash"`
}

func validateRestoreRequest(request RestoreEnvCompileRequest) error {
	if strings.TrimSpace(request.IncidentPublicID) == "" || request.IncidentID == 0 || request.CycleNo == 0 || request.IncidentVersion == 0 || strings.TrimSpace(request.CreatedByAgentRunID) == "" || len(request.DiagnosisHash) != 64 || !isLowerHex(request.DiagnosisHash) || !request.BaselineIsAncestor {
		return fmt.Errorf("%w: incident, diagnosis, ancestor and agent identity are required", ErrInvalidArgument)
	}
	if !validObjectID(request.BaseRevision) || !validObjectID(request.LastKnownGoodRevision) || !validObjectID(request.BaseBlobSHA) || !validObjectID(request.ExpectedTreeHash) {
		return fmt.Errorf("%w: revision/blob/tree identity is invalid", ErrInvalidArgument)
	}
	if request.CreatedAt.IsZero() || request.ExpiresAt.IsZero() || !request.ExpiresAt.After(request.CreatedAt) {
		return fmt.Errorf("%w: plan expiry is invalid", ErrInvalidArgument)
	}
	if request.PlanVersion <= 0 || len(request.CurrentContent) == 0 || len(request.CurrentContent) > MaxPostImageBytes || len(request.BaselineContent) == 0 || len(request.BaselineContent) > MaxPostImageBytes {
		return fmt.Errorf("%w: plan content bounds are invalid", ErrInvalidArgument)
	}
	cleanPath := path.Clean(strings.TrimSpace(request.TargetPath))
	if !repositoryPattern.MatchString(request.Repository) || request.BaseBranch == "" || cleanPath == "." || cleanPath != request.TargetPath || strings.HasPrefix(cleanPath, "../") || sensitiveGitOpsPath(cleanPath) || request.FileMode != "100644" {
		return fmt.Errorf("%w: fixed GitOps identity is required", ErrInvalidArgument)
	}
	if request.Target.APIVersion != "apps/v1" || request.Target.Kind != "Deployment" || request.Target.Namespace == "" || request.Target.Name == "" || request.Target.Container == "" || !envKeyPattern.MatchString(request.EnvKey) {
		return fmt.Errorf("%w: restore target is not allowlisted", ErrInvalidArgument)
	}
	policy := request.Policy
	if policy.Version == "" || policy.Repository != request.Repository || policy.BaseBranch != request.BaseBranch || policy.AllowedPath != request.TargetPath || policy.APIVersion != request.Target.APIVersion || policy.Namespace != request.Target.Namespace || policy.Workload != request.Target.Name || policy.Container != request.Target.Container || policy.EnvKey != request.EnvKey || policy.VerificationVersion == "" {
		return fmt.Errorf("%w: policy snapshot does not bind target", ErrInvalidArgument)
	}
	if policy.MaxDiffBytes <= 0 || policy.MaxDiffBytes > MaxPlanDiffBytes || policy.MaxPostImageBytes <= 0 || policy.MaxPostImageBytes > MaxPostImageBytes {
		return fmt.Errorf("%w: policy bounds are invalid", ErrInvalidArgument)
	}
	if len(request.VerificationPlan) == 0 || !json.Valid(request.VerificationPlan) || len(request.Evidence) == 0 || len(request.Evidence) > MaxEvidenceBindings {
		return fmt.Errorf("%w: verification plan and evidence are required", ErrInvalidArgument)
	}
	seen := make(map[string]struct{}, len(request.Evidence))
	for _, evidence := range request.Evidence {
		if _, err := uuid.Parse(evidence.ID); err != nil || len(evidence.ContentHash) != 64 || !isLowerHex(evidence.ContentHash) {
			return fmt.Errorf("%w: evidence binding is invalid", ErrInvalidArgument)
		}
		if _, ok := seen[evidence.ID]; ok {
			return fmt.Errorf("%w: duplicate evidence binding", ErrInvalidArgument)
		}
		seen[evidence.ID] = struct{}{}
	}
	return nil
}

func CanonicalPlanHash(plan RemediationPlan) (string, error) {
	evidence := append([]EvidenceBinding(nil), plan.EvidenceBindings...)
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].ID == evidence[j].ID {
			return evidence[i].ContentHash < evidence[j].ContentHash
		}
		return evidence[i].ID < evidence[j].ID
	})
	fields := [][]byte{
		[]byte(fmt.Sprint(plan.HashSchemaVersion)), []byte(fmt.Sprint(plan.PlanContentSchemaVersion)), []byte(plan.IncidentPublicID), []byte(fmt.Sprint(plan.IncidentVersion)), []byte(fmt.Sprint(plan.CycleNo)), []byte(fmt.Sprint(plan.PlanVersion)),
		[]byte(plan.CreatedByAgentRunID), []byte(plan.DiagnosisHash), []byte(string(plan.OperationType)),
		[]byte(plan.TargetRepository), []byte(plan.TargetBaseBranch), []byte(plan.TargetBaseRevision), []byte(plan.LastKnownGoodRevision), []byte(plan.TargetPath),
		[]byte(plan.BaseBlobSHA), []byte(plan.FileMode), []byte(plan.TargetFieldRef), []byte(plan.ExpectedBeforeHash),
		[]byte(plan.ExpectedPostImageHash), []byte(plan.ExpectedTreeHash), []byte(plan.CanonicalChangeManifest), []byte(plan.ProposedPatchHash),
		[]byte(plan.BoundedDiff), []byte(plan.PatchSummary), []byte(plan.PolicyVersion), []byte(plan.PolicySnapshot), []byte(plan.PolicySnapshotHash), []byte(plan.VerificationPlan),
		[]byte(plan.VerificationPlanHash), []byte(plan.EvidenceSetHash), []byte(string(plan.RiskLevel)), []byte(plan.CreatedAt.UTC().Format(time.RFC3339Nano)),
		[]byte(plan.ExpiresAt.UTC().Format(time.RFC3339Nano)), evidenceBytes(evidence),
	}
	return lengthPrefixedHash(fields...)
}

// ValidatePlan verifies the complete immutable contract produced by the
// compiler. Persistence adapters call this before accepting a row and again
// after reading one back from durable storage.
func ValidatePlan(plan RemediationPlan) error {
	if plan.PlanContentSchemaVersion != PlanContentSchemaVersion || plan.HashSchemaVersion != HashSchemaVersion {
		return fmt.Errorf("%w: unsupported plan schema", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(plan.PublicID); err != nil {
		return fmt.Errorf("%w: invalid plan public ID", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(plan.IncidentPublicID); err != nil {
		return fmt.Errorf("%w: invalid incident public ID", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(plan.CreatedByAgentRunID); err != nil {
		return fmt.Errorf("%w: invalid creating AgentRun public ID", ErrInvalidArgument)
	}
	if plan.IncidentID == 0 || plan.IncidentVersion == 0 || plan.CycleNo == 0 || plan.PlanVersion <= 0 || plan.RowVersion == 0 || plan.OperationType != OperationRestoreRequiredEnv {
		return fmt.Errorf("%w: invalid plan identity", ErrInvalidArgument)
	}
	switch plan.Status {
	case PlanAwaitingApproval, PlanApproved, PlanRejected, PlanSuperseded, PlanCancelled, PlanConsumed, PlanInvalidated, PlanPolicyRejected:
	default:
		return fmt.Errorf("%w: invalid plan status", ErrInvalidArgument)
	}
	if plan.RiskLevel != RiskLow || plan.TargetRepository == "" || plan.TargetBaseBranch == "" || plan.TargetPath == "" || plan.TargetFieldRef == "" || plan.FileMode != "100644" {
		return fmt.Errorf("%w: invalid plan target", ErrInvalidArgument)
	}
	for _, objectID := range []string{plan.TargetBaseRevision, plan.LastKnownGoodRevision, plan.BaseBlobSHA, plan.ExpectedTreeHash} {
		if !validObjectID(objectID) {
			return fmt.Errorf("%w: invalid Git object identity", ErrInvalidArgument)
		}
	}
	for _, digest := range []string{
		plan.DiagnosisHash, plan.ExpectedBeforeHash, plan.ExpectedPostImageHash,
		plan.ProposedPatchHash, plan.PolicySnapshotHash, plan.VerificationPlanHash,
		plan.EvidenceSetHash, plan.CanonicalPlanHash, plan.PlanHash,
	} {
		if len(digest) != 64 || !isLowerHex(digest) {
			return fmt.Errorf("%w: invalid plan hash", ErrInvalidArgument)
		}
	}
	if plan.CanonicalPlanHash != plan.PlanHash {
		return fmt.Errorf("%w: canonical and compatibility plan hashes differ", ErrInvalidArgument)
	}
	if len(plan.PostImage) == 0 || len(plan.PostImage) > MaxPostImageBytes || hashBytes(plan.PostImage) != plan.ExpectedPostImageHash {
		return fmt.Errorf("%w: post-image hash or size mismatch", ErrInvalidArgument)
	}
	if len(plan.BoundedDiff) == 0 || len(plan.BoundedDiff) > MaxPlanDiffBytes {
		return fmt.Errorf("%w: bounded diff size", ErrInvalidArgument)
	}
	if len(plan.CanonicalChangeManifest) == 0 || len(plan.CanonicalChangeManifest) > 4096 || !json.Valid(plan.CanonicalChangeManifest) || hashBytes(plan.CanonicalChangeManifest) != plan.ProposedPatchHash {
		return fmt.Errorf("%w: canonical change manifest", ErrInvalidArgument)
	}
	var manifest canonicalChangeManifest
	if err := json.Unmarshal(plan.CanonicalChangeManifest, &manifest); err != nil || manifest.Path != plan.TargetPath || manifest.BaseBlobSHA != plan.BaseBlobSHA || manifest.FileMode != plan.FileMode || manifest.PostImageHash != plan.ExpectedPostImageHash {
		return fmt.Errorf("%w: canonical change manifest binding", ErrInvalidArgument)
	}
	if len(plan.PolicySnapshot) == 0 || len(plan.PolicySnapshot) > MaxPolicyBytes || !json.Valid(plan.PolicySnapshot) || plan.PolicyVersion == "" || hashBytes(plan.PolicySnapshot) != plan.PolicySnapshotHash {
		return fmt.Errorf("%w: policy snapshot binding", ErrInvalidArgument)
	}
	if len(plan.VerificationPlan) == 0 || len(plan.VerificationPlan) > 16*1024 || !json.Valid(plan.VerificationPlan) || hashBytes(plan.VerificationPlan) != plan.VerificationPlanHash {
		return fmt.Errorf("%w: verification plan binding", ErrInvalidArgument)
	}
	if len(plan.EvidenceBindings) == 0 || len(plan.EvidenceBindings) > MaxEvidenceBindings || len(plan.EvidenceReferences) != len(plan.EvidenceBindings) {
		return fmt.Errorf("%w: evidence binding count", ErrInvalidArgument)
	}
	evidence := append([]EvidenceBinding(nil), plan.EvidenceBindings...)
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].ID == evidence[j].ID {
			return evidence[i].ContentHash < evidence[j].ContentHash
		}
		return evidence[i].ID < evidence[j].ID
	})
	for index, binding := range evidence {
		if _, err := uuid.Parse(binding.ID); err != nil || len(binding.ContentHash) != 64 || !isLowerHex(binding.ContentHash) {
			return fmt.Errorf("%w: invalid evidence binding", ErrInvalidArgument)
		}
		if index > 0 && binding.ID == evidence[index-1].ID {
			return fmt.Errorf("%w: duplicate evidence binding", ErrInvalidArgument)
		}
		if plan.EvidenceReferences[index] != binding.ID || plan.EvidenceBindings[index] != binding {
			return fmt.Errorf("%w: evidence bindings are not canonical", ErrInvalidArgument)
		}
	}
	evidenceHash, err := lengthPrefixedHash(evidenceBytes(evidence))
	if err != nil || evidenceHash != plan.EvidenceSetHash {
		return fmt.Errorf("%w: evidence set hash mismatch", ErrInvalidArgument)
	}
	if plan.Parameters.Target.APIVersion == "" || plan.Parameters.Target.Kind != "Deployment" || plan.Parameters.Target.Namespace == "" || plan.Parameters.Target.Name == "" || plan.Parameters.Target.Container == "" {
		return fmt.Errorf("%w: target resource binding", ErrInvalidArgument)
	}
	if plan.CreatedAt.IsZero() || plan.ExpiresAt.IsZero() || !plan.ExpiresAt.After(plan.CreatedAt) || len(plan.PatchSummary) > 2048 || len(plan.RollbackPlan) > 4096 || len(plan.ValidationPlan) > 4096 {
		return fmt.Errorf("%w: plan time or summary bounds", ErrInvalidArgument)
	}
	canonicalHash, err := CanonicalPlanHash(plan)
	if err != nil || canonicalHash != plan.CanonicalPlanHash {
		return fmt.Errorf("%w: canonical plan hash mismatch", ErrInvalidArgument)
	}
	return nil
}

func NewApproval(plan RemediationPlan, provider, login, role, reason, requestID string, authenticatedAt, expiresAt time.Time) (Approval, error) {
	return NewDecision(plan, DecisionApproved, provider, login, role, reason, requestID, authenticatedAt, expiresAt)
}

func NewDecision(plan RemediationPlan, decision Decision, provider, login, role, reason, requestID string, authenticatedAt, expiresAt time.Time) (Approval, error) {
	if plan.OperationType != OperationRestoreRequiredEnv || plan.CanonicalPlanHash == "" || plan.HashSchemaVersion != HashSchemaVersion || plan.PlanContentSchemaVersion != PlanContentSchemaVersion {
		return Approval{}, fmt.Errorf("%w: plan is not an approvable restore plan", ErrInvalidArgument)
	}
	if decision != DecisionApproved && decision != DecisionRejected {
		return Approval{}, fmt.Errorf("%w: invalid decision", ErrInvalidArgument)
	}
	provider = strings.TrimSpace(provider)
	login = strings.TrimSpace(login)
	role = strings.TrimSpace(role)
	reason = strings.TrimSpace(reason)
	requestID = strings.TrimSpace(requestID)
	authenticatedAt = normalizeTime(authenticatedAt)
	expiresAt = normalizeTime(expiresAt)
	if provider != "local" || login != "owner" || role != "owner" || reason == "" || requestID == "" || authenticatedAt.IsZero() || expiresAt.IsZero() || !expiresAt.After(authenticatedAt) || !authenticatedAt.Before(plan.ExpiresAt) || expiresAt.After(plan.ExpiresAt) {
		return Approval{}, fmt.Errorf("%w: approval identity or expiry is invalid", ErrInvalidArgument)
	}
	return Approval{PublicID: uuid.NewString(), PlanID: plan.ID, Decision: decision, Actor: login, CreatedAt: authenticatedAt, DecisionSchemaVersion: DecisionSchemaVersion, IncidentID: plan.IncidentID, CycleNo: plan.CycleNo, PlanVersion: plan.PlanVersion, ActorProvider: provider, Role: role, Reason: reason, RequestID: requestID, RequestAuthenticatedAt: authenticatedAt, ExpiresAt: expiresAt, ApprovedHashSchemaVersion: plan.HashSchemaVersion, ApprovedPlanHash: plan.CanonicalPlanHash, ApprovedPatchHash: plan.ProposedPatchHash, ApprovedBaseSHA: plan.TargetBaseRevision, ApprovedPostImageHash: plan.ExpectedPostImageHash, ApprovedTreeHash: plan.ExpectedTreeHash, ApprovedPolicyHash: plan.PolicySnapshotHash, ApprovedVerificationHash: plan.VerificationPlanHash, ApprovedEvidenceSetHash: plan.EvidenceSetHash}, nil
}

func ValidateApprovalBinding(plan RemediationPlan, approval Approval, now time.Time) error {
	if approval.Decision != DecisionApproved {
		return ErrApprovalMismatch
	}
	return ValidateDecisionBinding(plan, approval, now)
}

func ValidateDecisionBinding(plan RemediationPlan, decision Approval, now time.Time) error {
	if decision.DecisionSchemaVersion != DecisionSchemaVersion || decision.IncidentID != plan.IncidentID || decision.CycleNo != plan.CycleNo || decision.PlanID != plan.ID || decision.PlanVersion != plan.PlanVersion || decision.Decision != DecisionApproved && decision.Decision != DecisionRejected || decision.ApprovedHashSchemaVersion != plan.HashSchemaVersion || decision.ApprovedPlanHash != plan.CanonicalPlanHash || decision.ApprovedPatchHash != plan.ProposedPatchHash || decision.ApprovedBaseSHA != plan.TargetBaseRevision || decision.ApprovedPostImageHash != plan.ExpectedPostImageHash || decision.ApprovedTreeHash != plan.ExpectedTreeHash || decision.ApprovedPolicyHash != plan.PolicySnapshotHash || decision.ApprovedVerificationHash != plan.VerificationPlanHash || decision.ApprovedEvidenceSetHash != plan.EvidenceSetHash {
		return ErrApprovalMismatch
	}
	if _, err := uuid.Parse(decision.PublicID); err != nil || decision.ActorProvider != "local" || decision.Actor != "owner" || decision.Role != "owner" || strings.TrimSpace(decision.Reason) == "" || strings.TrimSpace(decision.RequestID) == "" || decision.RequestAuthenticatedAt.IsZero() || decision.CreatedAt.IsZero() || decision.ExpiresAt.IsZero() || decision.RequestAuthenticatedAt.After(decision.CreatedAt) || decision.ExpiresAt.After(plan.ExpiresAt) || !decision.ExpiresAt.After(decision.RequestAuthenticatedAt) || !now.UTC().Before(decision.ExpiresAt) || !now.UTC().Before(plan.ExpiresAt) {
		return ErrApprovalMismatch
	}
	if len(decision.Actor) > 128 || len(decision.Reason) > 1024 || len(decision.RequestID) > 128 {
		return ErrApprovalMismatch
	}
	return nil
}

// normalizeTime mirrors MySQL DATETIME(6) precision so canonical hashes and
// idempotency comparisons remain stable after a round trip through storage.
func normalizeTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func validObjectID(value string) bool {
	return gitObjectIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

func isLowerHex(value string) bool {
	return strings.ToLower(value) == value && isHex(value)
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func evidenceIDs(values []EvidenceBinding) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func evidenceBytes(values []EvidenceBinding) []byte {
	// Marshal through maps so repository reads can canonicalize MySQL JSON back
	// to exactly the same representation after a database round trip.
	canonical := make([]map[string]string, 0, len(values))
	for _, value := range values {
		canonical = append(canonical, map[string]string{
			"content_hash": value.ContentHash,
			"id":           value.ID,
		})
	}
	encoded, _ := json.Marshal(canonical)
	return encoded
}

func canonicalJSON(value any) ([]byte, error) {
	if raw, ok := value.(json.RawMessage); ok && (len(raw) == 0 || !json.Valid(raw)) {
		return nil, fmt.Errorf("%w: invalid JSON", ErrInvalidArgument)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	// Re-encoding the decoded value sorts map keys, providing one representation
	// independent of the object ordering chosen by MySQL JSON storage.
	return json.Marshal(decoded)
}

func lengthPrefixedHash(fields ...[]byte) (string, error) {
	var buffer bytes.Buffer
	for _, field := range fields {
		if uint64(len(field)) > uint64(^uint32(0)) {
			return "", fmt.Errorf("%w: canonical field too large", ErrInvalidArgument)
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = buffer.Write(length[:])
		_, _ = buffer.Write(field)
	}
	sum := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
