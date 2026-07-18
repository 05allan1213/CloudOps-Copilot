package adapter

import (
	"context"
	"encoding/json"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

// DemoModel is a deterministic, credential-free model for the disposable
// fast-demo path. It still drives the durable graph and cites persisted evidence.
type DemoModel struct{}

func NewDemoModel() *DemoModel { return &DemoModel{} }

func (DemoModel) Plan(context.Context, agent.IncidentContext, string) (agent.Plan, agent.ModelUsage, error) {
	return agent.Plan{
		Summary:   "Inspect the affected Kubernetes Deployment using the bounded read-only tool.",
		Questions: []string{"Does the persisted Deployment state explain the unhealthy workload signal?"},
	}, agent.ModelUsage{}, nil
}

func (DemoModel) SelectAction(_ context.Context, state agent.GraphState, _ []string) (agent.Action, agent.ModelUsage, error) {
	arguments, _ := json.Marshal(map[string]any{
		"namespace": state.Incident.Namespace,
		"name":      state.Incident.TargetName,
		"limit":     10,
	})
	return agent.Action{
		Tool:      "k8s.get_deployments",
		Arguments: arguments,
		Reason:    "Read the affected Deployment before forming a diagnosis.",
	}, agent.ModelUsage{}, nil
}

func (DemoModel) EvaluateCoverage(_ context.Context, state agent.GraphState) (agent.Coverage, agent.ModelUsage, error) {
	return agent.Coverage{
		Sufficient: len(state.Observations) > 0,
		Reason:     "The Kubernetes Deployment observation covers the selected demo failure mode.",
	}, agent.ModelUsage{}, nil
}

func (DemoModel) Diagnose(_ context.Context, state agent.GraphState) (agent.Diagnosis, agent.ModelUsage, error) {
	evidenceIDs := append([]string(nil), state.EvidenceIDs...)
	return agent.Diagnosis{
		Summary:    "The affected Deployment is unhealthy because its persisted replica state does not provide ready workload capacity.",
		Confidence: 0.95,
		Hypotheses: []agent.Hypothesis{{
			Statement:   "The zero replica count caused the workload outage.",
			Confidence:  0.95,
			EvidenceIDs: evidenceIDs,
		}},
		ConfirmedFacts: []agent.Claim{{
			Statement:   "The bounded Kubernetes observation captured zero ready workload capacity during the alert.",
			EvidenceIDs: evidenceIDs,
			Strong:      true,
		}},
		AffectedResources:      []string{state.Incident.Namespace + "/Deployment/" + state.Incident.TargetName},
		RecommendedNextActions: []string{"Submit the bounded replica restoration plan for explicit human approval."},
		CoverageSummary:        "Deployment state was read through the fixed read-only tool allowlist.",
		BudgetSummary:          "One bounded Kubernetes observation was sufficient for the demo scenario.",
	}, agent.ModelUsage{}, nil
}
