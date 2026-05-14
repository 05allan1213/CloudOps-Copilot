package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"server-web/copilot/nlu"
)

type IntentMetrics struct {
	Intent    string
	Precision float64
	Recall    float64
	F1        float64
	Support   int
	TP        int
	FP        int
	FN        int
}

type EvalResult struct {
	Total             int
	Correct           int
	Accuracy          float64
	RuleConfidentRate float64
	LowConfRate       float64
	ByIntent          map[string]IntentMetrics
}

func Evaluate(classifier *nlu.Classifier, cases []EvalCase) EvalResult {
	if len(cases) == 0 {
		return EvalResult{ByIntent: map[string]IntentMetrics{}}
	}

	type intentCounters struct {
		tp int
		fp int
		fn int
	}

	counters := make(map[string]*intentCounters)
	correct := 0
	ruleConfident := 0
	lowConf := 0

	for _, c := range cases {
		result := classifier.Classify(c.Input)
		predicted := result.Intent
		wanted := c.WantIntent

		if predicted == wanted {
			correct++
			if _, ok := counters[wanted]; !ok {
				counters[wanted] = &intentCounters{}
			}
			counters[wanted].tp++
		} else {
			if _, ok := counters[predicted]; !ok {
				counters[predicted] = &intentCounters{}
			}
			counters[predicted].fp++

			if _, ok := counters[wanted]; !ok {
				counters[wanted] = &intentCounters{}
			}
			counters[wanted].fn++
		}

		if result.Confidence >= 0.6 && predicted != nlu.IntentUnknown {
			ruleConfident++
		} else {
			lowConf++
		}
	}

	total := len(cases)
	byIntent := make(map[string]IntentMetrics, len(counters))
	for intent, ctr := range counters {
		support := ctr.tp + ctr.fn
		var precision, recall, f1 float64
		if ctr.tp+ctr.fp > 0 {
			precision = float64(ctr.tp) / float64(ctr.tp+ctr.fp)
		}
		if ctr.tp+ctr.fn > 0 {
			recall = float64(ctr.tp) / float64(ctr.tp+ctr.fn)
		}
		if precision+recall > 0 {
			f1 = 2 * precision * recall / (precision + recall)
		}
		byIntent[intent] = IntentMetrics{
			Intent:    intent,
			Precision: precision,
			Recall:    recall,
			F1:        f1,
			Support:   support,
			TP:        ctr.tp,
			FP:        ctr.fp,
			FN:        ctr.fn,
		}
	}

	var accuracy float64
	if total > 0 {
		accuracy = float64(correct) / float64(total)
	}

	var ruleConfidentRate, lowConfRate float64
	if total > 0 {
		ruleConfidentRate = float64(ruleConfident) / float64(total)
		lowConfRate = float64(lowConf) / float64(total)
	}

	return EvalResult{
		Total:             total,
		Correct:           correct,
		Accuracy:          accuracy,
		RuleConfidentRate: ruleConfidentRate,
		LowConfRate:       lowConfRate,
		ByIntent:          byIntent,
	}
}

func (r EvalResult) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "=== NLU Evaluation Report ===\n")
	fmt.Fprintf(&b, "Mode: rule-only\n")
	fmt.Fprintf(&b, "Total: %d, Correct: %d, Accuracy: %.1f%%\n", r.Total, r.Correct, r.Accuracy*100)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Rule confident rate: %.1f%% (%d/%d)\n", r.RuleConfidentRate*100, int(r.RuleConfidentRate*float64(r.Total)+0.5), r.Total)
	fmt.Fprintf(&b, "Low confidence rate: %.1f%% (%d/%d)\n", r.LowConfRate*100, int(r.LowConfRate*float64(r.Total)+0.5), r.Total)
	fmt.Fprintf(&b, "\n")

	intents := make([]string, 0, len(r.ByIntent))
	for intent := range r.ByIntent {
		intents = append(intents, intent)
	}
	sort.Strings(intents)

	fmt.Fprintf(&b, "%-26s %-10s %-10s %-10s %s\n", "Intent", "Precision", "Recall", "F1", "Support")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 65))
	for _, intent := range intents {
		m := r.ByIntent[intent]
		fmt.Fprintf(&b, "%-26s %-10.2f %-10.2f %-10.2f %d\n", m.Intent, m.Precision, m.Recall, m.F1, m.Support)
	}

	return b.String()
}

func (r EvalResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

func EvalResultFromJSON(data []byte) (EvalResult, error) {
	var r EvalResult
	return r, json.Unmarshal(data, &r)
}
