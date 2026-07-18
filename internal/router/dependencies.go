package router

import (
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

type Dependencies struct {
	Metrics     *middleware.Metrics
	CacheClient *rediscache.Client
	Handler     *handler.Handler
	AuthService handler.AuthService
}
