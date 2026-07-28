// Package command contains the small transactional command-idempotency
// contract shared by the V1 API and domain workflows.
package command

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

var (
	ErrPayloadConflict = errors.New("idempotency key was used with a different payload")
	ErrInProgress      = errors.New("idempotent command is already processing")
)

const (
	statusProcessing = "processing"
	statusCompleted  = "completed"
	maxResponseSize  = 8 * 1024
	maxTxRetries     = 3
)

// Request is the canonical identity of one authenticated command.
type Request struct {
	ActorIdentityHash string
	CommandScope      string
	IdempotencyKey    string
	RequestHash       string
}

// Response is persisted verbatim and returned for duplicate commands.
type Response struct {
	HTTPStatus       int
	Body             json.RawMessage
	ResourceType     string
	ResourcePublicID string
}

// TxFunc performs the domain transition and must only use the supplied
// transaction. It is rolled back on error, so a processing row cannot become
// a durable half-completed command.
type TxFunc func(context.Context, *sql.Tx) (Response, error)

// Store persists command identities in MySQL.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("command idempotency database is required")
	}
	return &Store{db: db}, nil
}

// Execute runs a command and atomically records its response. The bool return
// is true when the response came from an already completed duplicate.
func (s *Store) Execute(ctx context.Context, request Request, fn TxFunc) (Response, bool, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, false, err
	}
	if fn == nil {
		return Response{}, false, errors.New("command transaction callback is required")
	}
	var lastErr error
	for retry := 0; retry <= maxTxRetries; retry++ {
		response, duplicate, err := s.executeOnce(ctx, request, fn)
		if err == nil {
			return response, duplicate, nil
		}
		lastErr = err
		if !retryableTransactionError(err) || retry == maxTxRetries {
			return Response{}, false, err
		}
		if err := waitTransactionRetry(ctx, retry); err != nil {
			return Response{}, false, err
		}
	}
	return Response{}, false, lastErr
}

func waitTransactionRetry(ctx context.Context, retry int) error {
	delay := 10 * time.Millisecond * time.Duration(1<<retry)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Store) executeOnce(ctx context.Context, request Request, fn TxFunc) (Response, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Response{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `
INSERT INTO command_idempotency_records
    (public_id, actor_identity_hash, command_scope, idempotency_key, request_hash,
     status, http_status, response_json, resource_type, resource_public_id,
     created_at, expires_at)
VALUES (?, ?, ?, ?, ?, 'processing', NULL, NULL, '', '', NOW(6), TIMESTAMPADD(HOUR, 24, NOW(6)))
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		publicID, request.ActorIdentityHash, request.CommandScope, request.IdempotencyKey, request.RequestHash)
	if err != nil {
		return Response{}, false, err
	}
	if _, err := result.LastInsertId(); err != nil {
		return Response{}, false, err
	}

	var stored struct {
		PublicID         string
		RequestHash      string
		Status           string
		HTTPStatus       sql.NullInt64
		Body             []byte
		ResourceType     string
		ResourcePublicID string
	}
	if err := tx.QueryRowContext(ctx, `
	SELECT public_id, request_hash, status, http_status, response_json, resource_type, resource_public_id
FROM command_idempotency_records
WHERE actor_identity_hash = ? AND command_scope = ? AND idempotency_key = ?
FOR UPDATE`, request.ActorIdentityHash, request.CommandScope, request.IdempotencyKey).
		Scan(&stored.PublicID, &stored.RequestHash, &stored.Status, &stored.HTTPStatus, &stored.Body, &stored.ResourceType, &stored.ResourcePublicID); err != nil {
		return Response{}, false, err
	}
	if stored.RequestHash != request.RequestHash {
		return Response{}, false, ErrPayloadConflict
	}
	if stored.PublicID != publicID {
		if stored.Status != statusCompleted {
			return Response{}, false, ErrInProgress
		}
		if !stored.HTTPStatus.Valid || !json.Valid(stored.Body) {
			return Response{}, false, errors.New("completed command has an invalid stored response")
		}
		body, err := compactResponseJSON(stored.Body)
		if err != nil {
			return Response{}, false, err
		}
		return Response{HTTPStatus: int(stored.HTTPStatus.Int64), Body: body, ResourceType: stored.ResourceType, ResourcePublicID: stored.ResourcePublicID}, true, tx.Commit()
	}

	response, err := fn(ctx, tx)
	if err != nil {
		return Response{}, false, err
	}
	if err := validateResponse(response); err != nil {
		return Response{}, false, err
	}
	response.Body, err = compactResponseJSON(response.Body)
	if err != nil {
		return Response{}, false, err
	}
	completed, err := tx.ExecContext(ctx, `
UPDATE command_idempotency_records
SET status = 'completed', http_status = ?, response_json = ?, resource_type = ?,
    resource_public_id = ?, completed_at = NOW(6), expires_at = TIMESTAMPADD(HOUR, 24, NOW(6))
WHERE actor_identity_hash = ? AND command_scope = ? AND idempotency_key = ?
  AND request_hash = ? AND status = 'processing'`,
		response.HTTPStatus, []byte(response.Body), response.ResourceType, response.ResourcePublicID,
		request.ActorIdentityHash, request.CommandScope, request.IdempotencyKey, request.RequestHash)
	if err != nil {
		return Response{}, false, err
	}
	if affected, err := completed.RowsAffected(); err != nil || affected != 1 {
		return Response{}, false, errors.New("command completion lost its processing record")
	}
	if err := tx.Commit(); err != nil {
		return Response{}, false, err
	}
	return response, false, nil
}

func retryableTransactionError(err error) bool {
	var mysqlError *drivermysql.MySQLError
	if !errors.As(err, &mysqlError) {
		return false
	}
	return mysqlError.Number == 1213 || mysqlError.Number == 1205
}

func compactResponseJSON(body []byte) (json.RawMessage, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		return nil, fmt.Errorf("compact command response: %w", err)
	}
	return append(json.RawMessage(nil), compact.Bytes()...), nil
}

func validateRequest(request Request) error {
	for name, value := range map[string]string{
		"actor identity hash": request.ActorIdentityHash,
		"request hash":        request.RequestHash,
	} {
		if len(value) != 64 {
			return fmt.Errorf("%s must be a 64-character hex SHA-256", name)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("%s must be hex: %w", name, err)
		}
	}
	if strings.TrimSpace(request.CommandScope) == "" || len(request.CommandScope) > 128 {
		return errors.New("command scope must be 1..128 bytes")
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 128 {
		return errors.New("idempotency key must be 1..128 bytes")
	}
	return nil
}

func validateResponse(response Response) error {
	if response.HTTPStatus < 200 || response.HTTPStatus > 599 {
		return fmt.Errorf("response HTTP status %d is invalid", response.HTTPStatus)
	}
	if len(response.Body) == 0 || len(response.Body) > maxResponseSize || !json.Valid(response.Body) {
		return errors.New("command response must be valid bounded JSON")
	}
	if len(response.ResourceType) > 128 || len(response.ResourcePublicID) > 128 {
		return errors.New("command resource reference is too long")
	}
	return nil
}
