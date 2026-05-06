package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	authpkg "server-web/auth"
)

const (
	ContextUserID   = "user_id"
	ContextUsername = "username"
	ContextRole     = "role"
)

type authVerifier interface {
	AuthenticateBearer(authHeader string) (authpkg.Identity, error)
	AuthenticateToken(token string) (authpkg.Identity, error)
}

type tokenVersionVerifier interface {
	VerifyTokenVersion(ctx context.Context, identity authpkg.Identity) error
}

func Auth(verifier authVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "auth service unavailable",
			})
			return
		}

		identity, err := verifier.AuthenticateBearer(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"status": "error",
				"error":  "invalid or expired token",
			})
			return
		}

		c.Set(ContextUserID, identity.ID)
		c.Set(ContextUsername, identity.Username)
		c.Set(ContextRole, identity.Role)
		c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), identity))
		c.Next()
	}
}

func AuthWebSocket(verifier authVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "auth service unavailable",
			})
			return
		}

		identity, err := verifier.AuthenticateBearer(c.GetHeader("Authorization"))
		if errors.Is(err, authpkg.ErrBearerTokenMissing) {
			token := c.Query("token")
			if token == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"status": "error",
					"error":  "authorization header required",
				})
				return
			}
			identity, err = verifier.AuthenticateToken(token)
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"status": "error",
				"error":  "invalid or expired token",
			})
			return
		}

		c.Set(ContextUserID, identity.ID)
		c.Set(ContextUsername, identity.Username)
		c.Set(ContextRole, identity.Role)
		c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), identity))
		c.Next()
	}
}

func VerifyTokenVersion(verifier tokenVersionVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			c.Next()
			return
		}

		identity, ok := IdentityFromContext(c.Request.Context())
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"status": "error",
				"error":  "identity not found in context",
			})
			return
		}

		if err := verifier.VerifyTokenVersion(c.Request.Context(), identity); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"status": "error",
				"error":  "token has been revoked",
			})
			return
		}

		c.Next()
	}
}

type identityContextKey struct{}

func WithIdentity(ctx context.Context, identity authpkg.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (authpkg.Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(authpkg.Identity)
	return identity, ok
}
