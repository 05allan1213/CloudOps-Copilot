package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"server-web/internal/agent/runbook"
	k8sreader "server-web/internal/infra/k8sread"
	promclient "server-web/internal/infra/prometheus"
	"server-web/internal/infra/webhook"
	"server-web/internal/model"
	appalert "server-web/internal/service/alert"
)

const (
	StatusSuccess = "success"
	StatusError   = "error"

	// These names stay exported so allowlist tests can explicitly prove that
	// removed generic host and node tools are not registered for the Agent.
	ToolHostList          = "host.list"
	ToolHostMetrics       = "host.metrics"
	ToolAlertListActive   = "alert.list_active"
	ToolAlertHistory      = "alert.history"
	ToolPromQueryRange    = "prom.query_range"
	ToolRunbookSearch     = "runbook.search"
	ToolK8sGetPods        = "k8s.get_pods"
	ToolK8sGetDeployments = "k8s.get_deployments"
	ToolK8sGetServices    = "k8s.get_services"
	ToolK8sGetNodes       = "k8s.get_nodes"
	ToolK8sGetEvents      = "k8s.get_events"
	ToolK8sGetLogs        = "k8s.get_logs"

	defaultToolTimeout        = 30 * time.Second
	defaultAlertHistoryPage   = 1
	defaultAlertHistorySize   = 20
	maximumAlertHistorySize   = 100
	defaultPrometheusMaxPoint = 1000
)

var (
	promOffsetPattern   = regexp.MustCompile(`(?i)\boffset\s+([0-9]+)\s*([smhdwy])\b`)
	promSubqueryPattern = regexp.MustCompile(`\[[^\]]+:[^\]]*\]`)
)

type AlertService interface {
	Enabled() bool
	ActiveAlerts(ctx context.Context, severityFilter string) ([]webhook.AlertRecord, error)
}

type PrometheusClient interface {
	QueryRangeRaw(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]promclient.RangeSeries, error)
}

type RunbookSearcher interface {
	Search(ctx context.Context, req runbook.SearchRequest) ([]runbook.SearchResult, error)
	HealthCheck(ctx context.Context) bool
	Count() int
}

// Executor is the neutral V2 Agent tool facade. It exposes only registry-based,
// read-only execution; generic chat intent planning and host-product helpers are
// deliberately outside this package.
type Executor struct {
	alertService    AlertService
	promClient      PrometheusClient
	runbookSearcher RunbookSearcher
	k8sReader       k8sreader.Reader
	db              *gorm.DB
	registry        Registry
	timeout         time.Duration
	now             func() time.Time
}

type Options struct {
	AlertService    AlertService
	PromClient      PrometheusClient
	RunbookSearcher RunbookSearcher
	K8sReader       k8sreader.Reader
	DB              *gorm.DB
	Observer        Observer
	Timeout         time.Duration
	LogArgs         bool
	Now             func() time.Time
	AdditionalTools []Tool
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
		alertService:    options.AlertService,
		promClient:      options.PromClient,
		runbookSearcher: options.RunbookSearcher,
		k8sReader:       options.K8sReader,
		db:              options.DB,
		timeout:         timeout,
		now:             now,
	}
	if err := registerReadOnlyTools(registry, executor); err != nil {
		return nil, fmt.Errorf("register agent read-only tools: %w", err)
	}
	for _, additional := range options.AdditionalTools {
		if additional == nil || !additional.Schema().ReadOnly || additional.Schema().RiskLevel == RiskLevelHigh {
			return nil, fmt.Errorf("register additional read-only tool: %w", ErrPermissionDenied)
		}
		if err := registry.Register(additional); err != nil {
			return nil, fmt.Errorf("register additional read-only tool: %w", err)
		}
	}
	executor.registry = registry
	return executor, nil
}

func (e *Executor) ExecuteTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	if e == nil || e.registry == nil {
		return ToolResult{Success: false, Error: errorResult(ErrToolUnavailable)}, ErrToolUnavailable
	}
	return e.registry.Execute(ctx, name, args)
}

func (e *Executor) ToolSchemas() []ToolSchema {
	if e == nil || e.registry == nil {
		return nil
	}
	return e.registry.List()
}

func (e *Executor) Registry() Registry {
	if e == nil {
		return nil
	}
	return e.registry
}

func (e *Executor) queryAlertHistory(ctx context.Context, args alertHistoryArgs) (historyResult, error) {
	if e.db == nil {
		return historyResult{}, ErrToolUnavailable
	}
	options := parseAlertHistoryQueryOptions(args)
	stmt := e.db.WithContext(ctx).Model(&model.AlertHistory{})
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
		return historyResult{}, err
	}
	var histories []model.AlertHistory
	err := stmt.Order("fired_at DESC").Order("id DESC").Limit(options.PageSize).Offset((options.Page - 1) * options.PageSize).Find(&histories).Error
	return historyResult{Items: histories, Total: total, Page: options.Page, PageSize: options.PageSize}, err
}

func parseAlertHistoryQueryOptions(args alertHistoryArgs) alertHistoryQueryOptions {
	return alertHistoryQueryOptions{
		Status:    appalert.ParseEventFilter(args.Status),
		Severity:  appalert.ParseEventSeverityFilter(args.Severity),
		AlertName: strings.TrimSpace(args.AlertName),
		Instance:  strings.TrimSpace(args.Instance),
		Window:    parseHistoryWindow(args.Window),
		Page:      clampPositive(args.Page, defaultAlertHistoryPage, 0),
		PageSize:  clampPositive(args.PageSize, defaultAlertHistorySize, maximumAlertHistorySize),
	}
}

func clampPositive(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if maximum > 0 && value > maximum {
		return maximum
	}
	return value
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
	if maxPoints <= 0 || maxPoints > defaultPrometheusMaxPoint {
		return fmt.Errorf("%w: max_points must be between 1 and %d", ErrInvalidArgs, defaultPrometheusMaxPoint)
	}
	if int(end.Sub(start)/step)+1 > maxPoints {
		return fmt.Errorf("%w: query would return too many points", ErrInvalidArgs)
	}
	return nil
}

func containsDangerousQuery(query string) bool {
	normalized := strings.ToLower(query)
	if strings.Contains(normalized, "__internal_") || promSubqueryPattern.MatchString(query) || hasOffsetOverLimit(normalized, 7*24*time.Hour) {
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
	for _, match := range promOffsetPattern.FindAllStringSubmatch(query, -1) {
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
	return strings.Contains(normalized, "password") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "authorization") || strings.Contains(normalized, "api_key")
}
