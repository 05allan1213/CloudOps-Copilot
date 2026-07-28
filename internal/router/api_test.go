package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

func TestRootRouterMountsOnlyV1Contract(t *testing.T) {
	engine := newTestRouter(t, 0)
	present := make(map[string]bool)
	for _, route := range engine.Routes() {
		present[route.Method+" "+route.Path] = true
		for _, forbidden := range []string{"/api/v2", "/api/v3", "/api/v1/auth", "/api/v1/session"} {
			if strings.HasPrefix(route.Path, forbidden) {
				t.Fatalf("obsolete public route registered: %s %s", route.Method, route.Path)
			}
		}
	}
	for _, route := range api.Routes() {
		if !present[route.Method+" "+route.Path] {
			t.Errorf("missing V1 route %s %s", route.Method, route.Path)
		}
	}
}

func TestLocalOwnerQueryNeedsNoCredential(t *testing.T) {
	engine := newTestRouter(t, 0)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	request.Header.Set("Authorization", "Bearer ignored-in-local-owner-mode")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented || response.Header().Get("Content-Type") != api.ProblemMediaType || !strings.Contains(response.Body.String(), `"code":"NOT_IMPLEMENTED"`) {
		t.Fatalf("unwired Owner query response=%d/%q %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestV1BodyLimitAndObsoleteRoutes(t *testing.T) {
	engine := newTestRouter(t, 8)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/incidents/123e4567-e89b-12d3-a456-426614174000/close", strings.NewReader("0123456789"))
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || response.Header().Get("Content-Type") != api.ProblemMediaType {
		t.Fatalf("V1 body limit response=%d/%q %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}

	for _, path := range []string{"/api/v2/incidents", "/api/v3/incidents", "/api/v1/auth/login", "/api/v1/session/csrf"} {
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("obsolete route %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestUnknownV1RouteUsesProblemContract(t *testing.T) {
	engine := newTestRouter(t, 0)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/not-a-route", nil))
	if response.Code != http.StatusNotFound || response.Header().Get("Content-Type") != api.ProblemMediaType || !strings.Contains(response.Body.String(), `"code":"ROUTE_NOT_FOUND"`) {
		t.Fatalf("V1 no-route=%d/%q %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}

	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/not-a-route", nil))
	if response.Code != http.StatusNotFound || response.Header().Get("Content-Type") != "text/plain" || response.Body.String() != "404 page not found" {
		t.Fatalf("non-contract no-route=%d/%q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func newTestRouter(t *testing.T, maxBodyBytes int64) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.RateLimit.Enabled = false
	cfg.StaticDir = ""
	cfg.GlobalMaxBodyBytes = maxBodyBytes
	engine, err := NewRouter(cfg, Dependencies{Metrics: middleware.NewMetrics(), Handler: &handler.Handler{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}
