package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubread"
)

type legacyPullRequestReader interface {
	GetPullRequest(context.Context, change.RepositoryRef, int64) (change.PullRequest, error)
}

type legacyChangeReconciler struct {
	reader legacyPullRequestReader
}

func newLegacyChangeReconciler(config GitHubReconcileConfig) (cutover.LegacyChangeReconciler, error) {
	if !config.Configured() {
		return nil, nil
	}
	var token githubread.TokenProvider
	var err error
	if strings.TrimSpace(config.TokenFile) != "" {
		token = githubread.FileTokenProvider{Path: config.TokenFile}
	} else {
		repositoryNames := make([]string, 0, len(config.AllowedRepositories))
		for _, repository := range config.AllowedRepositories {
			parts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
			repositoryNames = append(repositoryNames, parts[1])
		}
		token, err = githubread.NewAppTokenProvider(githubread.AppTokenConfig{
			BaseURL: config.BaseURL, AppID: config.AppID, InstallationID: config.InstallationID,
			PrivateKeyFile: config.PrivateKeyFile, APIVersion: "2022-11-28",
			AllowedRepositories: repositoryNames,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize cutover GitHub App read authentication: %w", err)
		}
	}
	client, err := githubread.New(githubread.Config{
		BaseURL: config.BaseURL, TokenProvider: token, AllowedRepositories: config.AllowedRepositories,
		Timeout: config.Timeout, MaxRetries: config.MaxRetries, MaxPages: 1,
		MaxResponseBytes: 512 * 1024, APIVersion: "2022-11-28",
	})
	if err != nil {
		return nil, fmt.Errorf("initialize cutover GitHub read client: %w", err)
	}
	return legacyChangeReconciler{reader: client}, nil
}

func (r legacyChangeReconciler) ReconcilePullRequest(ctx context.Context, artifact cutover.LegacyExternalArtifact) (cutover.ReconciledPullRequest, error) {
	if r.reader == nil {
		return cutover.ReconciledPullRequest{}, errors.New("cutover GitHub reader is unavailable")
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(artifact.Repository), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || artifact.PullRequest <= 0 {
		return cutover.ReconciledPullRequest{}, errors.New("legacy pull request repository or number is invalid")
	}
	pullRequest, err := r.reader.GetPullRequest(ctx, change.RepositoryRef{Owner: parts[0], Name: parts[1]}, artifact.PullRequest)
	if err != nil {
		return cutover.ReconciledPullRequest{}, err
	}
	state := strings.ToLower(strings.TrimSpace(pullRequest.State))
	if pullRequest.Merged {
		state = "merged"
	}
	return cutover.ReconciledPullRequest{
		Repository: pullRequest.Repository, PullRequest: pullRequest.Number, URL: pullRequest.HTMLURL,
		BaseRevision: strings.ToLower(strings.TrimSpace(pullRequest.BaseSHA)),
		HeadBranch:   pullRequest.HeadBranch, HeadRevision: strings.ToLower(strings.TrimSpace(pullRequest.HeadSHA)),
		State: state, Merged: pullRequest.Merged,
		MergedCommitSHA: strings.ToLower(strings.TrimSpace(pullRequest.MergeCommitSHA)),
	}, nil
}
