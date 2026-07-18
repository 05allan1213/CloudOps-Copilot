package verification

import "errors"

var (
	ErrInvalidArgument   = errors.New("invalid delivery verification argument")
	ErrInvalidTransition = errors.New("invalid delivery verification transition")
	ErrNotFound          = errors.New("delivery verification not found")
	ErrConflict          = errors.New("delivery verification conflict")
	ErrLeaseLost         = errors.New("delivery verification lease lost")
	ErrUnavailable       = errors.New("delivery verification provider unavailable")
	ErrNotAllowed        = errors.New("delivery verification scope not allowed")
)
