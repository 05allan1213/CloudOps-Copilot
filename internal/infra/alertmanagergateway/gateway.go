// Package alertmanagergateway provides the narrow internal HTTP boundary
// between cloudops-api and the Worker-owned Alertmanager silence adapter.
package alertmanagergateway

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

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
)

const (
	basePath      = "/internal/providers/alertmanager"
	createPath    = basePath + "/silences"
	expirePath    = basePath + "/silences/expire"
	maxBodyBytes  = 32 * 1024
	maxReplyBytes = 32 * 1024
)

type gatewayProblem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type expireRequest struct {
	ProviderSilenceID       string `json:"provider_silence_id"`
	ConfigurationRevisionID string `json:"configuration_revision_id"`
}

type createResponse struct {
	ProviderSilenceID string `json:"provider_silence_id"`
}

type Server struct {
	provider alertdomain.SilenceProvider
}

func NewServer(provider alertdomain.SilenceProvider) (*Server, error) {
	if provider == nil {
		return nil, errors.New("alertmanager gateway Provider is required")
	}
	return &Server{provider: provider}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodPost {
		s.writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "typed Alertmanager operations require POST")
		return
	}
	switch request.URL.Path {
	case createPath:
		var input alertdomain.SilenceProviderRequest
		if !s.decode(w, request, &input) {
			return
		}
		providerID, err := s.provider.CreateSilence(request.Context(), input)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, createResponse{ProviderSilenceID: providerID})
	case expirePath:
		var input expireRequest
		if !s.decode(w, request, &input) {
			return
		}
		if err := s.provider.ExpireSilence(request.Context(), input.ProviderSilenceID, input.ConfigurationRevisionID); err != nil {
			s.writeProviderError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "expired"})
	default:
		s.writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "internal Provider route not found")
	}
}

func (s *Server) decode(w http.ResponseWriter, request *http.Request, target any) bool {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		s.writeProblem(w, http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "internal Provider request must use application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxBodyBytes))
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
	case errors.Is(err, alertdomain.ErrProviderDisabled):
		s.writeProblem(w, http.StatusServiceUnavailable, "ALERTMANAGER_PROVIDER_DISABLED", "Alertmanager is disabled in the bound Configuration Revision")
	case errors.Is(err, alertdomain.ErrInvalid):
		s.writeProblem(w, http.StatusUnprocessableEntity, "INVALID_SILENCE", "Alertmanager silence failed Worker-side bounds validation")
	default:
		s.writeProblem(w, http.StatusServiceUnavailable, "ALERTMANAGER_PROVIDER_UNAVAILABLE", "Alertmanager Provider request failed")
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

func (c *Client) CreateSilence(ctx context.Context, request alertdomain.SilenceProviderRequest) (string, error) {
	var response createResponse
	if err := c.do(ctx, createPath, request, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.ProviderSilenceID) == "" || len(response.ProviderSilenceID) > 128 {
		return "", alertdomain.ErrProviderUnavailable
	}
	return response.ProviderSilenceID, nil
}

func (c *Client) ExpireSilence(ctx context.Context, providerID, revisionID string) error {
	return c.do(ctx, expirePath, expireRequest{ProviderSilenceID: providerID, ConfigurationRevisionID: revisionID}, &struct{}{})
}

func (c *Client) do(ctx context.Context, path string, body, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxBodyBytes {
		return alertdomain.ErrInvalid
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create worker Alertmanager request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return alertdomain.ErrProviderUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(response.Body, maxReplyBytes+1))
	if err != nil || len(content) > maxReplyBytes {
		return alertdomain.ErrProviderUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem gatewayProblem
		_ = json.Unmarshal(content, &problem)
		switch problem.Code {
		case "ALERTMANAGER_PROVIDER_DISABLED":
			return alertdomain.ErrProviderDisabled
		case "INVALID_SILENCE":
			return alertdomain.ErrInvalid
		default:
			return alertdomain.ErrProviderUnavailable
		}
	}
	if len(content) == 0 {
		return nil
	}
	if err := json.Unmarshal(content, target); err != nil {
		return alertdomain.ErrProviderUnavailable
	}
	return nil
}

var _ alertdomain.SilenceProvider = (*Client)(nil)
