package startup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	commandapp "github.com/05allan1213/CloudOps-Copilot/internal/command"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentv3mysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	appalert "github.com/05allan1213/CloudOps-Copilot/internal/service/alert"
	authpkg "github.com/05allan1213/CloudOps-Copilot/internal/service/auth"
	appincident "github.com/05allan1213/CloudOps-Copilot/internal/service/incident"
)

func InitAPIContainer(cfg *config.Config, infra *di.Infra, runtimeReadiness handler.RuntimeReadiness) (*di.Container, error) {
	return initContainer(cfg, infra, true, runtimeReadiness)
}

func InitWorkerContainer(cfg *config.Config, infra *di.Infra) (*di.Container, error) {
	return initContainer(cfg, infra, false, nil)
}

func initContainer(cfg *config.Config, infra *di.Infra, initializeAuth bool, runtimeReadiness handler.RuntimeReadiness) (*di.Container, error) {
	container := di.NewContainer(infra)

	var authService *authpkg.Service
	if initializeAuth && !cfg.V3ProxyAuthEnabled && infra.DB != nil && len(strings.TrimSpace(cfg.JWTSecret)) >= 32 {
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
	if infra.KafkaProducer != nil {
		infra.KafkaProducer.SetObserver(metrics)
	}
	container.Metrics = metrics

	if infra.DB != nil {
		incidentStore, err := incidentmysql.NewStore(infra.DB)
		if err != nil {
			return nil, fmt.Errorf("incident store init failed: %w", err)
		}
		container.IncidentService, err = appincident.NewService(appincident.Config{
			UnitOfWork: incidentStore, AggregationWindow: cfg.IncidentAggregationWindow, Observer: metrics,
		})
		if err != nil {
			return nil, fmt.Errorf("incident service init failed: %w", err)
		}
	}
	if infra.MySQL != nil && infra.MySQL.SQLDB() != nil {
		var err error
		container.V3Queries, err = apiv3.NewMySQLQueryPort(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("V3 query port init failed: %w", err)
		}
		container.V3Commands, err = commandapp.NewPort(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("V3 command port init failed: %w", err)
		}
		if initializeAuth {
			container.V3Alertmanager, err = initV3AlertmanagerIngress(cfg, infra.MySQL.SQLDB(), runtimeReadiness)
			if err != nil {
				return nil, fmt.Errorf("V3 Alertmanager ingress init failed: %w", err)
			}
		}
	}

	alertService := appalert.NewService(infra.RedisClient, appalert.Options{
		DedupeTTL: cfg.AlertEventDedupeTTL,
		DB:        infra.DB,
		Producer:  infra.KafkaProducer,
	})
	container.AlertService = alertService

	h, err := handler.NewHandler(infra.PromClient, infra.RedisClient, handler.Config{
		ReadyTimeout:     cfg.ReadyTimeout,
		RequestTimeout:   cfg.RequestTimeout,
		IncidentService:  container.IncidentService,
		MySQLClient:      infra.MySQL,
		RuntimeReadiness: runtimeReadiness,
		AuthService:      authService,
	})
	if err != nil {
		return nil, err
	}
	container.Handler = h

	return container, nil
}

func initV3AlertmanagerIngress(cfg *config.Config, db *sql.DB, runtimeReadiness handler.RuntimeReadiness) (*alertmanageringress.Handler, error) {
	incidentStore, err := incidentv3mysql.NewStore(db)
	if err != nil {
		return nil, fmt.Errorf("incident store: %w", err)
	}
	targets, err := alertmanageringress.ParseTargetAllowlist(cfg.SignalTargetAllowlistJSON)
	if err != nil {
		return nil, fmt.Errorf("target allowlist: %w", err)
	}
	bearerToken, err := alertmanageringress.ReadBearerToken(cfg.AlertmanagerWebhookBearerTokenFile)
	if err != nil {
		return nil, fmt.Errorf("bearer: %w", err)
	}
	if cfg.AlertmanagerWebhookRequireBearer && len(bearerToken) == 0 {
		return nil, errors.New("bearer is required but ALERTMANAGER_WEBHOOK_BEARER_TOKEN_FILE is empty")
	}
	return alertmanageringress.NewHandler(alertmanageringress.Config{
		Store: incidentStore, Targets: targets,
		MaxBodyBytes: cfg.AlertmanagerWebhookMaxBodyBytes, RequestTimeout: cfg.RequestTimeout,
		BearerToken: bearerToken, RuntimeReady: runtimeReadiness,
	})
}
