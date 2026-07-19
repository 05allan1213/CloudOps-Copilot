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
	V3DomainSchemaVersion      = 3
	V3HashSchemaVersion        = 1
	V3PlanContentSchemaVersion = 1
	V3DecisionSchemaVersion    = 1
	MaxV3PlanDiffBytes         = 64 * 1024
	MaxV3PostImageBytes        = 256 * 1024
	MaxV3PolicyBytes           = 16 * 1024
	MaxV3Evidence              = 40
)

var (
	gitObjectIDPattern = regexp.MustCompile(`^[a-f0-9]{40}(?:[a-f0-9]{24})?$`)
	envKeyPattern      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	repositoryPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// V3RemediationHint is the only remediation authority the model may return.
// In particular, it has no repository, branch, patch, or environment value.
type V3RemediationHint struct {
	OperationHint         OperationType `json:"operation_hint"`
	TargetFieldRef        string        `json:"target_field_ref"`
	LastKnownGoodEvidence string        `json:"last_known_good_evidence_id"`
	SupportingEvidenceIDs []string      `json:"supporting_evidence_ids"`
}

func DecodeV3RemediationHint(payload []byte) (V3RemediationHint, error) {
	if len(payload) == 0 || len(payload) > MaxPlannerJSONBytes {
		return V3RemediationHint{}, fmt.Errorf("%w: remediation hint size", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var hint V3RemediationHint
	if err := decoder.Decode(&hint); err != nil {
		return V3RemediationHint{}, fmt.Errorf("%w: remediation hint schema: %v", ErrInvalidArgument, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return V3RemediationHint{}, fmt.Errorf("%w: remediation hint has multiple JSON values", ErrInvalidArgument)
	}
	if hint.OperationHint != OperationRestoreRequiredEnv || strings.TrimSpace(hint.TargetFieldRef) == "" || len(hint.SupportingEvidenceIDs) == 0 {
		return V3RemediationHint{}, fmt.Errorf("%w: only restore_required_env with bounded references is allowed", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(hint.LastKnownGoodEvidence); err != nil {
		return V3RemediationHint{}, fmt.Errorf("%w: invalid baseline evidence ID", ErrInvalidArgument)
	}
	seen := map[string]struct{}{}
	for _, id := range append([]string{hint.LastKnownGoodEvidence}, hint.SupportingEvidenceIDs...) {
		if _, err := uuid.Parse(id); err != nil {
			return V3RemediationHint{}, fmt.Errorf("%w: invalid evidence ID", ErrInvalidArgument)
		}
		if _, ok := seen[id]; ok {
			return V3RemediationHint{}, fmt.Errorf("%w: duplicate evidence ID", ErrInvalidArgument)
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
// complete immutable V3 plan. The model never supplies the copied env node.
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
	if err != nil || len(policyBytes) > MaxV3PolicyBytes {
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
		RowVersion:     1, CreatedAt: request.CreatedAt.UTC(), UpdatedAt: request.CreatedAt.UTC(),
		CycleNo: request.CycleNo, IncidentVersion: request.IncidentVersion, CreatedByAgentRunID: request.CreatedByAgentRunID, DiagnosisHash: strings.ToLower(request.DiagnosisHash),
		HashSchemaVersion: V3HashSchemaVersion, PlanContentSchemaVersion: V3PlanContentSchemaVersion,
		LastKnownGoodRevision: strings.ToLower(request.LastKnownGoodRevision), TargetBaseBranch: request.BaseBranch,
		BaseBlobSHA: strings.ToLower(request.BaseBlobSHA), FileMode: request.FileMode,
		TargetFieldRef:        "spec.template.spec.containers[name=" + request.Target.Container + "].env[name=" + request.EnvKey + "]",
		ExpectedPostImageHash: patch.PostImageHash, ExpectedTreeHash: strings.ToLower(request.ExpectedTreeHash),
		CanonicalChangeManifest: manifestBytes, BoundedDiff: patch.Diff, PostImage: patch.Content,
		PolicyVersion: request.Policy.Version, PolicySnapshot: policyBytes, VerificationPlan: verificationBytes,
		VerificationPlanHash: verificationHash, EvidenceBindings: evidence, EvidenceSetHash: evidenceSetHash,
		ExpiresAt: request.ExpiresAt.UTC(),
	}
	canonicalHash, err := CanonicalV3PlanHash(plan)
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
	if request.PlanVersion <= 0 || len(request.CurrentContent) == 0 || len(request.CurrentContent) > MaxV3PostImageBytes || len(request.BaselineContent) == 0 || len(request.BaselineContent) > MaxV3PostImageBytes {
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
	if policy.MaxDiffBytes <= 0 || policy.MaxDiffBytes > MaxV3PlanDiffBytes || policy.MaxPostImageBytes <= 0 || policy.MaxPostImageBytes > MaxV3PostImageBytes {
		return fmt.Errorf("%w: policy bounds are invalid", ErrInvalidArgument)
	}
	if len(request.VerificationPlan) == 0 || !json.Valid(request.VerificationPlan) || len(request.Evidence) == 0 || len(request.Evidence) > MaxV3Evidence {
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

func CanonicalV3PlanHash(plan RemediationPlan) (string, error) {
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

func NewV3Approval(plan RemediationPlan, provider, login, role, reason, requestID string, authenticatedAt, expiresAt time.Time) (Approval, error) {
	if plan.OperationType != OperationRestoreRequiredEnv || plan.CanonicalPlanHash == "" || plan.HashSchemaVersion != V3HashSchemaVersion || plan.PlanContentSchemaVersion != V3PlanContentSchemaVersion {
		return Approval{}, fmt.Errorf("%w: plan is not an approvable V3 restore plan", ErrInvalidArgument)
	}
	if strings.TrimSpace(provider) != "github" || strings.TrimSpace(login) == "" || strings.TrimSpace(role) != "operator" && strings.TrimSpace(role) != "admin" || strings.TrimSpace(requestID) == "" || authenticatedAt.IsZero() || expiresAt.IsZero() || !expiresAt.After(authenticatedAt) {
		return Approval{}, fmt.Errorf("%w: approval identity or expiry is invalid", ErrInvalidArgument)
	}
	return Approval{PublicID: uuid.NewString(), PlanID: plan.ID, Decision: DecisionApproved, Actor: login, CreatedAt: authenticatedAt.UTC(), DomainSchemaVersion: V3DomainSchemaVersion, DecisionSchemaVersion: V3DecisionSchemaVersion, IncidentID: plan.IncidentID, CycleNo: plan.CycleNo, PlanVersion: plan.PlanVersion, ActorProvider: provider, Role: role, Reason: reason, RequestID: requestID, RequestAuthenticatedAt: authenticatedAt.UTC(), ExpiresAt: expiresAt.UTC(), ApprovedHashSchemaVersion: plan.HashSchemaVersion, ApprovedPlanHash: plan.CanonicalPlanHash, ApprovedPatchHash: plan.ProposedPatchHash, ApprovedBaseSHA: plan.TargetBaseRevision, ApprovedPostImageHash: plan.ExpectedPostImageHash, ApprovedTreeHash: plan.ExpectedTreeHash, ApprovedPolicyHash: plan.PolicySnapshotHash, ApprovedVerificationHash: plan.VerificationPlanHash, ApprovedEvidenceSetHash: plan.EvidenceSetHash}, nil
}

func ValidateV3ApprovalBinding(plan RemediationPlan, approval Approval, now time.Time) error {
	if approval.DomainSchemaVersion != V3DomainSchemaVersion || approval.DecisionSchemaVersion != V3DecisionSchemaVersion || approval.IncidentID != plan.IncidentID || approval.CycleNo != plan.CycleNo || approval.PlanID != plan.ID || approval.PlanVersion != plan.PlanVersion || approval.Decision != DecisionApproved || approval.ApprovedHashSchemaVersion != plan.HashSchemaVersion || approval.ApprovedPlanHash != plan.CanonicalPlanHash || approval.ApprovedPatchHash != plan.ProposedPatchHash || approval.ApprovedBaseSHA != plan.TargetBaseRevision || approval.ApprovedPostImageHash != plan.ExpectedPostImageHash || approval.ApprovedTreeHash != plan.ExpectedTreeHash || approval.ApprovedPolicyHash != plan.PolicySnapshotHash || approval.ApprovedVerificationHash != plan.VerificationPlanHash || approval.ApprovedEvidenceSetHash != plan.EvidenceSetHash {
		return ErrApprovalMismatch
	}
	if !approval.ExpiresAt.IsZero() && !now.UTC().Before(approval.ExpiresAt) {
		return ErrApprovalMismatch
	}
	return nil
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
	encoded, _ := json.Marshal(values)
	return encoded
}

func canonicalJSON(value any) ([]byte, error) {
	if raw, ok := value.(json.RawMessage); ok {
		if len(raw) == 0 || !json.Valid(raw) {
			return nil, fmt.Errorf("%w: invalid JSON", ErrInvalidArgument)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		return json.Marshal(decoded)
	}
	return json.Marshal(value)
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
