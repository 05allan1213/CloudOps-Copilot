package githubread

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
)

type TokenProvider interface {
	Token(context.Context) (string, error)
}

type FileTokenProvider struct{ Path string }

func (p FileTokenProvider) Token(context.Context) (string, error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("github token file unavailable: %w", change.ErrUnavailable)
	}
	token := strings.TrimSpace(string(data))
	if token == "" || len(token) > 4096 {
		return "", fmt.Errorf("github token file invalid: %w", change.ErrInvalidArgument)
	}
	return token, nil
}

type AppTokenConfig struct {
	BaseURL             string
	AppID               int64
	InstallationID      int64
	PrivateKeyFile      string
	HTTPClient          *http.Client
	APIVersion          string
	AllowedRepositories []string
	Now                 func() time.Time
}

type AppTokenProvider struct {
	baseURL        *url.URL
	appID          int64
	installationID int64
	privateKeyFile string
	client         *http.Client
	apiVersion     string
	repositories   []string
	now            func() time.Time
	mu             sync.Mutex
	token          string
	expiresAt      time.Time
}

func NewAppTokenProvider(cfg AppTokenConfig) (*AppTokenProvider, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || cfg.AppID <= 0 || cfg.InstallationID <= 0 || strings.TrimSpace(cfg.PrivateKeyFile) == "" {
		return nil, fmt.Errorf("%w: invalid GitHub App authentication configuration", change.ErrInvalidArgument)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	version := cfg.APIVersion
	if version == "" {
		version = "2022-11-28"
	}
	return &AppTokenProvider{baseURL: base, appID: cfg.AppID, installationID: cfg.InstallationID, privateKeyFile: cfg.PrivateKeyFile, client: client, apiVersion: version, repositories: append([]string(nil), cfg.AllowedRepositories...), now: now}, nil
}

func (p *AppTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now().UTC()
	if p.token != "" && now.Add(5*time.Minute).Before(p.expiresAt) {
		return p.token, nil
	}
	jwt, err := p.signJWT(now)
	if err != nil {
		return "", err
	}
	endpoint := *p.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/app/installations/" + strconv.FormatInt(p.installationID, 10) + "/access_tokens"
	body := map[string]any{"permissions": map[string]string{"contents": "read", "pull_requests": "read", "checks": "read", "actions": "read"}}
	if len(p.repositories) > 0 {
		body["repositories"] = p.repositories
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("github installation token request invalid: %w", change.ErrInvalidArgument)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", p.apiVersion)
	req.Header.Set("User-Agent", "cloudops-copilot-change-intelligence")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github installation token unavailable: %w", change.ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("github installation token denied: %w", change.ErrPermission)
		}
		return "", fmt.Errorf("github installation token unavailable: %w", change.ErrUnavailable)
	}
	var response struct {
		Token       string            `json:"token"`
		ExpiresAt   time.Time         `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64*1024))
	if decoder.Decode(&response) != nil || strings.TrimSpace(response.Token) == "" || response.ExpiresAt.Before(now.Add(time.Minute)) {
		return "", fmt.Errorf("github installation token response invalid: %w", change.ErrUnavailable)
	}
	if err := validateInstallationPermissions(response.Permissions); err != nil {
		return "", err
	}
	p.token, p.expiresAt = response.Token, response.ExpiresAt.UTC()
	return p.token, nil
}

func validateInstallationPermissions(permissions map[string]string) error {
	required := map[string]struct{}{"contents": {}, "pull_requests": {}, "checks": {}, "actions": {}}
	allowed := map[string]struct{}{"metadata": {}, "contents": {}, "pull_requests": {}, "checks": {}, "actions": {}}
	for permission, level := range permissions {
		if _, ok := allowed[permission]; !ok || !strings.EqualFold(level, "read") {
			return fmt.Errorf("github installation token permissions exceed read boundary: %w", change.ErrPermission)
		}
		delete(required, permission)
	}
	if len(required) != 0 {
		return fmt.Errorf("github installation token lacks required read permissions: %w", change.ErrPermission)
	}
	return nil
}

func (p *AppTokenProvider) signJWT(now time.Time) (string, error) {
	data, err := os.ReadFile(p.privateKeyFile)
	if err != nil {
		return "", fmt.Errorf("github app private key unavailable: %w", change.ErrUnavailable)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("github app private key invalid: %w", change.ErrInvalidArgument)
	}
	var key *rsa.PrivateKey
	parsedPKCS8, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if pkcs8Err == nil {
		key, _ = parsedPKCS8.(*rsa.PrivateKey)
	}
	if key == nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("github app private key invalid: %w", change.ErrInvalidArgument)
		}
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": p.appID})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("github app jwt signing failed: %w", change.ErrUnavailable)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func tokenErrorContainsSecret(err error, secret string) bool {
	return err != nil && secret != "" && strings.Contains(err.Error(), secret)
}
