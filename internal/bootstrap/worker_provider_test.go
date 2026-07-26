package bootstrap

import (
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestRestoreEnvPolicyUsesFrozenVersion(t *testing.T) {
	target := OperationalTargetConfig{
		Namespace: "demo", Workload: "demo", Container: "demo",
		RepositoryOwner: "acme", RepositoryName: "gitops", BaseBranch: "main",
		GitOpsPath: "apps/demo/deployment.yaml", RequiredEnvKey: "REQUIRED_ENV",
	}
	policy := restoreEnvPolicy(target)
	if policy.Version != remediation.RestoreRequiredEnvPolicyVersion {
		t.Fatalf("policy version=%q want %q", policy.Version, remediation.RestoreRequiredEnvPolicyVersion)
	}
	if policy.VerificationVersion != verification.GoldenRequiredEnvProfileID ||
		policy.MaxDiffBytes != remediation.MaxPlanDiffBytes ||
		policy.MaxPostImageBytes != remediation.MaxPostImageBytes {
		t.Fatalf("policy is not bound to the frozen contract: %+v", policy)
	}
}
