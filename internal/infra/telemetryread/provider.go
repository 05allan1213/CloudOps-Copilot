// Package telemetryread implements Worker-owned bounded Elasticsearch and
// Tempo reads for the native Logs and Traces Workspaces.
package telemetryread

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

const (
	elasticsearchIndex = "logs-cloudops-*"
	maximumSpanCount   = 1_000
)

var sensitiveValue = regexp.MustCompile(`(?i)\b(token|password|secret|authorization|api[_-]?key)\s*[:=]\s*[^\s,;]+`)

type accessStore interface {
	Revision(context.Context, string) (settings.Revision, error)
	ProviderAccess(context.Context, string, settings.Provider) (settings.ProviderAccess, error)
}

type Provider struct {
	settings accessStore
	now      func() time.Time
}

func New(configuration accessStore) (*Provider, error) {
	if configuration == nil {
		return nil, errors.New("telemetry Provider requires Configuration Revision access")
	}
	return &Provider{settings: configuration, now: time.Now}, nil
}

func (p *Provider) Catalog(ctx context.Context, request telemetry.ProviderCatalogRequest) (telemetry.ProviderCatalog, error) {
	providerID, err := providerName(request.Provider)
	if err != nil {
		return telemetry.ProviderCatalog{}, err
	}
	revision, err := p.settings.Revision(ctx, request.ConfigurationRevision)
	if err != nil {
		return telemetry.ProviderCatalog{}, telemetry.ErrUnavailable
	}
	if err := telemetry.ValidateProviderCatalog(request, revision); err != nil {
		return telemetry.ProviderCatalog{}, err
	}
	access, err := p.settings.ProviderAccess(ctx, request.ConfigurationRevision, providerID)
	if err != nil {
		return telemetry.ProviderCatalog{}, telemetry.ErrUnavailable
	}
	defer access.Clear()
	path := "/"
	if providerID == settings.ProviderTempo {
		path = "/ready"
	}
	body, header, err := p.read(ctx, access, http.MethodGet, path, nil, nil, min(request.Bounds.MaxResponseBytes, 64*1024))
	if err != nil {
		return telemetry.ProviderCatalog{}, err
	}
	version := strings.TrimSpace(header.Get("X-Elastic-Product"))
	if providerID == settings.ProviderElasticsearch {
		var envelope struct {
			Version struct {
				Number string `json:"number"`
			} `json:"version"`
		}
		if json.Unmarshal(body, &envelope) == nil && strings.TrimSpace(envelope.Version.Number) != "" {
			version = envelope.Version.Number
		}
	}
	return telemetry.ProviderCatalog{Source: telemetry.ProviderSource{
		Provider: string(providerID), Identity: safeProviderIdentity(access.Configuration.Endpoint),
		ServerVersion: boundedString(version, 128), CollectedAt: p.now().UTC(),
	}}, nil
}

func (p *Provider) QueryLogs(ctx context.Context, request telemetry.ProviderLogRequest) (telemetry.ProviderLogResult, error) {
	revision, access, err := p.authorize(ctx, request.ConfigurationRevision, settings.ProviderElasticsearch)
	if err != nil {
		return telemetry.ProviderLogResult{}, err
	}
	defer access.Clear()
	if err := telemetry.ValidateProviderLogRequest(request, revision); err != nil {
		return telemetry.ProviderLogResult{}, err
	}
	var query json.RawMessage
	if err := json.Unmarshal([]byte(request.Query), &query); err != nil {
		return telemetry.ProviderLogResult{}, telemetry.ErrInvalid
	}
	order := "asc"
	if request.Tail {
		order = "desc"
	}
	body, err := json.Marshal(map[string]any{
		"size":             request.Bounds.MaxResults + 1,
		"track_total_hits": true,
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			map[string]any{"range": map[string]any{"@timestamp": map[string]any{
				"gte":    request.TimeRange.From.UTC().Format(time.RFC3339Nano),
				"lte":    request.TimeRange.To.UTC().Format(time.RFC3339Nano),
				"format": "strict_date_optional_time_nanos",
			}}},
			query,
		}}},
		"sort": []any{
			map[string]any{"@timestamp": map[string]any{"order": order, "unmapped_type": "date"}},
			map[string]any{"_shard_doc": map[string]any{"order": order}},
		},
		"_source": []string{
			"@timestamp", "message", "msg", "level", "log.level", "service", "service.name",
			"trace_id", "trace.id", "span_id", "span.id", "scenario_id", "cloudops.cluster_id",
			"reason", "error.reason", "cloudops.reason",
			"kubernetes.namespace", "kubernetes.namespace_name", "kubernetes.deployment.name",
			"kubernetes.statefulset.name", "kubernetes.daemonset.name", "kubernetes.pod.name",
		},
	})
	if err != nil || len(body) > telemetry.MaximumQueryBytes*2 {
		return telemetry.ProviderLogResult{}, telemetry.ErrBoundExceeded
	}
	content, header, err := p.read(ctx, access, http.MethodPost, "/"+elasticsearchIndex+"/_search", nil, body, request.Bounds.MaxResponseBytes)
	if err != nil {
		return telemetry.ProviderLogResult{}, err
	}
	result, err := parseElasticsearch(content, request)
	if err != nil {
		return telemetry.ProviderLogResult{}, err
	}
	result.Source = telemetry.ProviderSource{
		Provider: "elasticsearch", Identity: safeProviderIdentity(access.Configuration.Endpoint) + "/" + elasticsearchIndex,
		ServerVersion: boundedString(header.Get("X-Elastic-Product"), 128), CollectedAt: p.now().UTC(),
	}
	return result, nil
}

func (p *Provider) SearchTraces(ctx context.Context, request telemetry.ProviderTraceSearchRequest) (telemetry.ProviderTraceSearchResult, error) {
	revision, access, err := p.authorize(ctx, request.ConfigurationRevision, settings.ProviderTempo)
	if err != nil {
		return telemetry.ProviderTraceSearchResult{}, err
	}
	defer access.Clear()
	if err := telemetry.ValidateProviderTraceSearchRequest(request, revision); err != nil {
		return telemetry.ProviderTraceSearchResult{}, err
	}
	values := url.Values{
		"q": {request.Query}, "start": {strconv.FormatInt(request.TimeRange.From.UTC().Unix(), 10)},
		"end":   {strconv.FormatInt(request.TimeRange.To.UTC().Unix(), 10)},
		"limit": {strconv.Itoa(request.Bounds.MaxResults + 1)},
	}
	content, _, err := p.read(ctx, access, http.MethodGet, "/api/search", values, nil, request.Bounds.MaxResponseBytes)
	if err != nil {
		return telemetry.ProviderTraceSearchResult{}, err
	}
	result, err := parseTempoSearch(content, request)
	if err != nil {
		return telemetry.ProviderTraceSearchResult{}, err
	}
	result.Source = telemetry.ProviderSource{
		Provider: "tempo", Identity: safeProviderIdentity(access.Configuration.Endpoint), CollectedAt: p.now().UTC(),
	}
	return result, nil
}

func (p *Provider) Trace(ctx context.Context, request telemetry.ProviderTraceDetailRequest) (telemetry.ProviderTraceDetailResult, error) {
	revision, access, err := p.authorize(ctx, request.ConfigurationRevision, settings.ProviderTempo)
	if err != nil {
		return telemetry.ProviderTraceDetailResult{}, err
	}
	defer access.Clear()
	if err := telemetry.ValidateProviderTraceDetailRequest(request, revision); err != nil {
		return telemetry.ProviderTraceDetailResult{}, err
	}
	content, _, err := p.read(ctx, access, http.MethodGet, "/api/traces/"+request.TraceID, nil, nil, request.Bounds.MaxResponseBytes)
	if err != nil {
		return telemetry.ProviderTraceDetailResult{}, err
	}
	result, err := parseTempoTrace(content, request)
	if err != nil {
		return telemetry.ProviderTraceDetailResult{}, err
	}
	result.Source = telemetry.ProviderSource{
		Provider: "tempo", Identity: safeProviderIdentity(access.Configuration.Endpoint) + "/api/traces/" + request.TraceID,
		CollectedAt: p.now().UTC(),
	}
	return result, nil
}

func (p *Provider) authorize(ctx context.Context, revisionID string, provider settings.Provider) (settings.Revision, settings.ProviderAccess, error) {
	revision, err := p.settings.Revision(ctx, revisionID)
	if err != nil {
		return settings.Revision{}, settings.ProviderAccess{}, telemetry.ErrUnavailable
	}
	access, err := p.settings.ProviderAccess(ctx, revisionID, provider)
	if err != nil {
		return settings.Revision{}, settings.ProviderAccess{}, telemetry.ErrUnavailable
	}
	if !access.Configuration.Enabled {
		access.Clear()
		return settings.Revision{}, settings.ProviderAccess{}, telemetry.ErrProviderDisabled
	}
	return revision, access, nil
}

func (p *Provider) read(ctx context.Context, access settings.ProviderAccess, method, path string, values url.Values, body []byte, maxBytes int) ([]byte, http.Header, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(access.Configuration.Endpoint), "/"))
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		(base.Scheme != "http" && base.Scheme != "https") {
		return nil, nil, telemetry.ErrUnavailable
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	if values != nil {
		endpoint.RawQuery = values.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, telemetry.ErrInvalid
	}
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if len(access.Credential) > 0 {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(access.Credential)))
	}
	timeout := time.Duration(access.Configuration.TimeoutMS) * time.Millisecond
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("telemetry Provider redirects are disabled")
	}}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, telemetry.ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if maxBytes < 1 || maxBytes > telemetry.MaximumResponseBytes {
		return nil, nil, telemetry.ErrBoundExceeded
	}
	content, readErr := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if readErr != nil {
		return nil, nil, telemetry.ErrUnavailable
	}
	if len(content) > maxBytes {
		return nil, nil, telemetry.ErrBoundExceeded
	}
	if response.StatusCode == http.StatusNotFound && strings.HasPrefix(path, "/api/traces/") {
		return nil, nil, telemetry.ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, telemetry.ErrUnavailable
	}
	return content, response.Header.Clone(), nil
}

func parseElasticsearch(content []byte, request telemetry.ProviderLogRequest) (telemetry.ProviderLogResult, error) {
	var envelope struct {
		TimedOut bool `json:"timed_out"`
		Shards   struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Items []struct {
				ID     string         `json:"_id"`
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return telemetry.ProviderLogResult{}, telemetry.ErrUnavailable
	}
	truncated := envelope.Hits.Total.Value > request.Bounds.MaxResults || len(envelope.Hits.Items) > request.Bounds.MaxResults
	if len(envelope.Hits.Items) > request.Bounds.MaxResults {
		envelope.Hits.Items = envelope.Hits.Items[:request.Bounds.MaxResults]
	}
	entries := make([]telemetry.LogEntry, 0, len(envelope.Hits.Items))
	fieldSet := map[string]struct{}{}
	for _, hit := range envelope.Hits.Items {
		timestamp, ok := parseTimestamp(firstValue(hit.Source, "@timestamp", "ts", "timestamp"))
		if !ok || timestamp.Before(request.TimeRange.From) || timestamp.After(request.TimeRange.To) {
			continue
		}
		message := boundedString(stringValue(firstValue(hit.Source, "message", "msg")), 4096)
		message = sensitiveValue.ReplaceAllString(message, "$1=[REDACTED]")
		level := strings.ToLower(boundedString(stringValue(firstValue(hit.Source, "log.level", "level")), 32))
		traceID := canonicalHex(stringValue(firstValue(hit.Source, "trace.id", "trace_id")), 32)
		spanID := canonicalHex(stringValue(firstValue(hit.Source, "span.id", "span_id")), 16)
		service := boundedString(stringValue(firstValue(hit.Source, "service.name", "service")), 256)
		attributes := map[string]string{}
		for _, field := range []string{"scenario_id", "reason", "error.reason", "cloudops.reason", "kubernetes.pod.name", "kubernetes.container.name", "kubernetes.node.name", "logger", "caller"} {
			if value := boundedString(stringValue(firstValue(hit.Source, field)), 1024); value != "" {
				attributes[field] = sensitiveValue.ReplaceAllString(value, "$1=[REDACTED]")
				fieldSet[field] = struct{}{}
			}
		}
		for field, value := range map[string]string{"level": level, "service.name": service, "trace_id": traceID, "span_id": spanID} {
			if value != "" {
				fieldSet[field] = struct{}{}
			}
		}
		identity := hit.ID + "\x00" + timestamp.Format(time.RFC3339Nano) + "\x00" + message
		entries = append(entries, telemetry.LogEntry{
			ID: hashText(identity), Timestamp: timestamp, Level: level, Message: message, Service: service,
			TraceID: traceID, SpanID: spanID, Resource: request.Resource, Attributes: attributes, Links: []telemetry.ContextLink{},
		})
	}
	fields := make([]string, 0, len(fieldSet)+2)
	fields = append(fields, "@timestamp", "message")
	for field := range fieldSet {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return telemetry.ProviderLogResult{
		Histogram: histogram(entries, request.TimeRange), Entries: entries, Fields: fields,
		ResponseBytes: len(content), Partial: envelope.TimedOut || envelope.Shards.Failed > 0, Truncated: truncated,
	}, nil
}

func parseTempoSearch(content []byte, request telemetry.ProviderTraceSearchRequest) (telemetry.ProviderTraceSearchResult, error) {
	var envelope struct {
		Traces []struct {
			TraceID         string          `json:"traceID"`
			RootServiceName string          `json:"rootServiceName"`
			RootTraceName   string          `json:"rootTraceName"`
			StartUnixNano   json.RawMessage `json:"startTimeUnixNano"`
			DurationMS      float64         `json:"durationMs"`
			SpanSet         struct {
				Spans []json.RawMessage `json:"spans"`
			} `json:"spanSet"`
			SpanSets []struct {
				Spans []json.RawMessage `json:"spans"`
			} `json:"spanSets"`
		} `json:"traces"`
		Metrics struct {
			CompletedJobs int `json:"completedJobs"`
			TotalJobs     int `json:"totalJobs"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return telemetry.ProviderTraceSearchResult{}, telemetry.ErrUnavailable
	}
	truncated := len(envelope.Traces) > request.Bounds.MaxResults
	if truncated {
		envelope.Traces = envelope.Traces[:request.Bounds.MaxResults]
	}
	traces := make([]telemetry.TraceSummary, 0, len(envelope.Traces))
	for _, item := range envelope.Traces {
		traceID := canonicalHex(item.TraceID, 32)
		start, ok := parseUnixNano(item.StartUnixNano)
		if traceID == "" || !ok {
			continue
		}
		spanCount := len(item.SpanSet.Spans)
		for _, set := range item.SpanSets {
			spanCount += len(set.Spans)
		}
		traces = append(traces, telemetry.TraceSummary{
			TraceID: traceID, RootService: boundedString(item.RootServiceName, 256),
			RootOperation: boundedString(item.RootTraceName, 512), StartTime: start,
			DurationMS: item.DurationMS, SpanCount: spanCount, Resource: request.Resource,
			Links: []telemetry.ContextLink{},
		})
	}
	partial := envelope.Metrics.TotalJobs > 0 && envelope.Metrics.CompletedJobs < envelope.Metrics.TotalJobs
	return telemetry.ProviderTraceSearchResult{
		Traces: traces, ResponseBytes: len(content), Partial: partial, Truncated: truncated || partial,
	}, nil
}

type otlpTraceEnvelope struct {
	Batches       []otlpResourceSpans `json:"batches"`
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource struct {
		Attributes []otlpAttribute `json:"attributes"`
	} `json:"resource"`
	ScopeSpans      []otlpScopeSpans `json:"scopeSpans"`
	Instrumentation []otlpScopeSpans `json:"instrumentationLibrarySpans"`
}

type otlpScopeSpans struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpAttribute struct {
	Key   string         `json:"key"`
	Value map[string]any `json:"value"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              any             `json:"kind"`
	StartTimeUnixNano json.RawMessage `json:"startTimeUnixNano"`
	EndTimeUnixNano   json.RawMessage `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes"`
	Status            struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Events []struct {
		Name         string          `json:"name"`
		TimeUnixNano json.RawMessage `json:"timeUnixNano"`
		Attributes   []otlpAttribute `json:"attributes"`
	} `json:"events"`
}

func parseTempoTrace(content []byte, request telemetry.ProviderTraceDetailRequest) (telemetry.ProviderTraceDetailResult, error) {
	var envelope otlpTraceEnvelope
	if err := json.Unmarshal(content, &envelope); err != nil {
		return telemetry.ProviderTraceDetailResult{}, telemetry.ErrUnavailable
	}
	resources := append(envelope.ResourceSpans, envelope.Batches...)
	spans := make([]telemetry.Span, 0)
	traceAttributes := map[string]string{}
	rootService := ""
	for _, resource := range resources {
		resourceAttrs := attributeMap(resource.Resource.Attributes)
		if !resourceMatches(resourceAttrs, request) {
			continue
		}
		for key, value := range resourceAttrs {
			if slices.Contains([]string{"service.name", "service.version", "deployment.environment.name", "k8s.cluster.name", "k8s.namespace.name", "k8s.workload.kind", "k8s.workload.name"}, key) {
				traceAttributes[key] = value
			}
		}
		if rootService == "" {
			rootService = resourceAttrs["service.name"]
		}
		groups := append(resource.ScopeSpans, resource.Instrumentation...)
		for _, group := range groups {
			for _, item := range group.Spans {
				if canonicalHex(item.TraceID, 32) != request.TraceID {
					continue
				}
				start, startOK := parseUnixNano(item.StartTimeUnixNano)
				end, endOK := parseUnixNano(item.EndTimeUnixNano)
				spanID := canonicalHex(item.SpanID, 16)
				if !startOK || !endOK || !end.After(start) || spanID == "" {
					continue
				}
				events := make([]telemetry.SpanEvent, 0, len(item.Events))
				for _, event := range item.Events {
					at, ok := parseUnixNano(event.TimeUnixNano)
					if ok {
						events = append(events, telemetry.SpanEvent{Name: boundedString(event.Name, 256), Timestamp: at, Attributes: boundedAttributes(attributeMap(event.Attributes))})
					}
				}
				spans = append(spans, telemetry.Span{
					SpanID: spanID, ParentSpanID: canonicalHex(item.ParentSpanID, 16), Service: boundedString(resourceAttrs["service.name"], 256),
					Name: boundedString(item.Name, 512), Kind: boundedString(fmt.Sprint(item.Kind), 32), StartTime: start,
					DurationMS: float64(end.Sub(start)) / float64(time.Millisecond), Status: statusText(item.Status.Code),
					Attributes: boundedAttributes(attributeMap(item.Attributes)), Events: events, Resource: request.Resource,
					Links: []telemetry.ContextLink{},
				})
			}
		}
	}
	if len(spans) == 0 {
		return telemetry.ProviderTraceDetailResult{}, telemetry.ErrNotFound
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].StartTime.Equal(spans[j].StartTime) {
			return spans[i].SpanID < spans[j].SpanID
		}
		return spans[i].StartTime.Before(spans[j].StartTime)
	})
	truncated := len(spans) > maximumSpanCount
	if truncated {
		spans = spans[:maximumSpanCount]
	}
	decorateWaterfall(spans)
	start := spans[0].StartTime
	end := spans[0].StartTime.Add(time.Duration(spans[0].DurationMS * float64(time.Millisecond)))
	rootOperation := spans[0].Name
	for _, span := range spans {
		spanEnd := span.StartTime.Add(time.Duration(span.DurationMS * float64(time.Millisecond)))
		if span.StartTime.Before(start) {
			start = span.StartTime
		}
		if spanEnd.After(end) {
			end = spanEnd
		}
		if span.ParentSpanID == "" {
			rootOperation = span.Name
			if span.Service != "" {
				rootService = span.Service
			}
		}
	}
	if start.After(request.TimeRange.To) || end.Before(request.TimeRange.From) {
		return telemetry.ProviderTraceDetailResult{}, telemetry.ErrNotFound
	}
	return telemetry.ProviderTraceDetailResult{
		Detail: telemetry.TraceDetail{
			TraceID: request.TraceID, RootService: rootService, RootOperation: rootOperation,
			StartTime: start, DurationMS: float64(end.Sub(start)) / float64(time.Millisecond),
			Spans: spans, Attributes: traceAttributes, Resource: request.Resource, Links: []telemetry.ContextLink{},
		},
		ResponseBytes: len(content), Truncated: truncated,
	}, nil
}

func resourceMatches(attributes map[string]string, request telemetry.ProviderTraceDetailRequest) bool {
	return attributes["k8s.cluster.name"] == request.Scope.ClusterID &&
		attributes["k8s.namespace.name"] == request.Resource.Namespace &&
		strings.EqualFold(attributes["k8s.workload.kind"], request.Resource.Kind) &&
		attributes["k8s.workload.name"] == request.Resource.Name
}

func decorateWaterfall(spans []telemetry.Span) {
	byID := make(map[string]int, len(spans))
	for index := range spans {
		byID[spans[index].SpanID] = index
	}
	for index := range spans {
		seen := map[string]struct{}{spans[index].SpanID: {}}
		parent := spans[index].ParentSpanID
		for parent != "" && spans[index].Depth < len(spans) {
			if _, repeated := seen[parent]; repeated {
				break
			}
			seen[parent] = struct{}{}
			parentIndex, ok := byID[parent]
			if !ok {
				break
			}
			spans[index].Depth++
			parent = spans[parentIndex].ParentSpanID
		}
	}
	hasChildren := make(map[string]struct{}, len(spans))
	for index := range spans {
		if spans[index].ParentSpanID != "" {
			hasChildren[spans[index].ParentSpanID] = struct{}{}
		}
	}
	latest := -1
	for index := range spans {
		if _, parent := hasChildren[spans[index].SpanID]; parent {
			continue
		}
		if latest == -1 {
			latest = index
			continue
		}
		left := spans[index].StartTime.Add(time.Duration(spans[index].DurationMS * float64(time.Millisecond)))
		right := spans[latest].StartTime.Add(time.Duration(spans[latest].DurationMS * float64(time.Millisecond)))
		if left.After(right) {
			latest = index
		}
	}
	if latest == -1 {
		latest = len(spans) - 1
	}
	for {
		spans[latest].CriticalPath = true
		parentIndex, ok := byID[spans[latest].ParentSpanID]
		if !ok {
			break
		}
		latest = parentIndex
	}
}

func histogram(entries []telemetry.LogEntry, window telemetry.TimeRange) []telemetry.HistogramBucket {
	const bucketCount = 24
	width := window.To.Sub(window.From) / bucketCount
	if width <= 0 {
		return []telemetry.HistogramBucket{}
	}
	result := make([]telemetry.HistogramBucket, bucketCount)
	for index := range result {
		result[index] = telemetry.HistogramBucket{From: window.From.Add(time.Duration(index) * width), To: window.From.Add(time.Duration(index+1) * width)}
	}
	for _, entry := range entries {
		index := int(entry.Timestamp.Sub(window.From) / width)
		if index == bucketCount {
			index--
		}
		if index >= 0 && index < bucketCount {
			result[index].Count++
		}
	}
	return result
}

func attributeMap(values []otlpAttribute) map[string]string {
	result := make(map[string]string, min(len(values), telemetry.MaximumAttributeEntries))
	for _, item := range values {
		if len(result) >= telemetry.MaximumAttributeEntries {
			break
		}
		key := boundedString(strings.TrimSpace(item.Key), 256)
		value := boundedString(anyValue(item.Value), 1024)
		if key != "" && value != "" {
			result[key] = sensitiveValue.ReplaceAllString(value, "$1=[REDACTED]")
		}
	}
	return result
}

func boundedAttributes(values map[string]string) map[string]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, min(len(keys), telemetry.MaximumAttributeEntries))
	for _, key := range keys {
		if len(result) >= telemetry.MaximumAttributeEntries {
			break
		}
		result[boundedString(key, 256)] = boundedString(values[key], 1024)
	}
	return result
}

func anyValue(value map[string]any) string {
	for _, key := range []string{"stringValue", "intValue", "doubleValue", "boolValue", "bytesValue"} {
		if item, ok := value[key]; ok {
			return fmt.Sprint(item)
		}
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func firstValue(root map[string]any, paths ...string) any {
	for _, path := range paths {
		current := any(root)
		matched := true
		for _, part := range strings.Split(path, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				matched = false
				break
			}
			current, ok = object[part]
			if !ok {
				matched = false
				break
			}
		}
		if matched {
			return current
		}
		if direct, ok := root[path]; ok {
			return direct
		}
	}
	return nil
}

func parseTimestamp(value any) (time.Time, bool) {
	text := strings.TrimSpace(stringValue(value))
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseUnixNano(raw json.RawMessage) (time.Time, bool) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, value).UTC(), true
}

func statusText(value any) string {
	text := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	if text == "2" || strings.Contains(text, "ERROR") {
		return "error"
	}
	if text == "1" || strings.Contains(text, "OK") {
		return "ok"
	}
	return "unset"
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func canonicalHex(value string, size int) string {
	value = strings.TrimSpace(value)
	hexValue := strings.ToLower(value)
	if len(hexValue) == size {
		if _, err := hex.DecodeString(hexValue); err == nil {
			return hexValue
		}
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) == size/2 {
			return hex.EncodeToString(decoded)
		}
	}
	return ""
}

func providerName(value string) (settings.Provider, error) {
	switch strings.TrimSpace(value) {
	case "elasticsearch":
		return settings.ProviderElasticsearch, nil
	case "tempo":
		return settings.ProviderTempo, nil
	default:
		return "", telemetry.ErrInvalid
	}
}

func safeProviderIdentity(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return ""
	}
	parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
	return strings.TrimRight(parsed.String(), "/")
}

func boundedString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func hashText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

var _ telemetry.Provider = (*Provider)(nil)
