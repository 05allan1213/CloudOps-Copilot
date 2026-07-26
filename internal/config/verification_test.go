package config

import (
	"strings"
	"testing"
	"time"
)

func TestVerificationFlagsDefaultOff(t *testing.T) {
	cfg := Load()
	if cfg.DeliveryTrackingEnabled || cfg.VerificationEnabled {
		t.Fatalf("external verification must default off: %+v", cfg)
	}
}

func TestVerificationFlagsAndTimingFailClosed(t *testing.T) {
	cfg := Load()
	cfg.VerificationEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DELIVERY_TRACKING_ENABLED") {
		t.Fatalf("verification without delivery accepted: %v", err)
	}
	cfg.VerificationEnabled = false
	cfg.DeliveryTrackingEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "requires remediation") {
		t.Fatalf("delivery without remediation dependencies accepted: %v", err)
	}
	cfg.DeliveryPollInterval = 10 * time.Second
	cfg.VerificationLeaseDuration = 10 * time.Second
	cfg.DeliveryTrackingEnabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled verification should remain valid: %v", err)
	}
}
