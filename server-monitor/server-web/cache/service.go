package cache

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	promclient "server-web/prometheus"
	rediscache "server-web/redis"
)

type Client interface {
	Enabled() bool
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
}

type Service struct {
	client       Client
	hostsTTL     time.Duration
	dashboardTTL time.Duration
}

type Options struct {
	HostsTTL     time.Duration
	DashboardTTL time.Duration
}

type DashboardOverview struct {
	TotalHosts    int     `json:"total_hosts"`
	HealthyHosts  int     `json:"healthy_hosts"`
	DownHosts     int     `json:"down_hosts"`
	ActiveAlerts  int     `json:"active_alerts"`
	AvgCPU        float64 `json:"avg_cpu"`
	AvgMemory     float64 `json:"avg_memory"`
	GeneratedAt   string  `json:"generated_at"`
	AlertDegraded bool    `json:"alert_degraded,omitempty"`
}

func NewService(client Client, options Options) *Service {
	return &Service{
		client:       client,
		hostsTTL:     options.HostsTTL,
		dashboardTTL: options.DashboardTTL,
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.client != nil && s.client.Enabled()
}

func (s *Service) GetHosts(ctx context.Context) ([]promclient.Host, bool) {
	if !s.Enabled() {
		return nil, false
	}

	value, ok := s.client.Get(ctx, rediscache.HostsListKey)
	if !ok {
		return nil, false
	}

	var hosts []promclient.Host
	if err := json.Unmarshal(value, &hosts); err != nil {
		return nil, false
	}

	return hosts, true
}

func (s *Service) CacheHosts(ctx context.Context, hosts []promclient.Host) {
	if !s.Enabled() {
		return
	}

	value, err := json.Marshal(hosts)
	if err != nil {
		zap.L().Error("cache hosts marshal failed", zap.Error(err))
		return
	}

	if err := s.client.Set(ctx, rediscache.HostsListKey, value, s.hostsTTL); err != nil {
		zap.L().Error("cache hosts set failed", zap.Error(err))
	}
}

func (s *Service) GetDashboardOverview(ctx context.Context) (DashboardOverview, bool) {
	if !s.Enabled() {
		return DashboardOverview{}, false
	}

	value, ok := s.client.Get(ctx, rediscache.DashboardOverviewKey)
	if !ok {
		return DashboardOverview{}, false
	}

	var overview DashboardOverview
	if err := json.Unmarshal(value, &overview); err != nil {
		return DashboardOverview{}, false
	}

	return overview, true
}

func (s *Service) CacheDashboardOverview(ctx context.Context, overview DashboardOverview) {
	if !s.Enabled() {
		return
	}

	value, err := json.Marshal(overview)
	if err != nil {
		zap.L().Error("cache dashboard overview marshal failed", zap.Error(err))
		return
	}

	if err := s.client.Set(ctx, rediscache.DashboardOverviewKey, value, s.dashboardTTL); err != nil {
		zap.L().Error("cache dashboard overview set failed", zap.Error(err))
	}
}

func (s *Service) CountActiveAlerts(ctx context.Context) (int, bool) {
	if !s.Enabled() {
		return 0, false
	}

	values, err := s.client.HGetAll(ctx, rediscache.ActiveAlertsKey)
	if err != nil {
		zap.L().Warn("dashboard overview active alerts degraded", zap.Error(err))
		return 0, true
	}

	return len(values), false
}
