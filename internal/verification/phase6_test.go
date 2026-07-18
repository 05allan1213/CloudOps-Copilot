package verification

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestProfilesAndCompilerAreStrictAndDeterministic(t *testing.T) {
	profileSet := Profiles{Items: []Profile{{ID: "checkout-minimal", Service: "checkout", Environment: "staging", Namespace: "shop", Workload: "checkout", Templates: []Template{
		{ID: "trace-errors-v1", Type: CheckTraceErrorRateBelow, Required: false, Comparison: CompareLTE, Threshold: .01, LookbackSeconds: 600, TimeoutSeconds: 600, StabilitySeconds: 120},
		{ID: "metric-errors-v1", Type: CheckMetricErrorRateBelow, Required: true, Comparison: CompareLT, Threshold: .02, LookbackSeconds: 300, TimeoutSeconds: 600, StabilitySeconds: 120},
		{ID: "logs-absent-v1", Type: CheckLogErrorAbsent, Required: true, Comparison: CompareAbsent, Threshold: 0, LookbackSeconds: 300, TimeoutSeconds: 600, StabilitySeconds: 120},
	}}}}
	if err := profileSet.Validate(); err != nil {
		t.Fatal(err)
	}
	subject := Subject{Repository: "acme/gitops", PullRequest: 7, Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ArgoApplication: "checkout", ArgoProject: "shop", Cluster: "test", Environment: "staging", Namespace: "shop", Service: "checkout", WorkloadKind: "Deployment", WorkloadName: "checkout", AlertFingerprint: "fp"}
	profile, err := profileSet.Match(subject)
	if err != nil {
		t.Fatal(err)
	}
	cfg := CompilerConfig{PollInterval: 10 * time.Second, Timeout: 30 * time.Minute, StabilityWindow: 2 * time.Minute, AlertLookback: 30 * time.Minute}
	first, err := CompileTrustedPlanWithProfile(subject, cfg, profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileTrustedPlanWithProfile(subject, cfg, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.SchemaVersion != 2 || len(first.Checks) != 9 {
		t.Fatalf("plans are not deterministic: %+v %+v", first, second)
	}
	if first.Checks[6].Type != CheckLogErrorAbsent || first.Checks[7].Type != CheckMetricErrorRateBelow || first.Checks[8].Type != CheckTraceErrorRateBelow {
		t.Fatalf("unexpected deterministic order: %+v", first.Checks[6:])
	}
	if err := ValidatePlan(first); err != nil {
		t.Fatal(err)
	}

	bad := profileSet
	bad.Items[0].Templates[0].Threshold = math.NaN()
	if err := bad.Validate(); err == nil {
		t.Fatal("NaN threshold accepted")
	}
	subject.Environment = "production"
	if _, err := profileSet.Match(subject); err == nil {
		t.Fatal("untrusted environment accepted")
	}
}

func TestEvaluateObservationBoundariesAndNoData(t *testing.T) {
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	base := Check{Type: CheckMetricErrorRateBelow, Comparison: CompareLT, Threshold: .1, Lookback: 5 * time.Minute}
	observation := Observation{Status: ObservationAvailable, Value: .1, SampleCount: 1, SampledAt: now}
	if got := EvaluateObservation(base, observation, now); got.Status != SamplePending {
		t.Fatalf("exact LT threshold passed: %+v", got)
	}
	base.Comparison = CompareLTE
	if got := EvaluateObservation(base, observation, now); got.Status != SamplePassed {
		t.Fatalf("exact LTE threshold failed: %+v", got)
	}
	base.Comparison, base.Threshold = CompareGT, .1
	if got := EvaluateObservation(base, observation, now); got.Status != SamplePending {
		t.Fatalf("exact GT threshold passed: %+v", got)
	}
	base.Comparison = CompareGTE
	if got := EvaluateObservation(base, observation, now); got.Status != SamplePassed {
		t.Fatalf("exact GTE threshold failed: %+v", got)
	}
	if got := EvaluateObservation(base, Observation{Status: ObservationNoData}, now); got.Status != SampleUnavailable {
		t.Fatalf("metric no-data=%+v", got)
	}
	absence := Check{Type: CheckLogErrorAbsent, Comparison: CompareAbsent, Lookback: 5 * time.Minute}
	if got := EvaluateObservation(absence, Observation{Status: ObservationNoData}, now); got.Status != SamplePassed {
		t.Fatalf("absence no-data=%+v", got)
	}
	if got := EvaluateObservation(base, Observation{Status: ObservationUnavailable}, now); got.Status != SampleUnavailable {
		t.Fatalf("unavailable=%+v", got)
	}
	if got := EvaluateObservation(base, Observation{Status: ObservationMalformed}, now); got.Status != SampleInvalid {
		t.Fatalf("malformed=%+v", got)
	}
	stale := observation
	stale.SampledAt = now.Add(-6 * time.Minute)
	if got := EvaluateObservation(base, stale, now); got.Status != SampleInvalid {
		t.Fatalf("stale=%+v", got)
	}
}

func TestStabilityResetAndOptionalAggregate(t *testing.T) {
	start := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	check := Check{Status: CheckPending, StabilityWindow: 2 * time.Minute}
	if err := ApplySample(&check, Sample{Status: SamplePassed}, start); err != nil {
		t.Fatal(err)
	}
	if check.Status != CheckRunning {
		t.Fatal("single sample became terminal")
	}
	if err := ApplySample(&check, Sample{Status: SamplePending}, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if check.ConsecutiveSuccessSince != nil {
		t.Fatal("failure did not reset stability")
	}
	if err := ApplySample(&check, Sample{Status: SamplePassed}, start.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := ApplySample(&check, Sample{Status: SamplePassed}, start.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if check.Status != CheckPassed {
		t.Fatal("exact stability boundary did not pass")
	}
	status, _, terminal := Aggregate([]Check{{Required: true, Status: CheckPassed}, {Required: false, Status: CheckFailed}})
	if !terminal || status != RunPassed {
		t.Fatalf("optional check overrode aggregate: %s", status)
	}
	status, _, terminal = Aggregate([]Check{{Required: true, Status: CheckUnavailable}})
	if terminal || status == RunPassed {
		t.Fatalf("required unavailable passed: %s", status)
	}
	status, _, terminal = Aggregate([]Check{{Required: true, Status: CheckTimedOut}})
	if !terminal || status != RunTimedOut {
		t.Fatalf("required timeout=%s terminal=%v", status, terminal)
	}
}
