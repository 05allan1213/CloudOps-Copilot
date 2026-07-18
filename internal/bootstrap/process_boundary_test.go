package bootstrap

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const rootModulePath = "github.com/05allan1213/CloudOps-Copilot"

func TestWorkerDoesNotCompileMigrationCapability(t *testing.T) {
	dependencies := processDependencies(t, "./cmd/cloudops-worker")
	for _, forbidden := range []string{
		rootModulePath + "/internal/bootstrap/migrate",
		rootModulePath + "/internal/migration",
		rootModulePath + "/migrations",
	} {
		if dependencies[forbidden] {
			t.Errorf("cloudops-worker compiles migration capability %s", forbidden)
		}
	}
}

func TestWorkerDoesNotCompileLegacyLeaseClaimLoops(t *testing.T) {
	dependencies := processDependencies(t, "./cmd/cloudops-worker")
	for _, forbidden := range []string{
		rootModulePath + "/internal/startup/legacyworker",
		rootModulePath + "/internal/service/agentruntime",
		rootModulePath + "/internal/service/remediation",
		rootModulePath + "/internal/service/deliveryverification",
		rootModulePath + "/internal/infra/incidentmysql",
	} {
		if dependencies[forbidden] {
			t.Errorf("cloudops-worker compiles legacy lease claim package %s", forbidden)
		}
	}
	for _, required := range []string{
		rootModulePath + "/internal/asyncjob",
		rootModulePath + "/internal/taskhandler",
	} {
		if !dependencies[required] {
			t.Errorf("cloudops-worker does not compile required task runtime %s", required)
		}
	}
}

func TestWorkerBinaryDoesNotLinkLegacyClaimSymbols(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "cloudops-worker")
	build := exec.Command("go", "build", "-o", binary, "./cmd/cloudops-worker")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cloudops-worker: %v\n%s", err, output)
	}
	nm := exec.Command("go", "tool", "nm", binary)
	nm.Dir = root
	output, err := nm.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect cloudops-worker symbols: %v\n%s", err, output)
	}
	symbols := string(output)
	for _, forbidden := range []string{
		"internal/infra/incidentmysql.(*Store).ClaimNext",
		"internal/infra/incidentmysql.(*RemediationRepository).ClaimDelivery",
		"internal/infra/incidentmysql.(*VerificationRepository).ClaimDelivery",
		"internal/infra/incidentmysql.(*VerificationRepository).ClaimRun",
		"internal/service/agentruntime.(*Service).ProcessNext",
		"internal/service/remediation.(*Worker).RunOnce",
		"internal/service/deliveryverification.(*Service).ObserveNext",
		"internal/service/deliveryverification.(*Service).VerifyNext",
		"internal/startup/legacyworker.InitAgentRuntime",
		"internal/startup/legacyworker.InitRemediation",
		"internal/startup/legacyworker.InitDeliveryVerification",
	} {
		if strings.Contains(symbols, forbidden) {
			t.Errorf("cloudops-worker links forbidden legacy claim symbol %s", forbidden)
		}
	}
	for _, required := range []string{
		"internal/asyncjob.(*Repository).Claim",
		"internal/asyncjob.(*Repository).ClaimReady",
		"internal/asyncjob.(*Repository).TakeoverExpired",
		"internal/asyncjob.(*Runner).runPool",
	} {
		if !strings.Contains(symbols, required) {
			t.Errorf("cloudops-worker is missing required async claim symbol %s", required)
		}
	}
}

func TestMigrateDoesNotCompileRuntimeCapability(t *testing.T) {
	dependencies := processDependencies(t, "./cmd/cloudops-migrate")
	if dependencies[rootModulePath+"/internal/bootstrap"] {
		t.Errorf("cloudops-migrate compiles Worker bootstrap capability")
	}
	forbiddenTrees := []string{
		rootModulePath + "/internal/startup",
		rootModulePath + "/internal/service/agentruntime",
		rootModulePath + "/internal/service/remediation",
		rootModulePath + "/internal/service/deliveryverification",
		rootModulePath + "/internal/agent/llm",
		rootModulePath + "/internal/infra/githubwrite",
		rootModulePath + "/internal/infra/observabilityread",
	}
	for dependency := range dependencies {
		for _, forbidden := range forbiddenTrees {
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("cloudops-migrate compiles runtime capability %s", dependency)
			}
		}
	}
}

func processDependencies(t *testing.T, target string) map[string]bool {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "list", "-deps", target)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", target, err, output)
	}
	result := make(map[string]bool)
	for _, dependency := range strings.Fields(string(output)) {
		result[dependency] = true
	}
	return result
}
