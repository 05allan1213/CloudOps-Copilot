package api

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
)

func TestAPIServerGracefulShutdown(t *testing.T) {
	api := &API{
		cfg: APIConfig{Application: appconfig.Config{ShutdownTimeout: time.Second}},
		server: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- api.Serve(ctx, listener) }()
	waitForHTTPStatus(t, "http://"+listener.Addr().String()+"/", http.StatusNoContent)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLoadAPIConfig(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	config, err := LoadAPIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Application.ListenAddr == "" {
		t.Fatal("API listen address is empty")
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
