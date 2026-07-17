package tool

import (
	"errors"
	"fmt"
)

type ErrorCode string

func (c ErrorCode) String() string {
	return string(c)
}

const (
	ErrorCodeToolNotFound     ErrorCode = "tool_not_found"
	ErrorCodeInvalidArgs      ErrorCode = "invalid_args"
	ErrorCodePermissionDenied ErrorCode = "permission_denied"
	ErrorCodeToolTimeout      ErrorCode = "tool_timeout"
	ErrorCodeToolExecution    ErrorCode = "tool_execution"
	ErrorCodeToolUnavailable  ErrorCode = "tool_unavailable"
)

var (
	ErrToolNotFound          = errors.New("tool not found")
	ErrToolAlreadyRegistered = errors.New("tool already registered")
	ErrInvalidArgs           = errors.New("invalid tool arguments")
	ErrPermissionDenied      = errors.New("tool permission denied")
	ErrToolTimeout           = errors.New("tool timeout")
	ErrToolExecution         = errors.New("tool execution failed")
	ErrToolUnavailable       = errors.New("agent tool unavailable")
)

type ToolError struct {
	Code   ErrorCode `json:"error"`
	Field  string    `json:"field,omitempty"`
	Reason string    `json:"reason"`
	Cause  error     `json:"-"`
}

func NewToolError(code ErrorCode, field, reason string, cause error) *ToolError {
	return &ToolError{
		Code:   code,
		Field:  field,
		Reason: reason,
		Cause:  cause,
	}
}

func NewInvalidArgsError(field, reason string) *ToolError {
	return NewToolError(ErrorCodeInvalidArgs, field, reason, ErrInvalidArgs)
}

func NewToolNotFoundError(name string) *ToolError {
	return NewToolError(ErrorCodeToolNotFound, "name", fmt.Sprintf("%q is not registered", name), ErrToolNotFound)
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field != "" {
		return fmt.Sprintf("%s: %s: %s", e.Code, e.Field, e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Reason)
}

func (e *ToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
