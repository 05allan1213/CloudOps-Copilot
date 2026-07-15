package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"server-web/internal/config"
	"server-web/internal/middleware"

	_ "server-web/docs"
)

func NewRouter(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	router := gin.New()

	globalBodyLimit := limitRequestBody(cfg.GlobalMaxBodyBytes)
	router.Use(
		middleware.CORS(cfg.CORSOrigins),
		otelgin.Middleware("server-web"),
		middleware.Logging(),
		middleware.Recovery(),
		deps.Metrics.Handler(),
		middleware.RateLimit(deps.CacheClient, cfg.RateLimit),
		func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.Method != http.MethodGet {
				globalBodyLimit(c)
			} else {
				c.Next()
			}
		},
	)

	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	registerCoreRoutes(router, cfg, deps)
	registerAdminRoutes(router, cfg, deps)
	registerCopilotRoutes(router, cfg, deps)
	registerK8sRoutes(router, cfg, deps)
	if err := registerStaticRoutes(router, cfg.StaticDir); err != nil {
		return nil, err
	}
	return router, nil
}

func registerCoreRoutes(router *gin.Engine, cfg config.Config, deps Dependencies) {
	handler := deps.Handler
	router.GET("/metrics", gin.WrapH(deps.Metrics.HTTPHandler()))
	router.GET("/healthz", handler.Healthz)
	router.GET("/readyz", handler.Readyz)
	router.GET("/readyz/full", handler.ReadyzFull)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.POST("/api/v1/auth/login", handler.Login)
	router.POST("/api/v1/webhook/alertmanager", limitRequestBody(cfg.AlertmanagerWebhookMaxBodyBytes), handler.AlertmanagerWebhook)
	router.POST("/api/v2/webhook/alertmanager", limitRequestBody(cfg.AlertmanagerWebhookMaxBodyBytes), handler.IncidentAlertmanagerWebhook)

	protected := router.Group("")
	if cfg.AuthEnabled {
		protected.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService))
	}
	protected.GET("/api/v1/auth/me", handler.Me)
	protected.GET("/api/v1/hosts", handler.Hosts)
	protected.GET("/api/v1/hosts/:instance/metrics", handler.HostMetrics)
	protected.GET("/api/v1/dashboard/overview", handler.DashboardOverview)
	protected.GET("/api/v1/alerts/active", handler.ActiveAlerts)
	protected.GET("/api/v1/alerts/events", handler.AlertEvents)
	protected.GET("/api/v1/alert-histories", handler.ListAlertHistories)
	protected.GET("/api/v2/incidents", handler.ListIncidents)
	protected.GET("/api/v2/incidents/:id", handler.GetIncident)
	protected.GET("/api/v2/incidents/:id/signals", handler.ListIncidentSignals)
	protected.GET("/api/v2/incidents/:id/timeline", handler.ListIncidentTimeline)
	protected.GET("/api/v2/incidents/:id/evidence", handler.ListIncidentEvidence)
	protected.GET("/api/v2/incidents/:id/changes", handler.ListIncidentChanges)
	protected.GET("/api/v2/incidents/:id/change-context", handler.GetIncidentChangeContext)
	protected.POST("/api/v2/incidents/:id/agent-runs", handler.CreateAgentRun)
	protected.GET("/api/v2/incidents/:id/agent-runs", handler.ListAgentRuns)
	protected.GET("/api/v2/agent-runs/:id", handler.GetAgentRun)
	protected.GET("/api/v2/agent-runs/:id/steps", handler.ListAgentSteps)
	protected.GET("/api/v2/agent-runs/:id/evidence", handler.ListAgentEvidence)
	protected.POST("/api/v2/agent-runs/:id/cancel", handler.CancelAgentRun)

	wsGroup := router.Group("")
	if cfg.AuthEnabled {
		wsGroup.Use(middleware.AuthWebSocket(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService))
	}
	wsGroup.GET("/ws/alerts", handler.AlertsWebSocket)
}

func registerAdminRoutes(router *gin.Engine, cfg config.Config, deps Dependencies) {
	handler := deps.Handler
	protected := router.Group("")
	if cfg.AuthEnabled {
		protected.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService))
	}
	protected.GET("/api/v1/host-groups", handler.ListHostGroups)
	protected.GET("/api/v1/host-groups/:id", handler.GetHostGroup)
	protected.GET("/api/v1/alert-rules", handler.ListAlertRules)
	protected.GET("/api/v1/alert-rules/:id", handler.GetAlertRule)
	protected.GET("/api/v1/channels", handler.ListNotificationChannels)
	protected.GET("/api/v1/channels/:id", handler.GetNotificationChannel)

	admin := router.Group("/api/v1")
	if cfg.AuthEnabled {
		admin.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService), middleware.RequireRole("admin"))
	}
	admin.POST("/host-groups", handler.CreateHostGroup)
	admin.PUT("/host-groups/:id", handler.UpdateHostGroup)
	admin.DELETE("/host-groups/:id", handler.DeleteHostGroup)
	admin.POST("/host-groups/:id/members", handler.AddHostGroupMember)
	admin.DELETE("/host-groups/:id/members", handler.DeleteHostGroupMember)
	admin.POST("/alert-rules", handler.CreateAlertRule)
	admin.POST("/alert-rules/sync", handler.SyncAlertRules)
	admin.PUT("/alert-rules/:id", handler.UpdateAlertRule)
	admin.DELETE("/alert-rules/:id", handler.DeleteAlertRule)
	admin.POST("/channels", handler.CreateNotificationChannel)
	admin.PUT("/channels/:id", handler.UpdateNotificationChannel)
	admin.DELETE("/channels/:id", handler.DeleteNotificationChannel)
	admin.POST("/channels/:id/test", handler.TestNotificationChannel)
	admin.POST("/auth/register", handler.Register)
	admin.GET("/users", handler.ListUsers)
	admin.DELETE("/users/:id", handler.DeleteUser)

	remediationAdmin := router.Group("/api/v2")
	if cfg.AuthEnabled {
		remediationAdmin.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService), middleware.RequireRole("admin"))
	}
	remediationAdmin.GET("/remediations", handler.ListRemediations)
	remediationAdmin.GET("/remediations/:id", handler.GetRemediation)
	remediationAdmin.POST("/remediations/:id/approve", handler.ApproveRemediation)
	remediationAdmin.POST("/remediations/:id/reject", handler.RejectRemediation)
}

func registerCopilotRoutes(router *gin.Engine, cfg config.Config, deps Dependencies) {
	if !cfg.CopilotEnabled || deps.Copilot == nil {
		return
	}
	protected := router.Group("")
	if cfg.AuthEnabled {
		protected.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService))
	}
	protected.GET("/api/v1/copilot/tools", deps.Copilot.Handler.ListTools)
	protected.POST("/api/v1/copilot/chat", deps.Copilot.Handler.Chat)
	protected.GET("/api/v1/copilot/sessions", deps.Copilot.Handler.ListSessions)
	protected.GET("/api/v1/copilot/sessions/:id", deps.Copilot.Handler.GetSession)
	protected.GET("/api/v1/copilot/sessions/:id/messages", deps.Copilot.Handler.ListMessages)
	protected.DELETE("/api/v1/copilot/sessions/:id", deps.Copilot.Handler.DeleteSession)
	protected.POST("/api/v1/diagnosis", deps.Copilot.DiagnosisHandler.Trigger)
	protected.GET("/api/v1/diagnosis", deps.Copilot.DiagnosisHandler.List)
	protected.GET("/api/v1/diagnosis/:id", deps.Copilot.DiagnosisHandler.Get)
	if cfg.FeedbackEnabled && deps.Copilot.FeedbackHandler != nil {
		protected.POST("/api/v1/diagnosis/:id/feedback", deps.Copilot.FeedbackHandler.Submit)
	}
	if cfg.ActionApprovalEnabled && deps.Copilot.ActionHandler != nil {
		registerActionRoutes(router, cfg, deps.AuthService, deps.Copilot.ActionHandler)
	}
}
