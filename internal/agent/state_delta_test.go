package agent

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestReduceStateDeltaAppliesOnlyValidatedOperations(t *testing.T) {
	state := reducerState()
	evidence := reducerEvidence("fact-1", "incident-1", 3)
	policy := ReducerPolicy{
		AllowedActions: map[string]ToolActionPolicy{
			"inspect_workload": {TemplateIDs: []string{"workload-snapshot/v1"}, ParameterKeys: []string{"include_events"}, ExpectedFactTypes: []string{"workload.subject_confirmed"}},
		},
		AllowedScopes: map[string]struct{}{"scope-1": {}},
		Evidence:      map[string]EvidenceFact{evidence.ID: evidence},
		StepUsage:     Usage{Steps: 1, ModelCalls: 1, InputTokens: 10, OutputTokens: 5},
	}
	delta := StateDelta{
		SchemaVersion:          InvestigationStateSchemaVersion,
		BasisCheckpointVersion: 4,
		HypothesisOps:          []HypothesisOp{{Operation: HypothesisSupport, ID: "h-1", EvidenceIDs: []string{"fact-1"}}},
		QuestionOps:            []QuestionOp{{Operation: QuestionAnswer, ID: "q-1", Answer: "The pod spec lacks REQUIRED_ENV.", EvidenceIDs: []string{"fact-1"}}},
		ProposedAction: &ProposedAction{
			Tool: "inspect_workload", ScopeRef: "scope-1", TemplateID: "workload-snapshot/v1",
			BoundedParameters: json.RawMessage(`{"include_events":false}`),
			ExpectedFactTypes: []string{"workload.subject_confirmed"}, PurposeSummary: "Confirm the bounded workload state",
		},
		ProposedStop: StopContinue,
	}

	got, hash, err := ReduceStateDelta(state, delta, policy)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || got.LastAppliedDelta != hash || got.CheckpointVersion != 5 || got.NextNode != NodeExecuteTool {
		t.Fatalf("unexpected reduced state: hash=%q checkpoint=%d node=%s", hash, got.CheckpointVersion, got.NextNode)
	}
	if got.Hypotheses[0].Status != HypothesisSupported || len(got.Hypotheses[0].EvidenceID) != 1 {
		t.Fatalf("hypothesis not reduced: %+v", got.Hypotheses[0])
	}
	if got.Questions[0].Answer == "" || len(got.Questions[0].EvidenceID) != 1 {
		t.Fatalf("question not reduced: %+v", got.Questions[0])
	}
	if len(got.ToolAttempts) != 1 || got.ToolAttempts[0].Signature == "" || got.Usage.ModelCalls != 1 || got.Usage.Steps != 1 {
		t.Fatalf("action or usage not reduced: attempts=%+v usage=%+v", got.ToolAttempts, got.Usage)
	}
	if state.Hypotheses[0].Status != HypothesisActive || state.CheckpointVersion != 4 {
		t.Fatal("input state was mutated")
	}
}

func TestReduceStateDeltaRejectsStaleScopeEvidenceAndDuplicateAction(t *testing.T) {
	state := reducerState()
	current := reducerEvidence("fact-current", "incident-1", 3)
	foreign := reducerEvidence("fact-foreign", "incident-2", 3)
	basePolicy := ReducerPolicy{
		AllowedActions: map[string]ToolActionPolicy{
			"query_metrics": {TemplateIDs: []string{"readiness/v1"}, ParameterKeys: []string{"status"}, ExpectedFactTypes: []string{"metric.readiness_or_5xx_failure"}},
		},
		AllowedScopes: map[string]struct{}{"scope-1": {}},
		Evidence:      map[string]EvidenceFact{current.ID: current, foreign.ID: foreign},
		StepUsage:     Usage{Steps: 1},
	}
	action := &ProposedAction{Tool: "query_metrics", ScopeRef: "scope-1", TemplateID: "readiness/v1", BoundedParameters: json.RawMessage(`{"status":"firing"}`), ExpectedFactTypes: []string{"metric.readiness_or_5xx_failure"}, PurposeSummary: "Check the frozen metric template"}

	tests := []struct {
		name  string
		delta StateDelta
		want  error
	}{
		{name: "stale basis", delta: StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 3, ProposedStop: StopContinue, ProposedAction: action}, want: ErrConflict},
		{name: "foreign evidence", delta: StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 4, HypothesisOps: []HypothesisOp{{Operation: HypothesisSupport, ID: "h-1", EvidenceIDs: []string{"fact-foreign"}}}, ProposedStop: StopContinue, ProposedAction: action}, want: ErrPermission},
		{name: "scope escape", delta: StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 4, ProposedStop: StopContinue, ProposedAction: &ProposedAction{Tool: action.Tool, ScopeRef: "scope-other", TemplateID: action.TemplateID, BoundedParameters: action.BoundedParameters, ExpectedFactTypes: action.ExpectedFactTypes, PurposeSummary: action.PurposeSummary}}, want: ErrPermission},
		{name: "parameter escape", delta: StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 4, ProposedStop: StopContinue, ProposedAction: &ProposedAction{Tool: action.Tool, ScopeRef: action.ScopeRef, TemplateID: action.TemplateID, BoundedParameters: json.RawMessage(`{"promql":"up"}`), ExpectedFactTypes: action.ExpectedFactTypes, PurposeSummary: action.PurposeSummary}}, want: ErrPermission},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ReduceStateDelta(state, test.delta, basePolicy)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want %v", err, test.want)
			}
		})
	}

	canonical, _ := canonicalJSON(action.BoundedParameters)
	state.ToolAttempts = []ToolAttempt{{Signature: actionSignature(action.Tool, action.TemplateID, action.ScopeRef, canonical), Status: "available"}}
	_, _, err := ReduceStateDelta(state, StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 4, ProposedStop: StopContinue, ProposedAction: action}, basePolicy)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate action error=%v", err)
	}
}

func TestReduceStateDeltaRequiresActionOnlyForContinue(t *testing.T) {
	state := reducerState()
	policy := ReducerPolicy{StepUsage: Usage{Steps: 1}}
	for _, stop := range []StopProposal{StopDiagnose, StopInsufficient} {
		got, _, err := ReduceStateDelta(state, StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 4, ProposedStop: stop}, policy)
		if err != nil || got.CheckpointVersion != 5 {
			t.Fatalf("stop=%s state=%+v err=%v", stop, got, err)
		}
	}
	_, _, err := ReduceStateDelta(state, StateDelta{SchemaVersion: 1, BasisCheckpointVersion: 4, ProposedStop: StopContinue}, policy)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("continue without action error=%v", err)
	}
}

func reducerState() InvestigationState {
	return InvestigationState{
		SchemaVersion: InvestigationStateSchemaVersion, RunID: "run-1", IncidentID: "incident-1", CycleNo: 3,
		IncidentVersion: 7, CheckpointVersion: 4, NextNode: NodeSelectAction,
		Hypotheses: []HypothesisState{{ID: "h-1", Statement: "required env was removed", Status: HypothesisActive}},
		Questions:  []OpenQuestion{{ID: "q-1", Question: "Does the deployed pod contain REQUIRED_ENV?"}},
		Limits:     Limits{MaxSteps: 12, MaxToolCalls: 12, MaxModelCalls: 14, TokenBudget: 32000, MaxEvidenceItems: 40},
	}
}

func reducerEvidence(id, incident string, cycle uint64) EvidenceFact {
	return EvidenceFact{ID: id, EvidenceID: "evidence-" + id, IncidentID: incident, CycleNo: cycle, Type: "kubernetes.required_env_absent", SourceSystem: "kubernetes", CollectionPath: "podspec", CorroborationGroup: "deployed-state", Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete", ClaimUse: "support", CollectionStatus: CollectionAvailable, Direct: true}
}
