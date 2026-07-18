package config

import (
	"strings"
	"testing"
	"time"
)

func TestIncidentAggregationWindowDefaultAndValidation(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("INCIDENT_AGGREGATION_WINDOW_SECONDS", "")
	cfg := Load()
	if cfg.IncidentAggregationWindow != 4*time.Hour {
		t.Fatalf("default=%v want=4h", cfg.IncidentAggregationWindow)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config validation failed: %v", err)
	}

	t.Setenv("INCIDENT_AGGREGATION_WINDOW_SECONDS", "30")
	cfg = Load()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "INCIDENT_AGGREGATION_WINDOW_SECONDS") {
		t.Fatalf("expected aggregation window validation error, got %v", err)
	}
}
