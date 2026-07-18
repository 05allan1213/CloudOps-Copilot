package remediation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func HashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func CanonicalHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: canonical JSON", ErrInvalidArgument)
	}
	if len(payload) > MaxPlanJSONBytes {
		return "", fmt.Errorf("%w: canonical JSON size", ErrInvalidArgument)
	}
	return HashBytes(payload), nil
}

func ComputePlanHash(plan RemediationPlan) (string, error) {
	return CanonicalHash(struct {
		IncidentPublicID, OperationType, Repository, BaseRevision, Path string
		PlanVersion                                                     int
		Parameters                                                      Parameters
		EvidenceReferences                                              []string
		RiskLevel, PolicySnapshotHash, ExpectedBeforeHash, PatchHash    string
	}{plan.IncidentPublicID, string(plan.OperationType), plan.TargetRepository, plan.TargetBaseRevision, plan.TargetPath, plan.PlanVersion, plan.Parameters, plan.EvidenceReferences, string(plan.RiskLevel), plan.PolicySnapshotHash, plan.ExpectedBeforeHash, plan.ProposedPatchHash})
}
