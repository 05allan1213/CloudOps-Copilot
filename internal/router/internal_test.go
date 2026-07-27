package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentstore"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

func TestInternalRouterHasExactCapabilitySurfaceAndUserRouterDoesNotMountWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.RateLimit.Enabled = false
	cfg.StaticDir = ""
	targets, err := alertmanageringress.ParseTargetAllowlist(cfg.SignalTargetAllowlistJSON)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := alertmanageringress.NewHandler(alertmanageringress.Config{
		Store: internalRouterStore{}, Targets: targets,
		MaxBodyBytes: cfg.AlertmanagerWebhookMaxBodyBytes, RequestTimeout: cfg.RequestTimeout,
		Readiness: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{Metrics: middleware.NewMetrics(), Alertmanager: ingress}
	internal, err := NewInternalRouter(cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"POST /webhooks/alertmanager": true,
		"GET /livez":                  true, "GET /readyz": true, "GET /metrics": true,
	}
	got := make(map[string]bool)
	for _, route := range internal.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	if len(got) != len(want) {
		t.Fatalf("INTERNAL routes=%v", got)
	}
	for route := range want {
		if !got[route] {
			t.Errorf("missing INTERNAL route %s", route)
		}
	}
	for _, path := range []string{"/api/v1/incidents", "/api/v2/incidents", "/api/v1/auth/login"} {
		response := httptest.NewRecorder()
		internal.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("INTERNAL path %s status=%d", path, response.Code)
		}
	}

	user, err := NewRouter(cfg, Dependencies{Metrics: middleware.NewMetrics(), Handler: &handler.Handler{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range user.Routes() {
		if route.Path == "/webhooks/alertmanager" {
			t.Fatal("user listener mounted the INTERNAL webhook")
		}
	}
}

type internalRouterStore struct{}

func (internalRouterStore) Ready(context.Context) error { return nil }
func (internalRouterStore) IngestBatch(context.Context, []alertdomain.SignalInput) ([]alertdomain.IngestResult, error) {
	return nil, nil
}
func (internalRouterStore) RecordRejections(context.Context, []incidentstore.RejectionInput) error {
	return nil
}
