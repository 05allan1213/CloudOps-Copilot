package startup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	commandapp "github.com/05allan1213/CloudOps-Copilot/internal/command"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/alertmanagergateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/infrastructuregateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/monitoringgateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/telemetrygateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/notification"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/operation"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
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
		container.Settings, err = settings.NewService(infra.MySQL.SQLDB(), cfg.DataDir, settings.BootstrapDiagnostics{
			ListenBoundary: cfg.ListenAddr, MySQLDatabase: cfg.MySQLDatabase,
			DataDirectory: cfg.DataDir, WorkerManagementTarget: cfg.WorkerManagementTarget,
			Lifecycle: "make local-* / make scenario-*", ScenarioState: cfg.ScenarioState,
		})
		if err != nil {
			return nil, fmt.Errorf("settings service init failed: %w", err)
		}
		container.Notifications, err = notification.NewRepository(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("notification repository init failed: %w", err)
		}
		container.AgentWorkspace, err = agent.NewWorkspaceRepository(infra.MySQL.SQLDB())
		if err != nil {
			return nil, fmt.Errorf("agent workspace repository init failed: %w", err)
		}
		container.AgentWorkspace.SetRunbookDir(cfg.RunbookDir)
		operationRepository, operationErr := operation.NewRepository(infra.MySQL.SQLDB())
		if operationErr != nil {
			return nil, fmt.Errorf("operation repository init failed: %w", operationErr)
		}
		container.Operations, operationErr = operation.NewWorkspaceService(operationRepository, container.AgentWorkspace)
		if operationErr != nil {
			return nil, fmt.Errorf("operation workspace init failed: %w", operationErr)
		}
		alertmanagerClient, gatewayErr := alertmanagergateway.NewClient(cfg.WorkerManagementTarget, cfg.RequestTimeout+2*time.Second)
		if gatewayErr != nil {
			return nil, fmt.Errorf("alertmanager Provider Gateway client init failed: %w", gatewayErr)
		}
		container.Alerts, err = alertdomain.NewService(infra.MySQL.SQLDB(), alertmanagerClient,
			alertInvestigationStarter{workspace: container.AgentWorkspace})
		if err != nil {
			return nil, fmt.Errorf("alert service init failed: %w", err)
		}
		container.Alertmanager, err = initAlertmanagerIngress(cfg, container.Alerts, runtimeReadiness)
		if err != nil {
			return nil, fmt.Errorf("alertmanager ingress init failed: %w", err)
		}
		gatewayClient, gatewayErr := infrastructuregateway.NewClient(cfg.WorkerManagementTarget, cfg.K8SRequestTimeout)
		if gatewayErr != nil {
			return nil, fmt.Errorf("kubernetes Provider Gateway client init failed: %w", gatewayErr)
		}
		snapshotRepository, repositoryErr := infrastructure.NewMySQLRepository(infra.MySQL.SQLDB())
		if repositoryErr != nil {
			return nil, fmt.Errorf("infrastructure repository init failed: %w", repositoryErr)
		}
		container.Infrastructure, err = infrastructure.NewService(container.Settings, gatewayClient, snapshotRepository, nil)
		if err != nil {
			return nil, fmt.Errorf("infrastructure service init failed: %w", err)
		}
		container.Settings.SetProviderProbe(settings.ProviderKubernetes, container.Infrastructure.ProbeCluster)
		monitoringClient, gatewayErr := monitoringgateway.NewClient(cfg.WorkerManagementTarget, cfg.ObservabilityRequestTimeout+2*time.Second)
		if gatewayErr != nil {
			return nil, fmt.Errorf("prometheus Provider Gateway client init failed: %w", gatewayErr)
		}
		monitoringRepository, repositoryErr := observability.NewRepository(infra.MySQL.SQLDB())
		if repositoryErr != nil {
			return nil, fmt.Errorf("observability repository init failed: %w", repositoryErr)
		}
		container.Monitoring, err = observability.NewService(context.Background(), monitoringRepository, container.Settings, monitoringClient)
		if err != nil {
			return nil, fmt.Errorf("monitoring service init failed: %w", err)
		}
		telemetryClient, gatewayErr := telemetrygateway.NewClient(cfg.WorkerManagementTarget, cfg.ObservabilityRequestTimeout+2*time.Second)
		if gatewayErr != nil {
			return nil, fmt.Errorf("telemetry Provider Gateway client init failed: %w", gatewayErr)
		}
		telemetryRepository, repositoryErr := telemetry.NewRepository(infra.MySQL.SQLDB())
		if repositoryErr != nil {
			return nil, fmt.Errorf("telemetry repository init failed: %w", repositoryErr)
		}
		container.Telemetry, err = telemetry.NewService(telemetryRepository, container.Settings, telemetryClient)
		if err != nil {
			return nil, fmt.Errorf("telemetry service init failed: %w", err)
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

func initAlertmanagerIngress(cfg *config.Config, store alertmanageringress.Store, runtimeReadiness handler.RuntimeReadiness) (*alertmanageringress.Handler, error) {
	if store == nil {
		return nil, errors.New("alert ingress store is required")
	}
	var err error
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
		Store: store, Targets: targets,
		MaxBodyBytes: cfg.AlertmanagerWebhookMaxBodyBytes, RequestTimeout: cfg.RequestTimeout,
		BearerToken: bearerToken, Readiness: runtimeReadiness,
	})
}
