package incident

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Status is the seven-state Incident lifecycle exposed by /api/v1.
type Status string

const (
	StatusDetected         Status = "detected"
	StatusInvestigating    Status = "investigating"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusDelivering       Status = "delivering"
	StatusVerifying        Status = "verifying"
	StatusResolved         Status = "resolved"
	StatusClosed           Status = "closed"
)

var transitions = map[Status]map[Status]struct{}{
	StatusDetected: {
		StatusInvestigating: {},
		StatusClosed:        {},
	},
	StatusInvestigating: {
		StatusAwaitingApproval: {},
		StatusVerifying:        {},
		StatusClosed:           {},
	},
	StatusAwaitingApproval: {
		StatusDelivering:    {},
		StatusInvestigating: {},
		StatusClosed:        {},
	},
	StatusDelivering: {
		StatusVerifying:     {},
		StatusInvestigating: {},
	},
	StatusVerifying: {
		StatusResolved:      {},
		StatusInvestigating: {},
	},
	StatusResolved: {
		StatusInvestigating: {},
	},
}

var (
	ErrExpectedVersion      = errors.New("incident expected version mismatch")
	ErrCycleMismatch        = errors.New("incident cycle mismatch")
	ErrCloseBlocked         = errors.New("incident close is blocked")
	ErrVerificationRequired = errors.New("incident resolution requires passing verification")
)

// AllStatuses returns the complete, frozen status set.
func AllStatuses() []Status {
	return []Status{
		StatusDetected,
		StatusInvestigating,
		StatusAwaitingApproval,
		StatusDelivering,
		StatusVerifying,
		StatusResolved,
		StatusClosed,
	}
}

// CanTransition reports whether the target transition belongs to the lifecycle graph.
func CanTransition(from, to Status) bool {
	_, ok := transitions[from][to]
	return ok
}

// Incident is the canonical aggregate for one correlated failure lifecycle.
type Incident struct {
	ID                 uint64
	PublicID           string
	CorrelationKey     string
	CorrelationVersion uint16
	CycleNo            uint64
	Severity           Severity
	Status             Status
	Version            uint64
	NeedsAttention     bool
	BlockingReasonCode string
	BlockedAt          *time.Time
	ResolvedAt         *time.Time
	TerminalAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// CloseGuard captures the child and external-effect facts that must be locked
// and checked in the same transaction as close.
type CloseGuard struct {
	HasChangeRequest      bool
	ExternalWriteStarted  bool
	ExternalResultUnknown bool
	HasActiveVerification bool
}

// Validate rejects malformed aggregate state.
func (i Incident) Validate() error {
	if i.ID == 0 || uuid.Validate(i.PublicID) != nil {
		return fmt.Errorf("%w: numeric and public ids are required", ErrInvalidArgument)
	}
	if strings.TrimSpace(i.CorrelationKey) == "" || i.CorrelationVersion == 0 {
		return fmt.Errorf("%w: correlation identity is required", ErrInvalidArgument)
	}
	if i.CycleNo == 0 || i.Version == 0 {
		return fmt.Errorf("%w: cycle and version must be positive", ErrInvalidArgument)
	}
	if !IsValidSeverity(i.Severity) {
		return fmt.Errorf("%w: unknown severity %q", ErrInvalidArgument, i.Severity)
	}
	if _, ok := transitions[i.Status]; !ok && i.Status != StatusClosed {
		return fmt.Errorf("%w: unknown status %q", ErrInvalidArgument, i.Status)
	}
	if i.Status == StatusResolved && i.ResolvedAt == nil {
		return fmt.Errorf("%w: resolved_at is required", ErrInvalidArgument)
	}
	if i.Status != StatusResolved && i.ResolvedAt != nil {
		return fmt.Errorf("%w: resolved_at is only valid for resolved", ErrInvalidArgument)
	}
	if (i.Status == StatusResolved || i.Status == StatusClosed) && i.TerminalAt == nil {
		return fmt.Errorf("%w: terminal_at is required", ErrInvalidArgument)
	}
	return nil
}

// Transition applies an ordinary transition under optimistic-version and
// cycle fencing. Resolving uses ResolveFromVerification instead.
func (i *Incident) Transition(expectedVersion, expectedCycle uint64, to Status, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if to == StatusResolved {
		return ErrVerificationRequired
	}
	if to == StatusClosed {
		return ErrCloseBlocked
	}
	if i.Status == StatusResolved {
		return fmt.Errorf("%w: resolved incidents require Reopen", ErrInvalidTransition)
	}
	if !CanTransition(i.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, i.Status, to)
	}
	i.Status = to
	i.Version++
	i.UpdatedAt = at.UTC()
	if to == StatusClosed {
		terminal := at.UTC()
		i.TerminalAt = &terminal
	}
	return nil
}

// Close applies the close guard before using the normal transition graph.
func (i *Incident) Close(expectedVersion, expectedCycle uint64, guard CloseGuard, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if guard.HasChangeRequest || guard.ExternalWriteStarted || guard.ExternalResultUnknown || guard.HasActiveVerification {
		return ErrCloseBlocked
	}
	if !CanTransition(i.Status, StatusClosed) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, i.Status, StatusClosed)
	}
	terminal := at.UTC()
	i.Status = StatusClosed
	i.TerminalAt = &terminal
	i.Version++
	i.UpdatedAt = terminal
	return nil
}

// ResolveFromVerification is the only aggregate method that enters resolved.
func (i *Incident) ResolveFromVerification(expectedVersion, expectedCycle uint64, verificationPassed bool, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if !verificationPassed || i.Status != StatusVerifying {
		return ErrVerificationRequired
	}
	resolved := at.UTC()
	i.Status = StatusResolved
	i.ResolvedAt = &resolved
	i.TerminalAt = &resolved
	i.Version++
	i.UpdatedAt = resolved
	return nil
}

// Reopen begins an isolated cycle. The repository owns the 30-minute MySQL
// NOW(6) window and latest-terminal-row checks before calling this method.
func (i *Incident) Reopen(expectedVersion, expectedCycle uint64, at time.Time, incoming Severity) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if i.Status != StatusResolved {
		return fmt.Errorf("%w: only resolved incidents reopen", ErrInvalidTransition)
	}
	if !IsValidSeverity(incoming) {
		return fmt.Errorf("%w: unknown severity %q", ErrInvalidArgument, incoming)
	}
	i.CycleNo++
	i.Version++
	i.Status = StatusInvestigating
	i.Severity = incoming
	i.NeedsAttention = false
	i.BlockingReasonCode = ""
	i.BlockedAt = nil
	i.ResolvedAt = nil
	i.TerminalAt = nil
	i.UpdatedAt = at.UTC()
	return nil
}

// EscalateSeverity never lowers an active Incident's severity.
func (i *Incident) EscalateSeverity(expectedVersion, expectedCycle uint64, incoming Severity, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if !IsValidSeverity(i.Severity) || !IsValidSeverity(incoming) {
		return fmt.Errorf("%w: severity is outside the bounded enum", ErrInvalidArgument)
	}
	merged := MergeSeverity(i.Severity, incoming)
	if merged == i.Severity {
		return nil
	}
	i.Severity = merged
	i.Version++
	i.UpdatedAt = at.UTC()
	return nil
}

// Block records a bounded technical blocker without introducing a failed state.
func (i *Incident) Block(expectedVersion, expectedCycle uint64, reason string, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 128 {
		return fmt.Errorf("%w: blocking reason must be 1..128 bytes", ErrInvalidArgument)
	}
	blocked := at.UTC()
	i.NeedsAttention = true
	i.BlockingReasonCode = reason
	i.BlockedAt = &blocked
	i.Version++
	i.UpdatedAt = blocked
	return nil
}

func (i *Incident) checkFence(expectedVersion, expectedCycle uint64, at time.Time) error {
	if i == nil || at.IsZero() {
		return fmt.Errorf("%w: incident and time are required", ErrInvalidArgument)
	}
	if i.Version != expectedVersion {
		return ErrExpectedVersion
	}
	if i.CycleNo != expectedCycle {
		return ErrCycleMismatch
	}
	return nil
}
