package settings

import (
	"slices"
	"testing"
)

func TestEscalationPolicyNormalizationProducesStableDraftHash(t *testing.T) {
	t.Parallel()

	left := validEscalationDraft()
	left.EscalationPolicies = []EscalationPolicy{
		{
			ID: "ignored-policy-id", ConfigurationRevisionID: "ignored-revision-id",
			Name: "  sustained critical  ", Enabled: true,
			Severities:           []string{"WARNING", "critical", "warning"},
			Namespaces:           []string{"production", " payments ", "production"},
			LabelMatchers:        map[string]string{"team": " platform ", "service": "checkout"},
			MinimumFiringSeconds: 900, MinimumRecurrenceCount: 2, CreateIncident: true,
		},
	}
	right := validEscalationDraft()
	right.EscalationPolicies = []EscalationPolicy{
		{
			Name: "sustained critical", Enabled: true,
			Severities:           []string{"critical", "warning"},
			Namespaces:           []string{"payments", "production"},
			LabelMatchers:        map[string]string{"service": "checkout", "team": "platform"},
			MinimumFiringSeconds: 900, MinimumRecurrenceCount: 2, CreateIncident: true,
		},
	}

	normalizedLeft, leftErrors, leftHash := normalizeDraft(left)
	_, rightErrors, rightHash := normalizeDraft(right)
	if len(leftErrors) != 0 || len(rightErrors) != 0 {
		t.Fatalf("normalization errors: left=%#v right=%#v", leftErrors, rightErrors)
	}
	if leftHash == "" || leftHash != rightHash {
		t.Fatalf("draft hashes differ: left=%q right=%q", leftHash, rightHash)
	}
	policy := normalizedLeft.EscalationPolicies[0]
	if policy.ID != "" || policy.ConfigurationRevisionID != "" {
		t.Fatalf("persisted identities leaked into draft hash: %#v", policy)
	}
	if !slices.Equal(policy.Severities, []string{"critical", "warning"}) {
		t.Fatalf("severities = %#v", policy.Severities)
	}
	if !slices.Equal(policy.Namespaces, []string{"payments", "production"}) {
		t.Fatalf("namespaces = %#v", policy.Namespaces)
	}
}

func TestAutomaticEscalationRequiresEnabledValidPolicy(t *testing.T) {
	t.Parallel()

	draft := validEscalationDraft()
	draft.General.AutomaticEscalationEnabled = true
	draft.EscalationPolicies = nil
	_, fieldErrors, _ := normalizeDraft(draft)
	if !hasFieldError(fieldErrors, "general.automatic_escalation_enabled", "POLICY_REQUIRED") {
		t.Fatalf("field errors = %#v", fieldErrors)
	}

	draft.EscalationPolicies = []EscalationPolicy{{
		Name: "critical production", Enabled: true, Severities: []string{"critical"},
		Namespaces: []string{"production"}, LabelMatchers: map[string]string{"team": "platform"},
		MinimumRecurrenceCount: 1, CreateIncident: true,
	}}
	_, fieldErrors, _ = normalizeDraft(draft)
	if len(fieldErrors) != 0 {
		t.Fatalf("valid enabled policy rejected: %#v", fieldErrors)
	}
}

func TestEscalationPolicyBoundsAreValidated(t *testing.T) {
	t.Parallel()

	draft := validEscalationDraft()
	draft.EscalationPolicies = []EscalationPolicy{{
		Name: "invalid", Enabled: true, Severities: []string{"page"},
		LabelMatchers:        map[string]string{"bad label": "value"},
		MinimumFiringSeconds: 7*24*60*60 + 1, MinimumRecurrenceCount: 0,
		CreateIncident: false,
	}}
	_, fieldErrors, _ := normalizeDraft(draft)
	for _, expected := range []struct{ field, code string }{
		{"escalation_policies.0.severities", "INVALID_SEVERITY"},
		{"escalation_policies.0.label_matchers", "INVALID_MATCHER"},
		{"escalation_policies.0.minimum_firing_seconds", "INVALID_FIRING_DURATION"},
		{"escalation_policies.0.minimum_recurrence_count", "INVALID_RECURRENCE"},
		{"escalation_policies.0.create_incident", "INVALID_POLICY_ACTION"},
	} {
		if !hasFieldError(fieldErrors, expected.field, expected.code) {
			t.Errorf("missing %s/%s in %#v", expected.field, expected.code, fieldErrors)
		}
	}
}

func validEscalationDraft() Draft {
	scope := OperationalScope{Name: "Local", ClusterID: "cloudops-local", Environment: "local", Namespaces: []string{"demo"}}
	return Draft{
		Summary: "Alert escalation policy test",
		General: GeneralConfiguration{
			QueryMaxLookbackSeconds: 3600, QueryMaxResults: 1000, TelemetryRetentionDays: 7,
		},
		Scope: scope, Scopes: []OperationalScope{scope},
	}
}

func hasFieldError(errors []FieldError, field, code string) bool {
	for _, item := range errors {
		if item.Field == field && item.Code == code {
			return true
		}
	}
	return false
}
