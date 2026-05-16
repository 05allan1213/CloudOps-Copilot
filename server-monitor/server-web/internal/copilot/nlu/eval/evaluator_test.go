package eval

import (
	"math"
	"testing"

	"server-web/internal/copilot/nlu"
)

func TestEvaluate(t *testing.T) {
	classifier := nlu.NewClassifier()
	cases := GoldenSet()
	result := Evaluate(classifier, cases)

	t.Log(result.String())

	if result.Accuracy < 0.8 {
		t.Errorf("Accuracy %.1f%% is below 80%% baseline, see report above", result.Accuracy*100)
	}
}

func TestEvaluate_EmptyCases(t *testing.T) {
	classifier := nlu.NewClassifier()
	result := Evaluate(classifier, nil)

	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
	if result.Correct != 0 {
		t.Errorf("Correct = %d, want 0", result.Correct)
	}
	if result.Accuracy != 0 {
		t.Errorf("Accuracy = %f, want 0", result.Accuracy)
	}
	if result.RuleConfidentRate != 0 {
		t.Errorf("RuleConfidentRate = %f, want 0", result.RuleConfidentRate)
	}
	if result.LowConfRate != 0 {
		t.Errorf("LowConfRate = %f, want 0", result.LowConfRate)
	}
	if len(result.ByIntent) != 0 {
		t.Errorf("ByIntent has %d entries, want 0", len(result.ByIntent))
	}
}

func TestEvaluate_PerfectMatch(t *testing.T) {
	cases := []EvalCase{
		{Input: "当前有哪些告警", WantIntent: nlu.IntentAlertQuery},
		{Input: "最新5条告警事件", WantIntent: nlu.IntentAlertEventQuery},
		{Input: "CPU告警历史", WantIntent: nlu.IntentAlertHistoryQuery},
		{Input: "告警规则列表", WantIntent: nlu.IntentAlertRuleListQuery},
		{Input: "CPU使用率", WantIntent: nlu.IntentMetricQuery},
		{Input: "主机列表", WantIntent: nlu.IntentHostQuery},
		{Input: "诊断HighCPU告警", WantIntent: nlu.IntentDiagnosisRequest},
		{Input: "What can you do?", WantIntent: nlu.IntentGeneralChat},
	}

	classifier := nlu.NewClassifier()
	result := Evaluate(classifier, cases)

	t.Log(result.String())

	if math.Abs(result.Accuracy-1.0) > 1e-9 {
		t.Errorf("Accuracy = %f, want 1.0", result.Accuracy)
	}
	if result.Correct != len(cases) {
		t.Errorf("Correct = %d, want %d", result.Correct, len(cases))
	}

	for intent, m := range result.ByIntent {
		if m.FP != 0 {
			t.Errorf("intent %s: FP = %d, want 0", intent, m.FP)
		}
		if m.FN != 0 {
			t.Errorf("intent %s: FN = %d, want 0", intent, m.FN)
		}
		if math.Abs(m.Precision-1.0) > 1e-9 {
			t.Errorf("intent %s: Precision = %f, want 1.0", intent, m.Precision)
		}
		if math.Abs(m.Recall-1.0) > 1e-9 {
			t.Errorf("intent %s: Recall = %f, want 1.0", intent, m.Recall)
		}
		if math.Abs(m.F1-1.0) > 1e-9 {
			t.Errorf("intent %s: F1 = %f, want 1.0", intent, m.F1)
		}
	}
}
