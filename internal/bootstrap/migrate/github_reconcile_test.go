package migrate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
)

type legacyPullRequestReaderFunc func(context.Context, change.RepositoryRef, int64) (change.PullRequest, error)

func (f legacyPullRequestReaderFunc) GetPullRequest(ctx context.Context, repository change.RepositoryRef, number int64) (change.PullRequest, error) {
	return f(ctx, repository, number)
}

func TestLegacyChangeReconcilerReturnsOnlyExactReadIdentity(t *testing.T) {
	base, head, merged := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	reconciler := legacyChangeReconciler{reader: legacyPullRequestReaderFunc(func(_ context.Context, repository change.RepositoryRef, number int64) (change.PullRequest, error) {
		if repository.FullName() != "acme/app" || number != 7 {
			t.Fatalf("unexpected read identity %s#%d", repository.FullName(), number)
		}
		return change.PullRequest{Repository: repository.FullName(), Number: number, State: "closed", Merged: true,
			MergeCommitSHA: merged, BaseSHA: base, HeadSHA: head, HeadBranch: "feature/fix",
			HTMLURL: "https://github.com/acme/app/pull/7"}, nil
	})}
	got, err := reconciler.ReconcilePullRequest(context.Background(), cutover.LegacyExternalArtifact{Repository: "acme/app", PullRequest: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository != "acme/app" || got.PullRequest != 7 || got.URL != "https://github.com/acme/app/pull/7" ||
		got.BaseRevision != base || got.HeadRevision != head || got.HeadBranch != "feature/fix" ||
		got.State != "merged" || !got.Merged || got.MergedCommitSHA != merged {
		t.Fatalf("reconciled identity drift: %+v", got)
	}
}

func TestMigrateConfigRequiresIsolatedBoundedCutoverGitHubReadAuth(t *testing.T) {
	base := Config{MySQL: database.MySQLConfig{Host: "mysql", Port: "3306", User: "cloudops", Database: "cloudops", PingTimeout: time.Second},
		LockTimeout: time.Second, CommandTimeout: time.Second}
	if err := base.Validate(); err != nil {
		t.Fatalf("optional GitHub config rejected: %v", err)
	}
	invalid := base
	invalid.GitHub = GitHubReconcileConfig{BaseURL: "https://api.github.com", AppID: 1,
		AllowedRepositories: []string{"acme/app"}, Timeout: time.Second, MaxRetries: 1}
	if err := invalid.Validate(); err == nil {
		t.Fatal("partial GitHub App authentication was accepted")
	}
	valid := base
	valid.GitHub = GitHubReconcileConfig{BaseURL: "https://api.github.com", TokenFile: "/run/secrets/github-read-token",
		AllowedRepositories: []string{"acme/app"}, Timeout: time.Second, MaxRetries: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("bounded token-file authentication rejected: %v", err)
	}
	valid.GitHub.AppID, valid.GitHub.InstallationID, valid.GitHub.PrivateKeyFile = 1, 2, "/run/secrets/key.pem"
	if err := valid.Validate(); err == nil {
		t.Fatal("simultaneous token-file and App authentication was accepted")
	}
}
