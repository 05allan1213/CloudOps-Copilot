package businessbudget

import "testing"

func TestFrozenBusinessBudgetKindsAndOutcomes(t *testing.T) {
	if DefaultLimit != 3 || HardLimit != 5 {
		t.Fatalf("business budget=%d/%d, want 3/5", DefaultLimit, HardLimit)
	}
	for _, kind := range []Kind{KindAgentRun, KindRemediationPlan, KindVerificationRun} {
		if !kind.Valid() {
			t.Fatalf("kind %q is invalid", kind)
		}
		for count, want := range map[int]Outcome{
			0: OutcomeAllowed, 2: OutcomeAllowed, 3: OutcomeDefaultExhausted,
			4: OutcomeDefaultExhausted, 5: OutcomeHardExhausted, 6: OutcomeHardExhausted,
		} {
			if got := budgetOutcome(kind, count, false).Outcome; got != want {
				t.Fatalf("kind=%s count=%d outcome=%s want=%s", kind, count, got, want)
			}
		}
	}
	if Kind("task").Valid() {
		t.Fatal("unknown budget kind is valid")
	}
}

func TestAuthorizedOutcomeStillHonorsHardLimit(t *testing.T) {
	if got := budgetOutcome(KindAgentRun, 4, true).Outcome; got != OutcomeAllowed {
		t.Fatalf("authorized slot five outcome=%s", got)
	}
	if got := budgetOutcome(KindAgentRun, 5, true).Outcome; got != OutcomeHardExhausted {
		t.Fatalf("authorized slot six outcome=%s", got)
	}
}
