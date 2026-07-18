package router

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
)

func TestCoreRouterDoesNotRegisterLegacyProducts(t *testing.T) {
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
	routes := engine.Routes()
	present := map[string]bool{}
	for _, route := range routes {
		present[route.Path] = true
		for _, removed := range []string{"/api/v1/copilot", "/api/v1/diagnosis", "/api/v1/actions", "/api/v1/hosts", "/api/v1/host-groups", "/api/v1/k8s", "/api/v1/alert-rules", "/api/v1/channels", "/api/v1/users", "/ws/"} {
			if strings.HasPrefix(route.Path, removed) {
				t.Fatalf("legacy route registered: %s %s", route.Method, route.Path)
			}
		}
	}
	for _, required := range []string{"/api/v2/webhook/alertmanager", "/api/v2/workbench/incidents", "/api/v2/workbench/incidents/:id", "/api/v2/workbench/incidents/:id/resources", "/api/v2/workbench/incidents/:id/realtime"} {
		if !present[required] {
			t.Fatalf("required V2 route missing: %s", required)
		}
	}
}
