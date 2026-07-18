package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/health"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/startup"
	legacyworker "github.com/05allan1213/CloudOps-Copilot/internal/startup/legacyworker"
)

type legacyLoop interface {
	Start(context.Context)
	Stop()
}

type Worker struct {
	cfg        WorkerConfig
	infra      *di.Infra
	loops      []legacyLoop
	management *http.Server
	ready      atomic.Bool
	mysqlReady func(context.Context) error
}

func NewWorker(ctx context.Context, cfg WorkerConfig) (*Worker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	application := cfg.Application
	infra, err := startup.InitInfra(ctx, application, startup.InfraOptions{ServiceName: "cloudops-worker"})
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*Worker, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), application.ShutdownTimeout)
		defer cancel()
		return nil, errors.Join(cause, closeInfra(cleanupCtx, infra))
	}
	container, err := startup.InitWorkerContainer(&application, infra)
	if err != nil {
		return fail(err)
	}
	k8sReader, k8sClient, err := startup.InitK8sRuntime(application)
	if err != nil {
		return fail(err)
	}
	container.Handler.SetIncidentK8sReader(k8sReader)
	loops := make([]legacyLoop, 0, 3)
	agentWorker, err := legacyworker.InitAgentRuntime(ctx, application, container, k8sread.Deps{Reader: k8sReader, Client: k8sClient})
	if err != nil {
		return fail(err)
	}
	if agentWorker != nil {
		loops = append(loops, agentWorker)
	}
	if !application.FastDemoEnabled {
		remediationWorker, err := legacyworker.InitRemediation(application, container)
		if err != nil {
			return fail(err)
		}
		if remediationWorker != nil {
			loops = append(loops, remediationWorker)
		}
		deliveryWorker, err := legacyworker.InitDeliveryVerification(application, container)
		if err != nil {
			return fail(err)
		}
		if deliveryWorker != nil {
			loops = append(loops, deliveryWorker)
		}
	}

	worker := &Worker{cfg: cfg, infra: infra, loops: loops}
	if infra != nil && infra.MySQL != nil && infra.MySQL.Enabled() {
		worker.mysqlReady = infra.MySQL.Ready
	}
	worker.management = &http.Server{
		Addr: cfg.ManagementAddr,
		Handler: health.NewHandler(health.Options{
			Process: "cloudops-worker",
			Timeout: application.ReadyTimeout,
			Ready:   worker.readiness,
			Metrics: container.Metrics.HTTPHandler(),
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
	return worker, nil
}

func (w *Worker) readiness(ctx context.Context) error {
	if !w.ready.Load() {
		return errors.New("legacy worker loops are not initialized")
	}
	if w.mysqlReady == nil {
		return errors.New("mysql is not initialized")
	}
	if err := w.mysqlReady(ctx); err != nil {
		return fmt.Errorf("mysql readiness: %w", err)
	}
	return nil
}

func (w *Worker) Serve(ctx context.Context, listener net.Listener) error {
	for _, loop := range w.loops {
		loop.Start(ctx)
	}
	w.ready.Store(true)

	serverErr := make(chan error, 1)
	go func() {
		if err := w.management.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		runErr = err
	}
	w.ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), w.cfg.Application.ShutdownTimeout)
	defer cancel()
	loopStop := make(chan struct{})
	go func() {
		for i := len(w.loops) - 1; i >= 0; i-- {
			w.loops[i].Stop()
		}
		close(loopStop)
	}()
	serverShutdownErr := w.management.Shutdown(shutdownCtx)
	var loopStopErr error
	select {
	case <-loopStop:
	case <-shutdownCtx.Done():
		loopStopErr = fmt.Errorf("stop legacy worker loops: %w", shutdownCtx.Err())
	}
	return errors.Join(runErr, serverShutdownErr, loopStopErr, closeInfra(shutdownCtx, w.infra))
}

func RunWorker(ctx context.Context) error {
	cfg, err := LoadWorkerConfig()
	if err != nil {
		return err
	}
	log, err := logger.Init("cloudops-worker")
	if err != nil {
		return err
	}
	defer logger.Sync(log)
	worker, err := NewWorker(ctx, cfg)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.ManagementAddr)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
		defer cancel()
		return errors.Join(err, closeInfra(cleanupCtx, worker.infra))
	}
	return worker.Serve(ctx, listener)
}
