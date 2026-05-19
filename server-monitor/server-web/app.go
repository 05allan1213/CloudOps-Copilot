package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"server-web/internal/config"
	copilotk8s "server-web/internal/copilot/k8s"
	"server-web/internal/handler"
	"server-web/internal/infra/database"
	promclient "server-web/internal/infra/prometheus"
	"server-web/internal/infra/pubsub"
	rediscache "server-web/internal/infra/redis"
	ws "server-web/internal/infra/websocket"
	"server-web/internal/middleware"
	"server-web/internal/router"
	appalert "server-web/internal/service/alert"
	authpkg "server-web/internal/service/auth"
	appcache "server-web/internal/service/cache"

	eventbus "server-monitor/pkg/kafka"
	"server-monitor/pkg/shutdown"
	"server-monitor/pkg/tracer"
)

type infrastructure struct {
	shutdownTracer   func(context.Context) error
	prometheusClient *promclient.Client
	redisClient      *rediscache.Client
	mysqlClient      *database.MySQL
	kafkaProducer    *eventbus.Producer
	websocketHub     *ws.Hub
	alertHub         *pubsub.Hub
}

type services struct {
	authService    *authpkg.Service
	alertService   *appalert.Service
	handler        *handler.Handler
	metrics        *middleware.Metrics
	copilotRuntime *router.CopilotRuntime
	copilotDeps    *router.CopilotDeps
	k8sHandler     *handler.K8sHandler
	k8sReader      copilotk8s.Reader
}

type app struct {
	cfg               config.Config
	shutdownTracer    func(context.Context) error
	prometheusClient  *promclient.Client
	redisClient       *rediscache.Client
	mysqlClient       *database.MySQL
	kafkaProducer     *eventbus.Producer
	diagnosisConsumer *eventbus.Consumer
	copilotRuntime    *router.CopilotRuntime
	alertHub          *pubsub.Hub
	websocketHub      *ws.Hub
	k8sReader         copilotk8s.Reader
	server            *http.Server
	ctx               context.Context
	cancel            context.CancelFunc
	subscriberDone    <-chan struct{}
	diagnosisDone     <-chan struct{}
	alertHubConsumers <-chan struct{}
}

type wsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func initApp(ctx context.Context) (*app, error) {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	infra, err := initInfrastructure(ctx, cfg)
	if err != nil {
		return nil, err
	}
	svc, err := initServices(ctx, cfg, infra)
	if err != nil {
		return nil, err
	}

	router, err := router.NewRouter(cfg, router.Dependencies{
		Metrics:     svc.metrics,
		CacheClient: infra.redisClient,
		Handler:     svc.handler,
		K8sHandler:  svc.k8sHandler,
		AuthService: svc.authService,
		Copilot:     svc.copilotDeps,
	})
	if err != nil {
		return nil, fmt.Errorf("create router: %w", err)
	}

	diagnosisConsumer, err := initDiagnosisConsumer(cfg, infra.redisClient, svc.copilotRuntime, infra.websocketHub)
	if err != nil {
		return nil, err
	}

	appCtx, cancel := context.WithCancel(ctx)
	return &app{
		cfg:               cfg,
		shutdownTracer:    infra.shutdownTracer,
		prometheusClient:  infra.prometheusClient,
		redisClient:       infra.redisClient,
		mysqlClient:       infra.mysqlClient,
		kafkaProducer:     infra.kafkaProducer,
		diagnosisConsumer: diagnosisConsumer,
		copilotRuntime:    svc.copilotRuntime,
		alertHub:          infra.alertHub,
		websocketHub:      infra.websocketHub,
		k8sReader:         svc.k8sReader,
		server: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           router,
			ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
			ReadTimeout:       cfg.HTTPReadTimeout,
			WriteTimeout:      cfg.HTTPWriteTimeout,
			IdleTimeout:       cfg.HTTPIdleTimeout,
		},
		ctx:    appCtx,
		cancel: cancel,
	}, nil
}

func runApp(app *app) int {
	startBackgroundTasks(app)
	serverErr := make(chan error, 1)
	go func() {
		zap.L().Info("server-web listening", zap.String("addr", app.cfg.ListenAddr))
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	appDone := app.ctx.Done()
	exitCode := 0
	select {
	case sig := <-quit:
		zap.L().Info("server-web received shutdown signal", zap.String("signal", sig.String()))
	case <-appDone:
		zap.L().Info("server-web context canceled")
	case err := <-serverErr:
		exitCode = 1
		zap.L().Error("server-web exited", zap.Error(err))
	}
	signal.Stop(quit)

	return exitCode
}

func startBackgroundTasks(app *app) {
	go app.websocketHub.Run(app.ctx)

	if app.redisClient.Enabled() {
		pingCtx, pingCancel := context.WithTimeout(context.Background(), app.cfg.RedisStartupTimeout)
		if err := app.redisClient.Ping(pingCtx); err != nil {
			zap.L().Error("redis ping failed at startup",
				zap.String("addr", app.cfg.RedisAddr),
				zap.Error(err),
			)
		}
		pingCancel()

		subscriber := pubsub.NewSubscriber(app.redisClient, app.alertHub, rediscache.AlertChannel)
		done := make(chan struct{})
		app.subscriberDone = done
		go func() {
			defer close(done)
			subscriber.Run(app.ctx)
		}()
	}

	alertHubConsumers := make(chan struct{})
	app.alertHubConsumers = alertHubConsumers
	go func() {
		defer close(alertHubConsumers)
		for message := range app.alertHub.Messages() {
			if err := app.websocketHub.BroadcastBlocking(app.ctx, message); err != nil {
				if app.ctx.Err() != nil {
					return
				}
				zap.L().Warn("broadcast alert failed", zap.Error(err))
			}
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				zap.L().Error("broadcastHosts goroutine recovered from panic", zap.Any("panic", r))
			}
		}()
		broadcastHosts(app.ctx, app.prometheusClient, app.websocketHub, app.cfg.RequestTimeout, app.cfg.HostsBroadcastInterval)
	}()

	if app.k8sReader != nil && app.cfg.K8SAPIEnabled {
		startK8sEventWatcher(app.ctx, app.k8sReader, app.websocketHub, app.cfg.K8sEventWatchInterval)
	}

	if app.diagnosisConsumer != nil {
		done := make(chan struct{})
		app.diagnosisDone = done
		go func() {
			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					zap.L().Error("diagnosis consumer recovered from panic", zap.Any("panic", r))
				}
			}()
			err := app.diagnosisConsumer.Consume(app.ctx,
				func() {
					zap.L().Info("diagnosis kafka consumer ready")
				},
				func() {
					zap.L().Info("diagnosis kafka consumer not ready")
				},
			)
			if err != nil && app.ctx.Err() == nil {
				zap.L().Error("diagnosis kafka consumer stopped", zap.Error(err))
			}
		}()
	}
}

func shutdownApp(app *app) {
	zap.L().Info("server-web shutting down")

	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
		{Name: "tracer", Fn: app.shutdownTracer},
	})

	app.cancel()
	if app.diagnosisConsumer != nil {
		if err := app.diagnosisConsumer.Close(); err != nil {
			zap.L().Warn("diagnosis kafka consumer close failed", zap.Error(err))
		}
	}
	waitWithTimeout(app.subscriberDone, app.cfg.ShutdownTimeout, "subscriber")
	waitWithTimeout(app.diagnosisDone, app.cfg.ShutdownTimeout, "diagnosis-consumer")

	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "redis", Fn: func(ctx context.Context) error { return app.redisClient.Close() }},
		{Name: "mysql", Fn: func(ctx context.Context) error {
			if app.mysqlClient != nil {
				return app.mysqlClient.Close()
			}
			return nil
		}},
		{Name: "kafka-producer", Fn: func(ctx context.Context) error {
			if app.kafkaProducer != nil {
				return app.kafkaProducer.Close()
			}
			return nil
		}},
	})

	app.alertHub.Close()
	waitWithTimeout(app.alertHubConsumers, app.cfg.ShutdownTimeout, "alert-hub-consumers")

	zap.L().Info("server-web stopped")
}

func waitWithTimeout(ch <-chan struct{}, timeout time.Duration, name string) {
	if ch == nil {
		return
	}
	select {
	case <-ch:
		zap.L().Info("shutdown wait completed", zap.String("name", name))
	case <-time.After(timeout):
		zap.L().Warn("shutdown wait timed out, proceeding",
			zap.String("name", name),
			zap.Duration("timeout", timeout),
		)
	}
}

func broadcastHosts(ctx context.Context, promClient *promclient.Client, hub *ws.Hub, timeout time.Duration, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			hosts, err := promClient.GetHosts(queryCtx)
			cancel()
			if err != nil {
				zap.L().Warn("broadcast hosts query failed", zap.Error(err))
				continue
			}

			payload, err := json.Marshal(wsMessage{Type: "hosts", Data: hosts})
			if err != nil {
				zap.L().Warn("broadcast hosts marshal failed", zap.Error(err))
				continue
			}

			hub.Broadcast(payload)
		}
	}
}

func initInfrastructure(ctx context.Context, cfg config.Config) (infrastructure, error) {
	shutdownTracer := initTracer(ctx, cfg)
	gin.SetMode(cfg.GinMode)
	redisClient := rediscache.NewClient(rediscache.Options{
		Addr:            cfg.RedisAddr,
		Password:        cfg.RedisPassword,
		DB:              cfg.RedisDB,
		DialTimeout:     cfg.RedisDialTimeout,
		ReadTimeout:     cfg.RedisReadTimeout,
		WriteTimeout:    cfg.RedisWriteTimeout,
		ConnMaxLifetime: cfg.RedisConnMaxLifetime,
		ConnMaxIdleTime: cfg.RedisConnMaxIdleTime,
	})
	mysqlClient, err := initMySQL(cfg)
	if err != nil {
		return infrastructure{}, err
	}
	if mysqlClient != nil {
		zap.L().Info("mysql initialized",
			zap.String("host", cfg.MySQLHost),
			zap.String("port", cfg.MySQLPort),
			zap.String("database", cfg.MySQLDatabase),
		)
	}
	return infrastructure{
		shutdownTracer:   shutdownTracer,
		prometheusClient: promclient.NewClient(cfg.PrometheusURL, cfg.RequestTimeout),
		redisClient:      redisClient,
		mysqlClient:      mysqlClient,
		kafkaProducer:    initKafkaProducer(cfg),
		websocketHub:     ws.NewHub(cfg.WSMaxConnections, cfg.CORSOrigins),
		alertHub:         pubsub.NewHub(64),
	}, nil
}

func initServices(ctx context.Context, cfg config.Config, infra infrastructure) (services, error) {
	authService, err := initAuthService(cfg, infra.mysqlClient)
	if err != nil {
		return services{}, err
	}
	metrics := middleware.NewMetrics()
	infra.websocketHub.SetConnectionObserver(metrics.SetWebSocketConnections)
	if infra.kafkaProducer != nil {
		infra.kafkaProducer.SetObserver(metrics)
	}

	db := dbFromMySQL(infra.mysqlClient)
	alertService := appalert.NewService(infra.redisClient, appalert.Options{
		DedupeTTL: cfg.AlertEventDedupeTTL,
		DB:        db,
		Producer:  infra.kafkaProducer,
	})
	h, err := handler.NewHandler(infra.prometheusClient, infra.redisClient, handler.Config{
		ReadyTimeout:   cfg.ReadyTimeout,
		RequestTimeout: cfg.RequestTimeout,
		HostsTTL:       cfg.HostsCacheTTL,
		DashboardTTL:   cfg.DashboardOverviewTTL,
		DedupeTTL:      cfg.AlertEventDedupeTTL,
		CacheTimeout:   cfg.CacheWriteTimeout,
		RuleSync: handler.NewAlertRuleSyncConfig(
			cfg.AlertRuleSyncEnabled,
			cfg.AlertRulesFilePath,
			cfg.PromtoolPath,
			cfg.PrometheusReloadURL,
			cfg.AlertRuleSyncTimeout,
		),
		AlertService:    alertService,
		AlertProducer:   infra.kafkaProducer,
		MySQLClient:     infra.mysqlClient,
		DB:              db,
		AuthService:     authService,
		K8sAPIEnabled:   cfg.K8SEnabled && cfg.K8SAPIEnabled,
		K8sNodesEnabled: cfg.K8SEnabled && cfg.K8SAPIEnabled && cfg.K8SNodesEnabled,
		CopilotEnabled:  cfg.CopilotEnabled,
	}, infra.websocketHub)
	if err != nil {
		return services{}, err
	}

	k8sCacheService := appcache.NewService(infra.redisClient, appcache.Options{
		HostsTTL:     cfg.HostsCacheTTL,
		DashboardTTL: cfg.DashboardOverviewTTL,
	})
	k8sReader, k8sClient, _, k8sHandler, err := initK8sRuntime(cfg, infra, k8sCacheService)
	if err != nil {
		return services{}, err
	}

	copilotRuntime, copilotDeps, err := initCopilot(ctx, cfg, infra, metrics, alertService, db, k8sReader, k8sClient)
	if err != nil {
		return services{}, err
	}
	return services{
		authService:    authService,
		alertService:   alertService,
		handler:        h,
		metrics:        metrics,
		copilotRuntime: copilotRuntime,
		copilotDeps:    copilotDeps,
		k8sHandler:     k8sHandler,
		k8sReader:      k8sReader,
	}, nil
}

func initTracer(ctx context.Context, cfg config.Config) func(context.Context) error {
	shutdownTracer, err := tracer.Init(ctx, tracer.Config{
		ServiceName:  "server-web",
		OTLPEndpoint: cfg.TraceOTLPEndpoint,
		SampleRate:   cfg.TraceSampleRate,
	})
	if err != nil {
		zap.L().Warn("tracer init failed; tracing disabled",
			zap.String("endpoint", cfg.TraceOTLPEndpoint),
			zap.Error(err),
		)
		return func(context.Context) error { return nil }
	}
	if cfg.TraceOTLPEndpoint != "" {
		zap.L().Info("tracer initialized",
			zap.String("endpoint", cfg.TraceOTLPEndpoint),
			zap.Float64("sample_rate", cfg.TraceSampleRate),
		)
	}
	return shutdownTracer
}

func initMySQL(cfg config.Config) (*database.MySQL, error) {
	mysqlInitCtx, mysqlInitCancel := context.WithTimeout(context.Background(), cfg.MySQLStartupTimeout)
	mysqlClient, err := database.OpenMySQL(mysqlInitCtx, database.MySQLConfig{
		Host:        cfg.MySQLHost,
		Port:        cfg.MySQLPort,
		User:        cfg.MySQLUser,
		Password:    cfg.MySQLPassword,
		Database:    cfg.MySQLDatabase,
		PingTimeout: cfg.MySQLPingTimeout,
	})
	mysqlInitCancel()
	if err != nil {
		return nil, fmt.Errorf("mysql init failed: %w", err)
	}
	if mysqlClient != nil {
		if err := database.Migrate(mysqlClient.DB()); err != nil {
			return nil, fmt.Errorf("mysql migration failed: %w", err)
		}
	}
	return mysqlClient, nil
}

func initAuthService(cfg config.Config, mysqlClient *database.MySQL) (*authpkg.Service, error) {
	var authService *authpkg.Service
	if mysqlClient != nil && len(strings.TrimSpace(cfg.JWTSecret)) >= 32 {
		var err error
		authService, err = authpkg.NewService(mysqlClient.DB(), cfg.JWTSecret, time.Duration(cfg.JWTExpireHours)*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("auth service init failed: %w", err)
		}
		created, err := authService.EnsureInitialAdmin(context.Background(), cfg.AdminPassword)
		if err != nil {
			return nil, fmt.Errorf("initial admin setup failed: %w", err)
		}
		if created {
			zap.L().Info("initial admin user created", zap.String("username", "admin"))
		}
	}
	return authService, nil
}

func initKafkaProducer(cfg config.Config) *eventbus.Producer {
	var kafkaProducer *eventbus.Producer
	if len(cfg.KafkaBrokers) > 0 {
		producer, err := eventbus.NewProducer(cfg.KafkaBrokers)
		if err != nil {
			zap.L().Warn("kafka producer init failed; kafka events disabled",
				zap.Strings("brokers", cfg.KafkaBrokers),
				zap.Error(err),
			)
		} else {
			kafkaProducer = producer
			zap.L().Info("kafka producer initialized", zap.Strings("brokers", cfg.KafkaBrokers))
		}
	}
	return kafkaProducer
}

func dbFromMySQL(mysqlClient *database.MySQL) *gorm.DB {
	if mysqlClient == nil {
		return nil
	}
	return mysqlClient.DB()
}
