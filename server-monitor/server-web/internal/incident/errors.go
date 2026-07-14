// Package incident defines the framework-independent Incident domain contract.
package incident

import "errors"

var (
	// ErrInvalidTransition indicates that a requested Incident state transition is not allowed.
	ErrInvalidTransition = errors.New("invalid incident transition")
	// ErrInvalidArgument indicates malformed domain input.
	ErrInvalidArgument = errors.New("invalid incident argument")
	// ErrNotFound indicates that a requested Incident-domain object does not exist.
	ErrNotFound = errors.New("incident object not found")
	// ErrConflict indicates optimistic-lock or uniqueness contention.
	ErrConflict = errors.New("incident conflict")
	// ErrUnavailable indicates that Incident persistence is unavailable.
	ErrUnavailable = errors.New("incident persistence unavailable")
)
