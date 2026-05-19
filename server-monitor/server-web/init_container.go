package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"server-web/internal/config"
	"server-web/internal/di"
	"server-web/internal/handler"
	"server-web/internal/middleware"
	appalert "server-web/internal/service/alert"
	authpkg "server-web/internal/service/auth"
)

func initContainer(cfg *config.Config, infra *di.Infra) (*di.Container, error) {
	container := di.NewContainer(di.Config{
		HostsTTL:       cfg.HostsCacheTTL,
		DashboardTTL:   cfg.DashboardOverviewTTL,
		RequestTimeout: cfg.CopilotToolDefaultTimeout,
		CacheTimeout:   cfg.CacheWriteTimeout,
	}, infra)

	var authService *authpkg.Service
	if infra.DB != nil && len(strings.TrimSpace(cfg.JWTSecret)) >= 32 {
		var err error
		authService, err = authpkg.NewService(infra.DB, cfg.JWTSecret, time.Duration(cfg.JWTExpireHours)*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("auth service init failed: %w", err)
		}
		created, err := authService.EnsureInitialAdmin(context.Background(), cfg.AdminPassword)
		if err != nil {
			return nil, fmt.Errorf("initial admin setup failed: %w", err)
		}
		if created {
			zap.L().Info("initial admin user created", zap.String("username", "admin"))
		}
	}
	container.AuthService = authService

	metrics := middleware.NewMetrics()
	infra.WSHub.SetConnectionObserver(metrics.SetWebSocketConnections)
	if infra.KafkaProducer != nil {
		infra.KafkaProducer.SetObserver(metrics)
	}
	container.Metrics = metrics

	alertService := appalert.NewService(infra.RedisClient, appalert.Options{
		DedupeTTL: cfg.AlertEventDedupeTTL,
		DB:        infra.DB,
		Producer:  infra.KafkaProducer,
	})
	container.AlertService = alertService

	h, err := handler.NewHandler(infra.PromClient, infra.RedisClient, handler.Config{
		ReadyTimeout:   cfg.ReadyTimeout,
		RequestTimeout: cfg.RequestTimeout,
		HostsTTL:       cfg.HostsCacheTTL,
		DashboardTTL:   cfg.DashboardOverviewTTL,
		DedupeTTL:      cfg.AlertEventDedupeTTL,
		CacheTimeout:   cfg.CacheWriteTimeout,
		RuleSync: handler.NewAlertRuleSyncConfig(
			cfg.AlertRuleSyncEnabled,
			cfg.AlertRulesFilePath,
			cfg.PromtoolPath,
			cfg.PrometheusReloadURL,
			cfg.AlertRuleSyncTimeout,
		),
		AlertService:    alertService,
		AlertProducer:   infra.KafkaProducer,
		CacheService:    container.Cache(),
		HostService:     container.Host(),
		MySQLClient:     nil,
		DB:              infra.DB,
		AuthService:     authService,
		K8sAPIEnabled:   cfg.K8SEnabled && cfg.K8SAPIEnabled,
		K8sNodesEnabled: cfg.K8SEnabled && cfg.K8SAPIEnabled && cfg.K8SNodesEnabled,
		CopilotEnabled:  cfg.CopilotEnabled,
	}, infra.WSHub)
	if err != nil {
		return nil, err
	}
	container.Handler = h

	return container, nil
}
