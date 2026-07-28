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

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("operation repository requires MySQL")
	}
	return &Repository{db: db, now: time.Now}, nil
}

func (r *Repository) EnqueueActionCard(ctx context.Context, publicID string, request ExecuteRequest) (Execution, error) {
	return r.enqueue(ctx, SubjectActionCard, publicID, request)
}

func (r *Repository) EnqueueOperationPlan(ctx context.Context, publicID string, request ExecuteRequest) (Execution, error) {
	return r.enqueue(ctx, SubjectOperationPlan, publicID, request)
}

func (r *Repository) enqueue(ctx context.Context, subjectType, publicID string, request ExecuteRequest) (Execution, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" || !lowerHex64(request.ExpectedHash) {
		return Execution{}, ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Execution{}, err
	}
	defer rollback(tx)
	subject, err := r.loadSubject(ctx, tx, subjectType, publicID, true)
	if err != nil {
		return Execution{}, err
	}
	now := r.now().UTC()
	if err = r.validateAuthority(ctx, tx, subject, request.ExpectedHash, now); err != nil {
		return Execution{}, err
	}

	executionPublicID := uuid.NewString()
	cardID, planID := nullableSubjectIDs(subject)
	result, err := tx.ExecContext(ctx, `INSERT INTO operation_executions
(public_id,subject_type,action_card_id,operation_plan_id,authorization_id,configuration_revision_id,
 expected_content_hash,status,attempt,max_attempts,lease_generation,created_at,updated_at)
VALUES (?,?,?,?,?,? ,?,'ready',0,2,0,?,?)
ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, executionPublicID, subject.SubjectType, cardID, planID,
		subject.AuthorizationInternalID, subject.ConfigurationRevisionInternalID, subject.ContentHash, now, now)
	if err != nil {
		return Execution{}, fmt.Errorf("enqueue authorized operation: %w", err)
	}
	executionID, err := result.LastInsertId()
	if err != nil || executionID <= 0 {
		return Execution{}, fmt.Errorf("read operation execution identity: %w", err)
	}
	var storedHash string
	if err = tx.QueryRowContext(ctx, `SELECT public_id,expected_content_hash FROM operation_executions WHERE id=? FOR UPDATE`, executionID).
		Scan(&executionPublicID, &storedHash); err != nil {
		return Execution{}, err
	}
	if storedHash != subject.ContentHash || storedHash != request.ExpectedHash {
		return Execution{}, ErrConflict
	}
	var eventCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operation_events WHERE execution_id=?`, executionID).Scan(&eventCount); err != nil {
		return Execution{}, err
	}
	if eventCount == 0 {
		if err = appendEvent(ctx, tx, uint64(executionID), "execution.queued", map[string]any{
			"execution_id": executionPublicID, "subject_type": subject.SubjectType,
			"subject_id": subject.SubjectID, "authorized_content_hash": subject.AuthorizedHash,
			"configuration_revision_id": subject.ConfigurationRevisionID,
		}, now); err != nil {
			return Execution{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Execution{}, err
	}
	return r.Execution(ctx, executionPublicID)
}

func nullableSubjectIDs(subject Subject) (any, any) {
	if subject.SubjectType == SubjectActionCard {
		return subject.SubjectInternalID, nil
	}
	return nil, subject.SubjectInternalID
}

func (r *Repository) validateAuthority(ctx context.Context, query queryRower, subject Subject, expectedHash string, now time.Time) error {
	if subject.AuthorizationInternalID == 0 || subject.AuthorizationID == "" {
		return ErrUnauthorized
	}
	if subject.Status != "authorized" || !subject.ExpiresAt.After(now) || !subject.AuthorizationExpiresAt.After(now) {
		return ErrExpired
	}
	if expectedHash != subject.ContentHash || subject.AuthorizedHash != subject.ContentHash {
		return ErrUnauthorized
	}
	computed, err := canonicalSubjectHash(subject)
	if err != nil || computed != subject.ContentHash {
		return ErrUnauthorized
	}
	var activeRevision string
	if err = query.QueryRowContext(ctx, `SELECT revision.public_id FROM active_configuration active
JOIN configuration_revisions revision ON revision.id=active.configuration_revision_id
WHERE active.singleton_id=1`).Scan(&activeRevision); err != nil {
		return err
	}
	if activeRevision != subject.ConfigurationRevisionID {
		return ErrRevisionChanged
	}
	return nil
}

func canonicalSubjectHash(subject Subject) (string, error) {
	if subject.SubjectType == SubjectActionCard {
		return agent.ActionCardContentHash(agent.ActionCard{
			Authority: subject.Authority, ActionType: subject.OperationType, Target: subject.Target,
			Parameters: subject.Parameters, Preconditions: subject.Preconditions, Risk: subject.Risk,
			ExpiresAt: subject.ExpiresAt,
		})
	}
	if subject.SubjectType != SubjectOperationPlan {
		return "", ErrInvalidArgument
	}
	return agent.OperationPlanContentHash(agent.OperationPlan{
		Authority: subject.Authority, ConfigurationRevisionID: subject.ConfigurationRevisionID,
		OperationType: subject.OperationType, Target: subject.Target, Parameters: subject.Parameters,
		IntendedState: subject.IntendedState, Preconditions: subject.Preconditions, Risk: subject.Risk,
		VerificationIntent: subject.VerificationIntent, ExpiresAt: subject.ExpiresAt,
	})
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Repository) loadSubject(ctx context.Context, query queryRower, subjectType, publicID string, lock bool) (Subject, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	var subject Subject
	var authorizationID sql.NullInt64
	var authorizationPublicID, authorizedHash sql.NullString
	var authorizationExpiresAt sql.NullTime
	var incidentID sql.NullString
	switch subjectType {
	case SubjectActionCard:
		subject.SubjectType = SubjectActionCard
		err := query.QueryRowContext(ctx, `SELECT card.id,card.public_id,run.public_id,incident.public_id,
revision.id,revision.public_id,card.action_type,card.target_json,card.parameters_json,card.preconditions_json,
card.risk,card.content_hash,card.status,card.expires_at,
authorization.id,authorization.public_id,authorization.authorized_content_hash,authorization.expires_at
FROM agent_action_cards card
JOIN agent_runs run ON run.id=card.agent_run_id
JOIN configuration_revisions revision ON revision.id=run.configuration_revision_id
LEFT JOIN incidents incident ON incident.id=run.incident_id
LEFT JOIN agent_action_authorizations authorization
  ON authorization.action_card_id=card.id AND authorization.subject_type='action_card'
WHERE card.public_id=?`+lockClause, publicID).Scan(
			&subject.SubjectInternalID, &subject.SubjectID, &subject.RunID, &incidentID,
			&subject.ConfigurationRevisionInternalID, &subject.ConfigurationRevisionID,
			&subject.OperationType, &subject.Target, &subject.Parameters, &subject.Preconditions,
			&subject.Risk, &subject.ContentHash, &subject.Status, &subject.ExpiresAt,
			&authorizationID, &authorizationPublicID, &authorizedHash, &authorizationExpiresAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return Subject{}, ErrNotFound
		}
		if err != nil {
			return Subject{}, err
		}
		subject.Authority = "reversible"
		subject.IntendedState = json.RawMessage(`{}`)
		subject.VerificationIntent = json.RawMessage(`{}`)
	case SubjectOperationPlan:
		subject.SubjectType = SubjectOperationPlan
		err := query.QueryRowContext(ctx, `SELECT plan.id,plan.public_id,run.public_id,incident.public_id,
revision.id,revision.public_id,plan.operation_type,plan.target_json,plan.parameters_json,plan.intended_state_json,
plan.preconditions_json,plan.risk,plan.verification_intent_json,plan.content_hash,plan.status,plan.expires_at,
authorization.id,authorization.public_id,authorization.authorized_content_hash,authorization.expires_at
FROM agent_operation_plans plan
JOIN agent_runs run ON run.id=plan.agent_run_id
JOIN configuration_revisions revision ON revision.id=plan.configuration_revision_id
LEFT JOIN incidents incident ON incident.id=run.incident_id
LEFT JOIN agent_action_authorizations authorization
  ON authorization.operation_plan_id=plan.id AND authorization.subject_type='operation_plan'
WHERE plan.public_id=?`+lockClause, publicID).Scan(
			&subject.SubjectInternalID, &subject.SubjectID, &subject.RunID, &incidentID,
			&subject.ConfigurationRevisionInternalID, &subject.ConfigurationRevisionID,
			&subject.OperationType, &subject.Target, &subject.Parameters, &subject.IntendedState,
			&subject.Preconditions, &subject.Risk, &subject.VerificationIntent, &subject.ContentHash,
			&subject.Status, &subject.ExpiresAt, &authorizationID, &authorizationPublicID,
			&authorizedHash, &authorizationExpiresAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return Subject{}, ErrNotFound
		}
		if err != nil {
			return Subject{}, err
		}
		subject.Authority = "high_impact"
	default:
		return Subject{}, ErrInvalidArgument
	}
	if incidentID.Valid {
		subject.IncidentID = incidentID.String
	}
	if authorizationID.Valid {
		subject.AuthorizationInternalID = uint64(authorizationID.Int64)
	}
	if authorizationPublicID.Valid {
		subject.AuthorizationID = authorizationPublicID.String
	}
	if authorizedHash.Valid {
		subject.AuthorizedHash = authorizedHash.String
	}
	if authorizationExpiresAt.Valid {
		subject.AuthorizationExpiresAt = authorizationExpiresAt.Time.UTC()
	}
	subject.ExpiresAt = subject.ExpiresAt.UTC()
	return subject, nil
}

func (r *Repository) Ready(ctx context.Context) error {
	var version int64
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		return err
	}
	if version != schemaversion.Latest {
		return fmt.Errorf("unsupported operation schema version %d, want %d", version, schemaversion.Latest)
	}
	var tables int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema=DATABASE() AND table_name IN
('operation_executions','operation_events','operation_verification_observations','operation_change_freezes')`).Scan(&tables); err != nil {
		return err
	}
	if tables != 4 {
		return ErrProviderUnavailable
	}
	return nil
}

func appendEvent(ctx context.Context, tx *sql.Tx, executionID uint64, eventType string, payload any, occurredAt time.Time) error {
	var sequence uint32
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_no),0)+1 FROM operation_events WHERE execution_id=? FOR UPDATE`, executionID).
		Scan(&sequence); err != nil {
		return err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	hash, _, err := hashJSON(struct {
		ExecutionID uint64          `json:"execution_id"`
		Sequence    uint32          `json:"sequence"`
		Type        string          `json:"type"`
		Payload     json.RawMessage `json:"payload"`
		OccurredAt  time.Time       `json:"occurred_at"`
	}{executionID, sequence, eventType, payloadJSON, occurredAt.UTC()})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operation_events
(public_id,execution_id,sequence_no,event_type,payload_json,content_hash,occurred_at,created_at)
VALUES (?,?,?,?,?,?,?,?)`, uuid.NewString(), executionID, sequence, eventType, payloadJSON, hash, occurredAt.UTC(), occurredAt.UTC())
	return err
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
