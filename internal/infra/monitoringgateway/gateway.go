// Package monitoringgateway provides the narrow cluster-internal HTTP boundary
// between cloudops-api and the Worker-owned Prometheus adapter.
package monitoringgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
)

const (
	basePath      = "/internal/providers/prometheus"
	catalogPath   = basePath + "/catalog"
	queryPath     = basePath + "/query-range"
	maxBodyBytes  = 128 * 1024
	maxReplyBytes = observability.MaximumResponseBytes + 512*1024
)

type gatewayProblem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Server struct {
	provider observability.Provider
}

func NewServer(provider observability.Provider) (*Server, error) {
	if provider == nil {
		return nil, errors.New("monitoring gateway Provider is required")
	}
	return &Server{provider: provider}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		s.writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "typed Prometheus Provider operations require POST")
		return
	}
	switch r.URL.Path {
	case catalogPath:
		var request observability.ProviderCatalogRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err := s.provider.Catalog(r.Context(), request)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, value)
	case queryPath:
		var request observability.ProviderQueryRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err := s.provider.Query(r.Context(), request)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, value)
	default:
		s.writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "internal Provider route not found")
	}
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		s.writeProblem(w, http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "internal Provider request must use application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		s.writeProblem(w, http.StatusBadRequest, "INVALID_REQUEST", "internal Provider request is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		s.writeProblem(w, http.StatusBadRequest, "INVALID_REQUEST", "internal Provider request must contain one JSON value")
		return false
	}
	return true
}

func (s *Server) writeProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, observability.ErrProviderDisabled):
		s.writeProblem(w, http.StatusServiceUnavailable, "PROMETHEUS_PROVIDER_DISABLED", "Prometheus is disabled in the bound Configuration Revision")
	case errors.Is(err, observability.ErrBoundExceeded):
		s.writeProblem(w, http.StatusUnprocessableEntity, "QUERY_BOUND_EXCEEDED", "Prometheus result exceeded the bounded query contract")
	case errors.Is(err, observability.ErrInvalid), errors.Is(err, observability.ErrUnauthorized):
		s.writeProblem(w, http.StatusUnprocessableEntity, "INVALID_BOUNDED_QUERY", "Prometheus request failed Worker-side contract validation")
	default:
		s.writeProblem(w, http.StatusServiceUnavailable, "PROMETHEUS_PROVIDER_UNAVAILABLE", "Prometheus Provider request failed")
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeProblem(w http.ResponseWriter, status int, code, detail string) {
	s.writeJSON(w, status, gatewayProblem{Code: code, Detail: detail})
}

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

func NewClient(rawBaseURL string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBaseURL), "/"))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("worker Provider Gateway target is not a fixed HTTP endpoint")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{baseURL: parsed, http: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("worker Provider Gateway redirects are disabled")
		},
	}}, nil
}

func (c *Client) Catalog(ctx context.Context, request observability.ProviderCatalogRequest) (observability.ProviderCatalog, error) {
	var result observability.ProviderCatalog
	if err := c.do(ctx, catalogPath, request, &result); err != nil {
		return observability.ProviderCatalog{}, err
	}
	if result.MetricNames == nil {
		result.MetricNames = []string{}
	}
	return result, nil
}

func (c *Client) Query(ctx context.Context, request observability.ProviderQueryRequest) (observability.ProviderQueryResult, error) {
	var result observability.ProviderQueryResult
	if err := c.do(ctx, queryPath, request, &result); err != nil {
		return observability.ProviderQueryResult{}, err
	}
	if result.Result.Series == nil {
		result.Result.Series = []observability.QuerySeries{}
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, path string, body, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode worker Prometheus request: %w", err)
	}
	if len(encoded) > maxBodyBytes {
		return observability.ErrBoundExceeded
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create worker Prometheus request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return observability.ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(response.Body, maxReplyBytes+1))
	if err != nil {
		return observability.ErrUnavailable
	}
	if len(content) > maxReplyBytes {
		return observability.ErrBoundExceeded
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem gatewayProblem
		_ = json.Unmarshal(content, &problem)
		switch problem.Code {
		case "PROMETHEUS_PROVIDER_DISABLED":
			return observability.ErrProviderDisabled
		case "QUERY_BOUND_EXCEEDED":
			return observability.ErrBoundExceeded
		case "INVALID_BOUNDED_QUERY":
			return observability.ErrUnauthorized
		default:
			return observability.ErrUnavailable
		}
	}
	if err := json.Unmarshal(content, target); err != nil {
		return observability.ErrUnavailable
	}
	return nil
}

var _ observability.Provider = (*Client)(nil)
