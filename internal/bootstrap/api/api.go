package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/router"
	"github.com/05allan1213/CloudOps-Copilot/internal/startup"
)

type API struct {
	cfg            APIConfig
	infra          *di.Infra
	userServer     *http.Server
	internalServer *http.Server
}

func NewAPI(ctx context.Context, cfg APIConfig) (*API, error) {
	cfg = cfg.normalized()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	application := cfg.Application
	// The V3 API owns HTTP ingress and MySQL-backed commands/queries only.
	// Redis cache/rate-limit and Kafka event production remain legacy surfaces
	// and must never be initialized by this process.
	infra, err := startup.InitInfra(ctx, application, startup.InfraOptions{ServiceName: "cloudops-api"})
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*API, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), application.ShutdownTimeout)
		defer cancel()
		return nil, errors.Join(cause, closeInfra(cleanupCtx, infra))
	}
	var runtimeGuard *cutover.RuntimeGuard
	if infra.MySQL != nil && infra.MySQL.Enabled() && infra.MySQL.SQLDB() != nil {
		runtimeGuard, err = cutover.NewSQLRuntimeGuard(infra.MySQL.SQLDB(), cfg.RuntimeGeneration)
		if err != nil {
			return fail(fmt.Errorf("initialize API runtime generation guard: %w", err))
		}
		// Preserve the existing unready-but-non-mutating behavior for an empty or
		// old schema. Once the schema is current, marker ambiguity is a startup
		// refusal before either listener is created.
		guardCtx, guardCancel := context.WithTimeout(ctx, application.MySQLStartupTimeout)
		schemaReadyErr := infra.MySQL.Ready(guardCtx)
		if schemaReadyErr == nil {
			err = runtimeGuard.Check(guardCtx)
		}
		guardCancel()
		if err != nil {
			return fail(fmt.Errorf("enforce API runtime generation: %w", err))
		}
	}
	runtimeReadiness := func(ctx context.Context) error {
		if runtimeGuard == nil {
			return errors.New("runtime generation guard requires MySQL")
		}
		return runtimeGuard.Check(ctx)
	}
	container, err := startup.InitAPIContainer(&application, infra, runtimeReadiness)
	if err != nil {
		return fail(err)
	}
	k8sReader, k8sClient, err := startup.InitK8sRuntime(application)
	if err != nil {
		return fail(err)
	}
	if err := startup.InitAPIApplications(application, container, k8sReader, k8sClient); err != nil {
		return fail(err)
	}
	userHandler, err := router.NewRouter(application, container.Dependencies())
	if err != nil {
		return fail(fmt.Errorf("create API router: %w", err))
	}
	internalHandler, err := router.NewInternalRouter(application, container.Dependencies())
	if err != nil {
		return fail(fmt.Errorf("create INTERNAL API router: %w", err))
	}
	return &API{
		cfg:   cfg,
		infra: infra,
		userServer: &http.Server{
			Addr:              application.ListenAddr,
			Handler:           userHandler,
			ReadHeaderTimeout: application.HTTPReadHeaderTimeout,
			ReadTimeout:       application.HTTPReadTimeout,
			WriteTimeout:      application.HTTPWriteTimeout,
			IdleTimeout:       application.HTTPIdleTimeout,
		},
		internalServer: &http.Server{
			Addr:              cfg.InternalListenAddr,
			Handler:           internalHandler,
			ReadHeaderTimeout: application.HTTPReadHeaderTimeout,
			ReadTimeout:       application.HTTPReadTimeout,
			WriteTimeout:      application.HTTPWriteTimeout,
			IdleTimeout:       application.HTTPIdleTimeout,
		},
	}, nil
}

func (a *API) Serve(ctx context.Context, userListener, internalListener net.Listener) error {
	if userListener == nil || internalListener == nil {
		return errors.New("both user and INTERNAL API listeners are required")
	}
	defer func() { _ = userListener.Close() }()
	defer func() { _ = internalListener.Close() }()
	serverErr := make(chan error, 2)
	serve := func(server *http.Server, listener net.Listener) {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}
	go serve(a.userServer, userListener)
	go serve(a.internalServer, internalListener)

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		runErr = err
	}

	timeout := a.cfg.Application.ShutdownTimeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return errors.Join(
		runErr,
		a.userServer.Shutdown(shutdownCtx),
		a.internalServer.Shutdown(shutdownCtx),
		closeInfra(shutdownCtx, a.infra),
	)
}

func RunAPI(ctx context.Context) error {
	cfg, err := LoadAPIConfig()
	if err != nil {
		return err
	}
	log, err := logger.Init("cloudops-api")
	if err != nil {
		return err
	}
	defer logger.Sync(log)
	application, err := NewAPI(ctx, cfg)
	if err != nil {
		return err
	}
	userListener, err := net.Listen("tcp", cfg.Application.ListenAddr)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
		defer cancel()
		return errors.Join(err, closeInfra(cleanupCtx, application.infra))
	}
	internalListener, err := net.Listen("tcp", cfg.InternalListenAddr)
	if err != nil {
		_ = userListener.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
		defer cancel()
		return errors.Join(err, closeInfra(cleanupCtx, application.infra))
	}
	return application.Serve(ctx, userListener, internalListener)
}
