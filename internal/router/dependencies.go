package router

import (
	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/notification"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type Dependencies struct {
	Metrics        *middleware.Metrics
	CacheClient    *rediscache.Client
	Handler        *handler.Handler
	Queries        api.QueryPort
	Commands       api.CommandPort
	Alertmanager   *alertmanageringress.Handler
	Settings       *settings.Service
	Notifications  *notification.Repository
	Infrastructure *infrastructure.Service
}
