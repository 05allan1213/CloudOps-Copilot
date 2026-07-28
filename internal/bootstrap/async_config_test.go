package bootstrap

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultAsyncWorkerConfigMatchesV3Contract(t *testing.T) {
	cfg := DefaultAsyncWorkerConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigateMaxInFlight != 2 || cfg.DeliverMaxInFlight != 1 || cfg.ObserveMaxInFlight != 2 || cfg.VerifyMaxInFlight != 2 {
		t.Fatalf("pool defaults=%+v", cfg)
	}
}

func TestAsyncWorkerConfigRejectsInvalidTiming(t *testing.T) {
	tests := []struct {
		name string
		edit func(*AsyncWorkerConfig)
	}{
		{"external deadline", func(c *AsyncWorkerConfig) { c.InvestigateExternalDeadline = c.InvestigateHandlerDeadline }},
		{"handler deadline", func(c *AsyncWorkerConfig) { c.DeliverHandlerDeadline = c.DeliverLease }},
		{"heartbeat", func(c *AsyncWorkerConfig) { c.ObserveHeartbeat = c.ObserveLease/3 + time.Nanosecond }},
		{"drain", func(c *AsyncWorkerConfig) { c.DrainTimeout = 46 * time.Second }},
		{"exit", func(c *AsyncWorkerConfig) { c.ExitDeadline = 44 * time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultAsyncWorkerConfig()
			test.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoadAsyncWorkerConfigRejectsMalformedOverride(t *testing.T) {
	t.Setenv("ASYNC_OBSERVE_HEARTBEAT", "not-a-duration")
	if _, err := LoadAsyncWorkerConfig(); err == nil || !strings.Contains(err.Error(), "ASYNC_OBSERVE_HEARTBEAT") {
		t.Fatalf("error=%v", err)
	}
}
