package asyncjob

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

var (
	errHandlerDeadline = errors.New("async task handler deadline exceeded")
	errShutdownCancel  = errors.New("async task handler cancelled during shutdown")
	errHandlerPanic    = errors.New("async task handler panicked")
)

type Runner struct {
	cfg RunnerConfig

	stateMu        sync.Mutex
	started        bool
	claimsStopped  bool
	claimCtx       context.Context
	cancelClaims   context.CancelFunc
	handlerCtx     context.Context
	cancelHandlers context.CancelCauseFunc
	claimsDone     chan struct{}
	claimWG        sync.WaitGroup
	stopOnce       sync.Once

	semaphores map[Queue]chan struct{}
	activeMu   sync.Mutex
	active     map[Queue]int
	activeZero chan struct{}
}

func NewRunner(config RunnerConfig) (*Runner, error) {
	config.applyDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	zero := make(chan struct{})
	close(zero)
	runner := &Runner{
		cfg:        config,
		semaphores: make(map[Queue]chan struct{}, len(queues)),
		active:     make(map[Queue]int, len(queues)),
		activeZero: zero,
		claimsDone: make(chan struct{}),
	}
	for _, queue := range queues {
		runner.semaphores[queue] = make(chan struct{}, config.Pools[queue].MaxInFlight)
	}
	return runner, nil
}

// Start launches exactly one claim loop per queue and returns immediately.
// Store/schema availability is a readiness concern: claim failures back off in
// the pool loop so a migrated database can become ready without restarting the
// process.
func (r *Runner) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("async task runner context is required")
	}
	r.stateMu.Lock()
	if r.started {
		r.stateMu.Unlock()
		return errors.New("async task runner is already started")
	}
	if r.claimsStopped {
		r.stateMu.Unlock()
		return ErrClaimsStopped
	}
	r.claimCtx, r.cancelClaims = context.WithCancel(ctx)
	r.handlerCtx, r.cancelHandlers = context.WithCancelCause(context.WithoutCancel(ctx))
	r.started = true
	r.claimWG.Add(len(queues))
	for _, queue := range queues {
		go r.runPool(queue)
	}
	claimsDone := r.claimsDone
	r.stateMu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			r.StopClaims()
		case <-claimsDone:
		}
	}()
	return nil
}

// StopClaims synchronously stops all four claim loops. Running handlers are
// deliberately left alive for the bounded drain window.
func (r *Runner) StopClaims() {
	r.stopOnce.Do(func() {
		r.stateMu.Lock()
		r.claimsStopped = true
		cancel := r.cancelClaims
		started := r.started
		r.stateMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if started {
			r.claimWG.Wait()
		}
		close(r.claimsDone)
	})
}

// Drain stops claims and waits for every in-flight handler. It never marks a
// task succeeded or failed merely because the wait context expires.
func (r *Runner) Drain(ctx context.Context) error {
	if ctx == nil {
		return errors.New("async task drain context is required")
	}
	r.stateMu.Lock()
	started := r.started
	r.stateMu.Unlock()
	if !started {
		return ErrRunnerNotStarted
	}
	r.StopClaims()
	if err := r.waitActive(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrDrainTimeout, err)
	}
	return nil
}

// CancelActive cancels handler contexts and heartbeat loops. The abandoned
// running tasks retain no fabricated terminal outcome and wait for takeover.
func (r *Runner) CancelActive() {
	r.stateMu.Lock()
	cancel := r.cancelHandlers
	r.stateMu.Unlock()
	if cancel != nil {
		cancel(errShutdownCancel)
	}
}

// Shutdown enforces the 45-second drain and 55-second total default budget.
func (r *Runner) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("async task shutdown context is required")
	}
	r.StopClaims()
	drainCtx, cancelDrain := boundedTimeout(ctx, r.cfg.DrainTimeout)
	drainErr := r.waitActive(drainCtx)
	cancelDrain()
	if drainErr == nil {
		r.CancelActive()
		return nil
	}
	r.CancelActive()
	cancelCtx, cancelWait := boundedTimeout(ctx, r.cfg.CancelWait)
	waitErr := r.waitActive(cancelCtx)
	cancelWait()
	if waitErr != nil {
		return errors.Join(ErrDrainTimeout, fmt.Errorf("handlers did not stop after cancellation: %w", waitErr))
	}
	return ErrDrainTimeout
}

func (r *Runner) Ready(ctx context.Context) error {
	r.stateMu.Lock()
	started := r.started
	stopped := r.claimsStopped
	r.stateMu.Unlock()
	if !started {
		return ErrRunnerNotStarted
	}
	if stopped {
		return ErrClaimsStopped
	}
	if err := r.cfg.Store.Ready(ctx); err != nil {
		return fmt.Errorf("async task store readiness: %w", err)
	}
	return nil
}

func (r *Runner) InFlight(queue Queue) int {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	return r.active[queue]
}

func (r *Runner) runPool(queue Queue) {
	defer r.claimWG.Done()
	pool := r.cfg.Pools[queue]
	semaphore := r.semaphores[queue]
	// Keep the process live but do not claim from an absent, partial, or
	// unsupported schema. Migrate can make the store ready without a restart.
	for {
		if err := r.cfg.Store.Ready(r.claimCtx); err == nil {
			break
		}
		if !waitContext(r.claimCtx, r.cfg.PollInterval) {
			return
		}
	}
	for {
		select {
		case semaphore <- struct{}{}:
		case <-r.claimCtx.Done():
			return
		}
		if r.claimCtx.Err() != nil {
			<-semaphore
			return
		}
		execution, err := r.cfg.Store.Claim(r.claimCtx, ClaimRequest{
			Queue:         queue,
			Owner:         r.cfg.Owner,
			LeaseDuration: pool.LeaseDuration,
		})
		if err != nil {
			<-semaphore
			if r.claimCtx.Err() != nil {
				return
			}
			if !waitContext(r.claimCtx, r.cfg.PollInterval) {
				return
			}
			continue
		}
		if execution == nil || !r.executionMatchesPool(queue, *execution) {
			<-semaphore
			continue
		}
		r.addActive(queue)
		go r.execute(pool, *execution, semaphore)
	}
}

func (r *Runner) executionMatchesPool(queue Queue, execution Execution) bool {
	expected, err := QueueForTaskType(execution.Task.Type)
	if err != nil || expected != queue || execution.Task.Queue != queue {
		return false
	}
	return execution.Lease.Validate() == nil && execution.Task.ID == execution.Lease.TaskID
}

func (r *Runner) execute(pool PoolConfig, execution Execution, semaphore chan struct{}) {
	defer func() {
		<-semaphore
		r.doneActive(execution.Task.Queue)
	}()

	baseCtx, cancelLease := context.WithCancelCause(r.handlerCtx)
	handlerCtx, cancelDeadline := context.WithTimeoutCause(baseCtx, pool.HandlerDeadline, errHandlerDeadline)
	handlerCtx = withExternalCallPolicy(handlerCtx, pool.ExternalDeadline)
	heartbeatCtx, cancelHeartbeat := context.WithCancel(handlerCtx)
	heartbeatDone := make(chan error, 1)
	go func() {
		err := r.heartbeat(heartbeatCtx, execution.Lease, pool)
		if err != nil {
			cancelLease(ErrLeaseLost)
		}
		heartbeatDone <- err
	}()

	var result Result
	var panicErr error
	if r.cfg.Boundary != nil {
		boundCtx, boundaryErr := r.cfg.Boundary.Bind(handlerCtx, execution)
		if boundaryErr != nil {
			result = RetryAfter(0, "configuration_unavailable", "bound Configuration Revision could not be observed", nil)
		} else {
			result, panicErr = callHandler(boundCtx, r.cfg.Handlers[execution.Task.Type], execution)
		}
	} else {
		result, panicErr = callHandler(handlerCtx, r.cfg.Handlers[execution.Task.Type], execution)
	}
	cancelHeartbeat()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		cancelLease(ErrLeaseLost)
	}
	cause := context.Cause(handlerCtx)
	cancelDeadline()
	cancelLease(nil)

	if panicErr != nil || errors.Is(cause, ErrLeaseLost) || errors.Is(cause, errShutdownCancel) {
		return
	}
	if errors.Is(cause, errHandlerDeadline) {
		if result.Validate() != nil || result.Disposition == DispositionSucceeded {
			result = RetryAfter(0, "handler_deadline", "handler deadline exceeded before a durable result", nil)
		}
	}
	if err := result.Validate(); err != nil {
		return
	}
	if result.Disposition == DispositionRetry {
		result.RetryAfter = r.cfg.RetryBackoff.Apply(execution.Lease.Attempt, result.RetryAfter)
	}
	resolveBudget := min(5*time.Second, pool.LeaseDuration-pool.HandlerDeadline)
	if context.Cause(r.handlerCtx) != nil {
		return
	}
	resolveCtx, cancelResolve := context.WithTimeout(r.handlerCtx, resolveBudget)
	defer cancelResolve()
	_ = r.cfg.Store.Resolve(resolveCtx, execution.Lease, result)
}

func (r *Runner) heartbeat(ctx context.Context, lease Lease, pool PoolConfig) error {
	ticker := time.NewTicker(pool.HeartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			heartbeatCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), min(pool.HeartbeatPeriod, 5*time.Second))
			err := r.cfg.Store.Heartbeat(heartbeatCtx, lease, pool.LeaseDuration)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func callHandler(ctx context.Context, handler Handler, execution Execution) (result Result, panicErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = fmt.Errorf("%w: %v\n%s", errHandlerPanic, recovered, debug.Stack())
		}
	}()
	return handler.Handle(ctx, execution), nil
}

func (r *Runner) addActive(queue Queue) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	if r.totalActiveLocked() == 0 {
		r.activeZero = make(chan struct{})
	}
	r.active[queue]++
}

func (r *Runner) doneActive(queue Queue) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	if r.active[queue] > 0 {
		r.active[queue]--
	}
	if r.totalActiveLocked() == 0 {
		select {
		case <-r.activeZero:
		default:
			close(r.activeZero)
		}
	}
}

func (r *Runner) totalActiveLocked() int {
	total := 0
	for _, queue := range queues {
		total += r.active[queue]
	}
	return total
}

func (r *Runner) waitActive(ctx context.Context) error {
	r.activeMu.Lock()
	done := r.activeZero
	r.activeMu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func boundedTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
