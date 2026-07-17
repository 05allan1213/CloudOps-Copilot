package router

import (
	"testing"

	"github.com/gin-gonic/gin"

	"server-web/internal/config"
	"server-web/internal/handler"
)

func TestDemoRoutesAreAbsentUnlessFastDemoIsEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name    string
		enabled bool
		want    int
	}{
		{name: "formal mode", enabled: false, want: 0},
		{name: "guarded demo", enabled: true, want: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api/v2")
			registerDemoRoutes(group, config.Config{FastDemoEnabled: tt.enabled}, &handler.Handler{})
			got := 0
			for _, route := range router.Routes() {
				if route.Path == "/api/v2/demo/incidents/:id/plan" || route.Path == "/api/v2/demo/remediations/:id/execute" || route.Path == "/api/v2/demo/incidents/:id/verify" {
					got++
				}
				if route.Path == "/api/v2/fast-demo/incidents/:id/plan" {
					t.Fatalf("legacy demo route leaked: %s", route.Path)
				}
			}
			if got != tt.want {
				t.Fatalf("demo routes=%d want=%d", got, tt.want)
			}
		})
	}
}
