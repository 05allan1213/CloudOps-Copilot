package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"server-web/internal/copilot/diagnosis"
	"server-web/internal/copilot/nlu"
)

func (s *Service) executeTools(ctx context.Context, result nlu.Result) ([]ToolCall, string, error) {
	if s.tools == nil || result.Intent == nlu.IntentUnknown || result.Intent == nlu.IntentGeneralChat {
		return []ToolCall{}, "", nil
	}
	return s.tools.Execute(ctx, result)
}

func (s *Service) executeIntent(ctx context.Context, user User, result nlu.Result) ([]ToolCall, string, error) {
	if result.Intent == nlu.IntentDiagnosisRequest {
		return s.executeDiagnosis(ctx, user, result)
	}
	return s.executeTools(ctx, result)
}

func (s *Service) executeIntents(ctx context.Context, user User, result nlu.Result) ([]ToolCall, string, []IntentResult, error) {
	if len(result.Intents) == 0 {
		toolCalls, reply, err := s.executeIntent(ctx, user, result)
		return toolCalls, reply, nil, err
	}

	var allToolCalls []ToolCall
	var allReplies []string
	var intentResults []IntentResult
	contextEntities := map[string]string{}

	for i, is := range result.Intents {
		mergedEntities := mergeEntities(contextEntities, is.Entities)
		mergedResult := nlu.Result{
			Intent:       is.Intent,
			Confidence:   is.Confidence,
			Entities:     mergedEntities,
			SelectedTool: is.SelectedTool,
		}

		toolCalls, reply, err := s.executeIntent(ctx, user, mergedResult)

		ir := IntentResult{
			Intent:     is.Intent,
			Confidence: is.Confidence,
			ToolCalls:  toolCalls,
			Reply:      reply,
		}
		if err != nil {
			ir.Error = err.Error()
			allReplies = append(allReplies, fmt.Sprintf("[意图%d失败: %v]", i+1, err))
		} else {
			allReplies = append(allReplies, reply)
		}

		allToolCalls = append(allToolCalls, toolCalls...)
		intentResults = append(intentResults, ir)
		contextEntities = s.extractContextEntities(toolCalls, is.Intent)
	}

	combinedReply := strings.Join(filterEmpty(allReplies), "\n")
	return allToolCalls, combinedReply, intentResults, nil
}

func (s *Service) executeDiagnosis(ctx context.Context, user User, result nlu.Result) ([]ToolCall, string, error) {
	if s.diagnosis == nil {
		return []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: "诊断服务不可用"}}, "", nil
	}
	req := diagnosis.Request{
		Fingerprint: result.Entities["fingerprint"],
		AlertName:   result.Entities["alert_name"],
		Instance:    result.Entities["instance"],
		TriggerType: diagnosis.TriggerChat,
	}
	if rawID := strings.TrimSpace(result.Entities["alert_history_id"]); rawID != "" {
		if id, err := strconv.ParseUint(rawID, 10, 64); err == nil {
			req.AlertHistoryID = id
		}
	}
	report, err := s.diagnosis.Trigger(ctx, diagnosis.User{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, req)
	if err != nil {
		var conflict diagnosis.ConflictError
		if errors.As(err, &conflict) {
			return []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: err.Error(), Result: conflict.Candidates}}, buildDiagnosisCandidatesReply(conflict.Candidates), nil
		}
		return []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: err.Error()}}, buildDiagnosisErrorReply(err), nil
	}
	reply := fmt.Sprintf("诊断报告已生成：#%d，状态 %s，置信度 %.0f%%。摘要：%s", report.ID, report.Status, report.Confidence*100, report.Summary)
	return []ToolCall{{Name: "diagnosis.trigger", Status: "success", Result: report}}, reply, nil
}

func buildDiagnosisCandidatesReply(candidates []diagnosis.DiagnosisCandidate) string {
	if len(candidates) == 0 {
		return "匹配到多条告警，请提供 fingerprint 或 alert_history_id 后再诊断。"
	}
	var builder strings.Builder
	builder.WriteString("匹配到多条告警，请选择一条后再诊断：")
	for i, candidate := range candidates {
		if i >= 5 {
			builder.WriteString("\n- 还有更多候选，请使用更精确的 fingerprint 或 alert_history_id")
			break
		}
		builder.WriteString(fmt.Sprintf(
			"\n- alert_history_id=%d fingerprint=%s alert=%s instance=%s status=%s",
			candidate.AlertHistoryID,
			candidate.Fingerprint,
			candidate.AlertName,
			candidate.Instance,
			candidate.Status,
		))
	}
	return builder.String()
}

func buildDiagnosisErrorReply(err error) string {
	switch {
	case errors.Is(err, diagnosis.ErrInvalidRequest):
		return "请提供真实的 fingerprint、alert_history_id，或 alert_name + instance 后再诊断。可以先查询最近告警历史，复制其中的 alert_history_id 或 fingerprint。"
	case errors.Is(err, diagnosis.ErrNotFound):
		return "没有找到匹配的告警目标。请先查询最近告警历史或当前 firing 告警，再使用真实的 alert_history_id 或 fingerprint 发起诊断。"
	default:
		return ""
	}
}

func (s *Service) extractContextEntities(toolCalls []ToolCall, intent string) map[string]string {
	entities := map[string]string{}
	if len(toolCalls) == 0 {
		return entities
	}

	switch intent {
	case nlu.IntentAlertQuery, nlu.IntentAlertEventQuery, nlu.IntentAlertHistoryQuery:
		if len(toolCalls) == 1 && toolCalls[0].Status == "success" {
			if result, ok := toolCalls[0].Result.(map[string]interface{}); ok {
				if items, ok := result["items"].([]interface{}); ok && len(items) == 1 {
					if item, ok := items[0].(map[string]interface{}); ok {
						if alertName, ok := item["alert_name"].(string); ok && alertName != "" {
							entities["alert_name"] = alertName
						}
						if fingerprint, ok := item["fingerprint"].(string); ok && fingerprint != "" {
							entities["fingerprint"] = fingerprint
						}
						if severity, ok := item["severity"].(string); ok && severity != "" {
							entities["severity"] = severity
						}
					}
				}
			}
		}
	case nlu.IntentHostQuery:
		if len(toolCalls) == 1 && toolCalls[0].Status == "success" {
			if result, ok := toolCalls[0].Result.(map[string]interface{}); ok {
				if hosts, ok := result["hosts"].([]interface{}); ok && len(hosts) == 1 {
					if host, ok := hosts[0].(map[string]interface{}); ok {
						if instance, ok := host["instance"].(string); ok && instance != "" {
							entities["instance"] = instance
						}
					}
				}
			}
		}
	}
	return entities
}

func mergeEntities(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		if v != "" {
			merged[k] = v
		}
	}
	return merged
}

func filterEmpty(ss []string) []string {
	var result []string
	for _, s := range ss {
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}
