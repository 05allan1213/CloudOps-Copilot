package investigationread

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
)

const (
	changeDetailCurrentYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: demo
          image: example/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          env:
            - name: OTHER_ENV
              value: keep
`
	changeDetailParentYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: demo
          image: example/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          env:
            - name: OTHER_ENV
              value: keep
            - name: REQUIRED_ENV
              value: healthy
`
)

func TestChangeDetailBindsMergeRevisionToUniquePullRequestHeadCI(t *testing.T) {
	mergeSHA := strings.Repeat("a", 40)
	baseSHA := strings.Repeat("b", 40)
	headSHA := strings.Repeat("c", 40)
	changeRef := changeReference(change.RepositoryRef{Owner: "acme", Name: "gitops"}, mergeSHA)
	github := &changeDetailGitHubStub{
		commit: change.Commit{SHA: mergeSHA, Parents: []string{baseSHA, headSHA}, HTMLURL: "https://github.example/commit/" + mergeSHA},
		pullRequests: []change.PullRequest{{
			Repository: "acme/gitops", Number: 34, State: "closed", Merged: true,
			MergeCommitSHA: mergeSHA, BaseSHA: baseSHA, HeadSHA: headSHA, HTMLURL: "https://github.example/pull/34",
		}},
		files: map[string]change.FileContent{
			mergeSHA: {Revision: mergeSHA, Content: []byte(changeDetailCurrentYAML)},
			baseSHA:  {Revision: baseSHA, Content: []byte(changeDetailParentYAML)},
		},
		ci: change.CIStatus{
			CommitSHA: headSHA, Conclusion: "success",
			CheckRuns:    []change.CheckRun{{ID: 1, Status: "completed", Conclusion: "success"}},
			WorkflowRuns: []change.WorkflowRun{{ID: 2, HeadSHA: headSHA, Status: "completed", Conclusion: "success"}},
		},
	}
	tools := changeDetailToolset(github, mergeSHA)
	request := changeDetailRequest(changeRef)

	observation, err := tools.changeDetail(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if github.listedSHA != mergeSHA || github.ciSHA != headSHA {
		t.Fatalf("listed_sha=%q ci_sha=%q", github.listedSHA, github.ciSHA)
	}
	if observation.Status != agent.CollectionAvailable || len(observation.Facts) != 2 || observation.SafeDeepLink != "https://github.example/pull/34" {
		t.Fatalf("observation=%+v", observation)
	}
	for _, fact := range observation.Facts {
		if fact.Attributes["merge_sha"] != mergeSHA || fact.Attributes["head_sha"] != headSHA || fact.Attributes["pull_request_number"] != "34" {
			t.Fatalf("fact attributes=%+v", fact.Attributes)
		}
	}
	if observation.Facts[0].Type != "gitops.required_env_removed" || observation.Facts[1].Type != "change.ci_succeeded" || observation.Facts[1].Attributes["ci_commit_sha"] != headSHA {
		t.Fatalf("facts=%+v", observation.Facts)
	}
}

func TestChangeDetailRejectsAmbiguousPRAndMismatchedCIIdentity(t *testing.T) {
	mergeSHA := strings.Repeat("a", 40)
	baseSHA := strings.Repeat("b", 40)
	headSHA := strings.Repeat("c", 40)
	changeRef := changeReference(change.RepositoryRef{Owner: "acme", Name: "gitops"}, mergeSHA)
	validPR := change.PullRequest{Repository: "acme/gitops", Number: 34, Merged: true, MergeCommitSHA: mergeSHA, BaseSHA: baseSHA, HeadSHA: headSHA}
	base := changeDetailGitHubStub{
		commit:       change.Commit{SHA: mergeSHA, Parents: []string{baseSHA}},
		pullRequests: []change.PullRequest{validPR},
		files: map[string]change.FileContent{
			mergeSHA: {Revision: mergeSHA, Content: []byte(changeDetailCurrentYAML)},
			baseSHA:  {Revision: baseSHA, Content: []byte(changeDetailParentYAML)},
		},
		ci: change.CIStatus{CommitSHA: strings.Repeat("d", 40), Conclusion: "success"},
	}
	if _, err := changeDetailToolset(&base, mergeSHA).changeDetail(context.Background(), changeDetailRequest(changeRef)); !errors.Is(err, change.ErrInvalidArgument) {
		t.Fatalf("mismatched CI identity error=%v", err)
	}
	ambiguous := base
	ambiguous.pullRequests = []change.PullRequest{validPR, validPR}
	ambiguous.ci.CommitSHA = headSHA
	if _, err := changeDetailToolset(&ambiguous, mergeSHA).changeDetail(context.Background(), changeDetailRequest(changeRef)); !errors.Is(err, change.ErrConflict) {
		t.Fatalf("ambiguous pull request error=%v", err)
	}
	parentMismatch := base
	parentMismatch.pullRequests = []change.PullRequest{validPR}
	parentMismatch.pullRequests[0].BaseSHA = strings.Repeat("e", 40)
	parentMismatch.ci.CommitSHA = headSHA
	if _, err := changeDetailToolset(&parentMismatch, mergeSHA).changeDetail(context.Background(), changeDetailRequest(changeRef)); !errors.Is(err, change.ErrInvalidArgument) {
		t.Fatalf("pull request parent mismatch error=%v", err)
	}
}

func changeDetailToolset(github *changeDetailGitHubStub, mergeSHA string) *Toolset {
	target := Target{
		Service: "demo", Cluster: "kind", Environment: "local", Namespace: "demo", Workload: "demo", Container: "demo",
		Repository: change.RepositoryRef{Owner: "acme", Name: "gitops"}, BaseBranch: "main", GitOpsPath: "apps/demo/deployment.yaml",
		ArgoPath: "apps/demo", ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo", EnvKey: "REQUIRED_ENV",
	}
	return &Toolset{cfg: Config{
		GitHub: github, Argo: changeDetailArgoStub{application: change.ArgoApplication{
			Name: target.ArgoApplication, Project: target.ArgoProject, Repository: "https://github.com/acme/gitops.git",
			Path: target.ArgoPath, DeployedRevision: mergeSHA, History: []change.ArgoHistory{{Revision: mergeSHA, DeployedAt: time.Now().UTC()}},
		}},
		Target: target,
	}}
}

func changeDetailRequest(changeRef string) agent.InvestigationToolRequest {
	return agent.InvestigationToolRequest{Action: agent.ProposedAction{
		Tool: ToolGetChangeDetail, ScopeRef: "scope-1", TemplateID: TemplateChangeDetailV1,
		BoundedParameters: []byte(`{"change_ref":"` + changeRef + `"}`), ExpectedFactTypes: GoldenActionPolicies()[ToolGetChangeDetail].ExpectedFactTypes,
	}}
}

type changeDetailArgoStub struct {
	change.ArgoCDReader
	application change.ArgoApplication
}

func (s changeDetailArgoStub) GetApplication(context.Context, string, string) (change.ArgoApplication, error) {
	return s.application, nil
}

type changeDetailGitHubStub struct {
	commit       change.Commit
	pullRequests []change.PullRequest
	files        map[string]change.FileContent
	ci           change.CIStatus
	listedSHA    string
	ciSHA        string
}

func (s *changeDetailGitHubStub) GetCommit(context.Context, change.RepositoryRef, string) (change.Commit, error) {
	return s.commit, nil
}

func (*changeDetailGitHubStub) GetCommitDiff(context.Context, change.RepositoryRef, string) (change.DiffSummary, error) {
	return change.DiffSummary{}, nil
}

func (s *changeDetailGitHubStub) ListPullRequestsForCommit(_ context.Context, _ change.RepositoryRef, sha string) ([]change.PullRequest, error) {
	s.listedSHA = sha
	return s.pullRequests, nil
}

func (*changeDetailGitHubStub) GetPullRequest(context.Context, change.RepositoryRef, int64) (change.PullRequest, error) {
	return change.PullRequest{}, nil
}

func (*changeDetailGitHubStub) GetPullRequestFiles(context.Context, change.RepositoryRef, int64) (change.DiffSummary, error) {
	return change.DiffSummary{}, nil
}

func (s *changeDetailGitHubStub) GetCIStatus(_ context.Context, _ change.RepositoryRef, sha string) (change.CIStatus, error) {
	s.ciSHA = sha
	return s.ci, nil
}

func (s *changeDetailGitHubStub) GetFileContent(_ context.Context, _ change.RepositoryRef, revision, _ string) (change.FileContent, error) {
	return s.files[revision], nil
}
