package apiv3

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type identityContextKey struct{}

type csrfClaims struct {
	Provider  string `json:"provider"`
	Login     string `json:"login"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
	Nonce     string `json:"nonce"`
}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

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

func (h *Handler) authenticate(c *gin.Context) {
	if !h.requireAuth {
		identity := Identity{Subject: "anonymous", Provider: "local", Login: "anonymous", Role: "operator"}
		c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), identity))
		c.Next()
		return
	}
	if h.authenticator == nil {
		h.writeProblem(c, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication service is unavailable")
		c.Abort()
		return
	}
	identity, err := h.authenticator.Authenticate(c.Request.Context(), c.Request)
	if err != nil || !validIdentity(identity) {
		h.writeProblem(c, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "a trusted authenticated session is required")
		c.Abort()
		return
	}
	if err := h.authenticator.Verify(c.Request.Context(), identity); err != nil {
		h.writeProblem(c, http.StatusUnauthorized, "AUTHENTICATION_REVOKED", "the authenticated session is no longer valid")
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), identity))
	c.Next()
}

func (h *Handler) requireViewer(c *gin.Context) {
	if !h.requireAuth {
		c.Next()
		return
	}
	identity, ok := IdentityFromContext(c.Request.Context())
	if !ok || (identity.Role != "viewer" && identity.Role != "operator") {
		h.writeProblem(c, http.StatusForbidden, "ROLE_FORBIDDEN", "viewer or operator role is required")
		c.Abort()
		return
	}
	c.Next()
}

func (h *Handler) requireOperator(c *gin.Context) {
	if !h.requireAuth {
		c.Next()
		return
	}
	identity, ok := IdentityFromContext(c.Request.Context())
	if !ok || identity.Role != "operator" {
		h.writeProblem(c, http.StatusForbidden, "ROLE_FORBIDDEN", "operator role is required")
		c.Abort()
		return
	}
	c.Next()
}

func (h *Handler) requireCSRFToken(c *gin.Context) {
	if !h.requireCSRF {
		c.Next()
		return
	}
	if !h.originAllowed(c.Request) {
		h.writeProblem(c, http.StatusForbidden, "ORIGIN_FORBIDDEN", "Origin is not allowed for authenticated commands")
		c.Abort()
		return
	}
	identity, ok := IdentityFromContext(c.Request.Context())
	if !ok {
		h.writeProblem(c, http.StatusForbidden, "CSRF_INVALID", "CSRF token is not valid for this session")
		c.Abort()
		return
	}
	token := strings.TrimSpace(c.GetHeader(CSRFHeader))
	if token == "" || len(token) > 768 {
		h.writeProblem(c, http.StatusForbidden, "CSRF_REQUIRED", "CSRF token is required")
		c.Abort()
		return
	}
	if !h.verifyCSRFToken(token, identity, h.now().UTC()) {
		h.writeProblem(c, http.StatusForbidden, "CSRF_INVALID", "CSRF token is not valid for this session")
		c.Abort()
		return
	}
	c.Next()
}

func (h *Handler) issueCSRF(c *gin.Context) {
	identity, ok := IdentityFromContext(c.Request.Context())
	if !ok {
		h.writeProblem(c, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "authenticated session is required")
		return
	}
	if len(h.csrfSecret) < sha256.Size {
		h.writeProblem(c, http.StatusServiceUnavailable, "CSRF_UNAVAILABLE", "CSRF signing is unavailable")
		return
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "CSRF_ISSUE_FAILED", "unable to issue CSRF token")
		return
	}
	now := h.now().UTC()
	expiresAt := now.Add(h.csrfTTL)
	claims := csrfClaims{
		Provider:  identity.Provider,
		Login:     identity.Login,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(random),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "CSRF_ISSUE_FAILED", "unable to issue CSRF token")
		return
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	token := encodedPayload + "." + base64.RawURLEncoding.EncodeToString(h.signCSRF(encodedPayload))
	h.writeJSON(c, http.StatusOK, csrfResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		Actor: SessionActor{
			Provider: identity.Provider,
			Login:    identity.Login,
			Role:     identity.Role,
		},
	})
}

func (h *Handler) verifyCSRFToken(token string, identity Identity, now time.Time) bool {
	if len(h.csrfSecret) < sha256.Size {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, h.signCSRF(parts[0])) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var claims csrfClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	issuedAt := time.Unix(claims.IssuedAt, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	return claims.Provider == identity.Provider && claims.Login == identity.Login && claims.Nonce != "" &&
		!issuedAt.After(now.Add(30*time.Second)) && expiresAt.After(now) &&
		expiresAt.After(issuedAt) && !expiresAt.After(issuedAt.Add(h.csrfTTL+time.Second))
}

func (h *Handler) signCSRF(payload string) []byte {
	mac := hmac.New(sha256.New, h.csrfSecret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
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

func validIdentity(identity Identity) bool {
	return len(identity.Subject) >= 1 && len(identity.Subject) <= 256 &&
		len(identity.Provider) >= 1 && len(identity.Provider) <= 32 &&
		len(identity.Login) >= 1 && len(identity.Login) <= 128 &&
		!containsControl(identity.Subject) && !containsControl(identity.Provider) && !containsControl(identity.Login)
}

func boundedRequestID(value string) string {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) || containsControl(value) {
		return ""
	}
	return value
}
