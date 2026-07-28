package agent

import (
	"encoding/json"
	"testing"
)

func TestRunAndStepTransitionsAreClosed(t *testing.T) {
	runs := []RunStatus{RunPending, RunRunning, RunCompleted, RunFailed, RunCancelled}
	allowedRuns := map[[2]RunStatus]bool{{RunPending, RunRunning}: true, {RunPending, RunFailed}: true, {RunPending, RunCancelled}: true, {RunRunning, RunCompleted}: true, {RunRunning, RunFailed}: true, {RunRunning, RunCancelled}: true}
	for _, from := range runs {
		for _, to := range runs {
			if got := CanTransitionRun(from, to); got != allowedRuns[[2]RunStatus{from, to}] {
				t.Fatalf("run transition %s -> %s = %v", from, to, got)
			}
		}
	}
	steps := []StepStatus{StepPending, StepRunning, StepCompleted, StepFailed, StepCancelled}
	allowedSteps := map[[2]StepStatus]bool{{StepPending, StepRunning}: true, {StepPending, StepFailed}: true, {StepPending, StepCancelled}: true, {StepRunning, StepCompleted}: true, {StepRunning, StepFailed}: true, {StepRunning, StepCancelled}: true}
	for _, from := range steps {
		for _, to := range steps {
			if got := CanTransitionStep(from, to); got != allowedSteps[[2]StepStatus{from, to}] {
				t.Fatalf("step transition %s -> %s = %v", from, to, got)
			}
		}
	}
}

func TestUsageBudgets(t *testing.T) {
	limits := Limits{MaxSteps: 2, MaxToolCalls: 2, MaxModelCalls: 2, TokenBudget: 10, MaxEvidenceItems: 2}
	base := Usage{Steps: 1, ToolCalls: 1, ModelCalls: 1, InputTokens: 4, OutputTokens: 1, Evidence: 1}
	if err := base.CanCharge(Usage{Steps: 1, ToolCalls: 1, ModelCalls: 1, InputTokens: 3, OutputTokens: 2, Evidence: 1}, limits); err != nil {
		t.Fatalf("within budget: %v", err)
	}
	tests := []Usage{{Steps: 2}, {ToolCalls: 2}, {ModelCalls: 2}, {InputTokens: 6}, {Evidence: 2}}
	for _, delta := range tests {
		if err := base.CanCharge(delta, limits); err == nil {
			t.Fatalf("expected budget rejection for %+v", delta)
		}
	}
}

func TestDiagnosisValidationRequiresOwnedValidEvidence(t *testing.T) {
	evidence := map[string]EvidenceRecord{"e1": {PublicID: "e1", Valid: true}}
	valid := Diagnosis{Summary: "CPU saturation is supported.", Confidence: .8, Hypotheses: []Hypothesis{{Statement: "CPU saturation", Confidence: .8, EvidenceIDs: []string{"e1"}}}, ConfirmedFacts: []Claim{{Statement: "CPU is high", EvidenceIDs: []string{"e1"}, Strong: true}}, RecommendedNextActions: []string{"Review capacity using read-only dashboards."}}
	if problems := ValidateDiagnosis(valid, evidence); len(problems) != 0 {
		t.Fatalf("valid diagnosis rejected: %v", problems)
	}
	invalid := valid
	invalid.ConfirmedFacts = []Claim{{Statement: "unknown", EvidenceIDs: []string{"other"}, Strong: true}}
	invalid.RecommendedNextActions = []string{"kubectl delete pod bad"}
	if problems := ValidateDiagnosis(invalid, evidence); len(problems) < 2 {
		data, _ := json.Marshal(problems)
		t.Fatalf("expected evidence and remediation rejection: %s", data)
	}
}
