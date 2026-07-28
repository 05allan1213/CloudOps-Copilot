// Package change defines framework-independent change intelligence contracts.
package change

import "errors"

var (
	ErrInvalidArgument = errors.New("invalid change argument")
	ErrNotFound        = errors.New("change not found")
	ErrConflict        = errors.New("change conflict")
	ErrUnavailable     = errors.New("change dependency unavailable")
	ErrPermission      = errors.New("change source permission denied")
	ErrNotAllowed      = errors.New("change source not allowed")
)
