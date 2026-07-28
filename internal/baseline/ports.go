package baseline

import (
	"context"
	"database/sql"
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

// Transaction is the minimal transaction-bound SQL surface required by a
// baseline activation. Keeping it in the domain package lets another durable
// workflow include activation in its own atomic transaction without depending
// on a concrete *sql.Tx or the baseline MySQL adapter.
type Transaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type TransactionalStore interface {
	ActivateIn(context.Context, Transaction, Snapshot) (ActivationResult, error)
}
