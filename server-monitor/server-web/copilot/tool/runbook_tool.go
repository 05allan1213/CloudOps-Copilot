package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"server-web/copilot/runbook"
)

type runbookSearchTool struct {
	executor *Executor
	schema   ToolSchema
}

func newRunbookSearchTool(executor *Executor) runbookSearchTool {
	maxAlertName := 128.0
	maxKeywords := 20.0
	maxMetrics := 20.0
	limitMin, limitMax := 1.0, 5.0
	return runbookSearchTool{
		executor: executor,
		schema: ToolSchema{
			Name:        ToolRunbookSearch,
			Description: "Search local Markdown runbooks by alert name, keywords, and metrics.",
			RiskLevel:   RiskLevelLow,
			ReadOnly:    true,
			Timeout:     executor.timeout,
			Parameters: []ParamSchema{
				{Name: "alert_name", Type: ParamTypeString, Max: &maxAlertName},
				{Name: "keywords", Type: ParamTypeArray, Max: &maxKeywords},
				{Name: "metrics", Type: ParamTypeArray, Max: &maxMetrics},
				{Name: "limit", Type: ParamTypeInteger, Default: 2, Min: &limitMin, Max: &limitMax},
			},
		},
	}
}

func (t runbookSearchTool) Name() string {
	return ToolRunbookSearch
}

func (t runbookSearchTool) Description() string {
	return t.schema.Description
}

func (t runbookSearchTool) Schema() ToolSchema {
	return t.schema
}

func (t runbookSearchTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.executor == nil || t.executor.runbookSearcher == nil {
		return ToolResult{}, ErrToolUnavailable
	}
	var req runbook.SearchRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	req.AlertName = strings.TrimSpace(req.AlertName)
	req.Keywords = cleanStringList(req.Keywords, 64)
	req.Metrics = cleanStringList(req.Metrics, 128)
	results, err := t.executor.runbookSearcher.Search(ctx, req)
	if err != nil {
		if errors.Is(err, runbook.ErrUnavailable) {
			return ToolResult{}, ErrToolUnavailable
		}
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: results}, nil
}

func (t runbookSearchTool) HealthCheck(ctx context.Context) bool {
	return ctx.Err() == nil &&
		t.executor != nil &&
		t.executor.runbookSearcher != nil &&
		t.executor.runbookSearcher.HealthCheck(ctx) &&
		t.executor.runbookSearcher.Count() > 0
}

func cleanStringList(values []string, maxRunes int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len([]rune(value)) > maxRunes {
			value = string([]rune(value)[:maxRunes])
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
