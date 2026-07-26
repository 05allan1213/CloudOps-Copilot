// Package infrastructuregateway provides the narrow cluster-internal HTTP
// boundary between cloudops-api and the Worker-owned Kubernetes client.
package infrastructuregateway

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

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
)

const (
	basePath      = "/internal/providers/kubernetes"
	probePath     = basePath + "/probe"
	topologyPath  = basePath + "/topology"
	eventsPath    = basePath + "/events"
	maxBodyBytes  = 64 * 1024
	maxReplyBytes = 4 * 1024 * 1024
)

type Server struct {
	reader infrastructure.Reader
}

type gatewayProblem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type eventsRequest struct {
	ClusterID string                  `json:"cluster_id"`
	Resource  infrastructure.Resource `json:"resource"`
	Limit     int                     `json:"limit"`
}

type eventsResponse struct {
	Items     []infrastructure.Event `json:"items"`
	Truncated bool                   `json:"truncated"`
}

func NewServer(reader infrastructure.Reader) (*Server, error) {
	if reader == nil {
		return nil, errors.New("infrastructure gateway reader is required")
	}
	return &Server{reader: reader}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case probePath:
		if r.Method != http.MethodGet {
			s.writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "typed Provider probe requires GET")
			return
		}
		clusterID := strings.TrimSpace(r.URL.Query().Get("cluster_id"))
		if clusterID == "" || len(clusterID) > 128 {
			s.writeProblem(w, http.StatusBadRequest, "INVALID_CLUSTER", "typed Provider probe requires cluster_id")
			return
		}
		value, err := s.reader.Probe(r.Context(), clusterID)
		if err != nil {
			s.writeProblem(w, http.StatusServiceUnavailable, "KUBERNETES_PROVIDER_UNAVAILABLE", "Kubernetes API probe failed")
			return
		}
		s.writeJSON(w, http.StatusOK, value)
	case topologyPath:
		if r.Method != http.MethodPost {
			s.writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "typed topology read requires POST")
			return
		}
		var request infrastructure.ReadRequest
		if !s.decode(w, r, &request) {
			return
		}
		value, err := s.reader.Read(r.Context(), request)
		if err != nil {
			s.writeProblem(w, http.StatusServiceUnavailable, "KUBERNETES_TOPOLOGY_UNAVAILABLE", "bounded Kubernetes topology read failed")
			return
		}
		s.writeJSON(w, http.StatusOK, value)
	case eventsPath:
		if r.Method != http.MethodPost {
			s.writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "typed Event read requires POST")
			return
		}
		var request eventsRequest
		if !s.decode(w, r, &request) {
			return
		}
		if strings.TrimSpace(request.ClusterID) == "" || len(strings.TrimSpace(request.ClusterID)) > 128 {
			s.writeProblem(w, http.StatusBadRequest, "INVALID_CLUSTER", "typed Event read requires cluster_id")
			return
		}
		items, truncated, err := s.reader.Events(r.Context(), request.ClusterID, request.Resource, request.Limit)
		if err != nil {
			s.writeProblem(w, http.StatusServiceUnavailable, "KUBERNETES_EVENTS_UNAVAILABLE", "bounded Kubernetes Event read failed")
			return
		}
		if items == nil {
			items = []infrastructure.Event{}
		}
		s.writeJSON(w, http.StatusOK, eventsResponse{Items: items, Truncated: truncated})
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
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("worker Provider Gateway target is not a fixed HTTP endpoint")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{baseURL: parsed, http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("worker Provider Gateway redirects are disabled")
	}}}, nil
}

func (c *Client) Probe(ctx context.Context, clusterID string) (infrastructure.ProviderSource, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" || len(clusterID) > 128 {
		return infrastructure.ProviderSource{}, errors.New("Kubernetes Provider probe cluster identity is invalid")
	}
	var result infrastructure.ProviderSource
	query := url.Values{"cluster_id": []string{clusterID}}
	if err := c.do(ctx, http.MethodGet, probePath, query, nil, &result); err != nil {
		return infrastructure.ProviderSource{}, err
	}
	return result, nil
}

func (c *Client) Read(ctx context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	var result infrastructure.Projection
	if err := c.do(ctx, http.MethodPost, topologyPath, nil, request, &result); err != nil {
		return infrastructure.Projection{}, err
	}
	return result, nil
}

func (c *Client) Events(ctx context.Context, clusterID string, resource infrastructure.Resource, limit int) ([]infrastructure.Event, bool, error) {
	var result eventsResponse
	if err := c.do(ctx, http.MethodPost, eventsPath, nil, eventsRequest{ClusterID: clusterID, Resource: resource, Limit: limit}, &result); err != nil {
		return nil, false, err
	}
	if result.Items == nil {
		result.Items = []infrastructure.Event{}
	}
	return result.Items, result.Truncated, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, target any) error {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode worker Provider request: %w", err)
		}
		if len(encoded) > maxBodyBytes {
			return errors.New("worker Provider request exceeds the bounded body size")
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return fmt.Errorf("create worker Provider request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call worker Provider Gateway: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxReplyBytes+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read worker Provider response: %w", err)
	}
	if len(content) > maxReplyBytes {
		return errors.New("worker Provider response exceeds the bounded size")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem gatewayProblem
		_ = json.Unmarshal(content, &problem)
		if problem.Code == "" {
			problem.Code = "PROVIDER_GATEWAY_FAILED"
		}
		return fmt.Errorf("%s: worker Provider Gateway returned HTTP %d", problem.Code, response.StatusCode)
	}
	if err := json.Unmarshal(content, target); err != nil {
		return fmt.Errorf("decode worker Provider response: %w", err)
	}
	return nil
}

var _ infrastructure.Reader = (*Client)(nil)
