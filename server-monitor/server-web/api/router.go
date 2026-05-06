package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"

	"server-web/api/handlers"
	"server-web/api/middleware"
	"server-web/config"
	"server-web/database"
	eventbus "server-web/kafka"
	promclient "server-web/prometheus"
	rediscache "server-web/redis"
	ws "server-web/websocket"

	_ "server-web/docs"
)

type authService interface {
	handlers.AuthService
}

func NewRouter(cfg config.Config, promClient *promclient.Client, cacheClient *rediscache.Client, mysqlClient *database.MySQL, authService authService, websocketHub *ws.Hub, alertProducer *eventbus.Producer) (*gin.Engine, error) {
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
		return nil, err
	}

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
		AlertProducer: alertProducer,
		MySQLClient:   mysqlClient,
		DB:            dbFromMySQL(mysqlClient),
		AuthService:   authService,
	}, websocketHub)
	if err != nil {
		return nil, err
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
				return nil, err
			}
			router.Use(staticHandler)
		}
	}

	return router, nil
}

func dbFromMySQL(mysqlClient *database.MySQL) *gorm.DB {
	if mysqlClient == nil {
		return nil
	}
	return mysqlClient.DB()
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
