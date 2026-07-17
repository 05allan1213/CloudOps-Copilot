package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"server-web/internal/config"
	"server-web/internal/di"
	"server-web/internal/infra/k8sread"
	"server-web/internal/router"
	agentruntime "server-web/internal/service/agentruntime"
	"server-web/internal/service/deliveryverification"
	remediationservice "server-web/internal/service/remediation"
	"server-web/internal/startup"

	"server-monitor/pkg/shutdown"
)

type app struct {
	cfg                        config.Config
	infra                      *di.Infra
	agentWorker                *agentruntime.Worker
	remediationWorker          *remediationservice.Worker
	deliveryVerificationWorker *deliveryverification.Worker
	server                     *http.Server
	ctx                        context.Context
	cancel                     context.CancelFunc
}

func initConfig() (config.Config, error) {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func initApp(ctx context.Context) (*app, error) {
	cfg, err := initConfig()
	if err != nil {
		return nil, err
	}

	infra, err := startup.InitInfra(ctx, cfg)
	if err != nil {
		return nil, err
	}

	container, err := startup.InitContainer(&cfg, infra)
	if err != nil {
		return nil, err
	}

	k8sReader, k8sClient, err := startup.InitK8sRuntime(cfg)
	if err != nil {
		return nil, err
	}
	container.Handler.SetIncidentK8sReader(k8sReader)

	k8sDeps := k8sread.Deps{Reader: k8sReader, Client: k8sClient}
	agentWorker, err := startup.InitAgentRuntime(ctx, cfg, container, k8sDeps)
	if err != nil {
		return nil, err
	}
	var remediationWorker *remediationservice.Worker
	var deliveryVerificationWorker *deliveryverification.Worker
	if cfg.FastDemoEnabled {
		if _, err := startup.InitFastDemo(cfg, container, k8sClient); err != nil {
			return nil, err
		}
	} else {
		remediationWorker, err = startup.InitRemediation(cfg, container)
		if err != nil {
			return nil, err
		}
		deliveryVerificationWorker, err = startup.InitDeliveryVerification(cfg, container)
		if err != nil {
			return nil, err
		}
	}

	deps := container.Dependencies()
	router, err := router.NewRouter(cfg, deps)
	if err != nil {
		return nil, fmt.Errorf("create router: %w", err)
	}

	appCtx, cancel := context.WithCancel(ctx)
	application := &app{
		cfg:                        cfg,
		infra:                      infra,
		agentWorker:                agentWorker,
		remediationWorker:          remediationWorker,
		deliveryVerificationWorker: deliveryVerificationWorker,
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
	}
	return application, nil
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
	if app.agentWorker != nil {
		app.agentWorker.Start(app.ctx)
	}
	if app.remediationWorker != nil {
		app.remediationWorker.Start(app.ctx)
	}
	if app.deliveryVerificationWorker != nil {
		app.deliveryVerificationWorker.Start(app.ctx)
	}

	if app.infra.RedisClient.Enabled() {
		pingCtx, pingCancel := context.WithTimeout(context.Background(), app.cfg.RedisStartupTimeout)
		if err := app.infra.RedisClient.Ping(pingCtx); err != nil {
			zap.L().Error("redis ping failed at startup",
				zap.String("addr", app.cfg.RedisAddr),
				zap.Error(err),
			)
		}
		pingCancel()

	}
}

func shutdownApp(app *app) {
	zap.L().Info("server-web shutting down")

	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
		{Name: "tracer", Fn: app.infra.ShutdownTracer},
	})

	app.cancel()
	if app.agentWorker != nil {
		app.agentWorker.Stop()
	}
	if app.deliveryVerificationWorker != nil {
		app.deliveryVerificationWorker.Stop()
	}
	shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
		{Name: "redis", Fn: func(ctx context.Context) error { return app.infra.RedisClient.Close() }},
		{Name: "mysql", Fn: func(ctx context.Context) error {
			if app.infra.DB != nil {
				sqlDB, err := app.infra.DB.DB()
				if err != nil {
					return err
				}
				return sqlDB.Close()
			}
			return nil
		}},
		{Name: "kafka-producer", Fn: func(ctx context.Context) error {
			if app.infra.KafkaProducer != nil {
				return app.infra.KafkaProducer.Close()
			}
			return nil
		}},
	})

	zap.L().Info("server-web stopped")
}
