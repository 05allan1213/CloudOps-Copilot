package deliveryread

import (
	"context"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestV3ObserverBuildsRolloutIdentityFromRealReadPorts(t *testing.T) {
	gitopsRevision := strings.Repeat("b", 40)
	sourceRevision := strings.Repeat("c", 40)
	digest := "sha256:" + strings.Repeat("d", 64)
	observer, err := NewV3Observer(
		v3GitHubStub{},
		v3ArgoStub{application: change.ArgoApplication{Name: "demo", Project: "project", Repository: "https://github.com/acme/gitops", Path: "env/demo", TargetRevision: "main", DeployedRevision: gitopsRevision}},
		v3RolloutStub{observation: verification.RolloutObservation{Generation: 4, ObservedGeneration: 4, RolloutRevision: "7", DesiredReplicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2, PodsReady: 2, PodsTotal: 2, Progressing: true, Available: true}},
		v3RuntimeStub{values: []change.ContainerRuntime{{ContainerName: "demo", Image: "registry.example/acme/demo:release", ImageDigest: digest}}},
		v3RegistryStub{metadata: change.RegistryMetadata{ManifestDigest: digest, Revision: sourceRevision, Integrity: change.RegistryIntegrityVerified, Valid: true}},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := observer.Observe(context.Background(), taskhandler.DeliveryObserveRequest{
		Kind:            taskhandler.DeliveryObserveRollout,
		ArgoApplication: "demo", ArgoProject: "project", Cluster: "kind-cloudops",
		Environment: "local-demo", Namespace: "cloudops-demo", WorkloadKind: "Deployment",
		WorkloadName: "demo", Container: "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rollout == nil || result.Rollout.SourceRevision != sourceRevision || result.Rollout.ImageDigest != digest || result.Rollout.GitOpsRevision != gitopsRevision {
		t.Fatalf("rollout identity=%+v", result.Rollout)
	}
}

func TestV3ObserverRejectsUnverifiedRegistryIdentity(t *testing.T) {
	digest := "sha256:" + strings.Repeat("d", 64)
	observer, err := NewV3Observer(
		v3GitHubStub{}, v3ArgoStub{}, v3RolloutStub{},
		v3RuntimeStub{values: []change.ContainerRuntime{{ContainerName: "demo", Image: "registry.example/acme/demo:release", ImageDigest: digest}}},
		v3RegistryStub{metadata: change.RegistryMetadata{ManifestDigest: digest, Integrity: change.RegistryIntegrityUnknown}},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = observer.Observe(context.Background(), taskhandler.DeliveryObserveRequest{
		Kind: taskhandler.DeliveryObserveRollout, ArgoApplication: "demo", ArgoProject: "project",
		Cluster: "kind-cloudops", Environment: "local-demo", Namespace: "cloudops-demo",
		WorkloadKind: "Deployment", WorkloadName: "demo", Container: "demo",
	})
	if err == nil {
		t.Fatal("unverified Registry metadata was accepted")
	}
}

func TestV3GitHubBindsTreeContentActorAndRequiredCIIdentity(t *testing.T) {
	head := strings.Repeat("a", 40)
	merged := strings.Repeat("b", 40)
	base := strings.Repeat("c", 40)
	blobSHA, err := change.GitBlobObjectID([]byte("healthy"), 40)
	if err != nil {
		t.Fatal(err)
	}
	reader := &v3ExactGitHubStub{
		pullRequest: change.PullRequest{
			Number: 7, State: "closed", Merged: true, MergeCommitSHA: merged,
			BaseSHA: base, HeadSHA: head, MergedBy: "alice", MergedByType: "User", HTMLURL: "https://github.example/pr/7",
		},
		commits: map[string]change.Commit{
			head:   {SHA: head, TreeSHA: strings.Repeat("d", 40), Parents: []string{base}},
			merged: {SHA: merged, TreeSHA: strings.Repeat("d", 40), Parents: []string{base}},
		},
		files: map[string]change.FileContent{
			head:   {Revision: head, Path: "env/demo.yaml", BlobSHA: blobSHA, Content: []byte("healthy")},
			merged: {Revision: merged, Path: "env/demo.yaml", BlobSHA: blobSHA, Content: []byte("healthy")},
		},
		ci: change.CIStatus{
			CheckRuns:    []change.CheckRun{{ID: 1, Name: "cloudops/validate", Status: "completed", Conclusion: "success", AppID: 42}},
			WorkflowRuns: []change.WorkflowRun{{ID: 2, WorkflowID: 99, Path: ".github/workflows/cloudops.yaml", HeadSHA: head, Status: "completed", Conclusion: "success"}},
		},
	}
	github, err := NewV3GitHub(V3GitHubConfig{Reader: reader, RequiredCheckName: "cloudops/validate", ProducerAppID: 42, WorkflowID: 99, WorkflowPath: ".github/workflows/cloudops.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	request := taskhandler.DeliveryObserveRequest{Repository: "acme/gitops", PullRequest: 7, HeadSHA: head, TargetPath: "env/demo.yaml"}
	pullRequest, err := github.ObservePullRequest(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !pullRequest.HumanMerged || pullRequest.MergeMethod != "squash" || pullRequest.HeadPostImageHash == "" || pullRequest.HeadPostImageHash != pullRequest.MergedPostImageHash {
		t.Fatalf("pull request observation=%+v", pullRequest)
	}
	ci, err := github.ObserveCI(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !ci.RequiredChecksValid || ci.Status != "completed" || ci.Conclusion != "success" || ci.ProducerAppID != 42 || ci.WorkflowID != 99 {
		t.Fatalf("CI observation=%+v", ci)
	}
}

func TestV3GitHubRejectsAbbreviatedRevisionAndMismatchedBlobIdentity(t *testing.T) {
	head := strings.Repeat("a", 40)
	reader := &v3ExactGitHubStub{
		commits: map[string]change.Commit{
			head: {SHA: head, TreeSHA: strings.Repeat("d", 40)},
		},
		files: map[string]change.FileContent{
			head: {Revision: head, Path: "env/demo.yaml", BlobSHA: strings.Repeat("f", 40), Content: []byte("healthy")},
		},
	}
	github, err := NewV3GitHub(V3GitHubConfig{Reader: reader, RequiredCheckName: "cloudops/validate", ProducerAppID: 42, WorkflowID: 99, WorkflowPath: ".github/workflows/cloudops.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"deadbeef", head} {
		_, err := github.ObserveCI(context.Background(), taskhandler.DeliveryObserveRequest{
			Repository: "acme/gitops", HeadSHA: revision, TargetPath: "env/demo.yaml",
		})
		if err == nil {
			t.Fatalf("invalid exact Git identity %q was accepted", revision)
		}
	}
}

type v3GitHubStub struct{}

func (v3GitHubStub) ObservePullRequest(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryPullRequestObservation, error) {
	return taskhandler.DeliveryPullRequestObservation{}, nil
}
func (v3GitHubStub) ObserveCI(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryCIObservation, error) {
	return taskhandler.DeliveryCIObservation{}, nil
}

type v3ArgoStub struct{ application change.ArgoApplication }

func (s v3ArgoStub) GetApplication(context.Context, string, string) (change.ArgoApplication, error) {
	return s.application, nil
}
func (v3ArgoStub) GetResourceStatus(context.Context, string, string) ([]change.ArgoResource, bool, string, error) {
	return nil, false, "", nil
}

type v3RolloutStub struct {
	observation verification.RolloutObservation
}

func (s v3RolloutStub) ObserveDeployment(context.Context, string, string, string) (verification.RolloutObservation, error) {
	return s.observation, nil
}

type v3RuntimeStub struct{ values []change.ContainerRuntime }

func (s v3RuntimeStub) ResolveRuntime(context.Context, string, string, string) ([]change.ContainerRuntime, error) {
	return s.values, nil
}

type v3RegistryStub struct{ metadata change.RegistryMetadata }

func (s v3RegistryStub) ReadMetadata(context.Context, string, string) (change.RegistryMetadata, error) {
	return s.metadata, nil
}

type v3ExactGitHubStub struct {
	pullRequest change.PullRequest
	commits     map[string]change.Commit
	files       map[string]change.FileContent
	ci          change.CIStatus
}

func (s *v3ExactGitHubStub) GetPullRequest(context.Context, change.RepositoryRef, int64) (change.PullRequest, error) {
	return s.pullRequest, nil
}
func (s *v3ExactGitHubStub) GetCommit(_ context.Context, _ change.RepositoryRef, revision string) (change.Commit, error) {
	return s.commits[revision], nil
}
func (s *v3ExactGitHubStub) GetCIStatus(context.Context, change.RepositoryRef, string) (change.CIStatus, error) {
	return s.ci, nil
}
func (s *v3ExactGitHubStub) GetFileContent(_ context.Context, _ change.RepositoryRef, revision, _ string) (change.FileContent, error) {
	return s.files[revision], nil
}
