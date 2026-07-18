package githubread

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
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
)

type ErrorCode string

const (
	ErrorAuthentication ErrorCode = "authentication"
	ErrorPermission     ErrorCode = "permission"
	ErrorNotFound       ErrorCode = "not_found"
	ErrorConflict       ErrorCode = "conflict"
	ErrorRateLimit      ErrorCode = "rate_limit"
	ErrorValidation     ErrorCode = "validation"
	ErrorTemporary      ErrorCode = "temporary"
)

type APIError struct {
	Code       ErrorCode
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github read failed (%s, status=%d)", e.Code, e.StatusCode)
}

type Config struct {
	BaseURL              string
	TokenProvider        TokenProvider
	AllowedRepositories  []string
	AllowedBranches      []string
	AllowedPaths         []string
	DeniedPathPatterns   []string
	Timeout              time.Duration
	MaxRetries           int
	MaxPages             int
	MaxResponseBytes     int64
	MaxDiffFiles         int
	MaxPatchFiles        int
	MaxPatchBytesPerFile int
	MaxDiffBytes         int
	APIVersion           string
	AllowHTTPForTests    bool
	HTTPClient           *http.Client
	Sleep                func(context.Context, time.Duration) error
	Observer             Observer
}

type Observer interface {
	ObserveGitHubRequest(operation, result string, seconds float64)
	ObserveGitHubRateLimit(reason string)
	ObserveGitHubDiffTruncation(reason string)
}

type cacheEntry struct {
	etag string
	body []byte
}

type Client struct {
	baseURL              *url.URL
	token                TokenProvider
	allowedRepositories  map[string]struct{}
	allowedBranches      map[string]struct{}
	allowedPaths         []string
	deniedPaths          []string
	client               *http.Client
	maxRetries           int
	maxPages             int
	maxResponseBytes     int64
	maxDiffFiles         int
	maxPatchFiles        int
	maxPatchBytesPerFile int
	maxDiffBytes         int
	apiVersion           string
	sleep                func(context.Context, time.Duration) error
	cacheMu              sync.RWMutex
	cache                map[string]cacheEntry
	observer             Observer
}

var _ change.GitHubReader = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && (!cfg.AllowHTTPForTests || base.Scheme != "http")) || base.User != nil {
		return nil, fmt.Errorf("%w: invalid GitHub API host", change.ErrInvalidArgument)
	}
	allowed := map[string]struct{}{}
	for _, repository := range cfg.AllowedRepositories {
		repository = strings.ToLower(strings.Trim(strings.TrimSpace(repository), "/"))
		if strings.Count(repository, "/") != 1 {
			return nil, fmt.Errorf("%w: invalid repository allowlist", change.ErrInvalidArgument)
		}
		allowed[repository] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%w: repository allowlist is required", change.ErrInvalidArgument)
	}
	branches := map[string]struct{}{}
	for _, branch := range cfg.AllowedBranches {
		branches[strings.TrimSpace(branch)] = struct{}{}
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
	client := &Client{baseURL: base, token: cfg.TokenProvider, allowedRepositories: allowed, allowedBranches: branches, allowedPaths: append([]string(nil), cfg.AllowedPaths...), deniedPaths: append([]string(nil), cfg.DeniedPathPatterns...), client: httpClient, maxRetries: cfg.MaxRetries, maxPages: cfg.MaxPages, maxResponseBytes: cfg.MaxResponseBytes, maxDiffFiles: cfg.MaxDiffFiles, maxPatchFiles: cfg.MaxPatchFiles, maxPatchBytesPerFile: cfg.MaxPatchBytesPerFile, maxDiffBytes: cfg.MaxDiffBytes, apiVersion: cfg.APIVersion, sleep: sleep, cache: map[string]cacheEntry{}, observer: cfg.Observer}
	if client.maxRetries < 0 || client.maxRetries > 3 {
		return nil, fmt.Errorf("%w: GitHub retries must be 0-3", change.ErrInvalidArgument)
	}
	if client.maxPages <= 0 {
		client.maxPages = 3
	}
	if client.maxPages > 10 {
		return nil, fmt.Errorf("%w: GitHub page limit exceeds 10", change.ErrInvalidArgument)
	}
	if client.maxResponseBytes <= 0 {
		client.maxResponseBytes = 2 * 1024 * 1024
	}
	if client.maxDiffFiles <= 0 {
		client.maxDiffFiles = 100
	}
	if client.maxPatchFiles <= 0 {
		client.maxPatchFiles = 50
	}
	if client.maxPatchBytesPerFile <= 0 {
		client.maxPatchBytesPerFile = 8192
	}
	if client.maxDiffBytes <= 0 {
		client.maxDiffBytes = 128 * 1024
	}
	if client.apiVersion == "" {
		client.apiVersion = "2022-11-28"
	}
	return client, nil
}

func (c *Client) GetCommit(ctx context.Context, repo change.RepositoryRef, ref string) (change.Commit, error) {
	if err := c.authorize(repo); err != nil {
		return change.Commit{}, err
	}
	if err := c.authorizeRef(ref); err != nil {
		return change.Commit{}, err
	}
	var payload commitResponse
	if err := c.getJSON(ctx, c.repoPath(repo, "/commits/"+url.PathEscape(strings.TrimSpace(ref))), &payload); err != nil {
		return change.Commit{}, err
	}
	parents := make([]string, 0, len(payload.Parents))
	for _, parent := range payload.Parents {
		parents = append(parents, parent.SHA)
	}
	message, _ := change.RedactText(payload.Commit.Message, 4096)
	return change.Commit{Repository: repo.FullName(), SHA: payload.SHA, Parents: parents, Message: message, AuthorAt: payload.Commit.Author.Date.UTC(), CommitterAt: payload.Commit.Committer.Date.UTC(), HTMLURL: payload.HTMLURL}, nil
}

func (c *Client) GetCommitDiff(ctx context.Context, repo change.RepositoryRef, ref string) (change.DiffSummary, error) {
	if err := c.authorize(repo); err != nil {
		return change.DiffSummary{}, err
	}
	if err := c.authorizeRef(ref); err != nil {
		return change.DiffSummary{}, err
	}
	var files []fileResponse
	additions, deletions, externalURL := 0, 0, ""
	for pageNumber := 1; pageNumber <= c.maxPages && len(files) < c.maxDiffFiles+1; pageNumber++ {
		var payload commitResponse
		apiPath := c.repoPath(repo, "/commits/"+url.PathEscape(strings.TrimSpace(ref))) + "?per_page=100&page=" + strconv.Itoa(pageNumber)
		if err := c.getJSON(ctx, apiPath, &payload); err != nil {
			return change.DiffSummary{}, err
		}
		if pageNumber == 1 {
			additions, deletions, externalURL = payload.Stats.Additions, payload.Stats.Deletions, payload.HTMLURL
		}
		files = append(files, payload.Files...)
		if len(payload.Files) < 100 {
			break
		}
	}
	return c.boundDiff(files, additions, deletions, externalURL), nil
}

func (c *Client) GetPullRequest(ctx context.Context, repo change.RepositoryRef, number int64) (change.PullRequest, error) {
	if err := c.authorize(repo); err != nil {
		return change.PullRequest{}, err
	}
	if number <= 0 {
		return change.PullRequest{}, change.ErrInvalidArgument
	}
	var payload pullResponse
	if err := c.getJSON(ctx, c.repoPath(repo, "/pulls/"+strconv.FormatInt(number, 10)), &payload); err != nil {
		return change.PullRequest{}, err
	}
	title, _ := change.RedactText(payload.Title, 1024)
	body, _ := change.RedactText(payload.Body, 4096)
	return change.PullRequest{Repository: repo.FullName(), Number: payload.Number, Title: title, Body: body, State: payload.State, Merged: payload.Merged, MergeCommitSHA: payload.MergeCommitSHA, BaseSHA: payload.Base.SHA, HeadSHA: payload.Head.SHA, MergedAt: payload.MergedAt, HTMLURL: payload.HTMLURL}, nil
}

func (c *Client) ListPullRequestsForCommit(ctx context.Context, repo change.RepositoryRef, sha string) ([]change.PullRequest, error) {
	if err := c.authorize(repo); err != nil {
		return nil, err
	}
	if err := c.authorizeRef(sha); err != nil {
		return nil, err
	}
	var payload []pullResponse
	if err := c.getJSON(ctx, c.repoPath(repo, "/commits/"+url.PathEscape(sha)+"/pulls")+"?per_page=100", &payload); err != nil {
		return nil, err
	}
	result := make([]change.PullRequest, 0, len(payload))
	for _, item := range payload {
		title, _ := change.RedactText(item.Title, 1024)
		body, _ := change.RedactText(item.Body, 4096)
		result = append(result, change.PullRequest{Repository: repo.FullName(), Number: item.Number, Title: title, Body: body, State: item.State, Merged: item.Merged || item.MergedAt != nil, MergeCommitSHA: item.MergeCommitSHA, BaseSHA: item.Base.SHA, HeadSHA: item.Head.SHA, MergedAt: item.MergedAt, HTMLURL: item.HTMLURL})
	}
	return result, nil
}

func (c *Client) GetPullRequestFiles(ctx context.Context, repo change.RepositoryRef, number int64) (change.DiffSummary, error) {
	if err := c.authorize(repo); err != nil {
		return change.DiffSummary{}, err
	}
	if number <= 0 {
		return change.DiffSummary{}, change.ErrInvalidArgument
	}
	var files []fileResponse
	for page := 1; page <= c.maxPages && len(files) < c.maxDiffFiles+1; page++ {
		var current []fileResponse
		path := c.repoPath(repo, "/pulls/"+strconv.FormatInt(number, 10)+"/files") + "?per_page=100&page=" + strconv.Itoa(page)
		if err := c.getJSON(ctx, path, &current); err != nil {
			return change.DiffSummary{}, err
		}
		files = append(files, current...)
		if len(current) < 100 {
			break
		}
	}
	return c.boundDiff(files, 0, 0, ""), nil
}

func (c *Client) GetCIStatus(ctx context.Context, repo change.RepositoryRef, sha string) (change.CIStatus, error) {
	if err := c.authorize(repo); err != nil {
		return change.CIStatus{}, err
	}
	if err := c.authorizeRef(sha); err != nil {
		return change.CIStatus{}, err
	}
	result := change.CIStatus{CommitSHA: sha, Conclusion: "success"}
	for page := 1; page <= c.maxPages; page++ {
		var checks struct {
			CheckRuns []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"check_runs"`
		}
		path := c.repoPath(repo, "/commits/"+url.PathEscape(sha)+"/check-runs") + "?per_page=100&page=" + strconv.Itoa(page)
		if err := c.getJSON(ctx, path, &checks); err != nil {
			return change.CIStatus{}, err
		}
		for _, item := range checks.CheckRuns {
			result.CheckRuns = append(result.CheckRuns, change.CheckRun{ID: item.ID, Name: change.BoundUTF8(item.Name, 255), Status: item.Status, Conclusion: item.Conclusion, HTMLURL: item.HTMLURL})
			result.Conclusion = combineConclusion(result.Conclusion, item.Status, item.Conclusion)
		}
		if len(checks.CheckRuns) < 100 {
			break
		}
	}
	for page := 1; page <= c.maxPages; page++ {
		var workflows struct {
			WorkflowRuns []workflowResponse `json:"workflow_runs"`
		}
		path := c.repoPath(repo, "/actions/runs") + "?head_sha=" + url.QueryEscape(sha) + "&per_page=100&page=" + strconv.Itoa(page)
		if err := c.getJSON(ctx, path, &workflows); err != nil {
			return change.CIStatus{}, err
		}
		for _, item := range workflows.WorkflowRuns {
			if len(c.allowedBranches) > 0 {
				if _, allowed := c.allowedBranches[item.HeadBranch]; !allowed {
					result.Degraded = true
					continue
				}
			}
			result.WorkflowRuns = append(result.WorkflowRuns, change.WorkflowRun{ID: item.ID, Name: change.BoundUTF8(item.Name, 255), HeadSHA: item.HeadSHA, HeadBranch: item.HeadBranch, Status: item.Status, Conclusion: item.Conclusion, CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(), HTMLURL: item.HTMLURL})
			result.Conclusion = combineConclusion(result.Conclusion, item.Status, item.Conclusion)
		}
		if len(workflows.WorkflowRuns) < 100 {
			break
		}
	}
	if len(result.CheckRuns) == 0 && len(result.WorkflowRuns) == 0 {
		result.Conclusion = "unknown"
	}
	return result, nil
}

func (c *Client) authorize(repo change.RepositoryRef) error {
	full := strings.ToLower(strings.Trim(repo.Owner, "/") + "/" + strings.Trim(repo.Name, "/"))
	if _, ok := c.allowedRepositories[full]; !ok {
		return fmt.Errorf("%w: repository", change.ErrNotAllowed)
	}
	return nil
}

func (c *Client) authorizeRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if change.ValidCommitSHA(ref) {
		return nil
	}
	if _, ok := c.allowedBranches[ref]; ok {
		return nil
	}
	return fmt.Errorf("%w: branch or ref", change.ErrNotAllowed)
}

func (c *Client) repoPath(repo change.RepositoryRef, suffix string) string {
	return "/repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Name) + suffix
}

func (c *Client) getJSON(ctx context.Context, apiPath string, target any) error {
	body, err := c.get(ctx, apiPath)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(target); err != nil {
		return &APIError{Code: ErrorValidation}
	}
	return nil
}

func (c *Client) get(ctx context.Context, apiPath string) (body []byte, err error) {
	started := time.Now()
	if c.observer != nil {
		defer func() {
			c.observer.ObserveGitHubRequest(githubOperation(apiPath), metricResult(err), time.Since(started).Seconds())
		}()
	}
	if !strings.HasPrefix(apiPath, "/") {
		return nil, change.ErrInvalidArgument
	}
	endpoint := *c.baseURL
	basePath := strings.TrimRight(endpoint.Path, "/")
	parts := strings.SplitN(apiPath, "?", 2)
	endpoint.Path = basePath + parts[0]
	if len(parts) == 2 {
		endpoint.RawQuery = parts[1]
	}
	cacheKey := endpoint.String()
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, change.ErrInvalidArgument
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", c.apiVersion)
		req.Header.Set("User-Agent", "cloudops-copilot-change-intelligence")
		if c.token != nil {
			token, tokenErr := c.token.Token(ctx)
			if tokenErr != nil {
				return nil, tokenErr
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		c.cacheMu.RLock()
		cached, hasCached := c.cache[cacheKey]
		c.cacheMu.RUnlock()
		if hasCached && cached.etag != "" {
			req.Header.Set("If-None-Match", cached.etag)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < c.maxRetries {
				continue
			}
			return nil, fmt.Errorf("%w: GitHub transport", change.ErrUnavailable)
		}
		if resp.StatusCode == http.StatusNotModified && hasCached {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return nil, errors.Join(change.ErrUnavailable, closeErr)
			}
			return append([]byte(nil), cached.body...), nil
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
			closeErr := resp.Body.Close()
			if readErr != nil || closeErr != nil || int64(len(body)) > c.maxResponseBytes {
				return nil, errors.Join(&APIError{Code: ErrorValidation, StatusCode: resp.StatusCode}, readErr, closeErr)
			}
			if etag := resp.Header.Get("ETag"); etag != "" {
				c.cacheMu.Lock()
				if len(c.cache) >= 256 {
					c.cache = make(map[string]cacheEntry)
				}
				c.cache[cacheKey] = cacheEntry{etag: etag, body: append([]byte(nil), body...)}
				c.cacheMu.Unlock()
			}
			return body, nil
		}
		apiErr := classifyStatus(resp.StatusCode, resp.Header)
		if apiErr.Code == ErrorRateLimit && c.observer != nil {
			c.observer.ObserveGitHubRateLimit("api")
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if closeErr := resp.Body.Close(); closeErr != nil {
			return nil, errors.Join(apiErr, closeErr)
		}
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

func (c *Client) boundDiff(files []fileResponse, additions, deletions int, externalURL string) change.DiffSummary {
	complete, _ := json.Marshal(files)
	sum := sha256.Sum256(complete)
	result := change.DiffSummary{TotalFiles: len(files), Additions: additions, Deletions: deletions, ResultHash: hex.EncodeToString(sum[:]), ExternalURL: externalURL}
	bytesUsed, patches := 0, 0
	for index, file := range files {
		if index >= c.maxDiffFiles {
			result.Truncated = true
			break
		}
		item := change.FileChange{Filename: change.BoundUTF8(file.Filename, 512), Status: file.Status, Previous: change.BoundUTF8(file.PreviousFilename, 512), Additions: file.Additions, Deletions: file.Deletions, Changes: file.Changes}
		if !c.pathAllowed(file.Filename) {
			item.Redacted = true
			result.Redactions = append(result.Redactions, file.Filename)
			result.Files = append(result.Files, item)
			continue
		}
		if change.SensitivePath(file.Filename, c.deniedPaths) {
			item.Redacted = true
			result.Redactions = append(result.Redactions, file.Filename)
			result.Files = append(result.Files, item)
			continue
		}
		if file.Patch == "" {
			item.Binary = true
			result.Files = append(result.Files, item)
			continue
		}
		if strings.Contains(strings.ToLower(file.Status), "submodule") {
			item.Submodule = true
			result.Files = append(result.Files, item)
			continue
		}
		redactedPatch, containsCredential := change.RedactText(file.Patch, 0)
		if containsCredential {
			item.Redacted = true
			result.Redactions = append(result.Redactions, file.Filename)
			result.Files = append(result.Files, item)
			continue
		}
		if patches >= c.maxPatchFiles {
			item.Truncated = true
			result.Truncated = true
			result.Files = append(result.Files, item)
			continue
		}
		patch := change.BoundUTF8(redactedPatch, c.maxPatchBytesPerFile)
		if len(patch) < len(redactedPatch) {
			item.Truncated, result.Truncated = true, true
		}
		if bytesUsed+len(patch) > c.maxDiffBytes {
			item.Truncated, result.Truncated = true, true
			result.Files = append(result.Files, item)
			continue
		}
		item.Patch = patch
		bytesUsed += len(patch)
		patches++
		result.Files = append(result.Files, item)
	}
	if len(files) > len(result.Files) {
		result.Truncated = true
	}
	if result.Truncated && c.observer != nil {
		c.observer.ObserveGitHubDiffTruncation("bound")
	}
	return result
}

func (c *Client) pathAllowed(filename string) bool {
	if len(c.allowedPaths) == 0 {
		return true
	}
	filename = strings.TrimPrefix(strings.TrimSpace(filename), "./")
	for _, allowed := range c.allowedPaths {
		allowed = strings.TrimPrefix(strings.TrimSpace(allowed), "./")
		if allowed == "" {
			continue
		}
		if matched, _ := path.Match(allowed, filename); matched || filename == strings.TrimSuffix(allowed, "/") || strings.HasPrefix(filename, strings.TrimSuffix(allowed, "/")+"/") {
			return true
		}
	}
	return false
}

func githubOperation(apiPath string) string {
	switch {
	case strings.Contains(apiPath, "/check-runs"):
		return "check_runs"
	case strings.Contains(apiPath, "/actions/runs"):
		return "workflow_runs"
	case strings.Contains(apiPath, "/pulls/") && strings.Contains(apiPath, "/files"):
		return "pull_request_files"
	case strings.Contains(apiPath, "/pulls"):
		return "pull_request"
	default:
		return "commit"
	}
}

func metricResult(err error) string {
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

func classifyStatus(status int, headers http.Header) *APIError {
	result := &APIError{StatusCode: status}
	switch status {
	case 401:
		result.Code = ErrorAuthentication
	case 403:
		if strings.TrimSpace(headers.Get("X-RateLimit-Remaining")) == "0" {
			result.Code = ErrorRateLimit
		} else {
			result.Code = ErrorPermission
		}
	case 404:
		result.Code = ErrorNotFound
	case 409:
		result.Code = ErrorConflict
	case 422:
		result.Code = ErrorValidation
	case 429:
		result.Code = ErrorRateLimit
	default:
		if status >= 500 {
			result.Code = ErrorTemporary
		} else {
			result.Code = ErrorValidation
		}
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(headers.Get("Retry-After"))); err == nil && seconds >= 0 {
		result.RetryAfter = time.Duration(seconds) * time.Second
	}
	return result
}

func combineConclusion(current, status, conclusion string) string {
	if status != "completed" {
		return "in_progress"
	}
	switch conclusion {
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
		return "failure"
	}
	if current == "failure" || current == "in_progress" {
		return current
	}
	if conclusion == "" {
		return "unknown"
	}
	return current
}

type fileResponse struct {
	Filename         string `json:"filename"`
	Status           string `json:"status"`
	PreviousFilename string `json:"previous_filename"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Changes          int    `json:"changes"`
	Patch            string `json:"patch"`
}
type commitResponse struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
	Files []fileResponse `json:"files"`
	Stats struct {
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"stats"`
}
type pullResponse struct {
	Number         int64  `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Base           struct {
		SHA string `json:"sha"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	MergedAt *time.Time `json:"merged_at"`
	HTMLURL  string     `json:"html_url"`
}
type workflowResponse struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	HeadSHA    string    `json:"head_sha"`
	HeadBranch string    `json:"head_branch"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
}
