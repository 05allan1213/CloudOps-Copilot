package baseline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type ArgoReader interface {
	GetApplication(context.Context, string, string) (change.ArgoApplication, error)
}

type RuntimeReader interface {
	ResolveRuntime(context.Context, string, string, string) ([]change.ContainerRuntime, error)
}

type RegistryReader interface {
	ReadMetadata(context.Context, string, string) (change.RegistryMetadata, error)
}

type GitFileReader interface {
	GetFileContent(context.Context, change.RepositoryRef, string, string) (change.FileContent, error)
}

type SourceCommitReader interface {
	GetCommit(context.Context, change.RepositoryRef, string) (change.Commit, error)
}

type RolloutReader interface {
	ObserveDeployment(context.Context, string, string, string) (verification.RolloutObservation, error)
}

type PrometheusReader interface {
	ObserveV3(context.Context, observabilityread.V3MetricQuery) (verification.Observation, error)
}

type LogReader interface {
	ObserveLogErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error)
}

type TraceReader interface {
	ObserveTraceErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error)
}

type VerifierConfig struct {
	Target                Target
	Service               string
	ArgoApplication       string
	ArgoProject           string
	ArgoPath              string
	ArgoDestinationServer string
	SourceRepository      change.RepositoryRef
	AllowedOCISources     []string
	AlertNames            []string
	Lookback              time.Duration
	Now                   func() time.Time

	Argo       ArgoReader
	Runtime    RuntimeReader
	Registry   RegistryReader
	Git        GitFileReader
	SourceGit  SourceCommitReader
	Rollout    RolloutReader
	Prometheus PrometheusReader
	Logs       LogReader
	Traces     TraceReader
	Store      Store
}

type Verifier struct {
	cfg VerifierConfig
}

func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	cfg.Target = cfg.Target.Normalized()
	if err := cfg.Target.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Service) == "" || strings.TrimSpace(cfg.ArgoApplication) == "" || strings.TrimSpace(cfg.ArgoProject) == "" ||
		strings.TrimSpace(cfg.ArgoDestinationServer) == "" ||
		!validRelativePath(cfg.ArgoPath) {
		return nil, fmt.Errorf("%w: baseline provider target is incomplete", change.ErrInvalidArgument)
	}
	if _, err := repositoryRef(cfg.SourceRepository.FullName()); err != nil || len(cfg.AllowedOCISources) == 0 {
		return nil, fmt.Errorf("%w: baseline source identity is incomplete", change.ErrInvalidArgument)
	}
	if len(cfg.AlertNames) == 0 || len(cfg.AlertNames) > 20 {
		return nil, fmt.Errorf("%w: baseline alert allowlist is required", change.ErrInvalidArgument)
	}
	if cfg.Lookback < 30*time.Minute || cfg.Lookback > 24*time.Hour {
		return nil, fmt.Errorf("%w: baseline lookback must be 30m-24h", change.ErrInvalidArgument)
	}
	for name, provider := range map[string]any{
		"Argo": cfg.Argo, "Runtime": cfg.Runtime, "Registry": cfg.Registry, "Git": cfg.Git, "SourceGit": cfg.SourceGit,
		"Rollout": cfg.Rollout, "Prometheus": cfg.Prometheus, "Logs": cfg.Logs, "Traces": cfg.Traces, "Store": cfg.Store,
	} {
		if provider == nil {
			return nil, fmt.Errorf("%w: baseline provider %s is required", change.ErrInvalidArgument, name)
		}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Verifier{cfg: cfg}, nil
}

func (v *Verifier) Verify(ctx context.Context) (ActivationResult, error) {
	if v == nil {
		return ActivationResult{}, errors.New("nil baseline verifier")
	}
	target := v.cfg.Target.Normalized()
	targetHash, err := target.IdentityHash()
	if err != nil {
		return ActivationResult{}, err
	}
	now := v.cfg.Now().UTC()
	if now.IsZero() {
		return ActivationResult{}, fmt.Errorf("%w: verifier clock", change.ErrInvalidArgument)
	}

	application, err := v.cfg.Argo.GetApplication(ctx, v.cfg.ArgoApplication, v.cfg.ArgoProject)
	if err != nil {
		return ActivationResult{}, unavailable("argocd", err)
	}
	if err := validateArgo(target, v.cfg, application); err != nil {
		return ActivationResult{}, err
	}
	gitopsRevision := strings.ToLower(strings.TrimSpace(application.DeployedRevision))
	if !change.ValidExactGitObjectID(gitopsRevision) {
		return ActivationResult{}, fmt.Errorf("%w: Argo deployed revision is not exact", change.ErrConflict)
	}

	runtimes, err := v.cfg.Runtime.ResolveRuntime(ctx, target.Namespace, "Deployment", target.WorkloadName)
	if err != nil {
		return ActivationResult{}, unavailable("kubernetes", err)
	}
	runtime, err := exactRuntime(runtimes, target.ContainerName)
	if err != nil {
		return ActivationResult{}, err
	}
	imageDigest := strings.ToLower(strings.TrimSpace(runtime.ImageDigest))
	if !digestPattern.MatchString(imageDigest) {
		return ActivationResult{}, fmt.Errorf("%w: runtime image digest is not immutable", change.ErrConflict)
	}
	repository, err := imageRepository(runtime.Image)
	if err != nil {
		return ActivationResult{}, err
	}
	metadata, err := v.cfg.Registry.ReadMetadata(ctx, repository, imageDigest)
	if err != nil {
		return ActivationResult{}, unavailable("registry", err)
	}
	sourceRevision := strings.ToLower(strings.TrimSpace(metadata.Revision))
	labels := change.ValidateOCILabels(map[string]string{
		"org.opencontainers.image.revision": metadata.Revision,
		"org.opencontainers.image.source":   metadata.Source,
		"org.opencontainers.image.version":  metadata.Version,
	}, v.cfg.AllowedOCISources)
	if !metadata.Valid || metadata.Integrity != change.RegistryIntegrityVerified || !labels.Valid ||
		metadata.Degraded || metadata.Truncated || !strings.EqualFold(metadata.Repository, repository) ||
		!strings.EqualFold(metadata.ManifestDigest, imageDigest) || !digestPattern.MatchString(strings.ToLower(metadata.ConfigDigest)) ||
		!change.ValidExactGitObjectID(sourceRevision) || !repositoryURLMatches(metadata.Source, v.cfg.SourceRepository) {
		return ActivationResult{}, fmt.Errorf("%w: registry identity is not verified", change.ErrConflict)
	}
	sourceCommit, err := v.cfg.SourceGit.GetCommit(ctx, v.cfg.SourceRepository, sourceRevision)
	if err != nil {
		return ActivationResult{}, unavailable("github-source", err)
	}
	if !strings.EqualFold(sourceCommit.Repository, v.cfg.SourceRepository.FullName()) ||
		!strings.EqualFold(sourceCommit.SHA, sourceRevision) ||
		!change.ValidExactGitObjectID(strings.ToLower(strings.TrimSpace(sourceCommit.TreeSHA))) {
		return ActivationResult{}, fmt.Errorf("%w: OCI source revision is not an exact GitHub commit", change.ErrConflict)
	}

	repositoryRef, err := repositoryRef(target.Repository)
	if err != nil {
		return ActivationResult{}, err
	}
	file, err := v.cfg.Git.GetFileContent(ctx, repositoryRef, gitopsRevision, target.TargetPath)
	if err != nil {
		return ActivationResult{}, unavailable("github", err)
	}
	if file.Revision != "" && !strings.EqualFold(file.Revision, gitopsRevision) {
		return ActivationResult{}, fmt.Errorf("%w: config blob revision mismatch", change.ErrConflict)
	}
	if !strings.EqualFold(file.Repository, target.Repository) || file.Path != target.TargetPath ||
		!change.ValidExactGitObjectID(strings.ToLower(strings.TrimSpace(file.BlobSHA))) ||
		len(file.Content) == 0 || len(file.Content) > 256*1024 {
		return ActivationResult{}, fmt.Errorf("%w: config blob is empty or oversized", change.ErrConflict)
	}
	configHash := hashBytes(file.Content)

	rollout, err := v.cfg.Rollout.ObserveDeployment(ctx, target.Cluster, target.Namespace, target.WorkloadName)
	if err != nil {
		return ActivationResult{}, unavailable("kubernetes", err)
	}
	if !rolloutHealthy(rollout) {
		return ActivationResult{}, fmt.Errorf("%w: Deployment is not fully ready", change.ErrConflict)
	}

	argoObservation := map[string]any{
		"application": v.cfg.ArgoApplication, "project": v.cfg.ArgoProject,
		"deployed_revision": gitopsRevision, "sync_status": application.SyncStatus,
		"health_status": application.HealthStatus, "operation_phase": application.OperationPhase,
		"result_hash": application.ResultHash,
	}
	kubernetesObservation := map[string]any{
		"generation": rollout.Generation, "observed_generation": rollout.ObservedGeneration,
		"desired_replicas": rollout.DesiredReplicas, "updated_replicas": rollout.UpdatedReplicas,
		"ready_replicas": rollout.ReadyReplicas, "available_replicas": rollout.AvailableReplicas,
		"unavailable_replicas": rollout.UnavailableReplicas, "pods_ready": rollout.PodsReady,
		"pods_total": rollout.PodsTotal, "progressing": rollout.Progressing, "available": rollout.Available,
		"image_digest": imageDigest, "source_revision": sourceRevision,
		"oci_source": metadata.Source, "oci_version": metadata.Version,
		"registry_result_hash": metadata.ResultHash, "source_commit_tree": sourceCommit.TreeSHA,
	}

	alerts, err := v.cfg.Prometheus.ObserveV3(ctx, observabilityread.V3MetricQuery{
		Kind: observabilityread.V3MetricFiringAlerts, Service: v.cfg.Service,
		Namespace: target.Namespace, Environment: target.Environment, Cluster: target.Cluster,
		WorkloadName: target.WorkloadName, AlertNames: append([]string(nil), v.cfg.AlertNames...),
		Lookback: v.cfg.Lookback,
	})
	if err != nil {
		return ActivationResult{}, unavailable("prometheus", err)
	}
	if !booleanHealthy(alerts) {
		return ActivationResult{}, fmt.Errorf("%w: baseline alerts are firing or unqueryable", change.ErrConflict)
	}

	errorRate, err := v.cfg.Prometheus.ObserveV3(ctx, observabilityread.V3MetricQuery{
		Kind: observabilityread.V3MetricErrorRate, Service: v.cfg.Service,
		Namespace: target.Namespace, Environment: target.Environment, WorkloadName: target.WorkloadName,
		Lookback: v.cfg.Lookback,
	})
	if err != nil {
		return ActivationResult{}, unavailable("prometheus", err)
	}
	availability, err := v.cfg.Prometheus.ObserveV3(ctx, observabilityread.V3MetricQuery{
		Kind: observabilityread.V3MetricAvailability, Service: v.cfg.Service,
		Namespace: target.Namespace, Environment: target.Environment, WorkloadName: target.WorkloadName,
		Lookback: v.cfg.Lookback,
	})
	if err != nil {
		return ActivationResult{}, unavailable("prometheus", err)
	}
	if !metricHealthy(errorRate, .01, 50, false) || !metricHealthy(availability, .99, 50, true) {
		return ActivationResult{}, fmt.Errorf("%w: baseline request metrics do not meet the Golden thresholds", change.ErrConflict)
	}

	signalQuery := verification.SignalQuery{
		Service: v.cfg.Service, Namespace: target.Namespace, Environment: target.Environment,
		Revision: sourceRevision, Lookback: v.cfg.Lookback, Step: 10 * time.Second,
		MaxSeries: 20, MaxSamples: 1000,
	}
	logs, err := v.cfg.Logs.ObserveLogErrorRate(ctx, signalQuery)
	if err != nil {
		return ActivationResult{}, unavailable("elasticsearch", err)
	}
	if !absenceHealthy(logs.Observation) {
		return ActivationResult{}, fmt.Errorf("%w: required-env error logs are present or unqueryable", change.ErrConflict)
	}
	signalQuery.Template = string(verification.CheckTraceErrorRateBelow)
	traces, err := v.cfg.Traces.ObserveTraceErrorRate(ctx, signalQuery)
	if err != nil {
		return ActivationResult{}, unavailable("tempo", err)
	}
	if !metricHealthy(traces.Observation, .01, 20, false) {
		return ActivationResult{}, fmt.Errorf("%w: baseline trace error rate exceeds the Golden threshold", change.ErrConflict)
	}

	configObservation := map[string]any{
		"revision": gitopsRevision, "path": target.TargetPath,
		"blob_sha": strings.ToLower(strings.TrimSpace(file.BlobSHA)), "bytes": len(file.Content),
		"content_hash": configHash,
	}
	configBlobObservation := observation(ObservationConfigBlob, "github/config-blob", configObservation, now)
	configBlobObservation.ContentHash = configHash
	snapshot := Snapshot{
		Target: target, TargetIdentityHash: targetHash, SourceRevision: sourceRevision,
		ImageDigest: imageDigest, GitOpsRevision: gitopsRevision, ConfigHash: configHash,
		VerificationPolicyVersion: VerificationPolicyVersion, VerifiedAt: now,
		Observations: []Observation{
			observation(ObservationArgoRevision, "argocd/application", argoObservation, now),
			observation(ObservationKubernetesReadiness, "kubernetes/deployment", kubernetesObservation, now),
			observationFromVerification(ObservationAlertState, "prometheus/firing-alerts", alerts, now),
			observationFromVerification(ObservationMetric, "prometheus/golden-metrics", map[string]any{
				"error_rate": errorRate, "availability": availability,
			}, now),
			observationFromVerification(ObservationLog, "elasticsearch/required-env", logs.Observation, now),
			observationFromVerification(ObservationTrace, "tempo/error-rate", traces.Observation, now),
			configBlobObservation,
		},
	}
	if err := snapshot.Finalize(); err != nil {
		return ActivationResult{}, err
	}
	return v.cfg.Store.Activate(ctx, snapshot)
}

func validateArgo(target Target, cfg VerifierConfig, application change.ArgoApplication) error {
	targetRepository, err := repositoryRef(target.Repository)
	if err != nil {
		return err
	}
	if !strings.EqualFold(application.Name, cfg.ArgoApplication) ||
		!strings.EqualFold(application.Project, cfg.ArgoProject) ||
		!repositoryURLMatches(application.Repository, targetRepository) ||
		application.Path != strings.Trim(strings.TrimSpace(cfg.ArgoPath), "/") ||
		application.DestinationServer != cfg.ArgoDestinationServer || application.Namespace != target.Namespace ||
		!strings.EqualFold(application.SyncStatus, "Synced") ||
		!strings.EqualFold(application.HealthStatus, "Healthy") ||
		!strings.EqualFold(application.OperationPhase, "Succeeded") ||
		application.Degraded || application.Truncated ||
		!change.ValidExactGitObjectID(strings.ToLower(strings.TrimSpace(application.DeployedRevision))) {
		return fmt.Errorf("%w: Argo application is not an exact healthy deployment", change.ErrConflict)
	}
	return nil
}

func rolloutHealthy(value verification.RolloutObservation) bool {
	return value.Generation > 0 && value.ObservedGeneration == value.Generation &&
		value.DesiredReplicas > 0 && value.UpdatedReplicas == value.DesiredReplicas &&
		value.ReadyReplicas == value.DesiredReplicas && value.AvailableReplicas == value.DesiredReplicas &&
		value.UnavailableReplicas == 0 && value.PodsReady == value.DesiredReplicas &&
		value.PodsTotal == value.DesiredReplicas && value.Progressing && value.Available &&
		!value.ProgressDeadlineExceeded
}

func booleanHealthy(value verification.Observation) bool {
	return value.Status == verification.ObservationAvailable && value.QueryValid &&
		value.SourceHealthy && value.RetentionCovered && !value.Truncated &&
		value.MatchedCount == 0 && value.Value > 0
}

func metricHealthy(value verification.Observation, threshold float64, minimum int, availability bool) bool {
	if value.Status != verification.ObservationAvailable || !value.QueryValid ||
		!value.SourceHealthy || !value.RetentionCovered || value.Truncated ||
		value.SampleCount < minimum {
		return false
	}
	if availability {
		return value.Value >= threshold
	}
	return value.Value < threshold
}

func absenceHealthy(value verification.Observation) bool {
	if value.Status == verification.ObservationNoData {
		return value.QueryValid && value.SourceHealthy && value.RetentionCovered && !value.Truncated
	}
	return value.Status == verification.ObservationAvailable && value.QueryValid &&
		value.SourceHealthy && value.RetentionCovered && !value.Truncated && value.MatchedCount == 0
}

func observation(typ ObservationType, source string, payload any, at time.Time) Observation {
	raw, _ := json.Marshal(payload)
	return Observation{Type: typ, SourceIdentity: source, ObservedJSON: raw, ObservedAt: at.UTC()}
}

func observationFromVerification(typ ObservationType, source string, value any, fallback time.Time) Observation {
	raw, _ := json.Marshal(value)
	at := fallback.UTC()
	if observation, ok := value.(verification.Observation); ok && !observation.SampledAt.IsZero() {
		at = observation.SampledAt.UTC()
	}
	return Observation{Type: typ, SourceIdentity: source, ObservedJSON: raw, ObservedAt: at}
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
		return change.ContainerRuntime{}, fmt.Errorf("%w: runtime container identity is ambiguous", change.ErrConflict)
	}
	return result, nil
}

func repositoryRef(value string) (change.RepositoryRef, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return change.RepositoryRef{}, fmt.Errorf("%w: repository must be owner/name", change.ErrInvalidArgument)
	}
	return change.RepositoryRef{Owner: parts[0], Name: parts[1]}, nil
}

func imageRepository(image string) (string, error) {
	image = strings.TrimSpace(image)
	if at := strings.LastIndex(image, "@"); at >= 0 {
		image = image[:at]
	}
	lastSlash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > lastSlash {
		image = image[:colon]
	}
	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("%w: runtime image repository", change.ErrInvalidArgument)
	}
	return strings.Join(parts[1:], "/"), nil
}

func repositoryURLMatches(raw string, repository change.RepositoryRef) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "github.com") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	path := strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/")
	return strings.EqualFold(path, repository.FullName())
}

func validRelativePath(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), "/")
	return value != "" && value != "." && !strings.Contains(value, "..")
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func unavailable(provider string, err error) error {
	return fmt.Errorf("%w: baseline %s unavailable: %v", change.ErrUnavailable, provider, err)
}
