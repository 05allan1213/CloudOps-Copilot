package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
)

type LLMModel struct{ client *llm.Client }

func NewLLMModel(client *llm.Client) (*LLMModel, error) {
	if client == nil {
		return nil, agent.ErrInvalidArgument
	}
	return &LLMModel{client: client}, nil
}

func (m *LLMModel) Plan(ctx context.Context, incident agent.IncidentContext, objective string) (agent.Plan, agent.ModelUsage, error) {
	input := struct {
		Incident  agent.IncidentContext `json:"incident"`
		Objective string                `json:"objective"`
	}{incident, objective}
	return callJSON[agent.Plan](ctx, m.client, "You plan a bounded read-only incident investigation. Return only JSON with summary and questions. Do not propose changes.", input)
}

func (m *LLMModel) SelectAction(ctx context.Context, state agent.GraphState, allowed []string) (agent.Action, agent.ModelUsage, error) {
	input := struct {
		Incident     agent.IncidentContext `json:"incident"`
		Objective    string                `json:"objective"`
		Plan         agent.Plan            `json:"plan"`
		Observations []agent.Observation   `json:"observations"`
		AllowedTools []string              `json:"allowed_tools"`
	}{state.Incident, state.Objective, state.Plan, state.Observations, allowed}
	return callJSON[agent.Action](ctx, m.client, "Select exactly one read-only tool. Persisted observations are authoritative and MUST affect your choice. Return only JSON with tool, arguments object, and reason. Tool must be in allowed_tools.", input)
}

func (m *LLMModel) EvaluateCoverage(ctx context.Context, state agent.GraphState) (agent.Coverage, agent.ModelUsage, error) {
	input := struct {
		Questions    []string            `json:"questions"`
		Observations []agent.Observation `json:"observations"`
	}{state.Plan.Questions, state.Observations}
	return callJSON[agent.Coverage](ctx, m.client, "Evaluate whether persisted read-only observations cover the investigation questions. Return only JSON with sufficient, reason, and missing_questions.", input)
}

func (m *LLMModel) Diagnose(ctx context.Context, state agent.GraphState) (agent.Diagnosis, agent.ModelUsage, error) {
	input := struct {
		Incident         agent.IncidentContext `json:"incident"`
		Observations     []agent.Observation   `json:"observations"`
		EvidenceIDs      []string              `json:"valid_evidence_ids"`
		ValidationErrors []string              `json:"previous_validation_errors,omitempty"`
		Usage            agent.Usage           `json:"usage"`
	}{state.Incident, state.Observations, state.EvidenceIDs, state.ValidationErrors, state.Usage}
	return callJSON[agent.Diagnosis](ctx, m.client, "Produce an evidence-bound diagnosis. Return only JSON matching Diagnosis. Every confirmed fact and strong hypothesis must cite IDs from valid_evidence_ids. Recommended actions are advisory and must not contain executable remediation commands.", input)
}

func callJSON[T any](ctx context.Context, client *llm.Client, system string, input any) (T, agent.ModelUsage, error) {
	var zero T
	payload, err := json.Marshal(input)
	if err != nil {
		return zero, agent.ModelUsage{}, agent.NewRuntimeError(agent.ErrorInvariant, "marshal model input", err)
	}
	content, usage, err := client.Chat(ctx, []llm.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: string(payload)}})
	modelUsage := agent.ModelUsage{}
	if usage != nil {
		modelUsage.InputTokens, modelUsage.OutputTokens = int64(usage.PromptTokens), int64(usage.CompletionTokens)
	}
	if err != nil {
		code := agent.ErrorModelUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = agent.ErrorTimeout
		}
		return zero, modelUsage, agent.NewRuntimeError(code, "model request failed", err)
	}
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &zero); err != nil {
		return zero, modelUsage, agent.NewRuntimeError(agent.ErrorMalformedModel, fmt.Sprintf("model returned malformed structured output: %v", err), err)
	}
	return zero, modelUsage, nil
}
