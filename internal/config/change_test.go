package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestChangeIntegrationsDefaultOff(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	cfg := Load()
	if cfg.ChangeIntelligenceEnabled || cfg.GitHubEnabled || cfg.ArgoCDEnabled || cfg.ImageRevisionRequired || cfg.RegistryMetadataEnabled {
		t.Fatalf("Phase 3 external integrations must default off: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentWorkerIDDefaultsToReplicaHostname(t *testing.T) {
	prior, existed := os.LookupEnv("INCIDENT_AGENT_WORKER_ID")
	if err := os.Unsetenv("INCIDENT_AGENT_WORKER_ID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("INCIDENT_AGENT_WORKER_ID", prior)
		} else {
			_ = os.Unsetenv("INCIDENT_AGENT_WORKER_ID")
		}
	})
	t.Setenv("HOSTNAME", "cloudops-worker-replica-a")
	if got := Load().IncidentAgentWorkerID; got != "cloudops-worker-replica-a" {
		t.Fatalf("worker id=%q", got)
	}
}

func TestEffectiveChangeConfigOmitsCredentialReferences(t *testing.T) {
	cfg := Config{GitHubPrivateKeyFile: "/secret/private.pem", GitHubTokenFile: "/secret/github-token", ArgoCDTokenFile: "/secret/argo-token", RegistryBearerTokenFile: "/secret/registry-token", RegistryUsernameFile: "/secret/username", RegistryPasswordFile: "/secret/password"}
	raw, err := json.Marshal(cfg.EffectiveChangeConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private.pem", "github-token", "argo-token", "registry-token", "/secret/username", "/secret/password", "private_key", "token_file"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("effective config leaked %q: %s", forbidden, raw)
		}
	}
}

func TestRegistryMetadataConfigurationIsBoundedAndCredentialModesAreExclusive(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("MYSQL_HOST", "mysql")
	t.Setenv("CHANGE_INTELLIGENCE_ENABLED", "true")
	t.Setenv("CHANGE_SERVICE_MAPPINGS_JSON", `{"api":{"repository":{"owner":"acme","name":"api"},"argocd_application":"api","argocd_project":"prod","gitops_path":"apps/api"}}`)
	t.Setenv("IMAGE_REVISION_REQUIRED", "true")
	t.Setenv("IMAGE_ALLOWED_REGISTRIES", "registry.example")
	t.Setenv("REGISTRY_METADATA_ENABLED", "true")
	t.Setenv("REGISTRY_BASE_URL", "https://registry.example")
	t.Setenv("REGISTRY_ALLOWED_HOSTS", "registry.example")
	t.Setenv("REGISTRY_ALLOWED_REPOSITORIES", "acme/api")
	t.Setenv("REGISTRY_ALLOWED_AUTH_REALM_HOSTS", "auth.registry.example")
	t.Setenv("OCI_ALLOWED_SOURCE_REPOSITORIES", "https://github.com/acme/api")
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid anonymous/challenge registry configuration rejected: %v", err)
	}
	t.Setenv("REGISTRY_BEARER_TOKEN_FILE", "/var/run/secrets/registry/token")
	t.Setenv("REGISTRY_USERNAME_FILE", "/var/run/secrets/registry/username")
	t.Setenv("REGISTRY_PASSWORD_FILE", "/var/run/secrets/registry/password")
	cfg = Load()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("mixed registry authentication accepted: %v", err)
	}
}

func TestChangeIntegrationRequiresPersistenceMappingAndReadCredentials(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("CHANGE_INTELLIGENCE_ENABLED", "true")
	cfg := Load()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "MySQL") {
		t.Fatalf("expected persistence validation, got %v", err)
	}
	t.Setenv("MYSQL_HOST", "mysql")
	t.Setenv("CHANGE_SERVICE_MAPPINGS_JSON", `{"api":{"repository":{"owner":"acme","name":"api"},"argocd_application":"api","argocd_project":"prod","gitops_path":"apps/api"}}`)
	t.Setenv("GITHUB_ENABLED", "true")
	t.Setenv("GITHUB_ALLOWED_OWNERS", "acme")
	t.Setenv("GITHUB_ALLOWED_REPOSITORIES", "acme/api")
	cfg = Load()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "authentication mode") {
		t.Fatalf("expected GitHub credential validation, got %v", err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", "/var/run/secrets/github/token")
	t.Setenv("ARGOCD_ENABLED", "true")
	t.Setenv("ARGOCD_SERVER", "http://argocd.example")
	t.Setenv("ARGOCD_TOKEN_FILE", "/var/run/secrets/argocd/token")
	t.Setenv("ARGOCD_ALLOWED_APPLICATIONS", "api")
	t.Setenv("ARGOCD_ALLOWED_PROJECTS", "prod")
	cfg = Load()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "argocd") {
		t.Fatalf("expected Argo CD HTTPS validation, got %v", err)
	}
	t.Setenv("ARGOCD_SERVER", "https://argocd.example")
	cfg = Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid read-only Phase 3 configuration rejected: %v", err)
	}
}
