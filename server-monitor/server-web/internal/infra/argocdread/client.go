package argocdread

import (
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
	"strconv"
	"strings"
	"time"

	"server-web/internal/change"
)

type ErrorCode string

const (
	ErrorAuthentication ErrorCode = "authentication"
	ErrorPermission     ErrorCode = "permission"
	ErrorNotFound       ErrorCode = "not_found"
	ErrorRateLimit      ErrorCode = "rate_limit"
	ErrorTemporary      ErrorCode = "temporary"
	ErrorValidation     ErrorCode = "validation"
)

type APIError struct {
	Code       ErrorCode
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("argocd read failed (%s, status=%d)", e.Code, e.StatusCode)
}

type Config struct {
	Server              string
	TokenFile           string
	AllowedApplications []string
	AllowedProjects     []string
	Timeout             time.Duration
	MaxRetries          int
	MaxResponseBytes    int64
	MaxResources        int
	MaxDiffBytes        int
	AllowHTTPForTests   bool
	HTTPClient          *http.Client
	Sleep               func(context.Context, time.Duration) error
	Observer            Observer
}

type Observer interface {
	ObserveArgoCDRequest(operation, result string, seconds float64)
	ObserveArgoCDDiffTruncation(reason string)
}

type Client struct {
	baseURL      *url.URL
	tokenFile    string
	applications map[string]struct{}
	projects     map[string]struct{}
	client       *http.Client
	maxRetries   int
	maxResponse  int64
	maxResources int
	maxDiffBytes int
	sleep        func(context.Context, time.Duration) error
	observer     Observer
}

var _ change.ArgoCDReader = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	base, err := url.Parse(cfg.Server)
	if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "https" && (!cfg.AllowHTTPForTests || base.Scheme != "http")) {
		return nil, fmt.Errorf("%w: invalid Argo CD server", change.ErrInvalidArgument)
	}
	applications := stringSet(cfg.AllowedApplications)
	if len(applications) == 0 {
		return nil, fmt.Errorf("%w: Argo CD application allowlist required", change.ErrInvalidArgument)
	}
	projects := stringSet(cfg.AllowedProjects)
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 3 {
		return nil, fmt.Errorf("%w: Argo CD retries must be 0-3", change.ErrInvalidArgument)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	client := &Client{baseURL: base, tokenFile: cfg.TokenFile, applications: applications, projects: projects, client: httpClient, maxRetries: cfg.MaxRetries, maxResponse: cfg.MaxResponseBytes, maxResources: cfg.MaxResources, maxDiffBytes: cfg.MaxDiffBytes, sleep: sleep, observer: cfg.Observer}
	if client.maxResponse <= 0 {
		client.maxResponse = 2 * 1024 * 1024
	}
	if client.maxResources <= 0 {
		client.maxResources = 100
	}
	if client.maxResources > 500 {
		return nil, fmt.Errorf("%w: Argo CD resource limit exceeds 500", change.ErrInvalidArgument)
	}
	if client.maxDiffBytes <= 0 {
		client.maxDiffBytes = 128 * 1024
	}
	return client, nil
}

func (c *Client) GetApplication(ctx context.Context, application, project string) (change.ArgoApplication, error) {
	if err := c.authorize(application, project); err != nil {
		return change.ArgoApplication{}, err
	}
	path := "/api/v1/applications/" + url.PathEscape(application)
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	body, err := c.get(ctx, path)
	if err != nil {
		return change.ArgoApplication{}, err
	}
	var payload applicationResponse
	if json.Unmarshal(body, &payload) != nil {
		return change.ArgoApplication{}, &APIError{Code: ErrorValidation}
	}
	resources, truncated, resourceHash, resourceErr := c.GetResourceStatus(ctx, application, project)
	degraded := false
	unknowns := []string{}
	if resourceErr != nil {
		degraded = true
		unknowns = append(unknowns, "resource status unavailable")
	}
	operationMessage, operationRedacted := change.RedactText(payload.Status.OperationState.Message, 2048)
	item := change.ArgoApplication{Name: payload.Metadata.Name, Project: payload.Spec.Project, DestinationServer: payload.Spec.Destination.Server, Namespace: payload.Spec.Destination.Namespace, Repository: payload.Spec.Source.RepoURL, Path: payload.Spec.Source.Path, TargetRevision: payload.Spec.Source.TargetRevision, DeployedRevision: payload.Status.Sync.Revision, SyncStatus: normalizeSync(payload.Status.Sync.Status), HealthStatus: normalizeHealth(payload.Status.Health.Status), OperationPhase: normalizeOperation(payload.Status.OperationState.Phase), OperationMessage: operationMessage, Resources: resources, ExternalURL: strings.TrimRight(c.baseURL.String(), "/") + "/applications/" + url.PathEscape(application), ResultHash: resourceHash, Truncated: truncated, Degraded: degraded, Unknowns: unknowns}
	if operationRedacted {
		item.Degraded = true
		item.Unknowns = append(item.Unknowns, "operation message credential text redacted")
	}
	if !payload.Status.OperationState.FinishedAt.IsZero() {
		value := payload.Status.OperationState.FinishedAt.UTC()
		item.LastSyncedAt = &value
	}
	for index, history := range payload.Status.History {
		if index >= c.maxResources {
			item.Truncated = true
			break
		}
		item.History = append(item.History, change.ArgoHistory{ID: history.ID, Revision: history.Revision, DeployedAt: history.DeployedAt.UTC(), SourceRepo: history.Source.RepoURL, SourcePath: history.Source.Path})
		if item.LastSyncedAt == nil || history.DeployedAt.After(*item.LastSyncedAt) {
			value := history.DeployedAt.UTC()
			item.LastSyncedAt = &value
		}
	}
	if item.ResultHash == "" {
		sum := sha256.Sum256(body)
		item.ResultHash = hex.EncodeToString(sum[:])
	}
	if !strings.EqualFold(item.Name, application) || (project != "" && !strings.EqualFold(item.Project, project)) {
		return change.ArgoApplication{}, fmt.Errorf("%w: Argo CD response scope mismatch", change.ErrNotAllowed)
	}
	return item, nil
}

func (c *Client) GetResourceStatus(ctx context.Context, application, project string) ([]change.ArgoResource, bool, string, error) {
	if err := c.authorize(application, project); err != nil {
		return nil, false, "", err
	}
	path := "/api/v1/applications/" + url.PathEscape(application) + "/resource-tree"
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, false, "", err
	}
	var payload struct {
		Nodes []resourceResponse `json:"nodes"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, false, "", &APIError{Code: ErrorValidation}
	}
	complete, _ := json.Marshal(payload.Nodes)
	sum := sha256.Sum256(complete)
	items := make([]change.ArgoResource, 0, min(len(payload.Nodes), c.maxResources))
	bytesUsed := 0
	truncated := false
	for index, node := range payload.Nodes {
		if index >= c.maxResources {
			truncated = true
			break
		}
		item := change.ArgoResource{Group: change.BoundUTF8(node.Group, 128), Kind: change.BoundUTF8(node.Kind, 128), Namespace: change.BoundUTF8(node.Namespace, 255), Name: change.BoundUTF8(node.Name, 255), Status: normalizeSync(node.Status), Health: normalizeHealth(node.Health.Status), OutOfSync: strings.EqualFold(node.Status, "OutOfSync")}
		if strings.EqualFold(node.Kind, "Secret") {
			item.Redacted = true
		}
		encoded, _ := json.Marshal(item)
		if bytesUsed+len(encoded) > c.maxDiffBytes {
			truncated = true
			break
		}
		bytesUsed += len(encoded)
		items = append(items, item)
	}
	if truncated && c.observer != nil {
		c.observer.ObserveArgoCDDiffTruncation("bound")
	}
	return items, truncated, hex.EncodeToString(sum[:]), nil
}

func (c *Client) authorize(application, project string) error {
	if _, ok := c.applications[strings.ToLower(strings.TrimSpace(application))]; !ok {
		return fmt.Errorf("%w: Argo CD application", change.ErrNotAllowed)
	}
	if len(c.projects) > 0 {
		if _, ok := c.projects[strings.ToLower(strings.TrimSpace(project))]; !ok {
			return fmt.Errorf("%w: Argo CD project", change.ErrNotAllowed)
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, apiPath string) (body []byte, err error) {
	started := time.Now()
	if c.observer != nil {
		operation := "application"
		if strings.Contains(apiPath, "/resource-tree") {
			operation = "resource_tree"
		}
		defer func() {
			c.observer.ObserveArgoCDRequest(operation, argoMetricResult(err), time.Since(started).Seconds())
		}()
	}
	endpoint := *c.baseURL
	parts := strings.SplitN(apiPath, "?", 2)
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + parts[0]
	if len(parts) == 2 {
		endpoint.RawQuery = parts[1]
	}
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, change.ErrInvalidArgument
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "cloudops-copilot-change-intelligence")
		if c.tokenFile != "" {
			token, tokenErr := readToken(c.tokenFile)
			if tokenErr != nil {
				return nil, tokenErr
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < c.maxRetries {
				continue
			}
			return nil, fmt.Errorf("%w: Argo CD transport", change.ErrUnavailable)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxResponse+1))
			_ = resp.Body.Close()
			if readErr != nil || int64(len(body)) > c.maxResponse {
				return nil, &APIError{Code: ErrorValidation, StatusCode: resp.StatusCode}
			}
			return body, nil
		}
		apiErr := classifyStatus(resp.StatusCode, resp.Header.Get("Retry-After"))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if attempt < c.maxRetries && (apiErr.Code == ErrorRateLimit || apiErr.Code == ErrorTemporary) {
			delay := apiErr.RetryAfter
			if delay <= 0 {
				delay = time.Duration(attempt+1) * 100 * time.Millisecond
			}
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}
		return nil, apiErr
	}
	return nil, change.ErrUnavailable
}

func argoMetricResult(err error) string {
	if err == nil {
		return "success"
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return string(apiErr.Code)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "error"
}

func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("argocd token file unavailable: %w", change.ErrUnavailable)
	}
	token := strings.TrimSpace(string(data))
	if token == "" || len(token) > 4096 {
		return "", fmt.Errorf("argocd token file invalid: %w", change.ErrInvalidArgument)
	}
	return token, nil
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
func classifyStatus(status int, retry string) *APIError {
	result := &APIError{StatusCode: status}
	switch status {
	case 401:
		result.Code = ErrorAuthentication
	case 403:
		result.Code = ErrorPermission
	case 404:
		result.Code = ErrorNotFound
	case 429:
		result.Code = ErrorRateLimit
	default:
		if status >= 500 {
			result.Code = ErrorTemporary
		} else {
			result.Code = ErrorValidation
		}
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(retry)); err == nil && seconds >= 0 {
		result.RetryAfter = time.Duration(seconds) * time.Second
	}
	return result
}
func normalizeSync(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "synced":
		return "Synced"
	case "outofsync", "out_of_sync":
		return "OutOfSync"
	case "missing":
		return "Missing"
	default:
		return "Unknown"
	}
}
func normalizeHealth(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy":
		return "Healthy"
	case "progressing":
		return "Progressing"
	case "degraded":
		return "Degraded"
	case "missing":
		return "Missing"
	default:
		return "Unknown"
	}
}
func normalizeOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "running":
		return "Running"
	case "succeeded":
		return "Succeeded"
	case "failed":
		return "Failed"
	case "error":
		return "Error"
	case "terminating":
		return "Terminating"
	default:
		return "Unknown"
	}
}

type applicationResponse struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Project     string `json:"project"`
		Destination struct {
			Server    string `json:"server"`
			Namespace string `json:"namespace"`
		} `json:"destination"`
		Source struct {
			RepoURL        string `json:"repoURL"`
			Path           string `json:"path"`
			TargetRevision string `json:"targetRevision"`
		} `json:"source"`
	} `json:"spec"`
	Status struct {
		Sync struct {
			Status   string `json:"status"`
			Revision string `json:"revision"`
		} `json:"sync"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		OperationState struct {
			Phase      string    `json:"phase"`
			Message    string    `json:"message"`
			FinishedAt time.Time `json:"finishedAt"`
		} `json:"operationState"`
		History []struct {
			ID         int64     `json:"id"`
			Revision   string    `json:"revision"`
			DeployedAt time.Time `json:"deployedAt"`
			Source     struct {
				RepoURL string `json:"repoURL"`
				Path    string `json:"path"`
			} `json:"source"`
		} `json:"history"`
	} `json:"status"`
}
type resourceResponse struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Health    struct {
		Status string `json:"status"`
	} `json:"health"`
}
