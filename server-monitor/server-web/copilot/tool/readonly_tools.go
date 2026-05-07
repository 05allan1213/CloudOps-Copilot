package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	copilot "server-web/copilot/service"
)

const metadataReply = "reply"

type executorTool struct {
	executor    *Executor
	name        string
	description string
	schema      ToolSchema
	run         func(context.Context, map[string]string) ([]copilot.ToolCall, string, error)
}

func newExecutorTool(executor *Executor, name, description string, parameters []ParamSchema, risk RiskLevel, run func(context.Context, map[string]string) ([]copilot.ToolCall, string, error)) executorTool {
	return executorTool{
		executor:    executor,
		name:        name,
		description: description,
		schema: ToolSchema{
			Name:        name,
			Description: description,
			Parameters:  parameters,
			RiskLevel:   risk,
			ReadOnly:    true,
			Timeout:     executor.timeout,
		},
		run: run,
	}
}

func (t executorTool) Name() string {
	return t.name
}

func (t executorTool) Description() string {
	return t.description
}

func (t executorTool) Schema() ToolSchema {
	return t.schema
}

func (t executorTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	entities, err := rawArgsToStringMap(args)
	if err != nil {
		return ToolResult{}, err
	}

	calls, reply, err := t.run(ctx, entities)
	if err != nil {
		return ToolResult{}, err
	}
	if len(calls) == 0 {
		return ToolResult{Success: true, Metadata: replyMetadata(reply)}, nil
	}
	return toolResultFromCall(calls[0], reply), nil
}

type promQueryRangeTool struct {
	executor *Executor
	schema   ToolSchema
}

func newPromQueryRangeTool(executor *Executor) promQueryRangeTool {
	maxQueryLength := 2048.0
	maxPointsMin, maxPointsMax := 1.0, 1000.0
	return promQueryRangeTool{
		executor: executor,
		schema: ToolSchema{
			Name:        ToolPromQueryRange,
			Description: "Run a guarded Prometheus range query.",
			RiskLevel:   RiskLevelMedium,
			ReadOnly:    true,
			Timeout:     executor.timeout,
			Parameters: []ParamSchema{
				{Name: "query", Type: ParamTypeString, Required: true, Max: &maxQueryLength},
				{Name: "start", Type: ParamTypeString, Required: true},
				{Name: "end", Type: ParamTypeString, Required: true},
				{Name: "step", Type: ParamTypeString, Required: true, Pattern: `^[0-9]+(s|m|h)$`},
				{Name: "max_points", Type: ParamTypeInteger, Default: 1000, Min: &maxPointsMin, Max: &maxPointsMax},
			},
		},
	}
}

func (t promQueryRangeTool) Name() string {
	return ToolPromQueryRange
}

func (t promQueryRangeTool) Description() string {
	return t.schema.Description
}

func (t promQueryRangeTool) Schema() ToolSchema {
	return t.schema
}

func (t promQueryRangeTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var queryArgs struct {
		Query     string `json:"query"`
		Start     string `json:"start"`
		End       string `json:"end"`
		Step      string `json:"step"`
		MaxPoints int    `json:"max_points"`
	}
	if err := json.Unmarshal(args, &queryArgs); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}

	start, err := time.Parse(time.RFC3339, strings.TrimSpace(queryArgs.Start))
	if err != nil {
		return ToolResult{Success: false, Error: NewInvalidArgsError("start", "must be RFC3339 time")}, nil
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(queryArgs.End))
	if err != nil {
		return ToolResult{Success: false, Error: NewInvalidArgsError("end", "must be RFC3339 time")}, nil
	}
	step, err := time.ParseDuration(strings.TrimSpace(queryArgs.Step))
	if err != nil {
		return ToolResult{Success: false, Error: NewInvalidArgsError("step", "must be a duration")}, nil
	}

	call, err := t.executor.RunPromQueryRange(ctx, queryArgs.Query, start, end, step, queryArgs.MaxPoints)
	if err != nil {
		return ToolResult{}, err
	}
	return toolResultFromCall(call, "Prometheus range query completed."), nil
}

func registerReadOnlyTools(registry Registry, executor *Executor) error {
	for _, tool := range []Tool{
		newHostListTool(executor),
		newHostMetricsTool(executor),
		newAlertListActiveTool(executor),
		newAlertEventsTool(executor),
		newAlertHistoryTool(executor),
		newPromQueryRangeTool(executor),
	} {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func newHostListTool(executor *Executor) Tool {
	maxTextLength := 128.0
	groupMin := 1.0
	return newExecutorTool(
		executor,
		ToolHostList,
		"List monitored hosts.",
		[]ParamSchema{
			{Name: "status", Type: ParamTypeString, Enum: []string{"up", "down"}},
			{Name: "search", Type: ParamTypeString, Max: &maxTextLength},
			{Name: "instance", Type: ParamTypeString, Max: &maxTextLength},
			{Name: "sort", Type: ParamTypeString, Enum: []string{"instance", "cpu_desc", "memory_desc"}},
			{Name: "risk", Type: ParamTypeString, Enum: []string{"high_cpu", "high_memory"}},
			{Name: "group_id", Type: ParamTypeInteger, Min: &groupMin},
		},
		RiskLevelLow,
		executor.runHostList,
	)
}

func newHostMetricsTool(executor *Executor) Tool {
	maxInstanceLength := 128.0
	return newExecutorTool(
		executor,
		ToolHostMetrics,
		"Load host trend metrics.",
		[]ParamSchema{
			{Name: "instance", Type: ParamTypeString, Required: true, Max: &maxInstanceLength, Pattern: `^[A-Za-z0-9_.:-]+$`},
			{Name: "window", Type: ParamTypeString, Default: "1h", Enum: []string{"15m", "1h", "6h", "24h"}},
		},
		RiskLevelLow,
		executor.runHostMetrics,
	)
}

func newAlertListActiveTool(executor *Executor) Tool {
	return newExecutorTool(
		executor,
		ToolAlertListActive,
		"List active alerts.",
		[]ParamSchema{
			{Name: "severity", Type: ParamTypeString, Enum: []string{"critical", "warning", "info"}},
		},
		RiskLevelLow,
		executor.runAlertListActive,
	)
}

func newAlertEventsTool(executor *Executor) Tool {
	countMin, countMax := 1.0, 100.0
	return newExecutorTool(
		executor,
		ToolAlertEvents,
		"List recent alert events.",
		[]ParamSchema{
			{Name: "count", Type: ParamTypeInteger, Default: int(defaultAlertEventsCount), Min: &countMin, Max: &countMax},
			{Name: "status", Type: ParamTypeString, Enum: []string{"firing", "resolved"}},
			{Name: "severity", Type: ParamTypeString, Enum: []string{"critical", "warning", "info"}},
		},
		RiskLevelLow,
		executor.runAlertEvents,
	)
}

func newAlertHistoryTool(executor *Executor) Tool {
	pageMin, pageSizeMin, pageSizeMax := 1.0, 1.0, 100.0
	maxTextLength := 128.0
	return newExecutorTool(
		executor,
		ToolAlertHistory,
		"List alert history records.",
		[]ParamSchema{
			{Name: "status", Type: ParamTypeString, Enum: []string{"firing", "resolved"}},
			{Name: "severity", Type: ParamTypeString, Enum: []string{"critical", "warning", "info"}},
			{Name: "alert_name", Type: ParamTypeString, Max: &maxTextLength},
			{Name: "instance", Type: ParamTypeString, Max: &maxTextLength, Pattern: `^[A-Za-z0-9_.:-]+$`},
			{Name: "window", Type: ParamTypeString, Default: "7d", Enum: []string{"15m", "1h", "6h", "24h", "7d"}},
			{Name: "page", Type: ParamTypeInteger, Default: defaultAlertHistoryPage, Min: &pageMin},
			{Name: "page_size", Type: ParamTypeInteger, Default: defaultAlertHistoryPageSize, Min: &pageSizeMin, Max: &pageSizeMax},
		},
		RiskLevelLow,
		executor.runAlertHistory,
	)
}

func rawArgsToStringMap(args json.RawMessage) (map[string]string, error) {
	var values map[string]interface{}
	if len(args) == 0 {
		return map[string]string{}, nil
	}
	if err := json.Unmarshal(args, &values); err != nil {
		return nil, NewInvalidArgsError("", "must be valid JSON")
	}
	entities := make(map[string]string, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			entities[key] = typed
		case float64:
			entities[key] = formatToolNumber(typed)
		case bool:
			entities[key] = strconv.FormatBool(typed)
		case nil:
			continue
		default:
			entities[key] = fmt.Sprint(typed)
		}
	}
	return entities, nil
}

func toolResultFromCall(call copilot.ToolCall, reply string) ToolResult {
	if call.Status == StatusError {
		return ToolResult{
			Success:  false,
			Error:    NewToolError(ErrorCodeToolExecution, "", call.Error, ErrToolExecution),
			Metadata: replyMetadata(reply),
		}
	}
	return ToolResult{
		Success:  true,
		Data:     call.Result,
		Metadata: replyMetadata(reply),
	}
}

func replyMetadata(reply string) map[string]interface{} {
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	return map[string]interface{}{metadataReply: reply}
}

func formatToolNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
