package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowOrigin  = "Access-Control-Allow-Origin"
	corsAllowMethods = "Access-Control-Allow-Methods"
	corsAllowHeaders = "Access-Control-Allow-Headers"
	corsMaxAge       = "Access-Control-Max-Age"
	corsVary         = "Vary"
)

func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := buildOriginSet(allowedOrigins)
	if len(allowed) == 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		allowOrigin, ok := matchOrigin(origin, allowed)
		if !ok {
			c.Next()
			return
		}

		c.Header(corsAllowOrigin, allowOrigin)
		if allowOrigin != "*" {
			c.Header(corsVary, "Origin")
		}
		c.Header(corsAllowMethods, "GET, POST, OPTIONS")
		c.Header(corsAllowHeaders, "Content-Type, Authorization, X-Request-ID")
		c.Header(corsMaxAge, "600")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func buildOriginSet(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	return allowed
}

func matchOrigin(origin string, allowed map[string]struct{}) (string, bool) {
	if _, ok := allowed["*"]; ok {
		return "*", true
	}
	if _, ok := allowed[origin]; ok {
		return origin, true
	}
	return "", false
}
