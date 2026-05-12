package diagnosis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxDiagnosisPromptBytes = 16 * 1024

type LLMGenerator interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	Model() string
}

type Summarizer interface {
	Summarize(ctx context.Context, alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis) (DiagnosisSummary, LLMMetadata, error)
}

type LLMSummarizer struct {
	llm     LLMGenerator
	timeout time.Duration
}

type LLMSummarizerOptions struct {
	Timeout time.Duration
}

func NewLLMSummarizer(llm LLMGenerator) *LLMSummarizer {
	return NewLLMSummarizerWithOptions(llm, LLMSummarizerOptions{})
}

func NewLLMSummarizerWithOptions(llm LLMGenerator, options LLMSummarizerOptions) *LLMSummarizer {
	return &LLMSummarizer{llm: llm, timeout: options.Timeout}
}

func (s *LLMSummarizer) Summarize(ctx context.Context, alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis) (DiagnosisSummary, LLMMetadata, error) {
	prompt, hash, err := buildPrompt(alert, evidence, rules)
	if err != nil {
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only"}, nil
	}
	if s == nil || s.llm == nil {
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only", PromptHash: hash}, nil
	}

	llmCtx := ctx
	var cancel context.CancelFunc
	if s.timeout > 0 {
		llmCtx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	raw, err := s.llm.Generate(llmCtx, diagnosisSystemPrompt(), prompt)
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
	if summary.SeverityAssessment != "" && !isAllowedSeverity(summary.SeverityAssessment) {
		return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: invalid severity_assessment")
	}
	if summary.RootCauseHypotheses == nil {
		summary.RootCauseHypotheses = []RootCauseHypothesis{}
	}
	if summary.RecommendedActions == nil {
		summary.RecommendedActions = []RecommendedAction{}
	}
	for i, action := range summary.RecommendedActions {
		if strings.TrimSpace(action.Type) == "" {
			return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: recommended_actions[%d].type is required", i)
		}
		if strings.TrimSpace(action.Description) == "" {
			return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: recommended_actions[%d].description is required", i)
		}
		risk, ok := normalizeRisk(action.Risk)
		if !ok {
			return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: recommended_actions[%d].risk is invalid", i)
		}
		summary.RecommendedActions[i].Risk = risk
	}
	for i, hypothesis := range summary.RootCauseHypotheses {
		if strings.TrimSpace(hypothesis.Cause) == "" {
			return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: root_cause_hypotheses[%d].cause is required", i)
		}
		if hypothesis.Confidence != "" && !isAllowedConfidence(hypothesis.Confidence) {
			return DiagnosisSummary{}, fmt.Errorf("parse diagnosis summary: root_cause_hypotheses[%d].confidence is invalid", i)
		}
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
		"evidence":      compactEvidenceForPrompt(evidence),
		"rule_analysis": rules,
		"runbook_note":  runbookNote(evidence.Runbooks),
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
	if len(data) > maxDiagnosisPromptBytes {
		payload["evidence"] = minimalEvidenceForPrompt(evidence)
		data, err = json.Marshal(payload)
		if err != nil {
			return "", "", err
		}
	}
	prompt := string(data)
	if len(prompt) > maxDiagnosisPromptBytes {
		prompt = truncateBytes(prompt, maxDiagnosisPromptBytes)
	}
	sum := sha256.Sum256([]byte(diagnosisSystemPrompt() + prompt))
	return prompt, fmt.Sprintf("%x", sum[:]), nil
}

func diagnosisSystemPrompt() string {
	return strings.Join([]string{
		"You are CloudOps Copilot diagnosis summarizer.",
		"Return JSON only, without markdown unless the transport wraps it.",
		"Use only provided alert evidence and rule analysis.",
		"Treat runbooks as reference knowledge, not observed facts.",
		"Clearly separate metric evidence from runbook suggestions.",
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

func compactEvidenceForPrompt(evidence EvidenceBundle) EvidenceBundle {
	evidence.ActiveAlerts = limitSlice(evidence.ActiveAlerts, 20)
	evidence.Metrics = limitSlice(evidence.Metrics, 40)
	evidence.History = limitSlice(evidence.History, 20)
	evidence.Runbooks = compactRunbooksForPrompt(evidence.Runbooks, 2, 800)
	evidence.K8s = compactK8sEvidenceForPrompt(evidence.K8s)
	evidence.CollectionErrors = limitSlice(evidence.CollectionErrors, 20)
	return evidence
}

func minimalEvidenceForPrompt(evidence EvidenceBundle) map[string]interface{} {
	return map[string]interface{}{
		"alert_context":     evidence.AlertContext,
		"metrics":           limitSlice(evidence.Metrics, 12),
		"k8s":               compactK8sEvidenceForPrompt(evidence.K8s),
		"runbooks":          compactRunbooksForPrompt(evidence.Runbooks, 2, 500),
		"history_count":     len(evidence.History),
		"collection_errors": limitSlice(evidence.CollectionErrors, 12),
		"collected_at":      evidence.CollectedAt,
	}
}

func compactK8sEvidenceForPrompt(evidence K8sEvidence) K8sEvidence {
	evidence.Pods = limitSlice(evidence.Pods, 10)
	evidence.Deployments = limitSlice(evidence.Deployments, 5)
	evidence.Nodes = limitSlice(evidence.Nodes, 5)
	evidence.Events = limitSlice(evidence.Events, 10)
	evidence.Logs = limitSlice(evidence.Logs, 2)
	for i := range evidence.Logs {
		evidence.Logs[i].Lines = limitSlice(evidence.Logs[i].Lines, 20)
	}
	evidence.Errors = limitSlice(evidence.Errors, 10)
	return evidence
}

func runbookNote(runbooks []RunbookEvidence) string {
	if len(runbooks) == 0 {
		return "No matching runbook found."
	}
	return "Runbooks are reference knowledge. Do not treat suggested actions as approved execution."
}

func compactRunbooksForPrompt(runbooks []RunbookEvidence, maxItems int, maxSnippetBytes int) []RunbookEvidence {
	runbooks = limitSlice(runbooks, maxItems)
	result := make([]RunbookEvidence, len(runbooks))
	for i, item := range runbooks {
		result[i] = item
		result[i].Snippet = truncateBytes(item.Snippet, maxSnippetBytes)
	}
	return result
}

func limitSlice[T any](values []T, max int) []T {
	if len(values) <= max {
		return values
	}
	return values[:max]
}

func truncateBytes(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := 0
	for idx := range value {
		if idx > maxBytes {
			break
		}
		end = idx
	}
	if end == 0 {
		return ""
	}
	return value[:end]
}

func isAllowedSeverity(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "warning", "info":
		return true
	default:
		return false
	}
}

func isAllowedRisk(value string) bool {
	_, ok := normalizeRisk(value)
	return ok
}

func normalizeRisk(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "低":
		return "low", true
	case "medium", "moderate", "中", "中等":
		return "medium", true
	case "high", "高":
		return "high", true
	default:
		return "", false
	}
}

func isAllowedConfidence(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if numeric, err := strconv.ParseFloat(normalized, 64); err == nil {
		return numeric >= 0 && numeric <= 1
	}
	switch normalized {
	case ConfidenceLow, ConfidenceMedium, ConfidenceHigh:
		return true
	default:
		return false
	}
}
