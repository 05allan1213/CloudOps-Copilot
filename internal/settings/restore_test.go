package settings

import (
	"testing"
	"time"
)

func TestHistoricalProviderRestoreAllowed(t *testing.T) {
	for _, test := range []struct {
		provider   Provider
		historical bool
		want       bool
	}{
		{provider: ProviderGitHub, historical: true, want: true},
		{provider: ProviderArgoCD, historical: true, want: true},
		{provider: ProviderGitHub, historical: false, want: false},
		{provider: ProviderLLM, historical: true, want: false},
		{provider: ProviderKubernetes, historical: true, want: false},
	} {
		if got := historicalProviderRestoreAllowed(test.provider, test.historical); got != test.want {
			t.Errorf("historicalProviderRestoreAllowed(%q, %t)=%t want %t", test.provider, test.historical, got, test.want)
		}
	}
}

func TestValidatedProviderHealthPreservesUnavailableRestore(t *testing.T) {
	checkedAt := time.Date(2026, time.August, 3, 4, 30, 0, 0, time.UTC)
	config := ProviderConfiguration{Provider: ProviderGitHub, Enabled: true}
	state, detail, observedAt, ok := validatedProviderHealth(config, []ProviderResult{{
		Provider:  ProviderGitHub,
		State:     "unavailable",
		Detail:    "Provider health check returned HTTP 401",
		CheckedAt: &checkedAt,
	}})
	if !ok || state != "unavailable" || detail != "Provider health check returned HTTP 401" {
		t.Fatalf("validatedProviderHealth()=(%q, %q, %v, %t)", state, detail, observedAt, ok)
	}
	if got, valid := observedAt.(time.Time); !valid || !got.Equal(checkedAt) {
		t.Fatalf("validatedProviderHealth() checked_at=%v", observedAt)
	}
}
