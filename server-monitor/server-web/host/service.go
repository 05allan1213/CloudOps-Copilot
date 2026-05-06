package host

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appcache "server-web/cache"
	promclient "server-web/prometheus"
)

type PrometheusClient interface {
	GetHosts(ctx context.Context) ([]promclient.Host, error)
	QueryRange(ctx context.Context, metric, instance string, params map[string]string, start, end time.Time, step time.Duration) ([]promclient.RangeSeries, error)
}

type Service struct {
	promClient     PrometheusClient
	cacheService   *appcache.Service
	requestTimeout time.Duration
	cacheTimeout   time.Duration
}

type Options struct {
	RequestTimeout time.Duration
	CacheTimeout   time.Duration
}

type ListOptions struct {
	Status         string
	Query          string
	Sort           string
	Risk           string
	GroupFiltered  bool
	GroupInstances map[string]struct{}
}

type MetricsRange struct {
	Duration time.Duration
	Step     time.Duration
}

type MetricQuery struct {
	Name   string
	Metric string
	Params map[string]string
}

type MetricsResponse struct {
	Instance    string                              `json:"instance"`
	Range       string                              `json:"range"`
	StepSeconds int64                               `json:"stepSeconds"`
	Metrics     map[string][]promclient.RangeSeries `json:"metrics"`
}

var validStatuses = map[string]struct{}{
	"up":   {},
	"down": {},
}

var validSorts = map[string]struct{}{
	"instance":    {},
	"cpu_desc":    {},
	"memory_desc": {},
}

var validRisks = map[string]struct{}{
	"high_cpu":    {},
	"high_memory": {},
}

var validMetricsRanges = map[string]MetricsRange{
	"15m": {
		Duration: 15 * time.Minute,
		Step:     15 * time.Second,
	},
	"1h": {
		Duration: time.Hour,
		Step:     time.Minute,
	},
	"6h": {
		Duration: 6 * time.Hour,
		Step:     5 * time.Minute,
	},
	"24h": {
		Duration: 24 * time.Hour,
		Step:     15 * time.Minute,
	},
}

func NewService(promClient PrometheusClient, cacheService *appcache.Service, options Options) *Service {
	return &Service{
		promClient:     promClient,
		cacheService:   cacheService,
		requestTimeout: options.RequestTimeout,
		cacheTimeout:   options.CacheTimeout,
	}
}

func (s *Service) Hosts(ctx context.Context, options ListOptions) ([]promclient.Host, error) {
	if options.GroupFiltered && len(options.GroupInstances) == 0 {
		return []promclient.Host{}, nil
	}

	if cachedHosts, ok := s.cachedHosts(ctx, options); ok {
		return cachedHosts, nil
	}

	hosts, err := s.promClient.GetHosts(ctx)
	if err != nil {
		return nil, err
	}

	if s.cacheService != nil {
		cacheCtx, cacheCancel := context.WithTimeout(context.Background(), s.cacheTimeout)
		defer cacheCancel()
		s.cacheService.CacheHosts(cacheCtx, hosts)
	}

	if options.GroupFiltered {
		hosts = FilterByInstances(hosts, options.GroupInstances)
	}

	return Sort(Filter(hosts, options.Status, options.Query, options.Risk), options.Sort), nil
}

func (s *Service) cachedHosts(ctx context.Context, options ListOptions) ([]promclient.Host, bool) {
	if s.cacheService == nil {
		return nil, false
	}

	cachedHosts, ok := s.cacheService.GetHosts(ctx)
	if !ok {
		return nil, false
	}

	if options.GroupFiltered {
		cachedHosts = FilterByInstances(cachedHosts, options.GroupInstances)
	}
	return Sort(Filter(cachedHosts, options.Status, options.Query, options.Risk), options.Sort), true
}

func (s *Service) Metrics(ctx context.Context, instance string, rangeName string, mountpoint string, now time.Time) (MetricsResponse, bool, error) {
	rangeName, rangeConfig, ok := ParseMetricsRange(rangeName)
	if !ok {
		return MetricsResponse{}, false, nil
	}

	end := now.UTC()
	start := end.Add(-rangeConfig.Duration)
	queries := BuildMetricQueries(mountpoint)

	metrics, err := s.QueryMetrics(ctx, queries, instance, start, end, rangeConfig.Step)
	if err != nil {
		return MetricsResponse{}, true, err
	}

	return MetricsResponse{
		Instance:    instance,
		Range:       rangeName,
		StepSeconds: int64(rangeConfig.Step.Seconds()),
		Metrics:     metrics,
	}, true, nil
}

func (s *Service) QueryMetrics(ctx context.Context, queries []MetricQuery, instance string, start, end time.Time, step time.Duration) (map[string][]promclient.RangeSeries, error) {
	queryCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	metrics := make(map[string][]promclient.RangeSeries, len(queries))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for _, query := range queries {
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(queryCtx, s.requestTimeout)
			defer cancel()

			series, err := s.promClient.QueryRange(ctx, query.Metric, instance, query.Params, start, end, step)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("query host metric %s failed: %w", query.Name, err)
					cancelAll()
				}
				return
			}
			metrics[query.Name] = series
		}()
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	return metrics, nil
}

func ParseStatus(raw string) string {
	if _, ok := validStatuses[raw]; !ok {
		return ""
	}

	return raw
}

func NormalizeQuery(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func ParseSort(raw string) string {
	if _, ok := validSorts[raw]; !ok {
		return "instance"
	}

	return raw
}

func ParseRisk(raw string) string {
	if _, ok := validRisks[raw]; !ok {
		return ""
	}

	return raw
}

func ParseMetricsRange(raw string) (string, MetricsRange, bool) {
	if raw == "" {
		raw = "1h"
	}

	parsed, ok := validMetricsRanges[raw]
	return raw, parsed, ok
}

func BuildMetricQueries(mountpoint string) []MetricQuery {
	mountpoint = strings.TrimSpace(mountpoint)
	diskParams := map[string]string{}
	if mountpoint != "" {
		diskParams["mountpoint"] = mountpoint
	}

	return []MetricQuery{
		{Name: "cpu", Metric: promclient.MetricCPUUsage},
		{Name: "memory", Metric: promclient.MetricMemoryUsage},
		{Name: "disk", Metric: promclient.MetricDiskUsage, Params: diskParams},
		{Name: "network_recv", Metric: promclient.MetricNetworkRecv},
		{Name: "network_sent", Metric: promclient.MetricNetworkSent},
		{Name: "load1", Metric: promclient.MetricLoad1},
		{Name: "process_count", Metric: promclient.MetricProcessCount},
		{Name: "uptime", Metric: promclient.MetricUptime},
	}
}

func FilterByInstances(hosts []promclient.Host, instances map[string]struct{}) []promclient.Host {
	if len(instances) == 0 {
		return []promclient.Host{}
	}

	filtered := make([]promclient.Host, 0, len(hosts))
	for _, host := range hosts {
		if _, ok := instances[host.Instance]; ok {
			filtered = append(filtered, host)
		}
	}
	return filtered
}

func FilterByStatus(hosts []promclient.Host, statusFilter string) []promclient.Host {
	if statusFilter == "" {
		return hosts
	}

	filtered := make([]promclient.Host, 0, len(hosts))
	for _, host := range hosts {
		isUp := host.Status == "up"
		if statusFilter == "up" && isUp {
			filtered = append(filtered, host)
			continue
		}
		if statusFilter == "down" && !isUp {
			filtered = append(filtered, host)
		}
	}

	return filtered
}

func FilterByQuery(hosts []promclient.Host, queryFilter string) []promclient.Host {
	if queryFilter == "" {
		return hosts
	}

	filtered := make([]promclient.Host, 0, len(hosts))
	for _, host := range hosts {
		if strings.Contains(strings.ToLower(host.Instance), queryFilter) {
			filtered = append(filtered, host)
		}
	}

	return filtered
}

func FilterByRisk(hosts []promclient.Host, riskFilter string) []promclient.Host {
	if riskFilter == "" {
		return hosts
	}

	filtered := make([]promclient.Host, 0, len(hosts))
	for _, host := range hosts {
		switch riskFilter {
		case "high_cpu":
			if host.CPU >= 80 {
				filtered = append(filtered, host)
			}
		case "high_memory":
			if host.Memory >= 85 {
				filtered = append(filtered, host)
			}
		}
	}

	return filtered
}

func Filter(hosts []promclient.Host, statusFilter, queryFilter, riskFilter string) []promclient.Host {
	return FilterByRisk(FilterByQuery(FilterByStatus(hosts, statusFilter), queryFilter), riskFilter)
}

func BuildDashboardOverview(hosts []promclient.Host) appcache.DashboardOverview {
	overview := appcache.DashboardOverview{
		TotalHosts: len(hosts),
	}
	if len(hosts) == 0 {
		return overview
	}

	var totalCPU float64
	var totalMemory float64
	var healthyHosts int
	for _, host := range hosts {
		if host.Status == "up" {
			overview.HealthyHosts++
			healthyHosts++
			totalCPU += host.CPU
			totalMemory += host.Memory
		} else {
			overview.DownHosts++
		}
	}

	if healthyHosts > 0 {
		overview.AvgCPU = totalCPU / float64(healthyHosts)
		overview.AvgMemory = totalMemory / float64(healthyHosts)
	}

	return overview
}

func Sort(hosts []promclient.Host, sortBy string) []promclient.Host {
	sorted := append([]promclient.Host(nil), hosts...)

	switch sortBy {
	case "cpu_desc":
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].CPU == sorted[j].CPU {
				return sorted[i].Instance < sorted[j].Instance
			}
			return sorted[i].CPU > sorted[j].CPU
		})
	case "memory_desc":
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].Memory == sorted[j].Memory {
				return sorted[i].Instance < sorted[j].Instance
			}
			return sorted[i].Memory > sorted[j].Memory
		})
	default:
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].Instance < sorted[j].Instance
		})
	}

	return sorted
}
