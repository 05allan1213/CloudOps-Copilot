package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"server-web/config"
	rediscache "server-web/redis"
)

type rateLimitStore interface {
	Enabled() bool
	AllowSlidingWindow(ctx context.Context, key string, limit int64, window time.Duration, now time.Time) (bool, int64, error)
}

func RateLimit(store rateLimitStore, cfg config.RateLimitConfig) gin.HandlerFunc {
	if !cfg.Enabled || cfg.Requests <= 0 || cfg.Window <= 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		if shouldSkipRateLimit(c.Request.URL.Path) {
			c.Next()
			return
		}
		if store == nil || !store.Enabled() {
			c.Next()
			return
		}

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		key := rateLimitKey(c.ClientIP(), path)

		ctx, cancel := context.WithTimeout(c.Request.Context(), cfg.OperationTimeout)
		defer cancel()

		allowed, remaining, err := store.AllowSlidingWindow(ctx, key, cfg.Requests, cfg.Window, time.Now().UTC())
		if err != nil {
			zap.L().Warn("rate limit check failed",
				zap.String("key", key),
				zap.Error(err),
			)
			c.Next()
			return
		}

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", cfg.Requests))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Window-Seconds", fmt.Sprintf("%.0f", cfg.Window.Seconds()))

		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"status": "error",
				"error":  "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}

func shouldSkipRateLimit(path string) bool {
	if path == "/metrics" || path == "/healthz" || path == "/readyz" {
		return true
	}
	return !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/ws/")
}

func rateLimitKey(ip, path string) string {
	return fmt.Sprintf("%s:%s:%s", rediscache.RateLimitKeyPrefix, ip, path)
}
