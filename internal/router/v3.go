package router

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	authpkg "github.com/05allan1213/CloudOps-Copilot/internal/service/auth"
)

func registerV3Routes(engine *gin.Engine, cfg config.Config, deps Dependencies) error {
	var authenticator apiv3.Authenticator
	requireAuth := cfg.AuthEnabled
	requireCSRF := cfg.AuthEnabled
	csrfSecret := deriveV3CSRFSecret(cfg.JWTSecret)
	if cfg.V3ProxyAuthEnabled {
		proxyAuthenticator, err := apiv3.NewOAuthProxyAuthenticator(apiv3.OAuthProxyAuthConfig{
			ViewerLogins: cfg.V3OAuthViewerLogins, OperatorLogins: cfg.V3OAuthOperatorLogins,
		})
		if err != nil {
			return fmt.Errorf("configure V3 oauth2-proxy authentication: %w", err)
		}
		csrfSecret, err = readV3CSRFSecret(cfg.V3CSRFSecretFile)
		if err != nil {
			return err
		}
		authenticator = proxyAuthenticator
		requireAuth = true
		requireCSRF = true
	} else if deps.AuthService != nil {
		authenticator = v3CompatibilityAuth{service: deps.AuthService}
	}
	handler := apiv3.NewHandler(apiv3.Config{
		Queries:        deps.V3Queries,
		Commands:       deps.V3Commands,
		Authenticator:  authenticator,
		RequireAuth:    requireAuth,
		RequireCSRF:    requireCSRF,
		CSRFSecret:     csrfSecret,
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
	return nil
}

func deriveV3CSRFSecret(jwtSecret string) []byte {
	if strings.TrimSpace(jwtSecret) == "" {
		return nil
	}
	digest := sha256.Sum256([]byte("cloudops:v3:csrf:v1\x00" + jwtSecret))
	return digest[:]
}

func readV3CSRFSecret(path string) ([]byte, error) {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read V3 CSRF signing secret: %w", err)
	}
	secret := bytes.TrimSpace(raw)
	if len(secret) < sha256.Size {
		return nil, errors.New("V3 CSRF signing secret must contain at least 32 bytes")
	}
	return append([]byte(nil), secret...), nil
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

func (a v3CompatibilityAuth) Authenticate(_ context.Context, request *http.Request) (apiv3.Identity, error) {
	if a.service == nil {
		return apiv3.Identity{}, errors.New("compatibility auth is unavailable")
	}
	if request == nil {
		return apiv3.Identity{}, errors.New("compatibility request is unavailable")
	}
	identity, err := a.service.AuthenticateBearer(request.Header.Get("Authorization"))
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
