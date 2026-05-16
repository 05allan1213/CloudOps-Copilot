package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	appalert "server-web/alert"
	k8sreader "server-web/copilot/k8s"
	"server-web/copilot/nlu"
	"server-web/copilot/runbook"
	copilot "server-web/copilot/service"
	apphost "server-web/host"
	promclient "server-web/internal/infra/prometheus"
	"server-web/internal/infra/webhook"
	"server-web/internal/model"
)

const (
	StatusSuccess = "success"
	StatusError   = "error"

	ToolHostList          = "host.list"
	ToolHostMetrics       = "host.metrics"
	ToolAlertListActive   = "alert.list_active"
	ToolAlertEvents       = "alert.events"
	ToolAlertHistory      = "alert.history"
	ToolAlertRuleList     = "alert.rule_list"
	ToolPromQueryRange    = "prom.query_range"
	ToolRunbookSearch     = "runbook.search"
	ToolK8sGetPods        = "k8s.get_pods"
	ToolK8sGetDeployments = "k8s.get_deployments"
	ToolK8sGetServices    = "k8s.get_services"
	ToolK8sGetNodes       = "k8s.get_nodes"
	ToolK8sGetEvents      = "k8s.get_events"
	ToolK8sGetLogs        = "k8s.get_logs"

	defaultToolTimeout = 30 * time.Second

	defaultAlertEventsCount = int64(20)
	maxAlertEventsCount     = int64(100)

	defaultAlertHistoryPage     = 1
	defaultAlertHistoryPageSize = 20
	maxAlertHistoryPageSize     = 100
)

var (
	promOffsetPattern   = regexp.MustCompile(`(?i)\boffset\s+([0-9]+)\s*([smhdwy])\b`)
	promSubqueryPattern = regexp.MustCompile(`\[[^\]]+:[^\]]*\]`)
)

type HostService interface {
	Hosts(ctx context.Context, options apphost.ListOptions) ([]promclient.Host, error)
	Metrics(ctx context.Context, instance string, rangeName string, mountpoint string, now time.Time) (apphost.MetricsResponse, bool, error)
}

type AlertService interface {
	Enabled() bool
	ActiveAlerts(ctx context.Context, severityFilter string) ([]webhook.AlertRecord, error)
	AlertEvents(ctx context.Context, limit int64, statusFilter, severityFilter string) ([]webhook.AlertEvent, error)
}

type PrometheusClient interface {
	QueryRangeRaw(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]promclient.RangeSeries, error)
}

type RunbookSearcher interface {
	Search(ctx context.Context, req runbook.SearchRequest) ([]runbook.SearchResult, error)
	HealthCheck(ctx context.Context) bool
	Count() int
}

type Executor struct {
	hostService     HostService
	alertService    AlertService
	promClient      PrometheusClient
	runbookSearcher RunbookSearcher
	k8sReader       k8sreader.Reader
	k8sNodesEnabled bool
	db              *gorm.DB
	registry        Registry
	timeout         time.Duration
	now             func() time.Time
}

type Options struct {
	HostService     HostService
	AlertService    AlertService
	PromClient      PrometheusClient
	RunbookSearcher RunbookSearcher
	K8sReader       k8sreader.Reader
	K8sNodesEnabled bool
	DB              *gorm.DB
	Observer        Observer
	Timeout         time.Duration
	LogArgs         bool
	Now             func() time.Time
}

type DisabledExecutor struct{}

func NewDisabledExecutor() DisabledExecutor {
	return DisabledExecutor{}
}

func (DisabledExecutor) Execute(context.Context, nlu.Result) ([]copilot.ToolCall, string, error) {
	return nil, "", ErrToolUnavailable
}

func (DisabledExecutor) ToolSchemas() []copilot.ToolSchema {
	return []copilot.ToolSchema{}
}

type historyResult struct {
	Items    []model.AlertHistory `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type alertHistoryQueryOptions struct {
	Status    string
	Severity  string
	AlertName string
	Instance  string
	Window    time.Duration
	Page      int
	PageSize  int
}

type alertRuleListResult struct {
	Items []model.AlertRule `json:"items"`
	Total int64             `json:"total"`
}

type alertRuleListQueryOptions struct {
	Enabled  *bool
	Severity string
	Search   string
}

type hostListQueryOptions struct {
	ListOptions apphost.ListOptions
	GroupID     uint64
}

func NewExecutor(options Options) (*Executor, error) {
	return newExecutor(options, NewRegistry(WithLogArgs(options.LogArgs), WithObserver(options.Observer)))
}

func newExecutor(options Options, registry Registry) (*Executor, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	executor := &Executor{
		hostService:     options.HostService,
		alertService:    options.AlertService,
		promClient:      options.PromClient,
		runbookSearcher: options.RunbookSearcher,
		k8sReader:       options.K8sReader,
		k8sNodesEnabled: options.K8sNodesEnabled,
		db:              options.DB,
		timeout:         timeout,
		now:             now,
	}
	if err := registerReadOnlyTools(registry, executor); err != nil {
		return nil, fmt.Errorf("register copilot read-only tools: %w", err)
	}
	executor.registry = registry
	return executor, nil
}

func (e *Executor) Execute(ctx context.Context, result nlu.Result) ([]copilot.ToolCall, string, error) {
	if result.SelectedTool != "" {
		toolName := openAIToolNameToRegistryName(result.SelectedTool)
		args := e.typedEntitiesToToolArgs(toolName, result.Entities)
		return e.executeTool(ctx, toolName, args)
	}
	name, args, ok := e.planToolCall(result)
	if !ok {
		return []copilot.ToolCall{}, "", nil
	}
	return e.executeTool(ctx, name, args)
}

func (e *Executor) typedEntitiesToToolArgs(toolName string, entities map[string]string) json.RawMessage {
	if len(entities) == 0 {
		return json.RawMessage(`{}`)
	}
	schema := e.registrySchema(toolName)
	paramTypes := make(map[string]ParamType, len(schema.Parameters))
	for _, p := range schema.Parameters {
		paramTypes[p.Name] = p.Type
	}
	args := make(map[string]interface{}, len(entities))
	for k, v := range entities {
		if v == "" {
			continue
		}
		if pt, ok := paramTypes[k]; ok {
			args[k] = coerceParam(v, pt)
		} else {
			args[k] = v
		}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func (e *Executor) registrySchema(toolName string) ToolSchema {
	if e.registry == nil {
		return ToolSchema{}
	}
	for _, t := range e.registry.List() {
		if t.Name == toolName {
			return t
		}
	}
	return ToolSchema{}
}

func coerceParam(value string, paramType ParamType) interface{} {
	switch paramType {
	case ParamTypeInteger:
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	case ParamTypeNumber:
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	case ParamTypeBoolean:
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return value
}

func openAIToolNameToRegistryName(name string) string {
	return strings.ReplaceAll(name, "_", ".")
}

func (e *Executor) ExecuteTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	if e == nil || e.registry == nil {
		return ToolResult{Success: false, Error: errorResult(ErrToolUnavailable)}, ErrToolUnavailable
	}
	return e.registry.Execute(ctx, name, args)
}

func (e *Executor) ToolSchemas() []copilot.ToolSchema {
	if e.registry == nil {
		return []copilot.ToolSchema{}
	}
	schemas := e.registry.List()
	result := make([]copilot.ToolSchema, 0, len(schemas))
	for _, schema := range schemas {
		result = append(result, toServiceToolSchema(schema))
	}
	return result
}

func (e *Executor) Registry() Registry {
	return e.registry
}

func toServiceToolSchema(schema ToolSchema) copilot.ToolSchema {
	params := make([]copilot.ToolParamSchema, 0, len(schema.Parameters))
	for _, param := range schema.Parameters {
		params = append(params, copilot.ToolParamSchema{
			Name:        param.Name,
			Type:        string(param.Type),
			Required:    param.Required,
			Description: param.Description,
			Enum:        append([]string(nil), param.Enum...),
			Default:     param.Default,
			Min:         param.Min,
			Max:         param.Max,
			Pattern:     param.Pattern,
		})
	}
	return copilot.ToolSchema{
		Name:        schema.Name,
		Description: schema.Description,
		Parameters:  params,
		RiskLevel:   string(schema.RiskLevel),
		ReadOnly:    schema.ReadOnly,
		Timeout:     schema.Timeout,
	}
}

func (e *Executor) planToolCall(result nlu.Result) (string, json.RawMessage, bool) {
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return ToolAlertListActive, encodeToolArgs(stringArgs(result.Entities, "severity")), true
	case nlu.IntentAlertEventQuery:
		return ToolAlertEvents, encodeToolArgs(mixedArgs(result.Entities, map[string]ParamType{
			"count":    ParamTypeInteger,
			"status":   ParamTypeString,
			"severity": ParamTypeString,
		})), true
	case nlu.IntentAlertHistoryQuery:
		return ToolAlertHistory, encodeToolArgs(mixedArgs(result.Entities, map[string]ParamType{
			"status":     ParamTypeString,
			"severity":   ParamTypeString,
			"alert_name": ParamTypeString,
			"instance":   ParamTypeString,
			"window":     ParamTypeString,
			"page":       ParamTypeInteger,
			"page_size":  ParamTypeInteger,
		})), true
	case nlu.IntentAlertRuleListQuery:
		return ToolAlertRuleList, encodeToolArgs(mixedArgs(result.Entities, map[string]ParamType{
			"enabled":  ParamTypeBoolean,
			"severity": ParamTypeString,
			"search":   ParamTypeString,
		})), true
	case nlu.IntentMetricQuery:
		if result.Entities["alert_name"] != "" {
			return ToolRunbookSearch, encodeToolArgs(mixedArgs(result.Entities, map[string]ParamType{
				"alert_name": ParamTypeString,
				"limit":      ParamTypeInteger,
			})), true
		}
		if result.Entities["query"] != "" {
			return ToolPromQueryRange, e.promQueryRangeArgs(result.Entities), true
		}
		if result.Entities["instance"] != "" {
			return ToolHostMetrics, encodeToolArgs(stringArgs(result.Entities, "instance", "window")), true
		}
		if hasMetricKeywords(result.Entities) {
			return ToolRunbookSearch, encodeToolArgs(map[string]interface{}{
				"keywords": metricKeywordsFromEntities(result.Entities),
				"limit":    2,
			}), true
		}
		return ToolHostList, encodeToolArgs(mixedArgs(result.Entities, map[string]ParamType{
			"status":   ParamTypeString,
			"search":   ParamTypeString,
			"instance": ParamTypeString,
			"sort":     ParamTypeString,
			"risk":     ParamTypeString,
			"group_id": ParamTypeInteger,
		})), true
	case nlu.IntentHostQuery:
		return ToolHostList, encodeToolArgs(mixedArgs(result.Entities, map[string]ParamType{
			"status":   ParamTypeString,
			"search":   ParamTypeString,
			"instance": ParamTypeString,
			"sort":     ParamTypeString,
			"risk":     ParamTypeString,
			"group_id": ParamTypeInteger,
		})), true
	default:
		return "", nil, false
	}
}

func hasMetricKeywords(entities map[string]string) bool {
	_, ok := entities["metric_keywords"]
	return ok
}

func metricKeywordsFromEntities(entities map[string]string) []string {
	raw, ok := entities["metric_keywords"]
	if !ok || raw == "" {
		return []string{"cpu", "memory"}
	}
	keywords := strings.Split(raw, ",")
	var result []string
	for _, kw := range keywords {
		trimmed := strings.TrimSpace(kw)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{"cpu", "memory"}
	}
	return result
}

func (e *Executor) executeTool(ctx context.Context, name string, args json.RawMessage) ([]copilot.ToolCall, string, error) {
	if e.registry == nil {
		return nil, "", ErrToolUnavailable
	}
	result, err := e.registry.Execute(ctx, name, args)
	call := buildCallFromToolResult(name, result)
	if err != nil {
		if errors.Is(err, ErrToolUnavailable) {
			return nil, "", err
		}
		return []copilot.ToolCall{call}, "", nil
	}
	if call.Status == StatusError {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, replyFromResult(result), nil
}

func (e *Executor) promQueryRangeArgs(entities map[string]string) json.RawMessage {
	end := e.now()
	start := end.Add(-parseHistoryWindow(entities["window"]))
	step := time.Minute
	if end.Sub(start) > 24*time.Hour {
		step = 15 * time.Minute
	}
	return encodeToolArgs(map[string]interface{}{
		"query":      strings.TrimSpace(entities["query"]),
		"start":      start.Format(time.RFC3339),
		"end":        end.Format(time.RFC3339),
		"step":       durationArg(step),
		"max_points": 1000,
	})
}

func durationArg(duration time.Duration) string {
	switch {
	case duration%time.Hour == 0:
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	case duration%time.Minute == 0:
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
}

func stringArgs(entities map[string]string, keys ...string) map[string]interface{} {
	args := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(entities[key]); value != "" {
			args[key] = value
		}
	}
	return args
}

func mixedArgs(entities map[string]string, params map[string]ParamType) map[string]interface{} {
	args := make(map[string]interface{}, len(params))
	for key, paramType := range params {
		value := strings.TrimSpace(entities[key])
		if value == "" {
			continue
		}
		switch paramType {
		case ParamTypeInteger:
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err == nil {
				args[key] = parsed
			}
		case ParamTypeBoolean:
			parsed, err := strconv.ParseBool(value)
			if err == nil {
				args[key] = parsed
			}
		default:
			args[key] = value
		}
	}
	return args
}

func encodeToolArgs(args map[string]interface{}) json.RawMessage {
	data, err := json.Marshal(args)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func buildCallFromToolResult(name string, result ToolResult) copilot.ToolCall {
	if result.Error != nil || !result.Success {
		errorMessage := "tool execution failed"
		if result.Error != nil {
			errorMessage = publicToolError(result.Error).Error()
		}
		return copilot.ToolCall{Name: name, Status: StatusError, Error: errorMessage}
	}
	return buildCall(name, result.Data, nil)
}

func replyFromResult(result ToolResult) string {
	if result.Metadata == nil {
		return ""
	}
	reply, _ := result.Metadata[metadataReply].(string)
	return reply
}

func (e *Executor) runHostList(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.hostService == nil {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	options := parseHostListQueryOptions(entities)
	if options.GroupID != 0 {
		if e.db == nil {
			call := buildCall(ToolHostList, nil, fmt.Errorf("%w: db is required for group_id filter", ErrToolUnavailable))
			return []copilot.ToolCall{call}, "", nil
		}
		groupInstances, err := e.hostGroupInstances(toolCtx, options.GroupID)
		if err != nil {
			call := buildCall(ToolHostList, nil, err)
			return []copilot.ToolCall{call}, "", nil
		}
		options.ListOptions.GroupFiltered = true
		options.ListOptions.GroupInstances = groupInstances
	}

	hosts, err := e.hostService.Hosts(toolCtx, options.ListOptions)
	call := buildCall(ToolHostList, hosts, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d hosts.", len(hosts)), nil
}

func (e *Executor) hostGroupInstances(ctx context.Context, groupID uint64) (map[string]struct{}, error) {
	var instances []string
	if err := e.db.WithContext(ctx).
		Model(&model.HostGroupMember{}).
		Where("group_id = ?", groupID).
		Pluck("instance", &instances).Error; err != nil {
		return nil, fmt.Errorf("load host group members: %w", err)
	}

	groupInstances := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		instance = strings.TrimSpace(instance)
		if instance != "" {
			groupInstances[instance] = struct{}{}
		}
	}
	return groupInstances, nil
}

func parseHostListQueryOptions(entities map[string]string) hostListQueryOptions {
	search := strings.TrimSpace(entities["search"])
	if search == "" {
		search = entities["instance"]
	}
	return hostListQueryOptions{
		ListOptions: apphost.ListOptions{
			Status: apphost.ParseStatus(entities["status"]),
			Query:  apphost.NormalizeQuery(search),
			Sort:   apphost.ParseSort(entities["sort"]),
			Risk:   apphost.ParseRisk(entities["risk"]),
		},
		GroupID: parseOptionalUint(entities["group_id"]),
	}
}

func parseOptionalUint(value string) uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func (e *Executor) runHostMetrics(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.hostService == nil {
		return nil, "", ErrToolUnavailable
	}
	instance := strings.TrimSpace(entities["instance"])
	if instance == "" {
		return []copilot.ToolCall{buildCall(ToolHostMetrics, nil, fmt.Errorf("%w: instance is required", ErrInvalidArgs))}, "", nil
	}

	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	metrics, ok, err := e.hostService.Metrics(toolCtx, instance, parseWindow(entities["window"]), "", e.now())
	if !ok {
		err = fmt.Errorf("%w: invalid window", ErrInvalidArgs)
	}
	call := buildCall(ToolHostMetrics, metrics, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Loaded %s metrics for %s.", metrics.Range, metrics.Instance), nil
}

func (e *Executor) runAlertListActive(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.alertService == nil || !e.alertService.Enabled() {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	alerts, err := e.alertService.ActiveAlerts(toolCtx, appalert.ParseActiveSeverityFilter(entities["severity"]))
	call := buildCall(ToolAlertListActive, alerts, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d active alerts.", len(alerts)), nil
}

func (e *Executor) runAlertEvents(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.alertService == nil || !e.alertService.Enabled() {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	events, err := e.alertService.AlertEvents(
		toolCtx,
		parseAlertEventsCount(entities["count"]),
		appalert.ParseEventFilter(entities["status"]),
		appalert.ParseEventSeverityFilter(entities["severity"]),
	)
	call := buildCall(ToolAlertEvents, events, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d recent alert events.", len(events)), nil
}

func parseAlertEventsCount(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultAlertEventsCount
	}
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil || count <= 0 {
		return defaultAlertEventsCount
	}
	if count > maxAlertEventsCount {
		return maxAlertEventsCount
	}
	return count
}

func (e *Executor) runAlertHistory(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.db == nil {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	options := parseAlertHistoryQueryOptions(entities)
	stmt := e.db.WithContext(toolCtx).Model(&model.AlertHistory{})
	if options.Status != "" {
		stmt = stmt.Where("status = ?", options.Status)
	}
	if options.Severity != "" {
		stmt = stmt.Where("severity = ?", options.Severity)
	}
	if options.AlertName != "" {
		stmt = stmt.Where("alert_name LIKE ?", "%"+options.AlertName+"%")
	}
	if options.Instance != "" {
		stmt = stmt.Where("instance = ?", options.Instance)
	}
	if options.Window > 0 {
		stmt = stmt.Where("fired_at >= ?", e.now().Add(-options.Window))
	}

	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		call := buildCall(ToolAlertHistory, nil, err)
		return []copilot.ToolCall{call}, "", nil
	}

	var histories []model.AlertHistory
	err := stmt.
		Order("fired_at DESC").
		Order("id DESC").
		Limit(options.PageSize).
		Offset((options.Page - 1) * options.PageSize).
		Find(&histories).Error
	result := historyResult{Items: histories, Total: total, Page: options.Page, PageSize: options.PageSize}
	call := buildCall(ToolAlertHistory, result, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d alert history records.", total), nil
}

func (e *Executor) runAlertRuleList(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.db == nil {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	options := parseAlertRuleListQueryOptions(entities)
	stmt := e.db.WithContext(toolCtx).Model(&model.AlertRule{})
	if options.Enabled != nil {
		stmt = stmt.Where("enabled = ?", *options.Enabled)
	}
	if options.Severity != "" {
		stmt = stmt.Where("severity = ?", options.Severity)
	}
	if options.Search != "" {
		stmt = stmt.Where("name LIKE ? OR summary LIKE ?", "%"+options.Search+"%", "%"+options.Search+"%")
	}

	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		call := buildCall(ToolAlertRuleList, nil, err)
		return []copilot.ToolCall{call}, "", nil
	}

	var rules []model.AlertRule
	err := stmt.Order("id ASC").Find(&rules).Error
	result := alertRuleListResult{Items: rules, Total: total}
	call := buildCall(ToolAlertRuleList, result, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d alert rules.", total), nil
}

func parseAlertHistoryQueryOptions(entities map[string]string) alertHistoryQueryOptions {
	return alertHistoryQueryOptions{
		Status:    appalert.ParseEventFilter(entities["status"]),
		Severity:  appalert.ParseEventSeverityFilter(entities["severity"]),
		AlertName: strings.TrimSpace(entities["alert_name"]),
		Instance:  strings.TrimSpace(entities["instance"]),
		Window:    parseHistoryWindow(entities["window"]),
		Page:      parsePositiveInt(entities["page"], defaultAlertHistoryPage, 0),
		PageSize:  parsePositiveInt(entities["page_size"], defaultAlertHistoryPageSize, maxAlertHistoryPageSize),
	}
}

func parseAlertRuleListQueryOptions(entities map[string]string) alertRuleListQueryOptions {
	return alertRuleListQueryOptions{
		Enabled:  parseOptionalBool(entities["enabled"]),
		Severity: appalert.ParseEventSeverityFilter(entities["severity"]),
		Search:   strings.TrimSpace(entities["search"]),
	}
}

func parseOptionalBool(value string) *bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func parsePositiveInt(value string, defaultValue, maxValue int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	if maxValue > 0 && parsed > maxValue {
		return maxValue
	}
	return parsed
}

func (e *Executor) RunPromQueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration, maxPoints int) (copilot.ToolCall, error) {
	if e.promClient == nil {
		return copilot.ToolCall{}, ErrToolUnavailable
	}
	if err := validatePromQueryRange(query, start, end, step, maxPoints); err != nil {
		return buildCall(ToolPromQueryRange, nil, err), nil
	}

	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	series, err := e.promClient.QueryRangeRaw(toolCtx, query, start, end, step)
	return buildCall(ToolPromQueryRange, series, err), nil
}

func (e *Executor) runPromQueryRange(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	end := e.now()
	start := end.Add(-parseHistoryWindow(entities["window"]))
	step := time.Minute
	if end.Sub(start) > 24*time.Hour {
		step = 15 * time.Minute
	}

	call, err := e.RunPromQueryRange(ctx, entities["query"], start, end, step, 1000)
	if err != nil {
		return nil, "", err
	}
	if call.Status == StatusError {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, "Prometheus range query completed.", nil
}

func buildCall(name string, result interface{}, err error) copilot.ToolCall {
	if err != nil {
		return copilot.ToolCall{Name: name, Status: StatusError, Error: publicToolError(err).Error()}
	}
	return copilot.ToolCall{Name: name, Status: StatusSuccess, Result: sanitizeResult(result)}
}

func parseHostStatus(entities map[string]string) string {
	value := strings.ToLower(strings.TrimSpace(entities["status"]))
	switch value {
	case "up", "down":
		return value
	default:
		return ""
	}
}

func parseWindow(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "15m", "1h", "6h", "24h":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "1h"
	}
}

func parseHistoryWindow(value string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "7d":
		return 7 * 24 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "1h":
		return time.Hour
	case "15m":
		return 15 * time.Minute
	default:
		return 7 * 24 * time.Hour
	}
}

func validatePromQueryRange(query string, start, end time.Time, step time.Duration, maxPoints int) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("%w: query is required", ErrInvalidArgs)
	}
	if containsDangerousQuery(query) {
		return fmt.Errorf("%w: query is not allowed", ErrInvalidArgs)
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return fmt.Errorf("%w: invalid time range", ErrInvalidArgs)
	}
	if end.Sub(start) > 7*24*time.Hour {
		return fmt.Errorf("%w: range must not exceed 7d", ErrInvalidArgs)
	}
	if step < 15*time.Second {
		return fmt.Errorf("%w: step must be at least 15s", ErrInvalidArgs)
	}
	if maxPoints <= 0 || maxPoints > 1000 {
		return fmt.Errorf("%w: max_points must be between 1 and 1000", ErrInvalidArgs)
	}
	points := int(end.Sub(start)/step) + 1
	if points > maxPoints {
		return fmt.Errorf("%w: query would return too many points", ErrInvalidArgs)
	}
	return nil
}

func containsDangerousQuery(query string) bool {
	normalized := strings.ToLower(query)
	if strings.Contains(normalized, "__internal_") {
		return true
	}
	if promSubqueryPattern.MatchString(query) {
		return true
	}
	if hasOffsetOverLimit(normalized, 7*24*time.Hour) {
		return true
	}
	for _, pattern := range []string{"password", "token", "secret", "authorization", "go_memstats", "process_cmdline"} {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

func hasOffsetOverLimit(query string, limit time.Duration) bool {
	matches := promOffsetPattern.FindAllStringSubmatch(query, -1)
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		value, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return true
		}
		duration, ok := promOffsetDuration(value, match[2])
		if !ok || duration > limit {
			return true
		}
	}
	return false
}

func promOffsetDuration(value int64, unit string) (time.Duration, bool) {
	switch strings.ToLower(unit) {
	case "s":
		return time.Duration(value) * time.Second, true
	case "m":
		return time.Duration(value) * time.Minute, true
	case "h":
		return time.Duration(value) * time.Hour, true
	case "d":
		return time.Duration(value) * 24 * time.Hour, true
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, true
	case "y":
		return time.Duration(value) * 365 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func sanitizeResult(result interface{}) interface{} {
	data, err := json.Marshal(result)
	if err != nil {
		return result
	}
	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return result
	}
	return redactSensitive(decoded)
}

func redactSensitive(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		redacted := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				redacted[key] = "[REDACTED]"
				continue
			}
			redacted[key] = redactSensitive(item)
		}
		return redacted
	case []interface{}:
		redacted := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactSensitive(item))
		}
		return redacted
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	return strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "api_key")
}
