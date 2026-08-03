package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// DiagnosisValidationInput binds untrusted structured output to one immutable
// Incident cycle and the deterministic sufficiency result calculated for it.
type DiagnosisValidationInput struct {
	IncidentID  string
	CycleNo     uint64
	Facts       []EvidenceFact
	Policy      ClaimPolicy
	Sufficiency SufficiencyResult
}

// ValidateDiagnosisRecord is the single authority for creating an immutable,
// policy-bound DiagnosisRecord. Callers must still reload current Evidence in
// their owning transaction before persisting the returned record.
func ValidateDiagnosisRecord(candidate DiagnosisCandidate, input DiagnosisValidationInput) (DiagnosisRecord, error) {
	candidate.ClaimType = strings.TrimSpace(candidate.ClaimType)
	candidate.Summary = strings.TrimSpace(candidate.Summary)
	if strings.TrimSpace(input.IncidentID) == "" || input.CycleNo == 0 {
		return DiagnosisRecord{}, fmt.Errorf("%w: diagnosis incident cycle is required", ErrInvalidArgument)
	}
	if err := validateClaimPolicy(input.Policy); err != nil {
		return DiagnosisRecord{}, err
	}
	if candidate.ClaimType != input.Policy.ClaimType || candidate.Summary == "" || len(candidate.Summary) > 4096 {
		return DiagnosisRecord{}, errors.New("diagnosis claim type or summary is invalid")
	}
	switch candidate.Confidence {
	case DiagnosisConfirmed, DiagnosisLikely, DiagnosisUnknown:
	default:
		return DiagnosisRecord{}, errors.New("diagnosis confidence is not an allowed enum")
	}
	switch candidate.RemediationHint {
	case RemediationRestoreRequiredEnv, RemediationCollectMore, RemediationNone:
	default:
		return DiagnosisRecord{}, errors.New("diagnosis remediation hint is not an allowed enum")
	}
	if candidate.Confidence == DiagnosisConfirmed {
		if input.Sufficiency.Outcome != SufficiencyReady {
			return DiagnosisRecord{}, errors.New("confirmed diagnosis is unsupported by deterministic sufficiency")
		}
		if input.Policy.ClaimType == GoldenRequiredEnvClaimPolicy().ClaimType && candidate.RemediationHint != RemediationRestoreRequiredEnv {
			return DiagnosisRecord{}, errors.New("golden confirmed diagnosis requires restore_required_env")
		}
	} else if candidate.RemediationHint == RemediationRestoreRequiredEnv {
		return DiagnosisRecord{}, errors.New("restore_required_env requires a confirmed diagnosis")
	}
	if containsProhibitedDiagnosisText(candidate.Summary) {
		return DiagnosisRecord{}, errors.New("diagnosis summary contains prohibited execution instructions")
	}
	if len(candidate.EvidenceFactIDs) == 0 || len(candidate.EvidenceFactIDs) > 64 || len(candidate.Unknowns) > 20 {
		return DiagnosisRecord{}, errors.New("diagnosis evidence or unknowns exceed bounds")
	}

	facts := make(map[string]EvidenceFact, len(input.Facts))
	for _, fact := range input.Facts {
		facts[fact.ID] = fact
	}
	candidate.EvidenceFactIDs = stableDiagnosisStrings(candidate.EvidenceFactIDs)
	evidenceIDs := make([]string, 0, len(candidate.EvidenceFactIDs))
	for _, id := range candidate.EvidenceFactIDs {
		fact, ok := facts[id]
		if !ok || fact.IncidentID != input.IncidentID || fact.CycleNo != input.CycleNo ||
			fact.EvidenceID == "" || fact.CollectionStatus != CollectionAvailable || fact.Integrity != "verified" ||
			fact.ClaimUse == "forbidden" || fact.Truncated {
			return DiagnosisRecord{}, fmt.Errorf("diagnosis references unusable fact %q", id)
		}
		evidenceIDs = append(evidenceIDs, fact.EvidenceID)
	}
	if candidate.Confidence == DiagnosisConfirmed {
		for _, required := range input.Sufficiency.SupportingIDs {
			if !slices.Contains(candidate.EvidenceFactIDs, required) {
				return DiagnosisRecord{}, fmt.Errorf("confirmed diagnosis omits supporting fact %q", required)
			}
		}
	}
	for index := range candidate.Unknowns {
		candidate.Unknowns[index] = strings.TrimSpace(candidate.Unknowns[index])
		if candidate.Unknowns[index] == "" || len(candidate.Unknowns[index]) > 1024 || containsProhibitedDiagnosisText(candidate.Unknowns[index]) {
			return DiagnosisRecord{}, errors.New("diagnosis unknown is empty, oversized, or contains instructions")
		}
	}
	candidate.Unknowns = stableDiagnosisStrings(candidate.Unknowns)
	evidenceIDs = stableDiagnosisStrings(evidenceIDs)
	policyJSON, err := json.Marshal(input.Policy)
	if err != nil {
		return DiagnosisRecord{}, fmt.Errorf("encode diagnosis policy: %w", err)
	}
	record := DiagnosisRecord{
		Candidate: candidate, ClaimPolicyVersion: input.Policy.Version,
		ClaimPolicyHash: diagnosisSHA256(policyJSON), EvidenceIDs: evidenceIDs,
	}
	unsigned, err := json.Marshal(record)
	if err != nil {
		return DiagnosisRecord{}, fmt.Errorf("encode diagnosis record: %w", err)
	}
	record.DiagnosisHash = diagnosisSHA256(unsigned)
	return record, nil
}

func containsProhibitedDiagnosisText(value string) bool {
	normalized := strings.ToLower(value)
	for _, prohibited := range []string{
		"kubectl apply", "kubectl patch", "kubectl delete", "argocd sync", "argo cd sync",
		"create pull request", "force push", "execute shell", "restart deployment", "scale deployment",
	} {
		if strings.Contains(normalized, prohibited) {
			return true
		}
	}
	return false
}

func stableDiagnosisStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func diagnosisSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
