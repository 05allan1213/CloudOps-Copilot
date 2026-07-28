package verificationread

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type prometheusReader interface {
	ObserveBoundedMetric(context.Context, observabilityread.MetricQuery) (verification.Observation, error)
}

type logReader interface {
	ObserveLogErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error)
}

type traceReader interface {
	ObserveTraceErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error)
}

type Target struct {
	Cluster         string
	Environment     string
	Namespace       string
	Service         string
	Workload        string
	Container       string
	ArgoApplication string
	ArgoProject     string
	ReadyURL        string
}

type Config struct {
	DB            *sql.DB
	Prometheus    prometheusReader
	Elasticsearch logReader
	Tempo         traceReader
	Argo          change.ArgoCDReader
	Rollout       verification.RolloutReader
	Runtime       change.RuntimeReader
	Registry      change.RegistryMetadataReader
	Target        Target
	HTTPClient    *http.Client
	Now           func() time.Time
}

// Source performs exactly one fixed read bundle for one frozen verification check. It
// has no generic query-language or mutable-endpoint surface.
type Source struct {
	cfg      Config
	readyURL *url.URL
}

var _ taskhandler.VerificationObservationSource = (*Source)(nil)

func New(config Config) (*Source, error) {
	if config.DB == nil || config.Prometheus == nil || config.Elasticsearch == nil || config.Tempo == nil ||
		config.Argo == nil || config.Rollout == nil || config.Runtime == nil || config.Registry == nil {
		return nil, errors.New("verification source requires MySQL and every bounded read provider")
	}
	for _, value := range []string{config.Target.Cluster, config.Target.Environment, config.Target.Namespace, config.Target.Service,
		config.Target.Workload, config.Target.Container, config.Target.ArgoApplication, config.Target.ArgoProject, config.Target.ReadyURL} {
		if strings.TrimSpace(value) == "" {
			return nil, errors.New("verification target identity is incomplete")
		}
	}
	readyURL, err := url.Parse(config.Target.ReadyURL)
	if err != nil || readyURL.Host == "" || readyURL.User != nil || readyURL.RawQuery != "" || readyURL.Fragment != "" ||
		(readyURL.Scheme != "http" && readyURL.Scheme != "https") || readyURL.Path != "/readyz" {
		return nil, errors.New("verification readyz endpoint must be fixed")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Source{cfg: config, readyURL: readyURL}, nil
}

func (s *Source) Observe(ctx context.Context, run verification.Run, check verification.Check) (verification.Observation, error) {
	if s == nil || run.ID == 0 || run.IncidentID == 0 || check.VerificationRunID != run.ID || check.ProfileID != run.Plan.ProfileID ||
		check.Subject.Cluster != s.cfg.Target.Cluster || check.Subject.Environment != s.cfg.Target.Environment ||
		check.Subject.Namespace != s.cfg.Target.Namespace || check.Subject.Service != s.cfg.Target.Service ||
		check.Subject.WorkloadName != s.cfg.Target.Workload || !strings.EqualFold(check.Subject.WorkloadKind, "Deployment") ||
		check.Subject.ArgoApplication != s.cfg.Target.ArgoApplication || check.Subject.ArgoProject != s.cfg.Target.ArgoProject {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	switch check.Type {
	case verification.CheckArgoExactRevision:
		return s.observeArgo(ctx, run, true)
	case verification.CheckArgoSyncSucceeded:
		return s.observeArgo(ctx, run, false)
	case verification.CheckDeploymentObserved:
		return s.observeRollout(ctx, check, rolloutObserved)
	case verification.CheckDeploymentRolloutComplete:
		return s.observeRollout(ctx, check, rolloutComplete)
	case verification.CheckWorkloadReady:
		return s.observeWorkloadReady(ctx, check)
	case verification.CheckIncidentAlertsResolved:
		return s.observeAlerts(ctx, run, check)
	case verification.CheckMetricErrorRateBelow:
		return s.observeMetric(ctx, check, observabilityread.MetricErrorRate)
	case verification.CheckMetricAvailabilityAbove:
		return s.observeMetric(ctx, check, observabilityread.MetricAvailability)
	case verification.CheckLogRequiredEnvAbsent:
		return s.observeLogs(ctx, check)
	case verification.CheckTraceErrorRateBelow:
		return s.observeTraces(ctx, check)
	case verification.CheckDeploymentIdentity:
		return s.observeIdentity(ctx, run, check)
	default:
		return verification.Observation{}, verification.ErrInvalidArgument
	}
}

func (s *Source) observeArgo(ctx context.Context, run verification.Run, exact bool) (verification.Observation, error) {
	application, err := s.cfg.Argo.GetApplication(ctx, s.cfg.Target.ArgoApplication, s.cfg.Target.ArgoProject)
	if err != nil {
		return unavailable("argocd_read", err), nil
	}
	latest := latestHistory(application)
	matched := strings.EqualFold(application.DeployedRevision, run.TargetRevision)
	if exact {
		matched = matched && strings.EqualFold(latest, run.TargetRevision)
	} else {
		matched = matched && strings.EqualFold(application.SyncStatus, "Synced") && strings.EqualFold(application.OperationPhase, "Succeeded")
	}
	return booleanObservation(matched, s.cfg.Now(), "argocd://"+s.cfg.Target.ArgoApplication), nil
}

type rolloutPredicate func(verification.RolloutObservation) bool

func rolloutObserved(value verification.RolloutObservation) bool {
	return value.Generation > 0 && value.ObservedGeneration == value.Generation
}
func rolloutComplete(value verification.RolloutObservation) bool {
	return rolloutObserved(value) && value.DesiredReplicas == 2 && value.UpdatedReplicas == 2 && value.ReadyReplicas == 2 &&
		value.AvailableReplicas == 2 && value.UnavailableReplicas == 0 && value.PodsReady == 2 && value.PodsTotal == 2 &&
		!value.ProgressDeadlineExceeded
}

func (s *Source) observeRollout(ctx context.Context, check verification.Check, predicate rolloutPredicate) (verification.Observation, error) {
	rollout, err := s.cfg.Rollout.ObserveDeployment(ctx, check.Subject.Cluster, check.Subject.Namespace, check.Subject.WorkloadName)
	if err != nil {
		return unavailable("kubernetes_read", err), nil
	}
	observation := booleanObservation(predicate(rollout), s.cfg.Now(), "kubernetes://deployment/"+check.Subject.Namespace+"/"+check.Subject.WorkloadName)
	observation.SeriesCount = int(rollout.PodsReady)
	return observation, nil
}

func (s *Source) observeWorkloadReady(ctx context.Context, check verification.Check) (verification.Observation, error) {
	rollout, err := s.cfg.Rollout.ObserveDeployment(ctx, check.Subject.Cluster, check.Subject.Namespace, check.Subject.WorkloadName)
	if err != nil {
		return unavailable("kubernetes_read", err), nil
	}
	readyz := false
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, s.readyURL.String(), nil)
	if reqErr == nil {
		resp, readErr := s.cfg.HTTPClient.Do(req)
		if readErr == nil {
			readBytes, copyErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 4097))
			closeErr := resp.Body.Close()
			readyz = copyErr == nil && closeErr == nil && readBytes <= 4096 && resp.StatusCode == http.StatusOK
		}
	}
	matched := rollout.PodsReady == 2 && rollout.PodsTotal == 2 && readyz
	observation := booleanObservation(matched, s.cfg.Now(), "kubernetes://readyz/"+check.Subject.Namespace+"/"+check.Subject.WorkloadName)
	observation.SeriesCount = int(rollout.PodsReady)
	return observation, nil
}

func (s *Source) observeMetric(ctx context.Context, check verification.Check, kind observabilityread.MetricKind) (verification.Observation, error) {
	observation, err := s.cfg.Prometheus.ObserveBoundedMetric(ctx, observabilityread.MetricQuery{
		Kind: kind, Service: check.Subject.Service, Namespace: check.Subject.Namespace,
		Environment: s.cfg.Target.Environment, WorkloadName: check.Subject.WorkloadName, Lookback: check.Lookback,
	})
	if err != nil {
		return unavailable("prometheus_read", err), nil
	}
	return observation, nil
}

func (s *Source) observeLogs(ctx context.Context, check verification.Check) (verification.Observation, error) {
	result, err := s.cfg.Elasticsearch.ObserveLogErrorRate(ctx, signalQuery(check))
	if err != nil {
		return unavailable("elasticsearch_read", err), nil
	}
	return result.Observation, nil
}

func (s *Source) observeTraces(ctx context.Context, check verification.Check) (verification.Observation, error) {
	result, err := s.cfg.Tempo.ObserveTraceErrorRate(ctx, signalQuery(check))
	if err != nil {
		return unavailable("tempo_read", err), nil
	}
	return result.Observation, nil
}

func signalQuery(check verification.Check) verification.SignalQuery {
	return verification.SignalQuery{
		Template: string(check.Type), Service: check.Subject.Service, Namespace: check.Subject.Namespace,
		Environment: check.Subject.Environment, Revision: check.Subject.Revision, Lookback: check.Lookback,
		Step: 10 * time.Second, MaxSeries: 20, MaxSamples: 1000,
	}
}

func (s *Source) observeAlerts(ctx context.Context, run verification.Run, check verification.Check) (verification.Observation, error) {
	var expected struct {
		AlertNames []string `json:"alert_names"`
	}
	if err := json.Unmarshal(check.Expected, &expected); err != nil || len(expected.AlertNames) == 0 || len(expected.AlertNames) > 20 {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	var cycle uint64
	if err := s.cfg.DB.QueryRowContext(ctx, `SELECT cycle_no FROM verification_runs WHERE id = ? AND incident_id = ?`, run.ID, run.IncidentID).Scan(&cycle); err != nil || cycle == 0 {
		return unavailable("incident_signal", err), nil
	}
	var unresolved int
	err := s.cfg.DB.QueryRowContext(ctx, `SELECT COUNT(*)
FROM incident_signals firing
WHERE firing.incident_id = ? AND firing.cycle_no = ? AND firing.status = 'firing'
  AND NOT EXISTS (
    SELECT 1 FROM incident_signals resolved
    WHERE resolved.incident_id = firing.incident_id AND resolved.cycle_no = firing.cycle_no
      AND resolved.alert_instance_key = firing.alert_instance_key
      AND resolved.status = 'resolved'
  )`, run.IncidentID, cycle).Scan(&unresolved)
	if err != nil {
		return unavailable("incident_signal", err), nil
	}
	prometheus, err := s.cfg.Prometheus.ObserveBoundedMetric(ctx, observabilityread.MetricQuery{
		Kind: observabilityread.MetricFiringAlerts, Service: check.Subject.Service, Namespace: check.Subject.Namespace,
		Environment: check.Subject.Environment, WorkloadName: check.Subject.WorkloadName, Cluster: check.Subject.Cluster,
		AlertNames: expected.AlertNames, Lookback: check.Lookback,
	})
	if err != nil {
		return unavailable("prometheus_read", err), nil
	}
	matched := unresolved == 0 && prometheus.MatchedCount == 0 && prometheus.Value != 0
	observation := booleanObservation(matched, s.cfg.Now(), "incident+prometheus://alerts/"+run.IncidentPublicID)
	observation.MatchedCount = unresolved + prometheus.MatchedCount
	return observation, nil
}

func (s *Source) observeIdentity(ctx context.Context, run verification.Run, check verification.Check) (verification.Observation, error) {
	application, err := s.cfg.Argo.GetApplication(ctx, s.cfg.Target.ArgoApplication, s.cfg.Target.ArgoProject)
	if err != nil {
		return unavailable("argocd_read", err), nil
	}
	runtimes, err := s.cfg.Runtime.ResolveRuntime(ctx, check.Subject.Namespace, check.Subject.WorkloadKind, check.Subject.WorkloadName)
	if err != nil {
		return unavailable("kubernetes_read", err), nil
	}
	var runtime change.ContainerRuntime
	count := 0
	for _, value := range runtimes {
		if value.ContainerName == s.cfg.Target.Container {
			runtime, count = value, count+1
		}
	}
	if count != 1 {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	repository, err := registryRepository(runtime.Image)
	if err != nil {
		return verification.Observation{}, err
	}
	metadata, err := s.cfg.Registry.ReadMetadata(ctx, repository, runtime.ImageDigest)
	if err != nil {
		return unavailable("registry_read", err), nil
	}
	matched := metadata.Valid && metadata.Integrity == change.RegistryIntegrityVerified && !metadata.Truncated && !metadata.Degraded &&
		strings.EqualFold(metadata.Revision, run.Plan.SourceRevision) && strings.EqualFold(runtime.ImageDigest, run.Plan.ImageDigest) &&
		strings.EqualFold(application.DeployedRevision, run.Plan.GitOpsRevision)
	return booleanObservation(matched, s.cfg.Now(), "argocd+kubernetes+registry://deployment-identity"), nil
}

func booleanObservation(matched bool, at time.Time, reference string) verification.Observation {
	value, misses := 1.0, 0
	if !matched {
		value, misses = 0, 1
	}
	return verification.Observation{
		Status: verification.ObservationAvailable, Value: value, MatchedCount: misses, SampleCount: 1,
		SampledAt: at.UTC(), QueryValid: true, SourceHealthy: true, RetentionCovered: true, SourceReference: reference,
	}
}

func unavailable(source string, err error) verification.Observation {
	reason := "provider_unavailable"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "provider_timeout"
	}
	return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: reason, SourceReference: source + "://unavailable"}
}

func latestHistory(application change.ArgoApplication) string {
	result := ""
	var latest time.Time
	for _, item := range application.History {
		if item.Revision != "" && (result == "" || item.DeployedAt.After(latest)) {
			result, latest = item.Revision, item.DeployedAt
		}
	}
	return result
}

func registryRepository(image string) (string, error) {
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
		return "", fmt.Errorf("%w: runtime image repository", verification.ErrInvalidArgument)
	}
	return strings.Join(parts[1:], "/"), nil
}
