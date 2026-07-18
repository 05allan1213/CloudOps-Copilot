package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/router"
	"github.com/05allan1213/CloudOps-Copilot/internal/startup"
)

type API struct {
	cfg    APIConfig
	infra  *di.Infra
	server *http.Server
}

func NewAPI(ctx context.Context, cfg APIConfig) (*API, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	application := cfg.Application
	infra, err := startup.InitInfra(ctx, application, startup.InfraOptions{ServiceName: "cloudops-api", EnableKafka: true})
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*API, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), application.ShutdownTimeout)
		defer cancel()
		return nil, errors.Join(cause, closeInfra(cleanupCtx, infra))
	}
	container, err := startup.InitAPIContainer(&application, infra)
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
	handler, err := router.NewRouter(application, container.Dependencies())
	if err != nil {
		return fail(fmt.Errorf("create API router: %w", err))
	}
	return &API{
		cfg:   cfg,
		infra: infra,
		server: &http.Server{
			Addr:              application.ListenAddr,
			Handler:           handler,
			ReadHeaderTimeout: application.HTTPReadHeaderTimeout,
			ReadTimeout:       application.HTTPReadTimeout,
			WriteTimeout:      application.HTTPWriteTimeout,
			IdleTimeout:       application.HTTPIdleTimeout,
		},
	}, nil
}

func (a *API) Serve(ctx context.Context, listener net.Listener) error {
	serverErr := make(chan error, 1)
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	timeout := a.cfg.Application.ShutdownTimeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return errors.Join(runErr, a.server.Shutdown(shutdownCtx), closeInfra(shutdownCtx, a.infra))
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
	listener, err := net.Listen("tcp", cfg.Application.ListenAddr)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
		defer cancel()
		return errors.Join(err, closeInfra(cleanupCtx, application.infra))
	}
	return application.Serve(ctx, listener)
}
