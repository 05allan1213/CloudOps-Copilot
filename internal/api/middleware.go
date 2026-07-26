package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

func (h *Handler) requestIdentity(c *gin.Context) {
	ensureRequestIdentity(c)
	c.Next()
}

func ensureRequestIdentity(c *gin.Context) {
	if requestID, exists := c.Get(requestIDContextKey); exists && requestID != "" {
		return
	}
	requestID := boundedRequestID(c.GetHeader(RequestIDHeader))
	if requestID == "" {
		requestID = boundedRequestID(c.Writer.Header().Get(RequestIDHeader))
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	traceID := trace.SpanContextFromContext(c.Request.Context()).TraceID().String()
	if traceID == "00000000000000000000000000000000" || traceID == "" {
		traceID = requestID
	}
	c.Set(requestIDContextKey, requestID)
	c.Set(traceIDContextKey, traceID)
	c.Header(RequestIDHeader, requestID)
	c.Header(TraceIDHeader, traceID)
}

func (h *Handler) requireMutationOrigin(c *gin.Context) {
	if strings.TrimSpace(c.GetHeader("Origin")) == "" {
		h.writeProblem(c, http.StatusForbidden, "ORIGIN_REQUIRED", "Origin is required for mutation requests")
		c.Abort()
		return
	}
	if !h.originAllowed(c.Request) {
		h.writeProblem(c, http.StatusForbidden, "ORIGIN_FORBIDDEN", "Origin is not allowed for mutation requests")
		c.Abort()
		return
	}
	c.Next()
}

func normalizeOrigins(origins []string) map[string]struct{} {
	result := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
		parsed, err := url.Parse(raw)
		if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.Path == "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" {
			result[parsed.Scheme+"://"+strings.ToLower(parsed.Host)] = struct{}{}
		}
	}
	return result
}

func (h *Handler) originAllowed(request *http.Request) bool {
	if request == nil {
		return false
	}
	raw := strings.TrimSuffix(strings.TrimSpace(request.Header.Get("Origin")), "/")
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	normalized := parsed.Scheme + "://" + strings.ToLower(parsed.Host)
	if len(h.allowedOrigins) > 0 {
		_, ok := h.allowedOrigins[normalized]
		return ok
	}
	return strings.EqualFold(parsed.Host, request.Host)
}

func boundedRequestID(value string) string {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) || containsControl(value) {
		return ""
	}
	return value
}
