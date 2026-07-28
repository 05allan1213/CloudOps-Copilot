package alert

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIncidentCorrelationKeyIsolatesScenarioIdentity(t *testing.T) {
	base := strings.Repeat("a", 64)
	live := alertRow{CorrelationKey: base, Labels: json.RawMessage(`{"service":"checkout"}`)}
	first := alertRow{CorrelationKey: base, Labels: json.RawMessage(`{"scenario_id":"scenario-first"}`)}
	second := alertRow{CorrelationKey: base, Labels: json.RawMessage(`{"scenario_id":"scenario-second"}`)}
	repeated := alertRow{CorrelationKey: base, Labels: json.RawMessage(`{"scenario_id":"scenario-first","service":"checkout"}`)}

	if got := incidentCorrelationKey(live); got != base {
		t.Fatalf("Live Mode correlation key=%q want=%q", got, base)
	}
	firstKey := incidentCorrelationKey(first)
	if len(firstKey) != 64 || firstKey == base {
		t.Fatalf("Scenario correlation key=%q", firstKey)
	}
	if got := incidentCorrelationKey(repeated); got != firstKey {
		t.Fatalf("same Scenario correlation key drifted: %q != %q", got, firstKey)
	}
	if got := incidentCorrelationKey(second); got == firstKey || got == base {
		t.Fatalf("distinct Scenario correlation key was not isolated: %q", got)
	}
}

func TestAlertScenarioIDRequiresBoundedCanonicalIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		labels string
		want   string
	}{
		{name: "valid", labels: `{"scenario_id":"scenario-20260728033726-0949eabb"}`, want: "scenario-20260728033726-0949eabb"},
		{name: "missing", labels: `{"service":"checkout"}`},
		{name: "invalid uppercase", labels: `{"scenario_id":"Scenario-invalid"}`},
		{name: "invalid trailing separator", labels: `{"scenario_id":"scenario-invalid-"}`},
		{name: "invalid JSON", labels: `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := alertScenarioID(json.RawMessage(test.labels)); got != test.want {
				t.Fatalf("scenario identity=%q want=%q", got, test.want)
			}
		})
	}
}
