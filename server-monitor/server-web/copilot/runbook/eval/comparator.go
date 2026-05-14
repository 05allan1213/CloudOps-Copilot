package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const ragSignificanceThreshold = 0.05

type RAGComparisonResult struct {
	BaselineTop1Accuracy float64
	CurrentTop1Accuracy  float64
	AccuracyDelta        float64
	ByCategory           []CategoryComparison
}

type CategoryComparison struct {
	Category         string
	BaselineAccuracy float64
	CurrentAccuracy  float64
	AccuracyDelta    float64
	Improved         bool
	Regressed        bool
}

func CompareRAGEvaluation(baseline, current RAGEvalResult) RAGComparisonResult {
	result := RAGComparisonResult{
		BaselineTop1Accuracy: baseline.Top1Accuracy,
		CurrentTop1Accuracy:  current.Top1Accuracy,
		AccuracyDelta:        current.Top1Accuracy - baseline.Top1Accuracy,
	}

	baselineCats := make(map[string]CategoryResult, len(baseline.ByCategory))
	for name, cat := range baseline.ByCategory {
		baselineCats[name] = cat
	}

	seen := make(map[string]bool, len(current.ByCategory)+len(baseline.ByCategory))

	for name, cc := range current.ByCategory {
		seen[name] = true
		cmp := CategoryComparison{
			Category:        name,
			CurrentAccuracy: cc.Accuracy,
		}
		if bc, exists := baselineCats[name]; exists {
			cmp.BaselineAccuracy = bc.Accuracy
			cmp.AccuracyDelta = cc.Accuracy - bc.Accuracy
			if cmp.AccuracyDelta > ragSignificanceThreshold {
				cmp.Improved = true
			}
			if cmp.AccuracyDelta < -ragSignificanceThreshold {
				cmp.Regressed = true
			}
		}
		result.ByCategory = append(result.ByCategory, cmp)
	}

	for name, bc := range baseline.ByCategory {
		if !seen[name] {
			result.ByCategory = append(result.ByCategory, CategoryComparison{
				Category:         name,
				BaselineAccuracy: bc.Accuracy,
				AccuracyDelta:    -bc.Accuracy,
				Regressed:        true,
			})
		}
	}

	sort.Slice(result.ByCategory, func(i, j int) bool {
		return result.ByCategory[i].Category < result.ByCategory[j].Category
	})

	return result
}

func (r RAGComparisonResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== RAG Evaluation Comparison ===\n")
	fmt.Fprintf(&b, "Overall Top-1 Accuracy: %.1f%% → %.1f%% (Δ %+.1f%%)\n",
		r.BaselineTop1Accuracy*100, r.CurrentTop1Accuracy*100, r.AccuracyDelta*100)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "%-16s %-12s %-12s %-10s %s\n", "Category", "Baseline", "Current", "Delta", "Status")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 60))
	for _, cc := range r.ByCategory {
		status := "—"
		if cc.Improved {
			status = "↑ IMPROVED"
		}
		if cc.Regressed {
			status = "↓ REGRESSED"
		}
		fmt.Fprintf(&b, "%-16s %-12.1f %-12.1f %-10.1f %s\n",
			cc.Category, cc.BaselineAccuracy*100, cc.CurrentAccuracy*100, cc.AccuracyDelta*100, status)
	}
	improved := 0
	regressed := 0
	for _, cc := range r.ByCategory {
		if cc.Improved {
			improved++
		}
		if cc.Regressed {
			regressed++
		}
	}
	unchanged := len(r.ByCategory) - improved - regressed
	fmt.Fprintf(&b, "\nSummary: %d improved, %d regressed, %d unchanged\n", improved, regressed, unchanged)
	return b.String()
}

func (r RAGComparisonResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}
