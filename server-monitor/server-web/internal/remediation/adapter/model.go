package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"server-web/internal/copilot/llm"
	"server-web/internal/remediation"
)

type ChatClient interface {
	Chat(context.Context, []llm.ChatMessage) (string, *llm.ChatUsage, error)
}

// ModelPlanner is a narrow Agent adapter. Its input and output intentionally
// contain no repository, path, branch, credential, approval or policy data.
type ModelPlanner struct{ client ChatClient }

func NewModelPlanner(client ChatClient) (*ModelPlanner, error) {
	if client == nil {
		return nil, remediation.ErrInvalidArgument
	}
	return &ModelPlanner{client: client}, nil
}

func (m *ModelPlanner) Plan(ctx context.Context, incidentContext, diagnosis json.RawMessage, evidenceIDs []string) (remediation.PlannerOutput, error) {
	if len(incidentContext) == 0 || len(incidentContext) > remediation.MaxPlanJSONBytes || !json.Valid(incidentContext) || len(diagnosis) == 0 || len(diagnosis) > remediation.MaxPlanJSONBytes || !json.Valid(diagnosis) || len(evidenceIDs) == 0 || len(evidenceIDs) > 20 {
		return remediation.PlannerOutput{}, remediation.ErrInvalidArgument
	}
	input, err := json.Marshal(struct {
		Incident    json.RawMessage `json:"incident"`
		Diagnosis   json.RawMessage `json:"diagnosis"`
		EvidenceIDs []string        `json:"allowed_evidence_ids"`
	}{incidentContext, diagnosis, evidenceIDs})
	if err != nil {
		return remediation.PlannerOutput{}, remediation.ErrInvalidArgument
	}
	system := "Return only one JSON RemediationPlan proposal matching this schema: " + string(remediation.PlannerJSONSchema()) + ". Cite only allowed_evidence_ids. Never output repository, path, branch, commit, PR, credential, approval, policy, shell, Git, Kubernetes write, workflow, Secret, or RBAC instructions."
	content, _, err := m.client.Chat(ctx, []llm.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: string(input)}})
	if err != nil {
		return remediation.PlannerOutput{}, fmt.Errorf("planner model unavailable: %w", err)
	}
	output, err := remediation.DecodePlannerOutput([]byte(content))
	if err != nil {
		return remediation.PlannerOutput{}, err
	}
	allowed := make(map[string]struct{}, len(evidenceIDs))
	for _, id := range evidenceIDs {
		allowed[id] = struct{}{}
	}
	for _, id := range output.EvidenceIDs {
		if _, ok := allowed[id]; !ok {
			return remediation.PlannerOutput{}, fmt.Errorf("%w: planner cited foreign evidence", remediation.ErrPolicyRejected)
		}
	}
	return output, nil
}
