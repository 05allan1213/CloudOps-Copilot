package startup

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	commandapp "github.com/05allan1213/CloudOps-Copilot/internal/command"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentstore"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/notification"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func InitAPIContainer(cfg *config.Config, infra *di.Infra, runtimeReadiness handler.RuntimeReadiness) (*di.Container, error) {
	container := di.NewContainer(infra)

	metrics := middleware.NewMetrics()
	container.Metrics = metrics

	if infra.MySQL != nil && infra.MySQL.SQLDB() != nil {
		var err error
		container.Queries, err = api.NewMySQLQueryPort(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("query port init failed: %w", err)
		}
		container.Commands, err = commandapp.NewPort(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("command port init failed: %w", err)
		}
		container.Alertmanager, err = initAlertmanagerIngress(cfg, infra.MySQL.SQLDB(), runtimeReadiness)
		if err != nil {
			return nil, fmt.Errorf("alertmanager ingress init failed: %w", err)
		}
		container.Settings, err = settings.NewService(infra.MySQL.SQLDB(), cfg.DataDir, settings.BootstrapDiagnostics{
			ListenBoundary: cfg.ListenAddr, MySQLDatabase: cfg.MySQLDatabase,
			DataDirectory: cfg.DataDir, WorkerManagementTarget: cfg.WorkerManagementTarget,
			Lifecycle: "make local-*",
		})
		if err != nil {
			return nil, fmt.Errorf("settings service init failed: %w", err)
		}
		container.Notifications, err = notification.NewRepository(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("notification repository init failed: %w", err)
		}
	}

	h, err := handler.NewHandler(handler.Config{
		ReadyTimeout: cfg.ReadyTimeout,
		MySQLClient:  infra.MySQL,
	})
	if err != nil {
		return nil, err
	}
	container.Handler = h

	return container, nil
}

func initAlertmanagerIngress(cfg *config.Config, db *sql.DB, runtimeReadiness handler.RuntimeReadiness) (*alertmanageringress.Handler, error) {
	incidentStore, err := incidentstore.NewStore(db)
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
		BearerToken: bearerToken, Readiness: runtimeReadiness,
	})
}
