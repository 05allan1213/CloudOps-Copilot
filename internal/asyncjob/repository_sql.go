package asyncjob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	attentionTaskAttemptsExhausted      = "task_attempts_exhausted"
	attentionTaskLeaseExpired           = "task_lease_expired"
	attentionTaskSubjectVersionMismatch = "task_subject_version_mismatch"
	attentionTaskBusinessBudget         = "agent_run_budget_exhausted"
	attentionTaskDead                   = "task_dead"
)

const (
	resolveSucceededSQL = `UPDATE async_tasks
SET status = 'succeeded',
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = NULL,
    last_error_code = NULL,
    last_error_summary = NULL,
    completed_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at > NOW(6)`

	resolveRetrySQL = `UPDATE async_tasks
SET status = 'ready',
    available_at = TIMESTAMPADD(MICROSECOND, ?, NOW(6)),
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = NULL,
    last_error_code = ?,
    last_error_summary = ?,
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at > NOW(6)
  AND attempt < max_attempts`

	resolveDeadSQL = `UPDATE async_tasks
SET status = 'dead',
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = NULL,
    last_error_code = ?,
    last_error_summary = ?,
    dead_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at > NOW(6)`
)

type scanner interface {
	Scan(...any) error
}

func scanTask(row scanner) (Task, error) {
	var (
		task                    Task
		payload                 []byte
		checkpointSchema        sql.NullInt64
		checkpointVersion       sql.NullInt64
		checkpointHash          sql.NullString
		checkpoint              []byte
		logicalOperationKey     sql.NullString
		leaseOwner              sql.NullString
		leaseExpiresAt          sql.NullTime
		heartbeatAt             sql.NullTime
		configurationObservedAt sql.NullTime
		lastErrorCode           sql.NullString
		lastErrorSummary        sql.NullString
		startedAt               sql.NullTime
		completedAt             sql.NullTime
		deadAt                  sql.NullTime
		cancelledAt             sql.NullTime
		replayedFromTaskID      sql.NullInt64
	)
	err := row.Scan(
		&task.ID,
		&task.PublicID,
		&task.IncidentID,
		&task.CycleNo,
		&task.Queue,
		&task.Type,
		&task.SubjectType,
		&task.SubjectID,
		&task.Transition,
		&task.ExpectedSubjectVersion,
		&task.PayloadSchemaVersion,
		&payload,
		&task.ConfigurationRevisionID,
		&configurationObservedAt,
		&checkpointSchema,
		&checkpointVersion,
		&checkpointHash,
		&checkpoint,
		&task.DedupeKey,
		&task.ReplayGeneration,
		&logicalOperationKey,
		&task.MigratedLegacy,
		&task.MigratedLegacyContext,
		&task.Status,
		&task.Priority,
		&task.AvailableAt,
		&task.Attempt,
		&task.MaxAttempts,
		&leaseOwner,
		&task.LeaseGeneration,
		&leaseExpiresAt,
		&heartbeatAt,
		&lastErrorCode,
		&lastErrorSummary,
		&task.CreatedAt,
		&task.UpdatedAt,
		&startedAt,
		&completedAt,
		&deadAt,
		&cancelledAt,
		&replayedFromTaskID,
	)
	if err != nil {
		return Task{}, err
	}
	task.Payload = payload
	task.ConfigurationObservedAt = timePointer(configurationObservedAt)
	task.CheckpointSchema = uint32(checkpointSchema.Int64)
	task.CheckpointVersion = uint64(checkpointVersion.Int64)
	task.CheckpointHash = checkpointHash.String
	task.Checkpoint = checkpoint
	task.LogicalOperationKey = logicalOperationKey.String
	task.LeaseOwner = leaseOwner.String
	task.LeaseExpiresAt = timePointer(leaseExpiresAt)
	task.HeartbeatAt = timePointer(heartbeatAt)
	task.LastErrorCode = lastErrorCode.String
	task.LastErrorSummary = lastErrorSummary.String
	task.StartedAt = timePointer(startedAt)
	task.CompletedAt = timePointer(completedAt)
	task.DeadAt = timePointer(deadAt)
	task.CancelledAt = timePointer(cancelledAt)
	if replayedFromTaskID.Valid {
		value := uint64(replayedFromTaskID.Int64)
		task.ReplayedFromTaskID = &value
	}
	return task, nil
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func insertAttempt(ctx context.Context, tx *sql.Tx, task Task, claimKind string) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO async_task_attempts (
public_id, task_id, attempt, lease_owner, lease_generation, claim_kind,
configuration_revision_id, expected_subject_version, status, started_at, last_heartbeat_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'running', NOW(6), NOW(6), NOW(6))`, uuid.NewString(), task.ID, task.Attempt, task.LeaseOwner, task.LeaseGeneration, claimKind, task.ConfigurationRevisionID, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("insert async task attempt: %w", err)
	}
	if err := requireOneAttempt(result); err != nil {
		return err
	}
	return nil
}

func finishAttempt(ctx context.Context, tx *sql.Tx, lease Lease, disposition Disposition, code, summary string) error {
	status := string(disposition)
	result, err := tx.ExecContext(ctx, `UPDATE async_task_attempts
SET status = ?,
    finished_at = NOW(6),
    error_code = ?,
    error_summary = ?
WHERE task_id = ?
  AND attempt = ?
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND status = 'running'`, status, nullString(code), nullString(summary), lease.TaskID, lease.Attempt, lease.Owner, lease.Generation, lease.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("finish async task attempt: %w", err)
	}
	return requireOneAttempt(result)
}

func finishExpiredAttempt(ctx context.Context, tx *sql.Tx, task Task) error {
	result, err := tx.ExecContext(ctx, `UPDATE async_task_attempts
SET status = 'lease_expired',
    finished_at = NOW(6),
    error_code = 'lease_expired',
    error_summary = 'lease expired before completion'
WHERE task_id = ?
  AND attempt = ?
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND status = 'running'`, task.ID, task.Attempt, task.LeaseOwner, task.LeaseGeneration, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("finish expired async task attempt: %w", err)
	}
	return requireOneAttempt(result)
}

func markExpiredDead(ctx context.Context, tx *sql.Tx, task Task) error {
	result, err := tx.ExecContext(ctx, `UPDATE async_tasks
SET status = 'dead',
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = NULL,
    last_error_code = 'lease_expired',
    last_error_summary = 'lease expired at the maximum attempt count',
    dead_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at <= NOW(6)
  AND attempt >= max_attempts`, task.ID, task.LeaseOwner, task.LeaseGeneration, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("mark expired async task dead: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	lease := executionFor(task).Lease
	if err := finishAttempt(ctx, tx, lease, DispositionDead, "lease_expired", "lease expired at the maximum attempt count"); err != nil {
		return err
	}
	return markIncidentNeedsAttention(ctx, tx, task, attentionTaskLeaseExpired, "lease_expired")
}

func markStaleReadyDead(ctx context.Context, tx *sql.Tx, task Task) error {
	result, err := tx.ExecContext(ctx, `UPDATE async_tasks
SET status = 'dead',
    last_error_code = 'subject_version_mismatch',
    last_error_summary = 'task subject version or Incident cycle no longer matches',
    dead_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'ready'
  AND lease_generation = ?
  AND expected_subject_version = ?`, task.ID, task.LeaseGeneration, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("mark stale ready async task dead: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	return markIncidentNeedsAttention(ctx, tx, task, attentionTaskSubjectVersionMismatch, "subject_version_mismatch")
}

func markStaleRunningDead(ctx context.Context, tx *sql.Tx, task Task) error {
	result, err := tx.ExecContext(ctx, `UPDATE async_tasks
SET status = 'dead',
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = NULL,
    last_error_code = 'subject_version_mismatch',
    last_error_summary = 'task subject version or Incident cycle no longer matches',
    dead_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'running'
  AND lease_owner = ?
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND lease_expires_at <= NOW(6)`, task.ID, task.LeaseOwner, task.LeaseGeneration, task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("mark stale running async task dead: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	lease := executionFor(task).Lease
	if err := finishAttempt(ctx, tx, lease, DispositionDead, "subject_version_mismatch", "task subject version or Incident cycle no longer matches"); err != nil {
		return err
	}
	return markIncidentNeedsAttention(ctx, tx, task, attentionTaskSubjectVersionMismatch, "subject_version_mismatch")
}

func (r *Repository) ReapExhaustedReady(ctx context.Context, queue Queue) (bool, error) {
	if !queue.Valid() {
		return false, fmt.Errorf("invalid async task queue %q", queue)
	}
	return retryTransactionValue(ctx, func() (bool, error) {
		return r.reapExhaustedReadyOnce(ctx, queue)
	})
}

func (r *Repository) reapExhaustedReadyOnce(ctx context.Context, queue Queue) (bool, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	const selectSQL = `SELECT ` + taskColumns + `
FROM async_tasks FORCE INDEX (idx_async_tasks_ready_claim)
WHERE queue = ?
  AND status = 'ready'
  AND available_at <= NOW(6)
  AND attempt >= max_attempts
	ORDER BY priority DESC, available_at, id
	LIMIT 1`
	candidate, err := scanTask(tx.QueryRowContext(ctx, selectSQL, queue))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select exhausted ready async task candidate: %w", err)
	}
	subjectErr := lockSubjectVersion(ctx, tx, candidate)
	if subjectErr != nil && !errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		return false, subjectErr
	}
	const lockSQL = `SELECT ` + taskColumns + `
FROM async_tasks FORCE INDEX (idx_async_tasks_ready_claim)
WHERE id = ?
  AND queue = ?
  AND status = 'ready'
  AND available_at <= NOW(6)
  AND attempt >= max_attempts
  AND lease_generation = ?
  AND expected_subject_version = ?
FOR UPDATE SKIP LOCKED`
	task, err := scanTask(tx.QueryRowContext(ctx, lockSQL, candidate.ID, queue, candidate.LeaseGeneration, candidate.ExpectedSubjectVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock exhausted ready async task candidate: %w", err)
	}
	if !sameTaskIdentity(candidate, task) {
		return false, nil
	}
	if errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		if err := markStaleReadyDead(ctx, tx, task); err != nil {
			return false, err
		}
		if err := recordReadyDeadAttempt(ctx, tx, task, "subject_version_mismatch", "task subject version or Incident cycle no longer matches"); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit stale exhausted ready async task: %w", err)
		}
		return true, nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE async_tasks
SET status = 'dead',
    last_error_code = 'attempts_exhausted',
    last_error_summary = 'ready task reached the maximum attempt count',
    dead_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND status = 'ready'
  AND lease_generation = ?
  AND expected_subject_version = ?
  AND attempt >= max_attempts`, task.ID, task.LeaseGeneration, task.ExpectedSubjectVersion)
	if err != nil {
		return false, fmt.Errorf("mark exhausted ready async task dead: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return false, err
	}
	if err := recordReadyDeadAttempt(ctx, tx, task, "attempts_exhausted", "ready task reached the maximum attempt count"); err != nil {
		return false, err
	}
	if err := markIncidentNeedsAttention(ctx, tx, task, attentionTaskAttemptsExhausted, "attempts_exhausted"); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit exhausted ready async task: %w", err)
	}
	return true, nil
}

func recordReadyDeadAttempt(ctx context.Context, tx *sql.Tx, task Task, code, summary string) error {
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO async_task_attempts (
public_id, task_id, attempt, expected_subject_version, lease_owner,
lease_generation, claim_kind, status, started_at, finished_at, error_code,
error_summary, created_at
	) VALUES (?, ?, ?, ?, 'system:attempt-reaper', ?, 'ready', 'dead', NOW(6),
	          NOW(6), ?, ?, NOW(6))`,
		uuid.NewString(), task.ID, task.Attempt, task.ExpectedSubjectVersion, task.LeaseGeneration+1, code, summary)
	if err != nil {
		return fmt.Errorf("record exhausted ready async task attempt: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read exhausted ready async task attempt result: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_task_attempts WHERE task_id = ? AND attempt = ?`, task.ID, task.Attempt).Scan(&count); err != nil {
		return fmt.Errorf("verify exhausted ready async task attempt: %w", err)
	}
	if count != 1 {
		return errors.New("exhausted ready async task has no attempt audit")
	}
	return nil
}

func markIncidentNeedsAttention(ctx context.Context, tx *sql.Tx, task Task, reason, errorCode string) error {
	var incidentCycle uint64
	var incidentVersion uint64
	var incidentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT cycle_no, version, status
FROM incidents
WHERE id = ?
FOR UPDATE`, task.IncidentID).Scan(&incidentCycle, &incidentVersion, &incidentStatus); err != nil {
		return fmt.Errorf("lock async task Incident attention state: %w", err)
	}
	currentActiveCycle := incidentCycle == uint64(task.CycleNo) && isActiveIncidentStatus(incidentStatus)
	if currentActiveCycle {
		result, err := tx.ExecContext(ctx, `UPDATE incidents
SET needs_attention = TRUE,
    blocking_reason_code = ?,
    blocked_at = COALESCE(blocked_at, NOW(6)),
    version = version + 1,
    updated_at = NOW(6)
WHERE id = ?
  AND version = ?`, reason, task.IncidentID, incidentVersion)
		if err != nil {
			return fmt.Errorf("mark async task Incident needs_attention: %w", err)
		}
		if err := requireOneRow(result); err != nil {
			return fmt.Errorf("mark async task Incident needs_attention: %w", err)
		}
	}
	metadata, err := json.Marshal(map[string]any{
		"task_public_id":           task.PublicID,
		"task_type":                task.Type,
		"task_cycle_no":            task.CycleNo,
		"subject_type":             task.SubjectType,
		"transition":               task.Transition,
		"expected_subject_version": task.ExpectedSubjectVersion,
		"incident_cycle_no":        incidentCycle,
		"incident_status":          incidentStatus,
		"reason_code":              reason,
		"error_code":               errorCode,
	})
	if err != nil {
		return fmt.Errorf("encode async task dead Incident event: %w", err)
	}
	idempotencyKey := fmt.Sprintf("%x", sha256.Sum256([]byte("async-task-dead\x00"+task.PublicID)))
	if _, err := tx.ExecContext(ctx, `INSERT INTO incident_events (
public_id, incident_id, cycle_no, event_schema_version,
event_type, idempotency_key, actor_type, actor_id, summary, metadata_json,
occurred_at, created_at
) VALUES (?, ?, ?, 1, 'async_task_dead', ?, 'system', 'asyncjob',
	          'async task entered dead state', ?, NOW(6), NOW(6))`,
		uuid.NewString(), task.IncidentID, task.CycleNo, idempotencyKey, metadata); err != nil {
		return fmt.Errorf("append async task dead Incident event: %w", err)
	}
	return nil
}

func isActiveIncidentStatus(status string) bool {
	switch status {
	case "detected", "investigating", "awaiting_approval", "delivering", "verifying":
		return true
	default:
		return false
	}
}

func (r *Repository) Replay(ctx context.Context, request ReplayRequest) (*Task, error) {
	if request.DeadTaskID == 0 {
		return nil, errors.New("dead task id is required for replay")
	}
	if request.ExpectedSubjectVersion == 0 {
		return nil, errors.New("expected subject version is required for replay")
	}
	if request.ValidateSubject == nil {
		return nil, ErrReplayValidationRequired
	}
	return retryTransactionValue(ctx, func() (*Task, error) {
		return r.replayOnce(ctx, request)
	})
}

func (r *Repository) replayOnce(ctx context.Context, request ReplayRequest) (*Task, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	candidate, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks
WHERE id = ? AND status = 'dead'`, request.DeadTaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("load dead async task for replay: %w", err)
	}
	if candidate.ExpectedSubjectVersion != request.ExpectedSubjectVersion {
		return nil, ErrSubjectVersionMismatch
	}
	subjectErr := lockSubjectVersion(ctx, tx, candidate)
	if subjectErr != nil && !errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		return nil, subjectErr
	}
	source, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks
WHERE id = ?
  AND status = 'dead'
  AND expected_subject_version = ?
  AND replay_generation = ?
  AND dedupe_key = ?
FOR UPDATE`, candidate.ID, candidate.ExpectedSubjectVersion, candidate.ReplayGeneration, candidate.DedupeKey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("lock dead async task for replay: %w", err)
	}
	if !sameTaskIdentity(candidate, source) || source.ReplayGeneration != candidate.ReplayGeneration || source.DedupeKey != candidate.DedupeKey {
		return nil, ErrNoTask
	}
	if subjectErr != nil {
		return nil, subjectErr
	}
	if err := request.ValidateSubject(ctx, tx, source); err != nil {
		if errors.Is(err, ErrSubjectVersionMismatch) {
			return nil, err
		}
		return nil, fmt.Errorf("validate async task replay subject: %w", err)
	}
	nextGeneration := source.ReplayGeneration + 1
	existing, existingErr := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks
WHERE dedupe_key = ? AND replay_generation = ?
FOR UPDATE`, source.DedupeKey, nextGeneration))
	if existingErr == nil {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit existing async task replay: %w", err)
		}
		return &existing, nil
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return nil, fmt.Errorf("check existing async task replay: %w", existingErr)
	}
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
configuration_revision_id,
checkpoint_schema_version, checkpoint_version, checkpoint_hash, checkpoint_json,
dedupe_key, replay_generation, logical_operation_key, migrated_legacy, migrated_legacy_context, status, priority,
available_at, attempt, max_attempts, lease_generation, replayed_from_task_id,
created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?,
          NOW(6), 0, ?, 0, ?, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		publicID,
		source.IncidentID,
		source.CycleNo,
		source.Queue,
		source.Type,
		source.SubjectType,
		source.SubjectID,
		source.Transition,
		source.ExpectedSubjectVersion,
		source.PayloadSchemaVersion,
		source.Payload,
		source.ConfigurationRevisionID,
		nullUint32(source.CheckpointSchema),
		source.CheckpointVersion,
		nullString(source.CheckpointHash),
		nullBytes(source.Checkpoint),
		source.DedupeKey,
		nextGeneration,
		nullString(source.LogicalOperationKey),
		source.MigratedLegacy,
		source.MigratedLegacyContext,
		source.Priority,
		source.MaxAttempts,
		source.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert replayed async task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("read replayed async task id: %w", err)
	}
	replayed, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks WHERE id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("load replayed async task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit replayed async task: %w", err)
	}
	return &replayed, nil
}

func (r *Repository) Cancel(ctx context.Context, taskID, expectedSubjectVersion uint64, mutate Mutation) error {
	if taskID == 0 || expectedSubjectVersion == 0 {
		return errors.New("task id and expected subject version are required for cancellation")
	}
	return retryTransactionError(ctx, func() error {
		return r.cancelOnce(ctx, taskID, expectedSubjectVersion, mutate)
	})
}

func (r *Repository) cancelOnce(ctx context.Context, taskID, expectedSubjectVersion uint64, mutate Mutation) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	candidate, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks
WHERE id = ?
  AND expected_subject_version = ?
	  AND status IN ('ready', 'running')`, taskID, expectedSubjectVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return fmt.Errorf("load async task cancellation candidate: %w", err)
	}
	subjectErr := lockSubjectVersion(ctx, tx, candidate)
	if subjectErr != nil && !errors.Is(subjectErr, ErrSubjectVersionMismatch) {
		return subjectErr
	}
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM async_tasks
WHERE id = ?
  AND expected_subject_version = ?
  AND lease_generation = ?
  AND status IN ('ready', 'running')
FOR UPDATE`, candidate.ID, candidate.ExpectedSubjectVersion, candidate.LeaseGeneration))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock async task for cancellation: %w", err)
	}
	if !sameTaskIdentity(candidate, task) || task.Status != candidate.Status {
		return ErrLeaseLost
	}
	if subjectErr != nil {
		return subjectErr
	}
	if mutate != nil {
		if err := mutate(ctx, tx); err != nil {
			return fmt.Errorf("cancel async task domain mutation: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE async_tasks
SET status = 'cancelled',
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = NULL,
    lease_generation = lease_generation + 1,
    cancelled_at = NOW(6),
    updated_at = NOW(6)
WHERE id = ?
  AND expected_subject_version = ?
  AND status IN ('ready', 'running')`, taskID, expectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("cancel async task: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return err
	}
	if task.Status == StatusRunning {
		attemptResult, err := tx.ExecContext(ctx, `UPDATE async_task_attempts
SET status = 'cancelled', finished_at = NOW(6), error_code = 'cancelled', error_summary = 'task cancelled by workflow'
WHERE task_id = ? AND attempt = ? AND lease_owner = ? AND lease_generation = ? AND expected_subject_version = ? AND status = 'running'`, task.ID, task.Attempt, task.LeaseOwner, task.LeaseGeneration, task.ExpectedSubjectVersion)
		if err != nil {
			return fmt.Errorf("cancel async task attempt: %w", err)
		}
		if err := requireOneAttempt(attemptResult); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit async task cancellation: %w", err)
	}
	return nil
}

func nullUint32(value uint32) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
