package observabilityread

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type MetricKind string

const (
	MetricErrorRate    MetricKind = "error_rate"
	MetricAvailability MetricKind = "availability"
	MetricReadiness    MetricKind = "readiness"
	MetricFiringAlerts MetricKind = "firing_alerts"
)

// MetricQuery contains only trusted dimensions and enum-selected templates.
// It deliberately has no PromQL field.
type MetricQuery struct {
	Kind         MetricKind
	Service      string
	Namespace    string
	Environment  string
	Lookback     time.Duration
	AlertNames   []string
	Cluster      string
	WorkloadName string
}

// ObserveBoundedMetric executes the fixed demonstration Prometheus templates. Error-rate
// and availability observations include the real request denominator so the
// deterministic minimum-sample gate cannot pass on an empty range.
func (p *Prometheus) ObserveBoundedMetric(ctx context.Context, query MetricQuery) (verification.Observation, error) {
	if p == nil || p.c == nil {
		return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_unavailable"}, verification.ErrUnavailable
	}
	signal := verification.SignalQuery{
		Service: query.Service, Namespace: query.Namespace, Environment: query.Environment,
		Lookback: query.Lookback, Step: 10 * time.Second, MaxSeries: 1, MaxSamples: 4,
	}
	if err := p.c.authorize(signal); err != nil {
		return verification.Observation{}, err
	}
	labels := `namespace=` + quoteLabel(query.Namespace) + `,pod=~` + quoteLabel("^"+regexp.QuoteMeta(query.WorkloadName)+"-[a-z0-9]+-[a-z0-9]+$")
	lookback := prometheusDuration(query.Lookback)
	now := time.Now().UTC()
	read := func(expression string) (verification.Observation, error) {
		body, status, err := p.c.get(ctx, "prometheus", "/api/v1/query", url.Values{
			"query": {expression}, "time": {formatTime(now)},
		})
		if err != nil {
			result, readErr := unavailableResult(status, err)
			return result.Observation, readErr
		}
		observation, parseErr := parsePrometheus(body, 1, 4)
		if parseErr != nil {
			result, malformedErr := malformedResult(parseErr)
			return result.Observation, malformedErr
		}
		observation.QueryValid, observation.SourceHealthy, observation.RetentionCovered = true, true, true
		observation.SourceReference = p.c.safeReference("prometheus")
		return observation, nil
	}

	switch query.Kind {
	case MetricReadiness:
		return read(`min(cloudops_demo_workload_ready{` + labels + `})`)
	case MetricErrorRate, MetricAvailability:
		errorsObservation, err := read(`sum(increase(cloudops_demo_http_errors_total{` + labels + `,route="/"}[` + lookback + `])) or vector(0)`)
		if err != nil {
			return errorsObservation, err
		}
		requestsObservation, err := read(`sum(increase(cloudops_demo_http_requests_total{` + labels + `,route="/"}[` + lookback + `]))`)
		if err != nil {
			return requestsObservation, err
		}
		if errorsObservation.Status == verification.ObservationNoData || requestsObservation.Status == verification.ObservationNoData || requestsObservation.Value <= 0 {
			return verification.Observation{
				Status: verification.ObservationNoData, QueryValid: true, SourceHealthy: true,
				RetentionCovered: true, SourceReference: p.c.safeReference("prometheus"),
			}, nil
		}
		errorCount, requestCount := errorsObservation.Value, requestsObservation.Value
		value, numerator := errorCount/requestCount, errorCount
		if query.Kind == MetricAvailability {
			numerator, value = requestCount-errorCount, (requestCount-errorCount)/requestCount
		}
		return verification.Observation{
			Status: verification.ObservationAvailable, Value: value, Numerator: numerator, Denominator: requestCount,
			SampleCount: int(requestCount), SeriesCount: 1, SampledAt: latestTime(errorsObservation.SampledAt, requestsObservation.SampledAt),
			QueryValid: true, SourceHealthy: true, RetentionCovered: true, SourceReference: p.c.safeReference("prometheus"),
		}, nil
	case MetricFiringAlerts:
		if len(query.AlertNames) == 0 || len(query.AlertNames) > 20 || strings.TrimSpace(query.Cluster) == "" || strings.TrimSpace(query.WorkloadName) == "" {
			return verification.Observation{}, verification.ErrInvalidArgument
		}
		names := append([]string(nil), query.AlertNames...)
		sort.Strings(names)
		for index := range names {
			names[index] = regexp.QuoteMeta(strings.TrimSpace(names[index]))
			if names[index] == "" {
				return verification.Observation{}, verification.ErrInvalidArgument
			}
		}
		alertLabels := `alertstate="firing",alertname=~` + quoteLabel("^(?:"+strings.Join(names, "|")+")$") +
			`,cluster=` + quoteLabel(query.Cluster) + `,environment=` + quoteLabel(query.Environment) +
			`,namespace=` + quoteLabel(query.Namespace) + `,deployment=` + quoteLabel(query.WorkloadName)
		observation, err := read(`count(ALERTS{` + alertLabels + `})`)
		if err != nil {
			return observation, err
		}
		if observation.Status == verification.ObservationNoData {
			observation.Status, observation.Value, observation.MatchedCount, observation.SampleCount = verification.ObservationAvailable, 1, 0, 1
			observation.SampledAt = now
			return observation, nil
		}
		observation.MatchedCount, observation.SampleCount = int(observation.Value), 1
		if observation.MatchedCount == 0 {
			observation.Value = 1
		} else {
			observation.Value = 0
		}
		return observation, nil
	default:
		return verification.Observation{}, verification.ErrInvalidArgument
	}
}

func prometheusDuration(value time.Duration) string {
	seconds := int64(value.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10) + "s"
}

func latestTime(values ...time.Time) time.Time {
	var result time.Time
	for _, value := range values {
		if value.After(result) {
			result = value
		}
	}
	return result
}

func (query MetricQuery) String() string {
	return fmt.Sprintf("%s/%s/%s", query.Kind, query.Namespace, query.Service)
}
