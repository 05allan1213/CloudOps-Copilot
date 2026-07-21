package agenteval

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
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
	if !model.requiredEvidenceSeen {
		t.Fatal("diagnosis model did not receive deterministic required evidence IDs")
	}
	if model.views != 2 || model.secondViewFacts != 1 {
		t.Fatalf("observation did not affect replanning: views=%d secondFacts=%d", model.views, model.secondViewFacts)
	}
	if len(model.capturedViews) != 2 {
		t.Fatalf("captured model views=%d, want 2", len(model.capturedViews))
	}
	first, second := model.capturedViews[0], model.capturedViews[1]
	firstGap := first.ClaimSufficiency[policy.ClaimType]
	secondGap := second.ClaimSufficiency[policy.ClaimType]
	if len(first.CandidateClaims) != 1 || !slices.Contains(firstGap.MissingFacets, "subject") || !slices.Contains(firstGap.MissingFacets, "selector") {
		t.Fatalf("first view omitted deterministic claim gaps: %+v", first)
	}
	if slices.Contains(secondGap.MissingFacets, "subject") || !slices.Contains(secondGap.MissingFacets, "selector") || len(secondGap.SupportingIDs) != 1 {
		t.Fatalf("observation did not update deterministic sufficiency: %+v", secondGap)
	}
	if len(first.ActionCandidates) != 2 || len(second.ActionCandidates) != 1 || second.ActionCandidates[0].Tool != actionTwo.Tool {
		t.Fatalf("unused action candidates were not updated: first=%+v second=%+v", first.ActionCandidates, second.ActionCandidates)
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

func TestV2ConflictingSourcesCannotMakeAnyClaimReady(t *testing.T) {
	dataset, err := LoadDataset(filepath.Join("..", "..", "..", "eval", "v2", "dataset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evalCase EvalCase
	for _, candidate := range dataset.Cases {
		if candidate.ID == "model-conflicting-sources" {
			evalCase = candidate
			break
		}
	}
	if evalCase.ID == "" {
		t.Fatal("v2 conflicting-sources case is missing")
	}
	runtime, err := newCaseRuntime(evalCase)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range evalCase.Fixtures {
		if err := applyFixture(evalCase, runtime, fixture.Actions[0]); err != nil {
			t.Fatal(err)
		}
	}
	results, ready, _, err := evaluatePolicies(evalCase, runtime.state, runtime.facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 0 {
		t.Fatalf("conflicting authoritative sources produced READY claims: %v", ready)
	}
	identity := results["deployment_identity_regression/v1"]
	if identity.Outcome != agent.SufficiencyInsufficient || !slices.Contains(identity.BlockingIDs, "fact-conflict-env-present") {
		t.Fatalf("identity policy did not fail closed on the current Pod fact: %+v", identity)
	}
}

func TestV3RemovesInvalidUnchangedIdentityRegressionPolicy(t *testing.T) {
	dataset, err := LoadDataset(filepath.Join("..", "..", "..", "eval", "v3", "dataset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conflicting EvalCase
	for _, evalCase := range dataset.Cases {
		for _, policy := range evalCase.Policies {
			if policy.ClaimType == "deployment_identity_regression/v1" {
				t.Fatalf("case %q retained invalid unchanged-identity regression policy", evalCase.ID)
			}
		}
		if evalCase.ID == "model-conflicting-sources" {
			conflicting = evalCase
		}
	}
	if conflicting.ID == "" {
		t.Fatal("v3 conflicting-sources case is missing")
	}
	runtime, err := newCaseRuntime(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range conflicting.Fixtures {
		if err := applyFixture(conflicting, runtime, fixture.Actions[0]); err != nil {
			t.Fatal(err)
		}
	}
	_, ready, _, err := evaluatePolicies(conflicting, runtime.state, runtime.facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 0 {
		t.Fatalf("v3 conflicting authoritative sources produced READY claims: %v", ready)
	}
}

func TestRunModelKeepsInvestigatingWhenOneClaimIsReadyAndCandidatesRemain(t *testing.T) {
	early := agent.ClaimPolicy{
		Version: "early/v1", ClaimType: "early_claim/v1",
		Requirements:             []agent.FactRequirement{{Facet: "subject", AnyOf: []string{"workload.subject_confirmed"}}},
		MinIndependentCollectors: 1, RequireDirectFact: true,
	}
	target := agent.ClaimPolicy{
		Version: "target/v1", ClaimType: "target_claim/v1",
		Requirements: []agent.FactRequirement{
			{Facet: "subject", AnyOf: []string{"workload.subject_confirmed"}},
			{Facet: "selector", AnyOf: []string{"change.selector_mismatch"}},
		},
		MinIndependentCollectors: 2, RequireDirectFact: true,
	}
	actionOne := evalAction("inspect_workload", "workload-snapshot/v1", "scope-1", nil, "workload.subject_confirmed")
	actionTwo := evalAction("get_change_detail", "change-detail/v1", "scope-1", map[string]any{"change_ref": "change-1"}, "change.selector_mismatch")
	now := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)
	dataset := Dataset{SchemaVersion: DatasetSchemaVersion, DatasetID: "eval-ready/v1", Cases: []EvalCase{{
		ID: "case-ready", Mode: ModeModel, ScopeRef: "scope-1", Objective: "distinguish competing claims",
		Correlation: agent.CorrelationSnapshot{Cluster: "kind", Environment: "demo", Namespace: "demo", Workload: "app", TargetKind: "Deployment"},
		Window:      agent.QueryWindow{From: now.Add(-time.Minute), To: now}, Policies: []agent.ClaimPolicy{early, target},
		Limits: agent.Limits{MaxSteps: 8, MaxToolCalls: 3, MaxModelCalls: 6, TokenBudget: 10000, MaxEvidenceItems: 4, MaxCheckpointSize: 128 * 1024},
		Fixtures: []ToolFixture{
			{Actions: []agent.ProposedAction{actionOne}, Observation: evalObservation(actionOne, "kubernetes", "kubernetes/workload", evalFact("fact-subject", "workload.subject_confirmed", "kubernetes", "kubernetes/workload"))},
			{Actions: []agent.ProposedAction{actionTwo}, Observation: evalObservation(actionTwo, "github", "github/change", evalFact("fact-change", "change.selector_mismatch", "github", "github/change"))},
		},
	}}}
	oracle := Oracle{SchemaVersion: OracleSchemaVersion, DatasetID: dataset.DatasetID, Cases: map[string]CaseOracle{
		"case-ready": {ExpectedOutcome: OutcomeDiagnosed, AcceptableClaimTypes: []string{target.ClaimType}, RequiredEvidenceGroups: [][]string{{"workload.subject_confirmed"}, {"change.selector_mismatch"}}, MaxToolCalls: 2},
	}}
	split := Split{SchemaVersion: SplitSchemaVersion, DatasetID: dataset.DatasetID, Quality: []string{"case-ready"}, Repetitions: 1, Aggregation: "single_run"}
	model := &scriptedEvalModel{actions: []agent.ProposedAction{actionOne, actionTwo}, diagnosisClaim: target.ClaimType}

	report, err := RunModel(context.Background(), dataset, oracle, split, Manifest{DatasetID: dataset.DatasetID}, model, RunOptions{OnlySplit: SplitQuality, Repetitions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if model.views != 2 || report.Runs[0].ToolCalls != 2 || report.Runs[0].ClaimType != target.ClaimType {
		t.Fatalf("ready claim ended investigation before alternatives were resolved: model_views=%d run=%+v", model.views, report.Runs[0])
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

func TestUnusedActionCandidatesDropsAllVariantsOfUsedFixture(t *testing.T) {
	first := evalAction("query_metrics", "metric/v1", "scope-1", map[string]any{"window": "5m"}, "metric.symptom")
	second := evalAction("query_metrics", "metric/v1", "scope-1", map[string]any{"window": "15m"}, "metric.symptom")
	signature, err := agent.ActionSignature(first)
	if err != nil {
		t.Fatal(err)
	}
	evalCase := EvalCase{Fixtures: []ToolFixture{{Actions: []agent.ProposedAction{first, second}}}}
	runtime := &caseRuntime{fixtureUse: map[string]struct{}{signature: {}}}
	if candidates := unusedActionCandidates(evalCase, runtime); len(candidates) != 0 {
		t.Fatalf("used fixture variants remained candidates: %+v", candidates)
	}
}

type scriptedEvalModel struct {
	actions              []agent.ProposedAction
	views                int
	secondViewFacts      int
	capturedViews        []agent.ModelView
	requiredEvidenceSeen bool
	diagnosisClaim       string
}

func (m *scriptedEvalModel) ProposeDelta(_ context.Context, view agent.ModelView) (agent.StateDelta, agent.ModelUsage, error) {
	m.views++
	m.capturedViews = append(m.capturedViews, view)
	if m.views == 2 {
		m.secondViewFacts = len(view.Facts)
	}
	action := m.actions[m.views-1]
	return agent.StateDelta{SchemaVersion: 1, BasisCheckpointVersion: view.State.CheckpointVersion, ProposedAction: &action, ProposedStop: agent.StopContinue}, agent.ModelUsage{Calls: 1, InputTokens: 10, OutputTokens: 10}, nil
}

func (m *scriptedEvalModel) SynthesizeDiagnosis(_ context.Context, view agent.DiagnosisView) (agent.DiagnosisCandidate, agent.ModelUsage, error) {
	claim := view.AllowedClaimTypes[0]
	if m.diagnosisClaim != "" {
		claim = m.diagnosisClaim
	}
	m.requiredEvidenceSeen = len(view.RequiredEvidenceByClaim[claim]) > 0
	return agent.DiagnosisCandidate{
		ClaimType: claim, Summary: "The persisted facts identify the configured root cause.", Confidence: agent.DiagnosisConfirmed,
		EvidenceFactIDs: append([]string(nil), view.SufficiencyByClaim[claim].SupportingIDs...), RemediationHint: agent.RemediationNone,
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
