package bootstrap

import (
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
)

func validBaselineVerifierConfig() BaselineVerifierConfig {
	return BaselineVerifierConfig{
		Application: appconfig.Config{
			AppEnv:    "local-demo",
			MySQLHost: "mysql", MySQLPort: "3306", MySQLUser: "cloudops-baseline", MySQLPassword: "test-only", MySQLDatabase: "cloudops",
			MySQLStartupTimeout: 5 * time.Second, MySQLPingTimeout: 3 * time.Second,
			K8SEnabled: true, K8SInCluster: true, K8SAllowedNamespaces: []string{"demo"}, K8SRequestTimeout: 10 * time.Second,
			GitHubAPIBaseURL: "https://api.github.com", GitHubTimeout: 10 * time.Second, GitHubMaxRetries: 1,
			ArgoCDServer: "https://argocd-server.cloudops-argocd.svc", ArgoCDTokenFile: "/var/run/secrets/baseline/argocd-token",
			ArgoCDAllowedApplications: []string{"cloudops-demo"}, ArgoCDAllowedProjects: []string{"cloudops-demo"}, ArgoCDTimeout: 10 * time.Second,
			ArgoCDMaxResources: 100, ArgoCDMaxDiffBytes: 128 * 1024,
			RegistryBaseURL: "https://ghcr.io", RegistryAllowedHosts: []string{"ghcr.io"}, RegistryAllowedRepos: []string{"acme/demo"},
			RegistryAllowedAuthRealms: []string{"ghcr.io"}, OCIAllowedSources: []string{"https://github.com/acme/source"},
			RegistryTimeout: 10 * time.Second, RegistryMaxRetries: 1, RegistryManifestMaxBytes: 4 * 1024 * 1024,
			RegistryConfigMaxBytes: 1024 * 1024, RegistryCacheTTL: 5 * time.Minute, RegistryCacheMaxItems: 16,
			ObservabilityPrometheusURL: "http://prometheus.monitoring.svc:9090", ObservabilityTempoURL: "http://tempo.monitoring.svc:3200",
			ObservabilityRequestTimeout: 10 * time.Second, ObservabilityMaxLookback: time.Hour,
			ObservabilityMaxResponseBytes: 256 * 1024, ObservabilityMaxSamples: 1000, ObservabilityMaxSeries: 20,
			ObservabilityMaxTraces: 100, ObservabilityMaxRetries: 1,
		},
		Target: baseline.Target{
			Cluster: "cloudops-local", Environment: "local-demo", Namespace: "demo", WorkloadKind: "Deployment",
			WorkloadName: "demo", ContainerName: "demo", Repository: "acme/gitops",
			BaseBranch: "main", TargetPath: "apps/demo/deployment.yaml",
		},
		Service: "demo", ArgoPath: "apps/demo", ArgoDestinationServer: "https://kubernetes.default.svc",
		ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo", SourceRepository: change.RepositoryRef{Owner: "acme", Name: "source"},
		AlertNames: []string{"CloudOpsDemoRequiredEnvMissing"}, Lookback: 30 * time.Minute, CommandTimeout: 3 * time.Minute,
		ElasticsearchURL: "https://elasticsearch-es-http.cloudops-logging.svc:9200", ElasticsearchIndex: "logs-cloudops-*",
		ElasticsearchUsernameFile: "/var/run/secrets/baseline/elasticsearch-username",
		ElasticsearchPasswordFile: "/var/run/secrets/baseline/elasticsearch-password",
		ElasticsearchCAFile:       "/var/run/secrets/baseline/elasticsearch-ca.crt",
	}
}

func TestBaselineVerifierConfigRequiresLeastPrivilegeIdentity(t *testing.T) {
	cfg := validBaselineVerifierConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	cfg.Application.LLMAPIKey = "forbidden"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must not receive") {
		t.Fatalf("LLM credential was accepted: %v", err)
	}
	cfg = validBaselineVerifierConfig()
	cfg.Application.K8SWriteEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Kubernetes write identity was accepted: %v", err)
	}
}

func TestLoadBaselineVerifierConfigRejectsAmbientGitHubCredential(t *testing.T) {
	t.Setenv("GITHUB_PRIVATE_KEY_FILE", "/tmp/forbidden.pem")
	if _, err := LoadBaselineVerifierConfig(); err == nil || !strings.Contains(err.Error(), "must not be configured") {
		t.Fatalf("ambient GitHub credential was accepted: %v", err)
	}
}
