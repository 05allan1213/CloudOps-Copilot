package nlu

import (
	"regexp"
	"strings"
)

const (
	IntentAlertQuery        = "alert_query"
	IntentAlertEventQuery   = "alert_event_query"
	IntentAlertHistoryQuery = "alert_history_query"
	IntentHostQuery         = "host_query"
	IntentMetricQuery       = "metric_query"
	IntentGeneralChat       = "general_chat"
	IntentUnknown           = "unknown"
)

type Result struct {
	Intent     string
	Confidence float64
	Entities   map[string]string
}

type Classifier struct{}

var (
	instancePattern = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9._-]*(:\d{2,5})?|(?:\d{1,3}\.){3}\d{1,3}(:\d{2,5})?)\b`)
	windowPattern   = regexp.MustCompile(`(?i)\b(15m|1h|6h|24h)\b`)
)

func NewClassifier() *Classifier {
	return &Classifier{}
}

func (c *Classifier) Classify(message string) Result {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return unknownResult()
	}

	entities := extractEntities(message, normalized)
	hasAlert := containsAny(normalized, "告警", "alert", "firing", "resolved", "severity")
	hasHistory := containsAny(normalized, "历史", "history", "过去", "最近一周", "last week")
	hasEvent := containsAny(normalized, "事件", "event", "events", "最新", "latest")
	hasHost := containsAny(normalized, "主机", "host", "hosts", "instance", "机器", "节点", "node", "nodes", "离线", "offline")
	hasMetric := containsAny(normalized, "cpu", "内存", "memory", "磁盘", "disk", "负载", "load", "网络", "network", "metric", "metrics", "趋势", "trend")
	hasGeneral := containsAny(normalized, "能做什么", "help", "帮助", "what can you do", "解释", "explain")

	switch {
	case hasAlert && hasHistory:
		return Result{Intent: IntentAlertHistoryQuery, Confidence: 0.9, Entities: entities}
	case hasAlert && hasEvent:
		return Result{Intent: IntentAlertEventQuery, Confidence: 0.88, Entities: entities}
	case hasAlert:
		return Result{Intent: IntentAlertQuery, Confidence: 0.9, Entities: entities}
	case hasMetric:
		return Result{Intent: IntentMetricQuery, Confidence: 0.84, Entities: entities}
	case hasHost:
		return Result{Intent: IntentHostQuery, Confidence: 0.85, Entities: entities}
	case hasGeneral:
		return Result{Intent: IntentGeneralChat, Confidence: 0.72, Entities: entities}
	default:
		return unknownResult()
	}
}

func extractEntities(original, normalized string) map[string]string {
	entities := map[string]string{}
	if severity := extractSeverity(normalized); severity != "" {
		entities["severity"] = severity
	}
	if window := extractWindow(original, normalized); window != "" {
		entities["window"] = window
	}
	if instance := extractInstance(original); instance != "" && !isCommonKeyword(instance) {
		entities["instance"] = instance
	}
	return entities
}

func extractSeverity(normalized string) string {
	for _, severity := range []string{"critical", "warning", "info"} {
		if strings.Contains(normalized, severity) {
			return severity
		}
	}
	return ""
}

func extractWindow(original, normalized string) string {
	if match := windowPattern.FindString(original); match != "" {
		return strings.ToLower(match)
	}
	switch {
	case strings.Contains(normalized, "最近一周"), strings.Contains(normalized, "last week"):
		return "7d"
	case strings.Contains(normalized, "今天"), strings.Contains(normalized, "today"):
		return "24h"
	default:
		return "15m"
	}
}

func extractInstance(original string) string {
	matches := instancePattern.FindAllString(original, -1)
	for _, match := range matches {
		trimmed := strings.Trim(match, ".,?!;:")
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func containsAny(value string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

func isCommonKeyword(value string) bool {
	switch strings.ToLower(value) {
	case "cpu", "memory", "disk", "load", "network", "metric", "metrics", "alert", "alerts", "host", "hosts", "node", "nodes", "event", "events", "info", "warning", "critical", "firing", "resolved":
		return true
	default:
		return false
	}
}

func unknownResult() Result {
	return Result{
		Intent:     IntentUnknown,
		Confidence: 0,
		Entities:   map[string]string{},
	}
}
