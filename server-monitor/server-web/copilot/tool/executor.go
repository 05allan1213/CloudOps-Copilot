package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	appalert "server-web/alert"
	"server-web/copilot/nlu"
	copilot "server-web/copilot/service"
	apphost "server-web/host"
	"server-web/model"
	promclient "server-web/prometheus"
	"server-web/webhook"
)

const (
	StatusSuccess = "success"
	StatusError   = "error"

	ToolHostList        = "host.list"
	ToolHostMetrics     = "host.metrics"
	ToolAlertListActive = "alert.list_active"
	ToolAlertEvents     = "alert.events"
	ToolAlertHistory    = "alert.history"
	ToolPromQueryRange  = "prom.query_range"

	defaultToolTimeout = 5 * time.Second
)

var (
	ErrInvalidArgs     = errors.New("invalid tool arguments")
	ErrToolUnavailable = errors.New("copilot tool unavailable")
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

type Executor struct {
	hostService  HostService
	alertService AlertService
	promClient   PrometheusClient
	db           *gorm.DB
	timeout      time.Duration
	now          func() time.Time
}

type Options struct {
	HostService  HostService
	AlertService AlertService
	PromClient   PrometheusClient
	DB           *gorm.DB
	Timeout      time.Duration
	Now          func() time.Time
}

type historyResult struct {
	Items    []model.AlertHistory `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func NewExecutor(options Options) *Executor {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Executor{
		hostService:  options.HostService,
		alertService: options.AlertService,
		promClient:   options.PromClient,
		db:           options.DB,
		timeout:      timeout,
		now:          now,
	}
}

func (e *Executor) Execute(ctx context.Context, result nlu.Result) ([]copilot.ToolCall, string, error) {
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return e.runAlertListActive(ctx, result.Entities)
	case nlu.IntentAlertEventQuery:
		return e.runAlertEvents(ctx, result.Entities)
	case nlu.IntentAlertHistoryQuery:
		return e.runAlertHistory(ctx, result.Entities)
	case nlu.IntentMetricQuery:
		if result.Entities["query"] != "" {
			return e.runPromQueryRange(ctx, result.Entities)
		}
		if result.Entities["instance"] != "" {
			return e.runHostMetrics(ctx, result.Entities)
		}
		return e.runHostList(ctx, result.Entities)
	case nlu.IntentHostQuery:
		return e.runHostList(ctx, result.Entities)
	default:
		return []copilot.ToolCall{}, "", nil
	}
}

func (e *Executor) runHostList(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.hostService == nil {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	hosts, err := e.hostService.Hosts(toolCtx, apphost.ListOptions{
		Status: parseHostStatus(entities),
		Query:  apphost.NormalizeQuery(entities["instance"]),
		Sort:   "instance",
	})
	call := buildCall(ToolHostList, hosts, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d hosts.", len(hosts)), nil
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

	events, err := e.alertService.AlertEvents(toolCtx, 20, "", appalert.ParseEventSeverityFilter(entities["severity"]))
	call := buildCall(ToolAlertEvents, events, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d recent alert events.", len(events)), nil
}

func (e *Executor) runAlertHistory(ctx context.Context, entities map[string]string) ([]copilot.ToolCall, string, error) {
	if e.db == nil {
		return nil, "", ErrToolUnavailable
	}
	toolCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	pageSize := 20
	stmt := e.db.WithContext(toolCtx).Model(&model.AlertHistory{})
	if severity := appalert.ParseEventSeverityFilter(entities["severity"]); severity != "" {
		stmt = stmt.Where("severity = ?", severity)
	}
	if instance := strings.TrimSpace(entities["instance"]); instance != "" {
		stmt = stmt.Where("instance = ?", instance)
	}
	if window := parseHistoryWindow(entities["window"]); window > 0 {
		stmt = stmt.Where("fired_at >= ?", e.now().Add(-window))
	}

	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		call := buildCall(ToolAlertHistory, nil, err)
		return []copilot.ToolCall{call}, "", nil
	}

	var histories []model.AlertHistory
	err := stmt.Order("fired_at DESC").Order("id DESC").Limit(pageSize).Find(&histories).Error
	result := historyResult{Items: histories, Total: total, Page: 1, PageSize: pageSize}
	call := buildCall(ToolAlertHistory, result, err)
	if err != nil {
		return []copilot.ToolCall{call}, "", nil
	}
	return []copilot.ToolCall{call}, fmt.Sprintf("Found %d alert history records.", total), nil
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
		return copilot.ToolCall{Name: name, Status: StatusError, Error: err.Error()}
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
	for _, pattern := range []string{"password", "token", "secret", "authorization", "go_memstats", "process_"} {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
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
		strings.Contains(normalized, "authorization")
}
