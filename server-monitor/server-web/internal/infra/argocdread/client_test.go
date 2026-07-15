package argocdread

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"server-web/internal/change"
)

func TestClientApplicationHistoryResourceStatusAndRedaction(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("argocd-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer argocd-secret" || r.URL.Query().Get("project") != "prod" {
			t.Fatalf("unsafe request: %s %s", r.Method, r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/resource-tree") {
			_, _ = w.Write([]byte(`{"nodes":[{"group":"apps","kind":"Deployment","namespace":"payments","name":"checkout","status":"OutOfSync","health":{"status":"Degraded"}},{"kind":"Secret","namespace":"payments","name":"credentials","status":"Synced","health":{"status":"Healthy"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"checkout-prod"},"spec":{"project":"prod","destination":{"server":"https://kubernetes.default.svc","namespace":"payments"},"source":{"repoURL":"https://github.com/acme/gitops","path":"apps/checkout","targetRevision":"main"}},"status":{"sync":{"status":"Synced","revision":"deadbeef"},"health":{"status":"Healthy"},"operationState":{"phase":"Succeeded","message":"synced token=provider-secret","finishedAt":"2026-07-15T09:57:00Z"},"history":[{"id":1,"revision":"deadbeef","deployedAt":"2026-07-15T09:57:00Z","source":{"repoURL":"https://github.com/acme/gitops","path":"apps/checkout"}}]}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, Config{TokenFile: tokenFile, AllowedApplications: []string{"checkout-prod"}, MaxResources: 10, MaxDiffBytes: 4096})
	app, err := client.GetApplication(context.Background(), "checkout-prod", "prod")
	if err != nil || app.SyncStatus != "Synced" || app.HealthStatus != "Healthy" || app.DeployedRevision != "deadbeef" || len(app.History) != 1 || len(app.Resources) != 2 || !app.Resources[0].OutOfSync || !app.Resources[1].Redacted || len(app.ResultHash) != 64 {
		t.Fatalf("app=%+v err=%v", app, err)
	}
	if strings.Contains(app.OperationMessage, "provider-secret") || !strings.Contains(app.OperationMessage, "[REDACTED]") || !app.Degraded {
		t.Fatalf("provider credential text not safely redacted: %+v", app)
	}
}

func TestClientBoundsAllowlistStatusesRetryAndCancellation(t *testing.T) {
	resources := `{"nodes":[` + strings.Repeat(`{"kind":"Deployment","namespace":"ns","name":"`+strings.Repeat("x", 100)+`","status":"OutOfSync","health":{"status":"Progressing"}},`, 20) + `{"kind":"Pod","name":"last"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "resource-tree") {
			_, _ = w.Write([]byte(resources))
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"app"},"spec":{"project":"prod"},"status":{"sync":{"status":"OutOfSync"},"health":{"status":"Progressing"}}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, Config{MaxResources: 2, MaxDiffBytes: 256})
	items, truncated, hash, err := client.GetResourceStatus(context.Background(), "app", "prod")
	if err != nil || !truncated || len(items) > 2 || len(hash) != 64 {
		t.Fatalf("items=%+v truncated=%v hash=%q err=%v", items, truncated, hash, err)
	}
	if _, err := client.GetApplication(context.Background(), "other", "prod"); !errors.Is(err, change.ErrNotAllowed) {
		t.Fatalf("application allowlist err=%v", err)
	}
	if _, err := client.GetApplication(context.Background(), "app", "other"); !errors.Is(err, change.ErrNotAllowed) {
		t.Fatalf("project allowlist err=%v", err)
	}

	for _, tc := range []struct {
		status int
		code   ErrorCode
	}{{401, ErrorAuthentication}, {403, ErrorPermission}, {404, ErrorNotFound}, {429, ErrorRateLimit}, {500, ErrorTemporary}} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			defer s.Close()
			c := newTestClient(t, s.URL, Config{})
			_, err := c.GetApplication(context.Background(), "app", "prod")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code {
				t.Fatalf("err=%v", err)
			}
		})
	}
	var calls atomic.Int32
	retry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(429)
			return
		}
		if strings.HasSuffix(r.URL.Path, "resource-tree") {
			_, _ = w.Write([]byte(`{"nodes":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"app"},"spec":{"project":"prod"},"status":{}}`))
	}))
	defer retry.Close()
	c := newTestClient(t, retry.URL, Config{MaxRetries: 1, Sleep: func(context.Context, time.Duration) error { return nil }})
	if _, err := c.GetApplication(context.Background(), "app", "prod"); err != nil || calls.Load() != 3 {
		t.Fatalf("retry calls=%d err=%v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetApplication(ctx, "app", "prod"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestClientRejectsSSRFAndProductionHTTP(t *testing.T) {
	for _, server := range []string{"http://argocd.example", "file:///tmp/x", "https://user:pass@argocd.example"} {
		if _, err := New(Config{Server: server, AllowedApplications: []string{"app"}}); err == nil {
			t.Fatalf("expected rejection: %s", server)
		}
	}
}

func newTestClient(t *testing.T, server string, cfg Config) *Client {
	t.Helper()
	cfg.Server = server
	cfg.AllowHTTPForTests = strings.HasPrefix(server, "http://")
	if len(cfg.AllowedApplications) == 0 {
		cfg.AllowedApplications = []string{"app"}
	}
	if len(cfg.AllowedProjects) == 0 {
		cfg.AllowedProjects = []string{"prod"}
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
