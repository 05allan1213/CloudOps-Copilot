package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/health"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
)

type taskRunner interface {
	Start(context.Context) error
	StopClaims()
	Shutdown(context.Context) error
	Ready(context.Context) error
}

type Worker struct {
	cfg        WorkerConfig
	mysql      *database.MySQL
	runner     taskRunner
	management *http.Server
	ready      atomic.Bool
	mysqlReady func(context.Context) error

	stateMu        sync.RWMutex
	runnerStartErr error
	runnerStarted  bool
}

type runtimeGuardedTaskStore struct {
	asyncjob.Store
	runtimeReady func(context.Context) error
}

func (s runtimeGuardedTaskStore) Ready(ctx context.Context) error {
	if s.Store == nil {
		return errors.New("async task store is required")
	}
	if err := s.Store.Ready(ctx); err != nil {
		return err
	}
	if s.runtimeReady == nil {
		return errors.New("runtime generation guard is required")
	}
	return s.runtimeReady(ctx)
}

func NewWorker(ctx context.Context, cfg WorkerConfig) (*Worker, error) {
	cfg = cfg.normalized()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	application := cfg.Application
	hasInlineOperations := hasTaskOperations(cfg.TaskOperations)
	if !hasInlineOperations && cfg.TaskOperationFactory == nil {
		return nil, errors.New("initialize async task handlers: operations are not migrated and production task operation factory is required")
	}
	mysqlCtx, cancelMySQL := context.WithTimeout(ctx, application.MySQLStartupTimeout)
	mysql, err := database.OpenMySQL(mysqlCtx, database.MySQLConfig{
		Host: application.MySQLHost, Port: application.MySQLPort, User: application.MySQLUser,
		Password: application.MySQLPassword, Database: application.MySQLDatabase,
		PingTimeout: application.MySQLPingTimeout,
	})
	cancelMySQL()
	if err != nil {
		return nil, fmt.Errorf("initialize worker MySQL: %w", err)
	}
	if mysql == nil || !mysql.Enabled() || mysql.SQLDB() == nil {
		return nil, errors.New("cloudops-worker requires MySQL")
	}
	repository, err := asyncjob.NewRepository(mysql.SQLDB())
	if err != nil {
		_ = mysql.Close()
		return nil, err
	}
	runtimeGuard, err := cutover.NewSQLRuntimeGuard(mysql.SQLDB(), cfg.RuntimeGeneration)
	if err != nil {
		_ = mysql.Close()
		return nil, fmt.Errorf("initialize Worker runtime generation guard: %w", err)
	}
	guardCtx, guardCancel := context.WithTimeout(ctx, application.MySQLStartupTimeout)
	// Keep the existing migrate-without-restart behavior for an empty or old
	// schema. Once the schema is current, marker ambiguity is a startup refusal
	// before operation assembly or any Runner claim loop can be constructed.
	schemaReadyErr := mysql.Ready(guardCtx)
	if schemaReadyErr == nil {
		err = runtimeGuard.Check(guardCtx)
	}
	guardCancel()
	if err != nil {
		_ = mysql.Close()
		return nil, fmt.Errorf("enforce Worker runtime generation: %w", err)
	}
	var handlers map[asyncjob.TaskType]asyncjob.Handler
	if hasInlineOperations {
		handlers, err = taskhandler.NewRuntime(cfg.TaskOperations)
		if err != nil {
			_ = mysql.Close()
			return nil, fmt.Errorf("initialize async task handlers: %w", err)
		}
	} else {
		operations, buildErr := cfg.TaskOperationFactory.Build(ctx, mysql.SQLDB(), repository)
		if buildErr != nil {
			_ = mysql.Close()
			return nil, fmt.Errorf("assemble production async task operations: %w", buildErr)
		}
		handlers, buildErr = taskhandler.NewRuntime(operations)
		if buildErr != nil {
			_ = mysql.Close()
			return nil, fmt.Errorf("initialize production async task handlers: %w", buildErr)
		}
	}
	runner, err := asyncjob.NewRunner(asyncjob.RunnerConfig{
		Owner: cfg.Async.WorkerID,
		Store: runtimeGuardedTaskStore{
			Store: repository, runtimeReady: runtimeGuard.Check,
		},
		Handlers:     handlers,
		Pools:        asyncPoolConfigs(cfg.Async),
		DrainTimeout: cfg.Async.DrainTimeout,
		CancelWait:   cfg.Async.ExitDeadline - cfg.Async.DrainTimeout,
	})
	if err != nil {
		_ = mysql.Close()
		return nil, err
	}
	metrics := middleware.NewMetrics()
	worker := &Worker{
		cfg: cfg, mysql: mysql, runner: runner,
		mysqlReady: func(ctx context.Context) error {
			if err := mysql.Ready(ctx); err != nil {
				return err
			}
			return runtimeGuard.Check(ctx)
		},
	}
	worker.management = &http.Server{
		Addr: cfg.ManagementAddr,
		Handler: health.NewHandler(health.Options{
			Process: "cloudops-worker", Timeout: application.ReadyTimeout,
			Ready: worker.readiness, Metrics: metrics.HTTPHandler(),
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
	return worker, nil
}

func asyncPoolConfigs(cfg AsyncWorkerConfig) map[asyncjob.Queue]asyncjob.PoolConfig {
	return map[asyncjob.Queue]asyncjob.PoolConfig{
		asyncjob.QueueInvestigate: {
			MaxInFlight: cfg.InvestigateMaxInFlight, LeaseDuration: cfg.InvestigateLease,
			HeartbeatPeriod: cfg.InvestigateHeartbeat, HandlerDeadline: cfg.InvestigateHandlerDeadline,
			ExternalDeadline: cfg.InvestigateExternalDeadline,
		},
		asyncjob.QueueDeliver: {
			MaxInFlight: cfg.DeliverMaxInFlight, LeaseDuration: cfg.DeliverLease,
			HeartbeatPeriod: cfg.DeliverHeartbeat, HandlerDeadline: cfg.DeliverHandlerDeadline,
			ExternalDeadline: cfg.DeliverExternalDeadline,
		},
		asyncjob.QueueObserve: {
			MaxInFlight: cfg.ObserveMaxInFlight, LeaseDuration: cfg.ObserveLease,
			HeartbeatPeriod: cfg.ObserveHeartbeat, HandlerDeadline: cfg.ObserveHandlerDeadline,
			ExternalDeadline: cfg.ObserveExternalDeadline,
		},
		asyncjob.QueueVerify: {
			MaxInFlight: cfg.VerifyMaxInFlight, LeaseDuration: cfg.VerifyLease,
			HeartbeatPeriod: cfg.VerifyHeartbeat, HandlerDeadline: cfg.VerifyHandlerDeadline,
			ExternalDeadline: cfg.VerifyExternalDeadline,
		},
	}
}

func (w *Worker) readiness(ctx context.Context) error {
	if !w.ready.Load() {
		return errors.New("async task claim loops are not initialized")
	}
	if w.mysqlReady == nil {
		return errors.New("mysql is not initialized")
	}
	if err := w.mysqlReady(ctx); err != nil {
		return fmt.Errorf("mysql readiness: %w", err)
	}
	w.stateMu.RLock()
	startErr := w.runnerStartErr
	started := w.runnerStarted
	w.stateMu.RUnlock()
	if startErr != nil {
		return fmt.Errorf("async task runtime startup: %w", startErr)
	}
	if !started || w.runner == nil {
		return errors.New("async task runner is not initialized")
	}
	if err := w.runner.Ready(ctx); err != nil {
		return fmt.Errorf("async task runtime readiness: %w", err)
	}
	return nil
}

func (w *Worker) Serve(ctx context.Context, listener net.Listener) error {
	serverErr := make(chan error, 1)
	go func() {
		if err := w.management.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	runnerErr := w.runner.Start(ctx)
	w.stateMu.Lock()
	w.runnerStartErr = runnerErr
	w.runnerStarted = runnerErr == nil
	w.stateMu.Unlock()
	if runnerErr != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), w.cfg.Async.ExitDeadline)
		defer cancel()
		return errors.Join(fmt.Errorf("start async task runtime: %w", runnerErr), w.management.Shutdown(shutdownCtx), w.mysql.Close())
	}
	w.ready.Store(true)

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		runErr = err
	}
	w.ready.Store(false)

	// Claim loops stop synchronously before any other shutdown work begins.
	w.stateMu.RLock()
	started := w.runnerStarted
	w.stateMu.RUnlock()
	if started {
		w.runner.StopClaims()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), w.cfg.Async.ExitDeadline)
	defer cancel()
	serverShutdownErr := w.management.Shutdown(shutdownCtx)
	var runnerShutdownErr error
	if started {
		runnerShutdownErr = w.runner.Shutdown(shutdownCtx)
	}
	return errors.Join(runErr, serverShutdownErr, runnerShutdownErr, w.mysql.Close())
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
		_ = worker.mysql.Close()
		return err
	}
	return worker.Serve(ctx, listener)
}
