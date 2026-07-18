package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	authpkg "github.com/05allan1213/CloudOps-Copilot/internal/service/auth"
)

func TestRootRouterMountsV3WithoutChangingV2Surface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.AuthEnabled = false
	cfg.RateLimit.Enabled = false
	cfg.StaticDir = ""
	cfg.FastDemoEnabled = false
	engine, err := NewRouter(cfg, Dependencies{Metrics: middleware.NewMetrics(), Handler: &handler.Handler{}})
	if err != nil {
		t.Fatal(err)
	}
	present := make(map[string]bool)
	for _, route := range engine.Routes() {
		present[route.Method+" "+route.Path] = true
	}
	for _, route := range apiv3.Phase2Routes() {
		if !present[route.Method+" "+route.Path] {
			t.Errorf("missing V3 route %s %s", route.Method, route.Path)
		}
	}
	for _, v2 := range []string{"POST /api/v2/webhook/alertmanager", "GET /api/v2/incidents", "GET /api/v2/workbench/incidents"} {
		if !present[v2] {
			t.Errorf("V2 route changed or removed: %s", v2)
		}
	}
}

func TestV3CompatibilityAuthMapsRolesWithoutExposingNumericID(t *testing.T) {
	service := &v3AuthFake{identity: authpkg.Identity{ID: 42, Username: "alice", Role: "admin", TokenVersion: 3}}
	adapter := v3CompatibilityAuth{service: service}
	identity, err := adapter.AuthenticateBearer(context.Background(), "Bearer token")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Login != "alice" || identity.Role != "operator" || identity.Provider != "local" {
		t.Fatalf("identity=%+v", identity)
	}
	if err := adapter.Verify(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if !service.verified || service.verifyIdentity.ID != 42 || service.verifyIdentity.TokenVersion != 3 {
		t.Fatalf("verified=%v identity=%+v", service.verified, service.verifyIdentity)
	}
}

func TestUnwiredRuntimeV3QueryFailsClosedAndBodyErrorsStayVersioned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.AuthEnabled = false
	cfg.RateLimit.Enabled = false
	cfg.StaticDir = ""
	cfg.FastDemoEnabled = false
	cfg.GlobalMaxBodyBytes = 8
	engine, err := NewRouter(cfg, Dependencies{Metrics: middleware.NewMetrics(), Handler: &handler.Handler{}})
	if err != nil {
		t.Fatal(err)
	}

	query := httptest.NewRecorder()
	engine.ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil))
	if query.Code != http.StatusNotImplemented || query.Header().Get("Content-Type") != apiv3.ProblemMediaType || !strings.Contains(query.Body.String(), `"code":"NOT_IMPLEMENTED"`) {
		t.Fatalf("unwired V3 query response=%d/%q %s", query.Code, query.Header().Get("Content-Type"), query.Body.String())
	}

	v3Body := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/123e4567-e89b-12d3-a456-426614174000/close", strings.NewReader("0123456789"))
	engine.ServeHTTP(v3Body, request)
	if v3Body.Code != http.StatusRequestEntityTooLarge || v3Body.Header().Get("Content-Type") != apiv3.ProblemMediaType {
		t.Fatalf("V3 body limit response=%d/%q %s", v3Body.Code, v3Body.Header().Get("Content-Type"), v3Body.Body.String())
	}

	v2Body := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v2/webhook/alertmanager", strings.NewReader("0123456789"))
	engine.ServeHTTP(v2Body, request)
	if v2Body.Code != http.StatusRequestEntityTooLarge || v2Body.Header().Get("Content-Type") == apiv3.ProblemMediaType || !strings.Contains(v2Body.Body.String(), `"status":"error"`) {
		t.Fatalf("V2 body limit changed=%d/%q %s", v2Body.Code, v2Body.Header().Get("Content-Type"), v2Body.Body.String())
	}
}

func TestV2RouteTableIsExactAfterV3Mount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.AuthEnabled = false
	cfg.RateLimit.Enabled = false
	cfg.StaticDir = ""
	cfg.FastDemoEnabled = false
	engine, err := NewRouter(cfg, Dependencies{Metrics: middleware.NewMetrics(), Handler: &handler.Handler{}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, route := range []string{
		"POST /api/v2/webhook/alertmanager",
		"GET /api/v2/incidents", "GET /api/v2/incidents/:id", "GET /api/v2/incidents/:id/signals",
		"GET /api/v2/incidents/:id/timeline", "GET /api/v2/incidents/:id/evidence", "GET /api/v2/incidents/:id/changes",
		"GET /api/v2/incidents/:id/change-context", "GET /api/v2/incidents/:id/delivery",
		"GET /api/v2/incidents/:id/verifications", "GET /api/v2/incidents/:id/verifications/:verification_id",
		"GET /api/v2/incidents/:id/postmortem", "POST /api/v2/incidents/:id/agent-runs", "GET /api/v2/incidents/:id/agent-runs",
		"GET /api/v2/agent-runs/:id", "GET /api/v2/agent-runs/:id/steps", "GET /api/v2/agent-runs/:id/evidence", "POST /api/v2/agent-runs/:id/cancel",
		"GET /api/v2/workbench/incidents", "GET /api/v2/workbench/incidents/:id", "GET /api/v2/workbench/incidents/:id/signals",
		"GET /api/v2/workbench/incidents/:id/timeline", "GET /api/v2/workbench/incidents/:id/evidence", "GET /api/v2/workbench/incidents/:id/resources",
		"GET /api/v2/workbench/incidents/:id/investigation", "GET /api/v2/workbench/incidents/:id/remediation", "GET /api/v2/workbench/incidents/:id/delivery",
		"GET /api/v2/workbench/incidents/:id/verifications", "GET /api/v2/workbench/incidents/:id/verifications/:verification_id", "GET /api/v2/workbench/incidents/:id/realtime",
		"GET /api/v2/remediations", "GET /api/v2/remediations/:id", "POST /api/v2/remediations/:id/approve", "POST /api/v2/remediations/:id/reject",
	} {
		want[route] = true
	}
	got := map[string]bool{}
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/v2/") {
			got[route.Method+" "+route.Path] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("V2 route count=%d want=%d routes=%v", len(got), len(want), got)
	}
	for route := range want {
		if !got[route] {
			t.Errorf("V2 route changed or missing: %s", route)
		}
	}
}

func TestUnknownRouteErrorFormatIsVersionScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.AuthEnabled = false
	cfg.RateLimit.Enabled = false
	cfg.StaticDir = ""
	engine, err := NewRouter(cfg, Dependencies{Metrics: middleware.NewMetrics(), Handler: &handler.Handler{}})
	if err != nil {
		t.Fatal(err)
	}

	v3 := httptest.NewRecorder()
	engine.ServeHTTP(v3, httptest.NewRequest(http.MethodGet, "/api/v3/not-a-route", nil))
	if v3.Code != http.StatusNotFound || v3.Header().Get("Content-Type") != apiv3.ProblemMediaType || !strings.Contains(v3.Body.String(), `"code":"ROUTE_NOT_FOUND"`) {
		t.Fatalf("V3 no-route=%d/%q %s", v3.Code, v3.Header().Get("Content-Type"), v3.Body.String())
	}

	v2 := httptest.NewRecorder()
	engine.ServeHTTP(v2, httptest.NewRequest(http.MethodGet, "/api/v2/not-a-route", nil))
	if v2.Code != http.StatusNotFound || v2.Header().Get("Content-Type") != "text/plain" || v2.Body.String() != "404 page not found" {
		t.Fatalf("V2 no-route changed=%d/%q %q", v2.Code, v2.Header().Get("Content-Type"), v2.Body.String())
	}
}

type v3AuthFake struct {
	identity       authpkg.Identity
	verified       bool
	verifyIdentity authpkg.Identity
}

func (f *v3AuthFake) AuthenticateBearer(string) (authpkg.Identity, error) { return f.identity, nil }
func (f *v3AuthFake) VerifyTokenVersion(_ context.Context, identity authpkg.Identity) error {
	f.verified = true
	f.verifyIdentity = identity
	return nil
}
