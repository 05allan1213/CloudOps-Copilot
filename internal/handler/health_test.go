package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type readinessMySQL struct {
	enabled bool
	err     error
}

func (m readinessMySQL) Enabled() bool               { return m.enabled }
func (m readinessMySQL) Ping(context.Context) error  { return m.err }
func (m readinessMySQL) Ready(context.Context) error { return m.err }

func TestAPIReadinessRequiresSupportedMySQLSchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		mysql    mysqlClient
		runtime  RuntimeReadiness
		expected int
	}{
		{name: "missing", expected: http.StatusServiceUnavailable},
		{name: "disabled", mysql: readinessMySQL{}, expected: http.StatusServiceUnavailable},
		{name: "schema mismatch", mysql: readinessMySQL{enabled: true, err: errors.New("unsupported schema version 6, want 7")}, expected: http.StatusServiceUnavailable},
		{name: "missing runtime guard", mysql: readinessMySQL{enabled: true}, expected: http.StatusServiceUnavailable},
		{name: "marker mismatch", mysql: readinessMySQL{enabled: true}, runtime: func(context.Context) error { return errors.New("compatibility runtime refused after CUTOVER-V3") }, expected: http.StatusServiceUnavailable},
		{name: "ready", mysql: readinessMySQL{enabled: true}, runtime: func(context.Context) error { return nil }, expected: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{mysqlClient: test.mysql, runtimeReadiness: test.runtime, readyTimeout: time.Second}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
			handler.Readyz(ctx)
			if response.Code != test.expected {
				t.Fatalf("status=%d, want %d; body=%s", response.Code, test.expected, response.Body.String())
			}
		})
	}
}
