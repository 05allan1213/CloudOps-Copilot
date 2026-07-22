package registryread

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
)

const testRepository = "acme/server-web"

type fixtureDocument struct {
	manifest       []byte
	manifestDigest string
	config         []byte
	configDigest   string
}

func fixtureDocumentFor(revision string) fixtureDocument {
	source := "https://github.com/acme/server-web"
	config := []byte(fmt.Sprintf(`{"config":{"Labels":{"org.opencontainers.image.revision":%q,"org.opencontainers.image.source":%q,"org.opencontainers.image.version":%q}}}`, revision, source, revision))
	configDigest := testDigest(config)
	manifest := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":%q,"digest":%q,"size":%d},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:%s","size":99}]}`, manifestOCI, configOCI, configDigest, len(config), strings.Repeat("f", 64)))
	return fixtureDocument{manifest: manifest, manifestDigest: testDigest(manifest), config: config, configDigest: configDigest}
}

func testDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestAllowedConfigResponseType(t *testing.T) {
	for name, contentType := range map[string]string{
		"declared OCI config": configOCI,
		"GHCR blob redirect":  configGeneric,
	} {
		t.Run(name, func(t *testing.T) {
			if !allowedConfigResponseType(contentType, configOCI) {
				t.Fatalf("content type %q rejected", contentType)
			}
		})
	}
	if allowedConfigResponseType("text/plain", configOCI) {
		t.Fatal("unrelated config content type accepted")
	}
}

func newTestClient(t *testing.T, server *httptest.Server, mutate func(*Config)) *Client {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{BaseURL: server.URL, AllowedHosts: []string{parsed.Host}, AllowedRepositories: []string{testRepository}, AllowedAuthRealms: []string{parsed.Host}, Timeout: time.Second, MaxRetries: 1, ManifestMaxBytes: 64 * 1024, ConfigMaxBytes: 32 * 1024, CacheTTL: time.Minute, CacheMaxItems: 2, HTTPClient: server.Client()}
	if mutate != nil {
		mutate(&cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestReadMetadataTLSIntegrityLabelsCacheAndGETOnly(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		switch r.URL.Path {
		case "/v2/" + testRepository + "/manifests/" + doc.manifestDigest:
			w.Header().Set("Content-Type", manifestOCI)
			w.Header().Set("Docker-Content-Digest", doc.manifestDigest)
			w.Header().Set("Link", "</v2/_catalog>; rel=next")
			_, _ = w.Write(doc.manifest)
		case "/v2/" + testRepository + "/blobs/" + doc.configDigest:
			w.Header().Set("Content-Type", configOCI)
			w.Header().Set("Docker-Content-Digest", doc.configDigest)
			_, _ = w.Write(doc.config)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server, nil)
	first, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || first.ResultHash == "" || first != second || first.Integrity != change.RegistryIntegrityVerified || !first.Valid || first.Revision != strings.Repeat("a", 40) || first.Source != "https://github.com/acme/server-web" || first.Version != first.Revision || first.ManifestDigest != doc.manifestDigest || first.ConfigDigest != doc.configDigest || !first.Redaction.AuthMaterialOmitted || !first.Redaction.ResponsesOmitted {
		t.Fatalf("unexpected bounded metadata/cache result: requests=%d metadata=%+v", requests.Load(), first)
	}
}

func TestNewAndReadRejectUnsafeInputs(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	base := Config{BaseURL: server.URL, AllowedHosts: []string{parsed.Host}, AllowedRepositories: []string{testRepository}, Timeout: time.Second, ManifestMaxBytes: 4096, ConfigMaxBytes: 4096, CacheTTL: time.Minute, CacheMaxItems: 1, HTTPClient: server.Client()}
	for name, mutate := range map[string]func(*Config){
		"http base":        func(v *Config) { v.BaseURL = "http://registry.example"; v.AllowedHosts = []string{"registry.example"} },
		"foreign host":     func(v *Config) { v.AllowedHosts = []string{"other.example"} },
		"empty repository": func(v *Config) { v.AllowedRepositories = nil },
		"mixed auth":       func(v *Config) { v.BearerTokenFile, v.UsernameFile, v.PasswordFile = "/a", "/b", "/c" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
	client, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][2]string{
		"repository": {"foreign/repository", doc.manifestDigest},
		"digest":     {testRepository, "latest"},
	} {
		t.Run(name, func(t *testing.T) {
			_, readErr := client.ReadMetadata(context.Background(), input[0], input[1])
			if readErr == nil {
				t.Fatal("unsafe read accepted")
			}
		})
	}
}

func TestBearerChallengeScopeRealmAndCredentialRedaction(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	const token = "top-secret-registry-token"
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			if r.URL.Query().Get("scope") != "repository:"+testRepository+":pull" || r.URL.Query().Get("service") != "fixture" {
				t.Errorf("unexpected auth query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"token":"` + token + `"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm=%q,service="fixture",scope=%q`, server.URL+"/auth", "repository:"+testRepository+":pull"))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("provider text must not appear in errors"))
			return
		}
		switch r.URL.Path {
		case "/v2/" + testRepository + "/manifests/" + doc.manifestDigest:
			w.Header().Set("Content-Type", manifestOCI)
			_, _ = w.Write(doc.manifest)
		case "/v2/" + testRepository + "/blobs/" + doc.configDigest:
			w.Header().Set("Content-Type", configOCI)
			_, _ = w.Write(doc.config)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server, nil)
	if _, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest); err != nil {
		t.Fatal(err)
	}

	foreign := newTestClient(t, server, func(cfg *Config) { cfg.AllowedAuthRealms = []string{"auth.example.invalid"} })
	_, err := foreign.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
	if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "provider text") || !errors.Is(err, change.ErrNotAllowed) {
		t.Fatalf("foreign realm/redaction boundary failed: %v", err)
	}

	var scoped *httptest.Server
	scoped = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm=%q,service="fixture",scope=%q`, scoped.URL+"/auth", "repository:"+testRepository+":push"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer scoped.Close()
	pushClient := newTestClient(t, scoped, nil)
	_, err = pushClient.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != ErrorAuthentication {
		t.Fatalf("write-capable challenge scope accepted: %v", err)
	}
}

func TestDirectBearerAndBasicCredentialsAreFileOnly(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	dir := t.TempDir()
	writeSecret := func(name, value string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, mutate := range map[string]func(*Config){
		"bearer": func(cfg *Config) { cfg.BearerTokenFile = writeSecret("bearer-token", "fixture-bearer") },
		"basic": func(cfg *Config) {
			cfg.UsernameFile = writeSecret("username", "fixture-user")
			cfg.PasswordFile = writeSecret("password", "fixture-password")
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authorized := r.Header.Get("Authorization") == "Bearer fixture-bearer"
				if username, password, ok := r.BasicAuth(); ok && username == "fixture-user" && password == "fixture-password" {
					authorized = true
				}
				if !authorized {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if strings.Contains(r.URL.Path, "/manifests/") {
					w.Header().Set("Content-Type", manifestOCI)
					_, _ = w.Write(doc.manifest)
					return
				}
				w.Header().Set("Content-Type", configOCI)
				_, _ = w.Write(doc.config)
			}))
			defer server.Close()
			client := newTestClient(t, server, mutate)
			if _, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest); err != nil {
				t.Fatalf("file credential mode failed: %v", err)
			}
		})
	}
}

func TestRedirectsAreHTTPSGETAndBounded(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	for name, tc := range map[string]struct {
		count int
		err   bool
	}{"two redirects": {2, false}, "three redirects": {3, true}} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("redirect changed method to %s", r.Method)
				}
				if strings.Contains(r.URL.Path, "/blobs/") {
					w.Header().Set("Content-Type", configOCI)
					_, _ = w.Write(doc.config)
					return
				}
				if r.URL.Path == "/v2/"+testRepository+"/manifests/"+doc.manifestDigest {
					http.Redirect(w, r, "/redirect/1", http.StatusTemporaryRedirect)
					return
				}
				var step int
				_, _ = fmt.Sscanf(r.URL.Path, "/redirect/%d", &step)
				if step < tc.count {
					http.Redirect(w, r, fmt.Sprintf("/redirect/%d", step+1), http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", manifestOCI)
				_, _ = w.Write(doc.manifest)
			}))
			defer server.Close()
			client := newTestClient(t, server, nil)
			_, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
			if (err != nil) != tc.err {
				t.Fatalf("redirect boundary err=%v", err)
			}
		})
	}
}

func TestStatusClassificationRetryAfterAndNoErrorCaching(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	for status, code := range map[int]ErrorCode{401: ErrorAuthentication, 403: ErrorPermission, 404: ErrorNotFound, 429: ErrorRateLimit, 500: ErrorTemporary} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count.Add(1); w.WriteHeader(status) }))
			defer server.Close()
			client := newTestClient(t, server, func(cfg *Config) { cfg.MaxRetries = 0 })
			for range 2 {
				_, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != code {
					t.Fatalf("unexpected status classification: %v", err)
				}
			}
			if count.Load() != 2 {
				t.Fatalf("error was cached: requests=%d", count.Load())
			}
		})
	}

	var count atomic.Int32
	var waited time.Duration
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Content-Type", manifestOCI)
			_, _ = w.Write(doc.manifest)
			return
		}
		w.Header().Set("Content-Type", configOCI)
		_, _ = w.Write(doc.config)
	}))
	defer server.Close()
	client := newTestClient(t, server, func(cfg *Config) {
		cfg.Sleep = func(_ context.Context, delay time.Duration) error { waited = delay; return nil }
	})
	if _, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest); err != nil || waited != 2*time.Second || count.Load() != 3 {
		t.Fatalf("bounded retry failed: err=%v waited=%v requests=%d", err, waited, count.Load())
	}
	if got := parseRetryAfter(time.Now().Add(time.Second).UTC().Format(http.TimeFormat), time.Now()); got <= 0 || got > 2*time.Second {
		t.Fatalf("HTTP-date Retry-After not parsed: %v", got)
	}
}

func TestTimeoutCancellationResponseLimitsAndDigestMismatch(t *testing.T) {
	doc := fixtureDocumentFor(strings.Repeat("a", 40))
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		client := newTestClient(t, server, func(cfg *Config) { cfg.Timeout = 10 * time.Millisecond; cfg.MaxRetries = 0 })
		_, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
		if err == nil {
			t.Fatal("timeout accepted")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		client := newTestClient(t, server, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.ReadMetadata(ctx, testRepository, doc.manifestDigest)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	})
	t.Run("manifest limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, 2049)) }))
		defer server.Close()
		client := newTestClient(t, server, func(cfg *Config) { cfg.ManifestMaxBytes = 1024; cfg.MaxRetries = 0 })
		metadata, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != ErrorSizeLimit || !metadata.Truncated {
			t.Fatalf("size boundary failed: metadata=%+v err=%v", metadata, err)
		}
	})
	t.Run("digest mismatch", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", manifestOCI)
			_, _ = w.Write([]byte(`{"schemaVersion":2}`))
		}))
		defer server.Close()
		client := newTestClient(t, server, nil)
		metadata, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest)
		if err == nil || metadata.Integrity != change.RegistryIntegrityInvalid {
			t.Fatalf("digest mismatch accepted: metadata=%+v err=%v", metadata, err)
		}
	})
}

func TestCacheTTLCapacitySingleFlightAndWaiterCancellation(t *testing.T) {
	docs := []fixtureDocument{fixtureDocumentFor(strings.Repeat("a", 40)), fixtureDocumentFor(strings.Repeat("b", 40)), fixtureDocumentFor(strings.Repeat("c", 40))}
	byPath := map[string][]byte{}
	for _, doc := range docs {
		byPath["/v2/"+testRepository+"/manifests/"+doc.manifestDigest] = doc.manifest
		byPath["/v2/"+testRepository+"/blobs/"+doc.configDigest] = doc.config
	}
	var requests atomic.Int32
	gate := make(chan struct{})
	var gateOnce sync.Once
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.Contains(r.URL.Path, "/manifests/") {
			gateOnce.Do(func() { <-gate })
			w.Header().Set("Content-Type", manifestOCI)
		} else {
			w.Header().Set("Content-Type", configOCI)
		}
		body, ok := byPath[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	client := newTestClient(t, server, func(cfg *Config) {
		cfg.Now = func() time.Time { return now }
		cfg.CacheTTL = time.Second
		cfg.CacheMaxItems = 2
	})
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.ReadMetadata(context.Background(), testRepository, docs[0].manifestDigest)
		firstDone <- err
	}()
	for requests.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	waiterCtx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := client.ReadMetadata(waiterCtx, testRepository, docs[0].manifestDigest)
		waiterDone <- err
	}()
	cancel()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter cancellation lost: %v", err)
	}
	close(gate)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("single flight did not collapse reads: %d", requests.Load())
	}
	for _, doc := range docs[1:] {
		if _, err := client.ReadMetadata(context.Background(), testRepository, doc.manifestDigest); err != nil {
			t.Fatal(err)
		}
	}
	before := requests.Load()
	if _, err := client.ReadMetadata(context.Background(), testRepository, docs[0].manifestDigest); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before+2 {
		t.Fatal("LRU capacity did not evict oldest item")
	}
	now = now.Add(2 * time.Second)
	before = requests.Load()
	if _, err := client.ReadMetadata(context.Background(), testRepository, docs[2].manifestDigest); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before+2 {
		t.Fatal("expired cache item was reused")
	}
}
