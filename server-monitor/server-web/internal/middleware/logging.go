package middleware

import (
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

const requestIDHeader = "X-Request-ID"
const requestIDKey = "request_id"

var requestIDCounter uint64

func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := c.GetHeader(requestIDHeader)
		if requestID == "" {
			requestID = newRequestID(start)
		}
		c.Set(requestIDKey, requestID)
		c.Header(requestIDHeader, requestID)

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		latency := time.Since(start)
		logger.FromContext(c.Request.Context()).Info("http request",
			zap.String("request_id", requestID),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Float64("latency_ms", float64(latency.Microseconds())/1000),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}

func RequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(requestIDKey); ok {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return c.GetHeader(requestIDHeader)
}

func newRequestID(now time.Time) string {
	seq := atomic.AddUint64(&requestIDCounter, 1)
	return strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatUint(seq, 36)
}
