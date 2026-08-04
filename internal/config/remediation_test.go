package config

import "testing"

func TestRemediationAuthorityFlagsDefaultOff(t *testing.T) {
	cfg := Load()
	if cfg.RemediationEnabled || cfg.GitOpsPREnabled || cfg.GitHubWriteEnabled {
		t.Fatalf("remediation authority must default off: %+v", cfg)
	}
}

func TestRemediationFlagsCannotBeEnabledOutOfOrder(t *testing.T) {
	cfg := Load()
	cfg.GitHubWriteEnabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("GitHub write enabled without remediation and GitOps PR")
	}
}
