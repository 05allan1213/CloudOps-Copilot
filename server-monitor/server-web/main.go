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
	"syscall"
	"time"

	"go.uber.org/zap"

	promclient "server-web/prometheus"
	"server-web/pubsub"
	rediscache "server-web/redis"
	ws "server-web/websocket"

	"server-monitor/pkg/logger"
	"server-monitor/pkg/shutdown"
)

type wsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func main() {
	log, err := logger.Init("server-web")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := initApp(ctx)
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
	case <-app.ctx.Done():
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
	if app.subscriberDone != nil {
		<-app.subscriberDone
	}
	if app.diagnosisDone != nil {
		<-app.diagnosisDone
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
