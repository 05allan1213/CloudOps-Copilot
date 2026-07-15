package deliveryread

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server-web/internal/change"
)

type githubReaderFake struct {
	lastRepo   change.RepositoryRef
	lastNumber int64
	lastCommit string
	pr         change.PullRequest
	ci         change.CIStatus
}

func (f *githubReaderFake) GetCommit(context.Context, change.RepositoryRef, string) (change.Commit, error) {
	return change.Commit{}, nil
}
func (f *githubReaderFake) GetCommitDiff(context.Context, change.RepositoryRef, string) (change.DiffSummary, error) {
	return change.DiffSummary{}, nil
}
func (f *githubReaderFake) ListPullRequestsForCommit(context.Context, change.RepositoryRef, string) ([]change.PullRequest, error) {
	return nil, nil
}
func (f *githubReaderFake) GetPullRequest(_ context.Context, repo change.RepositoryRef, number int64) (change.PullRequest, error) {
	f.lastRepo, f.lastNumber = repo, number
	return f.pr, nil
}
func (f *githubReaderFake) GetPullRequestFiles(context.Context, change.RepositoryRef, int64) (change.DiffSummary, error) {
	return change.DiffSummary{}, nil
}
func (f *githubReaderFake) GetCIStatus(_ context.Context, repo change.RepositoryRef, commit string) (change.CIStatus, error) {
	f.lastRepo, f.lastCommit = repo, commit
	return f.ci, nil
}

type argoReaderFake struct {
	application string
	project     string
	result      change.ArgoApplication
}

func (f *argoReaderFake) GetApplication(_ context.Context, application, project string) (change.ArgoApplication, error) {
	f.application, f.project = application, project
	return f.result, nil
}
func (f *argoReaderFake) GetResourceStatus(context.Context, string, string) ([]change.ArgoResource, bool, string, error) {
	return nil, false, "", nil
}

func TestGitHubAdapterUsesExactRepositoryPRAndCommit(t *testing.T) {
	reader := &githubReaderFake{pr: change.PullRequest{State: "OPEN", HeadSHA: strings.Repeat("a", 40), MergeCommitSHA: strings.Repeat("b", 40)}, ci: change.CIStatus{Conclusion: "success"}}
	adapter := GitHub{Reader: reader}
	pr, err := adapter.ObservePullRequest(context.Background(), "acme/gitops", 42)
	if err != nil || reader.lastRepo.FullName() != "acme/gitops" || reader.lastNumber != 42 || pr.State != "open" {
		t.Fatalf("pr=%+v repo=%+v number=%d err=%v", pr, reader.lastRepo, reader.lastNumber, err)
	}
	commit := strings.Repeat("a", 40)
	ci, err := adapter.ObserveCI(context.Background(), "acme/gitops", commit)
	if err != nil || reader.lastCommit != commit || ci.Conclusion != "success" {
		t.Fatalf("ci=%+v commit=%s err=%v", ci, reader.lastCommit, err)
	}
	if _, err := adapter.ObservePullRequest(context.Background(), "https://github.com/acme/gitops", 42); err == nil {
		t.Fatal("arbitrary repository URL must be rejected")
	}
}

func TestArgoAdapterReturnsOnlyBoundedInternalSummary(t *testing.T) {
	reader := &argoReaderFake{result: change.ArgoApplication{TargetRevision: strings.Repeat("a", 40), DeployedRevision: strings.Repeat("a", 40), SyncStatus: "Synced", HealthStatus: "Healthy", OperationPhase: "Succeeded", Resources: []change.ArgoResource{{Kind: "Deployment", Namespace: "payments", Name: "api", Health: "Healthy"}}}}
	result, err := (Argo{Reader: reader}).ObserveApplication(context.Background(), "payments", "staging")
	if err != nil || reader.application != "payments" || reader.project != "staging" || len(result.ResourceHealth) > 16*1024 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var resources []map[string]any
	if json.Unmarshal(result.ResourceHealth, &resources) != nil || len(resources) != 1 || resources[0]["health"] != "Healthy" {
		t.Fatalf("bounded resources=%s", result.ResourceHealth)
	}
	reader.result.Resources = make([]change.ArgoResource, 1000)
	for i := range reader.result.Resources {
		reader.result.Resources[i] = change.ArgoResource{Kind: strings.Repeat("x", 64), Namespace: strings.Repeat("n", 255), Name: strings.Repeat("w", 255), Health: "Healthy"}
	}
	if _, err := (Argo{Reader: reader}).ObserveApplication(context.Background(), "payments", "staging"); err == nil {
		t.Fatal("oversized resource summary must be rejected")
	}
}
