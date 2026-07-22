package bootstrap

import (
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestV3RestoreEnvPolicyUsesFrozenVersion(t *testing.T) {
	target := V3WorkerTargetConfig{
		Namespace: "demo", Workload: "demo", Container: "demo",
		RepositoryOwner: "acme", RepositoryName: "gitops", BaseBranch: "main",
		GitOpsPath: "apps/demo/deployment.yaml", RequiredEnvKey: "REQUIRED_ENV",
	}
	policy := v3RestoreEnvPolicy(target)
	if policy.Version != remediation.RestoreRequiredEnvPolicyVersion {
		t.Fatalf("policy version=%q want %q", policy.Version, remediation.RestoreRequiredEnvPolicyVersion)
	}
	if policy.VerificationVersion != verification.GoldenRequiredEnvProfileID ||
		policy.MaxDiffBytes != remediation.MaxV3PlanDiffBytes ||
		policy.MaxPostImageBytes != remediation.MaxV3PostImageBytes {
		t.Fatalf("policy is not bound to the frozen V3 contract: %+v", policy)
	}
}
