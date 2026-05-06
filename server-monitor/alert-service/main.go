package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"alert-service/alert"
	"alert-service/config"
	"alert-service/health"
	"alert-service/kafka"
	servicemetrics "alert-service/metrics"
	redisstore "alert-service/redis"

	"server-monitor/pkg/httpmiddleware"
	"server-monitor/pkg/logger"
	"server-monitor/pkg/shutdown"
	"server-monitor/pkg/tracer"
)

const (
	redisDialTimeout  = 3 * time.Second
	redisReadTimeout  = 2 * time.Second
	redisWriteTimeout = 2 * time.Second
	readinessTimeout  = time.Second
	shutdownPhases    = 3
)

type app struct {
	cfg            config.Config
	shutdownTracer func(context.Context) error
	redisClient    *redisstore.Client
	consumer       *kafka.Consumer
	healthHandler  *health.Handler
	serviceMetrics *servicemetrics.Metrics
	server         *http.Server
	ctx            context.Context
	cancel         context.CancelFunc
	consumerErr    chan error
	consumerDone   chan struct{}
}

func main() {
	log, err := logger.Init("alert-service")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	app, err := initApp(context.Background())
	if err != nil {
		zap.L().Fatal("alert-service init failed", zap.Error(err))
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

	redisClient := redisstore.NewClient(redisstore.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DialTimeout:  redisDialTimeout,
		ReadTimeout:  redisReadTimeout,
		WriteTimeout: redisWriteTimeout,
	})

	serviceMetrics := servicemetrics.New()
	store := alert.NewStore(redisClient, alert.DefaultDedupTTL, serviceMetrics)
	consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroupID, store)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer init failed: %w", err)
	}
	consumer.SetObserver(serviceMetrics)

	healthHandler := health.NewHandler(redisClient, readinessTimeout)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           loggingMiddleware(recoveryMiddleware(newMux(healthHandler, serviceMetrics))),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	appCtx, cancel := context.WithCancel(context.Background())
	return &app{
		cfg:            cfg,
		shutdownTracer: shutdownTracer,
		redisClient:    redisClient,
		consumer:       consumer,
		healthHandler:  healthHandler,
		serviceMetrics: serviceMetrics,
		server:         server,
		ctx:            appCtx,
		cancel:         cancel,
		consumerErr:    make(chan error, 1),
		consumerDone:   make(chan struct{}),
	}, nil
}

func initTracer(ctx context.Context, cfg config.Config) func(context.Context) error {
	shutdownTracer, err := tracer.Init(ctx, tracer.Config{
		ServiceName:  "alert-service",
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

func newMux(healthHandler *health.Handler, serviceMetrics *servicemetrics.Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/readyz", healthHandler.Readyz)
	mux.Handle("/metrics", serviceMetrics.HTTPHandler())
	return mux
}

func runApp(app *app) int {
	startConsumer(app)
	serverErr := make(chan error, 1)
	go func() {
		zap.L().Info("alert-service listening", zap.String("addr", app.cfg.ListenAddr))
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	exitCode := 0
	select {
	case sig := <-quit:
		zap.L().Info("alert-service received shutdown signal", zap.String("signal", sig.String()))
	case err := <-serverErr:
		exitCode = 1
		zap.L().Error("alert-service exited", zap.Error(err))
	case err := <-app.consumerErr:
		exitCode = 1
		zap.L().Error("alert-service consumer exited", zap.Error(err))
	}
	signal.Stop(quit)
	return exitCode
}

func startConsumer(app *app) {
	go func() {
		runConsumerLoop(app.ctx, app.consumerDone, app.consumerErr, func() error {
			return app.consumer.Consume(app.ctx,
				func() {
					app.healthHandler.SetKafkaReady(true)
					app.serviceMetrics.SetKafkaReady(true)
					zap.L().Info("kafka consumer ready", zap.Strings("brokers", app.cfg.KafkaBrokers), zap.String("group_id", app.cfg.KafkaGroupID))
				},
				func() {
					app.healthHandler.SetKafkaReady(false)
					app.serviceMetrics.SetKafkaReady(false)
				},
			)
		})
	}()
}

func shutdownApp(app *app) {
	zap.L().Info("alert-service shutting down")
	app.cancel()
	app.healthHandler.SetKafkaReady(false)
	app.serviceMetrics.SetKafkaReady(false)

	perPhase := phaseTimeout(app.cfg.ShutdownTimeout, shutdownPhases)

	if err := app.consumer.Close(); err != nil {
		zap.L().Warn("kafka consumer close failed", zap.Error(err))
	}
	select {
	case <-app.consumerDone:
	case <-time.After(perPhase):
		zap.L().Warn("kafka consumer shutdown timed out")
	}

	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
		{Name: "tracer", Fn: app.shutdownTracer},
		{Name: "redis", Fn: func(ctx context.Context) error { return app.redisClient.Close() }},
	})

	zap.L().Info("alert-service stopped")
}

func loggingMiddleware(next http.Handler) http.Handler {
	logged := httpmiddleware.Logging(
		next,
		func(w http.ResponseWriter, r *http.Request, start time.Time) *http.Request {
			nextRequest, _ := httpmiddleware.EnsureRequestID(w, r, start)
			return nextRequest
		},
		func(r *http.Request, _ int, _ time.Duration) []zap.Field {
			fields := []zap.Field{zap.String("module", "http")}
			fields = append(fields, httpmiddleware.RequestMetadataFields(r, httpmiddleware.RequestIDFromRequest(r))...)
			return fields
		},
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("alert-service/http").Start(r.Context(), r.Method+" "+r.URL.Path)
		defer span.End()
		span.SetAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
		)
		r = r.WithContext(ctx)

		logged.ServeHTTP(w, r)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return httpmiddleware.Recovery(next, func(r *http.Request) []zap.Field {
		return []zap.Field{
			zap.String("module", "http"),
			zap.String("request_id", httpmiddleware.RequestIDFromRequest(r)),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		}
	})
}

func phaseTimeout(total time.Duration, phases int) time.Duration {
	if total <= 0 || phases <= 1 {
		return total
	}

	perPhase := total / time.Duration(phases)
	if perPhase <= 0 {
		return total
	}
	return perPhase
}

func runConsumerLoop(ctx context.Context, done chan<- struct{}, errCh chan<- error, consume func() error) {
	defer close(done)
	defer func() {
		if recovered := recover(); recovered != nil {
			zap.L().Error("alert-service consumer panic recovered", zap.Any("panic", recovered))
			if ctx.Err() == nil {
				select {
				case errCh <- fmt.Errorf("alert-service consumer panic: %v", recovered):
				default:
				}
			}
		}
	}()

	if err := consume(); err != nil && ctx.Err() == nil {
		select {
		case errCh <- err:
		default:
		}
	}
}
