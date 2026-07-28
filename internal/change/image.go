package change

import (
	"regexp"
	"sort"
	"strings"
)

type ImageResolutionStatus string

const (
	ImageConfirmed ImageResolutionStatus = "confirmed"
	ImageUnknown   ImageResolutionStatus = "unknown"
	ImageConflict  ImageResolutionStatus = "conflict"
)

type VerificationState string

const (
	VerificationVerified VerificationState = "verified"
	VerificationConflict VerificationState = "conflict"
	VerificationUnknown  VerificationState = "unknown"
)

type ResolutionFact struct {
	Kind         string            `json:"kind"`
	Source       string            `json:"source"`
	Value        string            `json:"value,omitempty"`
	Verification VerificationState `json:"verification"`
}

const (
	ReasonDigestConflict       = "digest_conflict"
	ReasonRevisionConflict     = "revision_conflict"
	ReasonSourceConflict       = "source_conflict"
	ReasonRuntimeDigestMissing = "runtime_digest_missing"
	ReasonRuntimeDigestInvalid = "runtime_digest_invalid"
	ReasonRegistryUnavailable  = "registry_unavailable"
	ReasonRegistryAuthFailed   = "registry_authentication_failed"
	ReasonMetadataInvalid      = "registry_metadata_invalid"
	ReasonMetadataTruncated    = "registry_metadata_truncated"
	ReasonLabelsMissing        = "oci_labels_missing"
	ReasonRevisionInvalid      = "oci_revision_invalid"
	ReasonVersionMismatch      = "oci_version_mismatch"
	ReasonArgoRevisionMissing  = "argocd_revision_missing"
	ReasonGitHubCommitMissing  = "github_commit_missing"
	ReasonMutableTagOnly       = "mutable_tag_only"
)

type ImageResolutionInput struct {
	RuntimeDigest            string           `json:"runtime_digest"`
	ImageTag                 string           `json:"image_tag"`
	RegistryMetadata         RegistryMetadata `json:"registry_metadata"`
	RegistryErrorCode        string           `json:"registry_error_code,omitempty"`
	ArgoDeployedRevision     string           `json:"argocd_deployed_revision"`
	GitHubCommitSHA          string           `json:"github_commit_sha"`
	ExpectedSourceRepository string           `json:"expected_source_repository"`
	AllowedOCISources        []string         `json:"allowed_oci_sources"`
}

type ImageResolution struct {
	Status           ImageResolutionStatus `json:"status"`
	Digest           string                `json:"digest"`
	Revision         string                `json:"revision"`
	Source           string                `json:"source"`
	Version          string                `json:"version"`
	Reasons          []string              `json:"reasons"`
	Facts            []ResolutionFact      `json:"facts"`
	RegistryMetadata RegistryMetadata      `json:"registry_metadata"`
	Valid            bool                  `json:"valid"`
	Truncated        bool                  `json:"truncated"`
	Degraded         bool                  `json:"degraded"`
}

type OCILabelValidation struct {
	Valid    bool     `json:"valid"`
	Revision string   `json:"revision"`
	Source   string   `json:"source"`
	Version  string   `json:"version"`
	Reasons  []string `json:"reasons"`
}

var sensitiveLabelKey = regexp.MustCompile(`(?i)(token|password|secret|credential|private.?key|authorization)`)

// ValidateOCILabels validates only build-produced OCI metadata against an explicit source allowlist.
func ValidateOCILabels(labels map[string]string, allowedSources []string) OCILabelValidation {
	result := OCILabelValidation{Revision: strings.TrimSpace(labels["org.opencontainers.image.revision"]), Source: strings.TrimSpace(labels["org.opencontainers.image.source"]), Version: strings.TrimSpace(labels["org.opencontainers.image.version"])}
	if !commitPattern.MatchString(result.Revision) {
		result.Reasons = append(result.Reasons, "OCI revision is missing or is not a commit SHA")
	}
	allowed := false
	for _, source := range allowedSources {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(source), "/"), strings.TrimRight(result.Source, "/")) {
			allowed = true
			break
		}
	}
	if result.Source == "" || !allowed {
		result.Reasons = append(result.Reasons, "OCI source is missing or not allowlisted")
	}
	if result.Version == "" || len(result.Version) > 255 || result.Version != result.Revision {
		result.Reasons = append(result.Reasons, "OCI version must exactly equal the revision")
	}
	for key := range labels {
		if sensitiveLabelKey.MatchString(key) {
			result.Reasons = append(result.Reasons, "sensitive label key is prohibited")
			break
		}
	}
	result.Valid = len(result.Reasons) == 0
	return result
}

func ResolveImageRevision(input ImageResolutionInput) ImageResolution {
	metadata := input.RegistryMetadata
	result := ImageResolution{Status: ImageUnknown, Digest: strings.ToLower(strings.TrimSpace(input.RuntimeDigest)), RegistryMetadata: metadata, Truncated: metadata.Truncated, Degraded: metadata.Degraded}
	addFact := func(kind, source, value string, state VerificationState) {
		result.Facts = append(result.Facts, ResolutionFact{Kind: kind, Source: source, Value: value, Verification: state})
	}
	addReason := func(code string) { result.Reasons = append(result.Reasons, code) }
	if result.Digest == "" {
		addReason(ReasonRuntimeDigestMissing)
		addFact("manifest_digest", "kubernetes_pod_image_id", "", VerificationUnknown)
	} else if !sha256DigestPattern.MatchString(result.Digest) {
		addReason(ReasonRuntimeDigestInvalid)
		addFact("manifest_digest", "kubernetes_pod_image_id", result.Digest, VerificationUnknown)
	} else {
		addFact("manifest_digest", "kubernetes_pod_image_id", result.Digest, VerificationVerified)
	}
	if input.RegistryErrorCode != "" {
		if input.RegistryErrorCode == "authentication" {
			addReason(ReasonRegistryAuthFailed)
		} else {
			addReason(ReasonRegistryUnavailable)
		}
		result.Degraded = true
	}
	if metadata.Truncated {
		addReason(ReasonMetadataTruncated)
	}
	if !metadata.Valid || metadata.Integrity != RegistryIntegrityVerified {
		addReason(ReasonMetadataInvalid)
	}
	manifestDigest := strings.ToLower(strings.TrimSpace(metadata.ManifestDigest))
	if result.Digest != "" && manifestDigest != "" && result.Digest != manifestDigest {
		addReason(ReasonDigestConflict)
		addFact("registry_manifest_digest", "registry_manifest", manifestDigest, VerificationConflict)
	} else if manifestDigest != "" {
		addFact("registry_manifest_digest", "registry_manifest", manifestDigest, VerificationVerified)
	}
	revision := strings.ToLower(strings.TrimSpace(metadata.Revision))
	result.Revision, result.Source, result.Version = revision, strings.TrimSpace(metadata.Source), strings.TrimSpace(metadata.Version)
	if revision == "" || result.Source == "" || result.Version == "" {
		addReason(ReasonLabelsMissing)
	}
	if revision != "" && !commitPattern.MatchString(revision) {
		addReason(ReasonRevisionInvalid)
	}
	if revision != "" && result.Version != revision {
		addReason(ReasonVersionMismatch)
	}
	if result.Source != "" && (!sourceAllowed(result.Source, input.AllowedOCISources) || !sameSourceRepository(result.Source, input.ExpectedSourceRepository)) {
		addReason(ReasonSourceConflict)
		addFact("oci_source", "registry_image_config", result.Source, VerificationConflict)
	} else if result.Source != "" {
		addFact("oci_source", "registry_image_config", result.Source, VerificationVerified)
	}
	argoRevision := strings.ToLower(strings.TrimSpace(input.ArgoDeployedRevision))
	if argoRevision == "" {
		addReason(ReasonArgoRevisionMissing)
		addFact("deployed_revision", "argocd_application", "", VerificationUnknown)
	} else if commitPattern.MatchString(revision) && argoRevision != revision {
		addReason(ReasonRevisionConflict)
		addFact("deployed_revision", "argocd_application", argoRevision, VerificationConflict)
	} else {
		addFact("deployed_revision", "argocd_application", argoRevision, VerificationVerified)
	}
	githubRevision := strings.ToLower(strings.TrimSpace(input.GitHubCommitSHA))
	if githubRevision == "" {
		addReason(ReasonGitHubCommitMissing)
		addFact("commit", "github_commit_api", "", VerificationUnknown)
	} else if commitPattern.MatchString(revision) && githubRevision != revision {
		addReason(ReasonRevisionConflict)
		addFact("commit", "github_commit_api", githubRevision, VerificationConflict)
	} else {
		addFact("commit", "github_commit_api", githubRevision, VerificationVerified)
	}
	if result.Digest == "" && strings.TrimSpace(input.ImageTag) != "" {
		addReason(ReasonMutableTagOnly)
	}
	result.Reasons = orderedReasons(result.Reasons)
	for _, reason := range result.Reasons {
		if reason == ReasonDigestConflict || reason == ReasonRevisionConflict || reason == ReasonSourceConflict {
			result.Status = ImageConflict
			return result
		}
	}
	if len(result.Reasons) == 0 {
		result.Status = ImageConfirmed
		result.Valid = true
	}
	return result
}

var sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func sourceAllowed(source string, allowed []string) bool {
	for _, candidate := range allowed {
		if normalizeSource(source) == normalizeSource(candidate) {
			return true
		}
	}
	return false
}

func sameSourceRepository(left, right string) bool {
	return right != "" && normalizeSource(left) == normalizeSource(right)
}

func normalizeSource(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(value), "/"), ".git"))
}

func orderedReasons(values []string) []string {
	priority := map[string]int{ReasonDigestConflict: 0, ReasonRevisionConflict: 1, ReasonSourceConflict: 2, ReasonRuntimeDigestMissing: 3, ReasonRuntimeDigestInvalid: 4, ReasonRegistryAuthFailed: 5, ReasonRegistryUnavailable: 6, ReasonMetadataTruncated: 7, ReasonMetadataInvalid: 8, ReasonLabelsMissing: 9, ReasonRevisionInvalid: 10, ReasonVersionMismatch: 11, ReasonArgoRevisionMissing: 12, ReasonGitHubCommitMissing: 13, ReasonMutableTagOnly: 14}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool { return priority[result[i]] < priority[result[j]] })
	return result
}
