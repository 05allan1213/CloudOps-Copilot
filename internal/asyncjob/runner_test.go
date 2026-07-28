package asyncjob

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu             sync.Mutex
	nextID         uint64
	pending        map[Queue][]Task
	claimCalls     map[Queue]int
	heartbeatCalls map[uint64]int
	heartbeatErr   map[uint64]error
	resolved       map[uint64]Result
	readyErr       error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		pending:        make(map[Queue][]Task),
		claimCalls:     make(map[Queue]int),
		heartbeatCalls: make(map[uint64]int),
		heartbeatErr:   make(map[uint64]error),
		resolved:       make(map[uint64]Result),
	}
}

func (s *fakeStore) add(queue Queue, taskType TaskType, count int) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uint64, 0, count)
	for range count {
		s.nextID++
		ids = append(ids, s.nextID)
		s.pending[queue] = append(s.pending[queue], Task{
			ID:                     s.nextID,
			Queue:                  queue,
			Type:                   taskType,
			Status:                 StatusReady,
			ExpectedSubjectVersion: 1,
			MaxAttempts:            3,
		})
	}
	return ids
}

func (s *fakeStore) Ready(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyErr
}

func (s *fakeStore) setReadyError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readyErr = err
}

func (s *fakeStore) Claim(ctx context.Context, request ClaimRequest) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls[request.Queue]++
	items := s.pending[request.Queue]
	allowed, err := request.taskTypeFilter()
	if err != nil {
		return nil, err
	}
	allowedSet := make(map[TaskType]struct{}, len(allowed))
	for _, taskType := range allowed {
		allowedSet[taskType] = struct{}{}
	}
	index := -1
	for candidate := range items {
		if _, ok := allowedSet[items[candidate].Type]; ok {
			index = candidate
			break
		}
	}
	if index < 0 {
		return nil, ErrNoTask
	}
	task := items[index]
	s.pending[request.Queue] = append(items[:index], items[index+1:]...)
	task.Status = StatusRunning
	task.Attempt++
	task.LeaseGeneration++
	task.LeaseOwner = request.Owner
	expires := time.Now().Add(request.LeaseDuration)
	task.LeaseExpiresAt = &expires
	return executionFor(task), nil
}

func (s *fakeStore) Heartbeat(_ context.Context, lease Lease, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatCalls[lease.TaskID]++
	return s.heartbeatErr[lease.TaskID]
}

func (s *fakeStore) Resolve(_ context.Context, lease Lease, result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved[lease.TaskID] = result
	return nil
}

func (s *fakeStore) claims(queue Queue) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimCalls[queue]
}

func (s *fakeStore) resolvedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.resolved)
}

func (s *fakeStore) resolvedDisposition(taskID uint64) (Disposition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.resolved[taskID]
	return result.Disposition, ok
}

func (s *fakeStore) resolvedResult(taskID uint64) (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.resolved[taskID]
	return result, ok
}

func successfulHandlers() map[TaskType]Handler {
	handlers := make(map[TaskType]Handler, len(taskTypes))
	for _, taskType := range taskTypes {
		handlers[taskType] = HandlerFunc(func(context.Context, Execution) Result { return Succeeded(nil) })
	}
	return handlers
}

func tinyPools(max map[Queue]int) map[Queue]PoolConfig {
	result := make(map[Queue]PoolConfig, len(queues))
	for _, queue := range queues {
		result[queue] = PoolConfig{
			MaxInFlight:      max[queue],
			LeaseDuration:    300 * time.Millisecond,
			HeartbeatPeriod:  50 * time.Millisecond,
			HandlerDeadline:  200 * time.Millisecond,
			ExternalDeadline: 100 * time.Millisecond,
		}
	}
	return result
}

func TestRunnerTakesSemaphoreBeforeClaimAndHonorsFourPoolLimits(t *testing.T) {
	store := newFakeStore()
	store.add(QueueInvestigate, TaskInvestigationAdvance, 8)
	store.add(QueueDeliver, TaskChangeEnsurePR, 8)
	store.add(QueueObserve, TaskDeliveryObserve, 8)
	store.add(QueueVerify, TaskVerificationAdvance, 8)

	release := make(chan struct{})
	handlers := successfulHandlers()
	for taskType := range handlers {
		handlers[taskType] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
			select {
			case <-release:
				return Succeeded(nil)
			case <-ctx.Done():
				return RetryAfter(0, "cancelled", "cancelled", nil)
			}
		})
	}
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[Queue]int{QueueInvestigate: 2, QueueDeliver: 1, QueueObserve: 2, QueueVerify: 2}
	waitFor(t, time.Second, func() bool {
		for queue, expected := range want {
			if runner.InFlight(queue) != expected {
				return false
			}
		}
		return true
	})
	runner.StopClaims()
	for queue, expected := range want {
		if got := store.claims(queue); got != expected {
			t.Fatalf("%s claims=%d, want %d; pool claimed without a semaphore", queue, got, expected)
		}
	}
	close(release)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
	if got := store.resolvedCount(); got != 7 {
		t.Fatalf("resolved=%d, want 7", got)
	}
}

func TestSaturatedInvestigatePoolDoesNotStarveDeliverOrVerify(t *testing.T) {
	store := newFakeStore()
	store.add(QueueInvestigate, TaskInvestigationAdvance, 8)
	deliverID := store.add(QueueDeliver, TaskChangeEnsurePR, 1)[0]
	verifyID := store.add(QueueVerify, TaskVerificationAdvance, 1)[0]

	releaseInvestigate := make(chan struct{})
	handlers := successfulHandlers()
	handlers[TaskInvestigationAdvance] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
		select {
		case <-releaseInvestigate:
			return Succeeded(nil)
		case <-ctx.Done():
			return RetryAfter(0, "cancelled", "cancelled", nil)
		}
	})
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		_, delivered := store.resolvedDisposition(deliverID)
		_, verified := store.resolvedDisposition(verifyID)
		return delivered && verified && runner.InFlight(QueueInvestigate) == 2
	})
	runner.StopClaims()
	close(releaseInvestigate)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func TestSlowObservePoolDoesNotConsumeOtherPoolCapacity(t *testing.T) {
	store := newFakeStore()
	store.add(QueueObserve, TaskDeliveryObserve, 4)
	investigateID := store.add(QueueInvestigate, TaskRemediationPrepare, 1)[0]
	deliverID := store.add(QueueDeliver, TaskChangeEnsurePR, 1)[0]
	verifyID := store.add(QueueVerify, TaskVerificationAdvance, 1)[0]

	releaseObserve := make(chan struct{})
	handlers := successfulHandlers()
	handlers[TaskDeliveryObserve] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
		select {
		case <-releaseObserve:
			return Succeeded(nil)
		case <-ctx.Done():
			return RetryAfter(0, "cancelled", "cancelled", nil)
		}
	})
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		_, investigated := store.resolvedDisposition(investigateID)
		_, delivered := store.resolvedDisposition(deliverID)
		_, verified := store.resolvedDisposition(verifyID)
		return investigated && delivered && verified && runner.InFlight(QueueObserve) == 2
	})
	runner.StopClaims()
	close(releaseObserve)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func TestStopClaimsPreventsFurtherClaims(t *testing.T) {
	store := newFakeStore()
	store.add(QueueInvestigate, TaskInvestigationAdvance, 1)
	block := make(chan struct{})
	handlers := successfulHandlers()
	handlers[TaskInvestigationAdvance] = HandlerFunc(func(context.Context, Execution) Result {
		<-block
		return Succeeded(nil)
	})
	pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers, Pools: pools, PollInterval: time.Millisecond, DrainTimeout: 100 * time.Millisecond, CancelWait: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return runner.InFlight(QueueInvestigate) == 1 })
	runner.StopClaims()
	before := store.claims(QueueInvestigate)
	store.add(QueueInvestigate, TaskInvestigationAdvance, 3)
	time.Sleep(20 * time.Millisecond)
	if got := store.claims(QueueInvestigate); got != before {
		t.Fatalf("claims after StopClaims=%d, before=%d", got, before)
	}
	if err := runner.Ready(context.Background()); !errors.Is(err, ErrClaimsStopped) {
		t.Fatalf("readiness after StopClaims=%v", err)
	}
	close(block)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownCancellationDoesNotFabricateTerminalResult(t *testing.T) {
	store := newFakeStore()
	store.add(QueueInvestigate, TaskInvestigationAdvance, 1)
	handlers := successfulHandlers()
	handlers[TaskInvestigationAdvance] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
		<-ctx.Done()
		return Succeeded(nil)
	})
	pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers, Pools: pools, PollInterval: time.Millisecond, DrainTimeout: 20 * time.Millisecond, CancelWait: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return runner.InFlight(QueueInvestigate) == 1 })
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Shutdown(shutdownCtx); !errors.Is(err, ErrDrainTimeout) {
		t.Fatalf("shutdown error=%v, want drain timeout", err)
	}
	if got := store.resolvedCount(); got != 0 {
		t.Fatalf("shutdown fabricated %d terminal task results", got)
	}
}

func TestHeartbeatLossCancelsHandlerAndRejectsResult(t *testing.T) {
	store := newFakeStore()
	taskID := store.add(QueueVerify, TaskVerificationAdvance, 1)[0]
	store.heartbeatErr[taskID] = ErrLeaseLost
	handlers := successfulHandlers()
	handlers[TaskVerificationAdvance] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
		<-ctx.Done()
		return RetryAfter(0, "dependency", "should not persist after lease loss", nil)
	})
	pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
	verify := pools[QueueVerify]
	verify.LeaseDuration = 60 * time.Millisecond
	verify.HeartbeatPeriod = 10 * time.Millisecond
	verify.HandlerDeadline = 40 * time.Millisecond
	verify.ExternalDeadline = 20 * time.Millisecond
	pools[QueueVerify] = verify
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers, Pools: pools, PollInterval: time.Millisecond, DrainTimeout: 100 * time.Millisecond, CancelWait: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.heartbeatCalls[taskID] > 0 && runner.InFlight(QueueVerify) == 0
	})
	runner.StopClaims()
	if _, ok := store.resolvedDisposition(taskID); ok {
		t.Fatal("stale handler result was resolved after heartbeat lease loss")
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerDeadlineBecomesRetryWhileLeaseIsStillValid(t *testing.T) {
	store := newFakeStore()
	taskID := store.add(QueueObserve, TaskDeliveryObserve, 1)[0]
	handlers := successfulHandlers()
	handlers[TaskDeliveryObserve] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
		<-ctx.Done()
		return Result{}
	})
	pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
	observe := pools[QueueObserve]
	observe.HandlerDeadline = 30 * time.Millisecond
	observe.ExternalDeadline = 20 * time.Millisecond
	pools[QueueObserve] = observe
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers, Pools: pools, PollInterval: time.Millisecond, DrainTimeout: 100 * time.Millisecond, CancelWait: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		result, ok := store.resolvedResult(taskID)
		return ok && result.Disposition == DispositionRetry && result.RetryAfter > 0
	})
	runner.StopClaims()
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerRetryBackoffPreventsHotLoopAndPreservesAdapterDelay(t *testing.T) {
	store := newFakeStore()
	zeroDelayID := store.add(QueueObserve, TaskDeliveryObserve, 1)[0]
	adapterDelayID := store.add(QueueVerify, TaskVerificationAdvance, 1)[0]
	handlers := successfulHandlers()
	handlers[TaskDeliveryObserve] = HandlerFunc(func(context.Context, Execution) Result {
		return RetryAfter(0, "dependency", "use policy backoff", nil)
	})
	handlers[TaskVerificationAdvance] = HandlerFunc(func(context.Context, Execution) Result {
		return RetryAfter(75*time.Millisecond, "adapter_retry_after", "preserve adapter delay", nil)
	})
	pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
	runner, err := NewRunner(RunnerConfig{
		Owner: "worker-a", Store: store, Handlers: handlers, Pools: pools,
		PollInterval: time.Millisecond, DrainTimeout: 100 * time.Millisecond, CancelWait: 100 * time.Millisecond,
		RetryBackoff: BackoffPolicy{Initial: 25 * time.Millisecond, Maximum: 100 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		_, zeroOK := store.resolvedResult(zeroDelayID)
		_, adapterOK := store.resolvedResult(adapterDelayID)
		return zeroOK && adapterOK
	})
	runner.StopClaims()
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
	zeroDelay, _ := store.resolvedResult(zeroDelayID)
	if zeroDelay.RetryAfter != 25*time.Millisecond {
		t.Fatalf("zero retry delay=%v, want policy delay 25ms", zeroDelay.RetryAfter)
	}
	adapterDelay, _ := store.resolvedResult(adapterDelayID)
	if adapterDelay.RetryAfter != 75*time.Millisecond {
		t.Fatalf("adapter retry delay=%v, want explicit 75ms", adapterDelay.RetryAfter)
	}
}

func TestRunnerInjectsExternalDeadlineShorterThanHandlerDeadline(t *testing.T) {
	store := newFakeStore()
	taskID := store.add(QueueDeliver, TaskChangeEnsurePR, 1)[0]
	type observation struct {
		timeout        time.Duration
		externalBefore bool
		err            error
	}
	observed := make(chan observation, 1)
	handlers := successfulHandlers()
	handlers[TaskChangeEnsurePR] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
		timeout, ok := ExternalCallTimeout(ctx)
		if !ok {
			observed <- observation{err: ErrExternalDeadlineMissing}
			return Result{}
		}
		externalCtx, cancel, err := ExternalCallContext(ctx)
		if err != nil {
			observed <- observation{err: err}
			return Result{}
		}
		defer cancel()
		handlerDeadline, handlerOK := ctx.Deadline()
		externalDeadline, externalOK := externalCtx.Deadline()
		observed <- observation{timeout: timeout, externalBefore: handlerOK && externalOK && externalDeadline.Before(handlerDeadline)}
		return Succeeded(nil)
	})
	pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
	runner, err := NewRunner(RunnerConfig{Owner: "worker-a", Store: store, Handlers: handlers, Pools: pools, PollInterval: time.Millisecond, DrainTimeout: 100 * time.Millisecond, CancelWait: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-observed:
		if got.err != nil || got.timeout != pools[QueueDeliver].ExternalDeadline || !got.externalBefore {
			t.Fatalf("external deadline observation=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not observe external deadline policy")
	}
	waitFor(t, time.Second, func() bool {
		disposition, ok := store.resolvedDisposition(taskID)
		return ok && disposition == DispositionSucceeded
	})
	runner.StopClaims()
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func TestStartInitializesLoopsButDoesNotClaimUntilStoreReady(t *testing.T) {
	store := newFakeStore()
	store.readyErr = errors.New("schema missing")
	store.add(QueueInvestigate, TaskInvestigationAdvance, 1)
	runner, err := NewRunner(RunnerConfig{
		Owner: "worker-a", Store: store, Handlers: successfulHandlers(), PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("initialize claim loops: %v", err)
	}
	if err := runner.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "schema missing") {
		t.Fatalf("readiness error=%v, want schema missing", err)
	}
	time.Sleep(20 * time.Millisecond)
	for _, queue := range Queues() {
		if claims := store.claims(queue); claims != 0 {
			t.Fatalf("%s claims=%d while store is not ready", queue, claims)
		}
	}
	store.setReadyError(nil)
	waitFor(t, time.Second, func() bool {
		result, ok := store.resolvedResult(1)
		return ok && result.Disposition == DispositionSucceeded
	})
	runner.StopClaims()
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}
