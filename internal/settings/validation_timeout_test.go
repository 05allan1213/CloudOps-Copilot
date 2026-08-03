package settings

import "testing"

func TestProviderTimeoutAllowsBoundedLongRunningModelRequests(t *testing.T) {
	draft := Draft{
		Summary: "Allow a bounded long-running model request",
		General: GeneralConfiguration{
			QueryMaxLookbackSeconds: 3600,
			QueryMaxResults:         1000,
			TelemetryRetentionDays:  7,
		},
		Scope: OperationalScope{
			Name: "Local", ClusterID: "cloudops-local", Environment: "local", Namespaces: []string{"demo"},
		},
	}
	draft.Scopes = []OperationalScope{draft.Scope}
	for _, provider := range operationalProviders {
		configuration := defaultProviderConfiguration(provider)
		configuration.Enabled = false
		draft.Providers = append(draft.Providers, configuration)
	}

	draft.Providers[0].TimeoutMS = 180000
	_, fieldErrors, _ := normalizeDraft(draft)
	for _, fieldError := range fieldErrors {
		if fieldError.Field == "providers.llm.timeout_ms" {
			t.Fatalf("180 second Provider timeout rejected: %+v", fieldError)
		}
	}

	draft.Providers[0].TimeoutMS = maximumProviderTimeoutMS + 1
	_, fieldErrors, _ = normalizeDraft(draft)
	for _, fieldError := range fieldErrors {
		if fieldError.Field == "providers.llm.timeout_ms" && fieldError.Code == "INVALID_TIMEOUT" {
			return
		}
	}
	t.Fatal("Provider timeout above five minutes was not rejected")
}
