package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	authpkg "server-web/internal/service/auth"
)

type webSocketAuthVerifier struct {
	token string
}

func (v *webSocketAuthVerifier) AuthenticateBearer(header string) (authpkg.Identity, error) {
	if header == "Bearer header-token" {
		return authpkg.Identity{ID: 1, Username: "header", Role: "viewer"}, nil
	}
	return authpkg.Identity{}, authpkg.ErrBearerTokenMissing
}

func (v *webSocketAuthVerifier) AuthenticateToken(token string) (authpkg.Identity, error) {
	v.token = token
	if token == "protocol-token" {
		return authpkg.Identity{ID: 2, Username: "protocol", Role: "viewer"}, nil
	}
	return authpkg.Identity{}, errors.New("invalid token")
}

func TestAuthWebSocketUsesHeaderOrBearerSubprotocolAndRejectsQueryTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, configure := range map[string]func(*http.Request){
		"authorization_header": func(request *http.Request) { request.Header.Set("Authorization", "Bearer header-token") },
		"bearer_subprotocol": func(request *http.Request) {
			request.Header.Set("Sec-WebSocket-Protocol", "cloudops-bearer, protocol-token")
		},
	} {
		t.Run(name, func(t *testing.T) {
			verifier := &webSocketAuthVerifier{}
			router := gin.New()
			router.GET("/ws/alerts", AuthWebSocket(verifier), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/ws/alerts", nil)
			configure(request)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("expected authenticated request, got %d", response.Code)
			}
		})
	}

	verifier := &webSocketAuthVerifier{}
	router := gin.New()
	router.GET("/ws/alerts", AuthWebSocket(verifier), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/ws/alerts?token=protocol-token", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("query-string token must be rejected, got %d", response.Code)
	}
	if verifier.token != "" {
		t.Fatal("query-string token reached token authentication")
	}
}

func TestWebSocketBearerTokenRequiresNamedProtocolPair(t *testing.T) {
	for header, expected := range map[string]string{
		"cloudops-bearer, token":             "token",
		"other, value, cloudops-bearer, jwt": "jwt",
		"cloudops-bearer":                    "",
		"token":                              "",
	} {
		if actual := webSocketBearerToken(header); actual != expected {
			t.Fatalf("header %q: expected %q, got %q", header, expected, actual)
		}
	}
}
