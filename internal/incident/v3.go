package incident

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// V3Status is the seven-state Incident lifecycle exposed by /api/v3.
// The legacy Status type remains unchanged until the Phase 7 cutover.
type V3Status string

const (
	V3StatusDetected         V3Status = "detected"
	V3StatusInvestigating    V3Status = "investigating"
	V3StatusAwaitingApproval V3Status = "awaiting_approval"
	V3StatusDelivering       V3Status = "delivering"
	V3StatusVerifying        V3Status = "verifying"
	V3StatusResolved         V3Status = "resolved"
	V3StatusClosed           V3Status = "closed"
)

var v3Transitions = map[V3Status]map[V3Status]struct{}{
	V3StatusDetected: {
		V3StatusInvestigating: {},
		V3StatusClosed:        {},
	},
	V3StatusInvestigating: {
		V3StatusAwaitingApproval: {},
		V3StatusVerifying:        {},
		V3StatusClosed:           {},
	},
	V3StatusAwaitingApproval: {
		V3StatusDelivering:    {},
		V3StatusInvestigating: {},
		V3StatusClosed:        {},
	},
	V3StatusDelivering: {
		V3StatusVerifying:     {},
		V3StatusInvestigating: {},
	},
	V3StatusVerifying: {
		V3StatusResolved:      {},
		V3StatusInvestigating: {},
	},
	V3StatusResolved: {
		V3StatusInvestigating: {},
	},
}

var (
	ErrV3ExpectedVersion = errors.New("v3 incident expected version mismatch")
	ErrV3CycleMismatch   = errors.New("v3 incident cycle mismatch")
	ErrV3CloseBlocked    = errors.New("v3 incident close is blocked")
	ErrV3Verification    = errors.New("v3 incident resolution requires passing verification")
)

// AllV3Statuses returns the complete, frozen V3 status set.
func AllV3Statuses() []V3Status {
	return []V3Status{
		V3StatusDetected,
		V3StatusInvestigating,
		V3StatusAwaitingApproval,
		V3StatusDelivering,
		V3StatusVerifying,
		V3StatusResolved,
		V3StatusClosed,
	}
}

// CanTransitionV3 reports whether the target transition belongs to the V3 graph.
func CanTransitionV3(from, to V3Status) bool {
	_, ok := v3Transitions[from][to]
	return ok
}

// IncidentV3 is the compatibility-binary projection for new V3 rows. Legacy
// rows are not converted or inferred by this model during Phase 2.
type IncidentV3 struct {
	ID                 uint64
	PublicID           string
	CorrelationKey     string
	CorrelationVersion uint16
	CycleNo            uint64
	Severity           Severity
	Status             V3Status
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

// Validate rejects malformed new V3 aggregate state without interpreting any
// legacy row as V3 state.
func (i IncidentV3) Validate() error {
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
	if _, ok := v3Transitions[i.Status]; !ok && i.Status != V3StatusClosed {
		return fmt.Errorf("%w: unknown v3 status %q", ErrInvalidArgument, i.Status)
	}
	if i.Status == V3StatusResolved && i.ResolvedAt == nil {
		return fmt.Errorf("%w: resolved_at is required", ErrInvalidArgument)
	}
	if i.Status != V3StatusResolved && i.ResolvedAt != nil {
		return fmt.Errorf("%w: resolved_at is only valid for resolved", ErrInvalidArgument)
	}
	if (i.Status == V3StatusResolved || i.Status == V3StatusClosed) && i.TerminalAt == nil {
		return fmt.Errorf("%w: terminal_at is required", ErrInvalidArgument)
	}
	return nil
}

// Transition applies an ordinary V3 transition under optimistic-version and
// cycle fencing. Resolving uses ResolveFromVerification instead.
func (i *IncidentV3) Transition(expectedVersion, expectedCycle uint64, to V3Status, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if to == V3StatusResolved {
		return ErrV3Verification
	}
	if to == V3StatusClosed {
		return ErrV3CloseBlocked
	}
	if i.Status == V3StatusResolved {
		return fmt.Errorf("%w: resolved incidents require Reopen", ErrInvalidTransition)
	}
	if !CanTransitionV3(i.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, i.Status, to)
	}
	i.Status = to
	i.Version++
	i.UpdatedAt = at.UTC()
	if to == V3StatusClosed {
		terminal := at.UTC()
		i.TerminalAt = &terminal
	}
	return nil
}

// Close applies the V3 close guard before using the normal transition graph.
func (i *IncidentV3) Close(expectedVersion, expectedCycle uint64, guard CloseGuard, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if guard.HasChangeRequest || guard.ExternalWriteStarted || guard.ExternalResultUnknown || guard.HasActiveVerification {
		return ErrV3CloseBlocked
	}
	if !CanTransitionV3(i.Status, V3StatusClosed) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, i.Status, V3StatusClosed)
	}
	terminal := at.UTC()
	i.Status = V3StatusClosed
	i.TerminalAt = &terminal
	i.Version++
	i.UpdatedAt = terminal
	return nil
}

// ResolveFromVerification is the only aggregate method that enters resolved.
func (i *IncidentV3) ResolveFromVerification(expectedVersion, expectedCycle uint64, verificationPassed bool, at time.Time) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if !verificationPassed || i.Status != V3StatusVerifying {
		return ErrV3Verification
	}
	resolved := at.UTC()
	i.Status = V3StatusResolved
	i.ResolvedAt = &resolved
	i.TerminalAt = &resolved
	i.Version++
	i.UpdatedAt = resolved
	return nil
}

// Reopen begins an isolated cycle. The repository owns the 30-minute MySQL
// NOW(6) window and latest-terminal-row checks before calling this method.
func (i *IncidentV3) Reopen(expectedVersion, expectedCycle uint64, at time.Time, incoming Severity) error {
	if err := i.checkFence(expectedVersion, expectedCycle, at); err != nil {
		return err
	}
	if i.Status != V3StatusResolved {
		return fmt.Errorf("%w: only resolved incidents reopen", ErrInvalidTransition)
	}
	if !IsValidSeverity(incoming) {
		return fmt.Errorf("%w: unknown severity %q", ErrInvalidArgument, incoming)
	}
	i.CycleNo++
	i.Version++
	i.Status = V3StatusInvestigating
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
func (i *IncidentV3) EscalateSeverity(expectedVersion, expectedCycle uint64, incoming Severity, at time.Time) error {
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
func (i *IncidentV3) Block(expectedVersion, expectedCycle uint64, reason string, at time.Time) error {
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

func (i *IncidentV3) checkFence(expectedVersion, expectedCycle uint64, at time.Time) error {
	if i == nil || at.IsZero() {
		return fmt.Errorf("%w: incident and time are required", ErrInvalidArgument)
	}
	if i.Version != expectedVersion {
		return ErrV3ExpectedVersion
	}
	if i.CycleNo != expectedCycle {
		return ErrV3CycleMismatch
	}
	return nil
}
