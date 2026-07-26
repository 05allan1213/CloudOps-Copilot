package incident

import "strings"

// Severity is the normalized Incident severity.
type Severity string

const (
	SeverityUnknown  Severity = "unknown"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// SignalStatus is the normalized lifecycle status of an incoming signal.
type SignalStatus string

const (
	SignalStatusFiring   SignalStatus = "firing"
	SignalStatusResolved SignalStatus = "resolved"
)

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

// IsValidSeverity reports whether a value belongs to the bounded domain enum.
func IsValidSeverity(value Severity) bool {
	switch value {
	case SeverityUnknown, SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}
