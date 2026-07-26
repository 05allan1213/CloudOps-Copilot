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

type taskStoreReadinessFunc func(context.Context) error

func (f taskStoreReadinessFunc) Ready(ctx context.Context) error { return f(ctx) }

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

func TestStandbyTaskRunnerNeverClaimsAndChecksSchema(t *testing.T) {
	readyCalls := atomic.Int32{}
	runner := &standbyTaskRunner{store: nil}
	if err := runner.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "requires the async task store") {
		t.Fatalf("missing store error=%v", err)
	}
	store := taskStoreReadinessFunc(func(context.Context) error {
		readyCalls.Add(1)
		return nil
	})
	runner = &standbyTaskRunner{store: store}
	if err := runner.Ready(context.Background()); !errors.Is(err, asyncjob.ErrRunnerNotStarted) {
		t.Fatalf("not-started error=%v", err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if readyCalls.Load() != 1 {
		t.Fatalf("readiness calls=%d, want 1", readyCalls.Load())
	}
	// Standby has no claim method and cannot consume task attempts.
	runner.stopped.Store(true)
	if err := runner.Ready(context.Background()); !errors.Is(err, asyncjob.ErrClaimsStopped) {
		t.Fatalf("stopped error=%v", err)
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
