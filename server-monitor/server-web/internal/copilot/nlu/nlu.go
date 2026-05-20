package nlu

import (
	"regexp"
	"strings"
	"time"
)

const (
	IntentAlertQuery         = "alert_query"
	IntentAlertEventQuery    = "alert_event_query"
	IntentAlertHistoryQuery  = "alert_history_query"
	IntentAlertRuleListQuery = "alert_rule_list_query"
	IntentDiagnosisRequest   = "diagnosis_request"
	IntentHostQuery          = "host_query"
	IntentMetricQuery        = "metric_query"
	IntentGeneralChat        = "general_chat"
	IntentUnknown            = "unknown"
)

const (
	ToolK8sGetPods        = "k8s.get_pods"
	ToolK8sGetDeployments = "k8s.get_deployments"
	ToolK8sGetServices    = "k8s.get_services"
	ToolK8sGetNodes       = "k8s.get_nodes"
	ToolK8sGetEvents      = "k8s.get_events"
	ToolK8sGetLogs        = "k8s.get_logs"
)

type Result struct {
	Intent       string
	Confidence   float64
	Entities     map[string]string
	Intents      []IntentScore
	SelectedTool string
	Source       string
}

type IntentScore struct {
	Intent       string
	Confidence   float64
	Entities     map[string]string
	SelectedTool string
}

type NLUObserver interface {
	ObserveNLUClassify(intent, source string, durationSeconds float64)
}

type Classifier struct {
	observer NLUObserver
}

type ClassifierOption func(*Classifier)

func WithNLUObserver(obs NLUObserver) ClassifierOption {
	return func(c *Classifier) {
		c.observer = obs
	}
}

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
		regexp.MustCompile(`(?i)\b(HighCPU|CriticalCPU|HighMemory|HighDisk|HostDown)\b`),
		regexp.MustCompile(`(?i)\balert[_ -]?name\s*[:=]\s*([a-z0-9_.:-]+)\b`),
		regexp.MustCompile(`(?i)\b([a-z][a-z0-9_.:-]*(?:cpu|memory|disk|down)[a-z0-9_.:-]*)\s*(?:alert\b|告警)`),
		regexp.MustCompile(`(?i)\b([a-z][a-z0-9_.:-]*(?:cpu|memory|disk|load|network)[a-z0-9_.:-]*)\s+alerts?\s+history\b`),
		regexp.MustCompile(`(?i)\b(cpu|memory|disk|load|network)\s+alerts?\s+history\b`),
		regexp.MustCompile(`(?i)(cpu|内存|memory|磁盘|disk|负载|load|网络|network)\s*告警\s*历史`),
		regexp.MustCompile(`(?i)(?:^|[\s的])([a-z][a-z0-9_.:-]{1,127})\s*告警`),
	}
	fingerprintPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bfingerprint\s*(?:[:=]|为|is)?\s*([a-z0-9][a-z0-9_.:-]{2,127})\b`),
	}
	alertHistoryIDPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\balert[_ -]?history[_ -]?id\s*(?:[:=]|为|is)?\s*(\d{1,20})\b`),
		regexp.MustCompile(`(?i)\bhistory[_ -]?id\s*(?:[:=]|为|is)?\s*(\d{1,20})\b`),
		regexp.MustCompile(`(?i)历史记录\s*(?:id)?\s*(?:[:=]|为)?\s*(\d{1,20})`),
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
	k8sNamespacePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bnamespace\s*[:=]?\s*([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)\b`),
		regexp.MustCompile(`(?i)([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)\s*(?:namespace|命名空间)`),
		regexp.MustCompile(`(?i)(?:namespace|命名空间)\s*([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)`),
	}
	k8sPodNamePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bpod\s+([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)\b`),
		regexp.MustCompile(`(?i)pod\s*([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)\s*的日志`),
	}
)

var multiIntentConnectors = []string{
	"并", "并且", "和", "同时", "然后", "再", "接着",
	"以及", "且", "还有", "另外",
	"and", "then", "also", "plus",
}

var multiIntentHints = []string{
	"并", "和", "同时", "然后", "再", "接着", "以及", "且", "还有", "另外",
	"and", "then", "also", "plus", "as well as",
}

func NewClassifier(opts ...ClassifierOption) *Classifier {
	c := &Classifier{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Classifier) Classify(message string) Result {
	start := time.Now()
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		result := unknownResult()
		c.observeResult(result, start)
		return result
	}

	entities := extractEntities(message, normalized)
	hasAlert := containsAny(normalized, "告警", "alert", "firing", "resolved", "severity")
	hasDiagnosis := containsAny(normalized, "诊断", "分析", "diagnose", "diagnosis", "analyze")
	hasAlertRule := containsAny(normalized, "告警规则", "alert rule", "alert rules", "rule list", "rules")
	hasHistory := containsAny(normalized, "历史", "history", "过去", "最近一周", "last week")
	hasEvent := containsAny(normalized, "事件", "event", "events", "最新", "latest")
	hasHost := containsAny(normalized, "主机", "host", "hosts", "instance", "机器", "节点", "node", "nodes", "离线", "offline")
	hasMetric := containsAny(normalized, "cpu", "内存", "memory", "磁盘", "disk", "负载", "load", "网络", "network", "metric", "metrics", "趋势", "trend", "promql", "query_range", "指标")
	hasHostListOption := containsAny(normalized, "search", "q=", "group", "group_id", "sort", "risk", "high cpu", "high memory", "高cpu", "高内存", "cpu_desc", "memory_desc")
	hasGeneral := containsAny(normalized, "能做什么", "help", "帮助", "what can you do", "解释", "explain", "你好", "hello", "hi")
	if result, ok := classifyK8sQuery(normalized, entities); ok {
		c.observeResult(result, start)
		return result
	}

	var result Result
	switch {
	case hasDiagnosis && hasAlert:
		result = Result{Intent: IntentDiagnosisRequest, Confidence: 0.9, Entities: entities}
	case hasDiagnosis:
		result = Result{Intent: IntentDiagnosisRequest, Confidence: 0.82, Entities: entities}
	case hasAlertRule:
		result = Result{Intent: IntentAlertRuleListQuery, Confidence: 0.89, Entities: entities}
	case hasAlert && hasHistory:
		result = Result{Intent: IntentAlertHistoryQuery, Confidence: 0.9, Entities: entities}
	case hasAlert && hasEvent:
		result = Result{Intent: IntentAlertEventQuery, Confidence: 0.88, Entities: entities}
	case hasAlert:
		result = Result{Intent: IntentAlertQuery, Confidence: 0.9, Entities: entities}
	case hasHost && hasHostListOption:
		result = Result{Intent: IntentHostQuery, Confidence: 0.87, Entities: entities}
	case hasMetric:
		result = Result{Intent: IntentMetricQuery, Confidence: 0.84, Entities: entities}
	case hasHost:
		result = Result{Intent: IntentHostQuery, Confidence: 0.85, Entities: entities}
	case hasGeneral:
		result = Result{Intent: IntentGeneralChat, Confidence: 0.72, Entities: entities}
	default:
		result = unknownResult()
	}

	c.observeResult(result, start)
	return result
}

func (c *Classifier) observeResult(result Result, start time.Time) {
	if c.observer == nil {
		return
	}
	source := "rule"
	if result.Confidence < 0.6 {
		source = "rule-low"
	}
	c.observer.ObserveNLUClassify(result.Intent, source, time.Since(start).Seconds())
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
	if fingerprint := extractFirstPattern(original, fingerprintPatterns); fingerprint != "" {
		entities["fingerprint"] = fingerprint
	}
	if alertHistoryID := extractFirstPattern(original, alertHistoryIDPatterns); alertHistoryID != "" {
		entities["alert_history_id"] = alertHistoryID
	}
	if search := extractFirstPattern(original, searchPatterns); search != "" {
		entities["search"] = search
	}
	if groupID := extractFirstPattern(original, groupIDPatterns); groupID != "" {
		entities["group_id"] = groupID
	}
	if namespace := extractK8sNamespace(original); namespace != "" {
		entities["namespace"] = namespace
	}
	if podName := extractK8sPodName(original); podName != "" {
		entities["pod_name"] = podName
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
	if instance := extractInstance(original, entities["alert_name"], entities["fingerprint"], entities["alert_history_id"], entities["page"], entities["page_size"], entities["count"], entities["group_id"], entities["search"], entities["namespace"], entities["pod_name"]); instance != "" {
		entities["instance"] = instance
	}
	if metricKw := extractMetricKeywords(normalized); len(metricKw) > 0 {
		entities["metric_keywords"] = strings.Join(metricKw, ",")
	}
	return entities
}

func classifyK8sQuery(normalized string, entities map[string]string) (Result, bool) {
	if !hasK8sContext(normalized) {
		return Result{}, false
	}
	selectedTool := ""
	switch {
	case containsAny(normalized, "日志", "log", "logs"):
		selectedTool = ToolK8sGetLogs
	case containsAny(normalized, "event", "events") || (containsAny(normalized, "事件") && containsAny(normalized, "k8s", "kubernetes", "pod", "deployment", "service", "namespace", "命名空间")):
		selectedTool = ToolK8sGetEvents
	case containsAny(normalized, "deployment", "deployments", "deploy"):
		selectedTool = ToolK8sGetDeployments
	case containsAny(normalized, "service", "services", "svc"):
		selectedTool = ToolK8sGetServices
	case containsAny(normalized, "pod", "pods"):
		selectedTool = ToolK8sGetPods
	case containsAny(normalized, "node", "nodes", "节点"):
		selectedTool = ToolK8sGetNodes
	default:
		return Result{}, false
	}
	return Result{
		Intent:       IntentMetricQuery,
		Confidence:   0.93,
		Entities:     entities,
		SelectedTool: selectedTool,
	}, true
}

func hasK8sContext(normalized string) bool {
	if containsAny(normalized, "k8s", "kubernetes", "namespace", "命名空间", "pod", "pods", "deployment", "deployments", "svc", "container", "容器") {
		return true
	}
	if containsAny(normalized, "service", "services") && containsAny(normalized, "集群", "cluster") {
		return true
	}
	if containsAny(normalized, "node", "nodes", "节点") && containsAny(normalized, "k8s", "kubernetes", "集群", "cluster") {
		return true
	}
	return false
}

func extractK8sNamespace(original string) string {
	return strings.TrimSpace(extractFirstPattern(original, k8sNamespacePatterns))
}

func extractK8sPodName(original string) string {
	return strings.TrimSpace(extractFirstPattern(original, k8sPodNamePatterns))
}

var metricKeywordDefs = map[string]string{
	"cpu":     "cpu",
	"内存":      "memory",
	"memory":  "memory",
	"磁盘":      "disk",
	"disk":    "disk",
	"负载":      "load",
	"load":    "load",
	"网络":      "network",
	"network": "network",
}

func extractMetricKeywords(normalized string) []string {
	var keywords []string
	seen := make(map[string]struct{})
	for kw, canonical := range metricKeywordDefs {
		if strings.Contains(normalized, kw) {
			if _, dup := seen[canonical]; !dup {
				seen[canonical] = struct{}{}
				keywords = append(keywords, canonical)
			}
		}
	}
	return keywords
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
	case strings.Contains(normalized, "昨天"), strings.Contains(normalized, "yesterday"):
		return "24h"
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
	case "list", "show", "get", "current", "latest", "recent", "last",
		"cpu", "memory", "disk", "load", "network", "metric", "metrics", "alert", "alerts", "rule", "rules", "host", "hosts", "node", "nodes", "event", "events", "history", "records", "query", "trend", "info", "warning", "critical", "firing", "resolved", "enabled", "disabled", "promql", "alert_name", "alertname", "page", "page_size", "count", "search", "sort", "risk", "group", "group_id", "high_cpu", "high_memory", "cpu_desc", "memory_desc", "k8s", "kubernetes":
		return true
	case "fingerprint":
		return true
	case "alert_history_id", "history_id":
		return true
	default:
		return false
	}
}

func (c *Classifier) ClassifyMulti(message string) Result {
	return c.ClassifyMultiWithMax(message, 3)
}

func (c *Classifier) ClassifyMultiWithMax(message string, maxIntents int) Result {
	if maxIntents <= 0 {
		maxIntents = 3
	}
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		result := unknownResult()
		c.observeResult(result, time.Now())
		return result
	}

	_, connStart := findFirstValidConnector(normalized)
	if connStart < 0 {
		result := c.Classify(message)
		return result
	}

	clauses := splitByConnectors(message)
	if len(clauses) < 2 {
		result := c.Classify(message)
		return result
	}

	var intents []IntentScore

	for i, clause := range clauses {
		if len(intents) >= maxIntents {
			break
		}
		classifyInput := clause
		if i > 0 && len(intents) > 0 {
			classifyInput = augmentClauseWithContext(clause, intents[0])
		}
		clauseResult := c.Classify(classifyInput)
		if i > 0 {
			clauseResult = inheritContext(clauseResult, intents[0], clause)
		}
		if clauseResult.Intent == IntentUnknown || clauseResult.Confidence < 0.5 {
			clauseResult = c.Classify(clause)
			if i > 0 {
				clauseResult = inheritContext(clauseResult, intents[0], clause)
			}
		}
		dupIdx := findDuplicateIntent(intents, clauseResult)
		if dupIdx >= 0 && entitiesEqual(intents[dupIdx].Entities, clauseResult.Entities) {
			continue
		}
		if dupIdx >= 0 {
			intents = append(intents, IntentScore{
				Intent:       clauseResult.Intent,
				Confidence:   clauseResult.Confidence,
				Entities:     clauseResult.Entities,
				SelectedTool: clauseResult.SelectedTool,
			})
			continue
		}
		intents = append(intents, IntentScore{
			Intent:       clauseResult.Intent,
			Confidence:   clauseResult.Confidence,
			Entities:     clauseResult.Entities,
			SelectedTool: clauseResult.SelectedTool,
		})
	}

	if len(intents) == 0 {
		return c.Classify(message)
	}

	return Result{
		Intent:     intents[0].Intent,
		Confidence: intents[0].Confidence,
		Entities:   intents[0].Entities,
		Intents:    intents,
	}
}

func TrimIntents(result Result, max int) Result {
	if max <= 0 || len(result.Intents) <= max {
		return result
	}
	trimmed := make([]IntentScore, max)
	copy(trimmed, result.Intents[:max])
	return Result{
		Intent:       result.Intent,
		Confidence:   result.Confidence,
		Entities:     result.Entities,
		Intents:      trimmed,
		SelectedTool: result.SelectedTool,
		Source:       result.Source,
	}
}

func splitByConnectors(message string) []string {
	lower := strings.ToLower(message)
	connector, connStart := findFirstValidConnector(lower)
	if connector == "" {
		return []string{message}
	}

	clause1 := strings.TrimSpace(message[:connStart])
	remainder := strings.TrimSpace(message[connStart+len(connector):])

	if clause1 == "" || remainder == "" {
		return []string{message}
	}

	rest := splitByConnectors(remainder)
	return append([]string{clause1}, rest...)
}

func findFirstValidConnector(lower string) (string, int) {
	bestConn := ""
	bestIdx := -1

	for _, conn := range multiIntentConnectors {
		searchFrom := 0
		for {
			idx := strings.Index(lower[searchFrom:], conn)
			if idx < 0 {
				break
			}
			absIdx := searchFrom + idx
			if isFalseConnector(lower, absIdx, conn) {
				searchFrom = absIdx + len(conn)
				continue
			}
			if bestIdx < 0 || absIdx < bestIdx {
				bestConn = conn
				bestIdx = absIdx
			}
			break
		}
	}
	return bestConn, bestIdx
}

var falseConnectorPatterns = []string{"再说", "和谐", "同时期", "然后呢"}

func isFalseConnector(lower string, idx int, conn string) bool {
	after := lower[idx:]
	for _, p := range falseConnectorPatterns {
		if strings.HasPrefix(after, p) {
			return true
		}
	}
	return false
}

func augmentClauseWithContext(clause string, primary IntentScore) string {
	lower := strings.ToLower(clause)
	hasDiag := containsAny(lower, "诊断", "分析", "diagnose", "diagnosis", "analyze")
	if !hasDiag {
		return clause
	}
	switch primary.Intent {
	case IntentAlertQuery, IntentAlertEventQuery, IntentAlertHistoryQuery:
		return clause + " 告警"
	default:
		return clause
	}
}

func findDuplicateIntent(intents []IntentScore, result Result) int {
	for i, is := range intents {
		if is.Intent == result.Intent {
			return i
		}
	}
	return -1
}

func entitiesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func inheritContext(result Result, primary IntentScore, clause string) Result {
	if result.Entities == nil {
		result.Entities = map[string]string{}
	}
	for k, v := range primary.Entities {
		if _, exists := result.Entities[k]; !exists {
			result.Entities[k] = v
		}
	}
	return result
}

func mergeIntentEntities(intents []IntentScore, newIntent Result, key string) []IntentScore {
	for i := range intents {
		if intents[i].Intent == key {
			if intents[i].Entities == nil {
				intents[i].Entities = map[string]string{}
			}
			for k, v := range newIntent.Entities {
				if v != "" {
					intents[i].Entities[k] = v
				}
			}
			return intents
		}
	}
	return nil
}

func HasMultiIntentHints(message string) bool {
	lower := strings.ToLower(message)
	for _, hint := range multiIntentHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func unknownResult() Result {
	return Result{
		Intent:     IntentUnknown,
		Confidence: 0,
		Entities:   map[string]string{},
	}
}
