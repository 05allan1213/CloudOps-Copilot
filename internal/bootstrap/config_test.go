package bootstrap

import (
	"testing"
)

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
	if string(workerConfig.RuntimeGeneration) != "compatibility" {
		t.Fatalf("runtime generation=%q", workerConfig.RuntimeGeneration)
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
	t.Setenv("V3_WORKER_PROVIDERS_ENABLED", "sometimes")
	if _, err := LoadWorkerConfig(); err == nil {
		t.Fatal("expected malformed V3 provider flag to fail closed")
	}
}
