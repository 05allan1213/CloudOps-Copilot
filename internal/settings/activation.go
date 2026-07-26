package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrNoActivationTask = errors.New("no configuration activation task available")

type ActivationRunner struct {
	service  *Service
	workerID string
	interval time.Duration

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewActivationRunner(service *Service, workerID string) (*ActivationRunner, error) {
	if service == nil {
		return nil, errors.New("configuration activation service is required")
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || len(workerID) > 128 {
		return nil, errors.New("configuration activation worker identity is invalid")
	}
	return &ActivationRunner{service: service, workerID: workerID, interval: 250 * time.Millisecond}, nil
}

func (r *ActivationRunner) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("configuration activation context is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("configuration activation runner is already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	r.started = true
	go r.run(runCtx)
	return nil
}

func (r *ActivationRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return nil
	}
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *ActivationRunner) Ready(ctx context.Context) error {
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if !started {
		return errors.New("configuration activation runner is not started")
	}
	return r.service.db.PingContext(ctx)
}

func (r *ActivationRunner) run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		err := r.claimAndObserve(ctx)
		if err != nil && !errors.Is(err, ErrNoActivationTask) && ctx.Err() == nil {
			select {
			case <-time.After(r.interval):
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (r *ActivationRunner) claimAndObserve(ctx context.Context) error {
	tx, err := r.service.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer rollback(tx)
	var taskID, revisionID uint64
	if err := tx.QueryRowContext(ctx, `SELECT id, configuration_revision_id
FROM configuration_activation_tasks FORCE INDEX (idx_configuration_activation_tasks_claim)
WHERE status = 'ready' AND available_at <= NOW(6) AND attempt < 10
ORDER BY available_at, id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&taskID, &revisionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoActivationTask
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE configuration_activation_tasks
SET status = 'running', worker_id = ?, attempt = attempt + 1, updated_at = NOW(6)
WHERE id = ? AND status = 'ready'`, r.workerID, taskID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNoActivationTask
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	record, observeErr := r.service.loadRevision(ctx, r.service.db, `revision.id = ?`, revisionID)
	if observeErr == nil && record.revision.Hash == "" {
		observeErr = errors.New("configuration revision hash is empty")
	}
	if observeErr == nil {
		_, observeErr = r.service.db.ExecContext(ctx, `UPDATE configuration_activation_tasks
SET status = 'succeeded', observed_hash = ?, observed_at = NOW(6), last_error = NULL, updated_at = NOW(6)
WHERE id = ? AND status = 'running' AND worker_id = ?`, record.revision.Hash, taskID, r.workerID)
		return observeErr
	}
	_, updateErr := r.service.db.ExecContext(ctx, `UPDATE configuration_activation_tasks
SET status = CASE WHEN attempt >= 10 THEN 'failed' ELSE 'ready' END,
available_at = TIMESTAMPADD(SECOND, 1, NOW(6)), last_error = ?, updated_at = NOW(6)
WHERE id = ? AND status = 'running' AND worker_id = ?`, boundedError(observeErr), taskID, r.workerID)
	return errors.Join(observeErr, updateErr)
}

func boundedError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 1024 {
		value = value[:1024]
	}
	return value
}

// ObserveTaskBoundary verifies the immutable revision attached to an async task
// after claim and records the exact boundary observation before any handler runs.
func (s *Service) ObserveTaskBoundary(ctx context.Context, taskID, revisionID uint64) (context.Context, error) {
	if taskID == 0 || revisionID == 0 {
		return ctx, errors.New("async task configuration binding is incomplete")
	}
	record, err := s.loadRevision(ctx, s.db, `revision.id = ?`, revisionID)
	if err != nil {
		return ctx, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE async_tasks
SET configuration_observed_at = NOW(6), updated_at = NOW(6)
WHERE id = ? AND configuration_revision_id = ? AND status = 'running'`, taskID, revisionID)
	if err != nil {
		return ctx, fmt.Errorf("record async task configuration boundary: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ctx, errors.New("async task configuration boundary lost its lease state")
	}
	return context.WithValue(ctx, revisionContextKey{}, record.revision), nil
}

type revisionContextKey struct{}

func RevisionFromContext(ctx context.Context) (Revision, bool) {
	revision, ok := ctx.Value(revisionContextKey{}).(Revision)
	return revision, ok
}
