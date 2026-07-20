package apiv3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthProxyAuthenticatorUsesOnlyAllowlistedGitHubLogin(t *testing.T) {
	authenticator, err := NewOAuthProxyAuthenticator(OAuthProxyAuthConfig{
		ViewerLogins: []string{"viewer-user"}, OperatorLogins: []string{"Ops-User"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil)
	request.Header.Set(OAuthProxyUserHeader, "Ops-User")
	request.Header.Set("X-Auth-Request-Email", "attacker@example.invalid")
	request.Header.Set("X-Auth-Request-Groups", "admin")
	identity, err := authenticator.Authenticate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if identity != (Identity{Subject: "github-login:ops-user", Provider: "github", Login: "ops-user", Role: "operator"}) {
		t.Fatalf("identity=%+v", identity)
	}
	if err := authenticator.Verify(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	identity.Role = "viewer"
	if err := authenticator.Verify(context.Background(), identity); err == nil {
		t.Fatal("forged role was accepted")
	}
}

func TestOAuthProxyAuthenticatorRejectsForgedAndForwardedCredentials(t *testing.T) {
	authenticator, err := NewOAuthProxyAuthenticator(OAuthProxyAuthConfig{OperatorLogins: []string{"operator"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		apply func(http.Header)
	}{
		{name: "missing user", apply: func(http.Header) {}},
		{name: "unlisted user", apply: func(header http.Header) { header.Set(OAuthProxyUserHeader, "intruder") }},
		{name: "authorization", apply: func(header http.Header) {
			header.Set(OAuthProxyUserHeader, "operator")
			header.Set("Authorization", "Bearer oauth-access-token")
		}},
		{name: "session cookie", apply: func(header http.Header) {
			header.Set(OAuthProxyUserHeader, "operator")
			header.Set("Cookie", "_oauth2_proxy=session")
		}},
		{name: "forwarded user", apply: func(header http.Header) {
			header.Set(OAuthProxyUserHeader, "operator")
			header.Set("X-Forwarded-User", "intruder")
		}},
		{name: "access token", apply: func(header http.Header) {
			header.Set(OAuthProxyUserHeader, "operator")
			header.Set("X-Auth-Request-Access-Token", "secret")
		}},
		{name: "ambiguous user", apply: func(header http.Header) {
			header.Add(OAuthProxyUserHeader, "operator")
			header.Add(OAuthProxyUserHeader, "intruder")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil)
			test.apply(request.Header)
			if _, err := authenticator.Authenticate(context.Background(), request); err == nil {
				t.Fatal("untrusted proxy request was accepted")
			}
		})
	}
}

func TestOAuthProxyAuthenticatorRejectsInvalidRoleMaps(t *testing.T) {
	for _, config := range []OAuthProxyAuthConfig{
		{},
		{ViewerLogins: []string{"invalid login"}},
		{ViewerLogins: []string{"same"}, OperatorLogins: []string{"SAME"}},
	} {
		if _, err := NewOAuthProxyAuthenticator(config); err == nil {
			t.Fatalf("invalid config accepted: %+v", config)
		}
	}
}
