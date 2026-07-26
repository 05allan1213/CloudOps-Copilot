package verification

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeliveryStateMachine(t *testing.T) {
	valid := [][2]string{{"pr_created", "ci_pending"}, {"ci_pending", "ci_passed"}, {"ci_passed", "merge_pending"}, {"merge_pending", "merged"}, {"merged", "argocd_pending"}, {"argocd_pending", "syncing"}, {"syncing", "synced"}, {"synced", "rollout_pending"}, {"rollout_pending", "delivered"}}
	for _, transition := range valid {
		if !CanTransitionDelivery(transition[0], transition[1]) {
			t.Fatalf("expected %s -> %s", transition[0], transition[1])
		}
	}
	invalid := [][2]string{{"pr_created", "delivered"}, {"ci_failed", "ci_pending"}, {"delivered", "rollout_pending"}, {"merged", "delivered"}}
	for _, transition := range invalid {
		if CanTransitionDelivery(transition[0], transition[1]) {
			t.Fatalf("unexpected %s -> %s", transition[0], transition[1])
		}
	}
}

func TestRunAndCheckTerminalStatesAreImmutable(t *testing.T) {
	if !CanTransitionRun(RunPending, RunRunning) || !CanTransitionRun(RunRunning, RunPassed) {
		t.Fatal("expected normal run transitions")
	}
	if CanTransitionRun(RunPassed, RunRunning) || CanTransitionRun(RunFailed, RunRunning) {
		t.Fatal("terminal run must be immutable")
	}
	if CanTransitionCheck(CheckPassed, CheckRunning) || CanTransitionCheck(CheckFailed, CheckRunning) {
		t.Fatal("terminal check must be immutable")
	}
}

func TestApplySampleRecordsContinuousStabilityForCommonWindow(t *testing.T) {
	base := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	check := Check{Status: CheckPending, StabilityWindow: 20 * time.Second}
	passed := Sample{Status: SamplePassed, Observed: json.RawMessage(`{"healthy":true}`)}
	if err := ApplySample(&check, passed, base); err != nil || check.Status != CheckRunning {
		t.Fatalf("first success must only start window: status=%s err=%v", check.Status, err)
	}
	if err := ApplySample(&check, Sample{Status: SamplePending, Observed: json.RawMessage(`{"healthy":false}`)}, base.Add(10*time.Second)); err != nil || check.ConsecutiveSuccessSince != nil {
		t.Fatalf("failure inside window must reset: %+v err=%v", check, err)
	}
	if err := ApplySample(&check, passed, base.Add(20*time.Second)); err != nil || check.Status != CheckRunning {
		t.Fatalf("second first-success must restart window: status=%s err=%v", check.Status, err)
	}
	if err := ApplySample(&check, passed, base.Add(40*time.Second)); err != nil || check.Status != CheckRunning || check.ConsecutiveSuccessSince == nil {
		t.Fatalf("continuous success must remain available to the common window: status=%s err=%v", check.Status, err)
	}
	if err := ApplySample(&check, passed, base.Add(time.Minute)); err != nil {
		t.Fatalf("revalidation inside the run must remain accepted: %v", err)
	}
}

func TestUnavailableIsNeverPassAndAggregateRequiresAllRequired(t *testing.T) {
	check := Check{Status: CheckRunning, StabilityWindow: time.Second}
	if err := ApplySample(&check, Sample{Status: SampleUnavailable, Observed: json.RawMessage(`{}`), ReasonCode: "provider_unavailable"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if check.Status != CheckUnavailable || check.ConsecutiveSuccessSince != nil {
		t.Fatalf("unavailable must not pass: %+v", check)
	}
	status, reason, terminal := Aggregate([]Check{{Required: true, Status: CheckPassed}, {Required: true, Status: CheckUnavailable}, {Required: false, Status: CheckFailed}})
	if status != RunRunning || terminal || reason != "checks_pending" {
		t.Fatalf("required unavailable must remain pending: %s %s %v", status, reason, terminal)
	}
	status, _, terminal = Aggregate([]Check{{Required: true, Status: CheckPassed}, {Required: false, Status: CheckFailed}})
	if status != RunPassed || !terminal {
		t.Fatalf("optional failure must be recorded but not block required aggregate: %s %v", status, terminal)
	}
	status, reason, terminal = Aggregate([]Check{{Required: true, Status: CheckPassed}, {Required: true, Status: CheckFailed, FailureReason: "rollout_failed"}})
	if status != RunFailed || !terminal || reason == "" {
		t.Fatalf("required failure must fail Verification: %s %s %v", status, reason, terminal)
	}
}
