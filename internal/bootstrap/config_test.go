package bootstrap

import "testing"

func TestLoadProcessSpecificConfig(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("WORKER_MANAGEMENT_ADDR", "127.0.0.1:18081")

	workerConfig, err := LoadWorkerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if workerConfig.ManagementAddr != "127.0.0.1:18081" {
		t.Fatalf("management address=%q", workerConfig.ManagementAddr)
	}
	if workerConfig.ProviderGatewayEnabled {
		t.Fatal("Provider Gateway must remain disabled when it is not configured")
	}
}

func TestWorkerConfigRejectsInvalidManagementAddress(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("WORKER_MANAGEMENT_ADDR", "not-an-address")
	if _, err := LoadWorkerConfig(); err == nil {
		t.Fatal("expected invalid worker management address to fail")
	}
}

func TestWorkerConfigRejectsMalformedProductionProviderFlag(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("PROVIDER_GATEWAY_ENABLED", "sometimes")
	if _, err := LoadWorkerConfig(); err == nil {
		t.Fatal("expected malformed provider flag to fail closed")
	}
}
