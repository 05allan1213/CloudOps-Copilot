package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"k8s.io/client-go/kubernetes"

	appalert "server-web/alert"
	"server-web/api"
	"server-web/api/handlers"
	"server-web/api/middleware"
	authpkg "server-web/auth"
	appcache "server-web/cache"
	"server-web/config"
	copilotaction "server-web/copilot/action"
	copilotdiagnosis "server-web/copilot/diagnosis"
	copilotfeedback "server-web/copilot/feedback"
	copilothandler "server-web/copilot/handler"
	copilotk8s "server-web/copilot/k8s"
	copilotllm "server-web/copilot/llm"
	copilotnlu "server-web/copilot/nlu"
	copilotrunbook "server-web/copilot/runbook"
	copilotservice "server-web/copilot/service"
	copilotsession "server-web/copilot/session"
	copilottool "server-web/copilot/tool"
	"server-web/database"
	apphost "server-web/host"
	promclient "server-web/prometheus"
	"server-web/pubsub"
	rediscache "server-web/redis"
	ws "server-web/websocket"

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
	handler        *handlers.Handler
	metrics        *middleware.Metrics
	copilotRuntime *api.CopilotRuntime
	copilotDeps    *api.CopilotDeps
}

type app struct {
	cfg               config.Config
	shutdownTracer    func(context.Context) error
	prometheusClient  *promclient.Client
	redisClient       *rediscache.Client
	mysqlClient       *database.MySQL
	kafkaProducer     *eventbus.Producer
	diagnosisConsumer *eventbus.Consumer
	copilotRuntime    *api.CopilotRuntime
	alertHub          *pubsub.Hub
	websocketHub      *ws.Hub
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

	router, err := api.NewRouter(cfg, api.Dependencies{
		Metrics:     svc.metrics,
		CacheClient: infra.redisClient,
		Handler:     svc.handler,
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

	// Phase 1: stop traffic entrypoints.
	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
		{Name: "tracer", Fn: app.shutdownTracer},
	})

	// Phase 2: stop consumers and wait for background loops.
	app.cancel()
	if app.diagnosisConsumer != nil {
		if err := app.diagnosisConsumer.Close(); err != nil {
			zap.L().Warn("diagnosis kafka consumer close failed", zap.Error(err))
		}
	}
	waitWithTimeout(app.subscriberDone, app.cfg.ShutdownTimeout, "subscriber")
	waitWithTimeout(app.diagnosisDone, app.cfg.ShutdownTimeout, "diagnosis-consumer")

	// Phase 3: release external resources.
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

	// Phase 4: close internal hubs after producers have stopped.
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
	handler, err := handlers.NewHandler(infra.prometheusClient, infra.redisClient, handlers.Config{
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
		AlertProducer: infra.kafkaProducer,
		MySQLClient:   infra.mysqlClient,
		DB:            db,
		AuthService:   authService,
	}, infra.websocketHub)
	if err != nil {
		return services{}, err
	}

	copilotRuntime, copilotDeps, err := initCopilot(ctx, cfg, infra, metrics, alertService, db)
	if err != nil {
		return services{}, err
	}
	return services{
		authService:    authService,
		alertService:   alertService,
		handler:        handler,
		metrics:        metrics,
		copilotRuntime: copilotRuntime,
		copilotDeps:    copilotDeps,
	}, nil
}

func initCopilot(ctx context.Context, cfg config.Config, infra infrastructure, metrics *middleware.Metrics, alertService *appalert.Service, db *gorm.DB) (*api.CopilotRuntime, *api.CopilotDeps, error) {
	if !cfg.CopilotEnabled {
		return nil, nil, nil
	}

	copilotCacheService := appcache.NewService(infra.redisClient, appcache.Options{
		HostsTTL:     cfg.HostsCacheTTL,
		DashboardTTL: cfg.DashboardOverviewTTL,
	})
	copilotHostService := apphost.NewService(infra.prometheusClient, copilotCacheService, apphost.Options{
		RequestTimeout: cfg.CopilotToolDefaultTimeout,
		CacheTimeout:   cfg.CacheWriteTimeout,
	})
	k8sCfg := k8sConfigFromApp(cfg)
	k8sClient, err := copilotk8s.NewClient(k8sCfg)
	if err != nil {
		return nil, nil, err
	}
	var k8sReader copilotk8s.Reader
	if cfg.K8SEnabled {
		k8sReader = copilotk8s.NewServiceWithClient(k8sClient, k8sCfg)
	}
	runbookDocs, err := copilotrunbook.LoadDir(context.Background(), cfg.RunbookDir, copilotrunbook.LoadOptions{
		MaxFiles:     cfg.RunbookMaxFiles,
		MaxFileBytes: cfg.RunbookMaxFileBytes,
	})
	if err != nil {
		return nil, nil, err
	}

	llmClient := copilotllm.NewClient(copilotllm.Options{
		APIKey:    cfg.LLMAPIKey,
		APIURL:    cfg.LLMAPIURL,
		Model:     cfg.LLMModel,
		Timeout:   cfg.LLMTimeout,
		MaxTokens: cfg.LLMMaxTokens,
		Observer:  metrics,
	})
	runbookEmbedder, runbookVectorStore := initRunbookEmbedding(ctx, cfg, runbookDocs)
	runbookRetriever := copilotrunbook.NewRetriever(runbookDocs, copilotrunbook.RetrieverOptions{
		DefaultLimit: cfg.RunbookSearchTopN,
		MaxLimit:     5,
		BM25K1:       cfg.RunbookBM25K1,
		BM25B:        cfg.RunbookBM25B,
		Observer:     metrics,
		Embedder:     runbookEmbedder,
		VectorStore:  runbookVectorStore,
		RRFK:         cfg.RunbookRRFK,
		Reranker:     buildRunbookReranker(cfg, llmClient),
	})

	var tools copilotservice.ToolExecutor
	var toolExecutor *copilottool.Executor
	if cfg.CopilotToolRegistryEnabled {
		toolExecutor, err = copilottool.NewExecutor(copilottool.Options{
			HostService:     copilotHostService,
			AlertService:    alertService,
			PromClient:      infra.prometheusClient,
			RunbookSearcher: runbookRetriever,
			K8sReader:       k8sReader,
			K8sNodesEnabled: cfg.K8SNodesEnabled,
			DB:              db,
			Observer:        metrics,
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

	diagnosisRepo := copilotdiagnosis.NewRepository(db)
	feedbackRepo := copilotfeedback.NewMySQLRepository(db)
	diagnosisService := initDiagnosisService(cfg, alertService, llmClient, toolExecutor, diagnosisRepo, feedbackRepo, db, metrics)
	copilotHandler := copilothandler.NewHandler(copilotservice.NewService(copilotservice.Config{
		MaxMessageLength:     cfg.CopilotMaxMessageLength,
		SessionTTL:           cfg.CopilotSessionTTL,
		MaxSessionMessages:   cfg.CopilotMaxSessionMessages,
		Store:                copilotsession.NewRedisStore(infra.redisClient),
		Classifier:           copilotnlu.NewClassifier(copilotnlu.WithNLUObserver(metrics)),
		LLM:                  llmClient,
		Tools:                tools,
		Diagnosis:            diagnosisService,
		ToolDefs:             toolDefinitions(toolExecutor),
		ToolsClassifyEnabled: cfg.CopilotToolsClassifyEnabled,
		MultiIntentEnabled:   cfg.CopilotMultiIntentEnabled,
		MultiIntentMax:       cfg.CopilotMultiIntentMax,
	}))

	deps := &api.CopilotDeps{
		Handler:          copilotHandler,
		DiagnosisHandler: copilotdiagnosis.NewHandler(diagnosisService),
		FeedbackHandler:  copilotfeedback.NewHandler(copilotfeedback.NewService(feedbackRepo, metrics), api.NewReportAccessChecker(diagnosisRepo), cfg.FeedbackCommentMaxLength),
	}
	if cfg.ActionApprovalEnabled {
		actionHandler, err := initActionHandler(cfg, infra, metrics, db, k8sClient)
		if err != nil {
			return nil, nil, err
		}
		deps.ActionHandler = actionHandler
	}
	return &api.CopilotRuntime{DiagnosisService: diagnosisService, KafkaObserver: metrics}, deps, nil
}

func initRunbookEmbedding(ctx context.Context, cfg config.Config, docs []copilotrunbook.Document) (*copilotrunbook.Embedder, *copilotrunbook.MemoryVectorStore) {
	if cfg.EmbeddingAPIURL == "" || cfg.EmbeddingAPIKey == "" || cfg.EmbeddingModel == "" {
		return nil, nil
	}
	embedder := copilotrunbook.NewEmbedder(copilotrunbook.EmbedderOptions{
		APIURL:     cfg.EmbeddingAPIURL,
		APIKey:     cfg.EmbeddingAPIKey,
		Model:      cfg.EmbeddingModel,
		Timeout:    cfg.EmbeddingTimeout,
		Dimensions: cfg.EmbeddingDims,
	})
	buildCtx, buildCancel := context.WithTimeout(ctx, cfg.EmbeddingIndexBuildTimeout)
	store, err := buildRunbookVectorStore(buildCtx, docs, embedder, &buildIndexLogger{})
	buildCancel()
	if err != nil {
		zap.L().Warn("failed to build runbook vector index, falling back to structured+BM25", zap.Error(err))
		return embedder, nil
	}
	return embedder, store
}

func initDiagnosisService(cfg config.Config, alertService *appalert.Service, llmClient *copilotllm.Client, toolExecutor *copilottool.Executor, diagnosisRepo *copilotdiagnosis.Repository, feedbackRepo copilotfeedback.Repository, db *gorm.DB, metrics *middleware.Metrics) *copilotdiagnosis.Service {
	var runner copilotdiagnosis.ToolRunner
	if toolExecutor != nil {
		runner = diagnosisToolRunner{executor: toolExecutor}
	}
	return copilotdiagnosis.NewService(copilotdiagnosis.Config{
		Repository: diagnosisRepo,
		Resolver: copilotdiagnosis.NewResolver(copilotdiagnosis.ResolverOptions{
			DB:           db,
			AlertService: alertService,
			Timeout:      cfg.CopilotToolDefaultTimeout,
		}),
		Collector: copilotdiagnosis.NewEvidenceCollector(copilotdiagnosis.EvidenceOptions{
			Runner:        runner,
			Timeout:       45 * time.Second,
			RunbookLimit:  cfg.RunbookSearchTopN,
			RerankEnabled: cfg.RerankerEnabled,
		}),
		Summarizer: copilotdiagnosis.NewLLMSummarizerWithOptions(llmClient, copilotdiagnosis.LLMSummarizerOptions{
			Timeout:  cfg.DiagnosisLLMTimeout,
			Observer: metrics,
		}),
		FeedbackRepository: feedbackRepo,
	})
}

func initActionHandler(cfg config.Config, infra infrastructure, metrics *middleware.Metrics, db *gorm.DB, k8sClient kubernetes.Interface) (*copilotaction.Handler, error) {
	actionExecutionEnabled := cfg.ActionExecutionEnabled && cfg.K8SWriteEnabled
	actionExecutor := copilotaction.K8sExecutor(copilotaction.DisabledK8sExecutor{})
	if actionExecutionEnabled {
		if k8sClient == nil {
			return nil, errK8sClientRequired()
		}
		actionExecutor = copilotaction.NewClientK8sExecutor(k8sClient, copilotaction.ClientK8sExecutorConfig{
			AllowedNamespaces: cfg.K8SAllowedNamespaces,
			MaxReplicas:       cfg.ActionMaxReplicas,
			RequestTimeout:    cfg.K8SRequestTimeout,
		})
	} else if cfg.ActionExecutionEnabled && !cfg.K8SWriteEnabled {
		zap.L().Warn("ACTION_EXECUTION_ENABLED=true but K8S_WRITE_ENABLED=false; forcing k8s action execution disabled")
	}
	actionService := copilotaction.NewService(copilotaction.ServiceConfig{
		Repository:             copilotaction.NewRepository(db),
		Policy:                 copilotaction.NewPolicy(copilotaction.PolicyConfig{MaxReplicas: cfg.ActionMaxReplicas}),
		Executor:               actionExecutor,
		Notifier:               copilotaction.NewWebSocketNotifier(infra.websocketHub),
		OperationEvents:        operationEventProducer{producer: infra.kafkaProducer},
		Observer:               metrics,
		OperationEventsEnabled: cfg.ActionOperationEventsEnabled,
		StatusPushEnabled:      cfg.ActionStatusPushEnabled,
		ActionExecutionEnabled: actionExecutionEnabled,
		PendingTTL:             cfg.ActionPendingTTL,
	})
	return copilotaction.NewHandler(actionService), nil
}

func toolDefinitions(toolExecutor *copilottool.Executor) []copilotllm.ToolDefinition {
	if toolExecutor == nil {
		return nil
	}
	return copilottool.ConvertToOpenAITools(toolExecutor.Registry().List())
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

func initDiagnosisConsumer(cfg config.Config, redisClient *rediscache.Client, runtime *api.CopilotRuntime, hub *ws.Hub) (*eventbus.Consumer, error) {
	if !cfg.DiagnosisEnabled {
		zap.L().Info("diagnosis worker disabled")
		return nil, nil
	}
	if runtime == nil || runtime.DiagnosisService == nil {
		return nil, fmt.Errorf("diagnosis worker requires copilot diagnosis service")
	}
	if redisClient == nil || !redisClient.Enabled() {
		return nil, fmt.Errorf("diagnosis worker requires redis")
	}

	var notifier copilotdiagnosis.Notifier
	if cfg.DiagnosisStatusPushEnabled {
		notifier = copilotdiagnosis.NewWebSocketNotifier(hub)
	}
	worker := copilotdiagnosis.NewWorker(copilotdiagnosis.WorkerConfig{
		Service:     runtime.DiagnosisService,
		TaskStore:   copilotdiagnosis.NewRedisTaskStore(redisClient, nil),
		Notifier:    notifier,
		Timeout:     cfg.DiagnosisTaskTimeout,
		TTL:         cfg.DiagnosisTaskTTL,
		Concurrency: cfg.DiagnosisWorkerCount,
		Logger:      zap.L(),
	})
	consumer, err := eventbus.NewConsumer(eventbus.ConsumerConfig{
		Brokers:      cfg.KafkaBrokers,
		GroupID:      cfg.DiagnosisKafkaGroupID,
		RetryBackoff: eventbus.DefaultConsumeRetryBackoff,
	}, worker)
	if err != nil {
		return nil, fmt.Errorf("diagnosis kafka consumer init failed: %w", err)
	}
	consumer.SetRetryableErrors(cfg.DiagnosisRetryableErrors)
	consumer.SetObserver(runtime.KafkaObserver)
	zap.L().Info("diagnosis worker initialized",
		zap.Strings("brokers", cfg.KafkaBrokers),
		zap.String("group_id", cfg.DiagnosisKafkaGroupID),
		zap.Int("worker_count", cfg.DiagnosisWorkerCount),
	)
	return consumer, nil
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

type operationEventProducer struct {
	producer *eventbus.Producer
}

func (p operationEventProducer) SendOperationEvent(event copilotaction.OperationEvent) error {
	if p.producer == nil {
		return nil
	}
	return p.producer.SendOperationEvent(event.ActionType, event)
}

func buildRunbookVectorStore(ctx context.Context, docs []copilotrunbook.Document, embedder *copilotrunbook.Embedder, observers ...copilotrunbook.BuildIndexObserver) (*copilotrunbook.MemoryVectorStore, error) {
	if embedder == nil {
		return nil, nil
	}
	chunks := copilotrunbook.ChunkDocuments(docs)
	store, err := copilotrunbook.BuildMemoryIndex(ctx, embedder, chunks, observers...)
	if err != nil {
		return nil, fmt.Errorf("build runbook vector index: %w", err)
	}
	return store, nil
}

func buildRunbookReranker(cfg config.Config, llmClient *copilotllm.Client) *copilotrunbook.Reranker {
	if !cfg.RerankerEnabled || llmClient == nil {
		return nil
	}
	return copilotrunbook.NewReranker(copilotrunbook.RerankerOptions{
		LLM:     llmClient,
		TopN:    cfg.RerankerTopN,
		Timeout: cfg.RerankerTimeout,
	})
}

type buildIndexLogger struct{}

func (l *buildIndexLogger) ObserveBuildIndexBatchError(batchStart, batchEnd int, err error) {
	zap.L().Warn("runbook embedding batch failed", zap.Int("batch_start", batchStart), zap.Int("batch_end", batchEnd), zap.Error(err))
}

func k8sConfigFromApp(cfg config.Config) copilotk8s.Config {
	return copilotk8s.Config{
		Enabled:           cfg.K8SEnabled,
		WriteEnabled:      cfg.K8SWriteEnabled,
		InCluster:         cfg.K8SInCluster,
		Kubeconfig:        cfg.K8SKubeconfig,
		AllowedNamespaces: cfg.K8SAllowedNamespaces,
		DefaultNamespace:  cfg.K8SDefaultNamespace,
		RequestTimeout:    cfg.K8SRequestTimeout,
		LogTailLines:      cfg.K8SLogTailLines,
		LogMaxBytes:       cfg.K8SLogMaxBytes,
		EventLimit:        cfg.K8SEventLimit,
	}
}

func errK8sClientRequired() error {
	return errors.New("k8s client is required when k8s write execution is enabled")
}
