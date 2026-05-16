package eval

import (
	"testing"

	"server-web/internal/copilot/nlu"
)

func TestEvaluateMulti(t *testing.T) {
	classifier := nlu.NewClassifier()
	cases := MultiEvalSet()
	result := EvaluateMulti(classifier, cases)

	t.Log(result.String())

	if result.ExactMatchRate < 0.5 {
		t.Errorf("ExactMatchRate %.1f%% is below 50%% baseline, see report above", result.ExactMatchRate*100)
	}
}

func TestEvaluateMulti_EmptyCases(t *testing.T) {
	classifier := nlu.NewClassifier()
	result := EvaluateMulti(classifier, nil)

	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
	if result.ExactMatch != 0 {
		t.Errorf("ExactMatch = %d, want 0", result.ExactMatch)
	}
	if result.SplitAccuracy != 0 {
		t.Errorf("SplitAccuracy = %f, want 0", result.SplitAccuracy)
	}
	if len(result.ByCase) != 0 {
		t.Errorf("ByCase has %d entries, want 0", len(result.ByCase))
	}
}

func TestEvaluateMulti_SingleIntentCases(t *testing.T) {
	cases := []MultiEvalCase{
		{Input: "当前有哪些告警", WantIntents: []string{"alert_query"}},
		{Input: "CPU使用率", WantIntents: []string{"metric_query"}},
		{Input: "主机列表", WantIntents: []string{"host_query"}},
		{Input: "诊断HighCPU告警", WantIntents: []string{"diagnosis_request"}},
		{Input: "你好", WantIntents: []string{"general_chat"}},
	}

	classifier := nlu.NewClassifier()
	result := EvaluateMulti(classifier, cases)

	t.Log(result.String())

	if result.SingleIntentTotal != len(cases) {
		t.Errorf("SingleIntentTotal = %d, want %d", result.SingleIntentTotal, len(cases))
	}

	for _, c := range result.ByCase {
		if !c.SplitCorrect {
			t.Errorf("input %q: got %v, want %v (split correct)", c.Input, c.GotIntents, c.WantIntents)
		}
	}
}

func TestEvaluateMulti_MultiIntentCases(t *testing.T) {
	cases := []MultiEvalCase{
		{Input: "查告警并看主机", WantIntents: []string{"alert_query", "host_query"}},
		{Input: "查看 node-1 的 CPU 和内存", WantIntents: []string{"metric_query", "metric_query"}},
		{Input: "查告警然后看主机再查指标", WantIntents: []string{"alert_query", "host_query", "metric_query"}},
	}

	classifier := nlu.NewClassifier()
	result := EvaluateMulti(classifier, cases)

	t.Log(result.String())

	if result.Total != len(cases) {
		t.Errorf("Total = %d, want %d", result.Total, len(cases))
	}
}
