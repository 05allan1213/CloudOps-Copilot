package agenteval

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

func TestRunModelReplansAfterObservationAndReplaysCheckpoint(t *testing.T) {
	policy := agent.ClaimPolicy{
		Version: "test/v1", ClaimType: "selector_mismatch/v1",
		Requirements: []agent.FactRequirement{
			{Facet: "subject", AnyOf: []string{"workload.subject_confirmed"}},
			{Facet: "selector", AnyOf: []string{"change.selector_mismatch"}},
		},
		MinIndependentCollectors: 2, RequireDirectFact: true,
	}
	actionOne := evalAction("inspect_workload", "workload-snapshot/v1", "scope-1", nil, "workload.subject_confirmed")
	actionTwo := evalAction("get_change_detail", "change-detail/v1", "scope-1", map[string]any{"change_ref": "change-1"}, "change.selector_mismatch")
	now := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)
	dataset := Dataset{SchemaVersion: DatasetSchemaVersion, DatasetID: "eval-test/v1", Cases: []EvalCase{{
		ID: "case-1", Mode: ModeModel, ScopeRef: "scope-1", Objective: "identify the root cause",
		Correlation: agent.CorrelationSnapshot{Cluster: "kind", Environment: "demo", Namespace: "demo", Workload: "app", TargetKind: "Deployment"},
		Window:      agent.QueryWindow{From: now.Add(-5 * time.Minute), To: now}, Policies: []agent.ClaimPolicy{policy},
		Limits:           agent.Limits{MaxSteps: 12, MaxToolCalls: 4, MaxModelCalls: 8, TokenBudget: 10000, MaxEvidenceItems: 8, MaxCheckpointSize: 128 * 1024},
		ReplayAfterTools: 1,
		Fixtures: []ToolFixture{
			{Actions: []agent.ProposedAction{actionOne}, Observation: evalObservation(actionOne, "kubernetes", "kubernetes/workload", evalFact("fact-subject", "workload.subject_confirmed", "kubernetes", "kubernetes/workload"))},
			{Actions: []agent.ProposedAction{actionTwo}, Observation: evalObservation(actionTwo, "github", "github/change", evalFact("fact-change", "change.selector_mismatch", "github", "github/change"))},
		},
	}}}
	oracle := Oracle{SchemaVersion: OracleSchemaVersion, DatasetID: dataset.DatasetID, Cases: map[string]CaseOracle{
		"case-1": {ExpectedOutcome: OutcomeDiagnosed, AcceptableClaimTypes: []string{policy.ClaimType}, RequiredEvidenceGroups: [][]string{{"workload.subject_confirmed"}, {"change.selector_mismatch"}}, MaxToolCalls: 3, RequireReplay: true},
	}}
	split := Split{SchemaVersion: SplitSchemaVersion, DatasetID: dataset.DatasetID, Quality: []string{"case-1"}, Repetitions: 3, Aggregation: "single_run"}
	model := &scriptedEvalModel{actions: []agent.ProposedAction{actionOne, actionTwo}}

	report, err := RunModel(context.Background(), dataset, oracle, split, Manifest{DatasetID: dataset.DatasetID}, model, RunOptions{OnlySplit: SplitQuality, Repetitions: 1})
	if err != nil {
		t.Fatalf("RunModel() error = %v", err)
	}
	if len(report.Runs) != 1 || report.Runs[0].Outcome != OutcomeDiagnosed || report.Runs[0].ClaimType != policy.ClaimType {
		t.Fatalf("unexpected report run: %+v", report.Runs)
	}
	if report.Runs[0].ToolCalls != 2 || !report.Runs[0].ReplayOK || report.Aggregate.CitationRecall != 1 || report.Aggregate.Safety.Total() != 0 {
		t.Fatalf("unexpected replay/metric result: run=%+v aggregate=%+v", report.Runs[0], report.Aggregate)
	}
	if model.views != 2 || model.secondViewFacts != 1 {
		t.Fatalf("observation did not affect replanning: views=%d secondFacts=%d", model.views, model.secondViewFacts)
	}
}

func TestRunFixedBaselineUsesNoModelCalls(t *testing.T) {
	now := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)
	policy := agent.ClaimPolicy{Version: "test/v1", ClaimType: "subject/v1", Requirements: []agent.FactRequirement{{Facet: "subject", AnyOf: []string{"workload.subject_confirmed"}}}, MinIndependentCollectors: 1, RequireDirectFact: true}
	action := evalAction("inspect_workload", "workload-snapshot/v1", "scope-1", nil, "workload.subject_confirmed")
	dataset := Dataset{SchemaVersion: DatasetSchemaVersion, DatasetID: "eval-test/v1", Cases: []EvalCase{{
		ID: "case-1", Mode: ModeModel, ScopeRef: "scope-1", Objective: "identify",
		Correlation: agent.CorrelationSnapshot{Cluster: "kind", Environment: "demo", Namespace: "demo", Workload: "app", TargetKind: "Deployment"},
		Window:      agent.QueryWindow{From: now.Add(-time.Minute), To: now}, Policies: []agent.ClaimPolicy{policy},
		Limits:   agent.Limits{MaxSteps: 4, MaxToolCalls: 2, MaxModelCalls: 2, TokenBudget: 1000, MaxEvidenceItems: 2, MaxCheckpointSize: 128 * 1024},
		Fixtures: []ToolFixture{{Actions: []agent.ProposedAction{action}, Observation: evalObservation(action, "kubernetes", "kubernetes/workload", evalFact("fact-subject", "workload.subject_confirmed", "kubernetes", "kubernetes/workload"))}},
	}}}
	oracle := Oracle{SchemaVersion: OracleSchemaVersion, DatasetID: dataset.DatasetID, Cases: map[string]CaseOracle{"case-1": {ExpectedOutcome: OutcomeDiagnosed, AcceptableClaimTypes: []string{policy.ClaimType}, BaselineMaxToolCalls: 2}}}
	split := Split{SchemaVersion: SplitSchemaVersion, DatasetID: dataset.DatasetID, Quality: []string{"case-1"}, Repetitions: 3, Aggregation: "majority_vote"}

	report, err := RunFixedBaseline(dataset, oracle, split, Manifest{DatasetID: dataset.DatasetID}, RunOptions{OnlySplit: SplitQuality})
	if err != nil {
		t.Fatalf("RunFixedBaseline() error = %v", err)
	}
	if len(report.Runs) != 1 || report.Runs[0].Outcome != OutcomeDiagnosed || report.Runs[0].ModelCalls != 0 || report.Provider != "fixed-pipeline" {
		t.Fatalf("unexpected baseline: %+v", report)
	}
}

func TestSelectedCasesModelExcludesGuardrails(t *testing.T) {
	dataset := Dataset{Cases: []EvalCase{{ID: "cal"}, {ID: "quality"}, {ID: "guard"}}}
	split := Split{Calibration: []string{"cal"}, Quality: []string{"quality"}, Guardrail: []string{"guard"}}
	cases, err := selectedCases(dataset, split, SplitModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[0].ID != "cal" || cases[1].ID != "quality" {
		t.Fatalf("selected model cases = %+v", cases)
	}
}

type scriptedEvalModel struct {
	actions         []agent.ProposedAction
	views           int
	secondViewFacts int
}

func (m *scriptedEvalModel) ProposeDelta(_ context.Context, view agent.ModelView) (agent.StateDelta, agent.ModelUsage, error) {
	m.views++
	if m.views == 2 {
		m.secondViewFacts = len(view.Facts)
	}
	action := m.actions[m.views-1]
	return agent.StateDelta{SchemaVersion: 1, BasisCheckpointVersion: view.State.CheckpointVersion, ProposedAction: &action, ProposedStop: agent.StopContinue}, agent.ModelUsage{Calls: 1, InputTokens: 10, OutputTokens: 10}, nil
}

func (*scriptedEvalModel) SynthesizeDiagnosis(_ context.Context, view agent.DiagnosisView) (agent.DiagnosisCandidate, agent.ModelUsage, error) {
	return agent.DiagnosisCandidate{
		ClaimType: view.AllowedClaimTypes[0], Summary: "The persisted facts identify the configured root cause.", Confidence: agent.DiagnosisConfirmed,
		EvidenceFactIDs: append([]string(nil), view.SufficiencyByClaim[view.AllowedClaimTypes[0]].SupportingIDs...), RemediationHint: agent.RemediationNone,
	}, agent.ModelUsage{Calls: 1, InputTokens: 10, OutputTokens: 10}, nil
}

func evalAction(tool, template, scope string, parameters map[string]any, factTypes ...string) agent.ProposedAction {
	if parameters == nil {
		parameters = map[string]any{}
	}
	raw, _ := json.Marshal(parameters)
	return agent.ProposedAction{Tool: tool, ScopeRef: scope, TemplateID: template, BoundedParameters: raw, ExpectedFactTypes: factTypes, PurposeSummary: "collect one bounded fact"}
}

func evalObservation(action agent.ProposedAction, source, path string, facts ...agent.EvidenceFact) agent.ToolObservation {
	return agent.ToolObservation{Status: agent.CollectionAvailable, SourceSystem: source, CollectionPath: path, TemplateVersion: action.TemplateID, Summary: "bounded fixture observation", Facts: facts}
}

func evalFact(id, factType, source, path string) agent.EvidenceFact {
	return agent.EvidenceFact{ID: id, Type: factType, SourceSystem: source, CollectionPath: path, CorroborationGroup: source + "/" + path, Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete", ClaimUse: "support", CollectionStatus: agent.CollectionAvailable, Direct: true}
}
