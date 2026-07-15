package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"server-web/internal/config"
	copilotk8s "server-web/internal/copilot/k8s"
	"server-web/internal/di"
	promclient "server-web/internal/infra/prometheus"
	"server-web/internal/infra/pubsub"
	rediscache "server-web/internal/infra/redis"
	ws "server-web/internal/infra/websocket"
	"server-web/internal/router"
	agentruntime "server-web/internal/service/agentruntime"
	remediationservice "server-web/internal/service/remediation"
	"server-web/internal/startup"

	eventbus "server-monitor/pkg/kafka"
	"server-monitor/pkg/shutdown"
)

type app struct {
	cfg               config.Config
	infra             *di.Infra
	diagnosisConsumer *eventbus.Consumer
	copilotRuntime    *router.CopilotRuntime
	agentWorker       *agentruntime.Worker
	remediationWorker *remediationservice.Worker
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

	k8sReader, k8sClient, _, k8sHandler, err := startup.InitK8sRuntime(cfg, container)
	if err != nil {
		return nil, err
	}
	container.K8sHandler = k8sHandler

	k8sDeps := copilotk8s.Deps{Reader: k8sReader, Client: k8sClient}
	copilotRuntime, copilotDeps, err := startup.InitCopilot(ctx, cfg, container, k8sDeps)
	if err != nil {
		return nil, err
	}
	remediationWorker, err := startup.InitRemediation(cfg, container)
	if err != nil {
		return nil, err
	}

	deps := container.Dependencies()
	deps.Copilot = copilotDeps
	router, err := router.NewRouter(cfg, deps)
	if err != nil {
		return nil, fmt.Errorf("create router: %w", err)
	}

	diagnosisConsumer, err := startup.InitDiagnosisConsumer(cfg, infra.RedisClient, copilotRuntime, infra.WSHub)
	if err != nil {
		return nil, err
	}

	appCtx, cancel := context.WithCancel(ctx)
	application := &app{
		cfg:               cfg,
		infra:             infra,
		diagnosisConsumer: diagnosisConsumer,
		copilotRuntime:    copilotRuntime,
		remediationWorker: remediationWorker,
		k8sReader:         k8sReader,
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
	if copilotRuntime != nil {
		application.agentWorker = copilotRuntime.AgentWorker
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
	go app.infra.WSHub.Run(app.ctx)
	if app.agentWorker != nil {
		app.agentWorker.Start(app.ctx)
	}
	if app.remediationWorker != nil {
		app.remediationWorker.Start(app.ctx)
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

		subscriber := pubsub.NewSubscriber(app.infra.RedisClient, app.infra.AlertHub, rediscache.AlertChannel)
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
		for message := range app.infra.AlertHub.Messages() {
			if err := app.infra.WSHub.BroadcastBlocking(app.ctx, message); err != nil {
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
		broadcastHosts(app.ctx, app.infra.PromClient, app.infra.WSHub, app.cfg.RequestTimeout, app.cfg.HostsBroadcastInterval)
	}()

	if app.k8sReader != nil && app.cfg.K8SAPIEnabled {
		startK8sEventWatcher(app.ctx, app.k8sReader, app.infra.WSHub, app.cfg.K8sEventWatchInterval)
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
		{Name: "tracer", Fn: app.infra.ShutdownTracer},
	})

	app.cancel()
	if app.agentWorker != nil {
		app.agentWorker.Stop()
	}
	if app.diagnosisConsumer != nil {
		if err := app.diagnosisConsumer.Close(); err != nil {
			zap.L().Warn("diagnosis kafka consumer close failed", zap.Error(err))
		}
	}
	waitWithTimeout(app.subscriberDone, app.cfg.ShutdownTimeout, "subscriber")
	waitWithTimeout(app.diagnosisDone, app.cfg.ShutdownTimeout, "diagnosis-consumer")

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

	app.infra.AlertHub.Close()
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

func startK8sEventWatcher(ctx context.Context, k8sReader copilotk8s.Reader, hub *ws.Hub, interval time.Duration) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				zap.L().Error("k8s event watcher recovered from panic", zap.Any("error", r))
			}
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var lastEventTime time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				events, err := k8sReader.ListEvents(ctx, copilotk8s.EventQuery{Limit: 10})
				if err != nil {
					zap.L().Warn("k8s event watcher list events failed", zap.Error(err))
					continue
				}
				for i := len(events) - 1; i >= 0; i-- {
					e := events[i]
					if !lastEventTime.IsZero() && e.LastSeen.After(lastEventTime) {
						msg, _ := json.Marshal(map[string]interface{}{
							"type": "k8s_event",
							"data": e,
						})
						hub.Broadcast(msg)
					}
				}
				if len(events) > 0 {
					lastEventTime = events[0].LastSeen
				}
			}
		}
	}()
}
