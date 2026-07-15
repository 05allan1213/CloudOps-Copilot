package remediation

import "errors"

var (
	ErrInvalidArgument   = errors.New("invalid remediation argument")
	ErrInvalidTransition = errors.New("invalid remediation transition")
	ErrNotFound          = errors.New("remediation not found")
	ErrConflict          = errors.New("remediation conflict")
	ErrPolicyRejected    = errors.New("remediation policy rejected")
	ErrApprovalMismatch  = errors.New("approval hash mismatch")
	ErrForbidden         = errors.New("remediation forbidden")
	ErrDrift             = errors.New("approved content drift")
)
