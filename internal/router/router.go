package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	contractapi "github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

func NewRouter(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	router := gin.New()
	bodyLimit := contractapi.LimitRequestBody(cfg.GlobalMaxBodyBytes)
	rateLimit := contractapi.RateLimit(deps.CacheClient, contractapi.RateLimitConfig{
		Enabled: cfg.RateLimit.Enabled, Requests: cfg.RateLimit.Requests,
		Window: cfg.RateLimit.Window, OperationTimeout: cfg.RateLimit.OperationTimeout,
	})
	router.Use(
		contractapi.CORS(cfg.CORSOrigins),
		otelgin.Middleware("cloudops-api"),
		middleware.Logging(),
		contractapi.Recovery(),
		deps.Metrics.Handler(),
		rateLimit,
		func(c *gin.Context) {
			if c.Request.Method == http.MethodGet || !strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.Next()
				return
			}
			bodyLimit(c)
		},
	)

	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	registerHealthRoutes(router, deps)
	registerAPIRoutes(router, cfg, deps)
	if err := registerStaticRoutes(router, cfg.StaticDir); err != nil {
		return nil, err
	}
	return router, nil
}

func registerHealthRoutes(router *gin.Engine, deps Dependencies) {
	handler := deps.Handler
	router.GET("/metrics", gin.WrapH(deps.Metrics.HTTPHandler()))
	router.GET("/livez", handler.Healthz)
	router.GET("/healthz", handler.Healthz)
	router.GET("/readyz", handler.Readyz)
	router.GET("/readyz/full", handler.ReadyzFull)
}
