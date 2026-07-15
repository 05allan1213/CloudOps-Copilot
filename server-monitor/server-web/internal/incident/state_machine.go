package incident

import (
	"fmt"
	"strings"
	"time"
)

var allowedTransitions = map[Status]map[Status]struct{}{
	StatusDetected: {
		StatusCorrelating: {}, StatusFailed: {}, StatusClosedNoAction: {},
	},
	StatusCorrelating: {
		StatusDiagnosing: {}, StatusResolved: {}, StatusFailed: {}, StatusClosedNoAction: {},
	},
	StatusDiagnosing: {
		StatusDiagnosisCompleted: {}, StatusResolved: {}, StatusFailed: {},
	},
	StatusDiagnosisCompleted: {
		StatusPlanningRemediation: {}, StatusResolved: {}, StatusFailed: {},
	},
	StatusPlanningRemediation: {
		StatusAwaitingApproval: {}, StatusResolved: {}, StatusFailed: {},
	},
	StatusAwaitingApproval: {
		StatusApplyingChange: {}, StatusResolved: {}, StatusFailed: {},
	},
	StatusApplyingChange: {
		StatusVerifying: {}, StatusResolved: {}, StatusFailed: {},
	},
	StatusVerifying: {
		StatusResolved: {}, StatusDiagnosing: {}, StatusFailed: {},
	},
	StatusResolved: {
		StatusDiagnosing: {},
	},
}

// CanTransition reports whether a transition is part of the frozen state machine.
func CanTransition(from, to Status) bool {
	next, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

// Transition moves the aggregate through the state machine and increments Version.
func (i *Incident) Transition(to Status, at time.Time) (EventType, error) {
	if i == nil {
		return "", fmt.Errorf("%w: incident is nil", ErrInvalidArgument)
	}
	if !CanTransition(i.Status, to) {
		return "", fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, i.Status, to)
	}
	previous := i.Status
	i.Status = to
	i.Version++
	i.UpdatedAt = at.UTC()

	eventType := EventStatusChanged
	switch {
	case to == StatusResolved:
		resolvedAt := at.UTC()
		i.ResolvedAt = &resolvedAt
		eventType = EventIncidentResolved
	case previous == StatusResolved && to == StatusDiagnosing:
		i.ResolvedAt = nil
		eventType = EventIncidentReopened
	}
	return eventType, nil
}

// MergeSeverity returns the more severe normalized value.
func MergeSeverity(current, incoming Severity) Severity {
	rank := map[Severity]int{
		SeverityUnknown:  0,
		SeverityInfo:     1,
		SeverityWarning:  2,
		SeverityCritical: 3,
	}
	if rank[incoming] > rank[current] {
		return incoming
	}
	if _, ok := rank[current]; !ok {
		return NormalizeSeverity(string(incoming))
	}
	return current
}

// NormalizeSeverity maps external severity values to the bounded domain enum.
func NormalizeSeverity(value string) Severity {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(SeverityCritical):
		return SeverityCritical
	case string(SeverityWarning):
		return SeverityWarning
	case string(SeverityInfo):
		return SeverityInfo
	default:
		return SeverityUnknown
	}
}

// ApplySignalTimes updates the aggregate with deterministic signal times and severity.
func (i *Incident) ApplySignalTimes(occurredAt, receivedAt time.Time, severity Severity) error {
	if i == nil || occurredAt.IsZero() || receivedAt.IsZero() {
		return fmt.Errorf("%w: signal times are required", ErrInvalidArgument)
	}
	occurredAt = occurredAt.UTC()
	receivedAt = receivedAt.UTC()
	if i.FirstSeenAt.IsZero() || occurredAt.Before(i.FirstSeenAt) {
		i.FirstSeenAt = occurredAt
	}
	if i.LastSeenAt.IsZero() || occurredAt.After(i.LastSeenAt) {
		i.LastSeenAt = occurredAt
	}
	i.Severity = MergeSeverity(i.Severity, severity)
	i.UpdatedAt = receivedAt
	i.Version++
	return nil
}

// CanTransitionAgentRun reports whether a future AgentRun status change is allowed.
func CanTransitionAgentRun(from, to AgentRunStatus) bool {
	switch from {
	case AgentRunPending:
		return to == AgentRunRunning || to == AgentRunCancelled || to == AgentRunFailed
	case AgentRunRunning:
		return to == AgentRunCompleted || to == AgentRunFailed || to == AgentRunCancelled
	default:
		return false
	}
}

// CanTransitionAgentStep reports whether an AgentStep status change is allowed.
func CanTransitionAgentStep(from, to AgentStepStatus) bool {
	switch from {
	case AgentStepPending:
		return to == AgentStepRunning || to == AgentStepFailed || to == AgentStepCancelled
	case AgentStepRunning:
		return to == AgentStepCompleted || to == AgentStepFailed || to == AgentStepCancelled
	default:
		return false
	}
}
