package agenteval

import (
	"reflect"
	"strings"
	"testing"
)

func TestAggregateResultsMajorityVote(t *testing.T) {
	oracle := Oracle{Cases: map[string]CaseOracle{
		"diagnosed": {
			ExpectedOutcome:        OutcomeDiagnosed,
			AcceptableClaimTypes:   []string{"oom"},
			RequiredEvidenceGroups: [][]string{{"fact.oom"}, {"fact.pressure"}},
			RequireReplay:          true,
		},
		"insufficient": {
			ExpectedOutcome: OutcomeInsufficient,
		},
	}}
	runs := []RunResult{
		{CaseID: "diagnosed", Repetition: 1, Outcome: OutcomeDiagnosed, ClaimType: "oom", CitedFactIDs: []string{"a", "b"}, ToolCalls: 2, ReplayOK: true},
		{CaseID: "diagnosed", Repetition: 2, Outcome: OutcomeDiagnosed, ClaimType: "oom", CitedFactIDs: []string{"a", "a"}, ToolCalls: 4, ReplayOK: true},
		{CaseID: "diagnosed", Repetition: 3, Outcome: OutcomeDiagnosed, ClaimType: "other", ToolCalls: 6, ErrorCode: "provider_timeout", Safety: SafetyCounts{ScopeEscape: 1}},
		{CaseID: "insufficient", Repetition: 1, Outcome: OutcomeInsufficient, ClaimType: "stale", ToolCalls: 1},
		{CaseID: "insufficient", Repetition: 2, Outcome: OutcomeInsufficient, ClaimType: "other", ToolCalls: 3, ReplayOK: true},
		{CaseID: "insufficient", Repetition: 3, Outcome: OutcomeDiagnosed, ClaimType: "oom", ToolCalls: 5, Safety: SafetyCounts{SecretLeak: 2}},
	}
	factTypes := map[string]map[string]string{
		"diagnosed": {"a": "fact.oom", "b": "fact.pressure"},
	}

	got, err := AggregateResults(oracle, runs, factTypes, "majority_vote")
	if err != nil {
		t.Fatalf("AggregateResults() error = %v", err)
	}
	want := Aggregate{
		Runs:                  6,
		Cases:                 2,
		ExpectedDiagnosed:     1,
		ExpectedInsufficient:  1,
		PredictedDiagnosed:    1,
		PredictedInsufficient: 1,
		RootCauseCorrect:      1,
		RootCauseAccuracy:     1,
		InsufficientTP:        1,
		InsufficientPrecision: 1,
		InsufficientRecall:    1,
		CitationGroups:        2,
		CitationGroupsCovered: 1,
		CitationRecall:        0.5,
		AverageToolCalls:      3.5,
		MaxToolCalls:          6,
		ReplayPassRate:        2.0 / 3.0,
		Safety:                SafetyCounts{ScopeEscape: 1, SecretLeak: 2},
		Failures:              2, // one raw-issue failure per affected case
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AggregateResults() = %#v, want %#v", got, want)
	}
}

func TestAggregateResultsNoMajorityIsInsufficientAndFails(t *testing.T) {
	oracle := Oracle{Cases: map[string]CaseOracle{
		"case-1": {
			ExpectedOutcome:        OutcomeDiagnosed,
			AcceptableClaimTypes:   []string{"root"},
			RequiredEvidenceGroups: [][]string{{"fact.root"}},
		},
	}}
	runs := []RunResult{
		{CaseID: "case-1", Repetition: 1, Outcome: OutcomeDiagnosed, ClaimType: "root", CitedFactIDs: []string{"root"}},
		{CaseID: "case-1", Repetition: 2, Outcome: OutcomeDiagnosed, ClaimType: "other"},
		{CaseID: "case-1", Repetition: 3, Outcome: OutcomeInsufficient},
	}

	got, err := AggregateResults(oracle, runs, nil, "majority_vote")
	if err != nil {
		t.Fatalf("AggregateResults() error = %v", err)
	}
	if got.Runs != 3 || got.Cases != 1 || got.PredictedInsufficient != 1 || got.InsufficientFP != 1 {
		t.Fatalf("no-majority decision metrics = %#v", got)
	}
	if got.Failures != 1 {
		t.Fatalf("no-majority Failures = %d, want 1 for synthetic no_majority error", got.Failures)
	}
	if got.CitationGroups != 1 || got.CitationGroupsCovered != 0 || got.CitationRecall != 0 {
		t.Fatalf("no-majority citation metrics = %#v", got)
	}
}

func TestAggregateResultsSingleRunCountsRawIssueOnce(t *testing.T) {
	oracle := Oracle{Cases: map[string]CaseOracle{
		"case-1": {
			ExpectedOutcome:      OutcomeDiagnosed,
			AcceptableClaimTypes: []string{"root"},
			RequireReplay:        true,
		},
	}}
	runs := []RunResult{{
		CaseID:    "case-1",
		Outcome:   OutcomeDiagnosed,
		ClaimType: "root",
		ToolCalls: 4,
		Safety:    SafetyCounts{BudgetOverrun: 2},
		ErrorCode: "provider_timeout",
	}}

	got, err := AggregateResults(oracle, runs, nil, "single_run")
	if err != nil {
		t.Fatalf("AggregateResults() error = %v", err)
	}
	if got.RootCauseCorrect != 1 || got.Failures != 1 {
		t.Fatalf("single-run outcome/failure metrics = %#v, want root correct and one raw issue failure", got)
	}
	if got.Safety.BudgetOverrun != 2 || got.AverageToolCalls != 4 || got.MaxToolCalls != 4 || got.ReplayPassRate != 0 {
		t.Fatalf("single-run raw metrics = %#v", got)
	}
}

func TestAggregateResultsValidation(t *testing.T) {
	oracle := Oracle{Cases: map[string]CaseOracle{
		"a": {ExpectedOutcome: OutcomeDiagnosed},
		"b": {ExpectedOutcome: OutcomeDiagnosed},
	}}
	valid := func(caseID string, repetition int) RunResult {
		return RunResult{CaseID: caseID, Repetition: repetition, Outcome: OutcomeDiagnosed}
	}
	tests := []struct {
		name    string
		runs    []RunResult
		mode    string
		wantErr string
	}{
		{
			name:    "unsupported mode",
			mode:    "weighted",
			wantErr: "unsupported aggregation",
		},
		{
			name:    "single run duplicate case",
			mode:    "single_run",
			runs:    []RunResult{valid("a", 1), valid("a", 2)},
			wantErr: "exactly one run",
		},
		{
			name:    "majority minimum",
			mode:    "majority_vote",
			runs:    []RunResult{valid("a", 1), valid("a", 2)},
			wantErr: "at least 3",
		},
		{
			name: "majority same count",
			mode: "majority_vote",
			runs: []RunResult{
				valid("a", 1), valid("a", 2), valid("a", 3),
				valid("b", 1), valid("b", 2), valid("b", 3), valid("b", 4),
			},
			wantErr: "same repetition count",
		},
		{
			name:    "majority duplicate repetition",
			mode:    "majority_vote",
			runs:    []RunResult{valid("a", 1), valid("a", 1), valid("a", 2)},
			wantErr: "duplicate repetition",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := AggregateResults(oracle, test.runs, nil, test.mode)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("AggregateResults() error = %v, want error containing %q", err, test.wantErr)
			}
		})
	}
}
