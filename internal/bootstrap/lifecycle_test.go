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

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/health"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
)

type testLoop struct {
	starts atomic.Int32
	stops  atomic.Int32
}

func (l *testLoop) Start(context.Context) { l.starts.Add(1) }
func (l *testLoop) Stop()                 { l.stops.Add(1) }

type blockingTestLoop struct{ release <-chan struct{} }

func (l *blockingTestLoop) Start(context.Context) {}
func (l *blockingTestLoop) Stop()                 { <-l.release }

func TestWorkerOwnsLegacyLoopsAndShutsDown(t *testing.T) {
	loops := []*testLoop{{}, {}, {}}
	worker := &Worker{
		cfg:        WorkerConfig{Application: appconfig.Config{ShutdownTimeout: time.Second}},
		loops:      []legacyLoop{loops[0], loops[1], loops[2]},
		mysqlReady: func(context.Context) error { return nil },
	}
	worker.management = &http.Server{
		Handler: health.NewHandler(health.Options{Process: "cloudops-worker", Ready: worker.readiness}),
	}
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
	for index, loop := range loops {
		if loop.starts.Load() != 1 || loop.stops.Load() != 1 {
			t.Fatalf("loop %d starts=%d stops=%d", index, loop.starts.Load(), loop.stops.Load())
		}
	}
}

func TestWorkerReadinessRequiresLoopsAndMySQLSchema(t *testing.T) {
	worker := &Worker{}
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "loops") {
		t.Fatalf("uninitialized loops readiness err=%v", err)
	}
	worker.ready.Store(true)
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "mysql") {
		t.Fatalf("missing MySQL readiness err=%v", err)
	}
	worker.mysqlReady = func(context.Context) error { return errors.New("unsupported schema version 6, want 7") }
	if err := worker.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "schema version 6") {
		t.Fatalf("schema mismatch readiness err=%v", err)
	}
	worker.mysqlReady = func(context.Context) error { return nil }
	if err := worker.readiness(context.Background()); err != nil {
		t.Fatalf("ready worker err=%v", err)
	}
}

func TestWorkerShutdownBoundsBlockingLegacyLoop(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	worker := &Worker{
		cfg:        WorkerConfig{Application: appconfig.Config{ShutdownTimeout: 50 * time.Millisecond}},
		loops:      []legacyLoop{&blockingTestLoop{release: release}},
		mysqlReady: func(context.Context) error { return nil },
	}
	worker.management = &http.Server{Handler: health.NewHandler(health.Options{Process: "cloudops-worker", Ready: worker.readiness})}
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
		if err == nil || !strings.Contains(err.Error(), "stop legacy worker loops") {
			t.Fatalf("shutdown err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker shutdown exceeded bounded timeout")
	}
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
