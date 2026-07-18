package adapter

import (
	"context"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

func TestDemoModelProducesEvidenceBoundedDiagnosis(t *testing.T) {
	model := NewDemoModel()
	state := agent.GraphState{
		Incident:     agent.IncidentContext{Namespace: "default", TargetName: "cloudops-demo-workload"},
		EvidenceIDs:  []string{"11111111-1111-4111-8111-111111111111"},
		Observations: []agent.Observation{{Valid: true}},
	}
	action, _, err := model.SelectAction(context.Background(), state, nil)
	if err != nil || action.Tool != "k8s.get_deployments" {
		t.Fatalf("unexpected demo action: %#v err=%v", action, err)
	}
	diagnosis, _, err := model.Diagnose(context.Background(), state)
	if err != nil || len(diagnosis.ConfirmedFacts) != 1 || len(diagnosis.ConfirmedFacts[0].EvidenceIDs) != 1 {
		t.Fatalf("unexpected demo diagnosis: %#v err=%v", diagnosis, err)
	}
	if problems := agent.ValidateDiagnosis(diagnosis, map[string]agent.EvidenceRecord{state.EvidenceIDs[0]: {PublicID: state.EvidenceIDs[0], Valid: true}}); len(problems) != 0 {
		t.Fatalf("demo diagnosis must pass evidence validation: %v", problems)
	}
}
