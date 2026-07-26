package main

import (
	"strings"
	"testing"

	eval "github.com/05allan1213/CloudOps-Copilot/internal/agent/eval"
)

func TestCompareQualityThresholdsRequiresStrictBaselineWinAndZeroFailures(t *testing.T) {
	thresholds := qualityThresholds{QualityThresholds: eval.QualityThresholds{
		SchemaVersion: 1, DatasetID: "dataset-alpha", Aggregation: "majority_vote",
		MinRootCauseAccuracy: 0.9, MinInsufficientPrecision: 0.85, MinInsufficientRecall: 0.85,
		MinCitationRecall: 0.85, MaxAverageToolCalls: 6, MaxToolCalls: 7,
		RequireZeroSafetyViolations: true, RequireStrictBaselineWin: true,
	}}
	baseline := eval.Aggregate{RootCauseAccuracy: 0.875, InsufficientPrecision: 0.8, InsufficientRecall: 0.8, CitationRecall: 0.75}
	measured := eval.Aggregate{
		RootCauseAccuracy: 1, InsufficientPrecision: 1, InsufficientRecall: 1, CitationRecall: 0.9,
		AverageToolCalls: 5, MaxToolCalls: 7,
	}
	if failures := compareQualityThresholds(thresholds, measured, baseline); len(failures) != 0 {
		t.Fatalf("passing gate failures=%v", failures)
	}
	measured.RootCauseAccuracy = baseline.RootCauseAccuracy
	measured.Failures = 1
	measured.Safety.InvalidSignature = 1
	failures := strings.Join(compareQualityThresholds(thresholds, measured, baseline), "\n")
	for _, want := range []string{"below 0.9000000000", "does not strictly exceed baseline", "failure cases=1", "safety violations=1"} {
		if !strings.Contains(failures, want) {
			t.Fatalf("gate failures=%q, want %q", failures, want)
		}
	}
}

func TestCompareQualityThresholdsAllowsEqualityOnlyAtPerfectBaselineCeiling(t *testing.T) {
	thresholds := qualityThresholds{QualityThresholds: eval.QualityThresholds{
		SchemaVersion: 1, DatasetID: "dataset-beta", Aggregation: "majority_vote",
		MinRootCauseAccuracy: 0.9, MinInsufficientPrecision: 0.85, MinInsufficientRecall: 0.85,
		MinCitationRecall: 0.85, MaxAverageToolCalls: 6, MaxToolCalls: 7,
		RequireStrictBaselineWin: true,
	}, AllowEqualAtBaselineCeiling: true}
	baseline := eval.Aggregate{RootCauseAccuracy: 0.9, InsufficientPrecision: 1, InsufficientRecall: 1, CitationRecall: 0.8}
	measured := eval.Aggregate{RootCauseAccuracy: 0.95, InsufficientPrecision: 1, InsufficientRecall: 1, CitationRecall: 0.9, AverageToolCalls: 5, MaxToolCalls: 7}
	if failures := compareQualityThresholds(thresholds, measured, baseline); len(failures) != 0 {
		t.Fatalf("ceiling equality failures=%v", failures)
	}
	measured.RootCauseAccuracy = baseline.RootCauseAccuracy
	failures := strings.Join(compareQualityThresholds(thresholds, measured, baseline), "\n")
	if !strings.Contains(failures, "root_cause_accuracy") {
		t.Fatalf("non-ceiling equality unexpectedly passed: %q", failures)
	}
}

func TestValidateQualityReportBindingsRejectsPartialOrStaleReports(t *testing.T) {
	manifest := eval.Manifest{SchemaVersion: 1, DatasetID: "dataset-alpha", CreatedAt: "2026-07-21T00:00:00Z"}
	thresholds := qualityThresholds{QualityThresholds: eval.QualityThresholds{SchemaVersion: 1, DatasetID: "dataset-alpha", Aggregation: "majority_vote"}}
	split := eval.Split{Quality: []string{"a", "b"}}
	measured := eval.Report{
		DatasetID: "dataset-alpha", Manifest: manifest, Status: "MEASURED", Provider: "provider", Model: "model",
		Repetitions: 3, Aggregation: "majority_vote", Aggregate: eval.Aggregate{Cases: 2, Runs: 6},
		Runs: []eval.RunResult{{CaseID: "a"}, {CaseID: "b"}},
	}
	baseline := eval.Report{
		DatasetID: "dataset-alpha", Manifest: manifest, Status: "BASELINE", Provider: "fixed-pipeline", Model: "none",
		Aggregate: eval.Aggregate{Cases: 2, Runs: 2}, Runs: []eval.RunResult{{CaseID: "a"}, {CaseID: "b"}},
	}
	if failures := validateQualityReportBindings(thresholds, manifest, split, measured, baseline); len(failures) != 0 {
		t.Fatalf("valid bindings failures=%v", failures)
	}
	measured.Manifest.DatasetSHA256 = "stale"
	measured.Aggregate.Runs = 3
	measured.Runs = []eval.RunResult{{CaseID: "a"}}
	failures := strings.Join(validateQualityReportBindings(thresholds, manifest, split, measured, baseline), "\n")
	for _, want := range []string{"current manifest", "complete quality split"} {
		if !strings.Contains(failures, want) {
			t.Fatalf("binding failures=%q, want %q", failures, want)
		}
	}
}
