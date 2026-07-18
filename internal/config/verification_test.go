package config

import (
	"strings"
	"testing"
	"time"
)

func TestPhase5FlagsDefaultOff(t *testing.T) {
	cfg := Load()
	cfg.AuthEnabled = false
	if cfg.DeliveryTrackingEnabled || cfg.VerificationEnabled {
		t.Fatalf("Phase 5 external observation must default off: %+v", cfg)
	}
}

func TestPhase5FlagsAndTimingFailClosed(t *testing.T) {
	cfg := Load()
	cfg.AuthEnabled = false
	cfg.VerificationEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DELIVERY_TRACKING_ENABLED") {
		t.Fatalf("verification without delivery accepted: %v", err)
	}
	cfg.VerificationEnabled = false
	cfg.DeliveryTrackingEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "requires remediation") {
		t.Fatalf("delivery without Phase 1-4 dependencies accepted: %v", err)
	}
	cfg.DeliveryPollInterval = 10 * time.Second
	cfg.VerificationLeaseDuration = 10 * time.Second
	cfg.DeliveryTrackingEnabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled Phase 5 should preserve compatibility: %v", err)
	}
}

func TestPhase6ProfilesRejectUnknownFieldsRawQueriesAndNonFiniteThresholds(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown":      `{"profiles":[],"raw_query":"up"}`,
		"raw template": `{"profiles":[{"id":"p","service":"s","environment":"staging","namespace":"n","workload":"w","templates":[{"id":"t","type":"metric_error_rate_below","required":true,"comparison":"lt","threshold":0.1,"lookback_seconds":300,"timeout_seconds":600,"stability_seconds":120,"promql":"up"}]}]}`,
		"nan":          `{"profiles":[{"id":"p","service":"s","environment":"staging","namespace":"n","workload":"w","templates":[{"id":"t","type":"metric_error_rate_below","required":true,"comparison":"lt","threshold":"NaN","lookback_seconds":300,"timeout_seconds":600,"stability_seconds":120}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Load()
			cfg.AuthEnabled = false
			cfg.VerificationProfilesJSON = raw
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "VERIFICATION_PROFILES_JSON") {
				t.Fatalf("unsafe profile accepted: %v", err)
			}
		})
	}
}
