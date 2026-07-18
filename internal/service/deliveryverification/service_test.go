package deliveryverification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type fakeGitHub struct {
	pr    verification.PullRequestObservation
	ci    verification.CIObservation
	prErr error
	ciErr error
}

func (f fakeGitHub) ObservePullRequest(context.Context, string, int64) (verification.PullRequestObservation, error) {
	return f.pr, f.prErr
}
func (f fakeGitHub) ObserveCI(context.Context, string, string) (verification.CIObservation, error) {
	return f.ci, f.ciErr
}

type fakeArgo struct {
	observation verification.ArgoObservation
	err         error
}

func (f fakeArgo) ObserveApplication(context.Context, string, string) (verification.ArgoObservation, error) {
	return f.observation, f.err
}

type fakeRollout struct {
	observation verification.RolloutObservation
	err         error
}

func (f fakeRollout) ObserveDeployment(context.Context, string, string, string) (verification.RolloutObservation, error) {
	return f.observation, f.err
}

type fakeAlerts struct {
	resolved bool
	err      error
}

type fakeSignals struct {
	result verification.SignalResult
	err    error
}

func (f fakeSignals) ObserveMetric(context.Context, verification.SignalQuery) (verification.SignalResult, error) {
	return f.result, f.err
}
func (f fakeSignals) ObserveLogErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error) {
	return f.result, f.err
}
func (f fakeSignals) ObserveTraceErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error) {
	return f.result, f.err
}

func (f fakeAlerts) ResolvedSignal(context.Context, uint64, string, time.Time) (bool, time.Time, error) {
	return f.resolved, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), f.err
}

func baseDelivery(status string) *verification.Delivery {
	return &verification.Delivery{ID: 1, PublicID: "delivery", IncidentID: 1, IncidentPublicID: "incident", IncidentFingerprint: strings.Repeat("f", 64), ServiceName: "api", Repository: "acme/gitops", PRNumber: 42, HeadCommitSHA: strings.Repeat("a", 40), Status: status, Cluster: "staging", Environment: "staging", Namespace: "payments", WorkloadKind: "Deployment", WorkloadName: "api"}
}

func newObservationService(now time.Time, github fakeGitHub, argo fakeArgo, rollout fakeRollout) *Service {
	return &Service{cfg: Config{PollInterval: 10 * time.Second, Now: func() time.Time { return now }, GitHub: github, ArgoCD: argo, Rollout: rollout, Alerts: fakeAlerts{}, Mappings: map[string]Mapping{"api": {ArgoApplication: "payments", ArgoProject: "staging"}}}}
}

func TestDeliveryGitHubTransitions(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		github     fakeGitHub
		wantStatus string
		wantReason string
	}{
		"pending":  {fakeGitHub{pr: verification.PullRequestObservation{State: "open", HeadSHA: strings.Repeat("a", 40)}, ci: verification.CIObservation{Conclusion: "in_progress"}}, "ci_pending", ""},
		"passed":   {fakeGitHub{pr: verification.PullRequestObservation{State: "open", HeadSHA: strings.Repeat("a", 40)}, ci: verification.CIObservation{Conclusion: "success"}}, "ci_passed", ""},
		"failed":   {fakeGitHub{pr: verification.PullRequestObservation{State: "open", HeadSHA: strings.Repeat("a", 40)}, ci: verification.CIObservation{Conclusion: "failure"}}, "ci_failed", "required_ci_failed"},
		"closed":   {fakeGitHub{pr: verification.PullRequestObservation{State: "closed", HeadSHA: strings.Repeat("a", 40)}}, "pr_closed", "pr_closed_without_merge"},
		"mismatch": {fakeGitHub{pr: verification.PullRequestObservation{State: "open", HeadSHA: strings.Repeat("b", 40)}}, "revision_mismatch", "pull_request_head_mismatch"},
	} {
		t.Run(name, func(t *testing.T) {
			service := newObservationService(now, tc.github, fakeArgo{}, fakeRollout{})
			update, err := service.observeDelivery(context.Background(), baseDelivery("pr_created"))
			if err != nil || update.Status != tc.wantStatus || update.FailureReason != tc.wantReason {
				t.Fatalf("status=%s reason=%s err=%v", update.Status, update.FailureReason, err)
			}
		})
	}
}

func TestMergedCommitAndArgoRevisionAreExact(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	merged := strings.Repeat("c", 40)
	service := newObservationService(now, fakeGitHub{pr: verification.PullRequestObservation{State: "closed", Merged: true, MergeCommitSHA: merged}}, fakeArgo{}, fakeRollout{})
	update, err := service.observeDelivery(context.Background(), baseDelivery("merge_pending"))
	if err != nil || update.Status != "merged" || update.TargetRevision != merged || update.MergedCommitSHA != merged {
		t.Fatalf("merge binding failed: %+v err=%v", update, err)
	}
	delivery := baseDelivery("argocd_pending")
	delivery.MergedCommitSHA, delivery.TargetRevision = merged, merged
	service.cfg.ArgoCD = fakeArgo{observation: verification.ArgoObservation{TargetRevision: strings.Repeat("d", 40), DeployedRevision: merged, SyncStatus: "Synced", OperationPhase: "Succeeded", HealthStatus: "Healthy"}}
	update, err = service.observeDelivery(context.Background(), delivery)
	if err != nil || update.Status != "revision_mismatch" || update.FailureReason != "argocd_target_revision_mismatch" {
		t.Fatalf("target mismatch must fail closed: %+v err=%v", update, err)
	}
	service.cfg.ArgoCD = fakeArgo{observation: verification.ArgoObservation{TargetRevision: merged, DeployedRevision: merged, SyncStatus: "Synced", OperationPhase: "Succeeded", HealthStatus: "Healthy"}}
	update, err = service.observeDelivery(context.Background(), delivery)
	if err != nil || update.Status != "synced" || update.DetectedRevision != merged {
		t.Fatalf("exact revision should sync: %+v err=%v", update, err)
	}
}

func TestRolloutMustMeetEveryBoundedCondition(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	delivery := baseDelivery("rollout_pending")
	ready := verification.RolloutObservation{Generation: 7, ObservedGeneration: 7, DesiredReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3, Progressing: true, Available: true, PodsReady: 3, PodsTotal: 3}
	service := newObservationService(now, fakeGitHub{}, fakeArgo{}, fakeRollout{observation: ready})
	update, err := service.observeDelivery(context.Background(), delivery)
	if err != nil || update.Status != "delivered" {
		t.Fatalf("ready rollout should deliver: %+v err=%v", update, err)
	}
	ready.PodsReady = 2
	service.cfg.Rollout = fakeRollout{observation: ready}
	update, err = service.observeDelivery(context.Background(), delivery)
	if err != nil || update.Status != "rollout_pending" {
		t.Fatalf("partial pod readiness must remain pending: %+v err=%v", update, err)
	}
}

func TestVerificationProviderUnavailableAndRevisionMismatchNeverPass(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	revision := strings.Repeat("a", 40)
	run := &verification.Run{IncidentID: 1, TargetRevision: revision}
	check := &verification.Check{Type: verification.CheckArgoRevision, Subject: verification.Subject{ArgoApplication: "payments", ArgoProject: "staging"}}
	service := newObservationService(now, fakeGitHub{}, fakeArgo{err: errors.New("temporary")}, fakeRollout{})
	sample := service.executeCheck(context.Background(), run, check, now)
	if sample.Status != verification.SampleUnavailable {
		t.Fatalf("provider error must be unavailable: %+v", sample)
	}
	service.cfg.ArgoCD = fakeArgo{observation: verification.ArgoObservation{DeployedRevision: strings.Repeat("b", 40)}}
	sample = service.executeCheck(context.Background(), run, check, now)
	if sample.Status != verification.SampleFailed || sample.ReasonCode != "revision_mismatch" {
		t.Fatalf("revision mismatch must fail: %+v", sample)
	}
}

func TestObservabilityChecksUseDeterministicEvaluator(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	check := &verification.Check{Type: verification.CheckMetricErrorRateBelow, Comparison: verification.CompareLTE, Threshold: .01, Lookback: 5 * time.Minute, PollInterval: 10 * time.Second, Subject: verification.Subject{Service: "api", Namespace: "payments", Environment: "staging"}}
	result := verification.SignalResult{Observation: verification.Observation{Status: verification.ObservationAvailable, Value: .01, SampleCount: 1, SampledAt: now, SourceReference: "prometheus://trusted"}}
	service := newObservationService(now, fakeGitHub{}, fakeArgo{}, fakeRollout{})
	service.cfg.Metrics = fakeSignals{result: result}
	sample := service.executeCheck(context.Background(), &verification.Run{}, check, now)
	if sample.Status != verification.SamplePassed || sample.SourceReference != "prometheus://trusted" {
		t.Fatalf("threshold evaluator=%+v", sample)
	}
	service.cfg.Metrics = fakeSignals{err: errors.New("secret token must not pass")}
	sample = service.executeCheck(context.Background(), &verification.Run{}, check, now)
	if sample.Status != verification.SampleUnavailable || strings.Contains(string(sample.Observed), "secret") {
		t.Fatalf("provider error leaked or passed=%+v", sample)
	}
	check.Type, check.Comparison = verification.CheckLogErrorAbsent, verification.CompareAbsent
	service.cfg.Logs = fakeSignals{result: verification.SignalResult{Observation: verification.Observation{Status: verification.ObservationNoData}}}
	if sample = service.executeCheck(context.Background(), &verification.Run{}, check, now); sample.Status != verification.SamplePassed {
		t.Fatalf("explicit absence no-data=%+v", sample)
	}
}
