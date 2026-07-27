package agent

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (r *WorkspaceRepository) ProposeActionCard(ctx context.Context, request ActionProposalRequest) (ActionCard, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ActionCard{}, err
	}
	defer workspaceRollback(tx)
	var runID uint64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM agent_runs WHERE public_id=? AND run_kind='workspace' FOR UPDATE`,
		strings.TrimSpace(request.RunID)).Scan(&runID); errors.Is(err, sql.ErrNoRows) {
		return ActionCard{}, ErrNotFound
	} else if err != nil {
		return ActionCard{}, err
	}
	expiresAt := authorityTimestamp(request.ExpiresAt)
	hash, err := ActionCardContentHash(ActionCard{
		Authority: "reversible", ActionType: request.ActionType, Target: request.Target,
		Parameters: request.Parameters, Preconditions: request.Preconditions, Risk: request.Risk,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return ActionCard{}, ErrInvalidArgument
	}
	publicID, now := uuid.NewString(), r.now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_action_cards
(public_id,agent_run_id,action_type,target_json,parameters_json,preconditions_json,risk,content_hash,status,expires_at,created_at)
	VALUES (?,?,?,?,?,?,?,?,'proposed',?,?)
	ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, publicID, runID, request.ActionType, request.Target,
		request.Parameters, request.Preconditions, request.Risk, hash, expiresAt, now)
	if err != nil {
		return ActionCard{}, fmt.Errorf("persist reversible action card: %w", err)
	}
	internalID, err := result.LastInsertId()
	if err != nil || internalID <= 0 {
		return ActionCard{}, fmt.Errorf("read reversible action card identity: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT public_id FROM agent_action_cards WHERE id=?`, internalID).Scan(&publicID); err != nil {
		return ActionCard{}, err
	}
	if err = tx.Commit(); err != nil {
		return ActionCard{}, err
	}
	return r.ActionCard(ctx, publicID)
}

func (r *WorkspaceRepository) ProposeOperationPlan(ctx context.Context, request ActionProposalRequest) (OperationPlan, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return OperationPlan{}, err
	}
	defer workspaceRollback(tx)
	var runID, revisionID uint64
	var revisionPublicID string
	err = tx.QueryRowContext(ctx, `SELECT run.id,run.configuration_revision_id,revision.public_id
FROM agent_runs run JOIN configuration_revisions revision ON revision.id=run.configuration_revision_id
WHERE run.public_id=? AND run.run_kind='workspace' FOR UPDATE`, strings.TrimSpace(request.RunID)).Scan(
		&runID, &revisionID, &revisionPublicID)
	if errors.Is(err, sql.ErrNoRows) {
		return OperationPlan{}, ErrNotFound
	}
	if err != nil {
		return OperationPlan{}, err
	}
	expiresAt := authorityTimestamp(request.ExpiresAt)
	hash, err := OperationPlanContentHash(OperationPlan{
		Authority: "high_impact", ConfigurationRevisionID: revisionPublicID,
		OperationType: request.ActionType, Target: request.Target, Parameters: request.Parameters,
		IntendedState: request.IntendedState, Preconditions: request.Preconditions, Risk: request.Risk,
		VerificationIntent: request.VerificationIntent, ExpiresAt: expiresAt,
	})
	if err != nil {
		return OperationPlan{}, ErrInvalidArgument
	}
	publicID, now := uuid.NewString(), r.now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_operation_plans
(public_id,agent_run_id,configuration_revision_id,operation_type,target_json,parameters_json,intended_state_json,
 preconditions_json,risk,verification_intent_json,content_hash,status,expires_at,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,'proposed',?,?)
	ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, publicID, runID, revisionID, request.ActionType, request.Target,
		request.Parameters, request.IntendedState, request.Preconditions, request.Risk, request.VerificationIntent,
		hash, expiresAt, now)
	if err != nil {
		return OperationPlan{}, fmt.Errorf("persist immutable Operation Plan: %w", err)
	}
	internalID, err := result.LastInsertId()
	if err != nil || internalID <= 0 {
		return OperationPlan{}, fmt.Errorf("read immutable Operation Plan identity: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT public_id FROM agent_operation_plans WHERE id=?`, internalID).Scan(&publicID); err != nil {
		return OperationPlan{}, err
	}
	if err = tx.Commit(); err != nil {
		return OperationPlan{}, err
	}
	return r.OperationPlan(ctx, publicID)
}

// ActionCardContentHash is the canonical identity checked again immediately
// before a reversible effect. It intentionally excludes mutable projection
// fields such as status and authorization.
func ActionCardContentHash(card ActionCard) (string, error) {
	target, err := canonicalAuthorityJSON(card.Target)
	if err != nil {
		return "", err
	}
	parameters, err := canonicalAuthorityJSON(card.Parameters)
	if err != nil {
		return "", err
	}
	preconditions, err := canonicalAuthorityJSON(card.Preconditions)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Authority     string    `json:"authority"`
		ActionType    string    `json:"action_type"`
		Target        any       `json:"target"`
		Parameters    any       `json:"parameters"`
		Preconditions any       `json:"preconditions"`
		Risk          string    `json:"risk"`
		ExpiresAt     time.Time `json:"expires_at"`
	}{card.Authority, card.ActionType, target, parameters, preconditions, card.Risk, authorityTimestamp(card.ExpiresAt)})
	if err != nil {
		return "", err
	}
	return workspaceSHA256(canonical), nil
}

// OperationPlanContentHash covers every material Plan field required by the
// authority contract, including Configuration Revision, expiry, and the
// verification intent. Recomputing it makes stored-field drift fail closed.
func OperationPlanContentHash(plan OperationPlan) (string, error) {
	target, err := canonicalAuthorityJSON(plan.Target)
	if err != nil {
		return "", err
	}
	parameters, err := canonicalAuthorityJSON(plan.Parameters)
	if err != nil {
		return "", err
	}
	intendedState, err := canonicalAuthorityJSON(plan.IntendedState)
	if err != nil {
		return "", err
	}
	preconditions, err := canonicalAuthorityJSON(plan.Preconditions)
	if err != nil {
		return "", err
	}
	verificationIntent, err := canonicalAuthorityJSON(plan.VerificationIntent)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Authority             string    `json:"authority"`
		ConfigurationRevision string    `json:"configuration_revision_id"`
		OperationType         string    `json:"operation_type"`
		Target                any       `json:"target"`
		Parameters            any       `json:"parameters"`
		IntendedState         any       `json:"intended_state"`
		Preconditions         any       `json:"preconditions"`
		Risk                  string    `json:"risk"`
		VerificationIntent    any       `json:"verification_intent"`
		ExpiresAt             time.Time `json:"expires_at"`
	}{plan.Authority, plan.ConfigurationRevisionID, plan.OperationType, target, parameters,
		intendedState, preconditions, plan.Risk, verificationIntent, authorityTimestamp(plan.ExpiresAt)})
	if err != nil {
		return "", err
	}
	return workspaceSHA256(canonical), nil
}

func canonicalAuthorityJSON(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("authority JSON contains trailing content")
	}
	return value, nil
}

func authorityTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func (r *WorkspaceRepository) AuthorizeActionCard(ctx context.Context, publicID string, request AuthorizeActionRequest) (ActionCard, error) {
	if err := r.authorize(ctx, "action_card", strings.TrimSpace(publicID), request); err != nil {
		return ActionCard{}, err
	}
	return r.ActionCard(ctx, publicID)
}

func (r *WorkspaceRepository) AuthorizeOperationPlan(ctx context.Context, publicID string, request AuthorizeActionRequest) (OperationPlan, error) {
	if err := r.authorize(ctx, "operation_plan", strings.TrimSpace(publicID), request); err != nil {
		return OperationPlan{}, err
	}
	return r.OperationPlan(ctx, publicID)
}

func (r *WorkspaceRepository) authorize(ctx context.Context, subjectType, publicID string, request AuthorizeActionRequest) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	table, foreignColumn := "agent_action_cards", "action_card_id"
	if subjectType == "operation_plan" {
		table, foreignColumn = "agent_operation_plans", "operation_plan_id"
	}
	var internalID uint64
	var hash, status string
	var expiresAt time.Time
	query := `SELECT id,content_hash,status,expires_at FROM ` + table + ` WHERE public_id=? FOR UPDATE`
	if err = tx.QueryRowContext(ctx, query, publicID).Scan(&internalID, &hash, &status, &expiresAt); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	now := r.now().UTC()
	if status != "proposed" || !expiresAt.After(now) || request.ExpectedHash != hash {
		return ErrConflict
	}
	insert := `INSERT INTO agent_action_authorizations
(public_id,subject_type,` + foreignColumn + `,authorized_content_hash,authorized_by,reason,expires_at,created_at)
VALUES (?,?,?,?, 'local-owner',?,?,?)`
	if _, err = tx.ExecContext(ctx, insert, uuid.NewString(), subjectType, internalID, hash, request.Reason, expiresAt.UTC(), now); err != nil {
		return fmt.Errorf("persist exact action authorization: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET status='authorized' WHERE id=? AND status='proposed'`, internalID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) ActionCard(ctx context.Context, publicID string) (ActionCard, error) {
	item, _, err := scanActionCard(r.db.QueryRowContext(ctx, actionCardSelect+` WHERE card.public_id=?`, strings.TrimSpace(publicID)), r.now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return ActionCard{}, ErrNotFound
	}
	return item, err
}

func (r *WorkspaceRepository) OperationPlan(ctx context.Context, publicID string) (OperationPlan, error) {
	item, _, err := scanOperationPlan(r.db.QueryRowContext(ctx, operationPlanSelect+` WHERE plan.public_id=?`, strings.TrimSpace(publicID)), r.now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return OperationPlan{}, ErrNotFound
	}
	return item, err
}

func (r *WorkspaceRepository) OperationPlans(ctx context.Context, limit int) ([]OperationPlan, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, operationPlanSelect+` ORDER BY plan.created_at DESC,plan.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]OperationPlan, 0)
	for rows.Next() {
		item, _, scanErr := scanOperationPlan(rows, r.now().UTC())
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) ActionCards(ctx context.Context, limit int) ([]ActionCard, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, actionCardSelect+` ORDER BY card.created_at DESC,card.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ActionCard, 0)
	for rows.Next() {
		item, _, scanErr := scanActionCard(rows, r.now().UTC())
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) actionCards(ctx context.Context, runID uint64) ([]ActionCard, error) {
	rows, err := r.db.QueryContext(ctx, actionCardSelect+` WHERE card.agent_run_id=? ORDER BY card.created_at,card.id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ActionCard, 0)
	for rows.Next() {
		item, _, scanErr := scanActionCard(rows, r.now().UTC())
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *WorkspaceRepository) operationPlans(ctx context.Context, runID uint64) ([]OperationPlan, error) {
	rows, err := r.db.QueryContext(ctx, operationPlanSelect+` WHERE plan.agent_run_id=? ORDER BY plan.created_at,plan.id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]OperationPlan, 0)
	for rows.Next() {
		item, _, scanErr := scanOperationPlan(rows, r.now().UTC())
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const actionCardSelect = `SELECT card.id,card.public_id,run.public_id,card.action_type,card.target_json,
card.parameters_json,card.preconditions_json,card.risk,card.content_hash,card.status,card.expires_at,card.created_at,
authorization.public_id,authorization.authorized_content_hash,authorization.authorized_by,authorization.reason,
authorization.expires_at,authorization.created_at
FROM agent_action_cards card JOIN agent_runs run ON run.id=card.agent_run_id
LEFT JOIN agent_action_authorizations authorization ON authorization.action_card_id=card.id`

const operationPlanSelect = `SELECT plan.id,plan.public_id,run.public_id,revision.public_id,plan.operation_type,
plan.target_json,plan.parameters_json,plan.intended_state_json,plan.preconditions_json,plan.risk,
plan.verification_intent_json,plan.content_hash,plan.status,plan.expires_at,plan.created_at,
authorization.public_id,authorization.authorized_content_hash,authorization.authorized_by,authorization.reason,
authorization.expires_at,authorization.created_at
FROM agent_operation_plans plan JOIN agent_runs run ON run.id=plan.agent_run_id
JOIN configuration_revisions revision ON revision.id=plan.configuration_revision_id
LEFT JOIN agent_action_authorizations authorization ON authorization.operation_plan_id=plan.id`

func scanActionCard(scanner interface{ Scan(...any) error }, now time.Time) (ActionCard, uint64, error) {
	var item ActionCard
	var internalID uint64
	var authorizationID, authorizedHash, authorizedBy, reason sql.NullString
	var authorizationExpires, authorizationCreated sql.NullTime
	err := scanner.Scan(&internalID, &item.ID, &item.RunID, &item.ActionType, &item.Target, &item.Parameters,
		&item.Preconditions, &item.Risk, &item.ContentHash, &item.Status, &item.ExpiresAt, &item.CreatedAt,
		&authorizationID, &authorizedHash, &authorizedBy, &reason, &authorizationExpires, &authorizationCreated)
	if err != nil {
		return ActionCard{}, 0, err
	}
	item.Authority = "reversible"
	item.ExpiresAt, item.CreatedAt = item.ExpiresAt.UTC(), item.CreatedAt.UTC()
	if item.Status == "proposed" && !item.ExpiresAt.After(now) {
		item.Status = "expired"
	}
	item.Authorization = projectedAuthorization("action_card", item.ID, authorizationID, authorizedHash, authorizedBy,
		reason, authorizationExpires, authorizationCreated)
	return item, internalID, nil
}

func scanOperationPlan(scanner interface{ Scan(...any) error }, now time.Time) (OperationPlan, uint64, error) {
	var item OperationPlan
	var internalID uint64
	var authorizationID, authorizedHash, authorizedBy, reason sql.NullString
	var authorizationExpires, authorizationCreated sql.NullTime
	err := scanner.Scan(&internalID, &item.ID, &item.RunID, &item.ConfigurationRevisionID, &item.OperationType,
		&item.Target, &item.Parameters, &item.IntendedState, &item.Preconditions, &item.Risk,
		&item.VerificationIntent, &item.ContentHash, &item.Status, &item.ExpiresAt, &item.CreatedAt,
		&authorizationID, &authorizedHash, &authorizedBy, &reason, &authorizationExpires, &authorizationCreated)
	if err != nil {
		return OperationPlan{}, 0, err
	}
	item.Authority = "high_impact"
	item.ExpiresAt, item.CreatedAt = item.ExpiresAt.UTC(), item.CreatedAt.UTC()
	if item.Status == "proposed" && !item.ExpiresAt.After(now) {
		item.Status = "expired"
	}
	item.Authorization = projectedAuthorization("operation_plan", item.ID, authorizationID, authorizedHash, authorizedBy,
		reason, authorizationExpires, authorizationCreated)
	return item, internalID, nil
}

func projectedAuthorization(subjectType, subjectID string, id, hash, owner, reason sql.NullString, expires, created sql.NullTime) *ActionAuthorization {
	if !id.Valid {
		return nil
	}
	return &ActionAuthorization{
		ID: id.String, SubjectType: subjectType, SubjectID: subjectID, AuthorizedHash: hash.String,
		AuthorizedBy: owner.String, Reason: reason.String, ExpiresAt: expires.Time.UTC(), CreatedAt: created.Time.UTC(),
	}
}
