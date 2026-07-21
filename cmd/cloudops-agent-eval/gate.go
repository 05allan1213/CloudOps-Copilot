package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	eval "github.com/05allan1213/CloudOps-Copilot/internal/agent/eval"
)

type qualityGateReport struct {
	SchemaVersion int               `json:"schema_version"`
	DatasetID     string            `json:"dataset_id"`
	Status        string            `json:"status"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	Manifest      eval.Manifest     `json:"manifest"`
	Thresholds    qualityThresholds `json:"thresholds"`
	Baseline      eval.Aggregate    `json:"baseline"`
	Measured      eval.Aggregate    `json:"measured"`
	Failures      []string          `json:"failures,omitempty"`
}

type qualityThresholds struct {
	eval.QualityThresholds
	AllowEqualAtBaselineCeiling bool `json:"allow_equal_at_baseline_ceiling,omitempty"`
}

func runQualityGate(paths evalPathsValue, manifest eval.Manifest, split eval.Split, options options) (qualityGateReport, error) {
	if strings.TrimSpace(options.report) == "" || strings.TrimSpace(options.baseline) == "" {
		return qualityGateReport{}, errors.New("gate mode requires -report and -baseline")
	}
	thresholdPath := strings.TrimSpace(options.thresholds)
	if thresholdPath == "" {
		thresholdPath = paths.thresholds
	}
	var thresholds qualityThresholds
	if err := loadStrictJSON(thresholdPath, &thresholds); err != nil {
		return qualityGateReport{}, fmt.Errorf("load quality thresholds: %w", err)
	}
	if err := validateQualityThresholds(thresholds); err != nil {
		return qualityGateReport{}, err
	}
	var measured, baseline eval.Report
	if err := loadStrictJSON(options.report, &measured); err != nil {
		return qualityGateReport{}, fmt.Errorf("load measured report: %w", err)
	}
	if err := loadStrictJSON(options.baseline, &baseline); err != nil {
		return qualityGateReport{}, fmt.Errorf("load baseline report: %w", err)
	}

	result := qualityGateReport{
		SchemaVersion: 1, DatasetID: thresholds.DatasetID, Status: "PASS",
		Provider: measured.Provider, Model: measured.Model, Manifest: manifest,
		Thresholds: thresholds, Baseline: baseline.Aggregate, Measured: measured.Aggregate,
	}
	result.Failures = append(result.Failures, validateQualityReportBindings(thresholds, manifest, split, measured, baseline)...)
	result.Failures = append(result.Failures, compareQualityThresholds(thresholds, measured.Aggregate, baseline.Aggregate)...)
	if len(result.Failures) > 0 {
		result.Status = "FAIL"
	}
	return result, nil
}

func validateQualityThresholds(value qualityThresholds) error {
	if value.SchemaVersion != 1 || strings.TrimSpace(value.DatasetID) == "" || strings.TrimSpace(value.Aggregation) == "" {
		return errors.New("quality thresholds have invalid identity")
	}
	for name, threshold := range map[string]float64{
		"min_root_cause_accuracy":    value.MinRootCauseAccuracy,
		"min_insufficient_precision": value.MinInsufficientPrecision,
		"min_insufficient_recall":    value.MinInsufficientRecall,
		"min_citation_recall":        value.MinCitationRecall,
		"min_replay_pass_rate":       value.MinReplayPassRate,
	} {
		if threshold < 0 || threshold > 1 {
			return fmt.Errorf("quality threshold %s must be between 0 and 1", name)
		}
	}
	if value.MaxAverageToolCalls <= 0 || value.MaxToolCalls <= 0 {
		return errors.New("quality tool-call thresholds must be positive")
	}
	return nil
}

func validateQualityReportBindings(thresholds qualityThresholds, manifest eval.Manifest, split eval.Split, measured, baseline eval.Report) []string {
	failures := make([]string, 0)
	if thresholds.DatasetID != manifest.DatasetID || measured.DatasetID != manifest.DatasetID || baseline.DatasetID != manifest.DatasetID {
		failures = append(failures, "dataset identity differs across thresholds, manifest, measured report, or baseline")
	}
	if measured.Manifest != manifest || baseline.Manifest != manifest {
		failures = append(failures, "measured report or baseline is not bound to the current manifest")
	}
	if measured.Status != "MEASURED" || strings.TrimSpace(measured.Provider) == "" || strings.TrimSpace(measured.Model) == "" {
		failures = append(failures, "measured report lacks real-model identity or MEASURED status")
	}
	if baseline.Status != "BASELINE" || baseline.Provider != "fixed-pipeline" || baseline.Model != "none" {
		failures = append(failures, "baseline report is not the fixed non-Agent pipeline")
	}
	if measured.Aggregation != thresholds.Aggregation || measured.Repetitions < 3 {
		failures = append(failures, "measured aggregation or repetition count differs from thresholds")
	}
	if measured.Aggregate.Cases != len(split.Quality) || measured.Aggregate.Runs != len(split.Quality)*measured.Repetitions || !reportContainsExactly(measured.Runs, split.Quality) {
		failures = append(failures, "measured report does not contain the complete quality split")
	}
	if baseline.Aggregate.Cases != len(split.Quality) || baseline.Aggregate.Runs != len(split.Quality) || !reportContainsExactly(baseline.Runs, split.Quality) {
		failures = append(failures, "baseline report does not contain the complete quality split")
	}
	return failures
}

func compareQualityThresholds(thresholds qualityThresholds, measured, baseline eval.Aggregate) []string {
	failures := make([]string, 0)
	minimum := []struct {
		name      string
		measured  float64
		threshold float64
	}{
		{name: "root_cause_accuracy", measured: measured.RootCauseAccuracy, threshold: thresholds.MinRootCauseAccuracy},
		{name: "insufficient_precision", measured: measured.InsufficientPrecision, threshold: thresholds.MinInsufficientPrecision},
		{name: "insufficient_recall", measured: measured.InsufficientRecall, threshold: thresholds.MinInsufficientRecall},
		{name: "citation_recall", measured: measured.CitationRecall, threshold: thresholds.MinCitationRecall},
		{name: "replay_pass_rate", measured: measured.ReplayPassRate, threshold: thresholds.MinReplayPassRate},
	}
	for _, item := range minimum {
		if item.measured < item.threshold {
			failures = append(failures, fmt.Sprintf("%s %.10f is below %.10f", item.name, item.measured, item.threshold))
		}
	}
	if measured.AverageToolCalls > thresholds.MaxAverageToolCalls {
		failures = append(failures, fmt.Sprintf("average_tool_calls %.10f exceeds %.10f", measured.AverageToolCalls, thresholds.MaxAverageToolCalls))
	}
	if measured.MaxToolCalls > thresholds.MaxToolCalls {
		failures = append(failures, fmt.Sprintf("max_tool_calls %d exceeds %d", measured.MaxToolCalls, thresholds.MaxToolCalls))
	}
	if thresholds.RequireZeroSafetyViolations && measured.Safety.Total() != 0 {
		failures = append(failures, fmt.Sprintf("safety violations=%d, want zero", measured.Safety.Total()))
	}
	if measured.Failures != 0 {
		failures = append(failures, fmt.Sprintf("raw or majority-vote failure cases=%d, want zero", measured.Failures))
	}
	if thresholds.RequireStrictBaselineWin {
		comparisons := []struct {
			name               string
			measured, baseline float64
		}{
			{name: "root_cause_accuracy", measured: measured.RootCauseAccuracy, baseline: baseline.RootCauseAccuracy},
			{name: "insufficient_precision", measured: measured.InsufficientPrecision, baseline: baseline.InsufficientPrecision},
			{name: "insufficient_recall", measured: measured.InsufficientRecall, baseline: baseline.InsufficientRecall},
			{name: "citation_recall", measured: measured.CitationRecall, baseline: baseline.CitationRecall},
		}
		for _, comparison := range comparisons {
			atAllowedCeiling := thresholds.AllowEqualAtBaselineCeiling && comparison.baseline == 1 && comparison.measured == 1
			if comparison.measured <= comparison.baseline && !atAllowedCeiling {
				failures = append(failures, fmt.Sprintf("%s %.10f does not strictly exceed baseline %.10f", comparison.name, comparison.measured, comparison.baseline))
			}
		}
	}
	return failures
}

func reportContainsExactly(runs []eval.RunResult, expected []string) bool {
	seen := make(map[string]struct{}, len(expected))
	for _, run := range runs {
		seen[run.CaseID] = struct{}{}
	}
	actual := make([]string, 0, len(seen))
	for caseID := range seen {
		actual = append(actual, caseID)
	}
	slices.Sort(actual)
	want := slices.Clone(expected)
	slices.Sort(want)
	return slices.Equal(actual, want)
}

func loadStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains multiple top-level values")
	}
	return nil
}
