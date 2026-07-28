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

type LocalChangeFreezeAdapter struct {
	repository *Repository
}

func NewLocalChangeFreezeAdapter(repository *Repository) (*LocalChangeFreezeAdapter, error) {
	if repository == nil || repository.db == nil {
		return nil, ErrInvalidArgument
	}
	return &LocalChangeFreezeAdapter{repository: repository}, nil
}

func (*LocalChangeFreezeAdapter) OperationType() string { return ActionSetChangeFreeze }

type changeFreezeParameters struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

type changeFreezePrecondition struct {
	Type            string `json:"type"`
	ExpectedEnabled bool   `json:"expected_enabled"`
	ExpectedVersion uint64 `json:"expected_version"`
}

type changeFreezeToken struct {
	Target          OperationTarget `json:"target"`
	Enabled         bool            `json:"enabled"`
	Reason          string          `json:"reason"`
	ExpectedEnabled bool            `json:"expected_enabled"`
	ExpectedVersion uint64          `json:"expected_version"`
}

func (a *LocalChangeFreezeAdapter) Prepare(ctx context.Context, subject Subject) (PreparedEffect, error) {
	if subject.SubjectType != SubjectActionCard || subject.Authority != "reversible" || subject.OperationType != ActionSetChangeFreeze {
		return PreparedEffect{}, ErrInvalidArgument
	}
	var target OperationTarget
	if err := decodeExact(subject.Target, &target); err != nil || validateTarget(target) != nil {
		return PreparedEffect{}, ErrInvalidArgument
	}
	var parameters changeFreezeParameters
	if err := decodeExact(subject.Parameters, &parameters); err != nil {
		return PreparedEffect{}, err
	}
	parameters.Reason = strings.TrimSpace(parameters.Reason)
	if parameters.Reason == "" || len(parameters.Reason) > 1024 {
		return PreparedEffect{}, ErrInvalidArgument
	}
	var preconditions []changeFreezePrecondition
	if err := decodeExact(subject.Preconditions, &preconditions); err != nil || len(preconditions) != 1 || preconditions[0].Type != "local.change_freeze" {
		return PreparedEffect{}, ErrInvalidArgument
	}
	current, err := a.repository.ChangeFreeze(ctx, target)
	if err != nil {
		return PreparedEffect{}, err
	}
	precondition := preconditions[0]
	if current.Enabled != precondition.ExpectedEnabled || current.RowVersion != precondition.ExpectedVersion {
		return PreparedEffect{}, wrapPrecondition("change freeze state or version changed")
	}
	token, err := json.Marshal(changeFreezeToken{
		Target: target, Enabled: parameters.Enabled, Reason: parameters.Reason,
		ExpectedEnabled: precondition.ExpectedEnabled, ExpectedVersion: precondition.ExpectedVersion,
	})
	if err != nil {
		return PreparedEffect{}, err
	}
	before, err := changeFreezeObservation(current, "change freeze precondition matched", a.repository.now().UTC())
	if err != nil {
		return PreparedEffect{}, err
	}
	return PreparedEffect{External: false, Before: before, Token: token}, nil
}

func (a *LocalChangeFreezeAdapter) Apply(ctx context.Context, subject Subject, prepared PreparedEffect) (Observation, error) {
	if prepared.External || subject.ExecutionInternalID == 0 {
		return Observation{}, ErrInvalidArgument
	}
	var token changeFreezeToken
	if err := decodeExact(prepared.Token, &token); err != nil || validateTarget(token.Target) != nil {
		return Observation{}, ErrInvalidArgument
	}
	tx, err := a.repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Observation{}, err
	}
	defer rollback(tx)
	current, found, err := changeFreezeFrom(ctx, tx, token.Target, true)
	if err != nil {
		return Observation{}, err
	}
	if current.Enabled != token.ExpectedEnabled || current.RowVersion != token.ExpectedVersion {
		return Observation{}, wrapPrecondition("change freeze changed before local effect")
	}
	now := a.repository.now().UTC()
	identityHash, _, err := hashJSON(token.Target)
	if err != nil {
		return Observation{}, err
	}
	if !found {
		if token.ExpectedVersion != 0 || token.ExpectedEnabled {
			return Observation{}, ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO operation_change_freezes
(public_id,target_identity_hash,cluster_id,environment,namespace,workload_kind,workload_name,
enabled,reason,row_version,updated_by_execution_id,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,1,?,?,?)`, uuid.NewString(), identityHash, token.Target.ClusterID,
			token.Target.Environment, token.Target.Namespace, token.Target.WorkloadKind, token.Target.WorkloadName,
			token.Enabled, token.Reason, subject.ExecutionInternalID, now, now)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE operation_change_freezes
SET enabled=?,reason=?,row_version=row_version+1,updated_by_execution_id=?,updated_at=?
WHERE target_identity_hash=? AND row_version=? AND enabled=?`, token.Enabled, token.Reason,
			subject.ExecutionInternalID, now, identityHash, token.ExpectedVersion, token.ExpectedEnabled)
		if err == nil {
			if rows, _ := result.RowsAffected(); rows != 1 {
				return Observation{}, ErrConflict
			}
		}
	}
	if err != nil {
		return Observation{}, fmt.Errorf("apply local change freeze: %w", err)
	}
	updated, _, err := changeFreezeFrom(ctx, tx, token.Target, false)
	if err != nil {
		return Observation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Observation{}, err
	}
	verified := updated.Enabled == token.Enabled && updated.Reason == token.Reason
	summary := "current local change freeze matches the authorized state"
	if !verified {
		summary = "current local change freeze does not match the authorized state"
	}
	return changeFreezeObservation(updated, summary, now)
}

func (r *Repository) ChangeFreeze(ctx context.Context, target OperationTarget) (ChangeFreezeState, error) {
	if validateTarget(target) != nil {
		return ChangeFreezeState{}, ErrInvalidArgument
	}
	state, _, err := changeFreezeFrom(ctx, r.db, target, false)
	return state, err
}

func changeFreezeFrom(ctx context.Context, query queryRower, target OperationTarget, lock bool) (ChangeFreezeState, bool, error) {
	identityHash, _, err := hashJSON(target)
	if err != nil {
		return ChangeFreezeState{}, false, err
	}
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	state := ChangeFreezeState{Target: target}
	var updatedAt time.Time
	err = query.QueryRowContext(ctx, `SELECT enabled,reason,row_version,updated_at
FROM operation_change_freezes WHERE target_identity_hash=?`+lockClause, identityHash).
		Scan(&state.Enabled, &state.Reason, &state.RowVersion, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	if err != nil {
		return ChangeFreezeState{}, false, err
	}
	value := updatedAt.UTC()
	state.UpdatedAt = &value
	return state, true, nil
}

func changeFreezeObservation(state ChangeFreezeState, summary string, observedAt time.Time) (Observation, error) {
	identityHash, _, err := hashJSON(state.Target)
	if err != nil {
		return Observation{}, err
	}
	identity, err := json.Marshal(map[string]any{
		"provider": "mysql", "target_identity_hash": identityHash, "resource": "operation_change_freezes",
	})
	if err != nil {
		return Observation{}, err
	}
	evidence, err := json.Marshal(map[string]any{
		"target": state.Target, "enabled": state.Enabled, "reason": state.Reason,
		"row_version": state.RowVersion, "observed_at": observedAt.UTC(),
	})
	if err != nil {
		return Observation{}, err
	}
	return Observation{
		Source: "local", ProviderIdentity: identity, Evidence: evidence,
		Verified: strings.Contains(summary, "matches"), Summary: summary, ObservedAt: observedAt.UTC(),
	}, nil
}
