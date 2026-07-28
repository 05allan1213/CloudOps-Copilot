package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestBaselineDefaultsAreSafe(t *testing.T) {
	cfg := Load()
	if cfg.K8SWriteEnabled {
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

func TestKubernetesWriteRequiresExplicitNamespaceAllowlist(t *testing.T) {
	cfg := Load()
	cfg.K8SEnabled = true
	cfg.K8SWriteEnabled = true
	cfg.ScenarioState = "active"
	cfg.ScenarioID = "scenario-config-test"
	cfg.K8SAllowedNamespaces = []string{"*"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "explicit namespace allowlists") {
		t.Fatalf("wildcard Kubernetes write accepted: %v", err)
	}
	cfg.K8SAllowedNamespaces = []string{"demo"}
	cfg.K8SDefaultNamespace = "demo"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("bounded authorized Kubernetes operation configuration rejected: %v", err)
	}
}

func TestAgentToolUsesOnlySemanticNames(t *testing.T) {
	unsetForTest(t, "AGENT_TOOL_REGISTRY_ENABLED")
	unsetForTest(t, "AGENT_TOOL_DEFAULT_TIMEOUT_SECONDS")
	unsetForTest(t, "AGENT_TOOL_LOG_ARGS")
	t.Setenv("COPILOT_TOOL_REGISTRY_ENABLED", "false")
	t.Setenv("COPILOT_TOOL_DEFAULT_TIMEOUT_SECONDS", "17")
	t.Setenv("COPILOT_TOOL_LOG_ARGS", "true")

	legacyOnly := Load()
	if !legacyOnly.AgentToolRegistryEnabled || legacyOnly.AgentToolDefaultTimeout != 30*time.Second || legacyOnly.AgentToolLogArgs {
		t.Fatalf("legacy aliases still affected semantic configuration: %+v", legacyOnly)
	}

	t.Setenv("AGENT_TOOL_REGISTRY_ENABLED", "true")
	t.Setenv("AGENT_TOOL_DEFAULT_TIMEOUT_SECONDS", "9")
	t.Setenv("AGENT_TOOL_LOG_ARGS", "false")
	current := Load()
	if !current.AgentToolRegistryEnabled || current.AgentToolDefaultTimeout != 9*time.Second || current.AgentToolLogArgs {
		t.Fatalf("semantic Agent tool names were not applied: %+v", current)
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
