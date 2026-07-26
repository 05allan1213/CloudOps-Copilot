package investigationread

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

type baselineIdentity struct{ SourceRevision, ImageDigest, GitOpsRevision string }

func (t *Toolset) deploymentContext(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateDeploymentContextV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		Window string `json:"window"`
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	window, err := boundedWindow(params.Window, 30*time.Minute)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	application, err := t.cfg.Argo.GetApplication(ctx, t.cfg.Target.ArgoApplication, t.cfg.Target.ArgoProject)
	if err != nil {
		return unavailable(request.Action, "argocd", "argocd/deployment-context", err), nil
	}
	if !deploymentApplicationMatches(application, t.cfg.Target) {
		return agent.ToolObservation{}, agent.ErrPermission
	}
	runtimes, err := t.cfg.Runtime.ResolveRuntime(ctx, t.cfg.Target.Namespace, "Deployment", t.cfg.Target.Workload)
	if err != nil {
		return unavailable(request.Action, "kubernetes", "kubernetes/runtime-identity", err), nil
	}
	runtime, err := exactRuntime(runtimes, t.cfg.Target.Container)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	repository, err := repositoryFromImage(runtime.Image)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	metadata, err := t.cfg.Registry.ReadMetadata(ctx, repository, runtime.ImageDigest)
	if err != nil {
		return unavailable(request.Action, "registry", "registry/image-identity", err), nil
	}
	if !metadata.Valid || metadata.Integrity != change.RegistryIntegrityVerified || metadata.Truncated || metadata.Degraded || !strings.EqualFold(metadata.ManifestDigest, runtime.ImageDigest) || !exactRevision(metadata.Revision) {
		return agent.ToolObservation{}, change.ErrInvalidArgument
	}
	baseline, err := t.loadBaseline(ctx)
	if err != nil {
		return unavailable(request.Action, "mysql", "mysql/deployment-baseline", err), nil
	}
	deployedRevision := strings.ToLower(strings.TrimSpace(application.DeployedRevision))
	facts := make([]agent.EvidenceFact, 0, 16)
	argoType := "argocd.bad_revision_not_deployed"
	if exactRevision(deployedRevision) && !strings.EqualFold(deployedRevision, baseline.GitOpsRevision) && strings.EqualFold(application.SyncStatus, "Synced") && strings.EqualFold(application.OperationPhase, "Succeeded") {
		argoType = "argocd.bad_revision_deployed"
	}
	facts = append(facts, typedFact(request, argoType, "argocd", "argocd/deployment-context", "authoritative", "support", true, map[string]string{"deployed_revision": deployedRevision, "baseline_revision": baseline.GitOpsRevision, "sync_status": application.SyncStatus, "operation_phase": application.OperationPhase}))
	sourceSame := strings.EqualFold(metadata.Revision, baseline.SourceRevision)
	imageSame := strings.EqualFold(runtime.ImageDigest, baseline.ImageDigest)
	if sourceSame {
		facts = append(facts, typedFact(request, "source_revision.unchanged", "registry", "registry/image-config", "authoritative", "support", true, map[string]string{"source_revision": strings.ToLower(metadata.Revision), "baseline_source_revision": baseline.SourceRevision}))
	}
	if imageSame {
		facts = append(facts, typedFact(request, "image_digest.unchanged", "registry", "registry/manifest", "authoritative", "support", true, map[string]string{"image_digest": strings.ToLower(runtime.ImageDigest), "baseline_image_digest": baseline.ImageDigest}))
	}
	if !sourceSame && !imageSame {
		facts = append(facts, typedFact(request, "deployment.source_and_image_changed", "registry", "registry/deployment-identity", "authoritative", "support", true, map[string]string{"source_revision": strings.ToLower(metadata.Revision), "image_digest": strings.ToLower(runtime.ImageDigest)}))
	}
	cutoff := t.cfg.Now().Add(-window)
	history := append([]change.ArgoHistory(nil), application.History...)
	sort.Slice(history, func(i, j int) bool { return history[i].DeployedAt.After(history[j].DeployedAt) })
	count := 0
	seenRefs := map[string]struct{}{}
	for _, item := range history {
		if count >= 10 || item.DeployedAt.Before(cutoff) || !exactRevision(item.Revision) {
			continue
		}
		ref := changeReference(t.cfg.Target.Repository, item.Revision)
		if _, duplicate := seenRefs[ref]; duplicate {
			continue
		}
		seenRefs[ref] = struct{}{}
		fact := typedFact(request, "deployment.change_ref", "argocd", "argocd/deployment-context", "authoritative", "support", true, map[string]string{
			"change_ref": ref, "repository": t.cfg.Target.Repository.FullName(), "revision": strings.ToLower(item.Revision),
			"image_digest": strings.ToLower(runtime.ImageDigest), "path": t.cfg.Target.GitOpsPath,
			"deployed_at": item.DeployedAt.UTC().Format(time.RFC3339), "is_current": strconv.FormatBool(strings.EqualFold(item.Revision, deployedRevision)),
		})
		fact.ID = ref
		facts = append(facts, fact)
		count++
	}
	return available(request.Action, "argocd", "argocd/deployment-context", fmt.Sprintf("deployment identity is bound to one baseline and %d opaque change references", count), facts, application.ExternalURL), nil
}

func (t *Toolset) changeDetail(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateChangeDetailV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		ChangeRef string `json:"change_ref"`
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	if _, err := uuid.Parse(params.ChangeRef); err != nil {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	application, err := t.cfg.Argo.GetApplication(ctx, t.cfg.Target.ArgoApplication, t.cfg.Target.ArgoProject)
	if err != nil {
		return unavailable(request.Action, "argocd", "argocd/change-ref", err), nil
	}
	if !deploymentApplicationMatches(application, t.cfg.Target) {
		return agent.ToolObservation{}, agent.ErrPermission
	}
	revision := ""
	for _, item := range application.History {
		if exactRevision(item.Revision) && changeReference(t.cfg.Target.Repository, item.Revision) == params.ChangeRef {
			if revision != "" {
				return agent.ToolObservation{}, agent.ErrConflict
			}
			revision = strings.ToLower(item.Revision)
		}
	}
	if revision == "" && exactRevision(application.DeployedRevision) && changeReference(t.cfg.Target.Repository, application.DeployedRevision) == params.ChangeRef {
		revision = strings.ToLower(application.DeployedRevision)
	}
	if revision == "" {
		return agent.ToolObservation{}, agent.ErrPermission
	}
	commit, err := t.cfg.GitHub.GetCommit(ctx, t.cfg.Target.Repository, revision)
	if err != nil {
		return unavailable(request.Action, "github", "github/change-detail", err), nil
	}
	if !strings.EqualFold(commit.SHA, revision) || len(commit.Parents) != 1 || !exactRevision(commit.Parents[0]) {
		return agent.ToolObservation{}, change.ErrInvalidArgument
	}
	current, err := t.cfg.GitHub.GetFileContent(ctx, t.cfg.Target.Repository, revision, t.cfg.Target.GitOpsPath)
	if err != nil {
		return unavailable(request.Action, "github", "github/change-detail", err), nil
	}
	parent, err := t.cfg.GitHub.GetFileContent(ctx, t.cfg.Target.Repository, strings.ToLower(commit.Parents[0]), t.cfg.Target.GitOpsPath)
	if err != nil {
		return unavailable(request.Action, "github", "github/change-detail", err), nil
	}
	factType := "gitops.required_env_not_removed"
	patch, patchErr := remediation.RenderRestoreRequiredEnv(current.Content, parent.Content, remediationTarget(t.cfg.Target), t.cfg.Target.EnvKey)
	attributes := map[string]string{"change_ref": params.ChangeRef, "repository": t.cfg.Target.Repository.FullName(), "path": t.cfg.Target.GitOpsPath, "revision": revision}
	if patchErr == nil {
		factType = "gitops.required_env_removed"
		attributes["before_hash"], attributes["post_image_hash"] = patch.BeforeHash, patch.PostImageHash
	}
	facts := []agent.EvidenceFact{typedFact(request, factType, "github", "github/change-detail", "authoritative", "support", true, attributes)}
	ci, ciErr := t.cfg.GitHub.GetCIStatus(ctx, t.cfg.Target.Repository, revision)
	if ciErr != nil {
		return unavailable(request.Action, "github", "github/change-detail", ciErr), nil
	}
	ciType := "change.ci_not_succeeded"
	if strings.EqualFold(ci.Conclusion, "success") && !ci.Degraded {
		ciType = "change.ci_succeeded"
	}
	facts = append(facts, typedFact(request, ciType, "github", "github/change-detail", "authoritative", "support", true, map[string]string{
		"change_ref": params.ChangeRef, "repository": t.cfg.Target.Repository.FullName(), "revision": revision,
		"conclusion": ci.Conclusion, "check_runs": strconv.Itoa(len(ci.CheckRuns)), "workflow_runs": strconv.Itoa(len(ci.WorkflowRuns)),
	}))
	return available(request.Action, "github", "github/change-detail", "exact commit, parent file content and CI identity were read through allowlisted GitHub GETs", facts, commit.HTMLURL), nil
}

func (t *Toolset) loadBaseline(ctx context.Context) (returnIdentity baselineIdentity, retErr error) {
	rows, err := t.cfg.DB.QueryContext(ctx, `SELECT source_revision, image_digest, gitops_revision
FROM deployment_baselines
WHERE status = 'active' AND cluster = ? AND environment = ? AND namespace = ?
  AND workload_kind = 'Deployment' AND workload_name = ? AND container_name = ? AND repository = ?
  AND base_branch = ? AND target_path = ?
ORDER BY id LIMIT 2`, t.cfg.Target.Cluster, t.cfg.Target.Environment, t.cfg.Target.Namespace, t.cfg.Target.Workload,
		t.cfg.Target.Container, t.cfg.Target.Repository.FullName(), t.cfg.Target.BaseBranch, t.cfg.Target.GitOpsPath)
	if err != nil {
		return baselineIdentity{}, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close active deployment baseline rows: %w", closeErr))
		}
	}()
	var values []baselineIdentity
	for rows.Next() {
		var value baselineIdentity
		if err := rows.Scan(&value.SourceRevision, &value.ImageDigest, &value.GitOpsRevision); err != nil {
			return baselineIdentity{}, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return baselineIdentity{}, err
	}
	if len(values) != 1 || !exactRevision(values[0].SourceRevision) || !exactRevision(values[0].GitOpsRevision) || !strings.HasPrefix(values[0].ImageDigest, "sha256:") {
		return baselineIdentity{}, sql.ErrNoRows
	}
	values[0].SourceRevision, values[0].ImageDigest, values[0].GitOpsRevision = strings.ToLower(values[0].SourceRevision), strings.ToLower(values[0].ImageDigest), strings.ToLower(values[0].GitOpsRevision)
	return values[0], nil
}

func exactRuntime(values []change.ContainerRuntime, container string) (change.ContainerRuntime, error) {
	var result change.ContainerRuntime
	count := 0
	for _, value := range values {
		if value.ContainerName == container {
			result = value
			count++
		}
	}
	if count != 1 || result.Image == "" || result.ImageDigest == "" {
		return change.ContainerRuntime{}, change.ErrInvalidArgument
	}
	return result, nil
}
func sameRepositoryURL(raw string, repository change.RepositoryRef) bool {
	value := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(raw), "/"), ".git")
	return strings.HasSuffix(strings.ToLower(value), "/"+strings.ToLower(repository.FullName()))
}

func deploymentApplicationMatches(application change.ArgoApplication, target Target) bool {
	return application.Name == target.ArgoApplication && application.Project == target.ArgoProject &&
		(application.Repository == "" || sameRepositoryURL(application.Repository, target.Repository)) &&
		application.Path == target.ArgoPath
}
