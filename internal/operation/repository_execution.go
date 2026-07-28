package operation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (r *Repository) Claim(ctx context.Context, owner string, leaseDuration time.Duration) (Lease, bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 128 || leaseDuration <= 0 {
		return Lease{}, false, ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Lease{}, false, err
	}
	defer rollback(tx)
	now := r.now().UTC()
	if err = reapAmbiguousExecution(ctx, tx, now); err != nil {
		return Lease{}, false, err
	}

	var lease Lease
	var status string
	var maxAttempts uint32
	err = tx.QueryRowContext(ctx, `SELECT id,public_id,attempt,max_attempts,lease_generation,status
FROM operation_executions
WHERE ((status='running' AND lease_expires_at<=? AND external_effect_started_at IS NULL AND attempt<max_attempts)
    OR status='ready')
ORDER BY (status='running') DESC,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).
		Scan(&lease.ExecutionID, &lease.ExecutionPublicID, &lease.Attempt, &maxAttempts, &lease.Generation, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.Commit(); err != nil {
			return Lease{}, false, err
		}
		return Lease{}, false, nil
	}
	if err != nil {
		return Lease{}, false, err
	}
	claimType := "ready"
	if status == "running" {
		claimType = "takeover"
	}
	lease.Attempt++
	lease.Generation++
	expiresAt := now.Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `UPDATE operation_executions
SET status='running',attempt=?,lease_owner=?,lease_generation=?,lease_expires_at=?,
    started_at=COALESCE(started_at,?),updated_at=?
WHERE id=? AND status=? AND lease_generation=?`, lease.Attempt, owner, lease.Generation, expiresAt,
		now, now, lease.ExecutionID, status, lease.Generation-1)
	if err != nil {
		return Lease{}, false, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return Lease{}, false, ErrLeaseLost
	}
	if err = appendEvent(ctx, tx, lease.ExecutionID, "execution.running", map[string]any{
		"attempt": lease.Attempt, "claim_type": claimType, "lease_generation": lease.Generation,
	}, now); err != nil {
		return Lease{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Lease{}, false, err
	}
	lease.Owner = owner
	return lease, true, nil
}

func reapAmbiguousExecution(ctx context.Context, tx *sql.Tx, now time.Time) error {
	var id uint64
	err := tx.QueryRowContext(ctx, `SELECT id FROM operation_executions
WHERE status='running' AND lease_expires_at<=? AND external_effect_started_at IS NOT NULL
ORDER BY lease_expires_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE operation_executions
SET status='failed',failure_code='EFFECT_OUTCOME_UNKNOWN',
failure_summary='worker lease expired after the external effect boundary; blind retry is forbidden',
completed_at=?,lease_owner=NULL,lease_expires_at=NULL,updated_at=? WHERE id=? AND status='running'`, now, now, id); err != nil {
		return err
	}
	return appendEvent(ctx, tx, id, "execution.failed", map[string]any{
		"failure_code":    "EFFECT_OUTCOME_UNKNOWN",
		"failure_summary": "worker lease expired after the external effect boundary; reconciliation is required",
	}, now)
}

func (r *Repository) Heartbeat(ctx context.Context, lease Lease, duration time.Duration) error {
	if duration <= 0 || lease.Owner == "" {
		return ErrInvalidArgument
	}
	now := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE operation_executions SET lease_expires_at=?,updated_at=?
WHERE id=? AND status='running' AND lease_owner=? AND lease_generation=? AND attempt=? AND lease_expires_at>?`,
		now.Add(duration), now, lease.ExecutionID, lease.Owner, lease.Generation, lease.Attempt, now)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) SubjectForExecution(ctx context.Context, lease Lease) (Subject, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Subject{}, err
	}
	defer rollback(tx)
	if err = guardLease(ctx, tx, lease, r.now().UTC()); err != nil {
		return Subject{}, err
	}
	var subjectType, subjectID, expectedHash string
	err = tx.QueryRowContext(ctx, `SELECT execution.subject_type,
COALESCE(card.public_id,plan.public_id),execution.expected_content_hash
FROM operation_executions execution
LEFT JOIN agent_action_cards card ON card.id=execution.action_card_id
LEFT JOIN agent_operation_plans plan ON plan.id=execution.operation_plan_id
WHERE execution.id=? FOR UPDATE`, lease.ExecutionID).Scan(&subjectType, &subjectID, &expectedHash)
	if err != nil {
		return Subject{}, err
	}
	subject, err := r.loadSubject(ctx, tx, subjectType, subjectID, true)
	if err != nil {
		return Subject{}, err
	}
	subject.ExecutionInternalID = lease.ExecutionID
	subject.ExecutionID = lease.ExecutionPublicID
	subject.ExpectedContentHash = expectedHash
	if err = r.validateAuthority(ctx, tx, subject, expectedHash, r.now().UTC()); err != nil {
		return Subject{}, err
	}
	if err = tx.Commit(); err != nil {
		return Subject{}, err
	}
	return subject, nil
}

func (r *Repository) RecordPrepared(ctx context.Context, lease Lease, prepared PreparedEffect) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := r.now().UTC()
	if err = guardLease(ctx, tx, lease, now); err != nil {
		return err
	}
	beforeHash, _, err := hashJSON(struct {
		Source           string          `json:"source"`
		ProviderIdentity json.RawMessage `json:"provider_identity"`
		Evidence         json.RawMessage `json:"evidence"`
		ObservedAt       time.Time       `json:"observed_at"`
	}{prepared.Before.Source, prepared.Before.ProviderIdentity, prepared.Before.Evidence, prepared.Before.ObservedAt.UTC()})
	if err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, lease.ExecutionID, "preconditions.passed", map[string]any{
		"source": prepared.Before.Source, "observation_content_hash": beforeHash,
		"observed_at": prepared.Before.ObservedAt.UTC(), "external": prepared.External,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

// BeginEffect repeats the complete authority check in the same transaction
// that records the effect boundary. The adapter receives no permission to run
// unless this method succeeds.
func (r *Repository) BeginEffect(ctx context.Context, lease Lease, subject Subject, prepared PreparedEffect) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := r.now().UTC()
	if err = guardLease(ctx, tx, lease, now); err != nil {
		return err
	}
	current, err := r.loadSubject(ctx, tx, subject.SubjectType, subject.SubjectID, true)
	if err != nil {
		return err
	}
	if err = r.validateAuthority(ctx, tx, current, subject.ExpectedContentHash, now); err != nil {
		return err
	}
	if current.ContentHash != subject.ContentHash || current.AuthorizationID != subject.AuthorizationID {
		return ErrUnauthorized
	}
	marker, _, err := hashJSON(struct {
		ExecutionID    string `json:"execution_id"`
		Authorization  string `json:"authorization_id"`
		AuthorizedHash string `json:"authorized_hash"`
		Attempt        uint32 `json:"attempt"`
	}{subject.ExecutionID, subject.AuthorizationID, subject.AuthorizedHash, lease.Attempt})
	if err != nil {
		return err
	}
	if prepared.External {
		result, updateErr := tx.ExecContext(ctx, `UPDATE operation_executions
SET external_effect_started_at=?,external_effect_marker=?,updated_at=?
WHERE id=? AND status='running' AND external_effect_started_at IS NULL`, now, marker, now, lease.ExecutionID)
		if updateErr != nil {
			return updateErr
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return ErrConflict
		}
	}
	if err = appendEvent(ctx, tx, lease.ExecutionID, "effect.started", map[string]any{
		"external": prepared.External, "effect_marker": marker,
		"authorization_id": subject.AuthorizationID, "authorized_content_hash": subject.AuthorizedHash,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Complete(ctx context.Context, lease Lease, observation Observation) error {
	if observation.Source != "local" && observation.Source != "kubernetes" {
		return ErrInvalidArgument
	}
	if len(observation.ProviderIdentity) == 0 || len(observation.Evidence) == 0 || strings.TrimSpace(observation.Summary) == "" {
		return ErrInvalidArgument
	}
	status, verificationStatus := "succeeded", "passed"
	if !observation.Verified {
		status, verificationStatus = "verification_failed", "failed"
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := r.now().UTC()
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = now
	}
	if err = guardLease(ctx, tx, lease, now); err != nil {
		return err
	}
	verificationID := uuid.NewString()
	hash, _, err := hashJSON(struct {
		ExecutionID      string          `json:"execution_id"`
		Source           string          `json:"source"`
		Status           string          `json:"status"`
		ProviderIdentity json.RawMessage `json:"provider_identity"`
		Evidence         json.RawMessage `json:"evidence"`
		ObservedAt       time.Time       `json:"observed_at"`
	}{lease.ExecutionPublicID, observation.Source, verificationStatus, observation.ProviderIdentity,
		observation.Evidence, observation.ObservedAt.UTC()})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO operation_verification_observations
(public_id,execution_id,source,status,provider_identity_json,evidence_json,content_hash,summary,observed_at,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?)`, verificationID, lease.ExecutionID, observation.Source, verificationStatus,
		observation.ProviderIdentity, observation.Evidence, hash, observation.Summary,
		observation.ObservedAt.UTC(), now); err != nil {
		return err
	}
	resultJSON, err := json.Marshal(map[string]any{
		"verification_id": verificationID, "verification_status": verificationStatus,
		"verification_content_hash": hash, "summary": observation.Summary,
	})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE operation_executions
SET status=?,result_json=?,failure_code=NULL,failure_summary=NULL,completed_at=?,
lease_owner=NULL,lease_expires_at=NULL,updated_at=? WHERE id=? AND status='running'`,
		status, resultJSON, now, now, lease.ExecutionID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrLeaseLost
	}
	if err = appendEvent(ctx, tx, lease.ExecutionID, "verification."+verificationStatus, map[string]any{
		"verification_id": verificationID, "source": observation.Source,
		"content_hash": hash, "summary": observation.Summary,
	}, observation.ObservedAt.UTC()); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, lease.ExecutionID, "execution."+status, map[string]any{
		"status": status, "verification_id": verificationID,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Fail(ctx context.Context, lease Lease, executionErr error) error {
	status, code := "failed", failureCode(executionErr)
	if errors.Is(executionErr, ErrPreconditionFailed) {
		status = "precondition_failed"
	}
	summary := strings.TrimSpace(executionErr.Error())
	if len(summary) > 2048 {
		summary = summary[:2048]
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := r.now().UTC()
	if err = guardLease(ctx, tx, lease, now); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE operation_executions
SET status=?,failure_code=?,failure_summary=?,completed_at=?,lease_owner=NULL,lease_expires_at=NULL,updated_at=?
WHERE id=? AND status='running'`, status, code, summary, now, now, lease.ExecutionID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrLeaseLost
	}
	if err = appendEvent(ctx, tx, lease.ExecutionID, "execution."+status, map[string]any{
		"failure_code": code, "failure_summary": summary,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, ErrUnauthorized):
		return "AUTHORIZATION_INVALID"
	case errors.Is(err, ErrExpired):
		return "AUTHORIZATION_EXPIRED"
	case errors.Is(err, ErrRevisionChanged):
		return "CONFIGURATION_REVISION_CHANGED"
	case errors.Is(err, ErrPreconditionFailed):
		return "PRECONDITION_FAILED"
	case errors.Is(err, ErrProviderUnavailable):
		return "PROVIDER_NOT_CONFIGURED"
	case errors.Is(err, ErrInvalidArgument):
		return "UNSUPPORTED_OPERATION"
	case errors.Is(err, ErrConflict):
		return "PROVIDER_CONFLICT"
	default:
		return "EXECUTION_FAILED"
	}
}

func guardLease(ctx context.Context, tx *sql.Tx, lease Lease, now time.Time) error {
	var id uint64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM operation_executions
WHERE id=? AND public_id=? AND status='running' AND lease_owner=? AND lease_generation=?
AND attempt=? AND lease_expires_at>? FOR UPDATE`, lease.ExecutionID, lease.ExecutionPublicID,
		lease.Owner, lease.Generation, lease.Attempt, now).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	} else if err != nil {
		return err
	}
	if id != lease.ExecutionID {
		return ErrLeaseLost
	}
	return nil
}

func wrapPrecondition(detail string) error {
	return fmt.Errorf("%w: %s", ErrPreconditionFailed, detail)
}
