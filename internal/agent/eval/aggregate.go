package agenteval

import (
	"fmt"
	"sort"
)

const (
	aggregationSingleRun    = "single_run"
	aggregationMajorityVote = "majority_vote"
)

// AggregateResults reduces raw repetitions to one scored result per case.
//
// Score metrics that describe the decision (outcome, claim type, insufficient
// evidence, and citations) are calculated from the reduced results.  Run-level
// operational metrics (tool calls, replay, and safety) intentionally remain
// based on the raw runs.  A raw error or safety violation contributes one
// additional failure per affected case, regardless of how many repetitions
// carried that condition; this prevents a repeated provider failure from being
// counted once per repetition.
func AggregateResults(
	oracle Oracle,
	runs []RunResult,
	factTypes map[string]map[string]string,
	aggregation string,
) (Aggregate, error) {
	if aggregation != aggregationSingleRun && aggregation != aggregationMajorityVote {
		return Aggregate{}, fmt.Errorf("unsupported aggregation %q", aggregation)
	}
	if err := validateAggregateRuns(oracle, runs); err != nil {
		return Aggregate{}, err
	}

	groups, caseIDs := aggregateRunGroups(runs)
	var (
		aggregatedRuns []RunResult
		err            error
	)
	switch aggregation {
	case aggregationSingleRun:
		aggregatedRuns, err = aggregateSingleRunGroups(groups, caseIDs)
	case aggregationMajorityVote:
		aggregatedRuns, err = aggregateMajorityGroups(groups, caseIDs)
	}
	if err != nil {
		return Aggregate{}, err
	}

	// Raw errors and safety are kept out of the reduced scoring input.  They are
	// added below once per case so ScoreWithFactTypes cannot count them again.
	result, err := ScoreWithFactTypes(oracle, aggregatedRuns, factTypes)
	if err != nil {
		return Aggregate{}, err
	}
	overrideRawMetrics(&result, oracle, runs)
	return result, nil
}

func validateAggregateRuns(oracle Oracle, runs []RunResult) error {
	if err := validateScoreOracle(oracle); err != nil {
		return err
	}
	for index, run := range runs {
		if _, ok := oracle.Cases[run.CaseID]; !ok {
			return fmt.Errorf("run %d references unknown case %q", index, run.CaseID)
		}
		if err := validateScoreRun(index, run); err != nil {
			return err
		}
	}
	return nil
}

func aggregateRunGroups(runs []RunResult) (map[string][]RunResult, []string) {
	groups := make(map[string][]RunResult)
	for _, run := range runs {
		groups[run.CaseID] = append(groups[run.CaseID], run)
	}
	caseIDs := make([]string, 0, len(groups))
	for caseID := range groups {
		caseIDs = append(caseIDs, caseID)
	}
	sort.Strings(caseIDs)
	return groups, caseIDs
}

func aggregateSingleRunGroups(groups map[string][]RunResult, caseIDs []string) ([]RunResult, error) {
	result := make([]RunResult, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		group := groups[caseID]
		if len(group) != 1 {
			return nil, fmt.Errorf(
				"single_run requires exactly one run per case: case %q has %d runs",
				caseID, len(group),
			)
		}
		result = append(result, reducedRun(group[0]))
	}
	return result, nil
}

func aggregateMajorityGroups(groups map[string][]RunResult, caseIDs []string) ([]RunResult, error) {
	result := make([]RunResult, 0, len(caseIDs))
	expectedRepetitions := 0
	for _, caseID := range caseIDs {
		group := groups[caseID]
		if len(group) < 3 {
			return nil, fmt.Errorf(
				"majority_vote requires at least 3 runs per case: case %q has %d",
				caseID, len(group),
			)
		}
		if expectedRepetitions == 0 {
			expectedRepetitions = len(group)
		} else if len(group) != expectedRepetitions {
			return nil, fmt.Errorf(
				"majority_vote requires the same repetition count per case: case %q has %d, want %d",
				caseID, len(group), expectedRepetitions,
			)
		}

		seenRepetitions := make(map[int]struct{}, len(group))
		for _, run := range group {
			if _, exists := seenRepetitions[run.Repetition]; exists {
				return nil, fmt.Errorf(
					"majority_vote case %q has duplicate repetition %d",
					caseID, run.Repetition,
				)
			}
			seenRepetitions[run.Repetition] = struct{}{}
		}

		result = append(result, majorityRun(caseID, group))
	}
	return result, nil
}

type aggregateTuple struct {
	outcome   string
	claimType string
}

func majorityRun(caseID string, runs []RunResult) RunResult {
	counts := make(map[aggregateTuple]int)
	for _, run := range runs {
		key := aggregateTuple{outcome: run.Outcome, claimType: aggregateClaimType(run)}
		counts[key]++
	}

	var winner aggregateTuple
	winnerFound := false
	for key, count := range counts {
		if count*2 > len(runs) {
			winner = key
			winnerFound = true
			break
		}
	}
	if !winnerFound {
		return RunResult{
			CaseID:       caseID,
			Outcome:      OutcomeInsufficient,
			ErrorCode:    "no_majority",
			ErrorSummary: "no strict majority for outcome and claim type",
		}
	}

	winningRuns := make([]RunResult, 0, len(runs))
	for _, run := range runs {
		key := aggregateTuple{outcome: run.Outcome, claimType: aggregateClaimType(run)}
		if key == winner {
			winningRuns = append(winningRuns, run)
		}
	}

	result := RunResult{CaseID: caseID, Outcome: winner.outcome, ClaimType: winner.claimType}
	result.CitedFactIDs = majorityFactIDs(winningRuns)
	result.ReplayOK = true
	for _, run := range winningRuns {
		if !run.ReplayOK {
			result.ReplayOK = false
			break
		}
	}
	return result
}

func aggregateClaimType(run RunResult) string {
	if run.Outcome == OutcomeInsufficient {
		return ""
	}
	return run.ClaimType
}

func majorityFactIDs(runs []RunResult) []string {
	if len(runs) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, run := range runs {
		seen := make(map[string]struct{}, len(run.CitedFactIDs))
		for _, factID := range run.CitedFactIDs {
			if factID == "" {
				continue
			}
			if _, exists := seen[factID]; exists {
				continue
			}
			seen[factID] = struct{}{}
			counts[factID]++
		}
	}

	result := make([]string, 0, len(counts))
	for factID, count := range counts {
		if count*2 > len(runs) {
			result = append(result, factID)
		}
	}
	sort.Strings(result)
	return result
}

func reducedRun(run RunResult) RunResult {
	result := run
	result.ErrorCode = ""
	result.ErrorSummary = ""
	result.Safety = SafetyCounts{}
	return result
}

func overrideRawMetrics(result *Aggregate, oracle Oracle, runs []RunResult) {
	result.Runs = len(runs)
	result.MaxToolCalls = 0
	result.AverageToolCalls = 0
	result.ReplayPassRate = 0
	result.Safety = SafetyCounts{}

	var totalToolCalls int64
	replayRuns := 0
	replayPassed := 0
	rawIssueCases := make(map[string]struct{})
	for _, run := range runs {
		totalToolCalls += int64(run.ToolCalls)
		if run.ToolCalls > result.MaxToolCalls {
			result.MaxToolCalls = run.ToolCalls
		}
		caseOracle := oracle.Cases[run.CaseID]
		if caseOracle.RequireReplay {
			replayRuns++
			if run.ReplayOK {
				replayPassed++
			}
		}
		scoreAddSafetyCounts(&result.Safety, run.Safety)
		if run.ErrorCode != "" || run.Safety.Total() > 0 {
			rawIssueCases[run.CaseID] = struct{}{}
		}
	}
	if result.Runs > 0 {
		result.AverageToolCalls = float64(totalToolCalls) / float64(result.Runs)
	}
	result.ReplayPassRate = scoreRatio(replayPassed, replayRuns)
	result.Failures += len(rawIssueCases)
}
