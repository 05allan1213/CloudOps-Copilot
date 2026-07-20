package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/health"
)

type testTaskRunner struct {
	starts       atomic.Int32
	stops        atomic.Int32
	shutdowns    atomic.Int32
	startErr     error
	readyErr     error
	shutdownFunc func(context.Context) error
}

func (r *testTaskRunner) Start(context.Context) error {
	r.starts.Add(1)
	return r.startErr
}
func (r *testTaskRunner) StopClaims() { r.stops.Add(1) }
func (r *testTaskRunner) Shutdown(ctx context.Context) error {
	r.shutdowns.Add(1)
	if r.shutdownFunc != nil {
		return r.shutdownFunc(ctx)
	}
	return nil
}
func (r *testTaskRunner) Ready(context.Context) error { return r.readyErr }

type readinessOnlyTaskStore struct {
	asyncjob.Store
	readyErr   error
	readyCalls int
}

func (s *readinessOnlyTaskStore) Ready(context.Context) error {
	s.readyCalls++
	return s.readyErr
}

func TestRuntimeGuardedTaskStoreChecksGenerationAfterSchema(t *testing.T) {
	underlying := &readinessOnlyTaskStore{}
	guardCalls := 0
	refused := errors.New("compatibility runtime refused after CUTOVER-V3")
	store := runtimeGuardedTaskStore{
		Store: underlying,
		runtimeReady: func(context.Context) error {
			guardCalls++
			return refused
		},
	}
	if err := store.Ready(context.Background()); !errors.Is(err, refused) {
		t.Fatalf("guarded store readiness err=%v", err)
	}
	if underlying.readyCalls != 1 || guardCalls != 1 {
		t.Fatalf("ready calls schema=%d generation=%d", underlying.readyCalls, guardCalls)
	}

	underlying.readyErr = errors.New("unsupported async task schema")
	if err := store.Ready(context.Background()); !errors.Is(err, underlying.readyErr) {
		t.Fatalf("schema readiness err=%v", err)
	}
	if guardCalls != 1 {
		t.Fatalf("generation guard ran before schema readiness: calls=%d", guardCalls)
	}
}

func TestWorkerOwnsAsyncRunnerAndShutsDown(t *testing.T) {
	runner := &testTaskRunner{}
	worker := testWorker(runner, DefaultAsyncWorkerConfig())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Serve(ctx, listener) }()

	waitForHTTPStatus(t, "http://"+listener.Addr().String()+"/readyz", http.StatusOK)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if runner.starts.Load() != 1 || runner.stops.Load() == 0 || runner.shutdowns.Load() != 1 {
		t.Fatalf("starts=%d stops=%d shutdowns=%d", runner.starts.Load(), runner.stops.Load(), runner.shutdowns.Load())
	}
}

func TestWorkerReadinessRequiresClaimLoopsMySQLAndQueue(t *testing.T) {
	worker := &Worker{}
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "claim loops") {
		t.Fatalf("uninitialized runtime readiness err=%v", err)
	}
	worker.ready.Store(true)
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "mysql") {
		t.Fatalf("missing MySQL readiness err=%v", err)
	}
	worker.mysqlReady = func(context.Context) error { return errors.New("unsupported schema version 9, want 10") }
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "schema version 9") {
		t.Fatalf("schema mismatch readiness err=%v", err)
	}
	worker.mysqlReady = func(context.Context) error { return nil }
	worker.runner = &testTaskRunner{}
	worker.stateMu.Lock()
	worker.runnerStarted = true
	worker.stateMu.Unlock()
	if err := worker.readiness(context.Background()); err != nil {
		t.Fatalf("ready worker err=%v", err)
	}
	worker.runner.(*testTaskRunner).readyErr = errors.New("queue schema is incomplete")
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "queue schema") {
		t.Fatalf("queue readiness err=%v", err)
	}
}

func TestWorkerExitsWhenAsyncRunnerCannotStart(t *testing.T) {
	runner := &testTaskRunner{startErr: errors.New("queue schema is incomplete")}
	worker := testWorker(runner, DefaultAsyncWorkerConfig())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	err = worker.Serve(context.Background(), listener)
	if err == nil || !strings.Contains(err.Error(), "queue schema is incomplete") {
		t.Fatalf("serve error=%v", err)
	}
	if runner.stops.Load() != 0 || runner.shutdowns.Load() != 0 {
		t.Fatalf("failed start invoked stop/shutdown: stops=%d shutdowns=%d", runner.stops.Load(), runner.shutdowns.Load())
	}
}

func TestNewWorkerFailsClosedBeforeClaimingWhenOperationsAreMissing(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	cfg, err := LoadWorkerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorker(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "operations are not migrated") {
		t.Fatalf("missing task operations error=%v", err)
	}
}

func TestWorkerShutdownBoundsStuckHandler(t *testing.T) {
	asyncConfig := DefaultAsyncWorkerConfig()
	asyncConfig.DrainTimeout = 20 * time.Millisecond
	asyncConfig.ExitDeadline = 50 * time.Millisecond
	runner := &testTaskRunner{shutdownFunc: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	worker := testWorker(runner, asyncConfig)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Serve(ctx, listener) }()
	waitForHTTPStatus(t, "http://"+listener.Addr().String()+"/readyz", http.StatusOK)
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker shutdown exceeded bounded deadline")
	}
}

func testWorker(runner taskRunner, asyncConfig AsyncWorkerConfig) *Worker {
	worker := &Worker{
		cfg:        WorkerConfig{Async: asyncConfig},
		runner:     runner,
		mysqlReady: func(context.Context) error { return nil },
	}
	worker.management = &http.Server{Handler: health.NewHandler(health.Options{Process: "cloudops-worker", Ready: worker.readiness})}
	return worker
}

func waitForHTTPStatus(t *testing.T, url string, expected int) {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == expected {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not return status %d", url, expected)
}
