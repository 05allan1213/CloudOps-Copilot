package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				path := c.FullPath()
				if path == "" {
					path = c.Request.URL.Path
				}

				logger.FromContext(c.Request.Context()).Error("http request panic recovered",
					zap.String("request_id", RequestID(c)),
					zap.String("method", c.Request.Method),
					zap.String("path", path),
					zap.Any("error", recovered),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"status": "error",
					"error":  http.StatusText(http.StatusInternalServerError),
				})
			}
		}()

		c.Next()
	}
}
