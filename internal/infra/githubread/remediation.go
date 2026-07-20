package githubread

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

var _ remediation.ExactGitReader = (*Client)(nil)

// ReadRestoreFacts performs a fixed set of GET-only GitHub reads. Repository,
// branch and path are rechecked against the client's startup allowlists; the
// async task and model never provide any of these values.
func (c *Client) ReadRestoreFacts(ctx context.Context, query remediation.ExactGitRestoreQuery) (remediation.ExactGitRestoreFacts, error) {
	repo, err := exactRepositoryRef(query.Repository)
	if err != nil {
		return remediation.ExactGitRestoreFacts{}, err
	}
	query.BaseBranch = strings.TrimSpace(query.BaseBranch)
	query.TargetPath = strings.TrimPrefix(strings.TrimSpace(query.TargetPath), "./")
	query.BaselineRevision = strings.ToLower(strings.TrimSpace(query.BaselineRevision))
	if err := c.authorize(repo); err != nil || c.authorizeRef(query.BaseBranch) != nil || c.authorizeRef(query.BaselineRevision) != nil ||
		len(c.allowedPaths) == 0 || !c.pathAllowed(query.TargetPath) || change.SensitivePath(query.TargetPath, c.deniedPaths) ||
		path.Clean(query.TargetPath) != query.TargetPath || strings.HasPrefix(query.TargetPath, "../") {
		return remediation.ExactGitRestoreFacts{}, fmt.Errorf("%w: exact remediation Git query is outside the fixed allowlist", change.ErrNotAllowed)
	}

	var base exactCommitResponse
	if err := c.getJSON(ctx, c.repoPath(repo, "/commits/"+url.PathEscape(query.BaseBranch)), &base); err != nil {
		return remediation.ExactGitRestoreFacts{}, mapExactGitReadError(err)
	}
	base.SHA = strings.ToLower(strings.TrimSpace(base.SHA))
	base.Commit.Tree.SHA = strings.ToLower(strings.TrimSpace(base.Commit.Tree.SHA))
	if !change.ValidCommitSHA(base.SHA) || !change.ValidCommitSHA(base.Commit.Tree.SHA) {
		return remediation.ExactGitRestoreFacts{}, fmt.Errorf("%w: base ref did not resolve to exact commit/tree facts", change.ErrConflict)
	}

	current, err := c.readExactContent(ctx, repo, query.TargetPath, base.SHA)
	if err != nil {
		return remediation.ExactGitRestoreFacts{}, err
	}
	baseline, err := c.readExactContent(ctx, repo, query.TargetPath, query.BaselineRevision)
	if err != nil {
		return remediation.ExactGitRestoreFacts{}, err
	}

	var tree exactTreeResponse
	if err := c.getJSON(ctx, c.repoPath(repo, "/git/trees/"+url.PathEscape(base.Commit.Tree.SHA))+"?recursive=1", &tree); err != nil {
		return remediation.ExactGitRestoreFacts{}, mapExactGitReadError(err)
	}
	if len(tree.Tree) == 0 || len(tree.Tree) > remediation.MaxExactGitTreeEntries {
		return remediation.ExactGitRestoreFacts{}, fmt.Errorf("%w: recursive Git tree exceeds its fixed bound", change.ErrConflict)
	}
	entries := make([]remediation.GitTreeEntry, 0, len(tree.Tree))
	for _, entry := range tree.Tree {
		entries = append(entries, remediation.GitTreeEntry{
			Path: entry.Path, Mode: entry.Mode, Type: entry.Type, ObjectID: strings.ToLower(strings.TrimSpace(entry.SHA)),
		})
	}

	var comparison exactCompareResponse
	comparePath := "/compare/" + url.PathEscape(query.BaselineRevision+"..."+base.SHA)
	if err := c.getJSON(ctx, c.repoPath(repo, comparePath), &comparison); err != nil {
		return remediation.ExactGitRestoreFacts{}, mapExactGitReadError(err)
	}
	comparison.Status = strings.ToLower(strings.TrimSpace(comparison.Status))
	comparison.BaseCommit.SHA = strings.ToLower(strings.TrimSpace(comparison.BaseCommit.SHA))
	comparison.MergeBaseCommit.SHA = strings.ToLower(strings.TrimSpace(comparison.MergeBaseCommit.SHA))
	if comparison.BaseCommit.SHA != query.BaselineRevision || comparison.MergeBaseCommit.SHA != query.BaselineRevision {
		return remediation.ExactGitRestoreFacts{}, fmt.Errorf("%w: Git comparison is not bound to the baseline commit", change.ErrConflict)
	}
	ancestor := comparison.Status == "ahead" || comparison.Status == "identical"
	if comparison.Status == "ahead" && (comparison.AheadBy <= 0 || comparison.BehindBy != 0) ||
		comparison.Status == "identical" && (comparison.AheadBy != 0 || comparison.BehindBy != 0) {
		return remediation.ExactGitRestoreFacts{}, fmt.Errorf("%w: Git comparison counters are inconsistent", change.ErrConflict)
	}

	facts := remediation.ExactGitRestoreFacts{
		Repository: query.Repository, BaseBranch: query.BaseBranch, TargetPath: query.TargetPath,
		BaseRevision: base.SHA, BaseTreeSHA: strings.ToLower(strings.TrimSpace(tree.SHA)),
		BaseBlobSHA: current.SHA, FileMode: current.Mode, CurrentContent: current.Content,
		BaselineRevision: query.BaselineRevision, BaselineBlobSHA: baseline.SHA, BaselineContent: baseline.Content,
		BaselineIsAncestor: ancestor, BaseTreeWasTruncated: tree.Truncated, TreeEntries: entries,
	}
	if facts.BaseTreeSHA != base.Commit.Tree.SHA {
		return remediation.ExactGitRestoreFacts{}, fmt.Errorf("%w: commit and recursive tree identities differ", change.ErrConflict)
	}
	if err := remediation.ValidateExactGitRestoreFacts(facts); err != nil {
		return remediation.ExactGitRestoreFacts{}, err
	}
	return facts, nil
}

type exactContent struct {
	SHA     string
	Mode    string
	Content []byte
}

func (c *Client) readExactContent(ctx context.Context, repo change.RepositoryRef, targetPath, revision string) (exactContent, error) {
	var payload exactContentResponse
	apiPath := c.repoPath(repo, "/contents/"+escapeExactGitPath(targetPath)) + "?ref=" + url.QueryEscape(revision)
	if err := c.getJSON(ctx, apiPath, &payload); err != nil {
		return exactContent{}, mapExactGitReadError(err)
	}
	if payload.Type != "file" || payload.Path != targetPath || payload.Encoding != "base64" || payload.Size <= 0 || payload.Size > remediation.MaxV3PostImageBytes {
		return exactContent{}, fmt.Errorf("%w: exact Git content response is outside bounds", change.ErrConflict)
	}
	encoded := strings.NewReplacer("\n", "", "\r", "", " ", "", "\t", "").Replace(payload.Content)
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(content) != payload.Size || len(content) == 0 || len(content) > remediation.MaxV3PostImageBytes {
		return exactContent{}, fmt.Errorf("%w: exact Git blob content is malformed", change.ErrConflict)
	}
	sha := strings.ToLower(strings.TrimSpace(payload.SHA))
	if !change.ValidCommitSHA(sha) {
		return exactContent{}, fmt.Errorf("%w: exact Git blob SHA is malformed", change.ErrConflict)
	}
	return exactContent{SHA: sha, Mode: "100644", Content: content}, nil
}

func exactRepositoryRef(repository string) (change.RepositoryRef, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return change.RepositoryRef{}, fmt.Errorf("%w: exact Git repository is invalid", change.ErrInvalidArgument)
	}
	return change.RepositoryRef{Owner: parts[0], Name: parts[1]}, nil
}

func escapeExactGitPath(value string) string {
	parts := strings.Split(value, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func mapExactGitReadError(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case ErrorAuthentication, ErrorPermission:
		return fmt.Errorf("%w: GitHub exact-read identity is not authorized", change.ErrPermission)
	case ErrorNotFound, ErrorConflict, ErrorValidation:
		return fmt.Errorf("%w: exact Git object no longer exists or is inconsistent", change.ErrConflict)
	case ErrorRateLimit, ErrorTemporary:
		return fmt.Errorf("%w: exact Git facts are temporarily unavailable", change.ErrUnavailable)
	default:
		return err
	}
}

type exactCommitResponse struct {
	SHA    string `json:"sha"`
	Commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	} `json:"commit"`
}

type exactContentResponse struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int    `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

type exactTreeResponse struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

type exactCompareResponse struct {
	Status     string `json:"status"`
	AheadBy    int    `json:"ahead_by"`
	BehindBy   int    `json:"behind_by"`
	BaseCommit struct {
		SHA string `json:"sha"`
	} `json:"base_commit"`
	MergeBaseCommit struct {
		SHA string `json:"sha"`
	} `json:"merge_base_commit"`
}
