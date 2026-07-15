package deliveryread

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"server-web/internal/change"
	"server-web/internal/verification"
)

type GitHub struct{ Reader change.GitHubReader }

func (g GitHub) ObservePullRequest(ctx context.Context, repository string, number int64) (verification.PullRequestObservation, error) {
	repo, err := repositoryRef(repository)
	if err != nil || g.Reader == nil {
		return verification.PullRequestObservation{}, verification.ErrInvalidArgument
	}
	pr, err := g.Reader.GetPullRequest(ctx, repo, number)
	if err != nil {
		return verification.PullRequestObservation{}, err
	}
	return verification.PullRequestObservation{State: strings.ToLower(pr.State), Merged: pr.Merged, MergeCommitSHA: pr.MergeCommitSHA, BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA}, nil
}

func (g GitHub) ObserveCI(ctx context.Context, repository, commit string) (verification.CIObservation, error) {
	repo, err := repositoryRef(repository)
	if err != nil || g.Reader == nil {
		return verification.CIObservation{}, verification.ErrInvalidArgument
	}
	ci, err := g.Reader.GetCIStatus(ctx, repo, commit)
	if err != nil {
		return verification.CIObservation{}, err
	}
	return verification.CIObservation{Conclusion: ci.Conclusion}, nil
}

type Argo struct{ Reader change.ArgoCDReader }

func (a Argo) ObserveApplication(ctx context.Context, application, project string) (verification.ArgoObservation, error) {
	if a.Reader == nil {
		return verification.ArgoObservation{}, verification.ErrInvalidArgument
	}
	app, err := a.Reader.GetApplication(ctx, application, project)
	if err != nil {
		return verification.ArgoObservation{}, err
	}
	resources := make([]map[string]any, 0, len(app.Resources))
	for _, resource := range app.Resources {
		resources = append(resources, map[string]any{"kind": resource.Kind, "namespace": resource.Namespace, "name": resource.Name, "health": resource.Health, "status": resource.Status, "out_of_sync": resource.OutOfSync})
	}
	resourceJSON, _ := json.Marshal(resources)
	if len(resourceJSON) > 16*1024 {
		return verification.ArgoObservation{}, verification.ErrInvalidArgument
	}
	return verification.ArgoObservation{TargetRevision: app.TargetRevision, DeployedRevision: app.DeployedRevision, SyncStatus: app.SyncStatus, HealthStatus: app.HealthStatus, OperationPhase: app.OperationPhase, OperationMessage: app.OperationMessage, ResourceHealth: resourceJSON, LastSyncedAt: app.LastSyncedAt}, nil
}

func repositoryRef(value string) (change.RepositoryRef, error) {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return change.RepositoryRef{}, fmt.Errorf("%w: repository", verification.ErrInvalidArgument)
	}
	return change.RepositoryRef{Owner: parts[0], Name: parts[1]}, nil
}
