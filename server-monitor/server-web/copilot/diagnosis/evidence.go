package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"server-web/model"
	"strings"
	"sync"
	"time"
)

const (
	ToolAlertListActive = "alert.list_active"
	ToolAlertHistory    = "alert.history"
	ToolHostMetrics     = "host.metrics"
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
	runner  ToolRunner
	timeout time.Duration
	now     func() time.Time
}

type EvidenceOptions struct {
	Runner  ToolRunner
	Timeout time.Duration
	Now     func() time.Time
}

func NewEvidenceCollector(options EvidenceOptions) *EvidenceCollector {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &EvidenceCollector{runner: options.Runner, timeout: timeout, now: now}
}

func (c *EvidenceCollector) Collect(ctx context.Context, alert AlertContext) EvidenceBundle {
	collectedAt := c.now().UTC()
	bundle := EvidenceBundle{
		AlertContext: alert,
		Runbooks:     []json.RawMessage{},
		CollectedAt:  collectedAt,
	}
	if c == nil || c.runner == nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, CollectionError{Source: "tool_registry", Error: "tool registry unavailable"})
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

	wg.Add(3)
	go func() {
		defer wg.Done()
		active, err := c.collectActiveAlerts(ctx, alert)
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
		history, err := c.collectHistory(ctx, alert)
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
		metrics, err := c.collectMetrics(ctx, alert)
		if err != nil {
			recordError(ToolHostMetrics, err)
			return
		}
		mu.Lock()
		bundle.Metrics = append(bundle.Metrics, metrics...)
		mu.Unlock()
	}()
	wg.Wait()
	if err := ctx.Err(); err != nil {
		recordError("evidence_collector", err)
	}
	return bundle
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
		return nil, fmt.Errorf("instance is required")
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
		values := collectMetricValues(series)
		if len(values) == 0 {
			continue
		}
		metrics = append(metrics, MetricEvidence{
			Name:        name,
			Source:      ToolHostMetrics,
			Window:      payload.Range,
			Avg:         avg(values),
			Max:         max(values),
			Last:        values[len(values)-1],
			Trend:       trend(values),
			CollectedAt: c.now().UTC(),
		})
	}
	return metrics, nil
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
		return "tool execution failed"
	}
	return result.Error
}
