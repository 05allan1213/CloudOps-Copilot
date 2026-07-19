package asyncjob

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const taskColumns = `id, public_id, incident_id, cycle_no, queue, task_type,
subject_type, subject_id, transition, expected_subject_version,
payload_schema_version, payload_json, checkpoint_schema_version,
checkpoint_version, checkpoint_hash, checkpoint_json, dedupe_key,
replay_generation, logical_operation_key, status, priority, available_at,
attempt, max_attempts, lease_owner, lease_generation, lease_expires_at,
heartbeat_at, last_error_code, last_error_summary, created_at, updated_at,
started_at, completed_at, dead_at, cancelled_at, replayed_from_task_id`

const (
	claimReadySelectSQL = `SELECT ` + taskColumns + `
	FROM async_tasks FORCE INDEX (idx_async_tasks_ready_claim)
	WHERE queue = ?
	  AND id = ?
	  AND status = 'ready'
	  AND available_at <= NOW(6)
	  AND attempt < max_attempts
	  AND lease_generation = ?
	  AND expected_subject_version = ?
	FOR UPDATE SKIP LOCKED`

	claimReadyCandidateSQL = `SELECT ` + taskColumns + `
	FROM async_tasks FORCE INDEX (idx_async_tasks_ready_claim)
	WHERE queue = ?
	  AND status = 'ready'
	  AND available_at <= NOW(6)
	  AND attempt < max_attempts
	ORDER BY priority DESC, available_at, id
	LIMIT 1`

	claimReadyUpdateSQL = `UPDATE async_tasks
SET status = 'running',
    attempt = attempt + 1,
    lease_owner = ?,
    lease_generation = lease_generation + 1,
    lease_expires_at = TIMESTAMPADD(MICROSECOND, ?, NOW(6)),
    heartbeat_at = NOW(6),
    started_at = COALESCE(started_at, NOW(6)),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'ready'
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND attempt < max_attempts`

	takeoverSelectSQL = `SELECT ` + taskColumns + `
	FROM async_tasks FORCE INDEX (idx_async_tasks_expired_takeover)
	WHERE queue = ?
	  AND id = ?
	  AND status = 'running'
	  AND lease_expires_at <= NOW(6)
	  AND lease_generation = ?
	  AND expected_subject_version = ?
	FOR UPDATE SKIP LOCKED`

	takeoverCandidateSQL = `SELECT ` + taskColumns + `
	FROM async_tasks FORCE INDEX (idx_async_tasks_expired_takeover)
	WHERE queue = ?
	  AND status = 'running'
	  AND lease_expires_at <= NOW(6)
	ORDER BY lease_expires_at, id
	LIMIT 1`

	takeoverUpdateSQL = `UPDATE async_tasks
SET attempt = attempt + 1,
    lease_owner = ?,
    lease_generation = lease_generation + 1,
    lease_expires_at = TIMESTAMPADD(MICROSECOND, ?, NOW(6)),
    heartbeat_at = NOW(6),
    last_error_code = 'lease_expired',
    last_error_summary = 'previous lease expired before completion',
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at <= NOW(6)
  AND attempt < max_attempts`

	leaseGuardSQL = `SELECT ` + taskColumns + `
	FROM async_tasks
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
	  AND lease_expires_at > NOW(6)
	FOR UPDATE`

	leaseCandidateSQL = `SELECT ` + taskColumns + `
	FROM async_tasks
	WHERE id = ?
	  AND status = 'running'
	  AND lease_owner = ?
	  AND lease_generation = ?
	  AND expected_subject_version = ?
	  AND lease_expires_at > NOW(6)`

	heartbeatUpdateSQL = `UPDATE async_tasks
SET lease_expires_at = TIMESTAMPADD(MICROSECOND, ?, NOW(6)),
    heartbeat_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at > NOW(6)`

	checkpointUpdateSQL = `UPDATE async_tasks
SET checkpoint_schema_version = ?,
    checkpoint_version = ?,
    checkpoint_hash = ?,
    checkpoint_json = ?,
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at > NOW(6)`
)

type ClaimRequest struct {
	Queue         Queue
	Owner         string
	LeaseDuration time.Duration
}

func (r ClaimRequest) Validate() error {
	if !r.Queue.Valid() {
		return fmt.Errorf("invalid claim queue %q", r.Queue)
	}
	if strings.TrimSpace(r.Owner) == "" || len(r.Owner) > 128 {
		return errors.New("claim owner is required and must not exceed 128 bytes")
	}
	if r.LeaseDuration < time.Microsecond {
		return errors.New("claim lease duration must be positive")
	}
	return nil
}

type Store interface {
	Ready(context.Context) error
	Claim(context.Context, ClaimRequest) (*Execution, error)
	Heartbeat(context.Context, Lease, time.Duration) error
	Resolve(context.Context, Lease, Result) error
}

type Handler interface {
	Handle(context.Context, Execution) Result
}

type HandlerFunc func(context.Context, Execution) Result

func (f HandlerFunc) Handle(ctx context.Context, execution Execution) Result {
	return f(ctx, execution)
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("async task repository database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Ready(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping async task database: %w", err)
	}
	var version sql.NullInt64
	if err := r.db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil {
		return fmt.Errorf("read async task schema version: %w", err)
	}
	if !version.Valid || version.Int64 != 8 {
		return fmt.Errorf("unsupported async task schema version %d, want 8", version.Int64)
	}
	const schemaSQL = `SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_name IN ('async_tasks', 'async_task_attempts')
  AND engine = 'InnoDB'`
	var tables int
	if err := r.db.QueryRowContext(ctx, schemaSQL).Scan(&tables); err != nil {
		return fmt.Errorf("check async task schema: %w", err)
	}
	if tables != 2 {
		return fmt.Errorf("async task schema is incomplete: found %d of 2 InnoDB tables", tables)
	}
	return nil
}

func (r *Repository) Enqueue(ctx context.Context, task NewTask) (*Task, error) {
	if err := task.Validate(); err != nil {
		return nil, err
	}
	return retryTransactionValue(ctx, func() (*Task, error) {
		tx, err := r.begin(ctx)
		if err != nil {
			return nil, err
		}
		defer rollback(tx)
		if err := lockNewTaskSubject(ctx, tx, task); err != nil {
			return nil, err
		}
		result, err := r.EnqueueIn(ctx, tx, task)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit async task enqueue: %w", err)
		}
		return result, nil
	})
}

// EnqueueIn is used by an owning domain transaction. The caller must already
// hold the canonical Incident -> child locks before invoking it.
func (r *Repository) EnqueueIn(ctx context.Context, executor DBTX, task NewTask) (*Task, error) {
	if executor == nil {
		return nil, errors.New("async task enqueue transaction is required")
	}
	if err := task.Validate(); err != nil {
		return nil, err
	}
	queue, _ := QueueForTaskType(task.Type)
	payload := task.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	const insertSQL = `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
dedupe_key, replay_generation, logical_operation_key, status, priority,
available_at, attempt, max_attempts, lease_generation, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 'ready', ?,
          COALESCE(?, NOW(6)), 0, ?, 0, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`
	result, err := executor.ExecContext(
		ctx,
		insertSQL,
		uuid.NewString(),
		task.IncidentID,
		task.CycleNo,
		queue,
		task.Type,
		strings.TrimSpace(task.SubjectType),
		task.SubjectID,
		strings.TrimSpace(task.Transition),
		task.ExpectedSubjectVersion,
		task.PayloadSchemaVersion,
		[]byte(payload),
		task.DedupeKey,
		nullString(task.LogicalOperationKey),
		task.Priority,
		nullTime(task.AvailableAt),
		task.MaxAttempts,
	)
	if err != nil {
		return nil, fmt.Errorf("enqueue async task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read enqueued async task id: %w", err)
	}
	if id <= 0 {
		return nil, errors.New("enqueue async task returned an invalid id")
	}
	loaded, err := scanTask(executor.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks WHERE id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("load enqueued async task: %w", err)
	}
	return &loaded, nil
}

func lockNewTaskSubject(ctx context.Context, tx *sql.Tx, task NewTask) error {
	return lockSubjectVersion(ctx, tx, Task{
		IncidentID:             task.IncidentID,
		CycleNo:                task.CycleNo,
		SubjectType:            strings.TrimSpace(task.SubjectType),
		SubjectID:              task.SubjectID,
		ExpectedSubjectVersion: task.ExpectedSubjectVersion,
	})
}

func (r *Repository) Claim(ctx context.Context, request ClaimRequest) (*Execution, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if _, err := r.ReapExhaustedReady(ctx, request.Queue); err != nil {
		return nil, err
	}
	// Recovery is deterministic: an expired running task already consumed an
	// attempt and lease, so it wins over new ready work in the same queue.
	execution, err := r.TakeoverExpired(ctx, request)
	if err == nil || !errors.Is(err, ErrNoTask) {
		return execution, err
	}
	return r.ClaimReady(ctx, request)
}

func (r *Repository) ClaimReady(ctx context.Context, request ClaimRequest) (*Execution, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return retryTransactionValue(ctx, func() (*Execution, error) {
		return r.claimReadyOnce(ctx, request)
	})
}

func (r *Repository) claimReadyOnce(ctx context.Context, request ClaimRequest) (_ *Execution, err error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	candidate, err := scanTask(tx.QueryRowContext(ctx, claimReadyCandidateSQL, request.Queue))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("select ready async task candidate: %w", err)
	}
	subjectErr := lockSubjectVersion(ctx, tx, candidate)
	if subjectErr != nil && !errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		return nil, subjectErr
	}
	task, err := scanTask(tx.QueryRowContext(
		ctx,
		claimReadySelectSQL,
		request.Queue,
		candidate.ID,
		candidate.LeaseGeneration,
		candidate.ExpectedSubjectVersion,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("lock ready async task candidate: %w", err)
	}
	if !sameTaskIdentity(candidate, task) {
		return nil, ErrNoTask
	}
	if errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		if markErr := markStaleReadyDead(ctx, tx, task); markErr != nil {
			return nil, markErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit stale ready async task: %w", commitErr)
		}
		return nil, ErrNoTask
	}
	result, err := tx.ExecContext(
		ctx,
		claimReadyUpdateSQL,
		request.Owner,
		durationMicros(request.LeaseDuration),
		task.ID,
		task.LeaseGeneration,
		task.ExpectedSubjectVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("claim ready async task: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return nil, err
	}

	claimed, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks WHERE id = ?`, task.ID))
	if err != nil {
		return nil, fmt.Errorf("reload claimed async task: %w", err)
	}
	if err := insertAttempt(ctx, tx, claimed, "ready"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ready async task claim: %w", err)
	}
	return executionFor(claimed), nil
}

func (r *Repository) TakeoverExpired(ctx context.Context, request ClaimRequest) (*Execution, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return retryTransactionValue(ctx, func() (*Execution, error) {
		return r.takeoverExpiredOnce(ctx, request)
	})
}

func (r *Repository) takeoverExpiredOnce(ctx context.Context, request ClaimRequest) (_ *Execution, err error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	candidate, err := scanTask(tx.QueryRowContext(ctx, takeoverCandidateSQL, request.Queue))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("select expired async task candidate: %w", err)
	}
	subjectErr := lockSubjectVersion(ctx, tx, candidate)
	if subjectErr != nil && !errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		return nil, subjectErr
	}
	task, err := scanTask(tx.QueryRowContext(
		ctx,
		takeoverSelectSQL,
		request.Queue,
		candidate.ID,
		candidate.LeaseGeneration,
		candidate.ExpectedSubjectVersion,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("lock expired async task candidate: %w", err)
	}
	if !sameTaskIdentity(candidate, task) {
		return nil, ErrNoTask
	}
	if errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		if markErr := markStaleRunningDead(ctx, tx, task); markErr != nil {
			return nil, markErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit stale expired async task: %w", commitErr)
		}
		return nil, ErrNoTask
	}
	if task.Attempt >= task.MaxAttempts {
		if err := markExpiredDead(ctx, tx, task); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit exhausted async task: %w", err)
		}
		return nil, ErrNoTask
	}
	if err := finishExpiredAttempt(ctx, tx, task); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(
		ctx,
		takeoverUpdateSQL,
		request.Owner,
		durationMicros(request.LeaseDuration),
		task.ID,
		task.LeaseGeneration,
		task.ExpectedSubjectVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("take over expired async task: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return nil, err
	}
	taken, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks WHERE id = ?`, task.ID))
	if err != nil {
		return nil, fmt.Errorf("reload taken-over async task: %w", err)
	}
	if err := insertAttempt(ctx, tx, taken, "takeover"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit async task takeover: %w", err)
	}
	return executionFor(taken), nil
}

func (r *Repository) Heartbeat(ctx context.Context, lease Lease, duration time.Duration) error {
	if err := lease.Validate(); err != nil {
		return err
	}
	if duration < time.Microsecond {
		return errors.New("heartbeat lease duration must be positive")
	}
	return retryTransactionError(ctx, func() error {
		return r.heartbeatOnce(ctx, lease, duration)
	})
}

func (r *Repository) heartbeatOnce(ctx context.Context, lease Lease, duration time.Duration) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := lockExecutionState(ctx, tx, lease); err != nil {
		return err
	}
	result, err := tx.ExecContext(
		ctx,
		heartbeatUpdateSQL,
		durationMicros(duration),
		lease.TaskID,
		lease.Owner,
		lease.Generation,
		lease.ExpectedSubjectVersion,
	)
	if err != nil {
		return fmt.Errorf("heartbeat async task: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	attemptResult, err := tx.ExecContext(ctx, `UPDATE async_task_attempts
SET last_heartbeat_at = NOW(6)
WHERE task_id = ?
  AND attempt = ?
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND status = 'running'`, lease.TaskID, lease.Attempt, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("heartbeat async task attempt: %w", err)
	}
	if err := requireOneAttempt(attemptResult); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit async task heartbeat: %w", err)
	}
	return nil
}

func (r *Repository) Checkpoint(ctx context.Context, lease Lease, checkpoint Checkpoint, mutate Mutation) error {
	if err := lease.Validate(); err != nil {
		return err
	}
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	return retryTransactionError(ctx, func() error {
		return r.checkpointOnce(ctx, lease, checkpoint, mutate)
	})
}

func (r *Repository) checkpointOnce(ctx context.Context, lease Lease, checkpoint Checkpoint, mutate Mutation) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := lockExecutionState(ctx, tx, lease); err != nil {
		return err
	}
	if mutate != nil {
		if err := mutate(ctx, tx); err != nil {
			return fmt.Errorf("persist async task checkpoint domain state: %w", err)
		}
	}
	result, err := tx.ExecContext(
		ctx,
		checkpointUpdateSQL,
		checkpoint.SchemaVersion,
		checkpoint.Version,
		checkpoint.Hash,
		[]byte(checkpoint.Payload),
		lease.TaskID,
		lease.Owner,
		lease.Generation,
		lease.ExpectedSubjectVersion,
	)
	if err != nil {
		return fmt.Errorf("checkpoint async task: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit async task checkpoint: %w", err)
	}
	return nil
}

func (r *Repository) Resolve(ctx context.Context, lease Lease, resolution Result) error {
	if err := lease.Validate(); err != nil {
		return err
	}
	if err := resolution.Validate(); err != nil {
		return err
	}
	return retryTransactionError(ctx, func() error {
		return r.resolveOnce(ctx, lease, resolution)
	})
}

func (r *Repository) resolveOnce(ctx context.Context, lease Lease, resolution Result) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	task, err := lockExecutionState(ctx, tx, lease)
	if err != nil {
		if !errors.Is(err, ErrSubjectVersionMismatch) || task.ID == 0 {
			return err
		}
		resolution = deterministicMutationDead(err)
	}
	if err == nil && resolution.Mutate != nil {
		if _, savepointErr := tx.ExecContext(ctx, "SAVEPOINT async_task_domain_mutation"); savepointErr != nil {
			return fmt.Errorf("create async task domain mutation savepoint: %w", savepointErr)
		}
		mutationErr := resolution.Mutate(ctx, tx)
		if mutationErr != nil {
			if !isDeterministicMutationError(mutationErr) {
				return fmt.Errorf("resolve async task domain mutation: %w", mutationErr)
			}
			if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT async_task_domain_mutation"); rollbackErr != nil {
				return fmt.Errorf("roll back rejected async task domain mutation: %w", rollbackErr)
			}
			resolution = deterministicMutationDead(mutationErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT async_task_domain_mutation"); releaseErr != nil {
			return fmt.Errorf("release async task domain mutation savepoint: %w", releaseErr)
		}
	}

	disposition := resolution.Disposition
	if disposition == DispositionRetry && task.Attempt >= task.MaxAttempts {
		disposition = DispositionDead
	}
	var result sql.Result
	switch disposition {
	case DispositionSucceeded:
		result, err = tx.ExecContext(ctx, resolveSucceededSQL, lease.TaskID, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion)
	case DispositionRetry:
		result, err = tx.ExecContext(ctx, resolveRetrySQL, durationMicros(resolution.RetryAfter), resolution.ErrorCode, resolution.ErrorSummary, lease.TaskID, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion)
	case DispositionDead:
		result, err = tx.ExecContext(ctx, resolveDeadSQL, resolution.ErrorCode, resolution.ErrorSummary, lease.TaskID, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion)
	default:
		return ErrInvalidResult
	}
	if err != nil {
		return fmt.Errorf("resolve async task as %s: %w", disposition, err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	if err := finishAttempt(ctx, tx, lease, disposition, resolution.ErrorCode, resolution.ErrorSummary); err != nil {
		return err
	}
	if disposition == DispositionDead {
		reason := attentionTaskDead
		switch {
		case resolution.Disposition == DispositionRetry:
			reason = attentionTaskAttemptsExhausted
		case resolution.ErrorCode == "subject_version_mismatch":
			reason = attentionTaskSubjectVersionMismatch
		case resolution.ErrorCode == "business_budget_exceeded":
			reason = attentionTaskBusinessBudget
		}
		if err := markIncidentNeedsAttention(ctx, tx, task, reason, resolution.ErrorCode); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit async task resolution: %w", err)
	}
	return nil
}

func isDeterministicMutationError(err error) bool {
	return errors.Is(err, ErrSubjectVersionMismatch) ||
		errors.Is(err, ErrInvalidMutation) ||
		errors.Is(err, ErrPolicyViolation) ||
		errors.Is(err, ErrBusinessBudgetExceeded)
}

func deterministicMutationDead(err error) Result {
	switch {
	case errors.Is(err, ErrSubjectVersionMismatch):
		return Dead("subject_version_mismatch", "task subject version or Incident cycle no longer matches", nil)
	case errors.Is(err, ErrPolicyViolation):
		return Dead("policy_violation", "task domain mutation was rejected by policy", nil)
	case errors.Is(err, ErrBusinessBudgetExceeded):
		return Dead("business_budget_exceeded", "task business budget is exhausted", nil)
	default:
		return Dead("invalid_mutation", "task domain mutation rejected invalid input", nil)
	}
}

func (r *Repository) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin async task transaction: %w", err)
	}
	return tx, nil
}

func lockLease(ctx context.Context, tx *sql.Tx, lease Lease) (Task, error) {
	task, err := scanTask(tx.QueryRowContext(ctx, leaseGuardSQL, lease.TaskID, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrLeaseLost
	}
	if err != nil {
		return Task{}, fmt.Errorf("lock async task lease: %w", err)
	}
	if task.Attempt != lease.Attempt || task.MaxAttempts != lease.MaxAttempts {
		return Task{}, ErrLeaseLost
	}
	return task, nil
}

func lockExecutionState(ctx context.Context, tx *sql.Tx, lease Lease) (Task, error) {
	candidate, err := scanTask(tx.QueryRowContext(ctx, leaseCandidateSQL, lease.TaskID, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrLeaseLost
	}
	if err != nil {
		return Task{}, fmt.Errorf("load async task lease candidate: %w", err)
	}
	subjectErr := lockSubjectVersion(ctx, tx, candidate)
	if subjectErr != nil && !errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		return Task{}, subjectErr
	}
	task, err := lockLease(ctx, tx, lease)
	if err != nil {
		return Task{}, err
	}
	if !sameTaskIdentity(candidate, task) {
		return Task{}, ErrLeaseLost
	}
	if subjectErr != nil {
		return task, subjectErr
	}
	return task, nil
}

func sameTaskIdentity(left, right Task) bool {
	return left.ID == right.ID &&
		left.IncidentID == right.IncidentID &&
		left.CycleNo == right.CycleNo &&
		left.Queue == right.Queue &&
		left.Type == right.Type &&
		left.SubjectType == right.SubjectType &&
		left.SubjectID == right.SubjectID &&
		left.Transition == right.Transition &&
		left.ExpectedSubjectVersion == right.ExpectedSubjectVersion &&
		left.LeaseGeneration == right.LeaseGeneration
}

// lockSubjectVersion fences the durable business subject, not merely the copy
// of expected_subject_version stored on the task row. Every query also binds
// the subject to the task's Incident cycle so a stale or cross-cycle task
// cannot heartbeat, checkpoint, fail, or complete.
func lockSubjectVersion(ctx context.Context, tx *sql.Tx, task Task) error {
	var domainVersion, incidentCycle sql.NullInt64
	var incidentVersion uint64
	var incidentStatus sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT domain_schema_version, cycle_no, version, v3_status
FROM incidents
WHERE id = ?
FOR UPDATE`, task.IncidentID).Scan(&domainVersion, &incidentCycle, &incidentVersion, &incidentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSubjectVersionMismatch
		}
		return fmt.Errorf("lock async task Incident cycle: %w", err)
	}
	if !domainVersion.Valid || domainVersion.Int64 != 3 || !incidentCycle.Valid || !incidentStatus.Valid ||
		uint64(incidentCycle.Int64) != uint64(task.CycleNo) || !isActiveIncidentStatus(incidentStatus.String) {
		return ErrSubjectVersionMismatch
	}
	if task.SubjectType == "incident" {
		if task.SubjectID != task.IncidentID || incidentVersion != task.ExpectedSubjectVersion {
			return ErrSubjectVersionMismatch
		}
		return nil
	}

	var query string
	var args []any
	switch task.SubjectType {
	case "agent_run":
		query = `SELECT row_version FROM agent_runs
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
FOR UPDATE`
		args = []any{task.SubjectID, task.IncidentID, task.CycleNo}
	case "remediation_plan":
		query = `SELECT row_version FROM remediation_plans
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
FOR UPDATE`
		args = []any{task.SubjectID, task.IncidentID, task.CycleNo}
	case "change_request":
		query = `SELECT row_version FROM change_requests
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
FOR UPDATE`
		args = []any{task.SubjectID, task.IncidentID, task.CycleNo}
	case "verification_run":
		query = `SELECT row_version FROM verification_runs
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
FOR UPDATE`
		args = []any{task.SubjectID, task.IncidentID, task.CycleNo}
	default:
		return fmt.Errorf("unsupported async task subject type %q", task.SubjectType)
	}
	var version uint64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSubjectVersionMismatch
		}
		return fmt.Errorf("lock async task subject version: %w", err)
	}
	if version != task.ExpectedSubjectVersion {
		return ErrSubjectVersionMismatch
	}
	return nil
}

func executionFor(task Task) *Execution {
	return &Execution{
		Task: task,
		Lease: Lease{
			TaskID:                 task.ID,
			Owner:                  task.LeaseOwner,
			Generation:             task.LeaseGeneration,
			ExpectedSubjectVersion: task.ExpectedSubjectVersion,
			Attempt:                task.Attempt,
			MaxAttempts:            task.MaxAttempts,
		},
	}
}

func durationMicros(duration time.Duration) int64 {
	return duration.Microseconds()
}

func requireOneRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read guarded async task update result: %w", err)
	}
	if rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

func requireOneAttempt(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read async task attempt update result: %w", err)
	}
	if rows != 1 {
		return errors.New("async task attempt invariant violated")
	}
	return nil
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}
