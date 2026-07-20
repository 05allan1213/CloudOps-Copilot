package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

const LatestVersion int64 = 11

type Runner struct {
	provider *goose.Provider
}

func NewRunner(ctx context.Context, db *sql.DB, lockTimeout time.Duration) (*Runner, error) {
	if db == nil {
		return nil, errors.New("migration database is required")
	}
	if lockTimeout <= 0 {
		return nil, errors.New("migration lock timeout must be positive")
	}
	var databaseName sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		return nil, fmt.Errorf("read migration database name: %w", err)
	}
	if !databaseName.Valid || strings.TrimSpace(databaseName.String) == "" {
		return nil, errors.New("migration database name is empty")
	}
	locker := &mysqlSessionLocker{
		name:           LockName(databaseName.String),
		timeoutSeconds: int64(math.Ceil(lockTimeout.Seconds())),
	}
	provider, err := newProvider(db, locker)
	if err != nil {
		return nil, err
	}
	return &Runner{provider: provider}, nil
}

func newProvider(db *sql.DB, locker lock.SessionLocker) (*goose.Provider, error) {
	return goose.NewProvider(
		goose.DialectMySQL,
		db,
		migrations.FS,
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
	)
}

func (r *Runner) Up(ctx context.Context) ([]*goose.MigrationResult, error) {
	if r == nil || r.provider == nil {
		return nil, errors.New("migration runner is not initialized")
	}
	return r.provider.Up(ctx)
}

func (r *Runner) Version(ctx context.Context) (int64, error) {
	if r == nil || r.provider == nil {
		return 0, errors.New("migration runner is not initialized")
	}
	return r.provider.GetDBVersion(ctx)
}

func LockName(databaseName string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("cloudops-copilot:auto-migrate:"+databaseName)))
}

type mysqlSessionLocker struct {
	name           string
	timeoutSeconds int64
}

func (l *mysqlSessionLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", l.name, l.timeoutSeconds).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire migration lock %q: %w", l.name, err)
	}
	if !acquired.Valid {
		return fmt.Errorf("acquire migration lock %q returned NULL", l.name)
	}
	if acquired.Int64 != 1 {
		return fmt.Errorf("acquire migration lock %q timed out", l.name)
	}
	return nil
}

func (l *mysqlSessionLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", l.name).Scan(&released); err != nil {
		return fmt.Errorf("release migration lock %q: %w", l.name, err)
	}
	if !released.Valid || released.Int64 != 1 {
		return fmt.Errorf("release migration lock %q failed", l.name)
	}
	return nil
}

var _ lock.SessionLocker = (*mysqlSessionLocker)(nil)
