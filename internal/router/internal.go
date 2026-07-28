package router

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

// NewInternalRouter is the internal listener capability boundary. User Query
// and Command routes must never be registered here.
func NewInternalRouter(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	if deps.Metrics == nil {
		return nil, errors.New("internal router metrics are required")
	}
	if deps.Alertmanager == nil {
		return nil, errors.New("alertmanager ingress handler is required")
	}
	engine := gin.New()
	engine.Use(
		otelgin.Middleware("cloudops-api-internal"),
		middleware.Logging(),
		middleware.Recovery(),
		deps.Metrics.Handler(),
	)
	if err := engine.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	engine.POST("/webhooks/alertmanager", gin.WrapF(deps.Alertmanager.Webhook))
	engine.GET("/livez", gin.WrapF(deps.Alertmanager.Livez))
	engine.GET("/readyz", gin.WrapF(deps.Alertmanager.Readyz))
	engine.GET("/metrics", gin.WrapH(deps.Metrics.HTTPHandler()))
	engine.NoRoute(func(c *gin.Context) {
		c.Header("Content-Type", "text/plain")
		c.Status(http.StatusNotFound)
		_, _ = c.Writer.Write([]byte("404 page not found"))
	})
	return engine, nil
}
