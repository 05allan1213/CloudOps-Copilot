package verification

import (
	"strings"
	"testing"
	"time"
)

func TestProfilesAreFrozenDistinctAndLokiFree(t *testing.T) {
	input := validCompileInput("post_delivery")
	golden, err := CompilePlan(input)
	if err != nil {
		t.Fatal(err)
	}
	if golden.ProfileID != GoldenRequiredEnvProfileID || len(golden.Checks) != 10 || golden.Deadline != RunDeadline {
		t.Fatalf("golden plan=%+v", golden)
	}
	if err := ValidatePlan(golden); err != nil {
		t.Fatal(err)
	}
	for _, check := range golden.Checks {
		if strings.Contains(strings.ToLower(check.SourceIdentity), "loki") || check.StabilityWindow != CommonStabilityWindow || check.MinSamples <= 0 {
			t.Fatalf("unsafe check=%+v", check)
		}
	}
	second, err := CompilePlan(input)
	if err != nil || second.ProfileHash != golden.ProfileHash || len(second.Checks) != len(golden.Checks) {
		t.Fatalf("non-deterministic profile=%s/%s err=%v", golden.ProfileHash, second.ProfileHash, err)
	}

	noChangeInput := validCompileInput("no_change")
	noChange, err := CompilePlan(noChangeInput)
	if err != nil {
		t.Fatal(err)
	}
	if noChange.ProfileID != NoChangeProfileID || len(noChange.Checks) != 8 {
		t.Fatalf("no-change plan=%+v", noChange)
	}
	for _, check := range noChange.Checks {
		if check.Type == CheckArgoExactRevision || check.Type == CheckArgoSyncSucceeded {
			t.Fatal("no-change profile contains PR/sync-result checks")
		}
	}
	changed := GoldenRequiredEnvProfile()
	changed.Checks[6].Threshold = .02
	changedHash, err := ProfileHash(changed)
	if err != nil || changedHash == golden.ProfileHash {
		t.Fatal("profile hash did not change with threshold")
	}
}

func TestEvaluatorRequiresHealthAndMinimumSamples(t *testing.T) {
	plan, err := CompilePlan(validCompileInput("post_delivery"))
	if err != nil {
		t.Fatal(err)
	}
	metricSpec := plan.Checks[6]
	check := Check{Type: metricSpec.Type, Comparison: metricSpec.Comparison, Threshold: metricSpec.Threshold, Lookback: metricSpec.Lookback, MinSamples: metricSpec.MinSamples, SampleUnit: metricSpec.SampleUnit, FailureMode: metricSpec.FailureMode, InitialDelay: metricSpec.InitialDelay}
	now := time.Date(2026, 7, 19, 5, 0, 0, 0, time.UTC)
	base := Observation{Status: ObservationAvailable, Value: .001, Denominator: 49, SampleCount: 49, SampledAt: now, QueryValid: true, SourceHealthy: true, RetentionCovered: true}
	if got := EvaluateObservation(check, base, now); got.Status != SampleUnavailable || got.ReasonCode != "insufficient_samples" {
		t.Fatalf("under-sampled observation=%+v", got)
	}
	base.Denominator, base.SampleCount = 50, 50
	if got := EvaluateObservation(check, base, now); got.Status != SamplePassed {
		t.Fatalf("healthy metric=%+v", got)
	}
	base.SourceHealthy = false
	if got := EvaluateObservation(check, base, now); got.Status != SampleUnavailable {
		t.Fatalf("unhealthy source=%+v", got)
	}
	base.SourceHealthy, base.Status = true, ObservationNoData
	if got := EvaluateObservation(check, base, now); got.Status != SampleUnavailable {
		t.Fatalf("metric no-data=%+v", got)
	}

	logSpec := plan.Checks[8]
	logCheck := Check{Type: logSpec.Type, Comparison: logSpec.Comparison, Lookback: logSpec.Lookback, MinSamples: logSpec.MinSamples, SampleUnit: logSpec.SampleUnit, FailureMode: logSpec.FailureMode}
	absence := Observation{Status: ObservationNoData, QueryValid: true, SourceHealthy: true, RetentionCovered: true, SampledAt: now}
	if got := EvaluateObservation(logCheck, absence, now); got.Status != SamplePassed {
		t.Fatalf("healthy absence query=%+v", got)
	}
	absence.QueryValid = false
	if got := EvaluateObservation(logCheck, absence, now); got.Status != SampleUnavailable {
		t.Fatalf("invalid absence query=%+v", got)
	}
}

func TestEvaluatorHandlesEveryStructuralBooleanCheck(t *testing.T) {
	plan, err := CompilePlan(validCompileInput("post_delivery"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 20, 5, 0, 0, 0, time.UTC)
	want := map[CheckType]bool{
		CheckArgoExactRevision: true, CheckArgoSyncSucceeded: true,
		CheckDeploymentObserved: true, CheckDeploymentRolloutComplete: true,
		CheckWorkloadReady: true, CheckIncidentAlertsResolved: true,
	}
	for _, spec := range plan.Checks {
		if !want[spec.Type] {
			continue
		}
		check := Check{Type: spec.Type, Lookback: spec.Lookback, MinSamples: spec.MinSamples, SampleUnit: spec.SampleUnit, FailureMode: spec.FailureMode}
		observation := Observation{
			Status: ObservationAvailable, Value: 1, SampleCount: spec.MinSamples,
			SeriesCount: spec.MinSamples, SampledAt: now, QueryValid: true,
			SourceHealthy: true, RetentionCovered: true,
		}
		if got := EvaluateObservation(check, observation, now); got.Status != SamplePassed {
			t.Fatalf("healthy %s observation=%+v", spec.Type, got)
		}
		observation.Value = 0
		got := EvaluateObservation(check, observation, now)
		wantStatus := SamplePending
		if spec.FailureMode == FailureImmediate {
			wantStatus = SampleFailed
		}
		if got.Status != wantStatus {
			t.Fatalf("negative %s observation=%+v want=%s", spec.Type, got, wantStatus)
		}
		delete(want, spec.Type)
	}
	if len(want) != 0 {
		t.Fatalf("uncovered boolean checks=%v", want)
	}

	noChange, err := CompilePlan(validCompileInput("no_change"))
	if err != nil {
		t.Fatal(err)
	}
	identity := noChange.Checks[0]
	check := Check{Type: identity.Type, Lookback: identity.Lookback, MinSamples: identity.MinSamples, SampleUnit: identity.SampleUnit, FailureMode: identity.FailureMode}
	observation := Observation{Status: ObservationAvailable, Value: 1, SampleCount: 1, SampledAt: now, QueryValid: true, SourceHealthy: true, RetentionCovered: true}
	if got := EvaluateObservation(check, observation, now); got.Status != SamplePassed || got.ReasonCode != "identity_matches" {
		t.Fatalf("no-change identity observation=%+v", got)
	}
}

func TestCommonWindowUsesLatestSuccessStartAndInconclusiveUnavailable(t *testing.T) {
	start := time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC)
	first, second := start, start.Add(10*time.Second)
	checks := []Check{
		{Required: true, Status: CheckRunning, ConsecutiveSuccessSince: &first, LastCheckedAt: ptrTime(start.Add(69 * time.Second)), PollInterval: 10 * time.Second},
		{Required: true, Status: CheckRunning, ConsecutiveSuccessSince: &second, LastCheckedAt: ptrTime(start.Add(69 * time.Second)), PollInterval: 10 * time.Second},
	}
	status, reason, terminal, common := CommonWindowResult(checks, start.Add(69*time.Second), start.Add(5*time.Minute))
	if status != RunRunning || terminal || reason != "common_stability_window_pending" || common == nil || !common.Equal(second) {
		t.Fatalf("early common window=%s %s %v %v", status, reason, terminal, common)
	}
	status, reason, terminal, common = CommonWindowResult(checks, start.Add(70*time.Second), start.Add(5*time.Minute))
	if status != RunPassed || !terminal || reason != "all_required_checks_common_window_passed" || common == nil {
		t.Fatalf("passed common window=%s %s %v %v", status, reason, terminal, common)
	}
	checks[1].Status = CheckUnavailable
	status, reason, terminal, _ = CommonWindowResult(checks, start.Add(5*time.Minute), start.Add(5*time.Minute))
	if status != RunInconclusive || !terminal || reason != "required_check_unavailable" {
		t.Fatalf("unavailable deadline=%s %s %v", status, reason, terminal)
	}
	checks[1].Status = CheckRunning
	checks[1].ConsecutiveSuccessSince = nil
	checks[0].LastCheckedAt = ptrTime(start.Add(5 * time.Minute))
	checks[1].LastCheckedAt = ptrTime(start.Add(5 * time.Minute))
	status, _, terminal, _ = CommonWindowResult(checks, start.Add(5*time.Minute), start.Add(5*time.Minute))
	if status != RunTimedOut || !terminal {
		t.Fatalf("window deadline=%s terminal=%v", status, terminal)
	}
}

func TestImmediateFailureAndResetSemantics(t *testing.T) {
	plan, err := CompilePlan(validCompileInput("post_delivery"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 19, 7, 0, 0, 0, time.UTC)
	identitySpec := plan.Checks[0]
	identity := Check{Type: identitySpec.Type, Lookback: identitySpec.Lookback, MinSamples: identitySpec.MinSamples, SampleUnit: identitySpec.SampleUnit, FailureMode: identitySpec.FailureMode}
	if err := ApplySample(&identity, Sample{Status: SampleFailed, ReasonCode: "identity_mismatch"}, now); err != nil || identity.Status != CheckFailed {
		t.Fatalf("immediate failure status=%s err=%v", identity.Status, err)
	}
	metricSpec := plan.Checks[6]
	metric := Check{Status: CheckPending, Type: metricSpec.Type, StabilityWindow: metricSpec.StabilityWindow, PollInterval: metricSpec.PollInterval, FailureMode: metricSpec.FailureMode}
	if err := ApplySample(&metric, Sample{Status: SamplePassed}, now); err != nil || metric.Status != CheckRunning || metric.ConsecutiveSuccessSince == nil {
		t.Fatalf("first reset success=%+v err=%v", metric, err)
	}
	if err := ApplySample(&metric, Sample{Status: SamplePending, ReasonCode: "threshold_not_satisfied"}, now.Add(10*time.Second)); err != nil || metric.ConsecutiveSuccessSince != nil || metric.Status != CheckRunning {
		t.Fatalf("reset failure=%+v err=%v", metric, err)
	}
}

func validCompileInput(trigger string) CompileInput {
	return CompileInput{
		TriggerType: trigger, Repository: "acme/gitops", PullRequest: 7,
		TargetRevision: strings.Repeat("a", 40), SourceRevision: strings.Repeat("b", 40), ImageDigest: "sha256:" + strings.Repeat("c", 64), GitOpsRevision: strings.Repeat("a", 40),
		ArgoApplication: "demo", ArgoProject: "cloudops", Cluster: "kind", Environment: "demo", Namespace: "demo", Service: "demo", WorkloadName: "demo", AlertNames: []string{"DemoReadiness", "DemoErrors"},
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
