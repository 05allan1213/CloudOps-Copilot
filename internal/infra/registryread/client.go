// Package registryread implements the narrow OCI Distribution pull boundary
// used by image resolution. It cannot list, upload, tag, mount, or
// delete registry content.
package registryread

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
)

const (
	manifestOCI    = "application/vnd.oci.image.manifest.v1+json"
	manifestDocker = "application/vnd.docker.distribution.manifest.v2+json"
	configOCI      = "application/vnd.oci.image.config.v1+json"
	configDocker   = "application/vnd.docker.container.image.v1+json"
	configGeneric  = "application/octet-stream"
	maxTokenBytes  = int64(16 * 1024)
	maxSecretBytes = int64(8 * 1024)
)

type ErrorCode string

const (
	ErrorAuthentication ErrorCode = "authentication"
	ErrorPermission     ErrorCode = "permission"
	ErrorNotFound       ErrorCode = "not_found"
	ErrorRateLimit      ErrorCode = "rate_limit"
	ErrorTemporary      ErrorCode = "temporary"
	ErrorInvalid        ErrorCode = "invalid"
	ErrorNotAllowed     ErrorCode = "not_allowed"
	ErrorSizeLimit      ErrorCode = "size_limit"
	ErrorTimeout        ErrorCode = "timeout"
	ErrorCancelled      ErrorCode = "cancelled"
)

type APIError struct {
	Code       ErrorCode
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("registry request failed: code=%s status=%d", e.Code, e.StatusCode)
}
func (e *APIError) RegistryErrorCode() string { return string(e.Code) }

func (e *APIError) Unwrap() error {
	switch e.Code {
	case ErrorAuthentication, ErrorPermission:
		return change.ErrPermission
	case ErrorNotFound:
		return change.ErrNotFound
	case ErrorInvalid, ErrorSizeLimit:
		return change.ErrInvalidArgument
	case ErrorNotAllowed:
		return change.ErrNotAllowed
	case ErrorTimeout:
		return context.DeadlineExceeded
	case ErrorCancelled:
		return context.Canceled
	default:
		return change.ErrUnavailable
	}
}

type Observer interface {
	ObserveRegistryRequest(operation, result string, seconds float64)
	ObserveRegistryResponseLimit(kind string)
	ObserveRegistryCache(result string)
}

type Config struct {
	BaseURL              string
	AllowedHosts         []string
	AllowedRepositories  []string
	AllowedAuthRealms    []string
	AllowedRedirectHosts []string
	BearerTokenFile      string
	UsernameFile         string
	PasswordFile         string
	Timeout              time.Duration
	MaxRetries           int
	ManifestMaxBytes     int64
	ConfigMaxBytes       int64
	CacheTTL             time.Duration
	CacheMaxItems        int
	HTTPClient           *http.Client
	Observer             Observer
	Now                  func() time.Time
	Sleep                func(context.Context, time.Duration) error
}

type cacheValue struct {
	key       string
	metadata  change.RegistryMetadata
	expiresAt time.Time
}

type flight struct {
	done     chan struct{}
	metadata change.RegistryMetadata
	err      error
}

type Client struct {
	baseURL       *url.URL
	registryID    string
	allowedHosts  map[string]struct{}
	repositories  map[string]struct{}
	authRealms    map[string]struct{}
	redirectHosts map[string]struct{}
	bearerFile    string
	usernameFile  string
	passwordFile  string
	timeout       time.Duration
	maxRetries    int
	manifestLimit int64
	configLimit   int64
	cacheTTL      time.Duration
	cacheMaxItems int
	client        *http.Client
	observer      Observer
	now           func() time.Time
	sleep         func(context.Context, time.Duration) error

	mu      sync.Mutex
	cache   map[string]*list.Element
	lru     *list.List
	flights map[string]*flight
}

var (
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	repositoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)
	challengePair     = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)="([^"\r\n]*)"`)
)

var _ change.RegistryMetadataReader = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, fmt.Errorf("%w: registry base must be a fixed HTTPS origin", change.ErrInvalidArgument)
	}
	base.Path = ""
	base.RawPath = ""
	if cfg.Timeout <= 0 || cfg.Timeout > 30*time.Second || cfg.MaxRetries < 0 || cfg.MaxRetries > 3 {
		return nil, fmt.Errorf("%w: registry timeout or retry limit", change.ErrInvalidArgument)
	}
	if cfg.ManifestMaxBytes < 1024 || cfg.ManifestMaxBytes > 4*1024*1024 || cfg.ConfigMaxBytes < 1024 || cfg.ConfigMaxBytes > 1024*1024 {
		return nil, fmt.Errorf("%w: registry response limits", change.ErrInvalidArgument)
	}
	if cfg.CacheTTL <= 0 || cfg.CacheTTL > time.Hour || cfg.CacheMaxItems < 1 || cfg.CacheMaxItems > 2048 {
		return nil, fmt.Errorf("%w: registry cache limits", change.ErrInvalidArgument)
	}
	hosts := stringSet(cfg.AllowedHosts)
	if _, ok := hosts[strings.ToLower(base.Host)]; !ok {
		return nil, fmt.Errorf("%w: registry base host is not allowlisted", change.ErrNotAllowed)
	}
	repositories := stringSet(cfg.AllowedRepositories)
	for repository := range repositories {
		if !repositoryPattern.MatchString(repository) {
			return nil, fmt.Errorf("%w: invalid registry repository allowlist", change.ErrInvalidArgument)
		}
	}
	if len(repositories) == 0 {
		return nil, fmt.Errorf("%w: registry repository allowlist required", change.ErrInvalidArgument)
	}
	authModeCount := 0
	if strings.TrimSpace(cfg.BearerTokenFile) != "" {
		authModeCount++
	}
	if strings.TrimSpace(cfg.UsernameFile) != "" || strings.TrimSpace(cfg.PasswordFile) != "" {
		if strings.TrimSpace(cfg.UsernameFile) == "" || strings.TrimSpace(cfg.PasswordFile) == "" {
			return nil, fmt.Errorf("%w: registry basic auth requires username and password files", change.ErrInvalidArgument)
		}
		authModeCount++
	}
	if authModeCount > 1 {
		return nil, fmt.Errorf("%w: registry authentication modes are mutually exclusive", change.ErrInvalidArgument)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepContext
	}
	httpClient := http.DefaultClient
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	clientCopy := *httpClient
	redirectHosts := stringSet(cfg.AllowedRedirectHosts)
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 2 || req.URL.Scheme != "https" {
			return http.ErrUseLastResponse
		}
		if !strings.EqualFold(req.URL.Host, base.Host) {
			if _, ok := redirectHosts[strings.ToLower(req.URL.Host)]; !ok {
				return http.ErrUseLastResponse
			}
			req.Header.Del("Authorization")
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			return http.ErrUseLastResponse
		}
		return nil
	}
	sum := sha256.Sum256([]byte(strings.ToLower(base.Scheme + "://" + base.Host)))
	return &Client{
		baseURL: base, registryID: "registry:" + hex.EncodeToString(sum[:6]), allowedHosts: hosts, repositories: repositories,
		authRealms: stringSet(cfg.AllowedAuthRealms), redirectHosts: redirectHosts, bearerFile: strings.TrimSpace(cfg.BearerTokenFile),
		usernameFile: strings.TrimSpace(cfg.UsernameFile), passwordFile: strings.TrimSpace(cfg.PasswordFile), timeout: cfg.Timeout,
		maxRetries: cfg.MaxRetries, manifestLimit: cfg.ManifestMaxBytes, configLimit: cfg.ConfigMaxBytes,
		cacheTTL: cfg.CacheTTL, cacheMaxItems: cfg.CacheMaxItems, client: &clientCopy, observer: cfg.Observer, now: cfg.Now, sleep: cfg.Sleep,
		cache: map[string]*list.Element{}, lru: list.New(), flights: map[string]*flight{},
	}, nil
}

func (c *Client) ReadMetadata(ctx context.Context, repository, manifestDigest string) (change.RegistryMetadata, error) {
	ctx, span := otel.Tracer("github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread").Start(ctx, "registry.metadata.read")
	defer span.End()
	repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
	manifestDigest = strings.ToLower(strings.TrimSpace(manifestDigest))
	if _, ok := c.repositories[repository]; !ok {
		return change.RegistryMetadata{}, &APIError{Code: ErrorNotAllowed}
	}
	if !repositoryPattern.MatchString(repository) || !digestPattern.MatchString(manifestDigest) {
		return change.RegistryMetadata{}, &APIError{Code: ErrorInvalid}
	}
	key := repository + "\x00" + manifestDigest
	if value, ok := c.cached(key); ok {
		c.observeCache("hit")
		_, cacheSpan := otel.Tracer("github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread").Start(ctx, "registry.cache")
		cacheSpan.SetAttributes(attribute.String("registry.cache.result", "hit"))
		cacheSpan.End()
		return value, nil
	}
	c.observeCache("miss")
	_, cacheSpan := otel.Tracer("github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread").Start(ctx, "registry.cache")
	cacheSpan.SetAttributes(attribute.String("registry.cache.result", "miss"))
	cacheSpan.End()
	c.mu.Lock()
	if existing, ok := c.flights[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return change.RegistryMetadata{}, structuredContextError(ctx.Err())
		case <-existing.done:
			return existing.metadata, existing.err
		}
	}
	current := &flight{done: make(chan struct{})}
	c.flights[key] = current
	c.mu.Unlock()

	current.metadata, current.err = c.readUncached(ctx, repository, manifestDigest)
	if current.err == nil && current.metadata.Valid && !current.metadata.Truncated && !current.metadata.Degraded {
		c.store(key, current.metadata)
	}
	c.mu.Lock()
	delete(c.flights, key)
	close(current.done)
	c.mu.Unlock()
	return current.metadata, current.err
}

func (c *Client) readUncached(ctx context.Context, repository, manifestDigest string) (change.RegistryMetadata, error) {
	result := change.RegistryMetadata{RegistryID: c.registryID, Repository: repository, ManifestDigest: manifestDigest, ReadAt: c.now().UTC(), Integrity: change.RegistryIntegrityUnknown, Redaction: change.RegistryRedaction{AuthMaterialOmitted: true, ResponsesOmitted: true, Policy: "registry_metadata_bounded"}}
	auth, err := c.initialAuthorization()
	if err != nil {
		result.Degraded = true
		return result, err
	}
	manifestURL := c.endpoint("v2", repository, "manifests", manifestDigest)
	manifestCtx, manifestSpan := otel.Tracer("github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread").Start(ctx, "registry.manifest.read")
	manifestBody, manifestHeaders, auth, err := c.read(manifestCtx, "manifest", manifestURL, strings.Join([]string{manifestOCI, manifestDocker}, ", "), c.manifestLimit, repository, auth)
	manifestSpan.SetAttributes(attribute.String("registry.request.result", metricResult(err)))
	manifestSpan.End()
	if err != nil {
		result.Degraded = true
		result.Truncated = errorCode(err) == ErrorSizeLimit
		return result, err
	}
	if digestBytes(manifestBody) != manifestDigest || !headerDigestValid(manifestHeaders.Get("Docker-Content-Digest"), manifestDigest) {
		result.Integrity = change.RegistryIntegrityInvalid
		return result, &APIError{Code: ErrorInvalid}
	}
	var manifest struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
		Config        struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		} `json:"config"`
	}
	if json.Unmarshal(manifestBody, &manifest) != nil || manifest.SchemaVersion != 2 || !allowedManifestType(manifest.MediaType) || !allowedConfigType(manifest.Config.MediaType) || !digestPattern.MatchString(strings.ToLower(manifest.Config.Digest)) || manifest.Config.Size < 2 || manifest.Config.Size > c.configLimit {
		result.Integrity = change.RegistryIntegrityInvalid
		return result, &APIError{Code: ErrorInvalid}
	}
	if contentType := mediaType(manifestHeaders.Get("Content-Type")); contentType != "" && contentType != manifest.MediaType {
		result.Integrity = change.RegistryIntegrityInvalid
		return result, &APIError{Code: ErrorInvalid}
	}
	result.ManifestMediaType = manifest.MediaType
	result.ConfigDigest = strings.ToLower(manifest.Config.Digest)
	result.ConfigMediaType = manifest.Config.MediaType
	configURL := c.endpoint("v2", repository, "blobs", result.ConfigDigest)
	configCtx, configSpan := otel.Tracer("github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread").Start(ctx, "registry.config.read")
	configBody, configHeaders, _, err := c.read(configCtx, "config", configURL, manifest.Config.MediaType, c.configLimit, repository, auth)
	configSpan.SetAttributes(attribute.String("registry.request.result", metricResult(err)))
	configSpan.End()
	if err != nil {
		result.Degraded = true
		result.Truncated = errorCode(err) == ErrorSizeLimit
		return result, err
	}
	if int64(len(configBody)) != manifest.Config.Size || digestBytes(configBody) != result.ConfigDigest || !headerDigestValid(configHeaders.Get("Docker-Content-Digest"), result.ConfigDigest) {
		result.Integrity = change.RegistryIntegrityInvalid
		return result, &APIError{Code: ErrorInvalid}
	}
	if contentType := mediaType(configHeaders.Get("Content-Type")); contentType != "" && !allowedConfigResponseType(contentType, manifest.Config.MediaType) {
		result.Integrity = change.RegistryIntegrityInvalid
		return result, &APIError{Code: ErrorInvalid}
	}
	var config struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if json.Unmarshal(configBody, &config) != nil {
		result.Integrity = change.RegistryIntegrityInvalid
		return result, &APIError{Code: ErrorInvalid}
	}
	result.Revision = strings.TrimSpace(config.Config.Labels["org.opencontainers.image.revision"])
	result.Source = strings.TrimSpace(config.Config.Labels["org.opencontainers.image.source"])
	result.Version = strings.TrimSpace(config.Config.Labels["org.opencontainers.image.version"])
	result.Integrity = change.RegistryIntegrityVerified
	result.Valid = result.Revision != "" && result.Source != "" && result.Version != ""
	normalized, _ := json.Marshal([]any{result.RegistryID, result.Repository, result.ManifestDigest, result.ConfigDigest, result.ManifestMediaType, result.ConfigMediaType, result.Revision, result.Source, result.Version, result.Integrity, result.Valid})
	hash := sha256.Sum256(normalized)
	result.ResultHash = hex.EncodeToString(hash[:])
	return result, nil
}

func (c *Client) read(ctx context.Context, operation, target, accept string, limit int64, repository, authorization string) ([]byte, http.Header, string, error) {
	challenged := false
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		started := c.now()
		requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
		if err != nil {
			cancel()
			return nil, nil, authorization, &APIError{Code: ErrorInvalid}
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "cloudops-copilot-registry-read/1")
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			requestErr := requestCtx.Err()
			c.observeRequest(operation, requestResult(requestCtx, err), c.now().Sub(started))
			cancel()
			if ctx.Err() != nil {
				return nil, nil, authorization, structuredContextError(ctx.Err())
			}
			if errors.Is(requestErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil, authorization, &APIError{Code: ErrorTimeout}
			}
			if errors.Is(requestErr, context.Canceled) || errors.Is(err, context.Canceled) {
				return nil, nil, authorization, &APIError{Code: ErrorCancelled}
			}
			if attempt < c.maxRetries {
				continue
			}
			return nil, nil, authorization, &APIError{Code: ErrorTemporary}
		}
		if resp.StatusCode == http.StatusUnauthorized && !challenged && c.bearerFile == "" {
			cleanupErr := drainAndClose(resp.Body)
			cancel()
			if cleanupErr != nil {
				return nil, nil, authorization, errors.Join(&APIError{Code: ErrorTemporary}, cleanupErr)
			}
			challenged = true
			token, tokenErr := c.exchangeChallenge(ctx, repository, resp.Header.Get("WWW-Authenticate"))
			c.observeRequest(operation, metricResult(tokenErr), c.now().Sub(started))
			if tokenErr != nil {
				return nil, nil, authorization, tokenErr
			}
			authorization = "Bearer " + token
			attempt--
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, readErr := readBounded(resp.Body, limit)
			closeErr := resp.Body.Close()
			cancel()
			if readErr != nil || closeErr != nil {
				if errors.Is(readErr, errResponseTooLarge) {
					c.observeLimit(operation)
				}
				resultErr := errors.Join(readErr, closeErr)
				c.observeRequest(operation, metricResult(resultErr), c.now().Sub(started))
				return nil, nil, authorization, resultErr
			}
			c.observeRequest(operation, "success", c.now().Sub(started))
			return body, resp.Header.Clone(), authorization, nil
		}
		apiErr := classifyStatus(resp.StatusCode, resp.Header.Get("Retry-After"), c.now())
		cleanupErr := drainAndClose(resp.Body)
		cancel()
		c.observeRequest(operation, string(apiErr.Code), c.now().Sub(started))
		if cleanupErr != nil {
			return nil, nil, authorization, errors.Join(apiErr, cleanupErr)
		}
		if attempt < c.maxRetries && (apiErr.Code == ErrorRateLimit || apiErr.Code == ErrorTemporary) {
			delay := apiErr.RetryAfter
			if delay <= 0 {
				delay = time.Duration(attempt+1) * 100 * time.Millisecond
			}
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
			if err := c.sleep(ctx, delay); err != nil {
				return nil, nil, authorization, structuredContextError(err)
			}
			continue
		}
		return nil, nil, authorization, errors.Join(apiErr, cleanupErr)
	}
	return nil, nil, authorization, &APIError{Code: ErrorTemporary}
}

func (c *Client) exchangeChallenge(ctx context.Context, repository, raw string) (tokenResult string, retErr error) {
	ctx, span := otel.Tracer("github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread").Start(ctx, "registry.auth")
	defer func() {
		span.SetAttributes(attribute.String("registry.request.result", metricResult(retErr)))
		span.End()
	}()
	realm, service, scope, err := parseChallenge(raw)
	if err != nil || scope != "repository:"+repository+":pull" {
		return "", &APIError{Code: ErrorAuthentication}
	}
	realmURL, err := url.Parse(realm)
	if err != nil || realmURL.Scheme != "https" || realmURL.Host == "" || realmURL.User != nil || realmURL.RawQuery != "" || realmURL.Fragment != "" {
		return "", &APIError{Code: ErrorNotAllowed}
	}
	if _, ok := c.authRealms[strings.ToLower(realmURL.Host)]; !ok {
		return "", &APIError{Code: ErrorNotAllowed}
	}
	query := realmURL.Query()
	query.Set("service", service)
	query.Set("scope", scope)
	realmURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realmURL.String(), nil)
	if err != nil {
		return "", &APIError{Code: ErrorInvalid}
	}
	if c.usernameFile != "" {
		username, userErr := readSecret(c.usernameFile)
		password, passwordErr := readSecret(c.passwordFile)
		if userErr != nil || passwordErr != nil {
			return "", &APIError{Code: ErrorAuthentication}
		}
		req.SetBasicAuth(username, password)
	}
	started := c.now()
	resp, err := c.client.Do(req)
	if err != nil {
		c.observeRequest("auth", requestResult(ctx, err), c.now().Sub(started))
		return "", &APIError{Code: ErrorTemporary}
	}
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := classifyStatus(resp.StatusCode, resp.Header.Get("Retry-After"), c.now())
		c.observeRequest("auth", string(apiErr.Code), c.now().Sub(started))
		return "", apiErr
	}
	body, err := readBounded(resp.Body, maxTokenBytes)
	if err != nil {
		c.observeLimit("token")
		return "", err
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", &APIError{Code: ErrorAuthentication}
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		token = strings.TrimSpace(payload.AccessToken)
	}
	if token == "" || len(token) > int(maxSecretBytes) {
		return "", &APIError{Code: ErrorAuthentication}
	}
	c.observeRequest("auth", "success", c.now().Sub(started))
	return token, nil
}

func (c *Client) initialAuthorization() (string, error) {
	if c.bearerFile != "" {
		token, err := readSecret(c.bearerFile)
		if err != nil {
			return "", &APIError{Code: ErrorAuthentication}
		}
		return "Bearer " + token, nil
	}
	if c.usernameFile != "" {
		username, userErr := readSecret(c.usernameFile)
		password, passwordErr := readSecret(c.passwordFile)
		if userErr != nil || passwordErr != nil {
			return "", &APIError{Code: ErrorAuthentication}
		}
		req, _ := http.NewRequest(http.MethodGet, "https://registry.invalid", nil)
		req.SetBasicAuth(username, password)
		return req.Header.Get("Authorization"), nil
	}
	return "", nil
}

func (c *Client) endpoint(parts ...string) string {
	value := *c.baseURL
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.Contains(part, "/") && part != parts[1] {
			escaped = append(escaped, url.PathEscape(part))
			continue
		}
		escaped = append(escaped, part)
	}
	value.Path = "/" + strings.Join(escaped, "/")
	return value.String()
}

func (c *Client) cached(key string) (change.RegistryMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.cache[key]
	if !ok {
		return change.RegistryMetadata{}, false
	}
	value := element.Value.(cacheValue)
	if !c.now().Before(value.expiresAt) {
		delete(c.cache, key)
		c.lru.Remove(element)
		return change.RegistryMetadata{}, false
	}
	c.lru.MoveToFront(element)
	return value.metadata, true
}

func (c *Client) store(key string, metadata change.RegistryMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.cache[key]; ok {
		c.lru.Remove(element)
		delete(c.cache, key)
	}
	element := c.lru.PushFront(cacheValue{key: key, metadata: metadata, expiresAt: c.now().Add(c.cacheTTL)})
	c.cache[key] = element
	for c.lru.Len() > c.cacheMaxItems {
		oldest := c.lru.Back()
		value := oldest.Value.(cacheValue)
		delete(c.cache, value.key)
		c.lru.Remove(oldest)
	}
}

func parseChallenge(raw string) (string, string, string, error) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "Bearer ") {
		return "", "", "", errors.New("unsupported registry authentication challenge")
	}
	values := map[string]string{}
	for _, match := range challengePair.FindAllStringSubmatch(raw, -1) {
		key := strings.ToLower(match[1])
		if _, exists := values[key]; exists {
			return "", "", "", errors.New("duplicate registry authentication challenge field")
		}
		values[key] = match[2]
	}
	if values["realm"] == "" || values["service"] == "" || values["scope"] == "" {
		return "", "", "", errors.New("incomplete registry authentication challenge")
	}
	return values["realm"], values["service"], values["scope"], nil
}

var errResponseTooLarge = &APIError{Code: ErrorSizeLimit}

func readBounded(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, &APIError{Code: ErrorTemporary}
	}
	if int64(len(data)) > limit {
		return nil, errResponseTooLarge
	}
	return data, nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func headerDigestValid(header, expected string) bool {
	header = strings.ToLower(strings.TrimSpace(header))
	return header == "" || header == expected
}

func allowedManifestType(value string) bool { return value == manifestOCI || value == manifestDocker }
func allowedConfigType(value string) bool   { return value == configOCI || value == configDocker }
func allowedConfigResponseType(value, expected string) bool {
	return value == expected || value == configGeneric
}

func mediaType(value string) string {
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func classifyStatus(status int, retryAfter string, now time.Time) *APIError {
	result := &APIError{StatusCode: status, RetryAfter: parseRetryAfter(retryAfter, now)}
	switch status {
	case http.StatusUnauthorized:
		result.Code = ErrorAuthentication
	case http.StatusForbidden:
		result.Code = ErrorPermission
	case http.StatusNotFound:
		result.Code = ErrorNotFound
	case http.StatusTooManyRequests:
		result.Code = ErrorRateLimit
	default:
		if status >= 500 {
			result.Code = ErrorTemporary
		} else {
			result.Code = ErrorInvalid
		}
	}
	return result
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func readSecret(path string) (secret string, retErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	data, err := readBounded(file, maxSecretBytes)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("invalid registry credential file")
	}
	return value, nil
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drainAndClose(body io.ReadCloser) error {
	_, copyErr := io.Copy(io.Discard, io.LimitReader(body, 4096))
	return errors.Join(copyErr, body.Close())
}

func requestResult(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "temporary"
}

func errorCode(err error) ErrorCode {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ErrorTemporary
}

func metricResult(err error) string {
	if err == nil {
		return "success"
	}
	return string(errorCode(err))
}

func structuredContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return &APIError{Code: ErrorCancelled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &APIError{Code: ErrorTimeout}
	}
	return err
}

func (c *Client) observeRequest(operation, result string, elapsed time.Duration) {
	if c.observer != nil {
		c.observer.ObserveRegistryRequest(operation, result, elapsed.Seconds())
	}
}

func (c *Client) observeLimit(kind string) {
	if c.observer != nil {
		c.observer.ObserveRegistryResponseLimit(kind)
	}
}

func (c *Client) observeCache(result string) {
	if c.observer != nil {
		c.observer.ObserveRegistryCache(result)
	}
}
