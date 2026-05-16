package eval

import (
	"fmt"
	"sort"
	"strings"

	"server-web/internal/copilot/nlu"
)

type MultiEvalResult struct {
	Total             int
	ExactMatch        int
	ExactMatchRate    float64
	SplitAccuracy     float64
	SingleIntentPass  int
	SingleIntentTotal int
	ByCase            []MultiCaseResult
}

type MultiCaseResult struct {
	Input        string
	WantIntents  []string
	GotIntents   []string
	ExactMatch   bool
	SplitCorrect bool
}

func EvaluateMulti(classifier *nlu.Classifier, cases []MultiEvalCase) MultiEvalResult {
	if len(cases) == 0 {
		return MultiEvalResult{}
	}

	var byCase []MultiCaseResult
	exactMatch := 0
	splitCorrect := 0
	singlePass := 0
	singleTotal := 0

	for _, c := range cases {
		result := classifier.ClassifyMulti(c.Input)
		var gotIntents []string
		if len(result.Intents) > 0 {
			for _, is := range result.Intents {
				gotIntents = append(gotIntents, is.Intent)
			}
		} else {
			gotIntents = []string{result.Intent}
		}

		em := intentsEqual(gotIntents, c.WantIntents)
		sc := intentsSetEqual(gotIntents, c.WantIntents)

		if em {
			exactMatch++
		}
		if sc {
			splitCorrect++
		}

		if len(c.WantIntents) == 1 {
			singleTotal++
			if result.Intent == c.WantIntents[0] {
				singlePass++
			}
		}

		byCase = append(byCase, MultiCaseResult{
			Input:        c.Input,
			WantIntents:  c.WantIntents,
			GotIntents:   gotIntents,
			ExactMatch:   em,
			SplitCorrect: sc,
		})
	}

	total := len(cases)
	var exactMatchRate, splitAccuracy float64
	if total > 0 {
		exactMatchRate = float64(exactMatch) / float64(total)
		splitAccuracy = float64(splitCorrect) / float64(total)
	}

	return MultiEvalResult{
		Total:             total,
		ExactMatch:        exactMatch,
		ExactMatchRate:    exactMatchRate,
		SplitAccuracy:     splitAccuracy,
		SingleIntentPass:  singlePass,
		SingleIntentTotal: singleTotal,
		ByCase:            byCase,
	}
}

func intentsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func intentsSetEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSet := make(map[string]int, len(got))
	for _, s := range got {
		gotSet[s]++
	}
	wantSet := make(map[string]int, len(want))
	for _, s := range want {
		wantSet[s]++
	}
	if len(gotSet) != len(wantSet) {
		return false
	}
	for k, v := range gotSet {
		if wantSet[k] != v {
			return false
		}
	}
	return true
}

func (r MultiEvalResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== NLU Multi-Intent Evaluation Report ===\n")
	fmt.Fprintf(&b, "Total: %d, ExactMatch: %d (%.1f%%), SplitAccuracy: %.1f%%\n",
		r.Total, r.ExactMatch, r.ExactMatchRate*100, r.SplitAccuracy*100)
	if r.SingleIntentTotal > 0 {
		rate := float64(r.SingleIntentPass) / float64(r.SingleIntentTotal)
		fmt.Fprintf(&b, "Single-intent: %d/%d (%.1f%%)\n", r.SingleIntentPass, r.SingleIntentTotal, rate*100)
	}
	fmt.Fprintf(&b, "\n")

	sort.Slice(r.ByCase, func(i, j int) bool {
		return r.ByCase[i].Input < r.ByCase[j].Input
	})

	fmt.Fprintf(&b, "%-40s %-20s %-20s %-8s %-8s\n", "Input", "Want", "Got", "Exact", "Split")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 100))
	for _, c := range r.ByCase {
		wantStr := strings.Join(c.WantIntents, ",")
		gotStr := strings.Join(c.GotIntents, ",")
		exact := "✓"
		if !c.ExactMatch {
			exact = "✗"
		}
		split := "✓"
		if !c.SplitCorrect {
			split = "✗"
		}
		input := c.Input
		if len(input) > 38 {
			input = input[:38] + ".."
		}
		fmt.Fprintf(&b, "%-40s %-20s %-20s %-8s %-8s\n", input, wantStr, gotStr, exact, split)
	}

	return b.String()
}
