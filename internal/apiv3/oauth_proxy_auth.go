package apiv3

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

const OAuthProxyUserHeader = "X-Auth-Request-User"

var oauthProxyLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

type OAuthProxyAuthConfig struct {
	ViewerLogins   []string
	OperatorLogins []string
}

// OAuthProxyAuthenticator trusts exactly one identity value produced by the
// loopback oauth2-proxy sidecar. It deliberately rejects leaked OAuth/session
// credentials and never treats a mutable GitHub login as a numeric subject.
type OAuthProxyAuthenticator struct {
	roles map[string]string
}

func NewOAuthProxyAuthenticator(config OAuthProxyAuthConfig) (*OAuthProxyAuthenticator, error) {
	roles := make(map[string]string, len(config.ViewerLogins)+len(config.OperatorLogins))
	add := func(values []string, role string) error {
		for _, raw := range values {
			login := strings.TrimSpace(raw)
			if !oauthProxyLoginPattern.MatchString(login) {
				return errors.New("oauth2-proxy login allowlist is invalid")
			}
			login = strings.ToLower(login)
			if _, exists := roles[login]; exists {
				return errors.New("oauth2-proxy login is mapped more than once")
			}
			roles[login] = role
		}
		return nil
	}
	if err := add(config.ViewerLogins, "viewer"); err != nil {
		return nil, err
	}
	if err := add(config.OperatorLogins, "operator"); err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return nil, errors.New("oauth2-proxy login allowlist is required")
	}
	return &OAuthProxyAuthenticator{roles: roles}, nil
}

func (a *OAuthProxyAuthenticator) Authenticate(_ context.Context, request *http.Request) (Identity, error) {
	if a == nil || len(a.roles) == 0 || request == nil {
		return Identity{}, errors.New("oauth2-proxy authentication is unavailable")
	}
	if leakedProxyCredential(request.Header) {
		return Identity{}, errors.New("oauth2-proxy credential forwarding is forbidden")
	}
	values := request.Header.Values(OAuthProxyUserHeader)
	if len(values) != 1 {
		return Identity{}, errors.New("oauth2-proxy user header is missing or ambiguous")
	}
	login := strings.TrimSpace(values[0])
	if login != values[0] || !oauthProxyLoginPattern.MatchString(login) {
		return Identity{}, errors.New("oauth2-proxy user header is invalid")
	}
	login = strings.ToLower(login)
	role, allowed := a.roles[login]
	if !allowed {
		return Identity{}, errors.New("oauth2-proxy user is not allowlisted")
	}
	return Identity{
		Subject:  "github-login:" + login,
		Provider: "github",
		Login:    login,
		Role:     role,
	}, nil
}

func (a *OAuthProxyAuthenticator) Verify(_ context.Context, identity Identity) error {
	if a == nil || identity.Provider != "github" || identity.Login == "" ||
		identity.Login != strings.ToLower(strings.TrimSpace(identity.Login)) ||
		identity.Subject != "github-login:"+identity.Login {
		return errors.New("oauth2-proxy identity is invalid")
	}
	role, allowed := a.roles[identity.Login]
	if !allowed || role != identity.Role {
		return errors.New("oauth2-proxy identity is no longer allowlisted")
	}
	return nil
}

func leakedProxyCredential(header http.Header) bool {
	for _, name := range []string{
		"Authorization",
		"Proxy-Authorization",
		"Cookie",
		"X-Forwarded-User",
		"X-Forwarded-Access-Token",
		"X-Auth-Request-Access-Token",
	} {
		if strings.TrimSpace(header.Get(name)) != "" {
			return true
		}
	}
	return false
}
