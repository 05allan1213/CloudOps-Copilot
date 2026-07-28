// Package monitoringprometheus implements the Worker-owned bounded Prometheus
// adapter for Monitoring Workspace queries.
package monitoringprometheus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	promclient "github.com/05allan1213/CloudOps-Copilot/internal/infra/prometheus"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const maxBuildInfoBytes = 64 * 1024

type AccessSource interface {
	ProviderAccess(context.Context, string, settings.Provider) (settings.ProviderAccess, error)
}

type Adapter struct {
	access AccessSource
	now    func() time.Time
}

func New(access AccessSource) (*Adapter, error) {
	if access == nil {
		return nil, errors.New("prometheus adapter requires Configuration Revision access")
	}
	return &Adapter{access: access, now: time.Now}, nil
}

func (a *Adapter) Catalog(ctx context.Context, request observability.ProviderCatalogRequest) (observability.ProviderCatalog, error) {
	access, err := a.access.ProviderAccess(ctx, request.ConfigurationRevision, settings.ProviderPrometheus)
	if err != nil {
		return observability.ProviderCatalog{}, mapAccessError(err)
	}
	defer access.Clear()
	if !access.Configuration.Enabled {
		return observability.ProviderCatalog{}, observability.ErrProviderDisabled
	}
	if err := observability.ValidateProviderCatalog(request, access.Revision); err != nil {
		return observability.ProviderCatalog{}, err
	}
	client, err := boundedClient(access, request.Bounds.TimeoutMS)
	if err != nil {
		return observability.ProviderCatalog{}, err
	}
	defer client.Close()
	version, _, err := client.BuildInfo(ctx, maxBuildInfoBytes)
	if err != nil {
		return observability.ProviderCatalog{}, mapClientError(err)
	}
	collectedAt := a.now().UTC()
	result := observability.ProviderCatalog{
		Source: observability.ProviderSource{
			Provider: "prometheus", Identity: access.Configuration.Endpoint,
			ServerVersion: version, CollectedAt: collectedAt,
		},
		MetricNames: []string{},
	}
	limit := minPositive(request.Bounds.MaxSeries, access.Configuration.MaxResults, 1_000)
	names, _, catalogErr := client.MetricNames(ctx, limit, int64(request.Bounds.MaxResponseBytes))
	if catalogErr != nil {
		result.Partial = true
		return result, nil
	}
	result.MetricNames = names
	return result, nil
}

func (a *Adapter) Query(ctx context.Context, request observability.ProviderQueryRequest) (observability.ProviderQueryResult, error) {
	access, err := a.access.ProviderAccess(ctx, request.ConfigurationRevision, settings.ProviderPrometheus)
	if err != nil {
		return observability.ProviderQueryResult{}, mapAccessError(err)
	}
	defer access.Clear()
	if !access.Configuration.Enabled {
		return observability.ProviderQueryResult{}, observability.ErrProviderDisabled
	}
	if err := observability.ValidateProviderQuery(request, access.Revision); err != nil {
		return observability.ProviderQueryResult{}, err
	}
	client, err := boundedClient(access, request.Bounds.TimeoutMS)
	if err != nil {
		return observability.ProviderQueryResult{}, err
	}
	defer client.Close()
	version, _, err := client.BuildInfo(ctx, maxBuildInfoBytes)
	if err != nil {
		return observability.ProviderQueryResult{}, mapClientError(err)
	}
	series, responseBytes, partial, err := client.QueryRangeBounded(
		ctx, request.Query, request.TimeRange.From, request.TimeRange.To,
		time.Duration(request.Bounds.StepSeconds)*time.Second,
		int64(request.Bounds.MaxResponseBytes), request.Bounds.MaxSeries, request.Bounds.MaxSamples,
	)
	if err != nil {
		return observability.ProviderQueryResult{}, mapClientError(err)
	}
	result := observability.ProviderQueryResult{
		Source: observability.ProviderSource{
			Provider: "prometheus", Identity: access.Configuration.Endpoint,
			ServerVersion: version, CollectedAt: a.now().UTC(),
		},
		Result:      observability.QueryResult{ResultType: "matrix", Series: make([]observability.QuerySeries, 0, len(series))},
		SeriesCount: len(series), ResponseBytes: responseBytes, Partial: partial,
	}
	for _, sourceSeries := range series {
		points := make([]observability.QueryPoint, 0, len(sourceSeries.Values))
		for _, point := range sourceSeries.Values {
			points = append(points, observability.QueryPoint{Timestamp: point.Timestamp.UTC(), Value: point.Value})
		}
		result.SampleCount += len(points)
		result.Result.Series = append(result.Result.Series, observability.QuerySeries{
			Labels: cloneLabels(sourceSeries.Metric), Points: points,
		})
	}
	return result, nil
}

func boundedClient(access settings.ProviderAccess, requestedTimeoutMS int) (*promclient.Client, error) {
	timeoutMS := minPositive(requestedTimeoutMS, access.Configuration.TimeoutMS, 30_000)
	if timeoutMS <= 0 {
		return nil, fmt.Errorf("%w: Prometheus timeout is not configured", observability.ErrUnavailable)
	}
	client, err := promclient.NewBoundedClient(access.Configuration.Endpoint, access.Credential, time.Duration(timeoutMS)*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Prometheus Provider endpoint", observability.ErrUnavailable)
	}
	return client, nil
}

func mapAccessError(err error) error {
	if errors.Is(err, settings.ErrNotFound) {
		return observability.ErrUnavailable
	}
	return fmt.Errorf("%w: resolve Prometheus Provider access", observability.ErrUnavailable)
}

func mapClientError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, promclient.ErrResponseTooLarge), errors.Is(err, promclient.ErrResultLimit):
		return observability.ErrBoundExceeded
	case errors.Is(err, promclient.ErrInvalid):
		return fmt.Errorf("%w: Prometheus request is invalid", observability.ErrUnauthorized)
	default:
		return observability.ErrUnavailable
	}
}

func cloneLabels(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "" && len(key) <= 256 && len(value) <= 512 {
			result[key] = value
		}
	}
	return result
}

func minPositive(values ...int) int {
	result := 0
	for _, value := range values {
		if value > 0 && (result == 0 || value < result) {
			result = value
		}
	}
	return result
}

var _ observability.Provider = (*Adapter)(nil)
