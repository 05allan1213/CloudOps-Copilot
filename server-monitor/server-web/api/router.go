package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	appalert "server-web/alert"
	"server-web/api/handlers"
	"server-web/api/middleware"
	appcache "server-web/cache"
	"server-web/config"
	copilotaction "server-web/copilot/action"
	copilotdiagnosis "server-web/copilot/diagnosis"
	copilothandler "server-web/copilot/handler"
	copilotllm "server-web/copilot/llm"
	copilotrunbook "server-web/copilot/runbook"
	copilotservice "server-web/copilot/service"
	copilotsession "server-web/copilot/session"
	copilottool "server-web/copilot/tool"
	"server-web/database"
	apphost "server-web/host"
	eventbus "server-web/kafka"
	promclient "server-web/prometheus"
	rediscache "server-web/redis"
	ws "server-web/websocket"

	_ "server-web/docs"
)

type authService interface {
	handlers.AuthService
}

type CopilotRuntime struct {
	DiagnosisService *copilotdiagnosis.Service
	KafkaObserver    eventbus.ConsumerObserver
}

func NewRouter(cfg config.Config, promClient *promclient.Client, cacheClient *rediscache.Client, mysqlClient *database.MySQL, authService authService, websocketHub *ws.Hub, alertProducer *eventbus.Producer) (*gin.Engine, error) {
	router, _, err := NewRouterWithRuntime(cfg, promClient, cacheClient, mysqlClient, authService, websocketHub, alertProducer)
	return router, err
}

func NewRouterWithRuntime(cfg config.Config, promClient *promclient.Client, cacheClient *rediscache.Client, mysqlClient *database.MySQL, authService authService, websocketHub *ws.Hub, alertProducer *eventbus.Producer) (*gin.Engine, *CopilotRuntime, error) {
	router := gin.New()
	metrics := middleware.NewMetrics()
	if websocketHub != nil {
		websocketHub.SetConnectionObserver(metrics.SetWebSocketConnections)
	}
	if alertProducer != nil {
		alertProducer.SetObserver(metrics)
	}
	router.Use(
		middleware.CORS(cfg.CORSOrigins),
		otelgin.Middleware("server-web"),
		middleware.Logging(),
		middleware.Recovery(),
		metrics.Handler(),
		middleware.RateLimit(cacheClient, cfg.RateLimit),
	)

	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, nil, err
	}

	db := dbFromMySQL(mysqlClient)
	alertService := appalert.NewService(cacheClient, appalert.Options{
		DedupeTTL: cfg.AlertEventDedupeTTL,
		DB:        db,
		Producer:  alertProducer,
	})

	handler, err := handlers.NewHandler(promClient, cacheClient, handlers.Config{
		ReadyTimeout:   cfg.ReadyTimeout,
		RequestTimeout: cfg.RequestTimeout,
		HostsTTL:       cfg.HostsCacheTTL,
		DashboardTTL:   cfg.DashboardOverviewTTL,
		DedupeTTL:      cfg.AlertEventDedupeTTL,
		CacheTimeout:   cfg.CacheWriteTimeout,
		RuleSync: handlers.NewAlertRuleSyncConfig(
			cfg.AlertRuleSyncEnabled,
			cfg.AlertRulesFilePath,
			cfg.PromtoolPath,
			cfg.PrometheusReloadURL,
			cfg.AlertRuleSyncTimeout,
		),
		AlertService:  alertService,
		AlertProducer: alertProducer,
		MySQLClient:   mysqlClient,
		DB:            db,
		AuthService:   authService,
	}, websocketHub)
	if err != nil {
		return nil, nil, err
	}

	router.GET("/metrics", gin.WrapH(metrics.HTTPHandler()))
	router.GET("/healthz", handler.Healthz)
	router.GET("/readyz", handler.Readyz)
	router.GET("/readyz/full", handler.ReadyzFull)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.POST("/api/v1/auth/login", handler.Login)
	router.POST(
		"/api/v1/webhook/alertmanager",
		limitRequestBody(cfg.AlertmanagerWebhookMaxBodyBytes),
		handler.AlertmanagerWebhook,
	)

	protected := router.Group("")
	if cfg.AuthEnabled {
		protected.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService))
	}
	protected.GET("/api/v1/auth/me", handler.Me)
	protected.GET("/api/v1/hosts", handler.Hosts)
	protected.GET("/api/v1/hosts/:instance/metrics", handler.HostMetrics)
	protected.GET("/api/v1/dashboard/overview", handler.DashboardOverview)
	protected.GET("/api/v1/alerts/active", handler.ActiveAlerts)
	protected.GET("/api/v1/alerts/events", handler.AlertEvents)
	protected.GET("/api/v1/alert-histories", handler.ListAlertHistories)

	var copilotRuntime *CopilotRuntime
	if cfg.CopilotEnabled {
		copilotCacheService := appcache.NewService(cacheClient, appcache.Options{
			HostsTTL:     cfg.HostsCacheTTL,
			DashboardTTL: cfg.DashboardOverviewTTL,
		})
		copilotHostService := apphost.NewService(promClient, copilotCacheService, apphost.Options{
			RequestTimeout: cfg.RequestTimeout,
			CacheTimeout:   cfg.CacheWriteTimeout,
		})
		var tools copilotservice.ToolExecutor
		var toolExecutor *copilottool.Executor
		runbookDocs, err := copilotrunbook.LoadDir(context.Background(), cfg.RunbookDir, copilotrunbook.LoadOptions{
			MaxFiles:     cfg.RunbookMaxFiles,
			MaxFileBytes: cfg.RunbookMaxFileBytes,
		})
		if err != nil {
			return nil, nil, err
		}
		runbookRetriever := copilotrunbook.NewRetriever(runbookDocs, copilotrunbook.RetrieverOptions{
			DefaultLimit: cfg.RunbookSearchTopN,
			MaxLimit:     5,
		})
		if cfg.CopilotToolRegistryEnabled {
			toolExecutor, err = copilottool.NewExecutor(copilottool.Options{
				HostService:     copilotHostService,
				AlertService:    alertService,
				PromClient:      promClient,
				RunbookSearcher: runbookRetriever,
				DB:              db,
				Timeout:         cfg.CopilotToolDefaultTimeout,
				LogArgs:         cfg.CopilotToolLogArgs,
			})
			if err != nil {
				return nil, nil, err
			}
			tools = toolExecutor
		} else {
			tools = copilottool.NewDisabledExecutor()
		}
		llmClient := copilotllm.NewClient(copilotllm.Options{
			APIKey:    cfg.LLMAPIKey,
			APIURL:    cfg.LLMAPIURL,
			Model:     cfg.LLMModel,
			Timeout:   cfg.LLMTimeout,
			MaxTokens: cfg.LLMMaxTokens,
		})
		var runner copilotdiagnosis.ToolRunner
		if toolExecutor != nil {
			runner = diagnosisToolRunner{executor: toolExecutor}
		}
		diagnosisService := copilotdiagnosis.NewService(copilotdiagnosis.Config{
			Repository: copilotdiagnosis.NewRepository(db),
			Resolver: copilotdiagnosis.NewResolver(copilotdiagnosis.ResolverOptions{
				DB:           db,
				AlertService: alertService,
				Timeout:      cfg.CopilotToolDefaultTimeout,
			}),
			Collector: copilotdiagnosis.NewEvidenceCollector(copilotdiagnosis.EvidenceOptions{
				Runner:       runner,
				Timeout:      45 * time.Second,
				RunbookLimit: cfg.RunbookSearchTopN,
			}),
			Summarizer: copilotdiagnosis.NewLLMSummarizerWithOptions(llmClient, copilotdiagnosis.LLMSummarizerOptions{
				Timeout: cfg.DiagnosisLLMTimeout,
			}),
		})
		copilotRuntime = &CopilotRuntime{DiagnosisService: diagnosisService, KafkaObserver: metrics}
		copilotHandler := copilothandler.NewHandler(copilotservice.NewService(copilotservice.Config{
			MaxMessageLength:   cfg.CopilotMaxMessageLength,
			SessionTTL:         cfg.CopilotSessionTTL,
			MaxSessionMessages: cfg.CopilotMaxSessionMessages,
			Store:              copilotsession.NewRedisStore(cacheClient),
			LLM:                llmClient,
			Tools:              tools,
			Diagnosis:          diagnosisService,
		}))
		diagnosisHandler := copilotdiagnosis.NewHandler(diagnosisService)
		protected.GET("/api/v1/copilot/tools", copilotHandler.ListTools)
		protected.POST("/api/v1/copilot/chat", copilotHandler.Chat)
		protected.GET("/api/v1/copilot/sessions", copilotHandler.ListSessions)
		protected.GET("/api/v1/copilot/sessions/:id/messages", copilotHandler.ListMessages)
		protected.DELETE("/api/v1/copilot/sessions/:id", copilotHandler.DeleteSession)
		protected.POST("/api/v1/diagnosis", diagnosisHandler.Trigger)
		protected.GET("/api/v1/diagnosis", diagnosisHandler.List)
		protected.GET("/api/v1/diagnosis/:id", diagnosisHandler.Get)
		if cfg.ActionApprovalEnabled {
			actionExecutionEnabled := cfg.ActionExecutionEnabled
			if actionExecutionEnabled {
				zap.L().Warn("ACTION_EXECUTION_ENABLED=true but no action executor is configured; forcing action execution disabled")
				actionExecutionEnabled = false
			}
			actionService := copilotaction.NewService(copilotaction.ServiceConfig{
				Repository:             copilotaction.NewRepository(db),
				Policy:                 copilotaction.NewPolicy(copilotaction.PolicyConfig{MaxReplicas: cfg.ActionMaxReplicas}),
				Executor:               copilotaction.DisabledK8sExecutor{},
				Notifier:               copilotaction.NewWebSocketNotifier(websocketHub),
				OperationEvents:        operationEventProducer{producer: alertProducer},
				Observer:               metrics,
				OperationEventsEnabled: cfg.ActionOperationEventsEnabled,
				StatusPushEnabled:      cfg.ActionStatusPushEnabled,
				ActionExecutionEnabled: actionExecutionEnabled,
			})
			actionHandler := copilotaction.NewHandler(actionService)
			actionAdmin := router.Group("/api/v1")
			if cfg.AuthEnabled {
				actionAdmin.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
			}
			actionAdmin.POST("/diagnosis/:id/actions", actionHandler.CreateFromDiagnosis)
			actionAdmin.GET("/actions/pending", actionHandler.ListPending)
			actionAdmin.GET("/actions", actionHandler.ListActions)
			actionAdmin.GET("/actions/:id", actionHandler.GetAction)
			actionAdmin.POST("/actions/:id/approve", actionHandler.Approve)
			actionAdmin.POST("/actions/:id/reject", actionHandler.Reject)
			actionAdmin.POST("/actions/:id/execute", actionHandler.Execute)
			actionAdmin.GET("/audit-logs", actionHandler.ListAuditLogs)
			actionAdmin.GET("/audit-logs/:id", actionHandler.GetAuditLog)
		}
	}

	wsGroup := router.Group("")
	if cfg.AuthEnabled {
		wsGroup.Use(middleware.AuthWebSocket(authService), middleware.VerifyTokenVersion(authService))
	}
	wsGroup.GET("/ws/alerts", handler.AlertsWebSocket)

	hostGroupsRead := protected.Group("/api/v1/host-groups")
	hostGroupsRead.GET("", handler.ListHostGroups)
	hostGroupsRead.GET("/:id", handler.GetHostGroup)

	alertRulesRead := protected.Group("/api/v1/alert-rules")
	alertRulesRead.GET("", handler.ListAlertRules)
	alertRulesRead.GET("/:id", handler.GetAlertRule)

	channelsRead := protected.Group("/api/v1/channels")
	channelsRead.GET("", handler.ListNotificationChannels)
	channelsRead.GET("/:id", handler.GetNotificationChannel)

	hostGroupsWrite := router.Group("/api/v1/host-groups")
	if cfg.AuthEnabled {
		hostGroupsWrite.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
	}
	hostGroupsWrite.POST("", handler.CreateHostGroup)
	hostGroupsWrite.PUT("/:id", handler.UpdateHostGroup)
	hostGroupsWrite.DELETE("/:id", handler.DeleteHostGroup)
	hostGroupsWrite.POST("/:id/members", handler.AddHostGroupMember)
	hostGroupsWrite.DELETE("/:id/members", handler.DeleteHostGroupMember)

	alertRulesWrite := router.Group("/api/v1/alert-rules")
	if cfg.AuthEnabled {
		alertRulesWrite.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
	}
	alertRulesWrite.POST("", handler.CreateAlertRule)
	alertRulesWrite.POST("/sync", handler.SyncAlertRules)
	alertRulesWrite.PUT("/:id", handler.UpdateAlertRule)
	alertRulesWrite.DELETE("/:id", handler.DeleteAlertRule)

	channelsWrite := router.Group("/api/v1/channels")
	if cfg.AuthEnabled {
		channelsWrite.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
	}
	channelsWrite.POST("", handler.CreateNotificationChannel)
	channelsWrite.PUT("/:id", handler.UpdateNotificationChannel)
	channelsWrite.DELETE("/:id", handler.DeleteNotificationChannel)
	channelsWrite.POST("/:id/test", handler.TestNotificationChannel)

	authWrite := router.Group("/api/v1/auth")
	if cfg.AuthEnabled {
		authWrite.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
	}
	authWrite.POST("/register", handler.Register)

	usersWrite := router.Group("/api/v1/users")
	if cfg.AuthEnabled {
		usersWrite.Use(middleware.Auth(authService), middleware.VerifyTokenVersion(authService), middleware.RequireRole("admin"))
	}
	usersWrite.GET("", handler.ListUsers)
	usersWrite.DELETE("/:id", handler.DeleteUser)

	staticDir := cfg.StaticDir
	if staticDir != "" {
		if _, err := os.Stat(staticDir); err == nil {
			staticHandler, err := serveStatic(staticDir)
			if err != nil {
				return nil, nil, err
			}
			router.Use(staticHandler)
		}
	}

	return router, copilotRuntime, nil
}

func dbFromMySQL(mysqlClient *database.MySQL) *gorm.DB {
	if mysqlClient == nil {
		return nil
	}
	return mysqlClient.DB()
}

type copilotToolExecutor interface {
	ExecuteTool(ctx context.Context, name string, args json.RawMessage) (copilottool.ToolResult, error)
}

type diagnosisToolRunner struct {
	executor copilotToolExecutor
}

type operationEventProducer struct {
	producer *eventbus.Producer
}

func (p operationEventProducer) SendOperationEvent(event copilotaction.OperationEvent) error {
	if p.producer == nil {
		return nil
	}
	return p.producer.SendOperationEvent(event.ActionType, event)
}

func (r diagnosisToolRunner) ExecuteTool(ctx context.Context, name string, args json.RawMessage) (copilotdiagnosis.ToolResult, error) {
	if r.executor == nil {
		return copilotdiagnosis.ToolResult{Success: false, Error: "tool registry unavailable"}, nil
	}
	if _, ok := copilotservice.UserFromContext(ctx); !ok {
		if user, ok := copilotdiagnosis.UserFromContext(ctx); ok {
			ctx = copilotservice.WithUser(ctx, copilotservice.User{
				ID:       user.ID,
				Username: user.Username,
				Role:     user.Role,
			})
		}
	}
	result, err := r.executor.ExecuteTool(ctx, name, args)
	errorMessage := ""
	if result.Error != nil {
		errorMessage = result.Error.Error()
	}
	return copilotdiagnosis.ToolResult{
		Success: result.Success,
		Data:    result.Data,
		Error:   errorMessage,
	}, err
}

func limitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func serveStatic(staticDir string) (gin.HandlerFunc, error) {
	fileServer := http.FileServer(http.Dir(staticDir))
	absStaticDir, err := filepath.Abs(staticDir)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}

		if len(path) >= 5 && path[:5] == "/api/" {
			c.Next()
			return
		}
		if len(path) >= 4 && path[:4] == "/ws/" {
			c.Next()
			return
		}
		if path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/readyz/") {
			c.Next()
			return
		}

		filePath := filepath.Join(absStaticDir, filepath.Clean(path))
		if !strings.HasPrefix(filePath, absStaticDir+string(os.PathSeparator)) && filePath != absStaticDir {
			c.Next()
			return
		}

		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}

		indexPath := filepath.Join(absStaticDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}

		c.Next()
	}, nil
}
