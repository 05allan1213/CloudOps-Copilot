package observabilityread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const (
	defaultMaxBytes   = int64(256 * 1024)
	defaultMaxSamples = 1000
	defaultMaxSeries  = 20
	defaultMaxTraces  = 100
)

type Config struct {
	BaseURL             string
	TokenFile           string
	Tenant              string
	Timeout             time.Duration
	MaxResponseBytes    int64
	MaxSamples          int
	MaxSeries           int
	MaxTraces           int
	MaxLookback         time.Duration
	Retries             int
	AllowedServices     map[string]struct{}
	AllowedNamespaces   map[string]struct{}
	AllowedEnvironments map[string]struct{}
	AllowHTTPForTests   bool
	HTTPClient          *http.Client
}

type client struct {
	base         *url.URL
	tokenFile    string
	tenant       string
	http         *http.Client
	maxBytes     int64
	maxSamples   int
	maxSeries    int
	maxTraces    int
	maxLookback  time.Duration
	retries      int
	services     map[string]struct{}
	namespaces   map[string]struct{}
	environments map[string]struct{}
}

type Prometheus struct{ c *client }
type Loki struct{ c *client }
type Tempo struct{ c *client }

func NewPrometheus(cfg Config) (*Prometheus, error) {
	c, err := newClient(cfg)
	return &Prometheus{c: c}, err
}
func NewLoki(cfg Config) (*Loki, error)   { c, err := newClient(cfg); return &Loki{c: c}, err }
func NewTempo(cfg Config) (*Tempo, error) { c, err := newClient(cfg); return &Tempo{c: c}, err }

func newClient(cfg Config) (*client, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && (!cfg.AllowHTTPForTests || base.Scheme != "http")) {
		return nil, fmt.Errorf("invalid fixed observability endpoint")
	}
	if cfg.Timeout < time.Second || cfg.Timeout > time.Minute || cfg.MaxLookback < time.Minute || cfg.MaxLookback > 24*time.Hour || len(cfg.AllowedServices) == 0 || len(cfg.AllowedNamespaces) == 0 || len(cfg.AllowedEnvironments) == 0 || cfg.Retries < 0 || cfg.Retries > 2 {
		return nil, fmt.Errorf("invalid observability bounds")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxBytes
	}
	if cfg.MaxSamples == 0 {
		cfg.MaxSamples = defaultMaxSamples
	}
	if cfg.MaxSeries == 0 {
		cfg.MaxSeries = defaultMaxSeries
	}
	if cfg.MaxTraces == 0 {
		cfg.MaxTraces = defaultMaxTraces
	}
	if cfg.MaxResponseBytes < 1024 || cfg.MaxResponseBytes > 1024*1024 || cfg.MaxSamples < 1 || cfg.MaxSamples > 10000 || cfg.MaxSeries < 1 || cfg.MaxSeries > 100 || cfg.MaxTraces < 1 || cfg.MaxTraces > 1000 {
		return nil, fmt.Errorf("invalid observability limits")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout, Transport: http.DefaultTransport}
	}
	base.Path = strings.TrimRight(base.Path, "/")
	return &client{base: base, tokenFile: cfg.TokenFile, tenant: cfg.Tenant, http: httpClient, maxBytes: cfg.MaxResponseBytes, maxSamples: cfg.MaxSamples, maxSeries: cfg.MaxSeries, maxTraces: cfg.MaxTraces, maxLookback: cfg.MaxLookback, retries: cfg.Retries, services: cfg.AllowedServices, namespaces: cfg.AllowedNamespaces, environments: cfg.AllowedEnvironments}, nil
}

func (p *Prometheus) ObserveMetric(ctx context.Context, q verification.SignalQuery) (verification.SignalResult, error) {
	if err := p.c.authorize(q); err != nil {
		return verification.SignalResult{}, err
	}
	query, err := prometheusTemplate(q)
	if err != nil {
		return verification.SignalResult{}, err
	}
	values := url.Values{"query": {query}, "start": {formatTime(time.Now().UTC().Add(-q.Lookback))}, "end": {formatTime(time.Now().UTC())}, "step": {strconv.FormatInt(max(1, int64(q.Step.Seconds())), 10)}}
	body, status, err := p.c.get(ctx, "prometheus", "/api/v1/query_range", values)
	if err != nil {
		return unavailableResult(status, err)
	}
	obs, err := parsePrometheus(body, p.c.maxSeries, p.c.maxSamples)
	if err != nil {
		return malformedResult(err)
	}
	obs.SourceReference = p.c.safeReference("prometheus")
	obs.QueryValid, obs.SourceHealthy, obs.RetentionCovered = true, true, true
	return verification.SignalResult{Value: obs.Value, SampleCount: obs.SampleCount, SeriesCount: obs.SeriesCount, Observation: obs}, nil
}

func (l *Loki) ObserveLogErrorRate(ctx context.Context, q verification.SignalQuery) (verification.SignalResult, error) {
	if err := l.c.authorize(q); err != nil {
		return verification.SignalResult{}, err
	}
	query, err := lokiTemplate(q)
	if err != nil {
		return verification.SignalResult{}, err
	}
	end := time.Now().UTC()
	values := url.Values{"query": {query}, "start": {strconv.FormatInt(end.Add(-q.Lookback).UnixNano(), 10)}, "end": {strconv.FormatInt(end.UnixNano(), 10)}, "limit": {strconv.Itoa(min(l.c.maxSamples, q.MaxSamples))}}
	body, status, err := l.c.get(ctx, "loki", "/loki/api/v1/query_range", values)
	if err != nil {
		return unavailableResult(status, err)
	}
	obs, err := parseLoki(body, l.c.maxSeries, min(l.c.maxSamples, q.MaxSamples))
	if err != nil {
		return malformedResult(err)
	}
	obs.SourceReference = l.c.safeReference("loki")
	obs.QueryValid, obs.SourceHealthy, obs.RetentionCovered = true, true, true
	return verification.SignalResult{Value: obs.Value, SampleCount: obs.SampleCount, SeriesCount: obs.SeriesCount, Observation: obs}, nil
}

func (t *Tempo) ObserveTraceErrorRate(ctx context.Context, q verification.SignalQuery) (verification.SignalResult, error) {
	if err := t.c.authorize(q); err != nil {
		return verification.SignalResult{}, err
	}
	query, err := tempoTemplate(q, verification.CheckType(q.Template) == verification.CheckTraceErrorRateBelow)
	if err != nil {
		return verification.SignalResult{}, err
	}
	end := time.Now().UTC()
	values := url.Values{"q": {query}, "start": {strconv.FormatInt(end.Add(-q.Lookback).Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "limit": {strconv.Itoa(t.c.maxTraces)}}
	body, status, err := t.c.get(ctx, "tempo", "/api/search", values)
	if err != nil {
		return unavailableResult(status, err)
	}
	obs, err := parseTempo(body, t.c.maxTraces, q.Template)
	if err != nil {
		return malformedResult(err)
	}
	if verification.CheckType(q.Template) == verification.CheckTraceErrorRateBelow && (obs.Status == verification.ObservationAvailable || obs.Status == verification.ObservationNoData) {
		allQuery, buildErr := tempoTemplate(q, false)
		if buildErr != nil {
			return malformedResult(buildErr)
		}
		values.Set("q", allQuery)
		allBody, allStatus, readErr := t.c.get(ctx, "tempo", "/api/search", values)
		if readErr != nil {
			return unavailableResult(allStatus, readErr)
		}
		allObs, parseErr := parseTempo(allBody, t.c.maxTraces, string(verification.CheckTraceErrorRateBelow))
		if parseErr != nil {
			return malformedResult(parseErr)
		}
		if allObs.Status != verification.ObservationAvailable || allObs.SampleCount == 0 {
			obs = verification.Observation{Status: verification.ObservationNoData}
		} else {
			errorCount := obs.SampleCount
			obs = verification.Observation{Status: verification.ObservationAvailable, SampleCount: allObs.SampleCount, MatchedCount: errorCount, Numerator: float64(errorCount), Denominator: float64(allObs.SampleCount), Value: float64(errorCount) / float64(allObs.SampleCount), SampledAt: allObs.SampledAt}
		}
	}
	obs.SourceReference = t.c.safeReference("tempo")
	obs.QueryValid, obs.SourceHealthy, obs.RetentionCovered = true, true, true
	return verification.SignalResult{Value: obs.Value, SampleCount: obs.SampleCount, Observation: obs}, nil
}

func (c *client) authorize(q verification.SignalQuery) error {
	if _, ok := c.services[q.Service]; !ok {
		return verification.ErrNotAllowed
	}
	if _, ok := c.namespaces[q.Namespace]; !ok {
		return verification.ErrNotAllowed
	}
	if _, ok := c.environments[q.Environment]; !ok {
		return verification.ErrNotAllowed
	}
	if q.Lookback < time.Minute || q.Lookback > c.maxLookback || q.Step < time.Second || q.MaxSeries < 1 || q.MaxSeries > c.maxSeries || q.MaxSamples < 1 || q.MaxSamples > c.maxSamples {
		return verification.ErrInvalidArgument
	}
	return nil
}

func (c *client) get(ctx context.Context, provider, path string, values url.Values) ([]byte, int, error) {
	ctx, span := otel.Tracer("server-web/observabilityread").Start(ctx, provider+".observation")
	span.SetAttributes(attribute.String("provider", provider), attribute.String("operation", "query"))
	defer span.End()
	u := *c.base
	u.Path += path
	u.RawQuery = values.Encode()
	var lastStatus int
	for attempt := 0; attempt <= c.retries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, 0, verification.ErrInvalidArgument
		}
		req.Header.Set("Accept", "application/json")
		if c.tenant != "" {
			req.Header.Set("X-Scope-OrgID", c.tenant)
		}
		if c.tokenFile != "" {
			token, readErr := os.ReadFile(c.tokenFile)
			if readErr != nil || len(token) > 16*1024 {
				return nil, 0, verification.ErrUnavailable
			}
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			if attempt < c.retries {
				continue
			}
			return nil, 0, verification.ErrUnavailable
		}
		lastStatus = resp.StatusCode
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			return nil, lastStatus, verification.ErrUnavailable
		}
		if int64(len(data)) > c.maxBytes {
			return nil, lastStatus, fmt.Errorf("response_limit")
		}
		if resp.StatusCode == http.StatusOK {
			return data, lastStatus, nil
		}
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < c.retries {
			continue
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, lastStatus, verification.ErrNotAllowed
		case http.StatusNotFound:
			return nil, lastStatus, verification.ErrNotFound
		default:
			return nil, lastStatus, verification.ErrUnavailable
		}
	}
	return nil, lastStatus, verification.ErrUnavailable
}

func prometheusTemplate(q verification.SignalQuery) (string, error) {
	labels := `service=` + quoteLabel(q.Service) + `,namespace=` + quoteLabel(q.Namespace) + `,environment=` + quoteLabel(q.Environment)
	switch verification.CheckType(q.Template) {
	case verification.CheckMetricErrorRateBelow:
		return `sum(rate(http_requests_total{` + labels + `,status=~"5.."}[5m])) / clamp_min(sum(rate(http_requests_total{` + labels + `}[5m])), 1)`, nil
	case verification.CheckMetricAvailabilityAbove:
		return `sum(rate(http_requests_total{` + labels + `,status!~"5.."}[5m])) / clamp_min(sum(rate(http_requests_total{` + labels + `}[5m])), 1)`, nil
	case verification.CheckMetricLatencyP95Below:
		return `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{` + labels + `}[5m])))`, nil
	default:
		return "", verification.ErrInvalidArgument
	}
}

func lokiTemplate(q verification.SignalQuery) (string, error) {
	selector := `{service_name=` + quoteLabel(q.Service) + `,namespace=` + quoteLabel(q.Namespace) + `,environment=` + quoteLabel(q.Environment) + `}`
	switch verification.CheckType(q.Template) {
	case verification.CheckLogErrorAbsent, verification.CheckLogErrorRateBelow:
		return `sum(count_over_time(` + selector + ` |~ "(?i)(error|fatal)"[5m]))`, nil
	default:
		return "", verification.ErrInvalidArgument
	}
}

func tempoTemplate(q verification.SignalQuery, errorOnly bool) (string, error) {
	service := escapeTrace(q.Service)
	environment := escapeTrace(q.Environment)
	switch verification.CheckType(q.Template) {
	case verification.CheckTraceErrorRateBelow:
		if errorOnly {
			return `{ resource.service.name = "` + service + `" && resource.deployment.environment = "` + environment + `" && status = error }`, nil
		}
		return `{ resource.service.name = "` + service + `" && resource.deployment.environment = "` + environment + `" }`, nil
	case verification.CheckTraceLatencyP95Below:
		return `{ resource.service.name = "` + service + `" && resource.deployment.environment = "` + environment + `" }`, nil
	default:
		return "", verification.ErrInvalidArgument
	}
}

func quoteLabel(v string) string { return strconv.Quote(v) }
func escapeTrace(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "").Replace(v)
}
func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', 3, 64)
}

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][]json.RawMessage `json:"values"`
			Value  []json.RawMessage   `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func parsePrometheus(data []byte, maxSeries, maxSamples int) (verification.Observation, error) {
	var payload prometheusResponse
	if json.Unmarshal(data, &payload) != nil || payload.Status != "success" || (payload.Data.ResultType != "matrix" && payload.Data.ResultType != "vector") {
		return verification.Observation{}, errors.New("malformed")
	}
	if len(payload.Data.Result) == 0 {
		return verification.Observation{Status: verification.ObservationNoData}, nil
	}
	if len(payload.Data.Result) > maxSeries {
		return verification.Observation{}, errors.New("series_limit")
	}
	value, count, latest := 0.0, 0, time.Time{}
	for _, series := range payload.Data.Result {
		points := series.Values
		if len(points) == 0 && len(series.Value) > 0 {
			points = [][]json.RawMessage{series.Value}
		}
		for _, point := range points {
			if len(point) != 2 || count >= maxSamples {
				return verification.Observation{}, errors.New("sample_limit")
			}
			var ts float64
			var raw string
			if json.Unmarshal(point[0], &ts) != nil || json.Unmarshal(point[1], &raw) != nil {
				return verification.Observation{}, errors.New("malformed")
			}
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return verification.Observation{}, errors.New("non_finite")
			}
			value += parsed
			count++
			candidate := time.Unix(0, int64(ts*1e9)).UTC()
			if candidate.After(latest) {
				latest = candidate
			}
		}
	}
	if count == 0 {
		return verification.Observation{Status: verification.ObservationNoData}, nil
	}
	return verification.Observation{Status: verification.ObservationAvailable, Value: value / float64(count), SampleCount: count, SeriesCount: len(payload.Data.Result), SampledAt: latest}, nil
}

func parseLoki(data []byte, maxSeries, maxSamples int) (verification.Observation, error) {
	obs, err := parsePrometheus(data, maxSeries, maxSamples)
	if err != nil {
		return obs, err
	}
	if obs.Status == verification.ObservationAvailable {
		obs.MatchedCount = int(math.Round(obs.Value))
	}
	return obs, nil
}

type tempoResponse struct {
	Traces []struct {
		StartTimeUnixNano string `json:"startTimeUnixNano"`
		DurationMs        int64  `json:"durationMs"`
		RootServiceName   string `json:"rootServiceName"`
	} `json:"traces"`
}

func parseTempo(data []byte, maxTraces int, template string) (verification.Observation, error) {
	var payload tempoResponse
	if json.Unmarshal(data, &payload) != nil {
		return verification.Observation{}, errors.New("malformed")
	}
	if len(payload.Traces) == 0 {
		return verification.Observation{Status: verification.ObservationNoData}, nil
	}
	if len(payload.Traces) > maxTraces {
		return verification.Observation{}, errors.New("trace_limit")
	}
	values := make([]float64, 0, len(payload.Traces))
	latest := time.Time{}
	for _, trace := range payload.Traces {
		ns, err := strconv.ParseInt(trace.StartTimeUnixNano, 10, 64)
		if err != nil || trace.DurationMs < 0 {
			return verification.Observation{}, errors.New("malformed")
		}
		candidate := time.Unix(0, ns).UTC()
		if candidate.After(latest) {
			latest = candidate
		}
		values = append(values, float64(trace.DurationMs)/1000)
	}
	value := float64(len(values))
	if verification.CheckType(template) == verification.CheckTraceLatencyP95Below {
		for i := 0; i < len(values); i++ {
			for j := i + 1; j < len(values); j++ {
				if values[j] < values[i] {
					values[i], values[j] = values[j], values[i]
				}
			}
		}
		value = values[int(math.Ceil(.95*float64(len(values))))-1]
	}
	return verification.Observation{Status: verification.ObservationAvailable, Value: value, SampleCount: len(values), MatchedCount: len(values), SampledAt: latest}, nil
}

func unavailableResult(status int, err error) (verification.SignalResult, error) {
	reason := "provider_unavailable"
	if status == http.StatusUnauthorized {
		reason = "provider_unauthorized"
	} else if status == http.StatusForbidden {
		reason = "provider_forbidden"
	} else if status == http.StatusNotFound {
		reason = "provider_not_found"
	} else if status == http.StatusTooManyRequests {
		reason = "provider_rate_limited"
	} else if status >= 500 {
		reason = "provider_server_error"
	}
	return verification.SignalResult{Observation: verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: reason}}, err
}
func malformedResult(err error) (verification.SignalResult, error) {
	return verification.SignalResult{Observation: verification.Observation{Status: verification.ObservationMalformed, ReasonCode: "malformed_response"}}, err
}
func (c *client) safeReference(provider string) string { return provider + "://" + c.base.Host }

var secretPattern = regexp.MustCompile(`(?i)(bearer|authorization|password|token|secret|cookie|dsn)[^ ]*`)

func Redact(value string) string { return secretPattern.ReplaceAllString(value, "[REDACTED]") }
