package deliveryread

import (
	"context"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestDeliveryObserverBuildsRolloutIdentityFromRealReadPorts(t *testing.T) {
	gitopsRevision := strings.Repeat("b", 40)
	sourceRevision := strings.Repeat("c", 40)
	digest := "sha256:" + strings.Repeat("d", 64)
	observer, err := NewDeliveryObserver(
		deliveryGitHubStub{},
		argoStub{application: change.ArgoApplication{Name: "demo", Project: "project", Repository: "https://github.com/acme/gitops", Path: "env/demo", TargetRevision: "main", DeployedRevision: gitopsRevision}},
		rolloutStub{observation: verification.RolloutObservation{Generation: 4, ObservedGeneration: 4, RolloutRevision: "7", DesiredReplicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2, PodsReady: 2, PodsTotal: 2, Progressing: true, Available: true}},
		runtimeStub{values: []change.ContainerRuntime{{ContainerName: "demo", Image: "registry.example/acme/demo:release", ImageDigest: digest}}},
		registryStub{metadata: change.RegistryMetadata{ManifestDigest: digest, Revision: sourceRevision, Integrity: change.RegistryIntegrityVerified, Valid: true}},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := observer.Observe(context.Background(), taskhandler.DeliveryObserveRequest{
		Kind:            taskhandler.DeliveryObserveRollout,
		ArgoApplication: "demo", ArgoProject: "project", Cluster: "cloudops-local",
		Environment: "local-demo", Namespace: "demo", WorkloadKind: "Deployment",
		WorkloadName: "demo", Container: "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rollout == nil || result.Rollout.SourceRevision != sourceRevision || result.Rollout.ImageDigest != digest || result.Rollout.GitOpsRevision != gitopsRevision {
		t.Fatalf("rollout identity=%+v", result.Rollout)
	}
}

func TestDeliveryObserverRejectsUnverifiedRegistryIdentity(t *testing.T) {
	digest := "sha256:" + strings.Repeat("d", 64)
	observer, err := NewDeliveryObserver(
		deliveryGitHubStub{}, argoStub{}, rolloutStub{},
		runtimeStub{values: []change.ContainerRuntime{{ContainerName: "demo", Image: "registry.example/acme/demo:release", ImageDigest: digest}}},
		registryStub{metadata: change.RegistryMetadata{ManifestDigest: digest, Integrity: change.RegistryIntegrityUnknown}},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = observer.Observe(context.Background(), taskhandler.DeliveryObserveRequest{
		Kind: taskhandler.DeliveryObserveRollout, ArgoApplication: "demo", ArgoProject: "project",
		Cluster: "cloudops-local", Environment: "local-demo", Namespace: "demo",
		WorkloadKind: "Deployment", WorkloadName: "demo", Container: "demo",
	})
	if err == nil {
		t.Fatal("unverified Registry metadata was accepted")
	}
}

func TestDeliveryGitHubBindsTreeContentActorAndRequiredCIIdentity(t *testing.T) {
	head := strings.Repeat("a", 40)
	merged := strings.Repeat("b", 40)
	base := strings.Repeat("c", 40)
	blobSHA, err := change.GitBlobObjectID([]byte("healthy"), 40)
	if err != nil {
		t.Fatal(err)
	}
	reader := &exactGitHubStub{
		pullRequest: change.PullRequest{
			Number: 7, State: "closed", Merged: true, MergeCommitSHA: merged,
			BaseSHA: base, HeadSHA: head, MergedBy: "alice", MergedByType: "User", HTMLURL: "https://github.example/pr/7",
		},
		commits: map[string]change.Commit{
			head:   {SHA: head, TreeSHA: strings.Repeat("d", 40), Parents: []string{base}},
			merged: {SHA: merged, TreeSHA: strings.Repeat("d", 40), Parents: []string{base}},
		},
		files: map[string]change.FileContent{
			head:   {Revision: head, Path: "apps/demo/deployment.yaml", BlobSHA: blobSHA, Content: []byte("healthy")},
			merged: {Revision: merged, Path: "apps/demo/deployment.yaml", BlobSHA: blobSHA, Content: []byte("healthy")},
		},
		ci: change.CIStatus{
			CheckRuns:    []change.CheckRun{{ID: 1, Name: "gitops-required-check", Status: "completed", Conclusion: "success", AppID: 42}},
			WorkflowRuns: []change.WorkflowRun{{ID: 2, WorkflowID: 99, Path: ".github/workflows/gitops-required-check.yml", HeadSHA: head, Status: "completed", Conclusion: "success"}},
		},
	}
	github, err := NewDeliveryGitHub(DeliveryGitHubConfig{Reader: reader, RequiredCheckName: "gitops-required-check", ProducerAppID: 42, WorkflowID: 99, WorkflowPath: ".github/workflows/gitops-required-check.yml"})
	if err != nil {
		t.Fatal(err)
	}
	request := taskhandler.DeliveryObserveRequest{Repository: "acme/gitops", PullRequest: 7, HeadSHA: head, TargetPath: "apps/demo/deployment.yaml"}
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

func TestDeliveryGitHubSelectsLatestExactHeadRequiredRuns(t *testing.T) {
	head := strings.Repeat("a", 40)
	check, checkOK := selectRequiredCheck([]change.CheckRun{
		{ID: 10, Name: "gitops-required-check", Status: "completed", Conclusion: "failure", AppID: 42},
		{ID: 11, Name: "gitops-required-check", Status: "completed", Conclusion: "success", AppID: 42},
	}, "gitops-required-check", 42)
	workflow, workflowOK := selectRequiredWorkflow([]change.WorkflowRun{
		{ID: 20, WorkflowID: 99, Path: ".github/workflows/gitops-required-check.yml", HeadSHA: head, Status: "completed", Conclusion: "failure"},
		{ID: 21, WorkflowID: 99, Path: ".github/workflows/gitops-required-check.yml", HeadSHA: head, Status: "completed", Conclusion: "success"},
	}, head, 99, ".github/workflows/gitops-required-check.yml")
	status, conclusion := requiredCIResult(check, workflow, checkOK && workflowOK)
	if !checkOK || !workflowOK || check.ID != 11 || workflow.ID != 21 || status != "completed" || conclusion != "success" {
		t.Fatalf("latest exact-head CI selection check=%+v workflow=%+v status=%s conclusion=%s", check, workflow, status, conclusion)
	}
}

func TestDeliveryGitHubRejectsConflictingDuplicateRunIdentity(t *testing.T) {
	head := strings.Repeat("a", 40)
	if _, ok := selectRequiredCheck([]change.CheckRun{
		{ID: 11, Name: "gitops-required-check", Status: "completed", Conclusion: "success", AppID: 42},
		{ID: 11, Name: "gitops-required-check", Status: "completed", Conclusion: "failure", AppID: 42},
	}, "gitops-required-check", 42); ok {
		t.Fatal("conflicting duplicate required CheckRun identity was accepted")
	}
	if _, ok := selectRequiredWorkflow([]change.WorkflowRun{
		{ID: 21, WorkflowID: 99, Path: ".github/workflows/gitops-required-check.yml", HeadSHA: head, Status: "completed", Conclusion: "success"},
		{ID: 21, WorkflowID: 99, Path: ".github/workflows/gitops-required-check.yml", HeadSHA: head, Status: "completed", Conclusion: "failure"},
	}, head, 99, ".github/workflows/gitops-required-check.yml"); ok {
		t.Fatal("conflicting duplicate required WorkflowRun identity was accepted")
	}
}

func TestDeliveryGitHubRejectsAbbreviatedRevisionAndMismatchedBlobIdentity(t *testing.T) {
	head := strings.Repeat("a", 40)
	reader := &exactGitHubStub{
		commits: map[string]change.Commit{
			head: {SHA: head, TreeSHA: strings.Repeat("d", 40)},
		},
		files: map[string]change.FileContent{
			head: {Revision: head, Path: "apps/demo/deployment.yaml", BlobSHA: strings.Repeat("f", 40), Content: []byte("healthy")},
		},
	}
	github, err := NewDeliveryGitHub(DeliveryGitHubConfig{Reader: reader, RequiredCheckName: "gitops-required-check", ProducerAppID: 42, WorkflowID: 99, WorkflowPath: ".github/workflows/gitops-required-check.yml"})
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"deadbeef", head} {
		_, err := github.ObserveCI(context.Background(), taskhandler.DeliveryObserveRequest{
			Repository: "acme/gitops", HeadSHA: revision, TargetPath: "apps/demo/deployment.yaml",
		})
		if err == nil {
			t.Fatalf("invalid exact Git identity %q was accepted", revision)
		}
	}
}

type deliveryGitHubStub struct{}

func (deliveryGitHubStub) ObservePullRequest(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryPullRequestObservation, error) {
	return taskhandler.DeliveryPullRequestObservation{}, nil
}
func (deliveryGitHubStub) ObserveCI(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryCIObservation, error) {
	return taskhandler.DeliveryCIObservation{}, nil
}

type argoStub struct{ application change.ArgoApplication }

func (s argoStub) GetApplication(context.Context, string, string) (change.ArgoApplication, error) {
	return s.application, nil
}
func (argoStub) GetResourceStatus(context.Context, string, string) ([]change.ArgoResource, bool, string, error) {
	return nil, false, "", nil
}

type rolloutStub struct {
	observation verification.RolloutObservation
}

func (s rolloutStub) ObserveDeployment(context.Context, string, string, string) (verification.RolloutObservation, error) {
	return s.observation, nil
}

type runtimeStub struct{ values []change.ContainerRuntime }

func (s runtimeStub) ResolveRuntime(context.Context, string, string, string) ([]change.ContainerRuntime, error) {
	return s.values, nil
}

type registryStub struct{ metadata change.RegistryMetadata }

func (s registryStub) ReadMetadata(context.Context, string, string) (change.RegistryMetadata, error) {
	return s.metadata, nil
}

type exactGitHubStub struct {
	pullRequest change.PullRequest
	commits     map[string]change.Commit
	files       map[string]change.FileContent
	ci          change.CIStatus
}

func (s *exactGitHubStub) GetPullRequest(context.Context, change.RepositoryRef, int64) (change.PullRequest, error) {
	return s.pullRequest, nil
}
func (s *exactGitHubStub) GetCommit(_ context.Context, _ change.RepositoryRef, revision string) (change.Commit, error) {
	return s.commits[revision], nil
}
func (s *exactGitHubStub) GetCIStatus(context.Context, change.RepositoryRef, string) (change.CIStatus, error) {
	return s.ci, nil
}
func (s *exactGitHubStub) GetFileContent(_ context.Context, _ change.RepositoryRef, revision, _ string) (change.FileContent, error) {
	return s.files[revision], nil
}
