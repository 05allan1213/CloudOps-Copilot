package router

import (
	"server-web/internal/handler"
	rediscache "server-web/internal/infra/redis"
	"server-web/internal/middleware"
)

type Dependencies struct {
	Metrics     *middleware.Metrics
	CacheClient *rediscache.Client
	Handler     *handler.Handler
	AuthService handler.AuthService
}
