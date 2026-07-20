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
		userServer: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})},
		internalServer: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		})},
	}
	userListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	internalListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- api.Serve(ctx, userListener, internalListener) }()
	waitForHTTPStatus(t, "http://"+userListener.Addr().String()+"/", http.StatusNoContent)
	waitForHTTPStatus(t, "http://"+internalListener.Addr().String()+"/", http.StatusAccepted)
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
	if config.InternalListenAddr == "" || config.InternalListenAddr == config.Application.ListenAddr {
		t.Fatalf("invalid INTERNAL listen address %q", config.InternalListenAddr)
	}
}

func TestAPIConfigRejectsSharedUserAndInternalAddress(t *testing.T) {
	cfg := APIConfig{Application: appconfig.Load(), InternalListenAddr: "127.0.0.1:18080"}
	cfg.Application.AuthEnabled = false
	cfg.Application.ListenAddr = cfg.InternalListenAddr
	if err := cfg.Validate(); err == nil {
		t.Fatal("shared user and INTERNAL listener address was accepted")
	}
}

func TestAPIConfigRejectsSharedUserAndInternalPortAcrossHosts(t *testing.T) {
	cfg := APIConfig{Application: appconfig.Load(), InternalListenAddr: "0.0.0.0:18080"}
	cfg.Application.AuthEnabled = false
	cfg.Application.ListenAddr = "127.0.0.1:18080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("shared user and INTERNAL listener port was accepted across bind hosts")
	}
}

func TestAPIConfigRequiresLoopbackUserListenerForProxyAuth(t *testing.T) {
	cfg := APIConfig{Application: appconfig.Load(), InternalListenAddr: "0.0.0.0:18082"}
	cfg.Application.AuthEnabled = false
	cfg.Application.V3ProxyAuthEnabled = true
	cfg.Application.V3CSRFSecretFile = "/tmp/csrf-secret"
	cfg.Application.V3OAuthOperatorLogins = []string{"operator"}
	cfg.Application.ListenAddr = "0.0.0.0:18080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("non-loopback proxy user listener was accepted")
	}
	cfg.Application.ListenAddr = "127.0.0.1:18080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("loopback proxy user listener rejected: %v", err)
	}
}

func TestAPIServerFailureStopsBothListeners(t *testing.T) {
	api := &API{
		cfg: APIConfig{Application: appconfig.Config{ShutdownTimeout: time.Second}},
		userServer: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})},
		internalServer: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})},
	}
	userListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	internalListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := internalListener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := api.Serve(context.Background(), userListener, internalListener); err == nil {
		t.Fatal("closed INTERNAL listener did not fail the API process")
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
