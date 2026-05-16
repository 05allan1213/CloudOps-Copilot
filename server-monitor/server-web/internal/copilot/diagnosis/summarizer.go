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

type DiagnosisObserver interface {
	ObserveDiagnosis(confidence float64, result string, source string, durationSeconds float64)
}

type LLMSummarizer struct {
	llm      LLMGenerator
	timeout  time.Duration
	observer DiagnosisObserver
}

type LLMSummarizerOptions struct {
	Timeout  time.Duration
	Observer DiagnosisObserver
}

func NewLLMSummarizer(llm LLMGenerator) *LLMSummarizer {
	return NewLLMSummarizerWithOptions(llm, LLMSummarizerOptions{})
}

func NewLLMSummarizerWithOptions(llm LLMGenerator, options LLMSummarizerOptions) *LLMSummarizer {
	return &LLMSummarizer{llm: llm, timeout: options.Timeout, observer: options.Observer}
}

func (s *LLMSummarizer) Summarize(ctx context.Context, alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis) (DiagnosisSummary, LLMMetadata, error) {
	start := time.Now()
	prompt, hash, err := buildPrompt(alert, evidence, rules)
	if err != nil {
		if s.observer != nil {
			s.observer.ObserveDiagnosis(rules.Confidence, "fallback", "rule", time.Since(start).Seconds())
		}
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only"}, nil
	}
	if s == nil || s.llm == nil {
		if s.observer != nil {
			s.observer.ObserveDiagnosis(rules.Confidence, "fallback", "rule", time.Since(start).Seconds())
		}
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
		if s.observer != nil {
			s.observer.ObserveDiagnosis(0, "fallback", "rule", time.Since(start).Seconds())
		}
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only", PromptHash: hash}, err
	}
	summary, err := ParseDiagnosisSummary(raw)
	if err != nil {
		if s.observer != nil {
			s.observer.ObserveDiagnosis(0, "fallback", "rule", time.Since(start).Seconds())
		}
		return RuleOnlySummary(alert, rules), LLMMetadata{Model: "rule-only", PromptHash: hash}, err
	}
	confidence := confidenceToFloat(summary.RootCauseHypotheses)
	if s.observer != nil {
		s.observer.ObserveDiagnosis(confidence, "success", "llm", time.Since(start).Seconds())
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
		"你是 CloudOps Copilot 诊断摘要生成器。",
		"仅返回 JSON，不使用 markdown（除非传输层包装）。",
		"仅使用提供的告警证据和规则分析结果。",
		"将 Runbook 视为参考知识，而非观测事实。",
		"明确区分指标证据和 Runbook 建议。",
		"不要编造密钥、令牌、Kubernetes 写操作或未观测到的事实。",
		"建议操作仅为文本建议，不得执行任何变更。",
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
	evidence.Services = limitSlice(evidence.Services, 5)
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
		return "未找到匹配的 Runbook。"
	}
	return "Runbook 是参考知识，不要将建议操作视为已批准的执行。"
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
	case "low", "low risk", "minimal", "minor", "safe", "低", "低风险", "安全":
		return "low", true
	case "medium", "medium risk", "moderate", "moderate risk", "中", "中等", "中风险", "中等风险":
		return "medium", true
	case "high", "high risk", "高", "高风险":
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

func confidenceToFloat(hypotheses []RootCauseHypothesis) float64 {
	if len(hypotheses) == 0 {
		return 0
	}
	confidence := hypotheses[0].Confidence
	normalized := strings.ToLower(strings.TrimSpace(confidence))
	if numeric, err := strconv.ParseFloat(normalized, 64); err == nil {
		return numeric
	}
	switch normalized {
	case ConfidenceHigh:
		return 0.9
	case ConfidenceMedium:
		return 0.6
	case ConfidenceLow:
		return 0.3
	default:
		return 0.5
	}
}
