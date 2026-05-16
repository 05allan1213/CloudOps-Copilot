package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	k8sreader "server-web/internal/copilot/k8s"
	promclient "server-web/internal/infra/prometheus"
	"server-web/internal/model"
)

const (
	ToolAlertListActive   = "alert.list_active"
	ToolAlertHistory      = "alert.history"
	ToolHostMetrics       = "host.metrics"
	ToolPromQueryRange    = "prom.query_range"
	ToolRunbookSearch     = "runbook.search"
	ToolK8sGetPods        = "k8s.get_pods"
	ToolK8sGetDeployments = "k8s.get_deployments"
	ToolK8sGetServices    = "k8s.get_services"
	ToolK8sGetNodes       = "k8s.get_nodes"
	ToolK8sGetEvents      = "k8s.get_events"
	ToolK8sGetLogs        = "k8s.get_logs"

	defaultEvidenceTimeout     = 45 * time.Second
	defaultEvidenceToolTimeout = 30 * time.Second
)

type ToolRunner interface {
	ExecuteTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
}

type ToolResult struct {
	Success bool
	Data    interface{}
	Error   string
}

type EvidenceCollector struct {
	runner        ToolRunner
	timeout       time.Duration
	toolTimeout   time.Duration
	runbookLimit  int
	rerankEnabled bool
	now           func() time.Time
}

type EvidenceOptions struct {
	Runner        ToolRunner
	Timeout       time.Duration
	ToolTimeout   time.Duration
	RunbookLimit  int
	RerankEnabled bool
	Now           func() time.Time
}

func NewEvidenceCollector(options EvidenceOptions) *EvidenceCollector {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultEvidenceTimeout
	}
	toolTimeout := options.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = defaultEvidenceToolTimeout
	}
	runbookLimit := options.RunbookLimit
	if runbookLimit <= 0 {
		runbookLimit = 2
	}
	if runbookLimit > 5 {
		runbookLimit = 5
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &EvidenceCollector{runner: options.Runner, timeout: timeout, toolTimeout: toolTimeout, runbookLimit: runbookLimit, rerankEnabled: options.RerankEnabled, now: now}
}

func (c *EvidenceCollector) Collect(ctx context.Context, alert AlertContext) EvidenceBundle {
	collectedAt := c.now().UTC()
	bundle := EvidenceBundle{
		AlertContext: alert,
		Runbooks:     []RunbookEvidence{},
		CollectedAt:  collectedAt,
	}
	if c == nil || c.runner == nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, CollectionError{Source: "tool_registry", Error: "工具注册表不可用"})
		return bundle
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	recordError := func(source string, err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		bundle.CollectionErrors = append(bundle.CollectionErrors, CollectionError{Source: source, Error: err.Error()})
	}

	wg.Add(5)
	go func() {
		defer wg.Done()
		toolCtx, toolCancel := c.withToolTimeout(ctx)
		defer toolCancel()
		active, err := c.collectActiveAlerts(toolCtx, alert)
		if err != nil {
			recordError(ToolAlertListActive, err)
			return
		}
		mu.Lock()
		bundle.ActiveAlerts = active
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		toolCtx, toolCancel := c.withToolTimeout(ctx)
		defer toolCancel()
		history, err := c.collectHistory(toolCtx, alert)
		if err != nil {
			recordError(ToolAlertHistory, err)
			return
		}
		mu.Lock()
		bundle.History = history
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		toolCtx, toolCancel := c.withToolTimeout(ctx)
		defer toolCancel()
		metrics, err := c.collectMetrics(toolCtx, alert)
		if err != nil {
			recordError(ToolHostMetrics, err)
			return
		}
		mu.Lock()
		bundle.Metrics = append(bundle.Metrics, metrics...)
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		toolCtx, toolCancel := c.withToolTimeout(ctx)
		defer toolCancel()
		metrics, err := c.collectPromQueryRange(toolCtx, alert)
		if err != nil {
			recordError(ToolPromQueryRange, err)
			return
		}
		mu.Lock()
		bundle.Metrics = append(bundle.Metrics, metrics...)
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		toolCtx, toolCancel := c.withToolTimeout(ctx)
		defer toolCancel()
		runbooks, err := c.collectRunbooks(toolCtx, alert)
		if err != nil {
			recordError(ToolRunbookSearch, err)
			return
		}
		mu.Lock()
		bundle.Runbooks = runbooks
		mu.Unlock()
	}()
	if isK8sTarget(alert) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			toolCtx, toolCancel := c.withToolTimeout(ctx)
			defer toolCancel()
			evidence, errors := c.collectK8sEvidence(toolCtx, alert)
			mu.Lock()
			bundle.K8s = evidence
			bundle.CollectionErrors = append(bundle.CollectionErrors, errors...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		recordError("evidence_collector", err)
	}
	return bundle
}

func (c *EvidenceCollector) collectK8sEvidence(ctx context.Context, alert AlertContext) (K8sEvidence, []CollectionError) {
	evidence := K8sEvidence{
		Enabled:     true,
		Namespace:   firstNonEmpty(alert.Namespace, "default"),
		TargetKind:  alert.TargetKind,
		TargetName:  alert.TargetName,
		CollectedAt: c.now().UTC(),
	}
	var errs []CollectionError
	record := func(source string, err error) {
		if err == nil {
			return
		}
		item := CollectionError{Source: source, Error: err.Error()}
		errs = append(errs, item)
		evidence.Errors = append(evidence.Errors, item)
	}

	switch alert.TargetKind {
	case TargetKindK8sDeployment:
		args, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "name": alert.TargetName, "limit": 1})
		deployments, err := executeK8sTool[[]k8sreader.DeploymentSummary](ctx, c.runner, ToolK8sGetDeployments, args)
		if err != nil {
			record(ToolK8sGetDeployments, err)
		} else {
			evidence.Deployments = deployments
		}
		selector := deploymentSelector(alert.TargetName, deployments)
		podArgs, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "label_selector": selector, "limit": 20})
		pods, err := executeK8sTool[[]k8sreader.PodSummary](ctx, c.runner, ToolK8sGetPods, podArgs)
		if err != nil {
			record(ToolK8sGetPods, err)
		} else {
			evidence.Pods = pods
		}
		serviceArgs, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "limit": 50})
		services, err := executeK8sTool[[]k8sreader.ServiceSummary](ctx, c.runner, ToolK8sGetServices, serviceArgs)
		if err != nil {
			record(ToolK8sGetServices, err)
		} else {
			evidence.Services = relatedServices(services, alert.TargetName, deployments)
		}
		eventArgs, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "involved_kind": "Deployment", "involved_name": alert.TargetName, "limit": 10})
		events, err := executeK8sTool[[]k8sreader.EventSummary](ctx, c.runner, ToolK8sGetEvents, eventArgs)
		if err != nil {
			record(ToolK8sGetEvents, err)
		} else {
			evidence.Events = events
		}
	case TargetKindK8sPod:
		podArgs, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "field_selector": "metadata.name=" + alert.TargetName, "limit": 1})
		pods, err := executeK8sTool[[]k8sreader.PodSummary](ctx, c.runner, ToolK8sGetPods, podArgs)
		if err != nil {
			record(ToolK8sGetPods, err)
		} else {
			evidence.Pods = pods
		}
		eventArgs, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "involved_kind": "Pod", "involved_name": alert.TargetName, "limit": 10})
		events, err := executeK8sTool[[]k8sreader.EventSummary](ctx, c.runner, ToolK8sGetEvents, eventArgs)
		if err != nil {
			record(ToolK8sGetEvents, err)
		} else {
			evidence.Events = events
		}
		logArgs, _ := json.Marshal(map[string]interface{}{"namespace": evidence.Namespace, "pod_name": alert.TargetName, "tail_lines": 20})
		logs, err := executeK8sTool[k8sreader.LogSnippet](ctx, c.runner, ToolK8sGetLogs, logArgs)
		if err != nil {
			record(ToolK8sGetLogs, err)
		} else {
			evidence.Logs = []k8sreader.LogSnippet{logs}
		}
	case TargetKindK8sNode:
		nodes, err := executeK8sTool[[]k8sreader.NodeSummary](ctx, c.runner, ToolK8sGetNodes, json.RawMessage(`{"limit":50}`))
		if err != nil {
			record(ToolK8sGetNodes, err)
		} else {
			for _, node := range nodes {
				if node.Name == alert.TargetName || alert.TargetName == "" {
					evidence.Nodes = append(evidence.Nodes, node)
				}
			}
		}
	}
	return evidence, errs
}

func executeK8sTool[T any](ctx context.Context, runner ToolRunner, name string, args json.RawMessage) (T, error) {
	var zero T
	result, err := runner.ExecuteTool(ctx, name, args)
	if err != nil {
		return zero, err
	}
	if !result.Success {
		return zero, errors.New(toolErrorString(result))
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return zero, err
	}
	var payload T
	if err := json.Unmarshal(data, &payload); err != nil {
		return zero, err
	}
	return payload, nil
}

func isK8sTarget(alert AlertContext) bool {
	switch alert.TargetKind {
	case TargetKindK8sDeployment, TargetKindK8sPod, TargetKindK8sNode:
		return strings.TrimSpace(alert.TargetName) != ""
	default:
		return false
	}
}

func deploymentSelector(name string, deployments []k8sreader.DeploymentSummary) string {
	if len(deployments) > 0 && len(deployments[0].Selector) > 0 {
		return labels.Set(deployments[0].Selector).AsSelector().String()
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return labels.Set{"app": name}.AsSelector().String()
}

func relatedServices(services []k8sreader.ServiceSummary, deploymentName string, deployments []k8sreader.DeploymentSummary) []k8sreader.ServiceSummary {
	deploymentName = strings.TrimSpace(deploymentName)
	deploymentLabels := map[string]string(nil)
	if len(deployments) > 0 {
		deploymentLabels = deployments[0].Selector
	}
	result := make([]k8sreader.ServiceSummary, 0, len(services))
	for _, service := range services {
		if service.Name == deploymentName || selectorSubset(service.Selector, deploymentLabels) {
			result = append(result, service)
		}
	}
	return result
}

func selectorSubset(subset, values map[string]string) bool {
	if len(subset) == 0 || len(values) == 0 {
		return false
	}
	for key, value := range subset {
		if values[key] != value {
			return false
		}
	}
	return true
}

func (c *EvidenceCollector) collectRunbooks(ctx context.Context, alert AlertContext) ([]RunbookEvidence, error) {
	args, _ := json.Marshal(map[string]interface{}{
		"alert_name": alert.AlertName,
		"keywords":   runbookKeywords(alert),
		"metrics":    runbookMetrics(alert),
		"limit":      c.runbookLimit,
		"rerank":     c.rerankEnabled,
	})
	result, err := c.runner.ExecuteTool(ctx, ToolRunbookSearch, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, errors.New(toolErrorString(result))
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, err
	}
	var payload []RunbookEvidence
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	collectedAt := c.now().UTC()
	for i := range payload {
		payload[i].Source = ToolRunbookSearch
		payload[i].CollectedAt = collectedAt
	}
	return payload, nil
}

func (c *EvidenceCollector) withToolTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := defaultEvidenceToolTimeout
	if c != nil && c.toolTimeout > 0 {
		timeout = c.toolTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *EvidenceCollector) collectPromQueryRange(ctx context.Context, alert AlertContext) ([]MetricEvidence, error) {
	queries := supplementaryPromQueries(alert)
	if len(queries) == 0 {
		return nil, nil
	}
	collectedAt := c.now().UTC()
	start := collectedAt.Add(-15 * time.Minute)
	metrics := make([]MetricEvidence, 0, len(queries))
	var errs []error
	for name, query := range queries {
		args, _ := json.Marshal(map[string]interface{}{
			"query":      query,
			"start":      start.Format(time.RFC3339),
			"end":        collectedAt.Format(time.RFC3339),
			"step":       "1m",
			"max_points": 1000,
		})
		result, err := c.runner.ExecuteTool(ctx, ToolPromQueryRange, args)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !result.Success {
			errs = append(errs, errors.New(toolErrorString(result)))
			continue
		}
		evidence, err := metricEvidenceFromToolData(name, ToolPromQueryRange, "15m", result.Data, collectedAt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		metrics = append(metrics, evidence...)
	}
	if len(errs) > 0 {
		return metrics, errors.Join(errs...)
	}
	return metrics, nil
}

func (c *EvidenceCollector) collectActiveAlerts(ctx context.Context, alert AlertContext) ([]AlertContext, error) {
	args, _ := json.Marshal(map[string]interface{}{"severity": alert.Severity})
	result, err := c.runner.ExecuteTool(ctx, ToolAlertListActive, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, errors.New(toolErrorString(result))
	}
	var alerts []AlertContext
	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, err
	}
	var records []struct {
		Status      string            `json:"status"`
		Fingerprint string            `json:"fingerprint"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		EndsAt      time.Time         `json:"endsAt"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.Fingerprint == "" {
			continue
		}
		ctx := AlertContext{
			Fingerprint: record.Fingerprint,
			AlertName:   record.Labels["alertname"],
			Instance:    record.Labels["instance"],
			TargetKind:  "host",
			TargetName:  record.Labels["instance"],
			Namespace:   record.Labels["namespace"],
			Severity:    firstNonEmpty(record.Labels["severity"], "warning"),
			Status:      record.Status,
			Summary:     firstNonEmpty(record.Annotations["summary"], record.Annotations["description"]),
			Labels:      cloneLabels(record.Labels),
			Annotations: cloneLabels(record.Annotations),
			StartsAt:    record.StartsAt.UTC(),
			Source:      ToolAlertListActive,
			CollectedAt: c.now().UTC(),
		}
		if !record.EndsAt.IsZero() {
			value := record.EndsAt.UTC()
			ctx.EndsAt = &value
		}
		alerts = append(alerts, ctx)
	}
	return alerts, nil
}

func (c *EvidenceCollector) collectHistory(ctx context.Context, alert AlertContext) ([]HistoryEvidence, error) {
	args, _ := json.Marshal(map[string]interface{}{
		"alert_name": alert.AlertName,
		"instance":   alert.Instance,
		"window":     "24h",
		"page":       1,
		"page_size":  20,
	})
	result, err := c.runner.ExecuteTool(ctx, ToolAlertHistory, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, errors.New(toolErrorString(result))
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Items []model.AlertHistory `json:"items"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	history := make([]HistoryEvidence, 0, len(payload.Items))
	for _, item := range payload.Items {
		history = append(history, HistoryEvidence{
			AlertHistoryID: item.ID,
			Fingerprint:    item.Fingerprint,
			AlertName:      item.AlertName,
			Instance:       item.Instance,
			Severity:       item.Severity,
			Status:         item.Status,
			Summary:        item.Summary,
			FiredAt:        item.FiredAt.UTC(),
			ResolvedAt:     item.ResolvedAt,
		})
	}
	return history, nil
}

func (c *EvidenceCollector) collectMetrics(ctx context.Context, alert AlertContext) ([]MetricEvidence, error) {
	if strings.TrimSpace(alert.Instance) == "" {
		return nil, fmt.Errorf("instance 参数必填")
	}
	args, _ := json.Marshal(map[string]interface{}{"instance": alert.Instance, "window": "15m"})
	result, err := c.runner.ExecuteTool(ctx, ToolHostMetrics, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, errors.New(toolErrorString(result))
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Range   string `json:"range"`
		Metrics map[string][]struct {
			Values []struct {
				Value float64 `json:"value"`
			} `json:"values"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	metrics := make([]MetricEvidence, 0, len(payload.Metrics))
	for name, series := range payload.Metrics {
		metrics = append(metrics, metricEvidenceFromValues(name, ToolHostMetrics, payload.Range, collectMetricValues(series), c.now().UTC())...)
	}
	return metrics, nil
}

func metricEvidenceFromToolData(name, source, window string, data interface{}, collectedAt time.Time) ([]MetricEvidence, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var series []struct {
		Values []struct {
			Value float64 `json:"value"`
		} `json:"values"`
	}
	if err := json.Unmarshal(raw, &series); err != nil {
		return nil, err
	}
	return metricEvidenceFromValues(name, source, window, collectMetricValues(series), collectedAt), nil
}

func metricEvidenceFromValues(name, source, window string, values []float64, collectedAt time.Time) []MetricEvidence {
	if len(values) == 0 {
		return nil
	}
	return []MetricEvidence{
		{
			Name:        name,
			Source:      source,
			Window:      window,
			Avg:         avg(values),
			Max:         max(values),
			Last:        values[len(values)-1],
			Trend:       trend(values),
			CollectedAt: collectedAt,
		},
	}
}

func supplementaryPromQueries(alert AlertContext) map[string]string {
	alertName := strings.ToLower(alert.AlertName)
	queries := map[string]string{}
	addBuiltQuery := func(name, metric string) {
		query, err := promclient.BuildQuery(metric, alert.Instance, nil)
		if err == nil {
			queries[name] = query
		}
	}
	switch {
	case strings.Contains(alertName, "cpu"):
		addBuiltQuery("cpu_usage_prom", promclient.MetricCPUUsage)
		addBuiltQuery("load1_prom", promclient.MetricLoad1)
		addBuiltQuery("process_count_prom", promclient.MetricProcessCount)
	case strings.Contains(alertName, "memory") || strings.Contains(alertName, "内存"):
		addBuiltQuery("memory_usage_prom", promclient.MetricMemoryUsage)
		addBuiltQuery("memory_available_prom", promclient.MetricMemoryAvailable)
	case strings.Contains(alertName, "disk") || strings.Contains(alertName, "磁盘"):
		addBuiltQuery("disk_usage_prom", promclient.MetricDiskUsage)
	case strings.Contains(alertName, "down") || strings.Contains(alertName, "hostdown"):
		if strings.TrimSpace(alert.Instance) != "" {
			queries["up_prom"] = fmt.Sprintf(`up{job="server-probe",instance=%q}`, alert.Instance)
		}
	}
	return queries
}

func runbookKeywords(alert AlertContext) []string {
	values := []string{alert.AlertName, alert.Severity}
	for _, key := range []string{"alertname", "job", "namespace"} {
		if value := alert.Labels[key]; value != "" {
			values = append(values, value)
		}
	}
	return compactStrings(values)
}

func runbookMetrics(alert AlertContext) []string {
	alertName := strings.ToLower(alert.AlertName)
	switch {
	case strings.Contains(alertName, "cpu"):
		return []string{promclient.MetricCPUUsage, promclient.MetricLoad1, promclient.MetricProcessCount}
	case strings.Contains(alertName, "memory") || strings.Contains(alertName, "内存"):
		return []string{promclient.MetricMemoryUsage, promclient.MetricMemoryAvailable}
	case strings.Contains(alertName, "disk") || strings.Contains(alertName, "磁盘"):
		return []string{promclient.MetricDiskUsage}
	case strings.Contains(alertName, "down") || strings.Contains(alertName, "hostdown"):
		return []string{"up"}
	default:
		return nil
	}
}

func collectMetricValues(series []struct {
	Values []struct {
		Value float64 `json:"value"`
	} `json:"values"`
}) []float64 {
	values := []float64{}
	for _, item := range series {
		for _, point := range item.Values {
			if !math.IsNaN(point.Value) && !math.IsInf(point.Value, 0) {
				values = append(values, point.Value)
			}
		}
	}
	return values
}

func avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func trend(values []float64) string {
	if len(values) < 2 {
		return "flat"
	}
	first := values[0]
	last := values[len(values)-1]
	switch {
	case last > first:
		return "up"
	case last < first:
		return "down"
	default:
		return "flat"
	}
}

func toolErrorString(result ToolResult) string {
	if result.Error == "" {
		return "工具执行失败"
	}
	return result.Error
}
