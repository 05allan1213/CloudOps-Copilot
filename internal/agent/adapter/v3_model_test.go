package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
)

func TestV3StructuredModelRepairsDomainInvalidDeltaOnce(t *testing.T) {
	invalid := `{"schema_version":1,"basis_checkpoint_version":4,"proposed_action":{"tool":"github.create_pull_request","scope_ref":"scope-1","template_id":"metric/v1","bounded_parameters":{"service":"demo"},"expected_fact_types":["metric.symptom"],"purpose_summary":"inspect"},"proposed_stop":"continue"}`
	valid := `{"schema_version":1,"basis_checkpoint_version":4,"proposed_action":{"tool":"inspect_metric","scope_ref":"scope-1","template_id":"metric/v1","bounded_parameters":{"service":"demo"},"expected_fact_types":["metric.symptom"],"purpose_summary":"inspect the bounded metric"},"proposed_stop":"continue"}`
	model, calls := newV3StructuredTestModel(t, []string{invalid, valid})
	delta, usage, err := model.ProposeDelta(context.Background(), agent.ModelView{
		State: agent.InvestigationState{
			SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1,
			CheckpointVersion: 4, Limits: agent.Limits{MaxCheckpointSize: 64 * 1024},
		},
		ScopeRef: "scope-1",
		AllowedActions: []agent.ModelActionSchema{{
			Tool: "inspect_metric", TemplateIDs: []string{"metric/v1"}, ParameterKeys: []string{"service"},
			ExpectedFactTypes: []string{"metric.symptom"},
		}},
	})
	if err != nil || delta.ProposedAction == nil || delta.ProposedAction.Tool != "inspect_metric" {
		t.Fatalf("delta=%+v usage=%+v err=%v", delta, usage, err)
	}
	if got := calls(); got != 2 {
		t.Fatalf("provider calls=%d, want one request plus one repair", got)
	}
	if usage.Calls != 2 || usage.InputTokens != 4 || usage.OutputTokens != 6 {
		t.Fatalf("usage=%+v, want provider token sum", usage)
	}
}

func TestRuntimeModelIdentityUsesActualAdapterModelAndStableMaterials(t *testing.T) {
	client := llm.NewClient(llm.Options{APIKey: "fixture", APIURL: "https://provider.example/v1/chat/completions", Model: "actual-model-v7"})
	model, err := NewLLMModel(client)
	if err != nil {
		t.Fatal(err)
	}
	policies := map[string]agent.ToolActionPolicy{
		"z_tool": {TemplateIDs: []string{"z/v1"}, ParameterKeys: []string{"b", "a"}, ExpectedFactTypes: []string{"z.fact"}},
		"a_tool": {TemplateIDs: []string{"a/v1"}, ExpectedFactTypes: []string{"a.fact"}},
	}
	identity, err := model.RuntimeModelIdentity("provider-x", policies)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != "provider-x" || identity.ActualModel != "actual-model-v7" ||
		identity.PromptVersion != StructuredPromptVersion || identity.ToolSchemaVersion != InvestigationToolVersion ||
		identity.Validate() != nil {
		t.Fatalf("runtime identity=%+v", identity)
	}
	policies["z_tool"] = agent.ToolActionPolicy{TemplateIDs: []string{"z/v1"}, ParameterKeys: []string{"a", "b"}, ExpectedFactTypes: []string{"z.fact"}}
	again, err := model.RuntimeModelIdentity("provider-x", policies)
	if err != nil || again != identity {
		t.Fatalf("identity changed under map/slice ordering: first=%+v second=%+v err=%v", identity, again, err)
	}
}

func TestV3StructuredModelRepairsUnsupportedDiagnosisCitation(t *testing.T) {
	invalid := `{"claim_type":"required_env_config_regression/v1","summary":"REQUIRED_ENV is absent in the deployed revision.","confidence":"confirmed","evidence_fact_ids":["missing-fact"],"remediation_hint":"restore_required_env"}`
	valid := `{"claim_type":"required_env_config_regression/v1","summary":"REQUIRED_ENV is absent in the deployed revision.","confidence":"confirmed","evidence_fact_ids":["fact-1"],"unknowns":[],"remediation_hint":"restore_required_env"}`
	model, calls := newV3StructuredTestModel(t, []string{invalid, valid})
	candidate, usage, err := model.SynthesizeDiagnosis(context.Background(), agent.DiagnosisView{
		State: agent.InvestigationState{
			SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1,
			Coverage: agent.CoverageRequirements{ClaimType: agent.GoldenRequiredEnvClaimPolicy().ClaimType},
		},
		Facts: []agent.EvidenceFact{{
			ID: "fact-1", EvidenceID: "11111111-1111-4111-8111-111111111111", IncidentID: "incident-1", CycleNo: 1,
			CollectionStatus: agent.CollectionAvailable, Integrity: "verified", ClaimUse: "allowed",
		}},
		Sufficiency: agent.SufficiencyResult{Outcome: agent.SufficiencyReady, SupportingIDs: []string{"fact-1"}},
	})
	if err != nil || candidate.Confidence != agent.DiagnosisConfirmed || len(candidate.EvidenceFactIDs) != 1 || candidate.EvidenceFactIDs[0] != "fact-1" {
		t.Fatalf("candidate=%+v usage=%+v err=%v", candidate, usage, err)
	}
	if got := calls(); got != 2 {
		t.Fatalf("provider calls=%d, want one request plus one repair", got)
	}
}

func TestV3StructuredModelFailsClosedAfterOneRepair(t *testing.T) {
	model, calls := newV3StructuredTestModel(t, []string{`{"unexpected":true}`, `{"unexpected":true}`})
	_, _, err := model.ProposeDelta(context.Background(), agent.ModelView{
		State:    agent.InvestigationState{SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1},
		ScopeRef: "scope-1",
		AllowedActions: []agent.ModelActionSchema{{
			Tool: "inspect_metric", TemplateIDs: []string{"metric/v1"}, ParameterKeys: []string{"service"},
			ExpectedFactTypes: []string{"metric.symptom"},
		}},
	})
	var runtimeErr *agent.RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Code != agent.ErrorMalformedModel {
		t.Fatalf("error=%v, want malformed model RuntimeError", err)
	}
	if got := calls(); got != 2 {
		t.Fatalf("provider calls=%d, want exactly 2", got)
	}
}

func TestValidateModelDeltaAcceptsOnlyUnusedFrozenActionCandidates(t *testing.T) {
	candidate := agent.ProposedAction{
		Tool: "inspect_metric", ScopeRef: "scope-1", TemplateID: "metric/v1",
		BoundedParameters: json.RawMessage(`{"service":"demo"}`), ExpectedFactTypes: []string{"metric.symptom"},
		PurposeSummary: "inspect the bounded metric",
	}
	view := agent.ModelView{
		State: agent.InvestigationState{
			SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1,
			CheckpointVersion: 4, Limits: agent.Limits{MaxCheckpointSize: 64 * 1024},
		},
		ScopeRef: "scope-1",
		AllowedActions: []agent.ModelActionSchema{{
			Tool: "inspect_metric", TemplateIDs: []string{"metric/v1"}, ParameterKeys: []string{"service"},
			ExpectedFactTypes: []string{"metric.symptom"},
		}},
		ActionCandidates: []agent.ProposedAction{candidate}, ActionCandidatesExhaustive: true,
	}
	exact := agent.StateDelta{
		SchemaVersion: agent.InvestigationStateSchemaVersion, BasisCheckpointVersion: view.State.CheckpointVersion,
		ProposedAction: &candidate, ProposedStop: agent.StopContinue,
	}
	if err := ValidateModelDelta(view, exact); err != nil {
		t.Fatalf("exact frozen candidate rejected: %v", err)
	}

	modified := candidate
	modified.BoundedParameters = json.RawMessage(`{"service":"other"}`)
	changed := exact
	changed.ProposedAction = &modified
	if err := ValidateModelDelta(view, changed); err == nil {
		t.Fatal("modified candidate parameters were accepted")
	}
	modifiedPurpose := candidate
	modifiedPurpose.PurposeSummary = "different bounded purpose"
	changed.ProposedAction = &modifiedPurpose
	if err := ValidateModelDelta(view, changed); err == nil {
		t.Fatal("modified candidate purpose was accepted")
	}

	stop := agent.StateDelta{
		SchemaVersion: agent.InvestigationStateSchemaVersion, BasisCheckpointVersion: view.State.CheckpointVersion,
		ProposedStop: agent.StopInsufficient,
	}
	if err := ValidateModelDelta(view, stop); err == nil {
		t.Fatal("premature stop was accepted while frozen candidates remained")
	}
	readyView := view
	readyView.ClaimSufficiency = map[string]agent.SufficiencyResult{
		"metric_regression/v1": {Outcome: agent.SufficiencyReady, SupportingIDs: []string{"fact-1"}},
	}
	diagnose := stop
	diagnose.ProposedStop = agent.StopDiagnose
	if err := ValidateModelDelta(readyView, diagnose); err != nil {
		t.Fatalf("diagnose stop with a deterministically ready claim was rejected: %v", err)
	}

	productionView := view
	productionView.ActionCandidates = nil
	productionView.ActionCandidatesExhaustive = false
	changed.ProposedAction = &modified
	if err := ValidateModelDelta(productionView, changed); err != nil {
		t.Fatalf("production path without frozen candidates rejected a policy-valid action: %v", err)
	}
	if err := ValidateModelDelta(productionView, stop); err != nil {
		t.Fatalf("production path without frozen candidates rejected an insufficient stop: %v", err)
	}

	signature, err := agent.ActionSignature(candidate)
	if err != nil {
		t.Fatal(err)
	}
	usedView := view
	usedView.State.ToolAttempts = []agent.ToolAttempt{{Signature: signature, Tool: candidate.Tool, Status: "available"}}
	if err := ValidateModelDelta(usedView, exact); err == nil {
		t.Fatal("previously used candidate was accepted")
	}
}

func TestMarshalDeltaModelInputCompactsFrozenCandidateContext(t *testing.T) {
	candidate := agent.ProposedAction{
		Tool: "inspect_metric", ScopeRef: "scope-1", TemplateID: "metric/v1",
		BoundedParameters: json.RawMessage(`{"service":"demo"}`), ExpectedFactTypes: []string{"metric.symptom"},
		PurposeSummary: "inspect the bounded metric",
	}
	view := agent.ModelView{
		State: agent.InvestigationState{
			SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1,
			Objective: "identify the bounded cause", CheckpointVersion: 4, NextNode: agent.NodeSelectAction,
			Correlation: agent.CorrelationSnapshot{Cluster: "kind", Namespace: "demo", Workload: "api"},
			Limits:      agent.Limits{MaxCheckpointSize: 64 * 1024},
		},
		Facts: []agent.EvidenceFact{{
			ID: "fact-1", EvidenceID: "evidence-1", Type: "metric.symptom", SourceSystem: "prometheus",
			CollectionPath: "prometheus/metric", CollectionStatus: agent.CollectionAvailable,
			ClaimUse: "support", Direct: true, Attributes: map[string]string{"private": "not-provider-visible"},
		}},
		ScopeRef: "scope-1",
		AllowedActions: []agent.ModelActionSchema{{
			Tool: "inspect_metric", TemplateIDs: []string{"metric/v1"}, ParameterKeys: []string{"service"},
			ExpectedFactTypes: []string{"metric.symptom"},
		}},
		CandidateClaims: []agent.ClaimPolicy{{
			Version: "policy/v1", ClaimType: "metric_regression/v1",
			Requirements:      []agent.FactRequirement{{Facet: "metric", AnyOf: []string{"metric.symptom"}}, {Facet: "change", AnyOf: []string{"change.regression"}}},
			BlockingFactTypes: []string{"metric.healthy"}, MinIndependentCollectors: 2, RequireDirectFact: true,
		}},
		ClaimSufficiency: map[string]agent.SufficiencyResult{
			"metric_regression/v1": {Outcome: agent.SufficiencyContinue, MissingFacets: []string{"change"}, ReasonCodes: []string{"required_facets_missing"}, SupportingIDs: []string{"fact-1"}},
		},
		ActionCandidates:           []agent.ProposedAction{candidate},
		ActionCandidatesExhaustive: true,
	}
	payload, err := marshalDeltaModelInput(view)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["allowed_actions"]; exists {
		t.Fatal("candidate-mode payload repeated the full allowed action catalog")
	}
	state := decoded["state"].(map[string]any)
	if _, exists := state["correlation"]; exists {
		t.Fatal("candidate-mode state included redundant correlation details")
	}
	fact := decoded["facts"].([]any)[0].(map[string]any)
	if _, exists := fact["attributes"]; exists {
		t.Fatal("candidate-mode fact exposed non-decision attributes")
	}
	claim := decoded["candidate_claims"].([]any)[0].(map[string]any)
	if _, exists := claim["version"]; exists {
		t.Fatal("candidate-mode claim repeated non-decision policy metadata")
	}
	requirements := claim["missing_requirements"].([]any)
	if len(requirements) != 1 || requirements[0].(map[string]any)["facet"] != "change" {
		t.Fatalf("candidate-mode claim gaps=%+v", requirements)
	}

	productionView := view
	productionView.ActionCandidates = nil
	productionView.ActionCandidatesExhaustive = false
	compact, err := marshalDeltaModelInput(productionView)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := json.Marshal(productionView)
	if string(compact) != string(full) {
		t.Fatal("production model view changed without frozen candidates")
	}

	depletedView := view
	depletedView.ActionCandidates = nil
	depletedView.ActionCandidatesExhaustive = true
	depleted, err := marshalDeltaModelInput(depletedView)
	if err != nil {
		t.Fatal(err)
	}
	var depletedInput map[string]any
	if err := json.Unmarshal(depleted, &depletedInput); err != nil {
		t.Fatal(err)
	}
	if exhaustive, ok := depletedInput["action_candidates_exhaustive"].(bool); !ok || !exhaustive {
		t.Fatalf("depleted candidate payload omitted exhaustive contract: %+v", depletedInput)
	}
	if candidates, ok := depletedInput["action_candidates"].([]any); !ok || len(candidates) != 0 {
		t.Fatalf("depleted candidate payload retained actions: %+v", depletedInput["action_candidates"])
	}
}

func TestValidateModelDeltaRequiresStopAfterExhaustiveCandidatesAreDepleted(t *testing.T) {
	view := agent.ModelView{
		State: agent.InvestigationState{
			SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1,
			CheckpointVersion: 4, Limits: agent.Limits{MaxCheckpointSize: 64 * 1024},
		},
		ScopeRef: "scope-1",
		AllowedActions: []agent.ModelActionSchema{{
			Tool: "inspect_metric", TemplateIDs: []string{"metric/v1"}, ParameterKeys: []string{"service"},
			ExpectedFactTypes: []string{"metric.symptom"},
		}},
		ActionCandidatesExhaustive: true,
	}
	stop := agent.StateDelta{
		SchemaVersion: agent.InvestigationStateSchemaVersion, BasisCheckpointVersion: view.State.CheckpointVersion,
		ProposedStop: agent.StopInsufficient,
	}
	if err := ValidateModelDelta(view, stop); err != nil {
		t.Fatalf("terminal insufficient stop rejected: %v", err)
	}
	action := agent.ProposedAction{
		Tool: "inspect_metric", ScopeRef: "scope-1", TemplateID: "metric/v1",
		BoundedParameters: json.RawMessage(`{"service":"demo"}`), ExpectedFactTypes: []string{"metric.symptom"},
		PurposeSummary: "inspect the bounded metric",
	}
	continueDelta := stop
	continueDelta.ProposedStop = agent.StopContinue
	continueDelta.ProposedAction = &action
	if err := ValidateModelDelta(view, continueDelta); err == nil {
		t.Fatal("action after depleted exhaustive candidates was accepted")
	}
}

func TestMarshalDiagnosisModelInputCompactsRequiredEvidenceContext(t *testing.T) {
	view := agent.DiagnosisView{
		State: agent.InvestigationState{
			SchemaVersion: agent.InvestigationStateSchemaVersion, IncidentID: "incident-1", CycleNo: 1,
			Objective: "identify the bounded cause", CheckpointVersion: 4,
			Correlation: agent.CorrelationSnapshot{Cluster: "kind", Namespace: "demo", Workload: "api"},
		},
		Facts: []agent.EvidenceFact{
			{ID: "fact-1", EvidenceID: "evidence-1", Type: "metric.symptom", SourceSystem: "prometheus", CollectionPath: "prometheus/metric", CollectionStatus: agent.CollectionAvailable, ClaimUse: "support", Direct: true, Attributes: map[string]string{"private": "hidden"}},
			{ID: "fact-unused", EvidenceID: "evidence-2", Type: "noise", SourceSystem: "logs", CollectionPath: "logs/noise", CollectionStatus: agent.CollectionAvailable, ClaimUse: "support", Direct: true},
		},
		AllowedClaimTypes:       []string{"metric_regression/v1"},
		RequiredEvidenceByClaim: map[string][]string{"metric_regression/v1": {"fact-1"}},
	}
	payload, err := marshalDiagnosisModelInput(view)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["sufficiency"]; exists {
		t.Fatal("compact diagnosis payload repeated full sufficiency object")
	}
	facts := decoded["facts"].([]any)
	if len(facts) != 1 || facts[0].(map[string]any)["fact_id"] != "fact-1" {
		t.Fatalf("compact diagnosis facts=%+v", facts)
	}
	required := decoded["required_evidence_by_claim"].(map[string]any)["metric_regression/v1"].([]any)
	if len(required) != 1 || required[0] != "fact-1" {
		t.Fatalf("compact diagnosis required evidence=%+v", required)
	}

	productionView := view
	productionView.RequiredEvidenceByClaim = nil
	compact, err := marshalDiagnosisModelInput(productionView)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := json.Marshal(productionView)
	if string(compact) != string(full) {
		t.Fatal("production diagnosis view changed without required evidence projection")
	}
}

func newV3StructuredTestModel(t *testing.T, outputs []string) (*LLMModel, func() int) {
	t.Helper()
	var mutex sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		index := calls
		calls++
		mutex.Unlock()
		if index >= len(outputs) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": outputs[index]}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	t.Cleanup(server.Close)
	zeroRetries := 0
	client := llm.NewClient(llm.Options{
		APIKey: "test-key", APIURL: server.URL, Model: "test-model", Timeout: time.Second,
		MaxRetries: &zeroRetries, HTTPClient: &http.Client{Timeout: time.Second},
	})
	model, err := NewLLMModel(client)
	if err != nil {
		t.Fatal(err)
	}
	return model, func() int {
		mutex.Lock()
		defer mutex.Unlock()
		return calls
	}
}
