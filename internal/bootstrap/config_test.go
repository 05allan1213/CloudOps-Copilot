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
}

func TestWorkerConfigRejectsInvalidManagementAddress(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("WORKER_MANAGEMENT_ADDR", "not-an-address")
	if _, err := LoadWorkerConfig(); err == nil {
		t.Fatal("expected invalid worker management address to fail")
	}
}
