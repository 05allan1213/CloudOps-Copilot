package service

import (
	"context"
	"fmt"
	"strings"

	"server-web/internal/copilot/nlu"
)

func (s *Service) buildReplyWithSummary(ctx context.Context, message string, result nlu.Result, toolReply string, toolCalls []ToolCall, history []ChatHistoryItem) (string, []string) {
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

func (s *Service) chatWithLLM(ctx context.Context, message string, result nlu.Result, toolCalls []ToolCall, history []ChatHistoryItem, fallbackReply string, fallbackSuggestions []string) (string, []string) {
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

func summaryOrFallback(result SummaryResult, fallbackReply string, fallbackSuggestions []string) (string, []string) {
	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		reply = fallbackReply
	}
	suggestions := filterEmptyStrings(result.Suggestions)
	if len(suggestions) == 0 {
		suggestions = fallbackSuggestions
	}
	return reply, suggestions
}

func hasSuccessfulToolCall(toolCalls []ToolCall) bool {
	for _, call := range toolCalls {
		if call.Status == "success" {
			return true
		}
	}
	return false
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
		return "我是 CloudOps 智能助手，可以帮你查询主机、指标、活跃告警、告警事件和告警历史。"
	default:
		return "暂时无法识别您的意图，请说明您想查询主机、指标、活跃告警、告警事件还是告警历史。"
	}
}

func buildSuggestions(result nlu.Result) []string {
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return []string{"查看当前活跃告警", "查看严重级别告警"}
	case nlu.IntentAlertEventQuery:
		return []string{"查看最新告警事件", "查看最近已恢复告警"}
	case nlu.IntentAlertHistoryQuery:
		return []string{"查看最近一周CPU告警历史", "查看警告级别告警历史"}
	case nlu.IntentHostQuery:
		return []string{"查看当前主机列表", "查看离线主机"}
	case nlu.IntentMetricQuery:
		return []string{"查看 node-1 最近1小时CPU", "查看最近24小时内存趋势"}
	case nlu.IntentDiagnosisRequest:
		return []string{"显示最近 5 条告警历史", "显示当前 firing 告警"}
	case nlu.IntentGeneralChat:
		return []string{"当前有哪些活跃告警？", "哪些主机离线了？", "查看 node-1 的CPU趋势"}
	default:
		return []string{"当前有哪些活跃告警？", "哪些主机离线了？", "查看 node-1 的CPU趋势"}
	}
}
