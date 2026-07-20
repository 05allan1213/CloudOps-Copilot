// Package businessbudget owns the frozen per-Incident-cycle record budgets.
// Every guard locks the Incident row before counting, so compliant producers
// serialize their count, child insert, and task enqueue in one transaction.
package businessbudget

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

const (
	DefaultLimit = 3
	HardLimit    = 5
)

var (
	ErrInvalidKind           = errors.New("invalid business budget kind")
	ErrInvalidAuthorization  = errors.New("invalid business budget authorization")
	ErrAuthorizationConflict = errors.New("business budget authorization conflicts with an existing slot")
	ErrAuthorizationUsed     = errors.New("business budget authorization already used")
)

type Kind string

const (
	KindAgentRun        Kind = "agent_run"
	KindRemediationPlan Kind = "remediation_plan"
	KindVerificationRun Kind = "verification_run"
)

func (k Kind) Valid() bool {
	switch k {
	case KindAgentRun, KindRemediationPlan, KindVerificationRun:
		return true
	default:
		return false
	}
}

type Outcome string

const (
	OutcomeAllowed          Outcome = "allowed"
	OutcomeDefaultExhausted Outcome = "default_exhausted"
	OutcomeHardExhausted    Outcome = "hard_exhausted"
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Result struct {
	Kind                        Kind
	Outcome                     Outcome
	Count                       int
	IncidentVersion             uint64
	AuthorizationID             uint64
	AuthorizationPublicID       string
	AuthorizationSlot           int
	OriginatingAgentRunID       uint64
	OriginatingAgentRunPublicID string
}

func (r Result) Allowed() bool { return r.Outcome == OutcomeAllowed }

type Actor struct {
	Provider               string
	Login                  string
	Role                   string
	Reason                 string
	RequestID              string
	RequestAuthenticatedAt time.Time
}

type Authorization struct {
	ID       uint64
	PublicID string
	Slot     int
}

// AuthorizeAgentRun reserves the next explicit AgentRun slot. Callers must
// keep the returned authorization and the investigation.start enqueue in the
// same transaction.
func AuthorizeAgentRun(ctx context.Context, tx DBTX, incidentID uint64, cycleNo uint32, actor Actor) (Authorization, Result, error) {
	if tx == nil || incidentID == 0 || cycleNo == 0 {
		return Authorization{}, Result{}, ErrInvalidAuthorization
	}
	incidentVersion, err := lockIncident(ctx, tx, incidentID, cycleNo)
	if err != nil {
		return Authorization{}, Result{}, err
	}
	count, err := countRecords(ctx, tx, KindAgentRun, incidentID, cycleNo)
	if err != nil {
		return Authorization{}, Result{}, err
	}
	result := Result{Kind: KindAgentRun, Count: count, IncidentVersion: incidentVersion}
	if count >= HardLimit {
		result.Outcome = OutcomeHardExhausted
		return Authorization{}, result, nil
	}
	if count < DefaultLimit {
		result.Outcome = OutcomeAllowed
		return Authorization{}, result, nil
	}
	actor.Login = strings.TrimSpace(actor.Login)
	actor.Reason = strings.TrimSpace(actor.Reason)
	actor.RequestID = strings.TrimSpace(actor.RequestID)
	if err := validateActor(actor); err != nil {
		return Authorization{}, Result{}, err
	}

	publicID := uuid.NewString()
	slot := count + 1
	databaseTime := actor.RequestAuthenticatedAt.UTC()
	if databaseTime.IsZero() {
		if err := tx.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseTime); err != nil {
			return Authorization{}, Result{}, fmt.Errorf("read budget authorization time: %w", err)
		}
	}
	insert, err := tx.ExecContext(ctx, `
INSERT INTO incident_cycle_budget_authorizations
    (public_id, domain_schema_version, authorization_schema_version, incident_id,
     cycle_no, budget_kind, slot_no, actor_provider, actor_login, actor_role,
     reason, request_id, request_authenticated_at, created_at)
VALUES (?, 3, 1, ?, ?, 'agent_run', ?, ?, ?, ?, ?, ?, ?, NOW(6))`,
		publicID, incidentID, cycleNo, slot, actor.Provider, actor.Login, actor.Role,
		actor.Reason, actor.RequestID, databaseTime)
	if err != nil {
		var mysqlError *drivermysql.MySQLError
		if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
			return Authorization{}, Result{}, ErrAuthorizationConflict
		}
		return Authorization{}, Result{}, fmt.Errorf("persist AgentRun retry authorization: %w", err)
	}
	id, err := insert.LastInsertId()
	if err != nil || id <= 0 {
		return Authorization{}, Result{}, fmt.Errorf("read AgentRun retry authorization id: %w", err)
	}
	result.Outcome = OutcomeAllowed
	result.AuthorizationID = uint64(id)
	result.AuthorizationPublicID = publicID
	result.AuthorizationSlot = slot
	return Authorization{ID: uint64(id), PublicID: publicID, Slot: slot}, result, nil
}

// GuardAutomatic allows only the default three records. It never consumes an
// operator authorization.
func GuardAutomatic(ctx context.Context, tx DBTX, kind Kind, incidentID uint64, cycleNo uint32) (Result, error) {
	if err := validateGuard(tx, kind, incidentID, cycleNo); err != nil {
		return Result{}, err
	}
	incidentVersion, err := lockIncident(ctx, tx, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	count, err := countRecords(ctx, tx, kind, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	result := budgetOutcome(kind, count, false)
	result.IncidentVersion = incidentVersion
	return result, nil
}

// GuardAgentRun is the final owner guard. Slots four and five require the
// public ID of an unconsumed durable authorization for the exact cycle/slot.
func GuardAgentRun(ctx context.Context, tx DBTX, incidentID uint64, cycleNo uint32, authorizationPublicID string) (Result, error) {
	if err := validateGuard(tx, KindAgentRun, incidentID, cycleNo); err != nil {
		return Result{}, err
	}
	incidentVersion, err := lockIncident(ctx, tx, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	count, err := countRecords(ctx, tx, KindAgentRun, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	if count >= HardLimit {
		return Result{Kind: KindAgentRun, Outcome: OutcomeHardExhausted, Count: count, IncidentVersion: incidentVersion}, nil
	}
	authorizationPublicID = strings.TrimSpace(authorizationPublicID)
	if count < DefaultLimit {
		if authorizationPublicID != "" {
			return Result{}, fmt.Errorf("%w: authorization is not valid before slot four", ErrInvalidAuthorization)
		}
		return Result{Kind: KindAgentRun, Outcome: OutcomeAllowed, Count: count, IncidentVersion: incidentVersion}, nil
	}
	if authorizationPublicID == "" {
		return Result{Kind: KindAgentRun, Outcome: OutcomeDefaultExhausted, Count: count, IncidentVersion: incidentVersion}, nil
	}
	authorization, err := loadAuthorization(ctx, tx, authorizationPublicID, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	if authorization.Slot != count+1 {
		return Result{}, fmt.Errorf("%w: authorization slot %d cannot create slot %d", ErrInvalidAuthorization, authorization.Slot, count+1)
	}
	var used int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs
WHERE business_budget_authorization_id = ?`, authorization.ID).Scan(&used); err != nil {
		return Result{}, fmt.Errorf("check AgentRun budget authorization use: %w", err)
	}
	if used != 0 {
		return Result{}, ErrAuthorizationUsed
	}
	return Result{
		Kind: KindAgentRun, Outcome: OutcomeAllowed, Count: count, IncidentVersion: incidentVersion,
		AuthorizationID: authorization.ID, AuthorizationPublicID: authorization.PublicID,
		AuthorizationSlot: authorization.Slot,
	}, nil
}

// GuardChild derives authorization only from the durable originating AgentRun.
// An authorization can bind at most one Plan and at most one VerificationRun.
func GuardChild(ctx context.Context, tx DBTX, kind Kind, incidentID uint64, cycleNo uint32, originatingAgentRunID uint64) (Result, error) {
	if kind != KindRemediationPlan && kind != KindVerificationRun {
		return Result{}, ErrInvalidKind
	}
	if err := validateGuard(tx, kind, incidentID, cycleNo); err != nil {
		return Result{}, err
	}
	incidentVersion, err := lockIncident(ctx, tx, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	count, err := countRecords(ctx, tx, kind, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	if count >= HardLimit {
		return Result{Kind: kind, Outcome: OutcomeHardExhausted, Count: count, IncidentVersion: incidentVersion, OriginatingAgentRunID: originatingAgentRunID}, nil
	}
	result := Result{Kind: kind, Outcome: OutcomeAllowed, Count: count, IncidentVersion: incidentVersion, OriginatingAgentRunID: originatingAgentRunID}
	if originatingAgentRunID == 0 {
		if count >= DefaultLimit {
			result.Outcome = OutcomeDefaultExhausted
		}
		return result, nil
	}
	authorization, originatingAgentRunPublicID, err := loadAgentRunAuthorization(ctx, tx, originatingAgentRunID, incidentID, cycleNo)
	if err != nil {
		return Result{}, err
	}
	result.OriginatingAgentRunPublicID = originatingAgentRunPublicID
	if authorization.ID == 0 {
		if count >= DefaultLimit {
			result.Outcome = OutcomeDefaultExhausted
		}
		return result, nil
	}
	used, err := childAuthorizationUsed(ctx, tx, kind, authorization.ID)
	if err != nil {
		return Result{}, err
	}
	if used {
		return Result{}, ErrAuthorizationUsed
	}
	result.AuthorizationID = authorization.ID
	result.AuthorizationPublicID = authorization.PublicID
	result.AuthorizationSlot = authorization.Slot
	return result, nil
}

// CurrentOriginatingAgentRun returns only the Incident projection's exact
// current AgentRun when it belongs to the same V3 cycle. It deliberately does
// not fall back to any older cycle-level authorization.
func CurrentOriginatingAgentRun(ctx context.Context, tx DBTX, incidentID uint64, cycleNo uint32) (uint64, error) {
	if tx == nil || incidentID == 0 || cycleNo == 0 {
		return 0, ErrInvalidAuthorization
	}
	var runID sql.NullInt64
	var ownerIncident, ownerCycle sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT i.current_agent_run_id, ar.incident_id, ar.cycle_no
FROM incidents i
LEFT JOIN agent_runs ar ON ar.id = i.current_agent_run_id AND ar.domain_schema_version = 3
WHERE i.id = ? AND i.domain_schema_version = 3 AND i.cycle_no = ?
FOR UPDATE`, incidentID, cycleNo).Scan(&runID, &ownerIncident, &ownerCycle)
	if err != nil {
		return 0, err
	}
	if !runID.Valid {
		return 0, nil
	}
	if !ownerIncident.Valid || !ownerCycle.Valid || uint64(ownerIncident.Int64) != incidentID || uint64(ownerCycle.Int64) != uint64(cycleNo) {
		return 0, fmt.Errorf("%w: current AgentRun escaped the Incident cycle", ErrInvalidAuthorization)
	}
	return uint64(runID.Int64), nil
}

// MarkExhausted is a successful domain mutation: it leaves the Incident open,
// marks attention, and writes a kind-specific Timeline without creating work.
func MarkExhausted(ctx context.Context, tx DBTX, result Result, incidentID uint64, cycleNo uint32, source string) error {
	if tx == nil || !result.Kind.Valid() || incidentID == 0 || cycleNo == 0 ||
		(result.Outcome != OutcomeDefaultExhausted && result.Outcome != OutcomeHardExhausted) {
		return ErrInvalidKind
	}
	source = strings.TrimSpace(source)
	if source == "" || len(source) > 128 {
		return ErrInvalidKind
	}
	reason := string(result.Kind) + "_budget_exhausted"
	if result.Outcome == OutcomeHardExhausted {
		reason = string(result.Kind) + "_hard_limit_exhausted"
	}
	updated, err := tx.ExecContext(ctx, `UPDATE incidents
SET version = CASE WHEN needs_attention = TRUE AND blocking_reason_code = ? THEN version ELSE version + 1 END,
    needs_attention = TRUE, blocking_reason_code = ?, blocked_at = COALESCE(blocked_at, NOW(6)),
    updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ?
  AND v3_status IN ('detected','investigating','awaiting_approval','delivering','verifying')`,
		reason, reason, incidentID, cycleNo, result.IncidentVersion)
	if err != nil {
		return fmt.Errorf("mark Incident business budget exhausted: %w", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return errors.New("business budget Incident is no longer open")
	}
	metadata, err := json.Marshal(map[string]any{
		"budget_kind": result.Kind, "count": result.Count, "default_limit": DefaultLimit,
		"hard_limit": HardLimit, "outcome": result.Outcome, "source": source,
	})
	if err != nil || len(metadata) > 8192 {
		return errors.New("business budget Timeline metadata is invalid")
	}
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO incident_events
    (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
     event_type, idempotency_key, actor_type, actor_id, summary, metadata_json,
     occurred_at, created_at)
VALUES (?, ?, 3, ?, 1, ?, ?, 'system', ?, ?, ?, NOW(6), NOW(6))`,
		uuid.NewString(), incidentID, cycleNo, reason,
		hashCanonical("business-budget", fmt.Sprint(incidentID), fmt.Sprint(cycleNo), reason, fmt.Sprint(result.Count), source),
		source, strings.ReplaceAll(reason, "_", " "), metadata)
	return err
}

func budgetOutcome(kind Kind, count int, authorized bool) Result {
	result := Result{Kind: kind, Count: count}
	switch {
	case count >= HardLimit:
		result.Outcome = OutcomeHardExhausted
	case count >= DefaultLimit && !authorized:
		result.Outcome = OutcomeDefaultExhausted
	default:
		result.Outcome = OutcomeAllowed
	}
	return result
}

func validateGuard(tx DBTX, kind Kind, incidentID uint64, cycleNo uint32) error {
	if tx == nil || !kind.Valid() || incidentID == 0 || cycleNo == 0 {
		return ErrInvalidKind
	}
	return nil
}

func validateActor(actor Actor) error {
	if actor.Provider != "github" || actor.Role != "operator" || actor.Login == "" || len(actor.Login) > 128 ||
		actor.Reason == "" || len(actor.Reason) > 1024 || actor.RequestID == "" || len(actor.RequestID) > 128 {
		return ErrInvalidAuthorization
	}
	return nil
}

func lockIncident(ctx context.Context, tx DBTX, incidentID uint64, cycleNo uint32) (uint64, error) {
	var foundCycle uint64
	var version uint64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT cycle_no, v3_status, version FROM incidents
WHERE id = ? AND domain_schema_version = 3 FOR UPDATE`, incidentID).Scan(&foundCycle, &status, &version); err != nil {
		return 0, fmt.Errorf("lock business budget Incident: %w", err)
	}
	if foundCycle != uint64(cycleNo) || status == "resolved" || status == "closed" {
		return 0, errors.New("business budget Incident cycle is not open")
	}
	return version, nil
}

func countRecords(ctx context.Context, tx DBTX, kind Kind, incidentID uint64, cycleNo uint32) (int, error) {
	table, err := tableForKind(kind)
	if err != nil {
		return 0, err
	}
	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3"
	if err := tx.QueryRowContext(ctx, query, incidentID, cycleNo).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s business budget: %w", kind, err)
	}
	return count, nil
}

func tableForKind(kind Kind) (string, error) {
	switch kind {
	case KindAgentRun:
		return "agent_runs", nil
	case KindRemediationPlan:
		return "remediation_plans", nil
	case KindVerificationRun:
		return "verification_runs", nil
	default:
		return "", ErrInvalidKind
	}
}

func loadAuthorization(ctx context.Context, tx DBTX, publicID string, incidentID uint64, cycleNo uint32) (Authorization, error) {
	var authorization Authorization
	var kind string
	err := tx.QueryRowContext(ctx, `SELECT id, public_id, budget_kind, slot_no
FROM incident_cycle_budget_authorizations
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
FOR UPDATE`, publicID, incidentID, cycleNo).Scan(&authorization.ID, &authorization.PublicID, &kind, &authorization.Slot)
	if errors.Is(err, sql.ErrNoRows) {
		return Authorization{}, ErrInvalidAuthorization
	}
	if err != nil {
		return Authorization{}, fmt.Errorf("load business budget authorization: %w", err)
	}
	if kind != string(KindAgentRun) || authorization.Slot < DefaultLimit+1 || authorization.Slot > HardLimit {
		return Authorization{}, ErrInvalidAuthorization
	}
	return authorization, nil
}

func loadAgentRunAuthorization(ctx context.Context, tx DBTX, runID, incidentID uint64, cycleNo uint32) (Authorization, string, error) {
	var runPublicID string
	var authorizationID sql.NullInt64
	var publicID sql.NullString
	var slot sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT ar.public_id, ar.business_budget_authorization_id, a.public_id, a.slot_no
FROM agent_runs ar
LEFT JOIN incident_cycle_budget_authorizations a
  ON a.id = ar.business_budget_authorization_id
 AND a.incident_id = ar.incident_id AND a.cycle_no = ar.cycle_no
 AND a.domain_schema_version = 3 AND a.budget_kind = 'agent_run'
WHERE ar.id = ? AND ar.incident_id = ? AND ar.cycle_no = ? AND ar.domain_schema_version = 3
FOR UPDATE`, runID, incidentID, cycleNo).Scan(&runPublicID, &authorizationID, &publicID, &slot)
	if errors.Is(err, sql.ErrNoRows) {
		return Authorization{}, "", fmt.Errorf("%w: originating AgentRun does not belong to the Incident cycle", ErrInvalidAuthorization)
	}
	if err != nil {
		return Authorization{}, "", fmt.Errorf("load originating AgentRun authorization: %w", err)
	}
	if !authorizationID.Valid {
		return Authorization{}, runPublicID, nil
	}
	if !publicID.Valid || !slot.Valid || slot.Int64 < DefaultLimit+1 || slot.Int64 > HardLimit {
		return Authorization{}, "", ErrInvalidAuthorization
	}
	return Authorization{ID: uint64(authorizationID.Int64), PublicID: publicID.String, Slot: int(slot.Int64)}, runPublicID, nil
}

func childAuthorizationUsed(ctx context.Context, tx DBTX, kind Kind, authorizationID uint64) (bool, error) {
	table, err := tableForKind(kind)
	if err != nil {
		return false, err
	}
	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE business_budget_authorization_id = ?"
	if err := tx.QueryRowContext(ctx, query, authorizationID).Scan(&count); err != nil {
		return false, fmt.Errorf("check %s budget authorization use: %w", kind, err)
	}
	return count != 0, nil
}

func hashCanonical(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(strings.TrimSpace(part)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
