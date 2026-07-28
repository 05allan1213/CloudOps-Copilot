package agent

import "testing"

func TestWorkspaceModelOutcomeRequiresTwoCitedEvidenceSources(t *testing.T) {
	evidence := []EvidenceCitation{
		{EvidenceID: "evidence-kubernetes", Source: "kubernetes"},
		{EvidenceID: "evidence-prometheus", Source: "prometheus"},
		{EvidenceID: "evidence-prometheus-second", Source: "prometheus"},
	}

	tests := []struct {
		name   string
		answer string
		want   WorkspaceOutcome
	}{
		{name: "no citation", answer: "没有可追溯引用。", want: WorkspaceOutcomeInsufficient},
		{name: "one source", answer: "[Evidence: evidence-kubernetes]", want: WorkspaceOutcomeInsufficient},
		{name: "two citations from one source", answer: "evidence-prometheus evidence-prometheus-second", want: WorkspaceOutcomeInsufficient},
		{name: "two distinct current sources", answer: "evidence-kubernetes evidence-prometheus", want: WorkspaceOutcomeDiagnosed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome, uncertainty := workspaceModelOutcome(test.answer, evidence)
			if outcome != test.want {
				t.Fatalf("outcome=%q want=%q", outcome, test.want)
			}
			if outcome == WorkspaceOutcomeDiagnosed && uncertainty != "medium" {
				t.Fatalf("diagnosed uncertainty=%q want medium", uncertainty)
			}
			if outcome == WorkspaceOutcomeInsufficient && uncertainty != "high" {
				t.Fatalf("insufficient uncertainty=%q want high", uncertainty)
			}
		})
	}
}
