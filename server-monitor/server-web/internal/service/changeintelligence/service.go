package changeintelligence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"server-web/internal/change"
	domain "server-web/internal/incident"
)

var commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

type IncidentReader interface {
	GetByPublicID(context.Context, string) (*domain.Incident, error)
}

type atomicEvidenceWriter interface {
	PersistWithEvidence(context.Context, *change.Change, *domain.EvidenceItem) (bool, error)
}

type ServiceMapping struct {
	Repository       change.RepositoryRef `json:"repository"`
	SourceRepository string               `json:"source_repository,omitempty"`
	ArgoApplication  string               `json:"argocd_application"`
	ArgoProject      string               `json:"argocd_project"`
	GitOpsPath       string               `json:"gitops_path"`
	ContainerName    string               `json:"container_name,omitempty"`
}

type Observer interface {
	ObserveChangeCorrelation(result string, seconds float64)
	ObserveChangeCandidate(source string)
}

type Config struct {
	Enabled           bool
	Lookback          time.Duration
	MaxCandidates     int
	Incidents         IncidentReader
	Changes           change.Repository
	GitHub            change.GitHubReader
	ArgoCD            change.ArgoCDReader
	Runtime           change.RuntimeReader
	Registry          change.RegistryMetadataReader
	RegistryHosts     []string
	AllowedOCISources []string
	Mappings          map[string]ServiceMapping
	Observer          Observer
	Now               func() time.Time
}

type Service struct{ cfg Config }

type Context struct {
	Enabled         bool                     `json:"enabled"`
	Status          string                   `json:"status"`
	CurrentRuntime  change.RuntimeContext    `json:"current_runtime"`
	Candidates      []change.Change          `json:"candidates"`
	Correlation     change.CorrelationResult `json:"correlation"`
	Unknowns        []string                 `json:"unknowns"`
	Degraded        bool                     `json:"degraded"`
	ImageResolution change.ImageResolution   `json:"image_resolution"`
	RefreshedAt     *time.Time               `json:"refreshed_at,omitempty"`
}

func New(cfg Config) (*Service, error) {
	if cfg.Lookback <= 0 {
		cfg.Lookback = 24 * time.Hour
	}
	if cfg.Lookback < time.Minute || cfg.Lookback > 30*24*time.Hour {
		return nil, fmt.Errorf("%w: invalid lookback", change.ErrInvalidArgument)
	}
	if cfg.MaxCandidates <= 0 {
		cfg.MaxCandidates = 10
	}
	if cfg.MaxCandidates > 50 {
		return nil, fmt.Errorf("%w: max candidates exceeds 50", change.ErrInvalidArgument)
	}
	if cfg.Incidents == nil || cfg.Changes == nil {
		return nil, fmt.Errorf("%w: change persistence dependencies", change.ErrInvalidArgument)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) Enabled() bool { return s != nil && s.cfg.Enabled }

func (s *Service) ListChanges(ctx context.Context, incidentID string, filter change.ListFilter) (change.Page, error) {
	if s == nil || !s.cfg.Enabled {
		return change.Page{}, change.ErrUnavailable
	}
	return s.cfg.Changes.ListByIncident(ctx, incidentID, filter)
}

func (s *Service) GetContext(ctx context.Context, incidentID string) (Context, error) {
	if s == nil || !s.cfg.Enabled {
		return Context{Enabled: false, Status: "disabled", Unknowns: []string{"change intelligence disabled"}}, nil
	}
	incident, err := s.cfg.Incidents.GetByPublicID(ctx, incidentID)
	if err != nil {
		return Context{}, err
	}
	page, err := s.cfg.Changes.ListByIncident(ctx, incidentID, change.ListFilter{Page: 1, PageSize: s.cfg.MaxCandidates})
	if err != nil {
		return Context{}, err
	}
	runtime := runtimeFromIncident(incident)
	resolution := change.ImageResolution{}
	if len(page.Items) > 0 {
		top := page.Items[0]
		runtime.Image = joinImage(top.ImageRepository, top.ImageTag)
		runtime.ImageDigest = top.ImageDigest
		runtime.Revision = firstNonEmpty(top.ArgoCDDeployedRevision, top.ImageRevision, top.CommitSHA)
		runtime.ArgoApplication = top.ArgoCDApplication
		var persisted struct {
			ImageResolution change.ImageResolution `json:"image_resolution"`
		}
		if json.Unmarshal(top.Metadata, &persisted) == nil {
			resolution = persisted.ImageResolution
		}
	}
	correlation := change.Correlate(runtime, page.Items, s.cfg.Lookback, s.cfg.Now())
	status := "ok"
	unknowns := append([]string(nil), correlation.Unknowns...)
	if len(page.Items) == 0 {
		status = "no_data"
	}
	return Context{Enabled: true, Status: status, CurrentRuntime: runtime, Candidates: page.Items, Correlation: correlation, Unknowns: unknowns, ImageResolution: resolution}, nil
}

func (s *Service) ResolveImage(ctx context.Context, incidentID string) (change.ImageResolution, error) {
	current, err := s.Refresh(ctx, incidentID)
	if err != nil {
		if observer, ok := s.cfg.Observer.(interface{ ObserveImageResolution(string) }); ok {
			observer.ObserveImageResolution("error")
		}
		return change.ImageResolution{}, err
	}
	result := current.ImageResolution
	return result, nil
}

func (s *Service) Refresh(ctx context.Context, incidentID string) (Context, error) {
	if s == nil || !s.cfg.Enabled {
		return Context{Enabled: false, Status: "disabled"}, change.ErrUnavailable
	}
	started := s.cfg.Now()
	ctx, span := otel.Tracer("server-web/internal/service/changeintelligence").Start(ctx, "change.context.load")
	defer span.End()
	incident, err := s.cfg.Incidents.GetByPublicID(ctx, incidentID)
	if err != nil {
		return Context{}, err
	}
	mapping, ok := s.mappingFor(incident)
	if !ok {
		return Context{Enabled: true, Status: "invalid_configuration", CurrentRuntime: runtimeFromIncident(incident), Degraded: true, Unknowns: []string{"service change mapping unavailable"}}, nil
	}
	runtime := runtimeFromIncident(incident)
	unknowns := []string{}
	degraded := false
	var registryMetadata change.RegistryMetadata
	registryErrorCode := ""
	runtimeRepository := ""
	runtimeTag := ""
	if s.cfg.Runtime != nil {
		runtimeCtx, runtimeSpan := otel.Tracer("server-web/internal/service/changeintelligence").Start(ctx, "change.runtime.resolve")
		containers, runtimeErr := s.cfg.Runtime.ResolveRuntime(runtimeCtx, incident.Namespace, incident.TargetKind, incident.TargetName)
		runtimeSpan.SetAttributes(attribute.String("result", resultName(runtimeErr)))
		runtimeSpan.End()
		if runtimeErr != nil {
			degraded = true
			unknowns = append(unknowns, "runtime image unavailable")
		} else if container, selected := selectRuntimeContainer(containers, mapping.ContainerName); selected {
			runtime.Image = container.Image
			runtime.ImageDigest = container.ImageDigest
			runtime.WorkloadKind = container.WorkloadKind
			runtime.WorkloadName = container.WorkloadName
			if runtime.ArgoApplication == "" {
				runtime.ArgoApplication = container.Annotations["argocd.argoproj.io/instance"]
			}
			runtimeRepository, runtimeTag, runtimeErr = registryRepository(container.Image, s.cfg.RegistryHosts)
			if runtimeErr != nil {
				degraded = true
				unknowns = append(unknowns, "runtime image repository unavailable")
			}
		} else {
			degraded = true
			unknowns = append(unknowns, "runtime container selection is ambiguous")
		}
	}
	if s.cfg.Registry != nil && runtimeRepository != "" && runtime.ImageDigest != "" {
		registryCtx, registrySpan := otel.Tracer("server-web/internal/service/changeintelligence").Start(ctx, "change.registry.query")
		registryMetadata, err = s.cfg.Registry.ReadMetadata(registryCtx, runtimeRepository, runtime.ImageDigest)
		registrySpan.SetAttributes(attribute.String("result", resultName(err)))
		registrySpan.End()
		if err != nil {
			degraded = true
			registryErrorCode = registryCode(err)
			unknowns = append(unknowns, "registry OCI metadata unavailable")
		}
	} else if s.cfg.Registry == nil {
		degraded = true
		registryErrorCode = "unavailable"
		unknowns = append(unknowns, "registry adapter disabled")
	} else {
		degraded = true
		registryErrorCode = "unavailable"
		unknowns = append(unknowns, "runtime immutable image identity unavailable")
	}
	expectedSource := strings.TrimSpace(mapping.SourceRepository)
	if expectedSource == "" {
		expectedSource = "https://github.com/" + mapping.Repository.FullName()
	}
	if runtime.ArgoApplication == "" {
		runtime.ArgoApplication = mapping.ArgoApplication
	}
	if s.cfg.ArgoCD == nil {
		imageResolution := s.resolveImage(ctx, change.ImageResolutionInput{RuntimeDigest: runtime.ImageDigest, ImageTag: runtimeTag, RegistryMetadata: registryMetadata, RegistryErrorCode: registryErrorCode, ExpectedSourceRepository: expectedSource, AllowedOCISources: s.cfg.AllowedOCISources})
		return s.finish(started, Context{Enabled: true, Status: "unavailable", CurrentRuntime: runtime, Degraded: true, Unknowns: append(unknowns, "argocd adapter disabled"), ImageResolution: imageResolution}), nil
	}
	argoCtx, argoSpan := otel.Tracer("server-web/internal/service/changeintelligence").Start(ctx, "change.argocd.query")
	application, argoErr := s.cfg.ArgoCD.GetApplication(argoCtx, mapping.ArgoApplication, mapping.ArgoProject)
	argoSpan.SetAttributes(attribute.String("result", resultName(argoErr)))
	argoSpan.End()
	if argoErr != nil {
		imageResolution := s.resolveImage(ctx, change.ImageResolutionInput{RuntimeDigest: runtime.ImageDigest, ImageTag: runtimeTag, RegistryMetadata: registryMetadata, RegistryErrorCode: registryErrorCode, ExpectedSourceRepository: expectedSource, AllowedOCISources: s.cfg.AllowedOCISources})
		return s.finish(started, Context{Enabled: true, Status: "unavailable", CurrentRuntime: runtime, Degraded: true, Unknowns: append(unknowns, "argocd application unavailable"), ImageResolution: imageResolution}), nil
	}
	runtime.Revision = application.DeployedRevision
	runtime.ArgoApplication = application.Name
	githubCommitSHA := ""
	var resolvedCommit *change.Commit
	if s.cfg.GitHub != nil && change.ValidCommitSHA(registryMetadata.Revision) {
		commit, commitErr := s.cfg.GitHub.GetCommit(ctx, mapping.Repository, registryMetadata.Revision)
		if commitErr == nil {
			githubCommitSHA = commit.SHA
			resolvedCommit = &commit
		} else {
			degraded = true
			unknowns = append(unknowns, "github OCI commit unavailable")
		}
	}
	imageResolution := s.resolveImage(ctx, change.ImageResolutionInput{RuntimeDigest: runtime.ImageDigest, ImageTag: runtimeTag, RegistryMetadata: registryMetadata, RegistryErrorCode: registryErrorCode, ArgoDeployedRevision: application.DeployedRevision, GitHubCommitSHA: githubCommitSHA, ExpectedSourceRepository: expectedSource, AllowedOCISources: s.cfg.AllowedOCISources})
	if imageResolution.Status != change.ImageConfirmed {
		degraded = degraded || imageResolution.Degraded || imageResolution.Status == change.ImageUnknown
	}
	history := boundedHistory(application.History, incident.FirstSeenAt, s.cfg.Lookback, s.cfg.MaxCandidates)
	if len(history) == 0 && application.DeployedRevision != "" && application.LastSyncedAt != nil {
		history = []change.ArgoHistory{{Revision: application.DeployedRevision, DeployedAt: *application.LastSyncedAt, SourceRepo: application.Repository, SourcePath: application.Path}}
	}
	candidates := make([]change.Change, 0, len(history))
	for _, deployment := range history {
		candidate, candidateUnknowns := s.buildCandidate(ctx, incident, mapping, application, deployment, runtime, imageResolution, resolvedCommit)
		unknowns = append(unknowns, candidateUnknowns...)
		if candidate.Degraded {
			degraded = true
		}
		candidates = append(candidates, candidate)
	}
	correlation := change.Correlate(runtime, candidates, s.cfg.Lookback, s.cfg.Now())
	byID := map[string]change.CorrelationCandidate{}
	for _, candidate := range correlation.Candidates {
		byID[candidate.ChangeID] = candidate
	}
	for index := range candidates {
		score := byID[candidates[index].PublicID]
		candidates[index].CorrelationScore = score.Score
		candidates[index].CorrelationReasons = score.Reasons
		candidates[index].Category = score.Category
		if score.Excluded {
			candidates[index].Status = change.StatusExcluded
		} else {
			candidates[index].Status = change.StatusMatched
		}
		if err := candidates[index].Validate(); err != nil {
			return Context{}, err
		}
		var persistErr error
		if writer, ok := s.cfg.Changes.(atomicEvidenceWriter); ok {
			facts, _ := json.Marshal(map[string]any{"change_id": candidates[index].PublicID, "source_type": candidates[index].SourceType, "status": candidates[index].Status, "category": candidates[index].Category, "commit_sha": candidates[index].CommitSHA, "image_digest": candidates[index].ImageDigest, "deployed_revision": candidates[index].ArgoCDDeployedRevision, "correlation_score": candidates[index].CorrelationScore, "correlation_reasons": candidates[index].CorrelationReasons, "degraded": candidates[index].Degraded, "image_resolution": imageResolution})
			evidence := &domain.EvidenceItem{PublicID: uuid.NewString(), IncidentID: candidates[index].IncidentID, Type: "change", Source: string(candidates[index].SourceType), ResourceRef: candidates[index].PublicID, TimeRange: json.RawMessage(`{}`), Query: "deterministic change correlation", Summary: change.BoundUTF8(candidates[index].ChangeSummary, 4096), Facts: facts, Truncated: candidates[index].Truncated, CollectedAt: s.cfg.Now().UTC()}
			_, persistErr = writer.PersistWithEvidence(ctx, &candidates[index], evidence)
		} else {
			_, persistErr = s.cfg.Changes.CreateIfAbsent(ctx, &candidates[index])
		}
		if persistErr != nil {
			return Context{}, persistErr
		}
		if s.cfg.Observer != nil {
			s.cfg.Observer.ObserveChangeCandidate(string(candidates[index].SourceType))
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].CorrelationScore > candidates[j].CorrelationScore })
	status := "ok"
	if len(candidates) == 0 {
		status = "no_data"
		unknowns = append(unknowns, "no deployment history within lookback")
	}
	correlation.Degraded = degraded
	correlation.Unknowns = dedupe(append(correlation.Unknowns, unknowns...))
	return s.finish(started, Context{Enabled: true, Status: status, CurrentRuntime: runtime, Candidates: candidates, Correlation: correlation, Unknowns: correlation.Unknowns, Degraded: degraded, ImageResolution: imageResolution}), nil
}

func (s *Service) buildCandidate(ctx context.Context, incident *domain.Incident, mapping ServiceMapping, app change.ArgoApplication, deployment change.ArgoHistory, runtime change.RuntimeContext, resolution change.ImageResolution, resolvedCommit *change.Commit) (change.Change, []string) {
	item, _ := change.New(incident.ID, change.SourceArgoCD, incident.PublicID, app.Name, deployment.Revision, deployment.DeployedAt.UTC().Format(time.RFC3339Nano))
	item.Repository = mapping.Repository.FullName()
	item.RepositoryOwner = mapping.Repository.Owner
	item.ArgoCDApplication = app.Name
	item.ArgoCDProject = app.Project
	item.ArgoCDTargetRevision = app.TargetRevision
	item.ArgoCDDeployedRevision = deployment.Revision
	item.Environment = incident.Environment
	item.Cluster = incident.Cluster
	item.Namespace = incident.Namespace
	item.ServiceName = incident.ServiceName
	item.WorkloadKind = incident.TargetKind
	item.WorkloadName = incident.TargetName
	item.GitOpsPath = mapping.GitOpsPath
	deployed := deployment.DeployedAt.UTC()
	item.DeployedAt = &deployed
	item.ImageRepository, item.ImageTag = splitImage(runtime.Image)
	if exactRevision(runtime.Revision, deployment.Revision) && resolution.Status == change.ImageConfirmed {
		item.ImageDigest = runtime.ImageDigest
		item.ImageRevision = resolution.Revision
	}
	item.ChangeSummary = "Argo CD deployed revision " + deployment.Revision
	item.Degraded = app.Degraded
	metadata := map[string]any{"argocd_sync_status": app.SyncStatus, "argocd_health_status": app.HealthStatus, "operation_phase": app.OperationPhase, "resource_count": len(app.Resources), "argocd_result_hash": app.ResultHash, "image_resolution": resolution}
	unknowns := []string{}
	if s.cfg.GitHub != nil && commitSHA.MatchString(deployment.Revision) {
		githubCtx, githubSpan := otel.Tracer("server-web/internal/service/changeintelligence").Start(ctx, "change.github.query")
		commit := change.Commit{}
		var commitErr error
		if resolvedCommit != nil && exactRevision(resolvedCommit.SHA, deployment.Revision) {
			commit = *resolvedCommit
		} else {
			commit, commitErr = s.cfg.GitHub.GetCommit(githubCtx, mapping.Repository, deployment.Revision)
		}
		if commitErr != nil {
			item.Degraded = true
			unknowns = append(unknowns, "github commit unavailable")
		} else {
			item.CommitSHA = commit.SHA
			item.BaseCommitSHA = firstParent(commit.Parents)
			item.ChangeSummary = change.BoundUTF8(commit.Message, 4096)
			metadata["commit_url"] = commit.HTMLURL
			prs, prErr := s.cfg.GitHub.ListPullRequestsForCommit(githubCtx, mapping.Repository, commit.SHA)
			if prErr == nil {
				for _, pr := range prs {
					if pr.Merged && exactRevision(pr.MergeCommitSHA, commit.SHA) {
						item.PullRequestNumber = pr.Number
						metadata["pull_request_url"] = pr.HTMLURL
						break
					}
				}
			}
			diff, diffErr := s.cfg.GitHub.GetCommitDiff(githubCtx, mapping.Repository, commit.SHA)
			if diffErr == nil {
				metadata["changed_file_count"] = diff.TotalFiles
				metadata["diff_result_hash"] = diff.ResultHash
				metadata["diff_truncated"] = diff.Truncated
				metadata["redacted_paths"] = len(diff.Redactions)
				item.Truncated = diff.Truncated
				item.RiskSummary = riskSummary(diff)
			}
			ci, ciErr := s.cfg.GitHub.GetCIStatus(githubCtx, mapping.Repository, commit.SHA)
			if ciErr == nil {
				item.WorkflowConclusion = ci.Conclusion
				if len(ci.WorkflowRuns) > 0 {
					item.WorkflowRunID = ci.WorkflowRuns[0].ID
					item.WorkflowName = ci.WorkflowRuns[0].Name
				}
				metadata["workflow_conclusion"] = ci.Conclusion
			} else {
				unknowns = append(unknowns, "ci status unavailable")
			}
		}
		githubSpan.SetAttributes(attribute.String("result", resultName(commitErr)), attribute.Bool("degraded", item.Degraded))
		githubSpan.End()
	} else {
		unknowns = append(unknowns, "github adapter disabled or deployed revision is not a commit SHA")
	}
	raw, _ := json.Marshal(metadata)
	sanitized, _, truncated := change.RedactJSON(raw, change.MaxMetadataBytes)
	item.Metadata = sanitized
	item.Truncated = item.Truncated || truncated
	item.Degraded = item.Degraded || len(unknowns) > 0
	return *item, unknowns
}

func (s *Service) mappingFor(incident *domain.Incident) (ServiceMapping, bool) {
	for _, key := range []string{strings.ToLower(incident.Environment + "/" + incident.Namespace + "/" + incident.ServiceName), strings.ToLower(incident.Namespace + "/" + incident.ServiceName), strings.ToLower(incident.ServiceName)} {
		if mapping, ok := s.cfg.Mappings[key]; ok {
			return mapping, true
		}
	}
	return ServiceMapping{}, false
}

func (s *Service) resolveImage(ctx context.Context, input change.ImageResolutionInput) change.ImageResolution {
	_, span := otel.Tracer("server-web/internal/service/changeintelligence").Start(ctx, "change.image.resolve")
	result := change.ResolveImageRevision(input)
	span.SetAttributes(attribute.String("image.resolution.status", string(result.Status)), attribute.Bool("image.resolution.degraded", result.Degraded), attribute.Bool("image.resolution.truncated", result.Truncated))
	span.End()
	if observer, ok := s.cfg.Observer.(interface{ ObserveImageResolution(string) }); ok {
		observer.ObserveImageResolution(string(result.Status))
	}
	if observer, ok := s.cfg.Observer.(interface{ ObserveImageResolutionConflict(string) }); ok {
		for _, reason := range result.Reasons {
			switch reason {
			case change.ReasonDigestConflict:
				observer.ObserveImageResolutionConflict("digest")
			case change.ReasonRevisionConflict:
				observer.ObserveImageResolutionConflict("revision")
			case change.ReasonSourceConflict:
				observer.ObserveImageResolutionConflict("source")
			}
		}
	}
	return result
}

func (s *Service) finish(started time.Time, result Context) Context {
	now := s.cfg.Now().UTC()
	result.RefreshedAt = &now
	if s.cfg.Observer != nil {
		s.cfg.Observer.ObserveChangeCorrelation(result.Status, now.Sub(started).Seconds())
	}
	return result
}
func runtimeFromIncident(item *domain.Incident) change.RuntimeContext {
	return change.RuntimeContext{IncidentPublicID: item.PublicID, FirstSeenAt: item.FirstSeenAt.UTC(), Cluster: item.Cluster, Environment: item.Environment, Namespace: item.Namespace, ServiceName: item.ServiceName, WorkloadKind: item.TargetKind, WorkloadName: item.TargetName}
}
func boundedHistory(items []change.ArgoHistory, incidentAt time.Time, lookback time.Duration, limit int) []change.ArgoHistory {
	result := []change.ArgoHistory{}
	start := incidentAt.Add(-lookback)
	for _, item := range items {
		at := item.DeployedAt.UTC()
		if at.After(incidentAt) || at.Before(start) {
			continue
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeployedAt.After(result[j].DeployedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
func riskSummary(diff change.DiffSummary) string {
	categories := []string{}
	for _, file := range diff.Files {
		name := strings.ToLower(file.Filename)
		switch {
		case file.Redacted:
			categories = append(categories, "sensitive_path_redacted")
		case strings.Contains(name, "deploy") || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"):
			categories = append(categories, "deployment_configuration")
		case strings.Contains(name, "config"):
			categories = append(categories, "application_configuration")
		}
	}
	categories = dedupe(categories)
	if len(categories) == 0 {
		return "no bounded high-risk category identified"
	}
	return strings.Join(categories, ",")
}
func resultName(err error) string {
	if err == nil {
		return "success"
	}
	switch {
	case errors.Is(err, change.ErrNotAllowed):
		return "not_allowed"
	case errors.Is(err, change.ErrPermission):
		return "permission"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "unavailable"
	}
}
func firstParent(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func exactRevision(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) && strings.TrimSpace(left) != ""
}

func selectRuntimeContainer(containers []change.ContainerRuntime, configuredName string) (change.ContainerRuntime, bool) {
	configuredName = strings.TrimSpace(configuredName)
	if configuredName == "" {
		if len(containers) != 1 {
			return change.ContainerRuntime{}, false
		}
		return containers[0], true
	}
	for _, container := range containers {
		if container.ContainerName == configuredName {
			return container, true
		}
	}
	return change.ContainerRuntime{}, false
}

func registryRepository(image string, allowedHosts []string) (string, string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", "", fmt.Errorf("%w: runtime image is empty", change.ErrInvalidArgument)
	}
	withoutDigest := image
	if at := strings.IndexByte(withoutDigest, '@'); at >= 0 {
		withoutDigest = withoutDigest[:at]
	}
	repository, tag := splitImage(withoutDigest)
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("%w: runtime image repository is not qualified", change.ErrInvalidArgument)
	}
	host := strings.ToLower(parts[0])
	hasExplicitHost := strings.ContainsAny(host, ".:") || host == "localhost"
	if hasExplicitHost {
		allowed := false
		for _, candidate := range allowedHosts {
			if strings.EqualFold(strings.TrimSpace(candidate), host) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", "", fmt.Errorf("%w: runtime image registry host", change.ErrNotAllowed)
		}
		repository = strings.Join(parts[1:], "/")
	}
	repository = strings.ToLower(strings.Trim(repository, "/"))
	if repository == "" || strings.Contains(repository, "..") {
		return "", "", fmt.Errorf("%w: runtime image repository", change.ErrInvalidArgument)
	}
	return repository, tag, nil
}

func registryCode(err error) string {
	var coded interface{ RegistryErrorCode() string }
	if errors.As(err, &coded) {
		return coded.RegistryErrorCode()
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "unavailable"
}
func splitImage(image string) (string, string) {
	at := strings.Index(image, "@")
	if at >= 0 {
		return image[:at], ""
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		return image[:colon], image[colon+1:]
	}
	return image, ""
}
func joinImage(repository, tag string) string {
	if tag == "" {
		return repository
	}
	return repository + ":" + tag
}
func dedupe(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
