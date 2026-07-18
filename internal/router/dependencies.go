package router

import (
	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

type Dependencies struct {
	Metrics        *middleware.Metrics
	CacheClient    *rediscache.Client
	Handler        *handler.Handler
	AuthService    handler.AuthService
	V3Queries      apiv3.QueryPort
	V3Commands     apiv3.CommandPort
	V3Alertmanager *alertmanageringress.Handler
}
