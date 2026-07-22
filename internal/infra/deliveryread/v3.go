package deliveryread

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

// V3GitHubReader owns the extra exact-tree, post-image and CI producer checks
// required by delivery.observe. The legacy GitHubReader intentionally does not
// claim those stronger facts.
type V3GitHubReader interface {
	ObservePullRequest(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryPullRequestObservation, error)
	ObserveCI(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryCIObservation, error)
}

type exactGitHubReader interface {
	GetPullRequest(context.Context, change.RepositoryRef, int64) (change.PullRequest, error)
	GetCommit(context.Context, change.RepositoryRef, string) (change.Commit, error)
	GetCIStatus(context.Context, change.RepositoryRef, string) (change.CIStatus, error)
	GetFileContent(context.Context, change.RepositoryRef, string, string) (change.FileContent, error)
}

type V3GitHubConfig struct {
	Reader            exactGitHubReader
	RequiredCheckName string
	ProducerAppID     int64
	WorkflowID        int64
	WorkflowPath      string
}

// V3GitHub upgrades the existing bounded GitHub read client with the exact
// tree/content and required-check identity contract expected by
// delivery.observe. It performs reads only.
type V3GitHub struct{ config V3GitHubConfig }

func NewV3GitHub(config V3GitHubConfig) (*V3GitHub, error) {
	config.RequiredCheckName = strings.TrimSpace(config.RequiredCheckName)
	config.WorkflowPath = strings.TrimSpace(config.WorkflowPath)
	if config.Reader == nil || config.RequiredCheckName == "" || config.ProducerAppID <= 0 || config.WorkflowID <= 0 ||
		!strings.HasPrefix(config.WorkflowPath, ".github/workflows/") ||
		(!strings.HasSuffix(config.WorkflowPath, ".yml") && !strings.HasSuffix(config.WorkflowPath, ".yaml")) {
		return nil, errors.New("V3 GitHub delivery reader requires exact check App and workflow identity")
	}
	return &V3GitHub{config: config}, nil
}

func (g *V3GitHub) ObservePullRequest(ctx context.Context, request taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryPullRequestObservation, error) {
	repository, err := repositoryRef(request.Repository)
	if err != nil || request.PullRequest <= 0 || strings.TrimSpace(request.TargetPath) == "" {
		return taskhandler.DeliveryPullRequestObservation{}, verification.ErrInvalidArgument
	}
	pullRequest, err := g.config.Reader.GetPullRequest(ctx, repository, request.PullRequest)
	if err != nil {
		return taskhandler.DeliveryPullRequestObservation{}, err
	}
	headCommit, headHash, err := g.commitIdentity(ctx, repository, pullRequest.HeadSHA, request.TargetPath)
	if err != nil {
		return taskhandler.DeliveryPullRequestObservation{}, err
	}
	result := taskhandler.DeliveryPullRequestObservation{
		Repository: repository.FullName(), PullRequest: pullRequest.Number,
		State: pullRequest.State, Merged: pullRequest.Merged, MergeCommitSHA: pullRequest.MergeCommitSHA,
		BaseSHA: pullRequest.BaseSHA, HeadSHA: pullRequest.HeadSHA, HeadBranch: pullRequest.HeadBranch, HeadTreeSHA: headCommit.TreeSHA,
		HeadPostImageHash: headHash, HumanMerged: pullRequest.MergedBy != "" && strings.EqualFold(pullRequest.MergedByType, "User"),
		MergedBy: pullRequest.MergedBy, MergedByType: pullRequest.MergedByType, URL: pullRequest.HTMLURL,
	}
	if !pullRequest.Merged {
		return result, nil
	}
	mergeCommit, mergeHash, err := g.commitIdentity(ctx, repository, pullRequest.MergeCommitSHA, request.TargetPath)
	if err != nil {
		return taskhandler.DeliveryPullRequestObservation{}, err
	}
	result.MergedTreeSHA, result.MergedPostImageHash = mergeCommit.TreeSHA, mergeHash
	// The production GitOps PR contains one worker-created commit. With the
	// branch rules preflight restricted to squash-only, a new one-parent merge
	// commit is the bounded REST evidence for that merge method.
	if len(mergeCommit.Parents) == 1 && !strings.EqualFold(mergeCommit.SHA, pullRequest.HeadSHA) {
		result.MergeMethod = "squash"
	}
	return result, nil
}

func (g *V3GitHub) ObserveCI(ctx context.Context, request taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryCIObservation, error) {
	repository, err := repositoryRef(request.Repository)
	if err != nil || strings.TrimSpace(request.HeadSHA) == "" || strings.TrimSpace(request.TargetPath) == "" {
		return taskhandler.DeliveryCIObservation{}, verification.ErrInvalidArgument
	}
	commit, contentHash, err := g.commitIdentity(ctx, repository, request.HeadSHA, request.TargetPath)
	if err != nil {
		return taskhandler.DeliveryCIObservation{}, err
	}
	status, err := g.config.Reader.GetCIStatus(ctx, repository, request.HeadSHA)
	if err != nil {
		return taskhandler.DeliveryCIObservation{}, err
	}
	check, checkOK := selectRequiredCheck(status.CheckRuns, g.config.RequiredCheckName, g.config.ProducerAppID)
	workflow, workflowOK := selectRequiredWorkflow(status.WorkflowRuns, request.HeadSHA, g.config.WorkflowID, g.config.WorkflowPath)
	ciStatus, conclusion := requiredCIResult(check, workflow, checkOK && workflowOK)
	return taskhandler.DeliveryCIObservation{
		HeadSHA: request.HeadSHA, HeadTreeSHA: commit.TreeSHA, HeadPostImageHash: contentHash,
		Status: ciStatus, Conclusion: conclusion, RequiredChecksValid: checkOK && workflowOK,
		RequiredCheckName: g.config.RequiredCheckName, ProducerAppID: check.AppID,
		WorkflowID: workflow.WorkflowID, WorkflowPath: workflow.Path,
	}, nil
}

func (g *V3GitHub) commitIdentity(ctx context.Context, repository change.RepositoryRef, revision, targetPath string) (change.Commit, string, error) {
	revision = strings.ToLower(strings.TrimSpace(revision))
	if !change.ValidExactGitObjectID(revision) {
		return change.Commit{}, "", verification.ErrInvalidArgument
	}
	commit, err := g.config.Reader.GetCommit(ctx, repository, revision)
	if err != nil {
		return change.Commit{}, "", err
	}
	file, err := g.config.Reader.GetFileContent(ctx, repository, revision, targetPath)
	if err != nil {
		return change.Commit{}, "", err
	}
	if strings.ToLower(strings.TrimSpace(commit.SHA)) != revision || !change.ValidExactGitObjectID(strings.ToLower(strings.TrimSpace(commit.TreeSHA))) ||
		file.Revision != revision || file.Path != strings.TrimPrefix(targetPath, "./") ||
		!change.ValidExactGitObjectID(file.BlobSHA) || len(file.Content) == 0 {
		return change.Commit{}, "", verification.ErrInvalidArgument
	}
	commit.SHA = revision
	commit.TreeSHA = strings.ToLower(strings.TrimSpace(commit.TreeSHA))
	blobID, err := change.GitBlobObjectID(file.Content, len(file.BlobSHA))
	if err != nil || blobID != file.BlobSHA {
		return change.Commit{}, "", verification.ErrInvalidArgument
	}
	sum := sha256.Sum256(file.Content)
	return commit, hex.EncodeToString(sum[:]), nil
}

func selectRequiredCheck(checks []change.CheckRun, name string, appID int64) (change.CheckRun, bool) {
	var result change.CheckRun
	for _, check := range checks {
		if check.Name != name || check.AppID != appID {
			continue
		}
		if result.ID != 0 {
			return change.CheckRun{}, false
		}
		result = check
	}
	return result, result.ID > 0
}

func selectRequiredWorkflow(workflows []change.WorkflowRun, headSHA string, workflowID int64, workflowPath string) (change.WorkflowRun, bool) {
	var result change.WorkflowRun
	for _, workflow := range workflows {
		if workflow.WorkflowID != workflowID || workflow.Path != workflowPath || !strings.EqualFold(workflow.HeadSHA, headSHA) {
			continue
		}
		if result.ID != 0 {
			return change.WorkflowRun{}, false
		}
		result = workflow
	}
	return result, result.ID > 0
}

func requiredCIResult(check change.CheckRun, workflow change.WorkflowRun, valid bool) (string, string) {
	if !valid {
		return "completed", "failure"
	}
	statuses := []string{strings.ToLower(check.Status), strings.ToLower(workflow.Status)}
	conclusions := []string{strings.ToLower(check.Conclusion), strings.ToLower(workflow.Conclusion)}
	for _, conclusion := range conclusions {
		if conclusion != "" && conclusion != "success" {
			return "completed", conclusion
		}
	}
	for _, status := range statuses {
		if status == "queued" || status == "in_progress" || status == "pending" || status == "requested" || status == "waiting" {
			if status == "pending" || status == "requested" || status == "waiting" {
				status = "queued"
			}
			return status, ""
		}
	}
	if conclusions[0] == "success" && conclusions[1] == "success" {
		return "completed", "success"
	}
	return "queued", ""
}

// V3Observer composes the existing bounded Argo, Kubernetes and Registry read
// ports while preserving the one requested delivery phase. The rollout phase
// performs a fixed four-read identity chain: rollout, runtime imageID, Registry
// immutable metadata and Argo exact revision. It never accepts provider query
// language or a dynamic endpoint from a task.
type V3Observer struct {
	github   V3GitHubReader
	argo     change.ArgoCDReader
	rollout  verification.RolloutReader
	runtime  change.RuntimeReader
	registry change.RegistryMetadataReader
}

func NewV3Observer(
	github V3GitHubReader,
	argo change.ArgoCDReader,
	rollout verification.RolloutReader,
	runtime change.RuntimeReader,
	registry change.RegistryMetadataReader,
) (*V3Observer, error) {
	if github == nil || argo == nil || rollout == nil || runtime == nil || registry == nil {
		return nil, errors.New("V3 delivery observer requires GitHub, Argo, Kubernetes rollout/runtime, and Registry readers")
	}
	return &V3Observer{github: github, argo: argo, rollout: rollout, runtime: runtime, registry: registry}, nil
}

func (o *V3Observer) Observe(ctx context.Context, request taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryObservation, error) {
	if o == nil {
		return taskhandler.DeliveryObservation{}, verification.ErrInvalidArgument
	}
	switch request.Kind {
	case taskhandler.DeliveryObservePullRequest:
		observation, err := o.github.ObservePullRequest(ctx, request)
		return taskhandler.DeliveryObservation{Kind: request.Kind, PullRequest: &observation}, err
	case taskhandler.DeliveryObserveCI:
		observation, err := o.github.ObserveCI(ctx, request)
		return taskhandler.DeliveryObservation{Kind: request.Kind, CI: &observation}, err
	case taskhandler.DeliveryObserveArgo:
		observation, err := o.observeArgo(ctx, request)
		return taskhandler.DeliveryObservation{Kind: request.Kind, Argo: &observation}, err
	case taskhandler.DeliveryObserveRollout:
		observation, err := o.observeRollout(ctx, request)
		return taskhandler.DeliveryObservation{Kind: request.Kind, Rollout: &observation}, err
	default:
		return taskhandler.DeliveryObservation{}, verification.ErrInvalidArgument
	}
}

func (o *V3Observer) observeArgo(ctx context.Context, request taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryArgoObservation, error) {
	application, err := o.argo.GetApplication(ctx, request.ArgoApplication, request.ArgoProject)
	if err != nil {
		return taskhandler.DeliveryArgoObservation{}, err
	}
	resources := make([]map[string]any, 0, len(application.Resources))
	for _, resource := range application.Resources {
		resources = append(resources, map[string]any{
			"group": resource.Group, "kind": resource.Kind, "namespace": resource.Namespace,
			"name": resource.Name, "status": resource.Status, "health": resource.Health,
			"out_of_sync": resource.OutOfSync, "redacted": resource.Redacted,
		})
	}
	resourceJSON, err := json.Marshal(resources)
	if err != nil || len(resourceJSON) > 16*1024 {
		return taskhandler.DeliveryArgoObservation{}, verification.ErrInvalidArgument
	}
	syncResultRevision := latestArgoHistoryRevision(application)
	return taskhandler.DeliveryArgoObservation{
		Application: application.Name, Project: application.Project,
		Repository: application.Repository, Path: application.Path,
		TargetRevision: application.TargetRevision, SyncRevision: application.DeployedRevision,
		SyncResultRevision: syncResultRevision, SyncStatus: application.SyncStatus,
		OperationPhase: application.OperationPhase, OperationMessage: application.OperationMessage,
		HealthStatus: application.HealthStatus, ResourceHealth: resourceJSON,
		LastSyncedAt: application.LastSyncedAt,
	}, nil
}

func latestArgoHistoryRevision(application change.ArgoApplication) string {
	result := ""
	var latest int64
	for _, history := range application.History {
		stamp := history.DeployedAt.UnixNano()
		if strings.TrimSpace(history.Revision) != "" && (result == "" || stamp > latest) {
			result, latest = history.Revision, stamp
		}
	}
	return result
}

func (o *V3Observer) observeRollout(ctx context.Context, request taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryRolloutObservation, error) {
	rollout, err := o.rollout.ObserveDeployment(ctx, request.Cluster, request.Namespace, request.WorkloadName)
	if err != nil {
		return taskhandler.DeliveryRolloutObservation{}, err
	}
	runtimes, err := o.runtime.ResolveRuntime(ctx, request.Namespace, request.WorkloadKind, request.WorkloadName)
	if err != nil {
		return taskhandler.DeliveryRolloutObservation{}, err
	}
	runtime, err := selectRuntimeContainer(runtimes, request.Container)
	if err != nil {
		return taskhandler.DeliveryRolloutObservation{}, err
	}
	repository, err := registryRepository(runtime.Image)
	if err != nil {
		return taskhandler.DeliveryRolloutObservation{}, err
	}
	metadata, err := o.registry.ReadMetadata(ctx, repository, runtime.ImageDigest)
	if err != nil {
		return taskhandler.DeliveryRolloutObservation{}, err
	}
	if !metadata.Valid || metadata.Integrity != change.RegistryIntegrityVerified || metadata.Truncated || metadata.Degraded ||
		!strings.EqualFold(metadata.ManifestDigest, runtime.ImageDigest) {
		return taskhandler.DeliveryRolloutObservation{}, fmt.Errorf("%w: Registry identity is not verified", verification.ErrNotAllowed)
	}
	application, err := o.argo.GetApplication(ctx, request.ArgoApplication, request.ArgoProject)
	if err != nil {
		return taskhandler.DeliveryRolloutObservation{}, err
	}
	return taskhandler.DeliveryRolloutObservation{
		Cluster: request.Cluster, Environment: request.Environment, Namespace: request.Namespace,
		WorkloadKind: request.WorkloadKind, WorkloadName: request.WorkloadName, Container: request.Container,
		SourceRevision: metadata.Revision, ImageDigest: runtime.ImageDigest, GitOpsRevision: application.DeployedRevision,
		Generation: rollout.Generation, ObservedGeneration: rollout.ObservedGeneration,
		RolloutRevision: rollout.RolloutRevision, DesiredReplicas: rollout.DesiredReplicas,
		UpdatedReplicas: rollout.UpdatedReplicas, ReadyReplicas: rollout.ReadyReplicas,
		AvailableReplicas: rollout.AvailableReplicas, UnavailableReplicas: rollout.UnavailableReplicas,
		PodsReady: rollout.PodsReady, PodsTotal: rollout.PodsTotal,
		Progressing: rollout.Progressing, Available: rollout.Available,
		ProgressDeadlineExceeded: rollout.ProgressDeadlineExceeded,
	}, nil
}

func selectRuntimeContainer(values []change.ContainerRuntime, name string) (change.ContainerRuntime, error) {
	var result change.ContainerRuntime
	for _, value := range values {
		if value.ContainerName != name {
			continue
		}
		if result.ContainerName != "" || strings.TrimSpace(value.Image) == "" || strings.TrimSpace(value.ImageDigest) == "" {
			return change.ContainerRuntime{}, verification.ErrInvalidArgument
		}
		result = value
	}
	if result.ContainerName == "" {
		return change.ContainerRuntime{}, verification.ErrNotFound
	}
	return result, nil
}

func registryRepository(image string) (string, error) {
	image = strings.TrimSpace(image)
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	lastSlash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > lastSlash {
		image = image[:colon]
	}
	parts := strings.Split(strings.Trim(image, "/"), "/")
	if len(parts) < 2 {
		return "", verification.ErrInvalidArgument
	}
	if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost" {
		parts = parts[1:]
	}
	repository := strings.Join(parts, "/")
	if !strings.Contains(repository, "/") || strings.ContainsAny(repository, " *@?#") {
		return "", verification.ErrInvalidArgument
	}
	return strings.ToLower(repository), nil
}

var _ taskhandler.DeliveryObserver = (*V3Observer)(nil)
var _ V3GitHubReader = (*V3GitHub)(nil)
