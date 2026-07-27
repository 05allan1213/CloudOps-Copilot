package alert

import (
	"database/sql"
	"testing"
	"time"
)

func TestPolicyMatchesEveryBoundedCondition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	policy := escalationPolicyRow{
		Severities: []string{"critical"}, Namespaces: []string{"production"},
		LabelMatchers:        map[string]string{"team": "platform", "service": "checkout"},
		MinimumFiringSeconds: 600, MinimumRecurrence: 2,
	}
	row := alertRow{
		Status: "firing", Severity: "critical", Namespace: "production", Recurrence: 2,
		StartsAt: now.Add(-10 * time.Minute), ResolvedAt: sql.NullTime{},
	}
	labels := map[string]string{"team": "platform", "service": "checkout"}
	if !policyMatches(policy, row, labels, now) {
		t.Fatal("expected exact policy match")
	}

	tests := []struct {
		name   string
		mutate func(*escalationPolicyRow, *alertRow, map[string]string, *time.Time)
	}{
		{"severity", func(_ *escalationPolicyRow, row *alertRow, _ map[string]string, _ *time.Time) {
			row.Severity = "warning"
		}},
		{"namespace", func(_ *escalationPolicyRow, row *alertRow, _ map[string]string, _ *time.Time) {
			row.Namespace = "staging"
		}},
		{"recurrence", func(_ *escalationPolicyRow, row *alertRow, _ map[string]string, _ *time.Time) { row.Recurrence = 1 }},
		{"duration", func(_ *escalationPolicyRow, row *alertRow, _ map[string]string, _ *time.Time) {
			row.StartsAt = now.Add(-599 * time.Second)
		}},
		{"label", func(_ *escalationPolicyRow, _ *alertRow, labels map[string]string, _ *time.Time) {
			labels["team"] = "other"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidatePolicy, candidateRow, candidateNow := policy, row, now
			candidateLabels := map[string]string{"team": "platform", "service": "checkout"}
			test.mutate(&candidatePolicy, &candidateRow, candidateLabels, &candidateNow)
			if policyMatches(candidatePolicy, candidateRow, candidateLabels, candidateNow) {
				t.Fatal("unexpected match")
			}
		})
	}
}

func TestPolicyMatchesAllowsUnboundedNamespaceAndDuration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	policy := escalationPolicyRow{Severities: []string{"warning"}, MinimumRecurrence: 1}
	row := alertRow{Severity: "warning", Namespace: "any", Recurrence: 1, StartsAt: now}
	if !policyMatches(policy, row, map[string]string{}, now) {
		t.Fatal("zero duration and empty namespace bounds should match")
	}
}
