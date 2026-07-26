package asyncjob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultPollInterval = 250 * time.Millisecond
	DefaultDrainTimeout = 45 * time.Second
	DefaultCancelWait   = 10 * time.Second
	maxExitBudget       = 55 * time.Second
)

type PoolConfig struct {
	MaxInFlight      int
	LeaseDuration    time.Duration
	HeartbeatPeriod  time.Duration
	HandlerDeadline  time.Duration
	ExternalDeadline time.Duration
}

func DefaultPoolConfigs() map[Queue]PoolConfig {
	return map[Queue]PoolConfig{
		QueueInvestigate: {
			MaxInFlight:      2,
			LeaseDuration:    90 * time.Second,
			HeartbeatPeriod:  20 * time.Second,
			HandlerDeadline:  45 * time.Second,
			ExternalDeadline: 40 * time.Second,
		},
		QueueDeliver: {
			MaxInFlight:      1,
			LeaseDuration:    60 * time.Second,
			HeartbeatPeriod:  15 * time.Second,
			HandlerDeadline:  30 * time.Second,
			ExternalDeadline: 25 * time.Second,
		},
		QueueObserve: {
			MaxInFlight:      2,
			LeaseDuration:    45 * time.Second,
			HeartbeatPeriod:  10 * time.Second,
			HandlerDeadline:  20 * time.Second,
			ExternalDeadline: 15 * time.Second,
		},
		QueueVerify: {
			MaxInFlight:      2,
			LeaseDuration:    45 * time.Second,
			HeartbeatPeriod:  10 * time.Second,
			HandlerDeadline:  20 * time.Second,
			ExternalDeadline: 15 * time.Second,
		},
	}
}

func (c PoolConfig) Validate(queue Queue) error {
	if !queue.Valid() {
		return fmt.Errorf("invalid async task queue %q", queue)
	}
	if c.MaxInFlight <= 0 {
		return fmt.Errorf("%s max-in-flight must be positive", queue)
	}
	if c.LeaseDuration < time.Microsecond || c.HeartbeatPeriod < time.Microsecond || c.HandlerDeadline < time.Microsecond || c.ExternalDeadline < time.Microsecond {
		return fmt.Errorf("%s timing values must be positive", queue)
	}
	if c.HeartbeatPeriod > c.LeaseDuration/3 {
		return fmt.Errorf("%s heartbeat period must be no greater than one third of the lease", queue)
	}
	if c.ExternalDeadline >= c.HandlerDeadline {
		return fmt.Errorf("%s external deadline must be less than handler deadline", queue)
	}
	if c.HandlerDeadline >= c.LeaseDuration {
		return fmt.Errorf("%s handler deadline must be less than lease duration", queue)
	}
	return nil
}

type RunnerConfig struct {
	Owner        string
	Store        Store
	Handlers     map[TaskType]Handler
	Pools        map[Queue]PoolConfig
	RetryBackoff BackoffPolicy
	PollInterval time.Duration
	DrainTimeout time.Duration
	CancelWait   time.Duration
	Boundary     Boundary
}

// Boundary binds immutable execution context after a task is claimed and
// before its handler runs. It must not perform an external Provider call.
type Boundary interface {
	Bind(context.Context, Execution) (context.Context, error)
}

type BoundaryFunc func(context.Context, Execution) (context.Context, error)

func (f BoundaryFunc) Bind(ctx context.Context, execution Execution) (context.Context, error) {
	return f(ctx, execution)
}

func (c *RunnerConfig) applyDefaults() {
	if c.RetryBackoff.isZero() {
		c.RetryBackoff = DefaultRetryBackoffPolicy()
	}
	if c.PollInterval == 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.DrainTimeout == 0 {
		c.DrainTimeout = DefaultDrainTimeout
	}
	if c.CancelWait == 0 {
		c.CancelWait = DefaultCancelWait
	}
	defaults := DefaultPoolConfigs()
	if c.Pools == nil {
		c.Pools = defaults
		return
	}
	for _, queue := range queues {
		if _, ok := c.Pools[queue]; !ok {
			c.Pools[queue] = defaults[queue]
		}
	}
}

func (c RunnerConfig) Validate() error {
	if strings.TrimSpace(c.Owner) == "" || len(c.Owner) > 128 {
		return errors.New("async task runner owner is required and must not exceed 128 bytes")
	}
	if c.Store == nil {
		return errors.New("async task runner store is required")
	}
	if c.PollInterval <= 0 || c.PollInterval > time.Minute {
		return errors.New("async task poll interval must be positive and no greater than one minute")
	}
	if err := c.RetryBackoff.Validate(); err != nil {
		return fmt.Errorf("invalid async task retry backoff: %w", err)
	}
	if c.DrainTimeout <= 0 || c.DrainTimeout > DefaultDrainTimeout {
		return errors.New("async task drain timeout must be positive and no greater than 45 seconds")
	}
	if c.CancelWait <= 0 || c.DrainTimeout+c.CancelWait > maxExitBudget {
		return errors.New("async task drain and cancel windows must fit within 55 seconds")
	}
	if len(c.Pools) != len(queues) {
		return errors.New("async task runner requires exactly four pool configurations")
	}
	for queue, pool := range c.Pools {
		if err := pool.Validate(queue); err != nil {
			return err
		}
	}
	if len(c.Handlers) != len(taskTypes) {
		return errors.New("async task runner requires exactly the five frozen task handlers")
	}
	for taskType, handler := range c.Handlers {
		if !taskType.Valid() {
			return fmt.Errorf("unsupported async task handler %q", taskType)
		}
		if handler == nil {
			return fmt.Errorf("async task handler %q is nil", taskType)
		}
	}
	for _, taskType := range taskTypes {
		if c.Handlers[taskType] == nil {
			return fmt.Errorf("missing async task handler %q", taskType)
		}
	}
	return nil
}
