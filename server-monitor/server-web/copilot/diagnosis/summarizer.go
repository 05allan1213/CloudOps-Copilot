package diagnosis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

type LLMGenerator interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	Model() string
}

type Summarizer interface {
	Summarize(ctx context.Context, alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis) (DiagnosisSummary, LLMMetadata, error)
}

type LLMSummarizer struct {
	llm LLMGenerator
}

func NewLLMSummarizer(llm LLMGenerator) *LLMSummarizer {
	return &LLMSummarizer{llm: llm}
}

func (s *LLMSummarizer) Summarize(ctx context.Context, alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis) (DiagnosisSummary, LLMMetadata, error) {
	prompt, hash, err := buildPrompt(alert, evidence, rules)
	if err != nil {
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only"}, nil
	}
	if s == nil || s.llm == nil {
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only", PromptHash: hash}, nil
	}

	raw, err := s.llm.Generate(ctx, diagnosisSystemPrompt(), prompt)
	if err != nil {
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only", PromptHash: hash}, err
	}
	summary, err := ParseDiagnosisSummary(raw)
	if err != nil {
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only", PromptHash: hash}, err
	}
	return summary, LLMMetadata{Model: s.llm.Model(), PromptHash: hash}, nil
}

func RuleOnlySummary(alert AlertContext, rules RuleAnalysis) DiagnosisSummary {
	actions := make([]RecommendedAction, 0, len(rules.NextSteps))
	for _, step := range rules.NextSteps {
		actions = append(actions, RecommendedAction{
			Type:        "inspect",
			Description: step,
			Risk:        "low",
		})
	}
	if len(actions) == 0 {
		actions = append(actions, RecommendedAction{Type: "inspect", Description: "补齐指标、告警历史和主机状态证据", Risk: "low"})
	}
	return DiagnosisSummary{
		Summary:            rules.Summary,
		SeverityAssessment: alert.Severity,
		RootCauseHypotheses: []RootCauseHypothesis{
			{
				Cause:      rules.Summary,
				Confidence: rules.ConfidenceLevel,
				Evidence:   passedRuleRefs(rules.Results),
			},
		},
		RecommendedActions: actions,
		NextSteps:          rules.NextSteps,
	}
}

func ParseDiagnosisSummary(raw string) (DiagnosisSummary, error) {
	body := extractJSONBody(raw)
	var summary DiagnosisSummary
	if err := json.Unmarshal([]byte(body), &summary); err != nil {
		return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: %w", err)
	}
	if strings.TrimSpace(summary.Summary) == "" {
		return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: summary is required")
	}
	if summary.RootCauseHypotheses == nil {
		summary.RootCauseHypotheses = []RootCauseHypothesis{}
	}
	if summary.RecommendedActions == nil {
		summary.RecommendedActions = []RecommendedAction{}
	}
	if summary.NextSteps == nil {
		summary.NextSteps = []string{}
	}
	return summary, nil
}

func extractJSONBody(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) >= 3 {
			return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	return strings.Trim(raw, "` \n\t")
}

func buildPrompt(alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis) (string, string, error) {
	payload := map[string]interface{}{
		"alert_context": alert,
		"evidence":      evidence,
		"rule_analysis": rules,
		"runbook_note":  "Phase 3 has not connected Runbook retrieval; runbooks are intentionally empty.",
		"output_schema": map[string]interface{}{
			"summary":               "string",
			"severity_assessment":   "critical|warning|info",
			"root_cause_hypotheses": []string{"cause", "confidence", "evidence"},
			"recommended_actions":   []string{"type", "description", "risk", "requires_approval"},
			"next_steps":            []string{"string"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	prompt := string(data)
	sum := sha256.Sum256([]byte(diagnosisSystemPrompt() + prompt))
	return prompt, fmt.Sprintf("%x", sum[:]), nil
}

func diagnosisSystemPrompt() string {
	return strings.Join([]string{
		"You are CloudOps Copilot diagnosis summarizer.",
		"Return JSON only, without markdown unless the transport wraps it.",
		"Use only provided alert evidence and rule analysis.",
		"Do not invent secrets, tokens, Kubernetes write operations, or unobserved facts.",
		"Recommended actions are text suggestions only and must not execute changes.",
	}, "\n")
}

func passedRuleRefs(results []RuleResult) []string {
	refs := []string{}
	for _, result := range results {
		if result.Passed {
			refs = append(refs, append([]string{result.Rule}, result.EvidenceRefs...)...)
		}
	}
	if len(refs) == 0 {
		return []string{"rule_analysis"}
	}
	return compactStrings(refs)
}
