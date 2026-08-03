package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type workspaceToolCall struct {
	name      string
	arguments json.RawMessage
	execute   func(context.Context) (WorkspaceToolObservation, error)
}

func (r *WorkspaceRunner) runBoundedTools(ctx context.Context, lease WorkspaceLease, execution WorkspaceExecutionContext, revision settings.Revision) error {
	maxCalls := execution.Limits.MaxToolCalls
	if maxCalls <= 0 || maxCalls > 8 {
		maxCalls = 8
	}
	toolTimeout := execution.Limits.ToolTimeout
	if toolTimeout <= 0 || toolTimeout > 30*time.Second {
		toolTimeout = 15 * time.Second
	}
	calls := make([]workspaceToolCall, 0, 4)

	kubernetesArgs, _ := json.Marshal(map[string]any{
		"resource":               workspacePrimaryResource(execution.Snapshot),
		"cluster_id":             execution.Snapshot.Scope.ClusterID,
		"namespaces":             execution.Snapshot.Scope.Namespaces,
		"configuration_revision": revision.ID,
		"limit":                  min(infrastructure.DefaultLimit, max(1, execution.Limits.MaxEvidenceItems*16)),
	})
	calls = append(calls, workspaceToolCall{
		name: "kubernetes.resources", arguments: kubernetesArgs,
		execute: func(toolCtx context.Context) (WorkspaceToolObservation, error) {
			return r.observeKubernetes(toolCtx, execution, revision)
		},
	})

	resource, hasWorkload := workspaceWorkloadResource(execution.Snapshot.Resources)
	if hasWorkload {
		stepSeconds := workspaceMetricStep(execution.Snapshot.TimeRange, revision.General.QueryMaxResults)
		metricRequest, prepareErr := observability.PrepareBoundedMetricToolRequest(
			"workload_availability", execution.Snapshot.Scope.ClusterID, resource.Namespace,
			resource, execution.Snapshot.TimeRange.From, execution.Snapshot.TimeRange.To, stepSeconds, revision,
		)
		metricArgs, _ := json.Marshal(map[string]any{
			"resource": resource.ID, "catalog_key": "workload_availability", "query": metricRequest.Query,
			"from": execution.Snapshot.TimeRange.From, "to": execution.Snapshot.TimeRange.To,
			"configuration_revision": revision.ID,
		})
		calls = append(calls, workspaceToolCall{
			name: "metrics.query", arguments: metricArgs,
			execute: func(toolCtx context.Context) (WorkspaceToolObservation, error) {
				if prepareErr != nil {
					return WorkspaceToolObservation{}, prepareErr
				}
				return r.observeMetrics(toolCtx, resource, metricRequest)
			},
		})

		logFilter := telemetry.LogFilter{}
		if _, scenarioID, ok := workspaceScenarioSubject(execution); ok {
			logFilter.ScenarioID = scenarioID
		}
		logFrom := workspaceBoundedLogFrom(
			execution.Snapshot.TimeRange.From, execution.Snapshot.TimeRange.To, logFilter.ScenarioID,
		)
		logRequest, logPrepareErr := telemetry.PrepareBoundedLogToolRequest(
			execution.Snapshot.Scope.ClusterID, resource.Namespace, resource,
			logFrom, execution.Snapshot.TimeRange.To,
			min(100, max(1, revision.General.QueryMaxResults)), logFilter, revision,
		)
		logArgs, _ := json.Marshal(map[string]any{
			"resource": resource.ID, "query": logRequest.Query,
			"from": logFrom, "to": execution.Snapshot.TimeRange.To,
			"configuration_revision": revision.ID, "scenario_id": logFilter.ScenarioID,
		})
		calls = append(calls, workspaceToolCall{
			name: "logs.query", arguments: logArgs,
			execute: func(toolCtx context.Context) (WorkspaceToolObservation, error) {
				if logPrepareErr != nil {
					return WorkspaceToolObservation{}, logPrepareErr
				}
				return r.observeLogs(toolCtx, execution, resource, logRequest)
			},
		})

		traceRequest, tracePrepareErr := telemetry.PrepareBoundedTraceToolRequest(
			execution.Snapshot.Scope.ClusterID, resource.Namespace, resource,
			execution.Snapshot.TimeRange.From, execution.Snapshot.TimeRange.To,
			min(50, max(1, revision.General.QueryMaxResults)), revision,
		)
		traceArgs, _ := json.Marshal(map[string]any{
			"resource": resource.ID, "query": traceRequest.Query,
			"from": execution.Snapshot.TimeRange.From, "to": execution.Snapshot.TimeRange.To,
			"configuration_revision": revision.ID,
		})
		calls = append(calls, workspaceToolCall{
			name: "traces.search", arguments: traceArgs,
			execute: func(toolCtx context.Context) (WorkspaceToolObservation, error) {
				if tracePrepareErr != nil {
					return WorkspaceToolObservation{}, tracePrepareErr
				}
				return r.observeTraces(toolCtx, resource, traceRequest)
			},
		})
	}

	for index, call := range calls {
		if index >= maxCalls {
			break
		}
		if err := r.executeTool(ctx, lease, call, toolTimeout); err != nil {
			return err
		}
	}
	return nil
}

func workspaceBoundedLogFrom(from, to time.Time, scenarioID string) time.Time {
	if scenarioID == "" {
		return from
	}
	focused := to.Add(-30 * time.Second)
	if focused.After(from) {
		return focused
	}
	return from
}

func (r *WorkspaceRunner) executeTool(ctx context.Context, lease WorkspaceLease, call workspaceToolCall, timeout time.Duration) error {
	step, err := r.config.Store.StartWorkspaceTool(ctx, lease, call.name, call.arguments)
	if err != nil {
		return err
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	observation, toolErr := call.execute(toolCtx)
	cancel()
	if toolErr == nil {
		_, err = r.config.Store.CompleteWorkspaceTool(ctx, lease, step, observation)
		return err
	}
	code, summary := workspaceToolError(toolErr)
	auditCtx, auditCancel := context.WithTimeout(context.Background(), 3*time.Second)
	failErr := r.config.Store.FailWorkspaceTool(auditCtx, lease, step, code, summary)
	auditCancel()
	if failErr != nil {
		return failErr
	}
	if ctx.Err() != nil {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return ctx.Err()
	}
	return nil
}

func (r *WorkspaceRunner) observeKubernetes(ctx context.Context, execution WorkspaceExecutionContext, revision settings.Revision) (WorkspaceToolObservation, error) {
	limit := min(infrastructure.DefaultLimit, max(1, execution.Limits.MaxEvidenceItems*16))
	projection, err := r.config.Kubernetes.Read(ctx, infrastructure.ReadRequest{
		ClusterID:  execution.Snapshot.Scope.ClusterID,
		Namespaces: append([]string(nil), execution.Snapshot.Scope.Namespaces...),
		Limit:      limit,
	})
	if err != nil {
		return WorkspaceToolObservation{}, err
	}
	facts := make([]any, 0, min(len(projection.Nodes), 32))
	for _, item := range projection.Nodes {
		if len(facts) == 32 {
			break
		}
		facts = append(facts, map[string]any{
			"kind": item.Kind, "namespace": item.Namespace, "name": item.Name,
			"status": item.Status, "health": item.Health.State, "summary": workspaceBound(item.Health.Summary, 256),
			"containers": item.Containers,
		})
	}
	factsJSON, _ := json.Marshal(facts)
	provenance, _ := json.Marshal(map[string]any{
		"provider": "kubernetes", "identity": projection.Source.Identity,
		"cluster_id": projection.Source.ClusterID, "server_version": projection.Source.ServerVersion,
		"configuration_revision_id": revision.ID,
	})
	collected := projection.Source.CollectedAt.UTC()
	if collected.IsZero() {
		collected = time.Now().UTC()
	}
	return WorkspaceToolObservation{
		Tool: "kubernetes.resources", Source: "kubernetes", ResourceRef: workspacePrimaryResource(execution.Snapshot),
		Query: fmt.Sprintf("typed resources cluster=%s namespaces=%s limit=%d", execution.Snapshot.Scope.ClusterID,
			strings.Join(execution.Snapshot.Scope.Namespaces, ","), limit),
		Summary: fmt.Sprintf("Kubernetes 返回 %d 个 typed resources、%d 条关系和 %d 个 Provider issues。", len(projection.Nodes), len(projection.Edges), len(projection.Issues)),
		Facts:   factsJSON, Provenance: provenance, ObservedAt: collected, CollectedAt: collected,
		Truncated: projection.Truncated, Partial: projection.Partial || len(projection.Issues) > 0,
		SourceRevision: projection.Source.ServerVersion,
		TypedFacts:     workspaceScenarioKubernetesFacts(execution, projection),
	}, nil
}

func (r *WorkspaceRunner) observeMetrics(ctx context.Context, resource telemetry.ResourceReference, request observability.ProviderQueryRequest) (WorkspaceToolObservation, error) {
	result, err := r.config.Metrics.Query(ctx, request)
	if err != nil {
		return WorkspaceToolObservation{}, err
	}
	facts := make([]any, 0, min(len(result.Result.Series), 16))
	observed := result.Source.CollectedAt.UTC()
	for _, series := range result.Result.Series {
		if len(facts) == 16 {
			break
		}
		var latest any
		if len(series.Points) > 0 {
			point := series.Points[len(series.Points)-1]
			latest = map[string]any{"timestamp": point.Timestamp.UTC(), "value": point.Value}
			if point.Timestamp.After(observed) {
				observed = point.Timestamp.UTC()
			}
		}
		facts = append(facts, map[string]any{"labels": series.Labels, "latest": latest})
	}
	factsJSON, _ := json.Marshal(facts)
	provenance, _ := json.Marshal(map[string]any{
		"provider": result.Source.Provider, "identity": result.Source.Identity,
		"server_version": result.Source.ServerVersion, "query_hash": request.QueryHash,
	})
	return WorkspaceToolObservation{
		Tool: "metrics.query", Source: "prometheus", ResourceRef: resource.ID, Query: request.Query,
		Summary: fmt.Sprintf("Prometheus 返回 %d 个 series、%d 个 samples。", result.SeriesCount, result.SampleCount),
		Facts:   factsJSON, Provenance: provenance, ObservedAt: observed, CollectedAt: result.Source.CollectedAt.UTC(),
		Truncated: result.Truncated, Partial: result.Partial, SourceRevision: result.Source.ServerVersion,
	}, nil
}

func (r *WorkspaceRunner) observeLogs(ctx context.Context, execution WorkspaceExecutionContext, resource telemetry.ResourceReference, request telemetry.ProviderLogRequest) (WorkspaceToolObservation, error) {
	result, err := r.config.Telemetry.QueryLogs(ctx, request)
	if err != nil {
		return WorkspaceToolObservation{}, err
	}
	facts := make([]any, 0, min(len(result.Entries), 12))
	observed := result.Source.CollectedAt.UTC()
	for _, entry := range result.Entries {
		if len(facts) == 12 {
			break
		}
		facts = append(facts, map[string]any{
			"timestamp": entry.Timestamp.UTC(), "level": entry.Level, "service": entry.Service,
			"trace_id": entry.TraceID, "message": workspaceBound(entry.Message, 320), "reason": workspaceLogReason(entry.Attributes),
		})
		if entry.Timestamp.After(observed) {
			observed = entry.Timestamp.UTC()
		}
	}
	factsJSON, _ := json.Marshal(facts)
	provenance, _ := json.Marshal(map[string]any{
		"provider": result.Source.Provider, "identity": result.Source.Identity,
		"server_version": result.Source.ServerVersion,
	})
	return WorkspaceToolObservation{
		Tool: "logs.query", Source: "elasticsearch", ResourceRef: resource.ID, Query: request.Query,
		Summary: fmt.Sprintf("Elasticsearch 返回 %d 条 bounded log entries。", len(result.Entries)),
		Facts:   factsJSON, Provenance: provenance, ObservedAt: observed, CollectedAt: result.Source.CollectedAt.UTC(),
		Truncated: result.Truncated, Partial: result.Partial, SourceRevision: result.Source.ServerVersion,
		TypedFacts: workspaceScenarioLogFacts(execution, resource, result.Entries),
	}, nil
}

func workspaceScenarioKubernetesFacts(execution WorkspaceExecutionContext, projection infrastructure.Projection) []EvidenceFact {
	resource, scenarioID, ok := workspaceScenarioSubject(execution)
	if !ok {
		return nil
	}
	var deployment *infrastructure.Resource
	for index := range projection.Nodes {
		item := &projection.Nodes[index]
		if item.Kind == resource.Kind && item.Namespace == resource.Namespace && item.Name == resource.Name &&
			item.Labels["cloudops.io/scenario-id"] == scenarioID {
			deployment = item
			break
		}
	}
	if deployment == nil {
		return nil
	}
	attributes := map[string]string{
		"scenario_id": scenarioID, "cluster_id": execution.Snapshot.Scope.ClusterID,
		"namespace": resource.Namespace, "workload_kind": resource.Kind, "workload": resource.Name,
		"container": "scenario", "env_key": "REQUIRED_ENV",
		"resource_uid": deployment.SourceUID, "resource_version": deployment.ResourceVersion,
		"generation": fmt.Sprint(deployment.Generation), "init_container_count": fmt.Sprint(deployment.InitContainerCount),
		"ephemeral_container_count": fmt.Sprint(deployment.EphemeralCount),
	}
	facts := []EvidenceFact{workspaceRuntimeFact(
		"workload.subject_confirmed", "kubernetes", "kubernetes.resources/deployment", "scenario-subject", "support", attributes,
	)}
	for _, container := range deployment.Containers {
		if container.Name != "scenario" {
			continue
		}
		if deployment.ContainersTruncated || container.EnvNamesTruncated || container.HasEnvFrom ||
			container.HasSecretReference {
			break
		}
		factType, claimUse := "kubernetes.required_env_absent", "support"
		for _, name := range container.EnvNames {
			if name == "REQUIRED_ENV" {
				factType, claimUse = "kubernetes.required_env_present", "blocking"
				break
			}
		}
		attributes["env_names"] = strings.Join(container.EnvNames, ",")
		facts = append(facts, workspaceRuntimeFact(
			factType, "kubernetes", "kubernetes.resources/deployment-pod-template", "scenario-required-env", claimUse, attributes,
		))
		break
	}
	return facts
}

func workspaceScenarioLogFacts(execution WorkspaceExecutionContext, resource telemetry.ResourceReference, entries []telemetry.LogEntry) []EvidenceFact {
	_, scenarioID, ok := workspaceScenarioSubject(execution)
	if !ok || resource.Kind != "Deployment" || resource.Namespace != "demo" || resource.Name != "cloudops-scenario-fault" {
		return nil
	}
	for _, entry := range entries {
		if entry.Attributes["scenario_id"] != scenarioID || workspaceLogReason(entry.Attributes) != "required_env_missing" {
			continue
		}
		return []EvidenceFact{workspaceRuntimeFact(
			"log.required_env_missing", "elasticsearch", "logs.query/structured-reason", "scenario-required-env-log", "support",
			map[string]string{
				"scenario_id": scenarioID, "namespace": resource.Namespace, "workload_kind": resource.Kind,
				"workload": resource.Name, "reason": "required_env_missing", "env_key": "REQUIRED_ENV",
			},
		)}
	}
	return nil
}

func workspaceRuntimeFact(factType, source, collectionPath, group, claimUse string, attributes map[string]string) EvidenceFact {
	return EvidenceFact{
		Type: factType, SourceSystem: source, CollectionPath: collectionPath, CorroborationGroup: group,
		Authority: "runtime_observation", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
		ClaimUse: claimUse, CollectionStatus: CollectionAvailable, Direct: true, Attributes: attributes,
	}
}

func workspaceScenarioSubject(execution WorkspaceExecutionContext) (telemetry.ResourceReference, string, bool) {
	resource, ok := workspaceWorkloadResource(execution.Snapshot.Resources)
	if !ok || execution.Snapshot.Scope.ClusterID != "cloudops-local" || execution.Snapshot.Scope.Environment != "local" ||
		resource.Kind != "Deployment" || resource.Namespace != "demo" || resource.Name != "cloudops-scenario-fault" {
		return telemetry.ResourceReference{}, "", false
	}
	var filters struct {
		ScenarioID string `json:"scenario_id"`
	}
	if json.Unmarshal(execution.Snapshot.Filters, &filters) != nil || !workspaceScenarioIdentity(filters.ScenarioID) {
		return telemetry.ResourceReference{}, "", false
	}
	return resource, filters.ScenarioID, true
}

func workspaceLogReason(attributes map[string]string) string {
	for _, key := range []string{"reason", "error.reason", "cloudops.reason"} {
		if value := strings.ToLower(strings.TrimSpace(attributes[key])); value != "" {
			return workspaceBound(value, 128)
		}
	}
	return ""
}

func (r *WorkspaceRunner) observeTraces(ctx context.Context, resource telemetry.ResourceReference, request telemetry.ProviderTraceSearchRequest) (WorkspaceToolObservation, error) {
	result, err := r.config.Telemetry.SearchTraces(ctx, request)
	if err != nil {
		return WorkspaceToolObservation{}, err
	}
	facts := make([]any, 0, min(len(result.Traces), 12))
	observed := result.Source.CollectedAt.UTC()
	for _, trace := range result.Traces {
		if len(facts) == 12 {
			break
		}
		facts = append(facts, map[string]any{
			"trace_id": trace.TraceID, "service": trace.RootService, "operation": trace.RootOperation,
			"start_time": trace.StartTime.UTC(), "duration_ms": trace.DurationMS,
			"span_count": trace.SpanCount, "error_span_count": trace.ErrorSpanCount,
		})
		if trace.StartTime.After(observed) {
			observed = trace.StartTime.UTC()
		}
	}
	factsJSON, _ := json.Marshal(facts)
	provenance, _ := json.Marshal(map[string]any{
		"provider": result.Source.Provider, "identity": result.Source.Identity,
		"server_version": result.Source.ServerVersion,
	})
	return WorkspaceToolObservation{
		Tool: "traces.search", Source: "tempo", ResourceRef: resource.ID, Query: request.Query,
		Summary: fmt.Sprintf("Tempo 返回 %d 条 bounded traces。", len(result.Traces)),
		Facts:   factsJSON, Provenance: provenance, ObservedAt: observed, CollectedAt: result.Source.CollectedAt.UTC(),
		Truncated: result.Truncated, Partial: result.Partial, SourceRevision: result.Source.ServerVersion,
	}, nil
}

func workspaceWorkloadResource(resources []telemetry.ResourceReference) (telemetry.ResourceReference, bool) {
	for _, resource := range resources {
		switch resource.Kind {
		case "Deployment", "StatefulSet", "DaemonSet":
			if resource.ID != "" && resource.Namespace != "" && resource.Name != "" {
				return resource, true
			}
		}
	}
	return telemetry.ResourceReference{}, false
}

func workspacePrimaryResource(snapshot WorkspaceContextSnapshot) string {
	if len(snapshot.Resources) > 0 && strings.TrimSpace(snapshot.Resources[0].ID) != "" {
		return snapshot.Resources[0].ID
	}
	return "cluster:" + snapshot.Scope.ClusterID
}

func workspaceMetricStep(window telemetry.TimeRange, maxResults int) int {
	if maxResults < 2 {
		maxResults = 2
	}
	seconds := int(window.To.Sub(window.From) / time.Second)
	step := (seconds + maxResults - 2) / (maxResults - 1)
	return min(3600, max(15, step))
}

func workspaceToolError(err error) (string, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "TOOL_TIMEOUT", "bounded Provider read timed out"
	case errors.Is(err, observability.ErrProviderDisabled), errors.Is(err, telemetry.ErrProviderDisabled):
		return "PROVIDER_DISABLED", "Provider is disabled in the exact Configuration Revision"
	case errors.Is(err, observability.ErrUnavailable), errors.Is(err, telemetry.ErrUnavailable),
		errors.Is(err, infrastructure.ErrUnavailable):
		return "PROVIDER_UNAVAILABLE", "Provider is unavailable for this bounded read"
	case errors.Is(err, observability.ErrBoundExceeded), errors.Is(err, telemetry.ErrBoundExceeded):
		return "QUERY_BOUND_EXCEEDED", "bounded Provider request exceeded its configured limits"
	case errors.Is(err, observability.ErrUnauthorized):
		return "QUERY_UNAUTHORIZED", "Provider query did not satisfy the exact authorization contract"
	default:
		value := strings.ToUpper(err.Error())
		if strings.Contains(value, "DISABLED") {
			return "PROVIDER_DISABLED", "Provider is disabled in the exact Configuration Revision"
		}
		if strings.Contains(value, "UNAVAILABLE") || strings.Contains(value, "GATEWAY") {
			return "PROVIDER_UNAVAILABLE", "Provider is unavailable for this bounded read"
		}
		return "TOOL_FAILED", workspaceBound(err.Error(), 512)
	}
}
