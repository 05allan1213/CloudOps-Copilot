package config

import "testing"

func TestPhase4AuthorityFlagsDefaultOff(t *testing.T) {
	cfg := Load()
	if cfg.RemediationEnabled || cfg.GitOpsPREnabled || cfg.GitHubWriteEnabled {
		t.Fatalf("Phase 4 authority must default off: %+v", cfg)
	}
}

func TestPhase4FlagsCannotBeEnabledOutOfOrder(t *testing.T) {
	cfg := Load()
	cfg.GitHubWriteEnabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("GitHub write enabled without remediation and GitOps PR")
	}
}
