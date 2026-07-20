package baseline

import (
	"context"
	"errors"
)

var (
	ErrConflict        = errors.New("baseline conflict")
	ErrSuperseded      = errors.New("baseline revision is already superseded")
	ErrLockUnavailable = errors.New("baseline activation lock unavailable")
)

type ActivationResult struct {
	BaselineID           uint64
	PublicID             string
	Created              bool
	SupersededBaselineID uint64
	ObservationIDs       []uint64
}

type Store interface {
	Activate(context.Context, Snapshot) (ActivationResult, error)
}
