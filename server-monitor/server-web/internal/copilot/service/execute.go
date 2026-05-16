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
	if !hasDiagnosisTarget(req) {
		inferredReq, toolCalls, reply, ok := s.inferDiagnosisTarget(ctx)
		if !ok {
			return toolCalls, reply, nil
		}
		req = inferredReq
		req.TriggerType = diagnosis.TriggerChat
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

func hasDiagnosisTarget(req diagnosis.Request) bool {
	return strings.TrimSpace(req.Fingerprint) != "" ||
		req.AlertHistoryID != 0 ||
		(strings.TrimSpace(req.AlertName) != "" && strings.TrimSpace(req.Instance) != "")
}

func (s *Service) inferDiagnosisTarget(ctx context.Context) (diagnosis.Request, []ToolCall, string, bool) {
	if s.tools == nil {
		err := fmt.Errorf("%w: 需要 fingerprint、alert_history_id，或 alert_name + instance", diagnosis.ErrInvalidRequest)
		return diagnosis.Request{}, []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: err.Error()}}, buildDiagnosisErrorReply(err), false
	}

	toolCalls, _, err := s.tools.Execute(ctx, nlu.Result{
		Intent:   nlu.IntentAlertQuery,
		Entities: map[string]string{"status": "firing"},
	})
	if err != nil {
		return diagnosis.Request{}, []ToolCall{{Name: "alert.list_active", Status: "error", Error: err.Error()}}, "", false
	}
	candidates := diagnosisCandidatesFromToolCalls(toolCalls)
	switch len(candidates) {
	case 0:
		if reply := firstToolErrorReply(toolCalls); reply != "" {
			return diagnosis.Request{}, toolCalls, reply, false
		}
		return diagnosis.Request{}, toolCalls, "当前没有 firing 告警可供自动诊断。请提供 fingerprint、alert_history_id，或先查询告警历史后再诊断。", false
	case 1:
		return requestFromDiagnosisCandidate(candidates[0]), nil, "", true
	default:
		return diagnosis.Request{}, []ToolCall{{
			Name:   "diagnosis.trigger",
			Status: "error",
			Error:  diagnosis.ErrConflict.Error(),
			Result: candidates,
		}}, buildDiagnosisCandidatesReply(candidates), false
	}
}

func firstToolErrorReply(toolCalls []ToolCall) string {
	for _, call := range toolCalls {
		if call.Status == "error" && call.Error != "" {
			return buildReply(nlu.Result{Intent: nlu.IntentAlertQuery}, "", toolCalls)
		}
	}
	return ""
}

func requestFromDiagnosisCandidate(candidate diagnosis.DiagnosisCandidate) diagnosis.Request {
	return diagnosis.Request{
		Fingerprint:    candidate.Fingerprint,
		AlertHistoryID: candidate.AlertHistoryID,
		AlertName:      candidate.AlertName,
		Instance:       candidate.Instance,
		TriggerType:    diagnosis.TriggerChat,
	}
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
		items := alertEntityItemsFromToolCalls(toolCalls)
		if len(items) == 1 {
			mergeEntityFields(entities, alertFieldsFromItem(items[0]))
		} else if len(items) > 1 {
			for i := len(items) - 1; i >= 0; i-- {
				fields := alertFieldsFromItem(items[i])
				if fields["status"] == "firing" {
					mergeEntityFields(entities, fields)
					break
				}
			}
		}
	case nlu.IntentHostQuery:
		if len(toolCalls) == 1 && toolCalls[0].Status == "success" {
			hosts := hostItemsFromToolCall(toolCalls[0])
			if len(hosts) == 1 {
				if instance, ok := hosts[0]["instance"].(string); ok && instance != "" {
					entities["instance"] = instance
				}
			}
		}
	}
	return entities
}

func hostItemsFromToolCall(call ToolCall) []map[string]interface{} {
	switch result := call.Result.(type) {
	case []interface{}:
		return mapsFromSlice(result)
	case []map[string]interface{}:
		return result
	case map[string]interface{}:
		if hosts, ok := result["hosts"].([]interface{}); ok {
			return mapsFromSlice(hosts)
		}
		return []map[string]interface{}{result}
	default:
		return nil
	}
}

func diagnosisCandidatesFromToolCalls(toolCalls []ToolCall) []diagnosis.DiagnosisCandidate {
	items := alertEntityItemsFromToolCalls(toolCalls)
	candidates := make([]diagnosis.DiagnosisCandidate, 0, len(items))
	for _, item := range items {
		fields := alertFieldsFromItem(item)
		if fields["fingerprint"] == "" && (fields["alert_name"] == "" || fields["instance"] == "") {
			continue
		}
		candidates = append(candidates, diagnosis.DiagnosisCandidate{
			AlertHistoryID: parseUintField(fields["alert_history_id"]),
			Fingerprint:    fields["fingerprint"],
			AlertName:      fields["alert_name"],
			Instance:       fields["instance"],
			Severity:       fields["severity"],
			Status:         fields["status"],
			Source:         "alert.list_active",
		})
	}
	return candidates
}

func alertEntityItemsFromToolCalls(toolCalls []ToolCall) []map[string]interface{} {
	var items []map[string]interface{}
	for _, call := range toolCalls {
		if call.Status != "success" {
			continue
		}
		items = append(items, alertEntityItems(call.Result)...)
	}
	return items
}

func alertEntityItems(result interface{}) []map[string]interface{} {
	switch value := result.(type) {
	case []interface{}:
		return mapsFromSlice(value)
	case []map[string]interface{}:
		return value
	case map[string]interface{}:
		if rawItems, ok := value["items"]; ok {
			return alertEntityItems(rawItems)
		}
		return []map[string]interface{}{value}
	default:
		return nil
	}
}

func mapsFromSlice(values []interface{}) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]interface{}); ok {
			items = append(items, item)
		}
	}
	return items
}

func alertFieldsFromItem(item map[string]interface{}) map[string]string {
	labels := stringMapFromAny(item["labels"])
	fields := map[string]string{
		"alert_history_id": stringFromAny(firstNonNil(item["alert_history_id"], item["id"])),
		"fingerprint":      stringFromAny(item["fingerprint"]),
		"alert_name":       firstNonEmpty(stringFromAny(item["alert_name"]), labels["alertname"]),
		"instance":         firstNonEmpty(stringFromAny(item["instance"]), labels["instance"]),
		"severity":         firstNonEmpty(stringFromAny(item["severity"]), labels["severity"]),
		"status":           stringFromAny(item["status"]),
	}
	return fields
}

func mergeEntityFields(dst map[string]string, fields map[string]string) {
	for key, value := range fields {
		if value != "" {
			dst[key] = value
		}
	}
}

func stringMapFromAny(value interface{}) map[string]string {
	result := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		for key, item := range typed {
			result[key] = strings.TrimSpace(item)
		}
	case map[string]interface{}:
		for key, item := range typed {
			if text := stringFromAny(item); text != "" {
				result[key] = text
			}
		}
	}
	return result
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stringFromAny(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatUint(uint64(typed), 10)
	case float32:
		return strconv.FormatUint(uint64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func parseUintField(value string) uint64 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return parsed
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
