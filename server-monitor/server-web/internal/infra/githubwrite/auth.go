package githubwrite

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

	"server-web/internal/remediation"
)

type TokenProvider interface {
	Token(context.Context) (string, error)
}

type FileTokenProvider struct{ Path string }

func (p FileTokenProvider) Token(context.Context) (string, error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("github write token unavailable: %w", remediation.ErrForbidden)
	}
	token := strings.TrimSpace(string(data))
	if token == "" || len(token) > 4096 {
		return "", fmt.Errorf("github write token invalid: %w", remediation.ErrInvalidArgument)
	}
	return token, nil
}

type AppTokenConfig struct {
	BaseURL, PrivateKeyFile string
	AppID, InstallationID   int64
	AllowedRepositories     []string
	HTTPClient              *http.Client
	Now                     func() time.Time
}

type AppTokenProvider struct {
	baseURL               *url.URL
	privateKeyFile        string
	appID, installationID int64
	repositories          []string
	client                *http.Client
	now                   func() time.Time
	mu                    sync.Mutex
	token                 string
	expiresAt             time.Time
}

func NewAppTokenProvider(cfg AppTokenConfig) (*AppTokenProvider, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || cfg.AppID <= 0 || cfg.InstallationID <= 0 || strings.TrimSpace(cfg.PrivateKeyFile) == "" || len(cfg.AllowedRepositories) == 0 {
		return nil, fmt.Errorf("%w: GitHub write App configuration", remediation.ErrInvalidArgument)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AppTokenProvider{baseURL: base, privateKeyFile: cfg.PrivateKeyFile, appID: cfg.AppID, installationID: cfg.InstallationID, repositories: append([]string(nil), cfg.AllowedRepositories...), client: client, now: now}, nil
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
	payload, _ := json.Marshal(map[string]any{"permissions": map[string]string{"contents": "write", "pull_requests": "write"}, "repositories": p.repositories})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(payload)))
	if err != nil {
		return "", remediation.ErrInvalidArgument
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "cloudops-copilot-gitops-writer")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github write installation token unavailable: %w", remediation.ErrForbidden)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("github write installation token denied: %w", remediation.ErrForbidden)
	}
	var result struct {
		Token       string            `json:"token"`
		ExpiresAt   time.Time         `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result) != nil || result.Token == "" || result.ExpiresAt.Before(now.Add(time.Minute)) {
		return "", fmt.Errorf("github write installation token invalid: %w", remediation.ErrForbidden)
	}
	if err := validateWritePermissions(result.Permissions); err != nil {
		return "", err
	}
	p.token, p.expiresAt = result.Token, result.ExpiresAt.UTC()
	return p.token, nil
}

func validateWritePermissions(permissions map[string]string) error {
	required := map[string]string{"metadata": "read", "contents": "write", "pull_requests": "write"}
	if len(permissions) != len(required) {
		return fmt.Errorf("github write installation permissions exceed boundary: %w", remediation.ErrForbidden)
	}
	for name, level := range permissions {
		if required[name] != strings.ToLower(level) {
			return fmt.Errorf("github write installation permissions exceed boundary: %w", remediation.ErrForbidden)
		}
	}
	return nil
}

func (p *AppTokenProvider) signJWT(now time.Time) (string, error) {
	data, err := os.ReadFile(p.privateKeyFile)
	if err != nil {
		return "", fmt.Errorf("github write private key unavailable: %w", remediation.ErrForbidden)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("github write private key invalid: %w", remediation.ErrInvalidArgument)
	}
	var key *rsa.PrivateKey
	if parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	}
	if key == nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("github write private key invalid: %w", remediation.ErrInvalidArgument)
		}
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": p.appID})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("github write jwt signing failed: %w", remediation.ErrForbidden)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
