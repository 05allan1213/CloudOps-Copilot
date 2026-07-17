package tool

import (
	"context"
	"encoding/json"

	k8sreader "server-web/internal/infra/k8sread"
)

type k8sTool struct {
	executor *Executor
	schema   ToolSchema
	run      func(context.Context, json.RawMessage) (ToolResult, error)
}

func newK8sReadOnlyTools(executor *Executor) []Tool {
	limitMin, limitMax := 1.0, float64(k8sreader.MaxLimit)
	tailMin, tailMax := 1.0, 1000.0
	maxNameLength := 253.0
	namespaceParam := ParamSchema{Name: "namespace", Type: ParamTypeString, Pattern: `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`}
	labelSelectorParam := ParamSchema{Name: "label_selector", Type: ParamTypeString, Pattern: `^[A-Za-z0-9_.\-/=!,() ]*$`}
	limitParam := ParamSchema{Name: "limit", Type: ParamTypeInteger, Default: k8sreader.DefaultLimit, Min: &limitMin, Max: &limitMax}
	return []Tool{
		newK8sTool(executor, ToolK8sGetPods, "List Kubernetes pods.", []ParamSchema{
			namespaceParam,
			labelSelectorParam,
			{Name: "field_selector", Type: ParamTypeString, Pattern: `^(metadata\.name|status\.phase)=.+$`},
			limitParam,
		}, executor.runK8sGetPods),
		newK8sTool(executor, ToolK8sGetDeployments, "List Kubernetes deployments.", []ParamSchema{
			namespaceParam,
			{Name: "name", Type: ParamTypeString, Max: &maxNameLength, Pattern: `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`},
			labelSelectorParam,
			limitParam,
		}, executor.runK8sGetDeployments),
		newK8sTool(executor, ToolK8sGetServices, "List Kubernetes services.", []ParamSchema{
			namespaceParam,
			labelSelectorParam,
			limitParam,
		}, executor.runK8sGetServices),
		newK8sTool(executor, ToolK8sGetEvents, "List Kubernetes events.", []ParamSchema{
			namespaceParam,
			{Name: "involved_kind", Type: ParamTypeString, Max: &maxNameLength},
			{Name: "involved_name", Type: ParamTypeString, Max: &maxNameLength, Pattern: `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`},
			limitParam,
		}, executor.runK8sGetEvents),
		newK8sTool(executor, ToolK8sGetLogs, "Read a bounded Kubernetes pod log snippet.", []ParamSchema{
			namespaceParam,
			{Name: "pod_name", Type: ParamTypeString, Required: true, Max: &maxNameLength, Pattern: `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`},
			{Name: "container", Type: ParamTypeString, Max: &maxNameLength},
			{Name: "tail_lines", Type: ParamTypeInteger, Default: 100, Min: &tailMin, Max: &tailMax},
		}, executor.runK8sGetLogs),
	}
}

func newK8sTool(executor *Executor, name, description string, parameters []ParamSchema, run func(context.Context, json.RawMessage) (ToolResult, error)) Tool {
	return k8sTool{
		executor: executor,
		schema: ToolSchema{
			Name:        name,
			Description: description,
			Parameters:  parameters,
			RiskLevel:   RiskLevelLow,
			ReadOnly:    true,
			Timeout:     executor.timeout,
		},
		run: run,
	}
}

func (t k8sTool) Name() string { return t.schema.Name }

func (t k8sTool) Description() string { return t.schema.Description }

func (t k8sTool) Schema() ToolSchema { return t.schema }

func (t k8sTool) Run(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.executor == nil || t.executor.k8sReader == nil {
		return ToolResult{}, ErrToolUnavailable
	}
	return t.run(ctx, args)
}

func (t k8sTool) HealthCheck(ctx context.Context) bool {
	return ctx.Err() == nil && t.executor != nil && t.executor.k8sReader != nil
}

func (e *Executor) runK8sGetPods(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var query k8sreader.QueryOptions
	if err := json.Unmarshal(args, &query); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	pods, err := e.k8sReader.ListPods(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: pods}, nil
}

func (e *Executor) runK8sGetDeployments(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var query k8sreader.QueryOptions
	if err := json.Unmarshal(args, &query); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	deployments, err := e.k8sReader.ListDeployments(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: deployments}, nil
}

func (e *Executor) runK8sGetServices(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var query k8sreader.QueryOptions
	if err := json.Unmarshal(args, &query); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	services, err := e.k8sReader.ListServices(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: services}, nil
}

func (e *Executor) runK8sGetEvents(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var query k8sreader.EventQuery
	if err := json.Unmarshal(args, &query); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	events, err := e.k8sReader.ListEvents(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: events}, nil
}

func (e *Executor) runK8sGetLogs(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var query k8sreader.LogQuery
	if err := json.Unmarshal(args, &query); err != nil {
		return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
	}
	logs, err := e.k8sReader.GetPodLogs(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Success: true, Data: logs}, nil
}
