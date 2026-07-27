package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestBaselineDefaultsAreSafe(t *testing.T) {
	cfg := Load()
	if cfg.FastDemoEnabled || cfg.FastDemoConfirmDisposable || cfg.K8SWriteEnabled {
		t.Fatalf("unsafe defaults enabled: %+v", cfg)
	}
	if !cfg.AgentToolRegistryEnabled || cfg.AgentToolDefaultTimeout != 30*time.Second || cfg.AgentToolLogArgs {
		t.Fatalf("unexpected Agent tool defaults: enabled=%v timeout=%v log_args=%v", cfg.AgentToolRegistryEnabled, cfg.AgentToolDefaultTimeout, cfg.AgentToolLogArgs)
	}
	if !cfg.RateLimit.Enabled {
		t.Fatal("rate limiting must default on")
	}
}

func TestUnavailablePrometheusDoesNotBlockCoreRuntime(t *testing.T) {
	cfg := Load()
	cfg.PrometheusURL = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unconfigured optional Prometheus provider rejected: %v", err)
	}
}

func TestFastDemoRequiresExplicitDisposableLocalEnvironment(t *testing.T) {
	base := Load()
	base.FastDemoEnabled = true
	base.IncidentAgentEnabled = true
	base.K8SEnabled = true
	base.K8SWriteEnabled = true
	base.K8SInCluster = false
	base.K8SKubeconfig = "/tmp/demo-kubeconfig"
	base.MySQLHost = "mysql"
	base.FastDemoRevision = strings.Repeat("a", 40)
	base.FastDemoCluster = "kind-cloudops-demo"
	base.FastDemoNamespace = "cloudops-demo"
	base.FastDemoWorkload = "cloudops-demo-workload"
	base.LLMAPIKey = ""

	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "APP_ENV=local-demo") {
		t.Fatalf("Fast Demo without environment guard accepted: %v", err)
	}
	base.AppEnv = "local-demo"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "FAST_DEMO_CONFIRM_DISPOSABLE") {
		t.Fatalf("Fast Demo without disposable confirmation accepted: %v", err)
	}
	base.FastDemoConfirmDisposable = true
	if err := base.Validate(); err != nil {
		t.Fatalf("valid local demo configuration rejected: %v", err)
	}
}

func TestKubernetesWriteRequiresExplicitNamespaceAllowlist(t *testing.T) {
	cfg := Load()
	cfg.K8SEnabled = true
	cfg.K8SWriteEnabled = true
	cfg.K8SAllowedNamespaces = []string{"*"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "explicit namespace allowlists") {
		t.Fatalf("wildcard Kubernetes write accepted: %v", err)
	}
	cfg.K8SAllowedNamespaces = []string{"default"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("bounded authorized Kubernetes operation configuration rejected: %v", err)
	}
}

func TestAgentToolLegacyAliasesAndNewNamePriority(t *testing.T) {
	unsetForTest(t, "AGENT_TOOL_REGISTRY_ENABLED")
	unsetForTest(t, "AGENT_TOOL_DEFAULT_TIMEOUT_SECONDS")
	unsetForTest(t, "AGENT_TOOL_LOG_ARGS")
	t.Setenv("COPILOT_TOOL_REGISTRY_ENABLED", "false")
	t.Setenv("COPILOT_TOOL_DEFAULT_TIMEOUT_SECONDS", "17")
	t.Setenv("COPILOT_TOOL_LOG_ARGS", "true")

	legacy := Load()
	if legacy.AgentToolRegistryEnabled || legacy.AgentToolDefaultTimeout != 17*time.Second || !legacy.AgentToolLogArgs {
		t.Fatalf("legacy aliases were not applied: %+v", legacy)
	}

	t.Setenv("AGENT_TOOL_REGISTRY_ENABLED", "true")
	t.Setenv("AGENT_TOOL_DEFAULT_TIMEOUT_SECONDS", "9")
	t.Setenv("AGENT_TOOL_LOG_ARGS", "false")
	current := Load()
	if !current.AgentToolRegistryEnabled || current.AgentToolDefaultTimeout != 9*time.Second || current.AgentToolLogArgs {
		t.Fatalf("new Agent tool names did not take priority: %+v", current)
	}
}

func TestFastDemoReplicaLegacyAliasAndNewNamePriority(t *testing.T) {
	unsetForTest(t, "FAST_DEMO_MAX_REPLICAS")
	t.Setenv("ACTION_MAX_REPLICAS", "12")
	if cfg := Load(); cfg.FastDemoMaxReplicas != 12 {
		t.Fatalf("legacy Fast Demo replica alias=%d, want 12", cfg.FastDemoMaxReplicas)
	}
	t.Setenv("FAST_DEMO_MAX_REPLICAS", "7")
	if cfg := Load(); cfg.FastDemoMaxReplicas != 7 {
		t.Fatalf("new Fast Demo replica limit=%d, want 7", cfg.FastDemoMaxReplicas)
	}
}

func unsetForTest(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
