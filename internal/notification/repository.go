// Package notification owns durable Owner Notifications and their read state.
package notification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalid  = errors.New("invalid Owner Notification")
	ErrNotFound = errors.New("Owner Notification not found")
)

type ContextLink struct {
	Workspace          string            `json:"workspace"`
	Path               string            `json:"path"`
	Query              map[string]string `json:"query"`
	OperationalScopeID string            `json:"operational_scope_id"`
	External           bool              `json:"external"`
}

type Item struct {
	ID          string      `json:"id"`
	SourceType  string      `json:"source_type"`
	SourceID    string      `json:"source_id"`
	SourceState string      `json:"source_state"`
	Severity    string      `json:"severity"`
	Reason      string      `json:"reason"`
	ContextLink ContextLink `json:"context_link"`
	Read        bool        `json:"read"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Page struct {
	Items       []Item `json:"items"`
	NextCursor  string `json:"next_cursor,omitempty"`
	UnreadCount int    `json:"unread_count"`
}

type NewItem struct {
	SourceType  string
	SourceID    string
	SourceState string
	Severity    string
	Reason      string
	ContextLink ContextLink
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("notification database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) List(ctx context.Context, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	afterID, err := r.cursorID(ctx, cursor)
	if err != nil {
		return Page{}, err
	}
	query := `SELECT id, public_id, source_type, source_public_id, source_state, severity,
reason, context_workspace, context_path, context_query_json,
operational_scope_public_id, read_at, created_at
FROM owner_notifications`
	args := make([]any, 0, 2)
	if afterID > 0 {
		query += ` WHERE id < ?`
		args = append(args, afterID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("list Owner Notifications: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0, limit+1)
	ids := make([]uint64, 0, limit+1)
	for rows.Next() {
		item, id, err := scanItem(rows)
		if err != nil {
			return Page{}, err
		}
		items = append(items, item)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.NextCursor = encodeCursor(items[limit-1].ID)
		page.Items = items[:limit]
		ids = ids[:limit]
	}
	if page.Items == nil {
		page.Items = []Item{}
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM owner_notifications WHERE read_at IS NULL`).Scan(&page.UnreadCount); err != nil {
		return Page{}, fmt.Errorf("count unread Owner Notifications: %w", err)
	}
	return page, nil
}

func (r *Repository) Events(ctx context.Context, lastEventID string, limit int) ([]Item, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	afterID, err := r.publicIDToInternal(ctx, strings.TrimSpace(lastEventID))
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, public_id, source_type, source_public_id,
source_state, severity, reason, context_workspace, context_path, context_query_json,
operational_scope_public_id, read_at, created_at
FROM owner_notifications WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list Owner Notification events: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		item, _, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) MarkRead(ctx context.Context, publicID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE owner_notifications
SET read_at = COALESCE(read_at, NOW(6)), updated_at = NOW(6) WHERE public_id = ?`, strings.TrimSpace(publicID))
	if err != nil {
		return fmt.Errorf("mark Owner Notification read: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) MarkAllRead(ctx context.Context, cursor string) (int64, error) {
	throughID, err := r.cursorID(ctx, cursor)
	if err != nil {
		return 0, err
	}
	query := `UPDATE owner_notifications SET read_at = NOW(6), updated_at = NOW(6) WHERE read_at IS NULL`
	args := []any{}
	if throughID > 0 {
		query += ` AND id <= ?`
		args = append(args, throughID)
	}
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mark Owner Notifications read: %w", err)
	}
	return result.RowsAffected()
}

func (r *Repository) Create(ctx context.Context, input NewItem) (Item, error) {
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.SourceState = strings.TrimSpace(input.SourceState)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.SourceType == "" || len(input.SourceType) > 64 || len(input.SourceState) > 64 ||
		len(input.Reason) < 1 || len(input.Reason) > 2048 ||
		(input.Severity != "P1" && input.Severity != "P2" && input.Severity != "P3" && input.Severity != "info") ||
		validateContextLink(input.ContextLink) != nil {
		return Item{}, ErrInvalid
	}
	if _, err := uuid.Parse(input.SourceID); err != nil {
		return Item{}, ErrInvalid
	}
	queryJSON, _ := json.Marshal(input.ContextLink.Query)
	dedupe := sha256.Sum256([]byte(strings.Join([]string{input.SourceType, input.SourceID, input.SourceState}, "\x00")))
	publicID := uuid.NewString()
	_, err := r.db.ExecContext(ctx, `INSERT INTO owner_notifications (
public_id, source_type, source_public_id, source_state, severity, reason,
context_workspace, context_path, context_query_json, operational_scope_public_id,
dedupe_identity, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE severity = VALUES(severity), reason = VALUES(reason),
context_workspace = VALUES(context_workspace), context_path = VALUES(context_path),
context_query_json = VALUES(context_query_json), operational_scope_public_id = VALUES(operational_scope_public_id),
updated_at = NOW(6)`,
		publicID, input.SourceType, input.SourceID, input.SourceState, input.Severity, input.Reason,
		input.ContextLink.Workspace, input.ContextLink.Path, queryJSON, input.ContextLink.OperationalScopeID,
		hex.EncodeToString(dedupe[:]),
	)
	if err != nil {
		return Item{}, fmt.Errorf("create Owner Notification: %w", err)
	}
	var item Item
	row := r.db.QueryRowContext(ctx, `SELECT id, public_id, source_type, source_public_id,
source_state, severity, reason, context_workspace, context_path, context_query_json,
operational_scope_public_id, read_at, created_at
FROM owner_notifications WHERE dedupe_identity = ?`, hex.EncodeToString(dedupe[:]))
	item, _, err = scanItem(row)
	return item, err
}

type scanner interface{ Scan(...any) error }

func scanItem(row scanner) (Item, uint64, error) {
	var item Item
	var id uint64
	var queryJSON []byte
	var readAt sql.NullTime
	if err := row.Scan(&id, &item.ID, &item.SourceType, &item.SourceID, &item.SourceState,
		&item.Severity, &item.Reason, &item.ContextLink.Workspace, &item.ContextLink.Path,
		&queryJSON, &item.ContextLink.OperationalScopeID, &readAt, &item.CreatedAt); err != nil {
		return Item{}, 0, fmt.Errorf("scan Owner Notification: %w", err)
	}
	if err := json.Unmarshal(queryJSON, &item.ContextLink.Query); err != nil {
		return Item{}, 0, fmt.Errorf("decode Owner Notification Context Link: %w", err)
	}
	if item.ContextLink.Query == nil {
		item.ContextLink.Query = map[string]string{}
	}
	item.ContextLink.External = false
	item.Read = readAt.Valid
	item.CreatedAt = item.CreatedAt.UTC()
	return item, id, nil
}

func validateContextLink(link ContextLink) error {
	allowed := map[string]string{
		"overview": "/overview", "infrastructure": "/infrastructure", "monitoring": "/monitoring",
		"alerts": "/alerts", "logs": "/logs", "traces": "/traces", "agent": "/agent",
		"incidents": "/incidents", "devops": "/devops", "settings": "/settings",
	}
	prefix, ok := allowed[link.Workspace]
	if !ok || (link.Path != prefix && !strings.HasPrefix(link.Path, prefix+"/")) || link.External {
		return ErrInvalid
	}
	if _, err := uuid.Parse(link.OperationalScopeID); err != nil {
		return ErrInvalid
	}
	values := url.Values{}
	for key, value := range link.Query {
		if len(key) > 64 || len(value) > 512 || strings.ContainsAny(key, "\x00\r\n") || strings.ContainsAny(value, "\x00\r\n") {
			return ErrInvalid
		}
		values.Set(key, value)
	}
	if len(values.Encode()) > 4096 {
		return ErrInvalid
	}
	return nil
}

func (r *Repository) cursorID(ctx context.Context, cursor string) (uint64, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) != 36 {
		return 0, ErrInvalid
	}
	return r.publicIDToInternal(ctx, string(decoded))
}

func (r *Repository) publicIDToInternal(ctx context.Context, publicID string) (uint64, error) {
	if publicID == "" {
		return 0, nil
	}
	if _, err := uuid.Parse(publicID); err != nil {
		return 0, ErrInvalid
	}
	var id uint64
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM owner_notifications WHERE public_id = ?`, publicID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalid
		}
		return 0, err
	}
	return id, nil
}

func encodeCursor(publicID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(publicID))
}
