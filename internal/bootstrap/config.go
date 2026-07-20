package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/configutil"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
)

// TaskOperationFactory assembles the five subject-bound production
// operations after the DML MySQL connection and unified task repository have
// been initialized. Keeping this boundary explicit lets startup validate all
// provider identities before the Runner can claim work without teaching the
// generic runtime how individual providers are constructed.
type TaskOperationFactory interface {
	Build(context.Context, *sql.DB, *asyncjob.Repository) (taskhandler.Config, error)
}

type taskOperationFactoryValidator interface {
	Validate() error
}

type TaskOperationFactoryFunc func(context.Context, *sql.DB, *asyncjob.Repository) (taskhandler.Config, error)

func (f TaskOperationFactoryFunc) Build(ctx context.Context, db *sql.DB, tasks *asyncjob.Repository) (taskhandler.Config, error) {
	return f(ctx, db, tasks)
}

type WorkerConfig struct {
	Application          appconfig.Config
	Async                AsyncWorkerConfig
	TaskOperations       taskhandler.Config
	TaskOperationFactory TaskOperationFactory
	ManagementAddr       string
	ReadHeaderTimeout    time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
}

func LoadWorkerConfig() (WorkerConfig, error) {
	application := appconfig.Load()
	asyncConfig, err := LoadAsyncWorkerConfig()
	if err != nil {
		return WorkerConfig{}, err
	}
	result := WorkerConfig{
		Application:       application,
		Async:             asyncConfig,
		ManagementAddr:    configutil.String("WORKER_MANAGEMENT_ADDR", ":8081"),
		ReadHeaderTimeout: application.HTTPReadHeaderTimeout,
		ReadTimeout:       application.HTTPReadTimeout,
		WriteTimeout:      application.HTTPWriteTimeout,
		IdleTimeout:       application.HTTPIdleTimeout,
	}
	if err := result.Validate(); err != nil {
		return WorkerConfig{}, err
	}
	return result, nil
}

func (c WorkerConfig) Validate() error {
	c = c.normalized()
	if err := c.Application.Validate(); err != nil {
		return fmt.Errorf("invalid cloudops-worker config: %w", err)
	}
	if err := configutil.ValidateListenAddr("WORKER_MANAGEMENT_ADDR", c.ManagementAddr); err != nil {
		return err
	}
	if c.ReadHeaderTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 {
		return fmt.Errorf("worker management HTTP timeouts must be positive")
	}
	if err := c.Async.Validate(); err != nil {
		return fmt.Errorf("invalid async worker config: %w", err)
	}
	if validator, ok := c.TaskOperationFactory.(taskOperationFactoryValidator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid production task operation config: %w", err)
		}
	}
	return nil
}

func (c WorkerConfig) normalized() WorkerConfig {
	if c.Async.WorkerID == "" {
		c.Async = DefaultAsyncWorkerConfig()
	}
	return c
}

func hasTaskOperations(config taskhandler.Config) bool {
	return config.InvestigationStep != nil || config.RemediationPrepare != nil || config.ChangeEnsurePR != nil ||
		config.DeliveryObserve != nil || config.VerificationAdvance != nil
}
