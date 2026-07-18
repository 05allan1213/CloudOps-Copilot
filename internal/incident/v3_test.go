package incident

import (
	"errors"
	"testing"
	"time"
)

func TestV3StatusSetAndTransitions(t *testing.T) {
	statuses := AllV3Statuses()
	if len(statuses) != 7 {
		t.Fatalf("status count=%d, want 7", len(statuses))
	}
	allowed := [][2]V3Status{
		{V3StatusDetected, V3StatusInvestigating},
		{V3StatusDetected, V3StatusClosed},
		{V3StatusInvestigating, V3StatusAwaitingApproval},
		{V3StatusInvestigating, V3StatusVerifying},
		{V3StatusInvestigating, V3StatusClosed},
		{V3StatusAwaitingApproval, V3StatusDelivering},
		{V3StatusAwaitingApproval, V3StatusInvestigating},
		{V3StatusAwaitingApproval, V3StatusClosed},
		{V3StatusDelivering, V3StatusVerifying},
		{V3StatusDelivering, V3StatusInvestigating},
		{V3StatusVerifying, V3StatusResolved},
		{V3StatusVerifying, V3StatusInvestigating},
		{V3StatusResolved, V3StatusInvestigating},
	}
	for _, transition := range allowed {
		if !CanTransitionV3(transition[0], transition[1]) {
			t.Errorf("expected transition %s -> %s", transition[0], transition[1])
		}
	}
	if CanTransitionV3(V3StatusClosed, V3StatusInvestigating) || CanTransitionV3(V3StatusDetected, V3StatusResolved) {
		t.Fatal("forbidden transition accepted")
	}
}

func TestV3IncidentFencingResolutionAndReopen(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 123456000, time.UTC)
	item := IncidentV3{
		ID: 1, PublicID: "123e4567-e89b-12d3-a456-426614174000",
		CorrelationKey: "v2:test", CorrelationVersion: 2, CycleNo: 1,
		Severity: SeverityWarning, Status: V3StatusDetected, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := item.Transition(1, 1, V3StatusInvestigating, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := item.Transition(1, 1, V3StatusVerifying, now); !errors.Is(err, ErrV3ExpectedVersion) {
		t.Fatalf("stale expected version error=%v", err)
	}
	if err := item.Transition(2, 2, V3StatusVerifying, now); !errors.Is(err, ErrV3CycleMismatch) {
		t.Fatalf("cross-cycle error=%v", err)
	}
	if err := item.Transition(2, 1, V3StatusVerifying, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := item.Transition(3, 1, V3StatusResolved, now); !errors.Is(err, ErrV3Verification) {
		t.Fatalf("ordinary resolution error=%v", err)
	}
	if err := item.ResolveFromVerification(3, 1, false, now); !errors.Is(err, ErrV3Verification) {
		t.Fatalf("non-passing resolution error=%v", err)
	}
	if err := item.ResolveFromVerification(3, 1, true, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if item.Status != V3StatusResolved || item.ResolvedAt == nil || item.TerminalAt == nil {
		t.Fatalf("resolved projection=%+v", item)
	}
	if err := item.Reopen(4, 1, now.Add(4*time.Second), SeverityCritical); err != nil {
		t.Fatal(err)
	}
	if item.CycleNo != 2 || item.Status != V3StatusInvestigating || item.ResolvedAt != nil || item.TerminalAt != nil || item.Severity != SeverityCritical {
		t.Fatalf("reopen projection=%+v", item)
	}
}

func TestV3CloseAndBlockContracts(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	for name, guard := range map[string]CloseGuard{
		"change":       {HasChangeRequest: true},
		"write":        {ExternalWriteStarted: true},
		"unknown":      {ExternalResultUnknown: true},
		"verification": {HasActiveVerification: true},
	} {
		t.Run(name, func(t *testing.T) {
			item := IncidentV3{CycleNo: 1, Version: 1, Status: V3StatusInvestigating}
			if err := item.Close(1, 1, guard, now); !errors.Is(err, ErrV3CloseBlocked) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	directClose := IncidentV3{CycleNo: 1, Version: 1, Status: V3StatusInvestigating}
	if err := directClose.Transition(1, 1, V3StatusClosed, now); !errors.Is(err, ErrV3CloseBlocked) {
		t.Fatalf("direct close error=%v", err)
	}
	if err := directClose.Close(1, 1, CloseGuard{}, now); err != nil {
		t.Fatalf("guarded close error=%v", err)
	}
	if directClose.Status != V3StatusClosed || directClose.TerminalAt == nil || directClose.Version != 2 {
		t.Fatalf("guarded close projection=%+v", directClose)
	}
	resolvedAt := now.Add(-time.Minute)
	directReopen := IncidentV3{
		CycleNo: 1, Version: 1, Status: V3StatusResolved,
		ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt,
	}
	if err := directReopen.Transition(1, 1, V3StatusInvestigating, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct reopen error=%v", err)
	}
	if directReopen.CycleNo != 1 || directReopen.ResolvedAt == nil || directReopen.TerminalAt == nil {
		t.Fatalf("direct reopen mutated aggregate=%+v", directReopen)
	}
	item := IncidentV3{CycleNo: 1, Version: 1, Status: V3StatusInvestigating, Severity: SeverityCritical}
	if err := item.Block(1, 1, "dependency_unavailable", now); err != nil {
		t.Fatal(err)
	}
	if item.Status != V3StatusInvestigating || !item.NeedsAttention || item.Version != 2 {
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
	item := IncidentV3{
		CycleNo: 1, Version: 3, Status: V3StatusResolved, Severity: SeverityCritical,
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
	invalid := IncidentV3{
		ID: 1, PublicID: "123e4567-e89b-12d3-a456-426614174000",
		CorrelationKey: "v2:test", CorrelationVersion: 2, CycleNo: 1,
		Severity: Severity("fatal"), Status: V3StatusDetected, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid aggregate severity error=%v", err)
	}

	resolvedAt := now.Add(-time.Minute)
	resolved := IncidentV3{
		CycleNo: 1, Version: 1, Status: V3StatusResolved, Severity: SeverityWarning,
		ResolvedAt: &resolvedAt, TerminalAt: &resolvedAt,
	}
	if err := resolved.Reopen(1, 1, now, Severity("fatal")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid reopen severity error=%v", err)
	}
	if resolved.CycleNo != 1 || resolved.Status != V3StatusResolved || resolved.Severity != SeverityWarning {
		t.Fatalf("invalid reopen mutated aggregate=%+v", resolved)
	}

	active := IncidentV3{CycleNo: 1, Version: 1, Status: V3StatusInvestigating, Severity: SeverityWarning}
	if err := active.EscalateSeverity(1, 1, Severity("fatal"), now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid escalation severity error=%v", err)
	}
	if active.Version != 1 || active.Severity != SeverityWarning {
		t.Fatalf("invalid escalation mutated aggregate=%+v", active)
	}
}
