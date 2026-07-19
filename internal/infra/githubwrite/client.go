package githubwrite

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	revisionPattern   = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)
	sha256Pattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	branchPattern     = regexp.MustCompile(`^cloudops/incident-[a-f0-9-]{36}/remediation-[a-f0-9-]{36}$`)
)

type Observer interface {
	ObserveGitHubWrite(operation, result string, seconds float64)
}

type Config struct {
	BaseURL               string
	TokenProvider         TokenProvider
	AllowedRepositories   []string
	AllowedBaseBranches   []string
	AllowedPaths          []string
	Timeout               time.Duration
	MaxResponseBytes      int64
	MaxContentBytes       int
	HTTPClient            *http.Client
	Observer              Observer
	AllowInsecureForTests bool
}

type Client struct {
	baseURL          *url.URL
	token            TokenProvider
	repositories     []string
	baseBranches     []string
	paths            []string
	client           *http.Client
	maxResponseBytes int64
	maxContentBytes  int
	observer         Observer
}

var _ remediation.GitHubWriter = (*Client)(nil)
var _ remediation.PhasedGitHubWriter = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && (!cfg.AllowInsecureForTests || base.Scheme != "http")) || cfg.TokenProvider == nil || len(cfg.AllowedRepositories) == 0 || len(cfg.AllowedBaseBranches) == 0 || len(cfg.AllowedPaths) == 0 {
		return nil, fmt.Errorf("%w: GitHub write configuration", remediation.ErrInvalidArgument)
	}
	for _, repo := range cfg.AllowedRepositories {
		if !repositoryPattern.MatchString(repo) {
			return nil, remediation.ErrInvalidArgument
		}
	}
	for _, allowedPath := range cfg.AllowedPaths {
		if invalidPath(allowedPath) {
			return nil, remediation.ErrInvalidArgument
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 1024 * 1024
	}
	if cfg.MaxContentBytes <= 0 {
		cfg.MaxContentBytes = 128 * 1024
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{baseURL: base, token: cfg.TokenProvider, repositories: append([]string(nil), cfg.AllowedRepositories...), baseBranches: append([]string(nil), cfg.AllowedBaseBranches...), paths: append([]string(nil), cfg.AllowedPaths...), client: client, maxResponseBytes: cfg.MaxResponseBytes, maxContentBytes: cfg.MaxContentBytes, observer: cfg.Observer}, nil
}

func (c *Client) ReadBaseFile(ctx context.Context, repository, revision, filePath string) ([]byte, error) {
	if err := c.authorize(repository, revision, "", filePath); err != nil {
		return nil, err
	}
	var result struct {
		Type, Encoding, Content string
		Size                    int
	}
	endpoint := c.repoEndpoint(repository, "/contents/"+escapePath(filePath)) + "?ref=" + url.QueryEscape(revision)
	if err := c.requestJSON(ctx, http.MethodGet, endpoint, nil, &result, http.StatusOK, "read_base"); err != nil {
		return nil, err
	}
	if result.Type != "file" || result.Encoding != "base64" || result.Size < 0 || result.Size > c.maxContentBytes {
		return nil, remediation.ErrForbidden
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(result.Content, "\n", ""))
	if err != nil || len(content) > c.maxContentBytes {
		return nil, remediation.ErrForbidden
	}
	return content, nil
}

func (c *Client) DeliverDraftPR(ctx context.Context, request remediation.DeliveryRequest) (remediation.DeliveryResult, error) {
	if err := c.authorize(request.Repository, request.BaseRevision, request.BaseBranch, request.Path); err != nil || !branchPattern.MatchString(request.Branch) || len(request.Content) == 0 || len(request.Content) > c.maxContentBytes || !strings.HasPrefix(request.Marker, "<!-- cloudops-remediation:") || !strings.HasSuffix(request.Marker, " -->") || !strings.Contains(request.PRBody, request.Marker) {
		return remediation.DeliveryResult{}, remediation.ErrForbidden
	}
	if err := c.ensureBaseRevision(ctx, request.Repository, request.BaseBranch, request.BaseRevision); err != nil {
		return remediation.DeliveryResult{}, err
	}
	if existing, found, err := c.findMarkerPR(ctx, request); err != nil {
		return remediation.DeliveryResult{}, err
	} else if found {
		if err := c.verifyExistingCommit(ctx, request, existing.CommitSHA); err != nil {
			return remediation.DeliveryResult{}, err
		}
		return existing, nil
	}
	commitSHA, branchExists, err := c.findBranchCommit(ctx, request.Repository, request.Branch)
	if err != nil {
		return remediation.DeliveryResult{}, err
	}
	if !branchExists {
		commitSHA, err = c.createSingleCommitBranch(ctx, request)
		if err != nil {
			return remediation.DeliveryResult{}, err
		}
	} else if err := c.verifyExistingCommit(ctx, request, commitSHA); err != nil {
		return remediation.DeliveryResult{}, err
	}
	return c.createDraftPR(ctx, request, commitSHA)
}

// ReconcileDraftPR performs only bounded reads and returns the next write
// phase. It is safe to call after an ambiguous GitHub response.
func (c *Client) ReconcileDraftPR(ctx context.Context, request remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	if err := c.validatePhasedRequest(request); err != nil {
		return remediation.WriteObservation{}, err
	}
	if err := c.ensureBaseRevision(ctx, request.Repository, request.BaseBranch, request.BaseRevision); err != nil {
		return remediation.WriteObservation{}, err
	}
	if existing, found, err := c.findMarkerPR(ctx, request.DeliveryRequest); err != nil {
		return remediation.WriteObservation{}, err
	} else if found {
		treeSHA, verifyErr := c.verifyPhasedCommit(ctx, request, existing.CommitSHA)
		if verifyErr != nil {
			return remediation.WriteObservation{}, verifyErr
		}
		return remediation.WriteObservation{Phase: remediation.WritePhaseComplete, BaseSHA: request.BaseRevision, BranchSHA: existing.CommitSHA, CommitSHA: existing.CommitSHA, TreeSHA: treeSHA, PRNumber: existing.PRNumber, PRURL: existing.PRURL, Reconciled: true}, nil
	}
	branchSHA, found, err := c.findBranchCommit(ctx, request.Repository, request.Branch)
	if err != nil {
		return remediation.WriteObservation{}, err
	}
	if !found {
		return remediation.WriteObservation{Phase: remediation.WritePhaseEnsureBranch, BaseSHA: request.BaseRevision, Reconciled: true}, nil
	}
	if strings.EqualFold(branchSHA, request.BaseRevision) {
		return remediation.WriteObservation{Phase: remediation.WritePhaseEnsureCommit, BaseSHA: request.BaseRevision, BranchSHA: branchSHA, Reconciled: true}, nil
	}
	treeSHA, err := c.verifyPhasedCommit(ctx, request, branchSHA)
	if err != nil {
		return remediation.WriteObservation{}, err
	}
	return remediation.WriteObservation{Phase: remediation.WritePhaseEnsureDraftPR, BaseSHA: request.BaseRevision, BranchSHA: branchSHA, CommitSHA: branchSHA, TreeSHA: treeSHA, Reconciled: true}, nil
}

func (c *Client) EnsureBranch(ctx context.Context, request remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	observation, err := c.ReconcileDraftPR(ctx, request)
	if err != nil || observation.Phase != remediation.WritePhaseEnsureBranch {
		return observation, err
	}
	if err := c.requestJSON(ctx, http.MethodPost, c.repoEndpoint(request.Repository, "/git/refs"), map[string]any{"ref": "refs/heads/" + request.Branch, "sha": request.BaseRevision}, nil, http.StatusCreated, "ensure_branch"); err != nil {
		return remediation.WriteObservation{}, err
	}
	return remediation.WriteObservation{Phase: remediation.WritePhaseEnsureCommit, BaseSHA: request.BaseRevision, BranchSHA: request.BaseRevision}, nil
}

func (c *Client) EnsureCommit(ctx context.Context, request remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	observation, err := c.ReconcileDraftPR(ctx, request)
	if err != nil || observation.Phase != remediation.WritePhaseEnsureCommit {
		return observation, err
	}
	baseFile, err := c.readFile(ctx, request.Repository, request.BaseRevision, request.Path)
	if err != nil {
		return remediation.WriteObservation{}, err
	}
	if !strings.EqualFold(baseFile.SHA, request.BaseBlobSHA) || remediation.HashBytes(baseFile.Content) != request.ExpectedBeforeHash {
		return remediation.WriteObservation{}, remediation.ErrDrift
	}
	var result struct {
		Commit struct {
			SHA  string `json:"sha"`
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	body := map[string]any{
		"message": request.CommitTitle,
		"content": base64.StdEncoding.EncodeToString(request.Content),
		"sha":     request.BaseBlobSHA,
		"branch":  request.Branch,
	}
	endpoint := c.repoEndpoint(request.Repository, "/contents/"+escapePath(request.Path))
	if err := c.requestJSON(ctx, http.MethodPut, endpoint, body, &result, http.StatusOK, "ensure_commit"); err != nil {
		return remediation.WriteObservation{}, err
	}
	if !revisionPattern.MatchString(result.Commit.SHA) || !strings.EqualFold(result.Commit.Tree.SHA, request.ExpectedTreeHash) {
		return remediation.WriteObservation{}, remediation.ErrDrift
	}
	return remediation.WriteObservation{Phase: remediation.WritePhaseEnsureDraftPR, BaseSHA: request.BaseRevision, BranchSHA: result.Commit.SHA, CommitSHA: result.Commit.SHA, TreeSHA: strings.ToLower(result.Commit.Tree.SHA)}, nil
}

func (c *Client) EnsureDraftPR(ctx context.Context, request remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	observation, err := c.ReconcileDraftPR(ctx, request)
	if err != nil || observation.Phase != remediation.WritePhaseEnsureDraftPR {
		return observation, err
	}
	result, err := c.createDraftPR(ctx, request.DeliveryRequest, observation.CommitSHA)
	if err != nil {
		return remediation.WriteObservation{}, err
	}
	return remediation.WriteObservation{Phase: remediation.WritePhaseComplete, BaseSHA: request.BaseRevision, BranchSHA: result.CommitSHA, CommitSHA: result.CommitSHA, TreeSHA: observation.TreeSHA, PRNumber: result.PRNumber, PRURL: result.PRURL}, nil
}

func (c *Client) validatePhasedRequest(request remediation.PhasedDeliveryRequest) error {
	if err := c.authorize(request.Repository, request.BaseRevision, request.BaseBranch, request.Path); err != nil ||
		!branchPattern.MatchString(request.Branch) || len(request.Content) == 0 || len(request.Content) > c.maxContentBytes ||
		!revisionPattern.MatchString(request.BaseBlobSHA) || !revisionPattern.MatchString(request.ExpectedTreeHash) ||
		!lowerSHA256(request.ExpectedBeforeHash) || !lowerSHA256(request.ExpectedPostImageHash) || !lowerSHA256(request.LogicalOperationKey) ||
		remediation.HashBytes(request.Content) != request.ExpectedPostImageHash ||
		!strings.HasPrefix(request.Marker, "<!-- cloudops-remediation:") || !strings.HasSuffix(request.Marker, " -->") || !strings.Contains(request.PRBody, request.Marker) {
		return remediation.ErrForbidden
	}
	return nil
}

type fileAtRevision struct {
	SHA     string
	Content []byte
}

func (c *Client) readFile(ctx context.Context, repository, revision, filePath string) (fileAtRevision, error) {
	if err := c.authorize(repository, revision, "", filePath); err != nil {
		return fileAtRevision{}, err
	}
	var result struct {
		Type, Encoding, Content, SHA string
		Size                         int
	}
	endpoint := c.repoEndpoint(repository, "/contents/"+escapePath(filePath)) + "?ref=" + url.QueryEscape(revision)
	if err := c.requestJSON(ctx, http.MethodGet, endpoint, nil, &result, http.StatusOK, "read_file"); err != nil {
		return fileAtRevision{}, err
	}
	if result.Type != "file" || result.Encoding != "base64" || result.Size < 0 || result.Size > c.maxContentBytes || !revisionPattern.MatchString(result.SHA) {
		return fileAtRevision{}, remediation.ErrForbidden
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(result.Content, "\n", ""))
	if err != nil || len(content) > c.maxContentBytes {
		return fileAtRevision{}, remediation.ErrForbidden
	}
	return fileAtRevision{SHA: strings.ToLower(result.SHA), Content: content}, nil
}

func (c *Client) verifyPhasedCommit(ctx context.Context, request remediation.PhasedDeliveryRequest, commitSHA string) (string, error) {
	var commit struct {
		Message string `json:"message"`
		Tree    struct {
			SHA string `json:"sha"`
		} `json:"tree"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, c.repoEndpoint(request.Repository, "/git/commits/"+url.PathEscape(commitSHA)), nil, &commit, http.StatusOK, "verify_phased_commit"); err != nil {
		return "", err
	}
	if commit.Message != request.CommitTitle || len(commit.Parents) != 1 || !strings.EqualFold(commit.Parents[0].SHA, request.BaseRevision) || !strings.EqualFold(commit.Tree.SHA, request.ExpectedTreeHash) {
		return "", remediation.ErrConflict
	}
	content, err := c.ReadBaseFile(ctx, request.Repository, commitSHA, request.Path)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(content, request.Content) || remediation.HashBytes(content) != request.ExpectedPostImageHash {
		return "", remediation.ErrDrift
	}
	return strings.ToLower(commit.Tree.SHA), nil
}

func (c *Client) ensureBaseRevision(ctx context.Context, repository, branch, approvedRevision string) error {
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, c.repoEndpoint(repository, "/git/ref/heads/"+escapePath(branch)), nil, &result, http.StatusOK, "verify_base_revision"); err != nil {
		return err
	}
	if !strings.EqualFold(result.Object.SHA, approvedRevision) {
		return remediation.ErrDrift
	}
	return nil
}

func (c *Client) verifyExistingCommit(ctx context.Context, request remediation.DeliveryRequest, commitSHA string) error {
	var commit struct {
		Message string `json:"message"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, c.repoEndpoint(request.Repository, "/git/commits/"+url.PathEscape(commitSHA)), nil, &commit, http.StatusOK, "verify_commit"); err != nil {
		return err
	}
	if commit.Message != request.CommitTitle || len(commit.Parents) != 1 || commit.Parents[0].SHA != request.BaseRevision {
		return remediation.ErrConflict
	}
	content, err := c.ReadBaseFile(ctx, request.Repository, commitSHA, request.Path)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, request.Content) {
		return remediation.ErrDrift
	}
	return nil
}

func (c *Client) ReadCI(ctx context.Context, repository, commitSHA string) (remediation.CIStatus, error) {
	if err := c.authorize(repository, commitSHA, "", c.paths[0]); err != nil {
		return "", err
	}
	var result struct {
		State string `json:"state"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, c.repoEndpoint(repository, "/commits/"+url.PathEscape(commitSHA)+"/status"), nil, &result, http.StatusOK, "read_ci"); err != nil {
		return "", err
	}
	switch result.State {
	case "success":
		return remediation.CIPassing, nil
	case "failure", "error":
		return remediation.CIFailing, nil
	case "pending", "expected", "":
		return remediation.CIPending, nil
	default:
		return remediation.CICancelled, nil
	}
}

func (c *Client) findMarkerPR(ctx context.Context, request remediation.DeliveryRequest) (remediation.DeliveryResult, bool, error) {
	owner := strings.SplitN(request.Repository, "/", 2)[0]
	endpoint := c.repoEndpoint(request.Repository, "/pulls") + "?state=open&head=" + url.QueryEscape(owner+":"+request.Branch)
	var prs []struct {
		Number  int64  `json:"number"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Draft   bool   `json:"draft"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, endpoint, nil, &prs, http.StatusOK, "find_pr"); err != nil {
		return remediation.DeliveryResult{}, false, err
	}
	for _, pr := range prs {
		if pr.Draft && strings.Contains(pr.Body, request.Marker) {
			return remediation.DeliveryResult{CommitSHA: pr.Head.SHA, PRNumber: pr.Number, PRURL: pr.HTMLURL}, true, nil
		}
	}
	if len(prs) != 0 {
		return remediation.DeliveryResult{}, false, remediation.ErrConflict
	}
	return remediation.DeliveryResult{}, false, nil
}

func (c *Client) findBranchCommit(ctx context.Context, repository, branch string) (string, bool, error) {
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := c.requestJSON(ctx, http.MethodGet, c.repoEndpoint(repository, "/git/ref/heads/"+escapePath(branch)), nil, &result, http.StatusOK, "find_branch")
	if errorsIsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !revisionPattern.MatchString(result.Object.SHA) {
		return "", false, remediation.ErrConflict
	}
	return result.Object.SHA, true, nil
}

func (c *Client) createSingleCommitBranch(ctx context.Context, request remediation.DeliveryRequest) (string, error) {
	var baseCommit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, c.repoEndpoint(request.Repository, "/git/commits/"+url.PathEscape(request.BaseRevision)), nil, &baseCommit, http.StatusOK, "read_base_commit"); err != nil {
		return "", err
	}
	var blob struct {
		SHA string `json:"sha"`
	}
	if err := c.requestJSON(ctx, http.MethodPost, c.repoEndpoint(request.Repository, "/git/blobs"), map[string]any{"content": base64.StdEncoding.EncodeToString(request.Content), "encoding": "base64"}, &blob, http.StatusCreated, "create_blob"); err != nil {
		return "", err
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	treeBody := map[string]any{"base_tree": baseCommit.Tree.SHA, "tree": []map[string]any{{"path": request.Path, "mode": "100644", "type": "blob", "sha": blob.SHA}}}
	if err := c.requestJSON(ctx, http.MethodPost, c.repoEndpoint(request.Repository, "/git/trees"), treeBody, &tree, http.StatusCreated, "create_tree"); err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := c.requestJSON(ctx, http.MethodPost, c.repoEndpoint(request.Repository, "/git/commits"), map[string]any{"message": request.CommitTitle, "tree": tree.SHA, "parents": []string{request.BaseRevision}}, &commit, http.StatusCreated, "create_commit"); err != nil {
		return "", err
	}
	if !revisionPattern.MatchString(commit.SHA) {
		return "", remediation.ErrConflict
	}
	if err := c.requestJSON(ctx, http.MethodPost, c.repoEndpoint(request.Repository, "/git/refs"), map[string]any{"ref": "refs/heads/" + request.Branch, "sha": commit.SHA}, nil, http.StatusCreated, "create_branch"); err != nil {
		return "", err
	}
	return commit.SHA, nil
}

func (c *Client) createDraftPR(ctx context.Context, request remediation.DeliveryRequest, commitSHA string) (remediation.DeliveryResult, error) {
	var pr struct {
		Number  int64  `json:"number"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
	}
	body := map[string]any{"title": request.PRTitle, "head": request.Branch, "base": request.BaseBranch, "body": request.PRBody, "draft": true}
	if err := c.requestJSON(ctx, http.MethodPost, c.repoEndpoint(request.Repository, "/pulls"), body, &pr, http.StatusCreated, "create_draft_pr"); err != nil {
		return remediation.DeliveryResult{}, err
	}
	if !pr.Draft || pr.Number <= 0 {
		return remediation.DeliveryResult{}, remediation.ErrConflict
	}
	return remediation.DeliveryResult{CommitSHA: commitSHA, PRNumber: pr.Number, PRURL: pr.HTMLURL}, nil
}

func (c *Client) authorize(repository, revision, baseBranch, filePath string) error {
	if !slices.Contains(c.repositories, repository) || !repositoryPattern.MatchString(repository) || !revisionPattern.MatchString(revision) || (baseBranch != "" && !slices.Contains(c.baseBranches, baseBranch)) || !slices.Contains(c.paths, path.Clean(filePath)) || invalidPath(filePath) {
		return remediation.ErrForbidden
	}
	return nil
}

func invalidPath(value string) bool {
	clean := path.Clean(strings.TrimSpace(value))
	lower := strings.ToLower(clean)
	return clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.HasPrefix(lower, ".github/") || strings.Contains(lower, "/.github/") || strings.Contains(lower, "workflow") || strings.Contains(lower, "secret") || strings.Contains(lower, "clusterrole") || strings.Contains(lower, "rolebinding") || strings.Contains(lower, "serviceaccount")
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func (c *Client) repoEndpoint(repository, suffix string) string {
	base := *c.baseURL
	base.Path = strings.TrimRight(base.Path, "/") + "/repos/" + repository + suffix
	return base.String()
}

func (c *Client) requestJSON(ctx context.Context, method, endpoint string, body, target any, want int, operation string) (err error) {
	started := time.Now()
	result := "error"
	defer func() {
		if err == nil {
			result = "success"
		}
		if c.observer != nil {
			c.observer.ObserveGitHubWrite(operation, result, time.Since(started).Seconds())
		}
	}()
	var reader io.Reader
	if body != nil {
		payload, marshalErr := json.Marshal(body)
		if marshalErr != nil || len(payload) > c.maxContentBytes*2 {
			return remediation.ErrInvalidArgument
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return remediation.ErrInvalidArgument
	}
	token, err := c.token.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "cloudops-copilot-gitops-writer")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("github write request unavailable: %w", remediation.ErrConflict)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusNotFound {
			return remediation.ErrNotFound
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return remediation.ErrForbidden
		}
		if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnprocessableEntity {
			return remediation.ErrConflict
		}
		return fmt.Errorf("github write request status %s: %w", strconv.Itoa(resp.StatusCode), remediation.ErrConflict)
	}
	if target == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, c.maxResponseBytes))
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, c.maxResponseBytes))
	if err := decoder.Decode(target); err != nil {
		return remediation.ErrConflict
	}
	return nil
}

func errorsIsNotFound(err error) bool { return err == remediation.ErrNotFound }

func lowerSHA256(value string) bool { return sha256Pattern.MatchString(value) }
