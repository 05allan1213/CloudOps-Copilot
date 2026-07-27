package operation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RunnerConfig struct {
	Owner             string
	Repository        *Repository
	Adapters          *AdapterRegistry
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ExecutionTimeout  time.Duration
}

type Runner struct {
	config RunnerConfig

	started       atomic.Bool
	claimsStopped atomic.Bool
	claimCancel   context.CancelFunc
	runCancel     context.CancelFunc
	wait          sync.WaitGroup
}

func NewRunner(config RunnerConfig) (*Runner, error) {
	if strings.TrimSpace(config.Owner) == "" || config.Repository == nil || config.Adapters == nil {
		return nil, ErrInvalidArgument
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 250 * time.Millisecond
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 60 * time.Second
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatInterval > config.LeaseDuration/3 {
		return nil, ErrInvalidArgument
	}
	if config.ExecutionTimeout <= 0 || config.ExecutionTimeout >= config.LeaseDuration {
		return nil, ErrInvalidArgument
	}
	return &Runner{config: config}, nil
}

func (r *Runner) Start(ctx context.Context) error {
	if r == nil || !r.started.CompareAndSwap(false, true) {
		return errors.New("operation runner is already started")
	}
	if err := r.config.Repository.Ready(ctx); err != nil {
		r.started.Store(false)
		return err
	}
	claimCtx, claimCancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	r.claimCancel, r.runCancel = claimCancel, runCancel
	r.wait.Add(1)
	go r.claimLoop(claimCtx, runCtx)
	go func() {
		select {
		case <-ctx.Done():
			r.StopClaims()
		case <-claimCtx.Done():
		}
	}()
	return nil
}

func (r *Runner) StopClaims() {
	if r == nil || !r.claimsStopped.CompareAndSwap(false, true) {
		return
	}
	if r.claimCancel != nil {
		r.claimCancel()
	}
}

func (r *Runner) Shutdown(ctx context.Context) error {
	if r == nil || !r.started.Load() {
		return nil
	}
	r.StopClaims()
	done := make(chan struct{})
	go func() {
		r.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		if r.runCancel != nil {
			r.runCancel()
		}
		return ctx.Err()
	}
}

func (r *Runner) Ready(ctx context.Context) error {
	if r == nil || !r.started.Load() || r.claimsStopped.Load() {
		return errors.New("operation runner is not accepting claims")
	}
	return r.config.Repository.Ready(ctx)
}

func (r *Runner) claimLoop(claimCtx, runCtx context.Context) {
	defer r.wait.Done()
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-claimCtx.Done():
			return
		case <-ticker.C:
		}
		lease, found, err := r.config.Repository.Claim(claimCtx, r.config.Owner, r.config.LeaseDuration)
		if err != nil || !found {
			continue
		}
		r.wait.Add(1)
		go func(value Lease) {
			defer r.wait.Done()
			r.execute(runCtx, value)
		}(lease)
	}
}

func (r *Runner) execute(base context.Context, lease Lease) {
	ctx, cancel := context.WithTimeout(base, r.config.ExecutionTimeout)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go r.heartbeat(ctx, lease, heartbeatDone)

	subject, err := r.config.Repository.SubjectForExecution(ctx, lease)
	var adapter Adapter
	var prepared PreparedEffect
	if err == nil {
		adapter, err = r.config.Adapters.Resolve(subject.OperationType)
	}
	if err == nil {
		prepared, err = adapter.Prepare(ctx, subject)
	}
	if err == nil {
		err = r.config.Repository.RecordPrepared(ctx, lease, prepared)
	}
	if err == nil {
		err = r.config.Repository.BeginEffect(ctx, lease, subject, prepared)
	}
	var observation Observation
	if err == nil {
		observation, err = adapter.Apply(ctx, subject, prepared)
	}
	if err == nil {
		err = r.config.Repository.Complete(ctx, lease, observation)
	}
	cancel()
	<-heartbeatDone
	if err == nil || errors.Is(err, ErrLeaseLost) {
		return
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer auditCancel()
	_ = r.config.Repository.Fail(auditCtx, lease, err)
}

func (r *Runner) heartbeat(ctx context.Context, lease Lease, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.config.Repository.Heartbeat(ctx, lease, r.config.LeaseDuration); err != nil {
				return
			}
		}
	}
}
