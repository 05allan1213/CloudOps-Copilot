package config

import (
	"strings"
	"testing"
)

func TestV2BaselineDefaultsAreSafe(t *testing.T) {
	cfg := Load()
	if cfg.FastDemoEnabled || cfg.FastDemoConfirmDisposable || cfg.ActionExecutionEnabled || cfg.K8SWriteEnabled || cfg.CopilotEnabled {
		t.Fatalf("unsafe or legacy defaults enabled: %+v", cfg)
	}
	if !cfg.AuthEnabled || !cfg.RateLimit.Enabled {
		t.Fatalf("formal security defaults must stay enabled: auth=%v rate_limit=%v", cfg.AuthEnabled, cfg.RateLimit.Enabled)
	}
}

func TestFastDemoRequiresExplicitDisposableLocalEnvironment(t *testing.T) {
	base := Load()
	base.AuthEnabled = false
	base.FastDemoEnabled = true
	base.IncidentAgentEnabled = true
	base.CopilotEnabled = true
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

func TestFormalKubernetesAndLegacyActionWritesFailClosed(t *testing.T) {
	cfg := Load()
	cfg.AuthEnabled = false
	cfg.K8SEnabled = true
	cfg.K8SWriteEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "guarded local Fast Demo") {
		t.Fatalf("formal Kubernetes write accepted: %v", err)
	}
	cfg.K8SWriteEnabled = false
	cfg.ActionExecutionEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("legacy action execution accepted: %v", err)
	}
}
