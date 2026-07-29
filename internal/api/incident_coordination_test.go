package api

import (
	"reflect"
	"testing"
	"time"
)

func TestHistoricalAgentRunOutcomeFallbackIsTerminalOnly(t *testing.T) {
	tests := []struct {
		name, explicit, status, diagnosis, want string
	}{
		{name: "explicit wins", explicit: "diagnosed", status: "completed", want: "diagnosed"},
		{name: "completed diagnosis", status: "completed", diagnosis: `{"candidate":{"summary":"validated"}}`, want: "diagnosed"},
		{name: "completed degraded", status: "completed", diagnosis: `{"summary":"insufficient","degraded":true}`, want: "insufficient"},
		{name: "failed", status: "failed", want: "failed"},
		{name: "cancelled", status: "cancelled", want: "cancelled"},
		{name: "active remains empty", status: "running", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := historicalAgentRunOutcome(test.explicit, test.status, []byte(test.diagnosis)); got != test.want {
				t.Fatalf("outcome=%q want=%q", got, test.want)
			}
		})
	}
}

func TestMissingOperationalScopeProducesZeroLinksAndNilDecisionPointer(t *testing.T) {
	if got := coordinationLink("agent", "/agent", map[string]string{"incident": "i"}, ""); !reflect.DeepEqual(got, IncidentContextLinkView{}) {
		t.Fatalf("missing-scope link=%+v", got)
	}
	if got := coordinationLinkPointer("agent", "/agent", nil, ""); got != nil {
		t.Fatalf("missing-scope pointer=%+v", got)
	}
	if got := coordinationLinkPointer("agent", "/agent", nil, "11111111-1111-4111-8111-111111111111"); got == nil || got.OperationalScopeID == "" {
		t.Fatalf("scoped pointer=%+v", got)
	}
	valid := IncidentAlertRelationView{
		ID: "11111111-1111-4111-8111-111111111111", Cycle: 1,
		AlertID: "22222222-2222-4222-8222-222222222222", Status: "firing", Severity: "warning",
		CreatedAt: time.Now().UTC(), ContextLink: IncidentContextLinkView{},
	}
	if err := validateIncidentAlertRelations([]IncidentAlertRelationView{valid}); err != nil {
		t.Fatalf("zero alert context link rejected: %v", err)
	}
}
