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
