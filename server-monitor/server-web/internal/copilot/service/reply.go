package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"server-web/internal/copilot/nlu"
	copilotsuggestion "server-web/internal/copilot/suggestion"
)

func (s *Service) buildReplyWithSummary(ctx context.Context, message string, result nlu.Result, toolReply string, toolCalls []ToolCall, history []ChatHistoryItem) (string, []Suggestion) {
	fallbackReply := buildReply(result, toolReply, toolCalls)
	fallbackSuggestions := buildSuggestions(result)
	if s.summarizer == nil || !s.summaryEnabled {
		return fallbackReply, fallbackSuggestions
	}

	if !hasSuccessfulToolCall(toolCalls) {
		if result.Intent == nlu.IntentGeneralChat || result.Intent == nlu.IntentUnknown || result.Intent == IntentUnknown {
			return s.chatWithLLM(ctx, message, result, toolCalls, history, fallbackReply, fallbackSuggestions)
		}
		return fallbackReply, fallbackSuggestions
	}

	summaryResult, err := s.summarizer.Summarize(ctx, SummaryInput{
		UserMessage: message,
		ToolCalls:   toolCalls,
		Intent:      result.Intent,
		History:     history,
	})
	if err != nil {
		return fallbackReply, fallbackSuggestions
	}
	return summaryOrFallback(summaryResult, fallbackReply, fallbackSuggestions)
}

func (s *Service) buildReplyWithSummaryStream(ctx context.Context, message string, result nlu.Result, toolReply string, toolCalls []ToolCall, history []ChatHistoryItem, onDelta func(string) error) (string, []Suggestion) {
	fallbackReply := buildReply(result, toolReply, toolCalls)
	fallbackSuggestions := buildSuggestions(result)
	if s.summarizer == nil || !s.summaryEnabled {
		return fallbackReply, fallbackSuggestions
	}

	streamSummarizer, ok := s.summarizer.(SummarizerStream)
	if !ok {
		return s.buildReplyWithSummary(ctx, message, result, toolReply, toolCalls, history)
	}

	needLLM := hasSuccessfulToolCall(toolCalls) ||
		result.Intent == nlu.IntentGeneralChat ||
		result.Intent == nlu.IntentUnknown ||
		result.Intent == IntentUnknown
	if !needLLM {
		return fallbackReply, fallbackSuggestions
	}

	summaryResult, err := streamSummarizer.SummarizeStream(ctx, SummaryInput{
		UserMessage: message,
		ToolCalls:   toolCalls,
		Intent:      result.Intent,
		History:     history,
	}, onDelta)
	if err != nil {
		return fallbackReply, fallbackSuggestions
	}
	return summaryOrFallback(summaryResult, fallbackReply, fallbackSuggestions)
}

func (s *Service) chatWithLLM(ctx context.Context, message string, result nlu.Result, toolCalls []ToolCall, history []ChatHistoryItem, fallbackReply string, fallbackSuggestions []Suggestion) (string, []Suggestion) {
	summaryResult, err := s.summarizer.Summarize(ctx, SummaryInput{
		UserMessage: message,
		ToolCalls:   toolCalls,
		Intent:      result.Intent,
		History:     history,
	})
	if err != nil {
		return fallbackReply, fallbackSuggestions
	}
	return summaryOrFallback(summaryResult, fallbackReply, fallbackSuggestions)
}

func summaryOrFallback(result SummaryResult, fallbackReply string, fallbackSuggestions []Suggestion) (string, []Suggestion) {
	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		reply = fallbackReply
	} else {
		reply = normalizeLineBreaks(reply)
		reply = ensureLineBreakBeforeQuestion(reply)
	}
	suggestions := copilotsuggestion.Normalize(result.Suggestions)
	if len(suggestions) == 0 {
		suggestions = fallbackSuggestions
	}
	return reply, suggestions
}

func normalizeLineBreaks(reply string) string {
	reply = strings.ReplaceAll(reply, `\n`, "\n")
	reply = strings.ReplaceAll(reply, `\r`, "\r")
	return reply
}

func ensureLineBreakBeforeQuestion(reply string) string {
	if !strings.HasSuffix(reply, "？") && !strings.HasSuffix(reply, "?") {
		return reply
	}
	lastEndByte := -1
	lastEndRuneLen := 0
	for i, ch := range reply {
		if ch == '。' || ch == '！' || ch == '.' || ch == '!' {
			lastEndByte = i
			lastEndRuneLen = utf8.RuneLen(ch)
		}
	}
	if lastEndByte < 0 {
		return reply
	}
	splitAt := lastEndByte + lastEndRuneLen
	after := reply[splitAt:]
	if strings.HasPrefix(after, "\n") {
		return reply
	}
	return reply[:splitAt] + "\n" + after
}

func hasSuccessfulToolCall(toolCalls []ToolCall) bool {
	for _, call := range toolCalls {
		if call.Status == "success" {
			return true
		}
	}
	return false
}

func buildReply(result nlu.Result, toolReply string, toolCalls []ToolCall) string {
	if toolReply != "" {
		return toolReply
	}
	for _, call := range toolCalls {
		if call.Status == "error" && call.Error != "" {
			return fmt.Sprintf("工具 %s 执行失败: %s", call.Name, call.Error)
		}
	}
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return "已识别为活跃告警查询，只读告警工具将在下一模块返回实时数据。"
	case nlu.IntentAlertEventQuery:
		return "已识别为告警事件查询，只读告警事件工具将在下一模块返回实时数据。"
	case nlu.IntentAlertHistoryQuery:
		return "已识别为告警历史查询，只读历史工具将在下一模块返回实时数据。"
	case nlu.IntentHostQuery:
		return "已识别为主机查询，只读主机工具将在下一模块返回实时数据。"
	case nlu.IntentMetricQuery:
		return "已识别为指标查询，只读指标工具将在下一模块返回实时数据。"
	case nlu.IntentDiagnosisRequest:
		return "请提供 fingerprint、alert_history_id，或 alert_name + instance 以生成单条告警诊断。"
	case nlu.IntentGeneralChat:
		return "我是 CloudOps 智能助手，可以帮你查询主机、指标、活跃告警、告警事件和告警历史。\n有什么想了解的吗？"
	default:
		return "暂时无法识别您的意图。\n请说明您想查询主机、指标、活跃告警、告警事件还是告警历史。"
	}
}

func buildSuggestions(result nlu.Result) []Suggestion {
	return copilotsuggestion.Build(result)
}

func filterEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
