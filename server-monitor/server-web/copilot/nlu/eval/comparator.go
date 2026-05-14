package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const significanceThreshold = 0.05

type ComparisonResult struct {
	BaselineAccuracy          float64
	CurrentAccuracy           float64
	AccuracyDelta             float64
	BaselineRuleConfidentRate float64
	CurrentRuleConfidentRate  float64
	RuleConfidentDelta        float64
	ByIntent                  []IntentComparison
}

type IntentComparison struct {
	Intent          string
	BaselineF1      float64
	CurrentF1       float64
	F1Delta         float64
	BaselineSupport int
	CurrentSupport  int
	Improved        bool
	Regressed       bool
}

func CompareEvaluation(baseline, current EvalResult) ComparisonResult {
	result := ComparisonResult{
		BaselineAccuracy:          baseline.Accuracy,
		CurrentAccuracy:           current.Accuracy,
		AccuracyDelta:             current.Accuracy - baseline.Accuracy,
		BaselineRuleConfidentRate: baseline.RuleConfidentRate,
		CurrentRuleConfidentRate:  current.RuleConfidentRate,
		RuleConfidentDelta:        current.RuleConfidentRate - baseline.RuleConfidentRate,
	}

	baselineIntents := make(map[string]IntentMetrics, len(baseline.ByIntent))
	for _, m := range baseline.ByIntent {
		baselineIntents[m.Intent] = m
	}

	seen := make(map[string]bool, len(current.ByIntent)+len(baseline.ByIntent))

	for _, cm := range current.ByIntent {
		seen[cm.Intent] = true
		bm, exists := baselineIntents[cm.Intent]
		ic := IntentComparison{
			Intent:         cm.Intent,
			CurrentF1:      cm.F1,
			CurrentSupport: cm.Support,
		}
		if exists {
			ic.BaselineF1 = bm.F1
			ic.BaselineSupport = bm.Support
			ic.F1Delta = cm.F1 - bm.F1
			if ic.F1Delta > significanceThreshold {
				ic.Improved = true
			}
			if ic.F1Delta < -significanceThreshold {
				ic.Regressed = true
			}
		}
		result.ByIntent = append(result.ByIntent, ic)
	}

	for _, bm := range baseline.ByIntent {
		if !seen[bm.Intent] {
			result.ByIntent = append(result.ByIntent, IntentComparison{
				Intent:          bm.Intent,
				BaselineF1:      bm.F1,
				BaselineSupport: bm.Support,
				F1Delta:         -bm.F1,
				Regressed:       true,
			})
		}
	}

	sort.Slice(result.ByIntent, func(i, j int) bool {
		return result.ByIntent[i].Intent < result.ByIntent[j].Intent
	})

	return result
}

func (r ComparisonResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== NLU Evaluation Comparison ===\n")
	fmt.Fprintf(&b, "Overall Accuracy: %.1f%% → %.1f%% (Δ %+.1f%%)\n",
		r.BaselineAccuracy*100, r.CurrentAccuracy*100, r.AccuracyDelta*100)
	fmt.Fprintf(&b, "Rule Confident Rate: %.1f%% → %.1f%% (Δ %+.1f%%)\n",
		r.BaselineRuleConfidentRate*100, r.CurrentRuleConfidentRate*100, r.RuleConfidentDelta*100)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "%-26s %-12s %-12s %-10s %s\n", "Intent", "Baseline F1", "Current F1", "Delta", "Status")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 70))
	for _, ic := range r.ByIntent {
		status := "—"
		if ic.Improved {
			status = "↑ IMPROVED"
		}
		if ic.Regressed {
			status = "↓ REGRESSED"
		}
		fmt.Fprintf(&b, "%-26s %-12.2f %-12.2f %-10.2f %s\n",
			ic.Intent, ic.BaselineF1, ic.CurrentF1, ic.F1Delta, status)
	}
	improved := 0
	regressed := 0
	for _, ic := range r.ByIntent {
		if ic.Improved {
			improved++
		}
		if ic.Regressed {
			regressed++
		}
	}
	unchanged := len(r.ByIntent) - improved - regressed
	fmt.Fprintf(&b, "\nSummary: %d improved, %d regressed, %d unchanged\n", improved, regressed, unchanged)
	return b.String()
}

func (r ComparisonResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}
