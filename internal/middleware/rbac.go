package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		value, exists := c.Get(ContextRole)
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"status": "error",
				"error":  "role not found in context",
			})
			return
		}

		role, ok := value.(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"status": "error",
				"error":  "invalid role in context",
			})
			return
		}
		if _, ok := allowed[role]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"status": "error",
				"error":  "insufficient permissions",
			})
			return
		}

		c.Next()
	}
}
