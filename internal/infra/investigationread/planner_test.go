package investigationread

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

func TestGoldenAcquisitionPlannerUsesFrozenSixToolOrderAndCurrentChangeRef(t *testing.T) {
	planner, err := NewGoldenAcquisitionPlanner(GoldenActionPolicies())
	if err != nil {
		t.Fatal(err)
	}
	const scopeRef = "scope-1"
	const changeRef = "11111111-1111-4111-8111-111111111111"
	state := agent.InvestigationState{}
	facts := []agent.EvidenceFact{{
		Type:       "deployment.change_ref",
		Attributes: map[string]string{"is_current": "true", "change_ref": changeRef},
	}}
	want := []string{
		ToolInspectWorkload,
		ToolQueryMetrics,
		ToolQueryLogs,
		ToolQueryTraces,
		ToolGetDeploymentContext,
		ToolGetChangeDetail,
	}
	got := make([]string, 0, len(want))
	for range want {
		action, nextErr := planner.NextAction(state, facts, scopeRef)
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		if action == nil {
			t.Fatal("planner exhausted before the frozen acquisition sequence completed")
		}
		got = append(got, action.Tool)
		if action.ScopeRef != scopeRef {
			t.Fatalf("scope_ref=%q", action.ScopeRef)
		}
		if action.Tool == ToolGetChangeDetail {
			var parameters map[string]string
			if err := json.Unmarshal(action.BoundedParameters, &parameters); err != nil {
				t.Fatal(err)
			}
			if parameters["change_ref"] != changeRef {
				t.Fatalf("change_ref=%q", parameters["change_ref"])
			}
		}
		state.ToolAttempts = append(state.ToolAttempts, agent.ToolAttempt{Tool: action.Tool})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool order=%v want=%v", got, want)
	}
	action, err := planner.NextAction(state, facts, scopeRef)
	if err != nil || action != nil {
		t.Fatalf("exhausted action=%+v err=%v", action, err)
	}
}

func TestGoldenAcquisitionPlannerRequiresExactlyOneCurrentChangeRef(t *testing.T) {
	planner, err := NewGoldenAcquisitionPlanner(GoldenActionPolicies())
	if err != nil {
		t.Fatal(err)
	}
	state := agent.InvestigationState{ToolAttempts: []agent.ToolAttempt{
		{Tool: ToolInspectWorkload},
		{Tool: ToolQueryMetrics},
		{Tool: ToolQueryLogs},
		{Tool: ToolQueryTraces},
		{Tool: ToolGetDeploymentContext},
	}}
	fact := agent.EvidenceFact{Type: "deployment.change_ref", Attributes: map[string]string{
		"is_current": "true", "change_ref": "11111111-1111-4111-8111-111111111111",
	}}
	if action, err := planner.NextAction(state, nil, "scope-1"); err != nil || action != nil {
		t.Fatalf("missing current fact action=%+v err=%v", action, err)
	}
	if _, err := planner.NextAction(state, []agent.EvidenceFact{fact, fact}, "scope-1"); !errors.Is(err, agent.ErrConflict) {
		t.Fatalf("duplicate current facts error=%v", err)
	}
}
