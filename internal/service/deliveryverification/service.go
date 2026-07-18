package deliveryverification

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
	verificationapplication "github.com/05allan1213/CloudOps-Copilot/internal/verification/application"
)

var exactRevision = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

type Mapping struct {
	ArgoApplication string
	ArgoProject     string
}

type Observer interface {
	ObserveDeliveryObservation(provider, result string)
	ObserveDeliveryTransition(from, to string)
	ObserveDeliveryDuration(result string, seconds float64)
	ObserveVerificationRun(status string, seconds float64)
	ObserveVerificationCheck(checkType, status string)
	ObserveVerificationLeaseTakeover()
	ObserveIncidentAfterVerification(result string)
	ObserveVerificationProvider(provider, result string, seconds float64)
	ObserveVerificationStabilityReset(checkType string)
	ObservePostmortem(result string)
}

type Config struct {
	DeliveryEnabled      bool
	VerificationEnabled  bool
	DeliveryWorkerID     string
	VerificationWorkerID string
	PollInterval         time.Duration
	DeliveryTimeout      time.Duration
	VerificationTimeout  time.Duration
	StabilityWindow      time.Duration
	LeaseDuration        time.Duration
	MaxAttempts          int
	Repository           verification.Repository
	GitHub               verification.GitHubReader
	ArgoCD               verification.ArgoReader
	Rollout              verification.RolloutReader
	Alerts               verification.AlertReader
	Metrics              verification.MetricReader
	Logs                 verification.LogReader
	Traces               verification.TraceReader
	Profiles             verification.Profiles
	Mappings             map[string]Mapping
	Observer             Observer
	Now                  func() time.Time
}

type Service struct {
	*verificationapplication.Service
	cfg Config
}

func New(cfg Config) (*Service, error) {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.PollInterval < time.Second || cfg.PollInterval > time.Minute || cfg.DeliveryTimeout < time.Minute || cfg.DeliveryTimeout > 24*time.Hour || cfg.VerificationTimeout < time.Minute || cfg.VerificationTimeout > 24*time.Hour || cfg.StabilityWindow < cfg.PollInterval || cfg.StabilityWindow > cfg.VerificationTimeout || cfg.LeaseDuration <= cfg.PollInterval || cfg.MaxAttempts < 1 || cfg.MaxAttempts > 10000 || strings.TrimSpace(cfg.DeliveryWorkerID) == "" || strings.TrimSpace(cfg.VerificationWorkerID) == "" {
		return nil, verification.ErrInvalidArgument
	}
	if (cfg.DeliveryEnabled || cfg.VerificationEnabled) && cfg.Repository == nil {
		return nil, verification.ErrInvalidArgument
	}
	if cfg.DeliveryEnabled && (cfg.GitHub == nil || cfg.ArgoCD == nil || cfg.Rollout == nil || len(cfg.Mappings) == 0) {
		return nil, verification.ErrInvalidArgument
	}
	if cfg.VerificationEnabled && (cfg.ArgoCD == nil || cfg.Rollout == nil || cfg.Alerts == nil) {
		return nil, verification.ErrInvalidArgument
	}
	if err := cfg.Profiles.Validate(); err != nil {
		return nil, err
	}
	if len(cfg.Profiles.Items) > 0 && (cfg.Metrics == nil || cfg.Logs == nil || cfg.Traces == nil) {
		return nil, verification.ErrInvalidArgument
	}
	application, err := verificationapplication.New(verificationapplication.Config{
		DeliveryEnabled:     cfg.DeliveryEnabled,
		VerificationEnabled: cfg.VerificationEnabled,
		Repository:          cfg.Repository,
	})
	if err != nil {
		return nil, err
	}
	return &Service{Service: application, cfg: cfg}, nil
}

func (s *Service) ObserveNext(ctx context.Context) (bool, error) {
	if !s.DeliveryEnabled() {
		return false, nil
	}
	delivery, err := s.cfg.Repository.ClaimDelivery(ctx, s.cfg.DeliveryWorkerID, s.cfg.Now(), s.cfg.LeaseDuration, s.cfg.DeliveryTimeout)
	if errors.Is(err, verification.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "delivery.claim")
	span.SetAttributes(attribute.String("delivery.status", delivery.Status))
	defer span.End()
	leaseCtx, finishHeartbeat := s.startLeaseHeartbeat(ctx, func(at time.Time) error {
		return s.cfg.Repository.HeartbeatDelivery(context.WithoutCancel(ctx), delivery.ID, delivery.RowVersion, delivery.LeaseOwner, at, s.cfg.LeaseDuration)
	})
	update, observeErr := s.observeDelivery(leaseCtx, delivery)
	heartbeatErr := finishHeartbeat()
	if observeErr == nil && heartbeatErr != nil {
		observeErr = heartbeatErr
	}
	if observeErr != nil {
		_ = s.cfg.Repository.ReleaseDelivery(context.WithoutCancel(ctx), delivery, s.cfg.Now().Add(s.cfg.PollInterval), providerReason(observeErr))
		return true, observeErr
	}
	_, persistSpan := otel.Tracer("server-web/deliveryverification").Start(ctx, "delivery.persist_transition")
	err = s.cfg.Repository.PersistDelivery(ctx, delivery, update)
	persistSpan.End()
	if err != nil {
		return true, err
	}
	if s.cfg.Observer != nil && update.Status != delivery.Status {
		s.cfg.Observer.ObserveDeliveryTransition(delivery.Status, update.Status)
		if verification.TerminalDelivery(update.Status) {
			s.cfg.Observer.ObserveDeliveryDuration(update.Status, s.cfg.Now().Sub(*delivery.DeliveryStartedAt).Seconds())
		}
	}
	return true, nil
}

func (s *Service) observeDelivery(ctx context.Context, d *verification.Delivery) (verification.DeliveryUpdate, error) {
	now := s.cfg.Now().UTC()
	mapping, ok := s.deliveryMapping(d)
	if !ok {
		return verification.DeliveryUpdate{}, verification.ErrNotAllowed
	}
	if d.WorkloadKind != "Deployment" || strings.TrimSpace(d.Cluster) == "" || strings.TrimSpace(d.Namespace) == "" || strings.TrimSpace(d.WorkloadName) == "" {
		return verification.DeliveryUpdate{}, verification.ErrNotAllowed
	}
	u := verification.DeliveryUpdate{Status: d.Status, CIStatus: d.CIStatus, PRState: d.PRState, MergedCommitSHA: d.MergedCommitSHA, TargetRevision: d.TargetRevision, DetectedRevision: d.DetectedRevision, ArgoSyncStatus: d.ArgoSyncStatus, ArgoOperationPhase: d.ArgoOperationPhase, ArgoHealthStatus: d.ArgoHealthStatus, ResourceHealth: d.ResourceHealth, SyncStartedAt: d.SyncStartedAt, SyncCompletedAt: d.SyncCompletedAt, DeploymentGeneration: d.DeploymentGeneration, ObservedGeneration: d.ObservedGeneration, RolloutRevision: d.RolloutRevision, DesiredReplicas: d.DesiredReplicas, UpdatedReplicas: d.UpdatedReplicas, AvailableReplicas: d.AvailableReplicas, UnavailableReplicas: d.UnavailableReplicas, NextPollAt: now.Add(s.cfg.PollInterval), ObservedAt: now, ArgoApplication: mapping.ArgoApplication, ArgoProject: mapping.ArgoProject, Cluster: d.Cluster, Environment: d.Environment, Namespace: d.Namespace, WorkloadKind: d.WorkloadKind, WorkloadName: d.WorkloadName}
	if d.DeliveryDeadlineAt != nil && !now.Before(*d.DeliveryDeadlineAt) {
		switch d.Status {
		case "pr_created", "ci_pending", "ci_passed", "merge_pending":
			u.Status, u.FailureReason = "merge_timeout", "merge_timeout"
		case "merged", "argocd_pending", "syncing":
			u.Status, u.FailureReason = "argocd_timeout", "argocd_timeout"
		default:
			u.Status, u.FailureReason = "rollout_failed", "rollout_timeout"
		}
		return u, nil
	}
	switch d.Status {
	case "pr_created", "ci_pending":
		ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "delivery.github_observe")
		pr, err := s.cfg.GitHub.ObservePullRequest(ctx, d.Repository, d.PRNumber)
		span.End()
		if err != nil {
			s.observeProvider("github", "unavailable")
			return u, err
		}
		s.observeProvider("github", "success")
		if !strings.EqualFold(pr.HeadSHA, d.HeadCommitSHA) {
			u.Status, u.FailureReason = "revision_mismatch", "pull_request_head_mismatch"
			return u, nil
		}
		u.PRState = pr.State
		if pr.State == "closed" && !pr.Merged {
			u.Status, u.FailureReason = "pr_closed", "pr_closed_without_merge"
			return u, nil
		}
		ci, err := s.cfg.GitHub.ObserveCI(ctx, d.Repository, d.HeadCommitSHA)
		if err != nil {
			s.observeProvider("github", "unavailable")
			return u, err
		}
		s.observeProvider("github", "success")
		switch ci.Conclusion {
		case "success":
			u.Status, u.CIStatus = "ci_passed", "passing"
		case "failure", "cancelled":
			u.Status, u.CIStatus, u.FailureReason = "ci_failed", "failing", "required_ci_failed"
			if ci.Conclusion == "cancelled" {
				u.CIStatus, u.FailureReason = "cancelled", "required_ci_cancelled"
			}
		default:
			u.Status, u.CIStatus = "ci_pending", "pending"
		}
	case "ci_passed":
		u.Status = "merge_pending"
	case "merge_pending":
		pr, err := s.cfg.GitHub.ObservePullRequest(ctx, d.Repository, d.PRNumber)
		if err != nil {
			s.observeProvider("github", "unavailable")
			return u, err
		}
		s.observeProvider("github", "success")
		u.PRState = pr.State
		if pr.Merged {
			sha := strings.ToLower(pr.MergeCommitSHA)
			if !exactRevision.MatchString(sha) {
				u.Status, u.FailureReason = "revision_mismatch", "merged_commit_invalid"
			} else {
				u.Status, u.MergedCommitSHA, u.TargetRevision = "merged", sha, sha
			}
		} else if pr.State == "closed" {
			u.Status, u.FailureReason = "pr_closed", "pr_closed_without_merge"
		}
	case "merged":
		if !exactRevision.MatchString(d.MergedCommitSHA) {
			u.Status, u.FailureReason = "revision_mismatch", "merged_commit_unbound"
		} else {
			u.Status, u.TargetRevision = "argocd_pending", strings.ToLower(d.MergedCommitSHA)
		}
	case "argocd_pending", "syncing":
		ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "delivery.argocd_observe")
		app, err := s.cfg.ArgoCD.ObserveApplication(ctx, mapping.ArgoApplication, mapping.ArgoProject)
		span.End()
		if err != nil {
			s.observeProvider("argocd", "unavailable")
			return u, err
		}
		s.observeProvider("argocd", "success")
		u.DetectedRevision, u.ArgoSyncStatus, u.ArgoOperationPhase, u.ArgoHealthStatus, u.ResourceHealth = app.DeployedRevision, app.SyncStatus, app.OperationPhase, app.HealthStatus, app.ResourceHealth
		if app.OperationPhase == "Failed" || app.OperationPhase == "Error" {
			u.Status, u.FailureReason = "argocd_failed", "argocd_operation_failed"
			return u, nil
		}
		if exactRevision.MatchString(strings.ToLower(app.TargetRevision)) && !strings.EqualFold(app.TargetRevision, d.TargetRevision) {
			u.Status, u.FailureReason = "revision_mismatch", "argocd_target_revision_mismatch"
			return u, nil
		}
		if !strings.EqualFold(app.DeployedRevision, d.TargetRevision) {
			u.Status = "argocd_pending"
			return u, nil
		}
		if app.SyncStatus == "Synced" && app.OperationPhase == "Succeeded" {
			u.Status, u.SyncCompletedAt = "synced", app.LastSyncedAt
		} else {
			u.Status = "syncing"
			if u.SyncStartedAt == nil {
				u.SyncStartedAt = &now
			}
		}
	case "synced":
		if !strings.EqualFold(d.DetectedRevision, d.TargetRevision) {
			u.Status, u.FailureReason = "revision_mismatch", "argocd_deployed_revision_mismatch"
		} else {
			u.Status = "rollout_pending"
		}
	case "rollout_pending":
		ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "delivery.kubernetes_observe")
		rollout, err := s.cfg.Rollout.ObserveDeployment(ctx, d.Cluster, d.Namespace, d.WorkloadName)
		span.End()
		if err != nil {
			s.observeProvider("kubernetes", "unavailable")
			return u, err
		}
		s.observeProvider("kubernetes", "success")
		u.DeploymentGeneration, u.ObservedGeneration, u.RolloutRevision = rollout.Generation, rollout.ObservedGeneration, rollout.RolloutRevision
		u.DesiredReplicas, u.UpdatedReplicas, u.AvailableReplicas, u.UnavailableReplicas = rollout.DesiredReplicas, rollout.UpdatedReplicas, rollout.AvailableReplicas, rollout.UnavailableReplicas
		if rollout.ProgressDeadlineExceeded {
			u.Status, u.FailureReason = "rollout_failed", "progress_deadline_exceeded"
			return u, nil
		}
		if rollout.ObservedGeneration >= rollout.Generation && rollout.UpdatedReplicas == rollout.DesiredReplicas && rollout.AvailableReplicas == rollout.DesiredReplicas && rollout.UnavailableReplicas == 0 && rollout.Progressing && rollout.Available && rollout.PodsReady == rollout.PodsTotal {
			u.Status = "delivered"
		}
	}
	return u, nil
}

func (s *Service) VerifyNext(ctx context.Context) (bool, error) {
	if !s.VerificationEnabled() {
		return false, nil
	}
	if _, err := s.ensureVerificationRun(ctx); err != nil && !errors.Is(err, verification.ErrNotFound) {
		return false, err
	}
	run, err := s.cfg.Repository.ClaimRun(ctx, s.cfg.VerificationWorkerID, s.cfg.Now(), s.cfg.LeaseDuration)
	if errors.Is(err, verification.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.claim")
	defer span.End()
	if run.LeaseTakeover && s.cfg.Observer != nil {
		s.cfg.Observer.ObserveVerificationLeaseTakeover()
	}
	now := s.cfg.Now().UTC()
	if !now.Before(run.DeadlineAt) {
		err := s.cfg.Repository.TimeoutRun(ctx, run, now)
		_, outcomeSpan := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.return_to_investigation")
		outcomeSpan.End()
		if err == nil && s.cfg.Observer != nil {
			s.cfg.Observer.ObserveVerificationRun(string(verification.RunTimedOut), now.Sub(*run.StartedAt).Seconds())
			s.cfg.Observer.ObserveIncidentAfterVerification("returned_to_investigation")
		}
		return true, err
	}
	checks, err := s.cfg.Repository.ListChecks(ctx, run.ID)
	if err != nil {
		return true, err
	}
	if status, _, terminal := verification.Aggregate(checks); terminal && status != verification.RunRunning {
		_, aggregateSpan := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.aggregate")
		result, aggregateErr := s.cfg.Repository.AggregateRun(ctx, run, now)
		aggregateSpan.End()
		if aggregateErr == nil {
			spanName := "verification.return_to_investigation"
			if result.Status == verification.RunPassed {
				spanName = "verification.resolve_incident"
			}
			_, outcomeSpan := otel.Tracer("server-web/deliveryverification").Start(ctx, spanName)
			outcomeSpan.End()
			s.observeRunResult(result, now)
			if result.Status == verification.RunPassed && s.cfg.Observer != nil {
				s.cfg.Observer.ObservePostmortem("generated")
			}
		} else if s.cfg.Observer != nil {
			s.cfg.Observer.ObservePostmortem("failed")
		}
		err = aggregateErr
		return true, err
	}
	var selected *verification.Check
	for i := range checks {
		if verification.TerminalCheck(checks[i].Status) {
			continue
		}
		if checks[i].LastCheckedAt == nil || !now.Before(checks[i].LastCheckedAt.Add(checks[i].PollInterval)) {
			selected = &checks[i]
			break
		}
	}
	if selected == nil {
		return true, s.cfg.Repository.ReleaseRun(ctx, run, now)
	}
	if selected.AttemptCount >= s.cfg.MaxAttempts {
		sample := verification.Sample{Status: verification.SampleFailed, Observed: json.RawMessage(`{"bounded":true}`), ReasonCode: "max_attempts_exceeded"}
		if err := s.cfg.Repository.PersistCheckSample(ctx, run, selected, sample, now, now); err != nil {
			return true, err
		}
		return true, nil
	}
	if selected.FirstCheckedAt != nil && !now.Before(selected.FirstCheckedAt.Add(selected.Timeout)) {
		sample := verification.Sample{Status: verification.SampleTimedOut, Observed: json.RawMessage(`{"bounded":true}`), ReasonCode: "check_timeout"}
		if err := s.cfg.Repository.PersistCheckSample(ctx, run, selected, sample, now, now); err != nil {
			return true, err
		}
		return true, nil
	}
	leaseCtx, finishHeartbeat := s.startLeaseHeartbeat(ctx, func(at time.Time) error {
		return s.cfg.Repository.HeartbeatRun(context.WithoutCancel(ctx), run.ID, run.RowVersion, run.LeaseOwner, at, s.cfg.LeaseDuration)
	})
	providerStarted := time.Now()
	sample := s.executeCheck(leaseCtx, run, selected, now)
	if s.cfg.Observer != nil && verificationProvider(selected.Type) != "" {
		s.cfg.Observer.ObserveVerificationProvider(verificationProvider(selected.Type), string(sample.Status), time.Since(providerStarted).Seconds())
		if selected.ConsecutiveSuccessSince != nil && sample.Status != verification.SamplePassed {
			s.cfg.Observer.ObserveVerificationStabilityReset(string(selected.Type))
		}
	}
	if heartbeatErr := finishHeartbeat(); heartbeatErr != nil {
		return true, heartbeatErr
	}
	_, persistSpan := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.persist_check")
	err = s.cfg.Repository.PersistCheckSample(ctx, run, selected, sample, now, now.Add(selected.PollInterval))
	persistSpan.End()
	if err != nil {
		return true, err
	}
	if s.cfg.Observer != nil {
		s.cfg.Observer.ObserveVerificationCheck(string(selected.Type), string(selected.Status))
	}
	// PersistCheckSample releases the lease. A subsequent claim performs the
	// aggregate transaction, which makes crash recovery between check persistence
	// and Incident transition explicit and replay-safe.
	return true, nil
}

func verificationProvider(t verification.CheckType) string {
	switch t {
	case verification.CheckMetricErrorRateBelow, verification.CheckMetricAvailabilityAbove, verification.CheckMetricLatencyP95Below:
		return "prometheus"
	case verification.CheckLogErrorAbsent, verification.CheckLogErrorRateBelow:
		return "loki"
	case verification.CheckTraceErrorRateBelow, verification.CheckTraceLatencyP95Below:
		return "tempo"
	default:
		return ""
	}
}

func (s *Service) ensureVerificationRun(ctx context.Context) (*verification.Run, error) {
	d, err := s.cfg.Repository.FindDeliveredWithoutRun(ctx)
	if err != nil {
		return nil, err
	}
	subject := verification.Subject{Repository: d.Repository, PullRequest: d.PRNumber, Revision: d.TargetRevision, ArgoApplication: d.ArgoApplication, ArgoProject: d.ArgoProject, Cluster: d.Cluster, Environment: d.Environment, Namespace: d.Namespace, Service: d.ServiceName, WorkloadKind: d.WorkloadKind, WorkloadName: d.WorkloadName, AlertFingerprint: d.IncidentFingerprint}
	ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.compile_plan")
	var profile *verification.Profile
	if len(s.cfg.Profiles.Items) > 0 {
		profile, err = s.cfg.Profiles.Match(subject)
		if err != nil {
			span.End()
			return nil, err
		}
	}
	plan, err := verification.CompileTrustedPlanWithProfile(subject, verification.CompilerConfig{PollInterval: s.cfg.PollInterval, Timeout: s.cfg.VerificationTimeout, StabilityWindow: s.cfg.StabilityWindow, AlertLookback: s.cfg.VerificationTimeout}, profile)
	span.End()
	if err != nil {
		return nil, err
	}
	return s.cfg.Repository.CreateRun(ctx, d, plan, s.cfg.Now())
}

func (s *Service) executeCheck(ctx context.Context, run *verification.Run, check *verification.Check, now time.Time) verification.Sample {
	ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.execute_check")
	span.SetAttributes(attribute.String("verification.check_type", string(check.Type)))
	defer span.End()
	sample := verification.Sample{Status: verification.SamplePending, Observed: json.RawMessage(`{}`)}
	switch check.Type {
	case verification.CheckArgoRevision, verification.CheckArgoSync, verification.CheckArgoHealth:
		app, err := s.cfg.ArgoCD.ObserveApplication(ctx, check.Subject.ArgoApplication, check.Subject.ArgoProject)
		if err != nil {
			return unavailableSample("argocd_unavailable")
		}
		observed, _ := json.Marshal(map[string]any{"deployed_revision": app.DeployedRevision, "sync_status": app.SyncStatus, "operation_phase": app.OperationPhase, "health_status": app.HealthStatus})
		sample.Observed, sample.SourceReference = observed, "argocd:"+check.Subject.ArgoApplication
		switch check.Type {
		case verification.CheckArgoRevision:
			if !strings.EqualFold(app.DeployedRevision, run.TargetRevision) {
				sample.Status, sample.ReasonCode = verification.SampleFailed, "revision_mismatch"
			} else {
				sample.Status = verification.SamplePassed
			}
		case verification.CheckArgoSync:
			if app.OperationPhase == "Failed" || app.OperationPhase == "Error" {
				sample.Status, sample.ReasonCode = verification.SampleFailed, "argocd_operation_failed"
			} else if app.SyncStatus == "Synced" && app.OperationPhase == "Succeeded" {
				sample.Status = verification.SamplePassed
			}
		case verification.CheckArgoHealth:
			var resources []map[string]any
			_ = json.Unmarshal(app.ResourceHealth, &resources)
			healthy := app.HealthStatus == "Healthy" && len(resources) > 0
			for _, resource := range resources {
				if resource["health"] != "Healthy" {
					healthy = false
				}
			}
			if healthy {
				sample.Status = verification.SamplePassed
			}
		}
	case verification.CheckDeploymentRollout, verification.CheckWorkloadReady:
		rollout, err := s.cfg.Rollout.ObserveDeployment(ctx, check.Subject.Cluster, check.Subject.Namespace, check.Subject.WorkloadName)
		if err != nil {
			return unavailableSample("kubernetes_unavailable")
		}
		observed, _ := json.Marshal(map[string]any{"generation": rollout.Generation, "observed_generation": rollout.ObservedGeneration, "desired": rollout.DesiredReplicas, "updated": rollout.UpdatedReplicas, "available": rollout.AvailableReplicas, "unavailable": rollout.UnavailableReplicas, "pods_ready": rollout.PodsReady, "pods_total": rollout.PodsTotal})
		sample.Observed, sample.SourceReference = observed, "kubernetes:"+check.Subject.Namespace+"/Deployment/"+check.Subject.WorkloadName
		if rollout.ProgressDeadlineExceeded {
			sample.Status, sample.ReasonCode = verification.SampleFailed, "progress_deadline_exceeded"
			return sample
		}
		if check.Type == verification.CheckDeploymentRollout {
			if rollout.ObservedGeneration >= rollout.Generation && rollout.UpdatedReplicas == rollout.DesiredReplicas && rollout.UnavailableReplicas == 0 && rollout.Progressing {
				sample.Status = verification.SamplePassed
			}
		} else if rollout.AvailableReplicas == rollout.DesiredReplicas && rollout.PodsReady == rollout.PodsTotal && rollout.Available {
			sample.Status = verification.SamplePassed
		}
	case verification.CheckAlertResolved:
		resolved, occurredAt, err := s.cfg.Alerts.ResolvedSignal(ctx, run.IncidentID, check.Subject.AlertFingerprint, now.Add(-check.Lookback))
		if err != nil {
			return unavailableSample("incident_signal_unavailable")
		}
		observed, _ := json.Marshal(map[string]any{"resolved": resolved, "fingerprint": check.Subject.AlertFingerprint, "occurred_at": occurredAt})
		sample.Observed, sample.SourceReference = observed, "incident_signal:"+check.Subject.AlertFingerprint
		if resolved {
			sample.Status = verification.SamplePassed
		}
	case verification.CheckMetricErrorRateBelow, verification.CheckMetricAvailabilityAbove, verification.CheckMetricLatencyP95Below,
		verification.CheckLogErrorAbsent, verification.CheckLogErrorRateBelow,
		verification.CheckTraceErrorRateBelow, verification.CheckTraceLatencyP95Below:
		query := verification.SignalQuery{Template: string(check.Type), Service: check.Subject.Service, Namespace: check.Subject.Namespace, Environment: check.Subject.Environment, Revision: check.Subject.Revision, Lookback: check.Lookback, Step: check.PollInterval, MaxSeries: 20, MaxSamples: 1000}
		var result verification.SignalResult
		var err error
		switch check.Type {
		case verification.CheckMetricErrorRateBelow, verification.CheckMetricAvailabilityAbove, verification.CheckMetricLatencyP95Below:
			result, err = s.cfg.Metrics.ObserveMetric(ctx, query)
		case verification.CheckLogErrorAbsent, verification.CheckLogErrorRateBelow:
			result, err = s.cfg.Logs.ObserveLogErrorRate(ctx, query)
		default:
			result, err = s.cfg.Traces.ObserveTraceErrorRate(ctx, query)
		}
		if err != nil && result.Observation.Status == "" {
			result.Observation = verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_unavailable"}
		}
		_, evaluateSpan := otel.Tracer("server-web/deliveryverification").Start(ctx, "verification.evaluate_check")
		sample = verification.EvaluateObservation(*check, result.Observation, now)
		evaluateSpan.End()
	default:
		sample.Status, sample.ReasonCode = verification.SampleInvalid, "unsupported_check_type"
	}
	return sample
}

type Worker struct {
	service *Service
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (s *Service) NewWorker() *Worker {
	return &Worker{service: s, stop: make(chan struct{}), done: make(chan struct{})}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(w.service.cfg.PollInterval)
		defer ticker.Stop()
		for {
			_, _ = w.service.ObserveNext(ctx)
			_, _ = w.service.VerifyNext(ctx)
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *Worker) Stop() { w.once.Do(func() { close(w.stop) }); <-w.done }

func (s *Service) observeProvider(provider, result string) {
	if s.cfg.Observer != nil {
		s.cfg.Observer.ObserveDeliveryObservation(provider, result)
	}
}

func (s *Service) observeRunResult(run *verification.Run, now time.Time) {
	if s.cfg.Observer == nil || run == nil {
		return
	}
	duration := float64(0)
	if run.StartedAt != nil {
		duration = now.Sub(*run.StartedAt).Seconds()
	}
	s.cfg.Observer.ObserveVerificationRun(string(run.Status), duration)
	if run.Status == verification.RunPassed {
		s.cfg.Observer.ObserveIncidentAfterVerification("resolved")
	} else {
		s.cfg.Observer.ObserveIncidentAfterVerification("returned_to_investigation")
	}
}
func unavailableSample(reason string) verification.Sample {
	return verification.Sample{Status: verification.SampleUnavailable, Observed: json.RawMessage(`{"available":false}`), ReasonCode: reason}
}

func (s *Service) deliveryMapping(d *verification.Delivery) (Mapping, bool) {
	keys := []string{
		strings.ToLower(strings.TrimSpace(d.Environment) + "/" + strings.TrimSpace(d.Namespace) + "/" + strings.TrimSpace(d.ServiceName)),
		strings.ToLower(strings.TrimSpace(d.Namespace) + "/" + strings.TrimSpace(d.ServiceName)),
		strings.ToLower(strings.TrimSpace(d.ServiceName)),
	}
	for _, key := range keys {
		if mapping, ok := s.cfg.Mappings[key]; ok {
			return mapping, true
		}
	}
	return Mapping{}, false
}

func (s *Service) startLeaseHeartbeat(ctx context.Context, heartbeat func(time.Time) error) (context.Context, func() error) {
	leaseCtx, cancel := context.WithCancel(ctx)
	stop := make(chan struct{})
	result := make(chan error, 1)
	interval := s.cfg.LeaseDuration / 3
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				result <- nil
				return
			case <-leaseCtx.Done():
				result <- leaseCtx.Err()
				return
			case <-ticker.C:
				if err := heartbeat(s.cfg.Now().UTC()); err != nil {
					cancel()
					result <- err
					return
				}
			}
		}
	}()
	var once sync.Once
	return leaseCtx, func() error {
		once.Do(func() { close(stop) })
		err := <-result
		cancel()
		return err
	}
}
func providerReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "provider_unavailable"
}
