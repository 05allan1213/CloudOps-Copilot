package incident

import (
	"errors"
	"testing"
	"time"
)

func TestStatusSetAndTransitions(t *testing.T) {
	statuses := AllStatuses()
	if len(statuses) != 7 {
		t.Fatalf("status count=%d, want 7", len(statuses))
	}
	allowed := [][2]Status{
		{StatusDetected, StatusInvestigating},
		{StatusInvestigating, StatusAwaitingApproval},
		{StatusInvestigating, StatusVerifying},
		{StatusAwaitingApproval, StatusDelivering},
		{StatusAwaitingApproval, StatusInvestigating},
		{StatusDelivering, StatusVerifying},
		{StatusDelivering, StatusInvestigating},
		{StatusVerifying, StatusResolved},
		{StatusVerifying, StatusInvestigating},
		{StatusResolved, StatusInvestigating},
		{StatusResolved, StatusClosed},
	}
	for _, transition := range allowed {
		if !CanTransition(transition[0], transition[1]) {
			t.Errorf("expected transition %s -> %s", transition[0], transition[1])
		}
	}
	if CanTransition(StatusClosed, StatusInvestigating) || CanTransition(StatusDetected, StatusResolved) {
		t.Fatal("forbidden transition accepted")
	}
}

func TestV3IncidentFencingResolutionAndReopen(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 123456000, time.UTC)
	item := Incident{
		ID: 1, PublicID: "123e4567-e89b-12d3-a456-426614174000",
		CorrelationKey: "semantic-test-correlation", CorrelationVersion: 2, CycleNo: 1,
		Severity: SeverityWarning, Status: StatusDetected, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := item.Transition(1, 1, StatusInvestigating, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := item.Transition(1, 1, StatusVerifying, now); !errors.Is(err, ErrExpectedVersion) {
		t.Fatalf("stale expected version error=%v", err)
	}
	if err := item.Transition(2, 2, StatusVerifying, now); !errors.Is(err, ErrCycleMismatch) {
		t.Fatalf("cross-cycle error=%v", err)
	}
	if err := item.Transition(2, 1, StatusVerifying, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := item.Transition(3, 1, StatusResolved, now); !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("ordinary resolution error=%v", err)
	}
	if err := item.ResolveFromVerification(3, 1, false, now); !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("non-passing resolution error=%v", err)
	}
	if err := item.ResolveFromVerification(3, 1, true, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusResolved || item.ResolvedAt == nil || item.TerminalAt == nil {
		t.Fatalf("resolved projection=%+v", item)
	}
	if err := item.Reopen(4, 1, now.Add(4*time.Second), SeverityCritical); err != nil {
		t.Fatal(err)
	}
	if item.CycleNo != 2 || item.Status != StatusInvestigating || item.ResolvedAt != nil || item.TerminalAt != nil || item.Severity != SeverityCritical {
		t.Fatalf("reopen projection=%+v", item)
	}
}

func TestV3CloseAndBlockContracts(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	proof := CloseGuard{HasPassingVerification: true, CommonWindowComplete: true, HasResolutionReport: true}
	for name, guard := range map[string]CloseGuard{
		"missing verification": {CommonWindowComplete: true, HasResolutionReport: true},
		"missing window":       {HasPassingVerification: true, HasResolutionReport: true},
		"missing report":       {HasPassingVerification: true, CommonWindowComplete: true},
		"active effect":        {HasPassingVerification: true, CommonWindowComplete: true, HasResolutionReport: true, HasActiveExternalEffect: true},
		"unknown effect":       {HasPassingVerification: true, CommonWindowComplete: true, HasResolutionReport: true, ExternalResultUnknown: true},
		"active verification":  {HasPassingVerification: true, CommonWindowComplete: true, HasResolutionReport: true, HasActiveVerification: true},
		"active task":          {HasPassingVerification: true, CommonWindowComplete: true, HasResolutionReport: true, HasActiveTask: true},
	} {
		t.Run(name, func(t *testing.T) {
			resolvedAt := now.Add(-time.Minute)
			item := Incident{CycleNo: 1, Version: 1, Status: StatusResolved, ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt}
			if err := item.Close(1, 1, guard, now); !errors.Is(err, ErrCloseBlocked) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	directClose := Incident{CycleNo: 1, Version: 1, Status: StatusInvestigating}
	if err := directClose.Transition(1, 1, StatusClosed, now); !errors.Is(err, ErrCloseBlocked) {
		t.Fatalf("direct close error=%v", err)
	}
	if err := directClose.Close(1, 1, proof, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unresolved close error=%v", err)
	}
	resolvedAt := now.Add(-time.Minute)
	directClose = Incident{CycleNo: 1, Version: 1, Status: StatusResolved, ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt}
	if err := directClose.Close(1, 1, proof, now); err != nil {
		t.Fatalf("guarded close error=%v", err)
	}
	if directClose.Status != StatusClosed || directClose.TerminalAt == nil || directClose.ResolvedAt == nil || directClose.Version != 2 {
		t.Fatalf("guarded close projection=%+v", directClose)
	}
	directReopen := Incident{
		CycleNo: 1, Version: 1, Status: StatusResolved,
		ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt,
	}
	if err := directReopen.Transition(1, 1, StatusInvestigating, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct reopen error=%v", err)
	}
	if directReopen.CycleNo != 1 || directReopen.ResolvedAt == nil || directReopen.TerminalAt == nil {
		t.Fatalf("direct reopen mutated aggregate=%+v", directReopen)
	}
	item := Incident{CycleNo: 1, Version: 1, Status: StatusInvestigating, Severity: SeverityCritical}
	if err := item.Block(1, 1, "dependency_unavailable", now); err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusInvestigating || !item.NeedsAttention || item.Version != 2 {
		t.Fatalf("blocked projection=%+v", item)
	}
	if err := item.EscalateSeverity(2, 1, SeverityInfo, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if item.Version != 2 || item.Severity != SeverityCritical {
		t.Fatal("severity downgrade changed the aggregate")
	}
}

func TestV3ReopenStartsNewCycleWithIncomingSeverity(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	resolvedAt := now.Add(-time.Minute)
	item := Incident{
		CycleNo: 1, Version: 3, Status: StatusResolved, Severity: SeverityCritical,
		ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt,
	}
	if err := item.Reopen(3, 1, now, SeverityWarning); err != nil {
		t.Fatal(err)
	}
	if item.CycleNo != 2 || item.Severity != SeverityWarning || item.ResolvedAt != nil || item.TerminalAt != nil {
		t.Fatalf("reopened aggregate=%+v", item)
	}
}

func TestV3RejectsSeverityOutsideBoundedEnum(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	invalid := Incident{
		ID: 1, PublicID: "123e4567-e89b-12d3-a456-426614174000",
		CorrelationKey: "semantic-test-correlation", CorrelationVersion: 2, CycleNo: 1,
		Severity: Severity("fatal"), Status: StatusDetected, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid aggregate severity error=%v", err)
	}

	resolvedAt := now.Add(-time.Minute)
	resolved := Incident{
		CycleNo: 1, Version: 1, Status: StatusResolved, Severity: SeverityWarning,
		ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt,
	}
	if err := resolved.Reopen(1, 1, now, Severity("fatal")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid reopen severity error=%v", err)
	}
	if resolved.CycleNo != 1 || resolved.Status != StatusResolved || resolved.Severity != SeverityWarning {
		t.Fatalf("invalid reopen mutated aggregate=%+v", resolved)
	}

	active := Incident{CycleNo: 1, Version: 1, Status: StatusInvestigating, Severity: SeverityWarning}
	if err := active.EscalateSeverity(1, 1, Severity("fatal"), now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid escalation severity error=%v", err)
	}
	if active.Version != 1 || active.Severity != SeverityWarning {
		t.Fatalf("invalid escalation mutated aggregate=%+v", active)
	}
}
