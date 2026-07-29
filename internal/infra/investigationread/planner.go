package investigationread

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

const GoldenAcquisitionPlanVersion = "required-env-symptom-first/v1"

type GoldenAcquisitionPlanner struct {
	policies map[string]agent.ToolActionPolicy
}

var _ agent.InvestigationActionPlanner = (*GoldenAcquisitionPlanner)(nil)

func NewGoldenAcquisitionPlanner(policies map[string]agent.ToolActionPolicy) (*GoldenAcquisitionPlanner, error) {
	required := []string{
		ToolInspectWorkload,
		ToolQueryMetrics,
		ToolQueryLogs,
		ToolQueryTraces,
		ToolGetDeploymentContext,
		ToolGetChangeDetail,
	}
	cloned := make(map[string]agent.ToolActionPolicy, len(required))
	for _, tool := range required {
		policy, ok := policies[tool]
		if !ok || len(policy.TemplateIDs) != 1 || len(policy.ExpectedFactTypes) == 0 {
			return nil, errors.New("golden acquisition planner requires the complete frozen action policy")
		}
		policy.TemplateIDs = slices.Clone(policy.TemplateIDs)
		policy.ExpectedFactTypes = slices.Clone(policy.ExpectedFactTypes)
		cloned[tool] = policy
	}
	return &GoldenAcquisitionPlanner{policies: cloned}, nil
}

func (p *GoldenAcquisitionPlanner) NextAction(state agent.InvestigationState, facts []agent.EvidenceFact, scopeRef string) (*agent.ProposedAction, error) {
	if p == nil || strings.TrimSpace(scopeRef) == "" {
		return nil, agent.ErrInvalidArgument
	}
	used := make(map[string]struct{}, len(state.ToolAttempts))
	for _, attempt := range state.ToolAttempts {
		used[attempt.Tool] = struct{}{}
	}
	steps := []struct {
		tool       string
		parameters json.RawMessage
		purpose    string
	}{
		{ToolInspectWorkload, json.RawMessage(`{}`), "confirm the exact workload and required environment state"},
		{ToolQueryMetrics, json.RawMessage(`{"window":"30m"}`), "confirm the bounded readiness and HTTP error symptom"},
		{ToolQueryLogs, json.RawMessage(`{"window":"30m","severity":"warning"}`), "confirm the required environment failure in bounded logs"},
		{ToolQueryTraces, json.RawMessage(`{"window":"30m","status":"error","limit":50}`), "confirm failed requests in bounded traces"},
		{ToolGetDeploymentContext, json.RawMessage(`{"window":"24h"}`), "bind the runtime symptom to the exact deployed GitOps revision and image"},
	}
	for _, step := range steps {
		if _, ok := used[step.tool]; ok {
			continue
		}
		return p.action(step.tool, scopeRef, step.parameters, step.purpose), nil
	}
	if _, ok := used[ToolGetChangeDetail]; ok {
		return nil, nil
	}
	changeRef, err := currentDeploymentChangeRef(facts)
	if err != nil {
		return nil, err
	}
	if changeRef == "" {
		return nil, nil
	}
	parameters, _ := json.Marshal(map[string]string{"change_ref": changeRef})
	return p.action(ToolGetChangeDetail, scopeRef, parameters, "verify the exact merged change and its pull request head CI"), nil
}

func (p *GoldenAcquisitionPlanner) action(tool, scopeRef string, parameters json.RawMessage, purpose string) *agent.ProposedAction {
	policy := p.policies[tool]
	return &agent.ProposedAction{
		Tool: tool, ScopeRef: scopeRef, TemplateID: policy.TemplateIDs[0],
		BoundedParameters: slices.Clone(parameters), ExpectedFactTypes: slices.Clone(policy.ExpectedFactTypes),
		PurposeSummary: purpose,
	}
}

func currentDeploymentChangeRef(facts []agent.EvidenceFact) (string, error) {
	result := ""
	currentFacts := 0
	for _, fact := range facts {
		if fact.Type != "deployment.change_ref" || !strings.EqualFold(fact.Attributes["is_current"], "true") {
			continue
		}
		currentFacts++
		if currentFacts > 1 {
			return "", agent.ErrConflict
		}
		candidate := strings.ToLower(strings.TrimSpace(fact.Attributes["change_ref"]))
		if _, err := uuid.Parse(candidate); err != nil {
			return "", agent.ErrInvalidArgument
		}
		result = candidate
	}
	return result, nil
}
