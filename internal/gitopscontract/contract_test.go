package gitopscontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryFixturesMatchExternalContract(t *testing.T) {
	healthy := filepath.Join("..", "..", "deploy", "contracts", "gitops-demo", "healthy", "apps", "demo")
	regression := filepath.Join("..", "..", "deploy", "contracts", "gitops-demo", "regression", "apps", "demo")
	if err := ValidateHealthy(healthy); err != nil {
		t.Fatalf("validate healthy fixture: %v", err)
	}
	if err := ValidateRegression(healthy, regression); err != nil {
		t.Fatalf("validate regression fixture: %v", err)
	}
}

func TestValidateHealthyRejectsMultipleDocuments(t *testing.T) {
	healthy := filepath.Join("..", "..", "deploy", "contracts", "gitops-demo", "healthy", "apps", "demo")
	target := copyFixtureTree(t, healthy)
	path := filepath.Join(target, "service.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("---\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHealthy(target); err == nil {
		t.Fatal("expected multiple YAML documents to fail")
	}
}

func TestValidateHealthyRejectsInventoryDrift(t *testing.T) {
	healthy := repositoryFixture(t, "healthy")
	target := copyFixtureTree(t, healthy)
	if err := os.WriteFile(filepath.Join(target, "extra.yaml"), []byte("apiVersion: v1\nkind: Service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHealthy(target); err == nil {
		t.Fatal("expected an extra manifest to fail")
	}
}

func TestValidateHealthyRejectsMutableImageAndIdentityDrift(t *testing.T) {
	healthy := repositoryFixture(t, "healthy")
	for name, replacement := range map[string][2]string{
		"mutable image": {"cloudops-demo@sha256:3c2008e41d439325d8b2fff920abf7dafa7d0ad9ad5f92dd1b8af34bd90f1e5d", "cloudops-demo:latest"},
		"namespace":     {"namespace: demo", "namespace: cloudops-demo"},
		"container":     {"- name: demo\n          image:", "- name: cloudops-demo\n          image:"},
	} {
		t.Run(name, func(t *testing.T) {
			target := copyFixtureTree(t, healthy)
			mutateFixtureFile(t, filepath.Join(target, "deployment.yaml"), replacement[0], replacement[1])
			if err := ValidateHealthy(target); err == nil {
				t.Fatalf("expected %s drift to fail", name)
			}
		})
	}
}

func TestValidateHealthyRejectsSymlink(t *testing.T) {
	healthy := repositoryFixture(t, "healthy")
	target := copyFixtureTree(t, healthy)
	path := filepath.Join(target, "service.yaml")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(healthy, "service.yaml"), path); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHealthy(target); err == nil {
		t.Fatal("expected a symlink manifest to fail")
	}
}

func TestValidateRegressionRejectsChangesBeyondRequiredEnv(t *testing.T) {
	healthy := repositoryFixture(t, "healthy")
	regression := repositoryFixture(t, "regression")
	target := copyFixtureTree(t, regression)
	mutateFixtureFile(t, filepath.Join(target, "service.yaml"), "port: 8080", "port: 8081")
	if err := ValidateRegression(healthy, target); err == nil {
		t.Fatal("expected an additional regression change to fail")
	}
}

func repositoryFixture(t *testing.T, state string) string {
	t.Helper()
	return filepath.Join("..", "..", "deploy", "contracts", "gitops-demo", state, "apps", "demo")
}

func mutateFixtureFile(t *testing.T, path, oldValue, newValue string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), oldValue, newValue, 1)
	if updated == string(data) {
		t.Fatalf("fixture mutation did not match %q in %s", oldValue, path)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyFixtureTree(t *testing.T, source string) string {
	t.Helper()
	target := t.TempDir()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(source, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := os.WriteFile(filepath.Join(target, entry.Name()), data, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	return target
}
