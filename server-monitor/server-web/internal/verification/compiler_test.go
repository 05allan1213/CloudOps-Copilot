package verification

import (
	"strings"
	"testing"
	"time"
)

func trustedSubject() Subject {
	return Subject{Repository: "acme/gitops", PullRequest: 42, Revision: strings.Repeat("a", 40), ArgoApplication: "payments", ArgoProject: "staging", Cluster: "staging", Environment: "staging", Namespace: "payments", WorkloadKind: "Deployment", WorkloadName: "api", AlertFingerprint: strings.Repeat("b", 64)}
}

func TestCompileTrustedPlanIsDeterministicAndBounded(t *testing.T) {
	cfg := CompilerConfig{PollInterval: 10 * time.Second, Timeout: 10 * time.Minute, StabilityWindow: 30 * time.Second, AlertLookback: 15 * time.Minute}
	first, err := CompileTrustedPlan(trustedSubject(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileTrustedPlan(trustedSubject(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Checks) != 6 || len(second.Checks) != len(first.Checks) {
		t.Fatalf("unexpected compiled checks: %d", len(first.Checks))
	}
	for i := range first.Checks {
		if first.Checks[i].Type != second.Checks[i].Type || string(first.Checks[i].Expected) != string(second.Checks[i].Expected) || !first.Checks[i].Required {
			t.Fatalf("non-deterministic check %d", i)
		}
	}
	if err := ValidatePlan(first); err != nil {
		t.Fatal(err)
	}
}

func TestCompilerRejectsUntrustedAndUnsupportedInputs(t *testing.T) {
	cfg := CompilerConfig{PollInterval: 10 * time.Second, Timeout: 10 * time.Minute, StabilityWindow: 30 * time.Second, AlertLookback: 15 * time.Minute}
	for name, mutate := range map[string]func(*Subject){
		"branch_revision": func(s *Subject) { s.Revision = "main" },
		"arbitrary_kind":  func(s *Subject) { s.WorkloadKind = "StatefulSet" },
		"missing_cluster": func(s *Subject) { s.Cluster = "" },
		"missing_alert":   func(s *Subject) { s.AlertFingerprint = "" },
	} {
		t.Run(name, func(t *testing.T) {
			subject := trustedSubject()
			mutate(&subject)
			if _, err := CompileTrustedPlan(subject, cfg); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	plan, err := CompileTrustedPlan(trustedSubject(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan.Checks[0].Type = CheckMetricThreshold
	plan.Checks[0].Expected = []byte(`{"promql":"up"}`)
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("arbitrary metric plan must be rejected without a trusted template adapter")
	}
}
