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
