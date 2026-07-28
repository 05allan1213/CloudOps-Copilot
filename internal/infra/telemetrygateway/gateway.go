// Package telemetrygateway provides the narrow cluster-internal HTTP boundary
// between cloudops-api and Worker-owned Elasticsearch and Tempo adapters.
package telemetrygateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

const (
	BasePath        = "/internal/providers/telemetry"
	catalogPath     = BasePath + "/catalog"
	logsPath        = BasePath + "/logs/query"
	traceSearchPath = BasePath + "/traces/search"
	traceDetailPath = BasePath + "/traces/detail"
	maxBodyBytes    = 128 * 1024
	maxReplyBytes   = telemetry.MaximumResponseBytes + 512*1024
)

type problem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Server struct{ provider telemetry.Provider }

func NewServer(provider telemetry.Provider) (*Server, error) {
	if provider == nil {
		return nil, errors.New("telemetry gateway Provider is required")
	}
	return &Server{provider: provider}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		s.writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "typed telemetry Provider operations require POST")
		return
	}
	var value any
	var err error
	switch r.URL.Path {
	case catalogPath:
		var request telemetry.ProviderCatalogRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err = s.provider.Catalog(r.Context(), request)
	case logsPath:
		var request telemetry.ProviderLogRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err = s.provider.QueryLogs(r.Context(), request)
	case traceSearchPath:
		var request telemetry.ProviderTraceSearchRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err = s.provider.SearchTraces(r.Context(), request)
	case traceDetailPath:
		var request telemetry.ProviderTraceDetailRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err = s.provider.Trace(r.Context(), request)
	default:
		s.writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "internal telemetry Provider route not found")
		return
	}
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, value)
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
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
	case errors.Is(err, telemetry.ErrProviderDisabled):
		s.writeProblem(w, http.StatusServiceUnavailable, "TELEMETRY_PROVIDER_DISABLED", "telemetry Provider is disabled in the bound Configuration Revision")
	case errors.Is(err, telemetry.ErrBoundExceeded):
		s.writeProblem(w, http.StatusUnprocessableEntity, "QUERY_BOUND_EXCEEDED", "telemetry result exceeded the bounded query contract")
	case errors.Is(err, telemetry.ErrInvalid):
		s.writeProblem(w, http.StatusUnprocessableEntity, "INVALID_BOUNDED_QUERY", "telemetry request failed Worker-side contract validation")
	case errors.Is(err, telemetry.ErrNotFound):
		s.writeProblem(w, http.StatusNotFound, "TELEMETRY_NOT_FOUND", "telemetry resource was not found in the bounded scope")
	default:
		s.writeProblem(w, http.StatusServiceUnavailable, "TELEMETRY_PROVIDER_UNAVAILABLE", "telemetry Provider request failed")
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeProblem(w http.ResponseWriter, status int, code, detail string) {
	s.writeJSON(w, status, problem{Code: code, Detail: detail})
}

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

func NewClient(rawBaseURL string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBaseURL), "/"))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("worker telemetry Provider Gateway target is not a fixed HTTP endpoint")
	}
	if timeout <= 0 {
		timeout = 32 * time.Second
	}
	return &Client{baseURL: parsed, http: &http.Client{
		Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("worker telemetry Provider Gateway redirects are disabled")
		},
	}}, nil
}

func (c *Client) Catalog(ctx context.Context, request telemetry.ProviderCatalogRequest) (telemetry.ProviderCatalog, error) {
	var result telemetry.ProviderCatalog
	return result, c.do(ctx, catalogPath, request, &result)
}

func (c *Client) QueryLogs(ctx context.Context, request telemetry.ProviderLogRequest) (telemetry.ProviderLogResult, error) {
	var result telemetry.ProviderLogResult
	if err := c.do(ctx, logsPath, request, &result); err != nil {
		return telemetry.ProviderLogResult{}, err
	}
	if result.Entries == nil {
		result.Entries = []telemetry.LogEntry{}
	}
	if result.Histogram == nil {
		result.Histogram = []telemetry.HistogramBucket{}
	}
	if result.Fields == nil {
		result.Fields = []string{}
	}
	return result, nil
}

func (c *Client) SearchTraces(ctx context.Context, request telemetry.ProviderTraceSearchRequest) (telemetry.ProviderTraceSearchResult, error) {
	var result telemetry.ProviderTraceSearchResult
	if err := c.do(ctx, traceSearchPath, request, &result); err != nil {
		return telemetry.ProviderTraceSearchResult{}, err
	}
	if result.Traces == nil {
		result.Traces = []telemetry.TraceSummary{}
	}
	return result, nil
}

func (c *Client) Trace(ctx context.Context, request telemetry.ProviderTraceDetailRequest) (telemetry.ProviderTraceDetailResult, error) {
	var result telemetry.ProviderTraceDetailResult
	if err := c.do(ctx, traceDetailPath, request, &result); err != nil {
		return telemetry.ProviderTraceDetailResult{}, err
	}
	if result.Detail.Spans == nil {
		result.Detail.Spans = []telemetry.Span{}
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, path string, body, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxBodyBytes {
		return telemetry.ErrBoundExceeded
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return telemetry.ErrInvalid
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return telemetry.ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(response.Body, maxReplyBytes+1))
	if err != nil {
		return telemetry.ErrUnavailable
	}
	if len(content) > maxReplyBytes {
		return telemetry.ErrBoundExceeded
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value problem
		_ = json.Unmarshal(content, &value)
		switch value.Code {
		case "TELEMETRY_PROVIDER_DISABLED":
			return telemetry.ErrProviderDisabled
		case "QUERY_BOUND_EXCEEDED":
			return telemetry.ErrBoundExceeded
		case "INVALID_BOUNDED_QUERY":
			return telemetry.ErrInvalid
		case "TELEMETRY_NOT_FOUND":
			return telemetry.ErrNotFound
		default:
			return telemetry.ErrUnavailable
		}
	}
	if err := json.Unmarshal(content, target); err != nil {
		return telemetry.ErrUnavailable
	}
	return nil
}

var _ telemetry.Provider = (*Client)(nil)
