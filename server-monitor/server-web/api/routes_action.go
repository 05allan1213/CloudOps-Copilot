package api

import (
	"github.com/gin-gonic/gin"

	"server-web/api/handlers"
	"server-web/api/middleware"
	"server-web/config"
	copilotaction "server-web/copilot/action"
)

func registerActionRoutes(router *gin.Engine, cfg config.Config, authService handlers.AuthService, handler *copilotaction.Handler) {
	actionAdmin := router.Group("/api/v1")
	if cfg.AuthEnabled {
		actionAdmin.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
	}
	actionAdmin.POST("/diagnosis/:id/actions", handler.CreateFromDiagnosis)
	actionAdmin.GET("/actions/pending", handler.ListPending)
	actionAdmin.GET("/actions", handler.ListActions)
	actionAdmin.GET("/actions/:id", handler.GetAction)
	actionAdmin.POST("/actions/:id/approve", handler.Approve)
	actionAdmin.POST("/actions/:id/reject", handler.Reject)
	actionAdmin.POST("/actions/:id/execute", handler.Execute)
	actionAdmin.GET("/audit-logs", handler.ListAuditLogs)
	actionAdmin.GET("/audit-logs/:id", handler.GetAuditLog)
}
