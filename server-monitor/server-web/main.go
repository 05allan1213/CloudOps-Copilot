// @title           Server Monitor API
// @version         1.0
// @description     服务器监控平台 API，提供主机监控、告警管理、规则配置等功能。
// @host             localhost:8080
// @BasePath         /api/v1
// @schemes          http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
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

	"server-web/api"
	authpkg "server-web/auth"
	"server-web/config"
	"server-web/database"
	eventbus "server-web/kafka"
	promclient "server-web/prometheus"
	"server-web/pubsub"
	rediscache "server-web/redis"
	ws "server-web/websocket"

	"server-monitor/pkg/logger"
	"server-monitor/pkg/shutdown"
	"server-monitor/pkg/tracer"
)

type wsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type app struct {
	cfg               config.Config
	shutdownTracer    func(context.Context) error
	prometheusClient  *promclient.Client
	redisClient       *rediscache.Client
	mysqlClient       *database.MySQL
	kafkaProducer     *eventbus.Producer
	alertHub          *pubsub.Hub
	websocketHub      *ws.Hub
	server            *http.Server
	ctx               context.Context
	cancel            context.CancelFunc
	subscriberDone    <-chan struct{}
	alertHubConsumers <-chan struct{}
}

func main() {
	log, err := logger.Init("server-web")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	app, err := initApp(context.Background())
	if err != nil {
		zap.L().Error("server-web init failed", zap.Error(err))
		os.Exit(1)
	}

	exitCode := runApp(app)
	shutdownApp(app)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func initApp(ctx context.Context) (*app, error) {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	shutdownTracer := initTracer(ctx, cfg)
	gin.SetMode(cfg.GinMode)
	prometheusClient := promclient.NewClient(cfg.PrometheusURL, cfg.RequestTimeout)
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
		return nil, err
	}

	authService, err := initAuthService(cfg, mysqlClient)
	if err != nil {
		return nil, err
	}

	if mysqlClient != nil {
		zap.L().Info("mysql initialized",
			zap.String("host", cfg.MySQLHost),
			zap.String("port", cfg.MySQLPort),
			zap.String("database", cfg.MySQLDatabase),
		)
	}

	kafkaProducer := initKafkaProducer(cfg)
	alertHub := pubsub.NewHub(64)
	websocketHub := ws.NewHub(cfg.WSMaxConnections, cfg.CORSOrigins)

	router, err := api.NewRouter(cfg, prometheusClient, redisClient, mysqlClient, authService, websocketHub, kafkaProducer)
	if err != nil {
		return nil, fmt.Errorf("create router: %w", err)
	}

	appCtx, cancel := context.WithCancel(context.Background())
	return &app{
		cfg:              cfg,
		shutdownTracer:   shutdownTracer,
		prometheusClient: prometheusClient,
		redisClient:      redisClient,
		mysqlClient:      mysqlClient,
		kafkaProducer:    kafkaProducer,
		alertHub:         alertHub,
		websocketHub:     websocketHub,
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

	exitCode := 0
	select {
	case sig := <-quit:
		zap.L().Info("server-web received shutdown signal", zap.String("signal", sig.String()))
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

	go broadcastHosts(app.ctx, app.prometheusClient, app.websocketHub, app.cfg.RequestTimeout, app.cfg.HostsBroadcastInterval)
}

func shutdownApp(app *app) {
	zap.L().Info("server-web shutting down")

	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
		{Name: "tracer", Fn: app.shutdownTracer},
	})

	app.cancel()
	if app.subscriberDone != nil {
		<-app.subscriberDone
	}

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
	if app.alertHubConsumers != nil {
		<-app.alertHubConsumers
	}

	zap.L().Info("server-web stopped")
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
