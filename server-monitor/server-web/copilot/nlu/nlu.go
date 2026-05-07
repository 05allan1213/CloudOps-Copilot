package nlu

import (
	"regexp"
	"strings"
)

const (
	IntentAlertQuery         = "alert_query"
	IntentAlertEventQuery    = "alert_event_query"
	IntentAlertHistoryQuery  = "alert_history_query"
	IntentAlertRuleListQuery = "alert_rule_list_query"
	IntentHostQuery          = "host_query"
	IntentMetricQuery        = "metric_query"
	IntentGeneralChat        = "general_chat"
	IntentUnknown            = "unknown"
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
	queryWindowOnly = regexp.MustCompile(`(?i)^(15m|1h|6h|24h|7d)$`)
	countPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:latest|last|recent)\s+(\d{1,3})\b`),
		regexp.MustCompile(`(?i)\b(\d{1,3})\s+(?:events?|alerts?|records?)\b`),
		regexp.MustCompile(`(?i)(?:最近|最新)\s*(\d{1,3})\s*(?:条|个)?`),
	}
	pagePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bpage\s+(\d{1,4})\b`),
		regexp.MustCompile(`(?i)\bpage\s*[:=]\s*(\d{1,4})\b`),
		regexp.MustCompile(`(?i)第\s*(\d{1,4})\s*页`),
	}
	pageSizePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bpage[_ -]?size\s*[:=]?\s*(\d{1,3})\b`),
		regexp.MustCompile(`(?i)\b(\d{1,3})\s*(?:per page|each page)\b`),
		regexp.MustCompile(`(?i)每页\s*(\d{1,3})\s*条`),
	}
	alertNamePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\balert[_ -]?name\s*[:=]\s*([a-z0-9_.:-]+)\b`),
		regexp.MustCompile(`(?i)\b([a-z][a-z0-9_.:-]*(?:cpu|memory|disk|load|network)[a-z0-9_.:-]*)\s+alerts?\s+history\b`),
		regexp.MustCompile(`(?i)\b(cpu|memory|disk|load|network)\s+alerts?\s+history\b`),
		regexp.MustCompile(`(?i)(cpu|内存|memory|磁盘|disk|负载|load|网络|network)\s*告警\s*历史`),
	}
	searchPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsearch\s*[:=]\s*([a-z0-9_.:-]+)\b`),
		regexp.MustCompile(`(?i)\bq\s*[:=]\s*([a-z0-9_.:-]+)\b`),
		regexp.MustCompile(`(?i)(?:搜索|查找)\s*([a-z0-9_.:-]+)`),
	}
	groupIDPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bgroup[_ -]?id\s*[:=]?\s*(\d{1,20})\b`),
		regexp.MustCompile(`(?i)\bgroup\s*[:=]\s*(\d{1,20})\b`),
		regexp.MustCompile(`(?i)主机组\s*(\d{1,20})`),
	}
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
	hasAlertRule := containsAny(normalized, "告警规则", "alert rule", "alert rules", "rule list", "rules")
	hasHistory := containsAny(normalized, "历史", "history", "过去", "最近一周", "last week")
	hasEvent := containsAny(normalized, "事件", "event", "events", "最新", "latest")
	hasHost := containsAny(normalized, "主机", "host", "hosts", "instance", "机器", "节点", "node", "nodes", "离线", "offline")
	hasMetric := containsAny(normalized, "cpu", "内存", "memory", "磁盘", "disk", "负载", "load", "网络", "network", "metric", "metrics", "趋势", "trend", "promql", "query_range")
	hasHostListOption := containsAny(normalized, "search", "q=", "group", "group_id", "sort", "risk", "high cpu", "high memory", "高cpu", "高内存", "cpu_desc", "memory_desc")
	hasGeneral := containsAny(normalized, "能做什么", "help", "帮助", "what can you do", "解释", "explain")

	switch {
	case hasAlertRule:
		return Result{Intent: IntentAlertRuleListQuery, Confidence: 0.89, Entities: entities}
	case hasAlert && hasHistory:
		return Result{Intent: IntentAlertHistoryQuery, Confidence: 0.9, Entities: entities}
	case hasAlert && hasEvent:
		return Result{Intent: IntentAlertEventQuery, Confidence: 0.88, Entities: entities}
	case hasAlert:
		return Result{Intent: IntentAlertQuery, Confidence: 0.9, Entities: entities}
	case hasHost && hasHostListOption:
		return Result{Intent: IntentHostQuery, Confidence: 0.87, Entities: entities}
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
	if status := extractStatus(normalized); status != "" {
		entities["status"] = status
	}
	if severity := extractSeverity(normalized); severity != "" {
		entities["severity"] = severity
	}
	if count := extractCount(original); count != "" {
		entities["count"] = count
	}
	if page := extractFirstPattern(original, pagePatterns); page != "" {
		entities["page"] = page
	}
	if pageSize := extractFirstPattern(original, pageSizePatterns); pageSize != "" {
		entities["page_size"] = pageSize
	}
	if alertName := extractAlertName(original); alertName != "" {
		entities["alert_name"] = alertName
	}
	if search := extractFirstPattern(original, searchPatterns); search != "" {
		entities["search"] = search
	}
	if groupID := extractFirstPattern(original, groupIDPatterns); groupID != "" {
		entities["group_id"] = groupID
	}
	if enabled := extractEnabled(normalized); enabled != "" {
		entities["enabled"] = enabled
	}
	if sort := extractSort(normalized); sort != "" {
		entities["sort"] = sort
	}
	if risk := extractRisk(normalized); risk != "" {
		entities["risk"] = risk
	}
	if query := extractPromQL(original); query != "" {
		entities["query"] = query
	}
	if window := extractWindow(original, normalized, entities["query"] != ""); window != "" {
		entities["window"] = window
	}
	if instance := extractInstance(original, entities["alert_name"], entities["page"], entities["page_size"], entities["count"], entities["group_id"], entities["search"]); instance != "" {
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

func extractEnabled(normalized string) string {
	switch {
	case containsAny(normalized, "disabled", "禁用", "未启用", "停用"):
		return "false"
	case containsAny(normalized, "enabled", "启用", "已启用"):
		return "true"
	default:
		return ""
	}
}

func extractStatus(normalized string) string {
	switch {
	case containsAny(normalized, "firing"):
		return "firing"
	case containsAny(normalized, "resolved", "已恢复", "恢复"):
		return "resolved"
	case containsAny(normalized, "离线", "offline", "down"):
		return "down"
	case containsAny(normalized, "在线", "healthy", "up"):
		return "up"
	default:
		return ""
	}
}

func extractCount(original string) string {
	return extractFirstPattern(original, countPatterns)
}

func extractFirstPattern(original string, patterns []*regexp.Regexp) string {
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(original)
		if len(matches) == 2 {
			return matches[1]
		}
	}
	return ""
}

func extractAlertName(original string) string {
	return strings.TrimSpace(extractFirstPattern(original, alertNamePatterns))
}

func extractSort(normalized string) string {
	switch {
	case containsAny(normalized, "cpu_desc", "highest cpu", "cpu desc", "按cpu排序", "按 cpu 排序"):
		return "cpu_desc"
	case containsAny(normalized, "memory_desc", "highest memory", "memory desc", "按memory排序", "按 memory 排序", "按内存排序", "按 内存 排序"):
		return "memory_desc"
	case containsAny(normalized, "instance sort", "sort by instance", "按实例排序", "按 instance 排序"):
		return "instance"
	default:
		return ""
	}
}

func extractRisk(normalized string) string {
	switch {
	case containsAny(normalized, "high_cpu", "high cpu", "cpu risk", "cpu高", "高cpu", "cpu 风险"):
		return "high_cpu"
	case containsAny(normalized, "high_memory", "high memory", "memory risk", "内存高", "高内存", "memory 风险"):
		return "high_memory"
	default:
		return ""
	}
}

func extractWindow(original, normalized string, hasPromQL bool) string {
	if hasPromQL {
		if window := extractPromQLWindow(original); window != "" {
			return window
		}
	}
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

func extractInstance(original string, ignoredValues ...string) string {
	matches := instancePattern.FindAllString(original, -1)
	for _, match := range matches {
		trimmed := strings.Trim(match, ".,?!;:")
		if trimmed != "" && !isCommonKeyword(trimmed) && !isIgnoredValue(trimmed, ignoredValues) {
			return trimmed
		}
	}
	return ""
}

func isIgnoredValue(value string, ignoredValues []string) bool {
	for _, ignored := range ignoredValues {
		if strings.EqualFold(value, ignored) {
			return true
		}
	}
	return false
}

func extractPromQL(original string) string {
	for _, marker := range []string{"promql:", "PromQL:", "query=", "QUERY="} {
		index := strings.Index(original, marker)
		if index < 0 {
			continue
		}
		query := strings.TrimSpace(original[index+len(marker):])
		return trimTrailingQueryWindow(strings.Trim(query, "` "))
	}
	return ""
}

func extractPromQLWindow(original string) string {
	query := rawPromQLText(original)
	fields := strings.Fields(strings.Trim(query, "` "))
	if len(fields) == 0 {
		return ""
	}
	if queryWindowOnly.MatchString(fields[len(fields)-1]) {
		return strings.ToLower(fields[len(fields)-1])
	}
	return ""
}

func trimTrailingQueryWindow(query string) string {
	fields := strings.Fields(query)
	if len(fields) <= 1 {
		return query
	}
	if !queryWindowOnly.MatchString(fields[len(fields)-1]) {
		return query
	}
	return strings.TrimSpace(strings.TrimSuffix(query, fields[len(fields)-1]))
}

func rawPromQLText(original string) string {
	for _, marker := range []string{"promql:", "PromQL:", "query=", "QUERY="} {
		index := strings.Index(original, marker)
		if index >= 0 {
			return strings.TrimSpace(original[index+len(marker):])
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
	case "cpu", "memory", "disk", "load", "network", "metric", "metrics", "alert", "alerts", "rule", "rules", "host", "hosts", "node", "nodes", "event", "events", "info", "warning", "critical", "firing", "resolved", "enabled", "disabled", "promql", "alert_name", "alertname", "page", "page_size", "count", "search", "sort", "risk", "group", "group_id", "high_cpu", "high_memory", "cpu_desc", "memory_desc":
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
