package bootstrap

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIBinaryLinksProductionV3AlertmanagerIngress(t *testing.T) {
	dependencies := processDependencies(t, "./cmd/cloudops-api")
	for _, required := range []string{
		rootModulePath + "/internal/alertmanageringress",
		rootModulePath + "/internal/infra/incidentstore",
	} {
		if !dependencies[required] {
			t.Errorf("cloudops-api does not compile required V3 ingress capability %s", required)
		}
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "cloudops-api")
	build := exec.Command("go", "build", "-o", binary, "./cmd/cloudops-api")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cloudops-api: %v\n%s", err, output)
	}
	nm := exec.Command("go", "tool", "nm", binary)
	nm.Dir = root
	output, err := nm.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect cloudops-api symbols: %v\n%s", err, output)
	}
	symbols := string(output)
	for _, required := range []string{
		"internal/alertmanageringress.(*Handler).Webhook",
		"internal/infra/incidentstore.(*Store).IngestBatch",
		"internal/router.NewInternalRouter",
	} {
		if !strings.Contains(symbols, required) {
			t.Errorf("cloudops-api is missing production V3 ingress symbol %s", required)
		}
	}
}
