package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"

	"server-probe/collector"
	"server-probe/config"

	"server-monitor/pkg/httpmiddleware"
	"server-monitor/pkg/logger"
	"server-monitor/pkg/shutdown"
	"server-monitor/pkg/tracer"
)

type app struct {
	cfg            config.Config
	shutdownTracer func(context.Context) error
	collectors     []collector.Collector
	server         *http.Server
	ctx            context.Context
	cancel         context.CancelFunc
}

func main() {
	log, err := logger.Init("server-probe")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	app, err := initApp(context.Background())
	if err != nil {
		zap.L().Error("server-probe init failed", zap.Error(err))
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
	if err := applyHostPaths(cfg); err != nil {
		return nil, fmt.Errorf("apply host paths: %w", err)
	}

	registry := prometheus.NewRegistry()
	collectors := newCollectors(cfg)
	for _, c := range collectors {
		c.Register(registry)
	}

	appCtx, cancel := context.WithCancel(context.Background())
	return &app{
		cfg:            cfg,
		shutdownTracer: shutdownTracer,
		collectors:     collectors,
		server: &http.Server{
			Addr:         cfg.ListenAddr,
			Handler:      tracedHandler(loggingMiddleware(recoveryMiddleware(newMux(cfg, registry)))),
			ReadTimeout:  cfg.HTTPReadTimeout,
			WriteTimeout: cfg.HTTPWriteTimeout,
			IdleTimeout:  cfg.HTTPIdleTimeout,
		},
		ctx:    appCtx,
		cancel: cancel,
	}, nil
}

func initTracer(ctx context.Context, cfg config.Config) func(context.Context) error {
	shutdownTracer, err := tracer.Init(ctx, tracer.Config{
		ServiceName:  "server-probe",
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

func newCollectors(cfg config.Config) []collector.Collector {
	return []collector.Collector{
		collector.NewCPUCollector(cfg.Hostname),
		collector.NewMemoryCollector(cfg.Hostname),
		collector.NewDiskCollector(cfg.Hostname),
		collector.NewNetworkCollector(cfg.Hostname),
		collector.NewLoadCollector(cfg.Hostname),
		collector.NewProcessCollector(cfg.Hostname),
	}
}

func newMux(cfg config.Config, registry *prometheus.Registry) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		MaxRequestsInFlight: cfg.PromHTTPMaxRequestsInFlight,
		Timeout:             cfg.PromHTTPTimeout,
	}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"healthy":true}`)); err != nil {
			zap.L().Error("healthz response write failed", zap.Error(err))
		}
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"ready":true}`)); err != nil {
			zap.L().Error("readyz response write failed", zap.Error(err))
		}
	})
	return mux
}

func runApp(app *app) int {
	startCollectorLoop(app)
	serverErr := make(chan error, 1)
	go func() {
		zap.L().Info("server-probe listening",
			zap.String("addr", app.cfg.ListenAddr),
			zap.String("metrics_path", app.cfg.MetricsPath),
		)
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	exitCode := 0
	select {
	case sig := <-quit:
		zap.L().Info("server-probe shutting down", zap.String("signal", sig.String()))
	case err := <-serverErr:
		zap.L().Error("server-probe exited", zap.Error(err))
		exitCode = 1
	}
	signal.Stop(quit)
	return exitCode
}

func startCollectorLoop(app *app) {
	updateCollectors(app.ctx, app.collectors)

	go func() {
		ticker := time.NewTicker(app.cfg.ScrapeInterval)
		defer ticker.Stop()

		for {
			select {
			case <-app.ctx.Done():
				return
			case <-ticker.C:
				updateCollectors(app.ctx, app.collectors)
			}
		}
	}()
}

func shutdownApp(app *app) {
	app.cancel()

	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
		{Name: "tracer", Fn: app.shutdownTracer},
	})

	zap.L().Info("server-probe stopped")
}

func updateCollectors(ctx context.Context, collectors []collector.Collector) {
	var wg sync.WaitGroup

	for _, c := range collectors {
		if err := ctx.Err(); err != nil {
			break
		}

		wg.Add(1)
		go func(c collector.Collector) {
			defer wg.Done()

			if err := c.Update(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				zap.L().Error("collector update failed",
					zap.String("collector", c.Name()),
					zap.Error(err),
				)
			}
		}(c)
	}

	wg.Wait()
}

func applyHostPaths(cfg config.Config) error {
	if cfg.HostProc != "" {
		if err := os.Setenv("HOST_PROC", cfg.HostProc); err != nil {
			return err
		}
	}
	if cfg.HostSys != "" {
		if err := os.Setenv("HOST_SYS", cfg.HostSys); err != nil {
			return err
		}
	}
	return nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return httpmiddleware.Logging(
		next,
		func(w http.ResponseWriter, r *http.Request, start time.Time) *http.Request {
			nextRequest, _ := httpmiddleware.EnsureRequestID(w, r, start)
			return nextRequest
		},
		func(r *http.Request, _ int, _ time.Duration) []zap.Field {
			return httpmiddleware.RequestMetadataFields(r, httpmiddleware.RequestIDFromRequest(r))
		},
	)
}

func tracedHandler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "server-probe")
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return httpmiddleware.Recovery(next, func(r *http.Request) []zap.Field {
		return []zap.Field{
			zap.String("request_id", httpmiddleware.RequestIDFromRequest(r)),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		}
	})
}
