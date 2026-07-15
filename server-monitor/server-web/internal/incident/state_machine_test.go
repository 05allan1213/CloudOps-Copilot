package incident

import (
	"errors"
	"testing"
	"time"
)

func TestStateMachineAllPairs(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusDetected, StatusCorrelating}: true, {StatusDetected, StatusFailed}: true, {StatusDetected, StatusClosedNoAction}: true,
		{StatusCorrelating, StatusDiagnosing}: true, {StatusCorrelating, StatusResolved}: true, {StatusCorrelating, StatusFailed}: true, {StatusCorrelating, StatusClosedNoAction}: true,
		{StatusDiagnosing, StatusDiagnosisCompleted}: true, {StatusDiagnosing, StatusResolved}: true, {StatusDiagnosing, StatusFailed}: true,
		{StatusDiagnosisCompleted, StatusPlanningRemediation}: true, {StatusDiagnosisCompleted, StatusResolved}: true, {StatusDiagnosisCompleted, StatusFailed}: true,
		{StatusPlanningRemediation, StatusAwaitingApproval}: true, {StatusPlanningRemediation, StatusResolved}: true, {StatusPlanningRemediation, StatusFailed}: true,
		{StatusAwaitingApproval, StatusApplyingChange}: true, {StatusAwaitingApproval, StatusResolved}: true, {StatusAwaitingApproval, StatusFailed}: true,
		{StatusApplyingChange, StatusVerifying}: true, {StatusApplyingChange, StatusResolved}: true, {StatusApplyingChange, StatusFailed}: true,
		{StatusVerifying, StatusResolved}: true, {StatusVerifying, StatusDiagnosing}: true, {StatusVerifying, StatusFailed}: true,
		{StatusResolved, StatusDiagnosing}: true,
	}
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	for _, from := range AllStatuses() {
		for _, to := range AllStatuses() {
			from, to := from, to
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				item := Incident{Status: from, Version: 7}
				event, err := item.Transition(to, now)
				if allowed[[2]Status{from, to}] {
					if err != nil {
						t.Fatalf("expected transition: %v", err)
					}
					if item.Version != 8 || item.Status != to {
						t.Fatalf("unexpected aggregate after transition: %+v", item)
					}
					if event == "" {
						t.Fatal("expected timeline event type")
					}
					return
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("expected ErrInvalidTransition, got %v", err)
				}
				if item.Version != 7 || item.Status != from {
					t.Fatalf("illegal transition mutated aggregate: %+v", item)
				}
			})
		}
	}
}

func TestResolvedReopensAndClearsResolvedAt(t *testing.T) {
	resolved := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	item := Incident{Status: StatusResolved, Version: 2, ResolvedAt: &resolved}
	event, err := item.Transition(StatusDiagnosing, resolved.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if event != EventIncidentReopened || item.ResolvedAt != nil || item.Version != 3 {
		t.Fatalf("unexpected reopen result: event=%s incident=%+v", event, item)
	}
}

func TestApplySignalTimesAndSeverity(t *testing.T) {
	base := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)
	item := Incident{Severity: SeverityInfo, FirstSeenAt: base, LastSeenAt: base, Version: 1}
	if err := item.ApplySignalTimes(base.Add(-time.Minute), base.Add(time.Second), SeverityCritical); err != nil {
		t.Fatal(err)
	}
	if !item.FirstSeenAt.Equal(base.Add(-time.Minute)) || !item.LastSeenAt.Equal(base) {
		t.Fatalf("unexpected time merge: %+v", item)
	}
	if item.Severity != SeverityCritical || item.Version != 2 {
		t.Fatalf("unexpected severity/version: %+v", item)
	}
}

func TestMergeSeverity(t *testing.T) {
	tests := []struct {
		current, incoming, want Severity
	}{
		{SeverityUnknown, SeverityInfo, SeverityInfo},
		{SeverityWarning, SeverityInfo, SeverityWarning},
		{SeverityWarning, SeverityCritical, SeverityCritical},
		{SeverityCritical, SeverityUnknown, SeverityCritical},
	}
	for _, tt := range tests {
		if got := MergeSeverity(tt.current, tt.incoming); got != tt.want {
			t.Fatalf("MergeSeverity(%s,%s)=%s, want %s", tt.current, tt.incoming, got, tt.want)
		}
	}
}

func TestAgentRunTransitions(t *testing.T) {
	if !CanTransitionAgentRun(AgentRunPending, AgentRunRunning) || !CanTransitionAgentRun(AgentRunRunning, AgentRunCompleted) {
		t.Fatal("expected normal AgentRun transitions")
	}
	if CanTransitionAgentRun(AgentRunCompleted, AgentRunRunning) || CanTransitionAgentRun(AgentRunPending, AgentRunCompleted) {
		t.Fatal("unexpected AgentRun transition")
	}
}
