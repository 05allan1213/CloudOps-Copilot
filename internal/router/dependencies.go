package router

import (
	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/notification"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type Dependencies struct {
	Metrics        *middleware.Metrics
	CacheClient    *rediscache.Client
	Handler        *handler.Handler
	Queries        api.QueryPort
	Commands       api.CommandPort
	Alertmanager   *alertmanageringress.Handler
	Alerts         *alert.Service
	Settings       *settings.Service
	Notifications  *notification.Repository
	Infrastructure *infrastructure.Service
	Monitoring     *observability.Service
	Telemetry      *telemetry.Service
	AgentWorkspace *agent.WorkspaceRepository
}
