package router

import (
	"github.com/gin-gonic/gin"

	contractapi "github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
)

func registerAPIRoutes(engine *gin.Engine, cfg config.Config, deps Dependencies) {
	handler := contractapi.NewHandler(contractapi.Config{
		Queries: deps.Queries, Commands: deps.Commands, AllowedOrigins: cfg.CORSOrigins,
		Settings: deps.Settings, Notifications: deps.Notifications, Infrastructure: deps.Infrastructure,
		Alerts:         deps.Alerts,
		Monitoring:     deps.Monitoring,
		Telemetry:      deps.Telemetry,
		AgentWorkspace: deps.AgentWorkspace,
		Operations:     deps.Operations,
	})
	contractapi.RegisterRoutes(engine.Group("/api/v1"), handler)
	engine.NoRoute(func(c *gin.Context) {
		if isAPIPath(c.Request.URL.Path) {
			contractapi.WriteRouteNotFound(c)
			return
		}
		c.Header("Content-Type", "text/plain")
		c.Status(404)
		_, _ = c.Writer.Write([]byte("404 page not found"))
	})
}

func isAPIPath(path string) bool {
	return path == "/api/v1" || len(path) > len("/api/v1/") && path[:len("/api/v1/")] == "/api/v1/"
}
