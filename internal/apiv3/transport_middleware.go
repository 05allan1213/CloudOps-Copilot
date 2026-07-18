package apiv3

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowOrigin      = "Access-Control-Allow-Origin"
	corsAllowCredentials = "Access-Control-Allow-Credentials"
	corsAllowMethods     = "Access-Control-Allow-Methods"
	corsAllowHeaders     = "Access-Control-Allow-Headers"
	corsExposeHeaders    = "Access-Control-Expose-Headers"
	corsMaxAge           = "Access-Control-Max-Age"
	corsVary             = "Vary"
)

// CORS is the browser boundary for V3 only. In particular, it permits the
// idempotency and CSRF headers required by authenticated Commands.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	policy := &Handler{allowedOrigins: normalizeOrigins(allowedOrigins)}
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			if c.Request.Method == http.MethodOptions {
				writeV3CORSHeaders(c, "")
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}
		if !policy.originAllowed(c.Request) {
			ensureRequestIdentity(c)
			policy.writeProblem(c, http.StatusForbidden, "ORIGIN_FORBIDDEN", "Origin is not allowed for the V3 API")
			c.Abort()
			return
		}
		writeV3CORSHeaders(c, origin)
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func writeV3CORSHeaders(c *gin.Context, origin string) {
	if origin != "" {
		c.Header(corsAllowOrigin, origin)
		c.Header(corsAllowCredentials, "true")
		c.Header(corsVary, "Origin")
	}
	c.Header(corsAllowMethods, "GET, POST, OPTIONS")
	c.Header(corsAllowHeaders, "Content-Type, Authorization, X-Request-ID, Idempotency-Key, X-CSRF-Token, Last-Event-ID")
	c.Header(corsExposeHeaders, "X-Request-ID, X-Trace-ID, Idempotent-Replay, X-RateLimit-Limit, X-RateLimit-Remaining, Retry-After")
	c.Header(corsMaxAge, "600")
}

// Recovery keeps V3 panic responses inside the stable problem contract.
func Recovery() gin.HandlerFunc {
	handler := NewHandler(Config{})
	return handler.recoverProblems
}

func (h *Handler) recoverProblems(c *gin.Context) {
	defer func() {
		if recover() != nil {
			ensureRequestIdentity(c)
			h.writeProblem(c, http.StatusInternalServerError, "INTERNAL_ERROR", "request could not be completed")
			c.Abort()
		}
	}()
	c.Next()
}

// LimitRequestBody provides the V3 413 problem response and also bounds
// chunked requests through http.MaxBytesReader.
func LimitRequestBody(maxBytes int64) gin.HandlerFunc {
	handler := NewHandler(Config{})
	return func(c *gin.Context) {
		if maxBytes > 0 && c.Request.ContentLength > maxBytes {
			ensureRequestIdentity(c)
			handler.writeProblem(c, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body exceeds the configured limit")
			c.Abort()
			return
		}
		if maxBytes > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

type SlidingWindowLimiter interface {
	Enabled() bool
	AllowSlidingWindow(context.Context, string, int64, time.Duration, time.Time) (bool, int64, error)
}

type RateLimitConfig struct {
	Enabled          bool
	Requests         int64
	Window           time.Duration
	OperationTimeout time.Duration
}

// RateLimit mirrors the existing fail-open infrastructure behavior while
// preserving the V3 error representation on an actual rejection.
func RateLimit(store SlidingWindowLimiter, config RateLimitConfig) gin.HandlerFunc {
	handler := NewHandler(Config{})
	if !config.Enabled || config.Requests <= 0 || config.Window <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = 500 * time.Millisecond
	}
	return func(c *gin.Context) {
		if store == nil || !store.Enabled() {
			c.Next()
			return
		}
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		key := fmt.Sprintf("cloudops:rate_limit:v3:%s:%s", c.ClientIP(), path)
		ctx, cancel := context.WithTimeout(c.Request.Context(), config.OperationTimeout)
		allowed, remaining, err := store.AllowSlidingWindow(ctx, key, config.Requests, config.Window, time.Now().UTC())
		cancel()
		if err != nil {
			c.Next()
			return
		}
		c.Header("X-RateLimit-Limit", strconv.FormatInt(config.Requests, 10))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		c.Header("X-RateLimit-Window-Seconds", strconv.FormatFloat(config.Window.Seconds(), 'f', 0, 64))
		if !allowed {
			c.Header("Retry-After", strconv.FormatFloat(config.Window.Seconds(), 'f', 0, 64))
			ensureRequestIdentity(c)
			handler.writeProblem(c, http.StatusTooManyRequests, "RATE_LIMITED", "request rate limit exceeded")
			c.Abort()
			return
		}
		c.Next()
	}
}
