package apiv3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestV3CORSAllowsCommandHeadersAndRejectsForeignOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CORS([]string{"https://console.example"}))
	engine.POST("/api/v3/command", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	preflight := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api/v3/command", nil)
	request.Header.Set("Origin", "https://console.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Idempotency-Key, X-CSRF-Token")
	engine.ServeHTTP(preflight, request)
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("preflight status=%d", preflight.Code)
	}
	allowHeaders := preflight.Header().Get(corsAllowHeaders)
	for _, required := range []string{IdempotencyHeader, CSRFHeader, "Last-Event-ID"} {
		if !strings.Contains(allowHeaders, required) {
			t.Fatalf("CORS allow headers %q missing %q", allowHeaders, required)
		}
	}
	if preflight.Header().Get(corsAllowOrigin) != "https://console.example" || preflight.Header().Get(corsAllowCredentials) != "true" {
		t.Fatalf("CORS origin/credentials=%q/%q", preflight.Header().Get(corsAllowOrigin), preflight.Header().Get(corsAllowCredentials))
	}

	foreign := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v3/command", nil)
	request.Header.Set("Origin", "https://attacker.example")
	engine.ServeHTTP(foreign, request)
	if foreign.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status=%d", foreign.Code)
	}
	assertProblem(t, foreign, "ORIGIN_FORBIDDEN")
}

func TestV3BodyLimitRateLimitAndRecoveryUseProblemContract(t *testing.T) {
	t.Run("body limit", func(t *testing.T) {
		engine := gin.New()
		engine.Use(LimitRequestBody(8))
		engine.POST("/api/v3/command", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v3/command", strings.NewReader("0123456789")))
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("body limit status=%d", response.Code)
		}
		assertProblem(t, response, "REQUEST_TOO_LARGE")
	})

	t.Run("rate limit", func(t *testing.T) {
		engine := gin.New()
		engine.Use(RateLimit(denyingLimiter{}, RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, OperationTimeout: time.Second}))
		engine.GET("/api/v3/incidents", func(c *gin.Context) { c.Status(http.StatusOK) })
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil))
		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("rate limit status=%d", response.Code)
		}
		assertProblem(t, response, "RATE_LIMITED")
	})

	t.Run("panic recovery", func(t *testing.T) {
		engine := gin.New()
		engine.Use(Recovery())
		engine.GET("/api/v3/panic", func(*gin.Context) { panic("contract panic") })
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/panic", nil))
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("recovery status=%d", response.Code)
		}
		assertProblem(t, response, "INTERNAL_ERROR")
	})
}

type denyingLimiter struct{}

func (denyingLimiter) Enabled() bool { return true }
func (denyingLimiter) AllowSlidingWindow(context.Context, string, int64, time.Duration, time.Time) (bool, int64, error) {
	return false, 0, nil
}
