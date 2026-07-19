// Package agent defines the infrastructure-independent bounded Agent domain.
package agent

import (
	"errors"
	"fmt"
)

// ErrorCode is a bounded failure category safe for persistence and metrics.
type ErrorCode string

const (
	ErrorValidation       ErrorCode = "validation"
	ErrorPermission       ErrorCode = "permission"
	ErrorNotFound         ErrorCode = "not_found"
	ErrorTimeout          ErrorCode = "timeout"
	ErrorRateLimit        ErrorCode = "rate_limit"
	ErrorTemporary        ErrorCode = "temporary_dependency"
	ErrorModelUnavailable ErrorCode = "model_unavailable"
	ErrorMalformedModel   ErrorCode = "malformed_model_output"
	ErrorBudgetExceeded   ErrorCode = "budget_exceeded"
	ErrorCancelled        ErrorCode = "cancelled"
	ErrorInvariant        ErrorCode = "internal_invariant"
	ErrorLeaseLost        ErrorCode = "lease_lost"
)

var (
	ErrInvalidArgument = errors.New("invalid agent argument")
	ErrNotFound        = errors.New("agent object not found")
	ErrConflict        = errors.New("agent conflict")
	ErrPermission      = errors.New("agent permission denied")
	ErrUnavailable     = errors.New("agent runtime unavailable")
	ErrLeaseLost       = errors.New("agent lease lost")
	ErrBudgetExceeded  = errors.New("agent budget exceeded")
	ErrCancelled       = errors.New("agent cancelled")
)

// RuntimeError carries deterministic retry semantics.
type RuntimeError struct {
	Code      ErrorCode
	Message   string
	Retryable bool
	Cause     error
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return string(e.Code)
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewRuntimeError constructs a classified error. Only transient categories may retry.
func NewRuntimeError(code ErrorCode, message string, cause error) *RuntimeError {
	retryable := code == ErrorTimeout || code == ErrorRateLimit || code == ErrorTemporary || code == ErrorModelUnavailable
	return &RuntimeError{Code: code, Message: message, Retryable: retryable, Cause: cause}
}
