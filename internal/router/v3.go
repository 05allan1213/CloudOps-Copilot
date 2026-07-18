package router

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	authpkg "github.com/05allan1213/CloudOps-Copilot/internal/service/auth"
)

func registerV3Routes(engine *gin.Engine, cfg config.Config, deps Dependencies) {
	var authenticator apiv3.Authenticator
	if deps.AuthService != nil {
		authenticator = v3CompatibilityAuth{service: deps.AuthService}
	}
	handler := apiv3.NewHandler(apiv3.Config{
		Queries:        deps.V3Queries,
		Commands:       deps.V3Commands,
		Authenticator:  authenticator,
		RequireAuth:    cfg.AuthEnabled,
		RequireCSRF:    cfg.AuthEnabled,
		CSRFSecret:     deriveV3CSRFSecret(cfg.JWTSecret),
		AllowedOrigins: cfg.CORSOrigins,
	})
	apiv3.RegisterRoutes(engine.Group("/api/v3"), handler)
	engine.NoRoute(func(c *gin.Context) {
		if isV3Path(c.Request.URL.Path) {
			apiv3.WriteRouteNotFound(c)
			return
		}
		c.Header("Content-Type", "text/plain")
		c.Status(404)
		_, _ = c.Writer.Write([]byte("404 page not found"))
	})
}

func deriveV3CSRFSecret(jwtSecret string) []byte {
	if strings.TrimSpace(jwtSecret) == "" {
		return nil
	}
	digest := sha256.Sum256([]byte("cloudops:v3:csrf:v1\x00" + jwtSecret))
	return digest[:]
}

func selectV3Middleware(v3, legacy gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isV3Path(c.Request.URL.Path) {
			v3(c)
			return
		}
		legacy(c)
	}
}

func isV3Path(path string) bool {
	return path == "/api/v3" || strings.HasPrefix(path, "/api/v3/")
}

// v3CompatibilityAuth preserves the current local JWT behavior during Phase
// 2. GitHub OAuth/oauth2-proxy replaces this adapter in its owning phase.
type v3CompatibilityAuth struct {
	service interface {
		AuthenticateBearer(string) (authpkg.Identity, error)
		VerifyTokenVersion(context.Context, authpkg.Identity) error
	}
}

func (a v3CompatibilityAuth) AuthenticateBearer(_ context.Context, authorization string) (apiv3.Identity, error) {
	if a.service == nil {
		return apiv3.Identity{}, errors.New("compatibility auth is unavailable")
	}
	identity, err := a.service.AuthenticateBearer(authorization)
	if err != nil {
		return apiv3.Identity{}, err
	}
	role := "viewer"
	if identity.Role == "admin" {
		role = "operator"
	}
	return apiv3.Identity{
		Subject:  fmt.Sprintf("local:%d:%d", identity.ID, identity.TokenVersion),
		Provider: "local", Login: identity.Username, Role: role,
	}, nil
}

func (a v3CompatibilityAuth) Verify(ctx context.Context, identity apiv3.Identity) error {
	if a.service == nil || identity.Provider != "local" {
		return errors.New("compatibility identity is invalid")
	}
	parts := strings.Split(identity.Subject, ":")
	if len(parts) != 3 || parts[0] != "local" {
		return errors.New("compatibility subject is invalid")
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 {
		return errors.New("compatibility subject id is invalid")
	}
	version, err := strconv.Atoi(parts[2])
	if err != nil || version < 0 {
		return errors.New("compatibility token version is invalid")
	}
	role := "viewer"
	if identity.Role == "operator" {
		role = "admin"
	}
	return a.service.VerifyTokenVersion(ctx, authpkg.Identity{ID: id, Username: identity.Login, Role: role, TokenVersion: version})
}
