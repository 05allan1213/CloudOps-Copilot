package tool

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type alertActiveTool struct {
	executor *Executor
	schema   ToolSchema
}

type alertHistoryTool struct {
	executor *Executor
	schema   ToolSchema
}

type alertHistoryArgs struct {
	Status    string `json:"status"`
	Severity  string `json:"severity"`
	AlertName string `json:"alert_name"`
	Instance  string `json:"instance"`
	Window    string `json:"window"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
}

type promQueryRangeTool struct {
	executor *Executor
	schema   ToolSchema
}

func registerReadOnlyTools(registry Registry, executor *Executor) error {
	tools := []Tool{
		newAlertActiveTool(executor),
		newAlertHistoryTool(executor),
		newPromQueryRangeTool(executor),
		newRunbookSearchTool(executor),
	}
	if executor.k8sReader != nil {
		tools = append(tools, newK8sReadOnlyTools(executor)...)
	}
	for _, candidate := range tools {
		if err := registry.Register(candidate); err != nil {
			return err
		}
	}
	return nil
}

func newAlertActiveTool(executor *Executor) Tool {
	return alertActiveTool{executor: executor, schema: ToolSchema{
		Name: ToolAlertListActive, Description: "List active alerts.", RiskLevel: RiskLevelLow, ReadOnly: true, Timeout: executor.timeout,
		Parameters: []ParamSchema{{Name: "severity", Type: ParamTypeString, Enum: []string{"critical", "warning", "info"}}},
	}}
}

func (t alertActiveTool) Name() string        { return t.schema.Name }
func (t alertActiveTool) Description() string { return t.schema.Description }
func (t alertActiveTool) Schema() ToolSchema  { return t.schema }
func (t alertActiveTool) HealthCheck(ctx context.Context) bool {
	return ctx.Err() == nil && t.executor != nil && t.executor.alertService != nil && t.executor.alertService.Enabled()
}
func (t alertActiveTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.executor == nil || t.executor.alertService == nil || !t.executor.alertService.Enabled() {
		return ToolResult{}, ErrToolUnavailable
	}
	var input struct {
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	alerts, err := t.executor.alertService.ActiveAlerts(ctx, strings.ToLower(strings.TrimSpace(input.Severity)))
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: alerts}, nil
}

func newAlertHistoryTool(executor *Executor) Tool {
	pageMin, pageSizeMin, pageSizeMax, maxText := 1.0, 1.0, 100.0, 128.0
	return alertHistoryTool{executor: executor, schema: ToolSchema{
		Name: ToolAlertHistory, Description: "List alert history records.", RiskLevel: RiskLevelLow, ReadOnly: true, Timeout: executor.timeout,
		Parameters: []ParamSchema{
			{Name: "status", Type: ParamTypeString, Enum: []string{"firing", "resolved"}},
			{Name: "severity", Type: ParamTypeString, Enum: []string{"critical", "warning", "info"}},
			{Name: "alert_name", Type: ParamTypeString, Max: &maxText},
			{Name: "instance", Type: ParamTypeString, Max: &maxText, Pattern: `^[A-Za-z0-9_.:-]+$`},
			{Name: "window", Type: ParamTypeString, Default: "7d", Enum: []string{"15m", "1h", "6h", "24h", "7d"}},
			{Name: "page", Type: ParamTypeInteger, Default: defaultAlertHistoryPage, Min: &pageMin},
			{Name: "page_size", Type: ParamTypeInteger, Default: defaultAlertHistorySize, Min: &pageSizeMin, Max: &pageSizeMax},
		},
	}}
}

func (t alertHistoryTool) Name() string        { return t.schema.Name }
func (t alertHistoryTool) Description() string { return t.schema.Description }
func (t alertHistoryTool) Schema() ToolSchema  { return t.schema }
func (t alertHistoryTool) HealthCheck(ctx context.Context) bool {
	return ctx.Err() == nil && t.executor != nil && t.executor.db != nil
}
func (t alertHistoryTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var input alertHistoryArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	result, err := t.executor.queryAlertHistory(ctx, input)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: result}, nil
}

func newPromQueryRangeTool(executor *Executor) Tool {
	maxQueryLength, minPoints, maxPoints := 2048.0, 1.0, float64(defaultPrometheusMaxPoint)
	return promQueryRangeTool{executor: executor, schema: ToolSchema{
		Name: ToolPromQueryRange, Description: "Run a guarded Prometheus range query.", RiskLevel: RiskLevelMedium, ReadOnly: true, Timeout: executor.timeout,
		Parameters: []ParamSchema{
			{Name: "query", Type: ParamTypeString, Required: true, Max: &maxQueryLength},
			{Name: "start", Type: ParamTypeString, Required: true},
			{Name: "end", Type: ParamTypeString, Required: true},
			{Name: "step", Type: ParamTypeString, Required: true, Pattern: `^[0-9]+(s|m|h)$`},
			{Name: "max_points", Type: ParamTypeInteger, Default: defaultPrometheusMaxPoint, Min: &minPoints, Max: &maxPoints},
		},
	}}
}

func (t promQueryRangeTool) Name() string        { return t.schema.Name }
func (t promQueryRangeTool) Description() string { return t.schema.Description }
func (t promQueryRangeTool) Schema() ToolSchema  { return t.schema }
func (t promQueryRangeTool) HealthCheck(ctx context.Context) bool {
	return ctx.Err() == nil && t.executor != nil && t.executor.promClient != nil
}
func (t promQueryRangeTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.executor == nil || t.executor.promClient == nil {
		return ToolResult{}, ErrToolUnavailable
	}
	var input struct {
		Query     string `json:"query"`
		Start     string `json:"start"`
		End       string `json:"end"`
		Step      string `json:"step"`
		MaxPoints int    `json:"max_points"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(input.Start))
	if err != nil {
		return ToolResult{Error: NewInvalidArgsError("start", "must be RFC3339 time")}, nil
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(input.End))
	if err != nil {
		return ToolResult{Error: NewInvalidArgsError("end", "must be RFC3339 time")}, nil
	}
	step, err := time.ParseDuration(strings.TrimSpace(input.Step))
	if err != nil {
		return ToolResult{Error: NewInvalidArgsError("step", "must be a duration")}, nil
	}
	if err := validatePromQueryRange(input.Query, start, end, step, input.MaxPoints); err != nil {
		return ToolResult{}, err
	}
	series, err := t.executor.promClient.QueryRangeRaw(ctx, strings.TrimSpace(input.Query), start, end, step)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: series}, nil
}
