package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	apphandler "github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

func NewRouter(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	router := gin.New()

	legacyBodyLimit := limitRequestBody(cfg.GlobalMaxBodyBytes)
	v3BodyLimit := apiv3.LimitRequestBody(cfg.GlobalMaxBodyBytes)
	legacyRateLimit := middleware.RateLimit(deps.CacheClient, cfg.RateLimit)
	v3RateLimit := apiv3.RateLimit(deps.CacheClient, apiv3.RateLimitConfig{
		Enabled: cfg.RateLimit.Enabled, Requests: cfg.RateLimit.Requests,
		Window: cfg.RateLimit.Window, OperationTimeout: cfg.RateLimit.OperationTimeout,
	})
	router.Use(
		selectV3Middleware(apiv3.CORS(cfg.CORSOrigins), middleware.CORS(cfg.CORSOrigins)),
		otelgin.Middleware("server-web"),
		middleware.Logging(),
		selectV3Middleware(apiv3.Recovery(), middleware.Recovery()),
		deps.Metrics.Handler(),
		selectV3Middleware(v3RateLimit, legacyRateLimit),
		func(c *gin.Context) {
			if c.Request.Method == http.MethodGet || !strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.Next()
				return
			}
			if isV3Path(c.Request.URL.Path) {
				v3BodyLimit(c)
				return
			}
			legacyBodyLimit(c)
		},
	)

	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	registerCoreRoutes(router, cfg, deps)
	registerRemediationRoutes(router, cfg, deps)
	if err := registerV3Routes(router, cfg, deps); err != nil {
		return nil, err
	}
	if err := registerStaticRoutes(router, cfg.StaticDir); err != nil {
		return nil, err
	}
	return router, nil
}

func registerCoreRoutes(router *gin.Engine, cfg config.Config, deps Dependencies) {
	handler := deps.Handler
	router.GET("/metrics", gin.WrapH(deps.Metrics.HTTPHandler()))
	router.GET("/livez", handler.Healthz)
	router.GET("/healthz", handler.Healthz)
	router.GET("/readyz", handler.Readyz)
	router.GET("/readyz/full", handler.ReadyzFull)
	if cfg.V3ProxyAuthEnabled {
		return
	}
	router.POST("/api/v1/auth/login", handler.Login)
	router.POST("/api/v2/webhook/alertmanager", limitRequestBody(cfg.AlertmanagerWebhookMaxBodyBytes), handler.IncidentAlertmanagerWebhook)

	protected := router.Group("")
	if cfg.AuthEnabled {
		protected.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService))
	}
	protected.GET("/api/v1/auth/me", handler.Me)
	protected.GET("/api/v2/incidents", handler.ListIncidents)
	protected.GET("/api/v2/incidents/:id", handler.GetIncident)
	protected.GET("/api/v2/incidents/:id/signals", handler.ListIncidentSignals)
	protected.GET("/api/v2/incidents/:id/timeline", handler.ListIncidentTimeline)
	protected.GET("/api/v2/incidents/:id/evidence", handler.ListIncidentEvidence)
	protected.GET("/api/v2/incidents/:id/changes", handler.ListIncidentChanges)
	protected.GET("/api/v2/incidents/:id/change-context", handler.GetIncidentChangeContext)
	protected.GET("/api/v2/incidents/:id/delivery", handler.GetIncidentDelivery)
	protected.GET("/api/v2/incidents/:id/verifications", handler.ListIncidentVerifications)
	protected.GET("/api/v2/incidents/:id/verifications/:verification_id", handler.GetIncidentVerification)
	protected.GET("/api/v2/incidents/:id/postmortem", handler.GetIncidentPostmortem)
	protected.GET("/api/v2/workbench/incidents", handler.ListWorkbenchIncidents)
	protected.GET("/api/v2/workbench/incidents/:id", handler.GetWorkbenchIncident)
	protected.GET("/api/v2/workbench/incidents/:id/signals", handler.ListWorkbenchSignals)
	protected.GET("/api/v2/workbench/incidents/:id/timeline", handler.ListWorkbenchTimeline)
	protected.GET("/api/v2/workbench/incidents/:id/evidence", handler.ListWorkbenchEvidence)
	protected.GET("/api/v2/workbench/incidents/:id/resources", handler.GetWorkbenchResources)
	protected.GET("/api/v2/workbench/incidents/:id/investigation", handler.GetWorkbenchInvestigation)
	protected.GET("/api/v2/workbench/incidents/:id/remediation", handler.GetWorkbenchRemediation)
	protected.GET("/api/v2/workbench/incidents/:id/delivery", handler.GetWorkbenchDelivery)
	protected.GET("/api/v2/workbench/incidents/:id/verifications", handler.ListWorkbenchVerifications)
	protected.GET("/api/v2/workbench/incidents/:id/verifications/:verification_id", handler.GetWorkbenchVerification)
	protected.GET("/api/v2/workbench/incidents/:id/realtime", handler.WorkbenchRealtime)
	protected.POST("/api/v2/incidents/:id/agent-runs", handler.CreateAgentRun)
	protected.GET("/api/v2/incidents/:id/agent-runs", handler.ListAgentRuns)
	protected.GET("/api/v2/agent-runs/:id", handler.GetAgentRun)
	protected.GET("/api/v2/agent-runs/:id/steps", handler.ListAgentSteps)
	protected.GET("/api/v2/agent-runs/:id/evidence", handler.ListAgentEvidence)
	protected.POST("/api/v2/agent-runs/:id/cancel", handler.CancelAgentRun)
}

func registerRemediationRoutes(router *gin.Engine, cfg config.Config, deps Dependencies) {
	if cfg.V3ProxyAuthEnabled {
		return
	}
	handler := deps.Handler
	remediationAdmin := router.Group("/api/v2")
	if cfg.AuthEnabled {
		remediationAdmin.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService), middleware.RequireRole("admin"))
	}
	remediationAdmin.GET("/remediations", handler.ListRemediations)
	remediationAdmin.GET("/remediations/:id", handler.GetRemediation)
	remediationAdmin.POST("/remediations/:id/approve", handler.ApproveRemediation)
	remediationAdmin.POST("/remediations/:id/reject", handler.RejectRemediation)
	registerDemoRoutes(remediationAdmin, cfg, handler)
}

func registerDemoRoutes(group *gin.RouterGroup, cfg config.Config, h *apphandler.Handler) {
	if !cfg.FastDemoEnabled {
		return
	}
	group.POST("/demo/incidents/:id/plan", h.CreateFastDemoPlan)
	group.POST("/demo/remediations/:id/execute", h.ExecuteFastDemo)
	group.POST("/demo/incidents/:id/verify", h.VerifyFastDemo)
}
