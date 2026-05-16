package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

const (
	KeyPrefix            = "chat:session"
	UserSessionKeyPrefix = "chat:user"
	DefaultTTL           = 2 * time.Hour
	DefaultMaxMessages   = 50
)

var ErrUnavailable = errors.New("Copilot 会话存储不可用")

type RedisClient interface {
	Enabled() bool
	HSet(ctx context.Context, key, field string, value []byte) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	RPush(ctx context.Context, key string, values ...[]byte) error
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LTrim(ctx context.Context, key string, start, stop int64) error
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	SAdd(ctx context.Context, key string, members ...string) error
	SMembers(ctx context.Context, key string) ([]string, error)
	SRem(ctx context.Context, key string, members ...string) error
}

type Store interface {
	GetMeta(ctx context.Context, sessionID string) (Meta, bool, error)
	AppendMessages(ctx context.Context, meta Meta, messages []Message, ttl time.Duration, maxMessages int) error
	ListSessions(ctx context.Context, userID uint64) ([]Summary, error)
	ListMessages(ctx context.Context, sessionID string) ([]Message, error)
	DeleteSession(ctx context.Context, userID uint64, sessionID string) error
	GetContext(ctx context.Context, sessionID string) (SessionContext, error)
	SetContext(ctx context.Context, sessionID string, ctxData SessionContext, ttl time.Duration) error
}

type Meta struct {
	ID        string
	UserID    uint64
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Summary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}

type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type SessionContext struct {
	LastIntent   string            `json:"last_intent"`
	LastEntities map[string]string `json:"last_entities"`
}

type RedisStore struct {
	client RedisClient
}

func NewRedisStore(client RedisClient) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) GetMeta(ctx context.Context, sessionID string) (Meta, bool, error) {
	if !s.enabled() {
		return Meta{}, false, ErrUnavailable
	}

	fields, err := s.client.HGetAll(ctx, metaKey(sessionID))
	if err != nil {
		return Meta{}, false, fmt.Errorf("get copilot session meta: %w", err)
	}
	if len(fields) == 0 {
		return Meta{}, false, nil
	}

	meta, err := parseMeta(fields)
	if err != nil {
		return Meta{}, false, err
	}
	return meta, true, nil
}

func (s *RedisStore) AppendMessages(ctx context.Context, meta Meta, messages []Message, ttl time.Duration, maxMessages int) error {
	if !s.enabled() {
		return ErrUnavailable
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if maxMessages <= 0 {
		maxMessages = DefaultMaxMessages
	}

	values := make([][]byte, 0, len(messages))
	for _, message := range messages {
		value, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("marshal copilot session message: %w", err)
		}
		values = append(values, value)
	}

	if err := s.client.RPush(ctx, messagesKey(meta.ID), values...); err != nil {
		return fmt.Errorf("append copilot session messages: %w", err)
	}
	if err := s.client.LTrim(ctx, messagesKey(meta.ID), int64(-maxMessages), -1); err != nil {
		return fmt.Errorf("trim copilot session messages: %w", err)
	}
	if err := s.writeMeta(ctx, meta); err != nil {
		return err
	}
	if err := s.client.SAdd(ctx, userSessionsKey(meta.UserID), meta.ID); err != nil {
		return fmt.Errorf("index copilot session: %w", err)
	}
	return s.refreshTTL(ctx, meta.UserID, meta.ID, ttl)
}

func (s *RedisStore) ListSessions(ctx context.Context, userID uint64) ([]Summary, error) {
	if !s.enabled() {
		return nil, ErrUnavailable
	}

	ids, err := s.client.SMembers(ctx, userSessionsKey(userID))
	if err != nil {
		return nil, fmt.Errorf("list copilot session ids: %w", err)
	}

	summaries := make([]Summary, 0, len(ids))
	for _, id := range ids {
		meta, ok, err := s.GetMeta(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			_ = s.client.SRem(ctx, userSessionsKey(userID), id)
			continue
		}
		if meta.UserID != userID {
			continue
		}
		summaries = append(summaries, Summary{
			ID:        meta.ID,
			Title:     meta.Title,
			UpdatedAt: meta.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt > summaries[j].UpdatedAt
	})
	return summaries, nil
}

func (s *RedisStore) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	if !s.enabled() {
		return nil, ErrUnavailable
	}

	values, err := s.client.LRange(ctx, messagesKey(sessionID), 0, -1)
	if err != nil {
		return nil, fmt.Errorf("list copilot session messages: %w", err)
	}

	messages := make([]Message, 0, len(values))
	for _, value := range values {
		var message Message
		if err := json.Unmarshal([]byte(value), &message); err != nil {
			return nil, fmt.Errorf("unmarshal copilot session message: %w", err)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *RedisStore) DeleteSession(ctx context.Context, userID uint64, sessionID string) error {
	if !s.enabled() {
		return ErrUnavailable
	}

	if err := s.client.Del(ctx, messagesKey(sessionID), metaKey(sessionID), contextKey(sessionID)); err != nil {
		return fmt.Errorf("delete copilot session: %w", err)
	}
	if err := s.client.SRem(ctx, userSessionsKey(userID), sessionID); err != nil {
		return fmt.Errorf("remove copilot session index: %w", err)
	}
	return nil
}

func (s *RedisStore) GetContext(ctx context.Context, sessionID string) (SessionContext, error) {
	if !s.enabled() {
		return SessionContext{}, ErrUnavailable
	}

	fields, err := s.client.HGetAll(ctx, contextKey(sessionID))
	if err != nil {
		return SessionContext{}, fmt.Errorf("get copilot session context: %w", err)
	}
	if len(fields) == 0 {
		return SessionContext{}, nil
	}

	result := SessionContext{LastIntent: fields["last_intent"]}
	if raw := fields["last_entities"]; raw != "" {
		var entities map[string]string
		if err := json.Unmarshal([]byte(raw), &entities); err == nil {
			result.LastEntities = entities
		}
	}
	return result, nil
}

func (s *RedisStore) SetContext(ctx context.Context, sessionID string, ctxData SessionContext, ttl time.Duration) error {
	if !s.enabled() {
		return ErrUnavailable
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	entitiesJSON, err := json.Marshal(ctxData.LastEntities)
	if err != nil {
		return fmt.Errorf("marshal copilot session context entities: %w", err)
	}
	fields := map[string][]byte{
		"last_intent":   []byte(ctxData.LastIntent),
		"last_entities": entitiesJSON,
	}
	for field, value := range fields {
		if err := s.client.HSet(ctx, contextKey(sessionID), field, value); err != nil {
			return fmt.Errorf("set copilot session context: %w", err)
		}
	}
	if err := s.client.Expire(ctx, contextKey(sessionID), ttl); err != nil {
		return fmt.Errorf("refresh copilot session context ttl: %w", err)
	}
	return nil
}

func (s *RedisStore) enabled() bool {
	return s != nil && s.client != nil && s.client.Enabled()
}

func (s *RedisStore) writeMeta(ctx context.Context, meta Meta) error {
	fields := map[string]string{
		"id":         meta.ID,
		"user_id":    strconv.FormatUint(meta.UserID, 10),
		"title":      meta.Title,
		"created_at": meta.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": meta.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for field, value := range fields {
		if err := s.client.HSet(ctx, metaKey(meta.ID), field, []byte(value)); err != nil {
			return fmt.Errorf("write copilot session meta: %w", err)
		}
	}
	return nil
}

func (s *RedisStore) refreshTTL(ctx context.Context, userID uint64, sessionID string, ttl time.Duration) error {
	for _, key := range []string{messagesKey(sessionID), metaKey(sessionID), contextKey(sessionID), userSessionsKey(userID)} {
		if err := s.client.Expire(ctx, key, ttl); err != nil {
			return fmt.Errorf("refresh copilot session ttl: %w", err)
		}
	}
	return nil
}

func parseMeta(fields map[string]string) (Meta, error) {
	userID, err := strconv.ParseUint(fields["user_id"], 10, 64)
	if err != nil {
		return Meta{}, fmt.Errorf("parse copilot session user_id: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339, fields["created_at"])
	if err != nil {
		return Meta{}, fmt.Errorf("parse copilot session created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, fields["updated_at"])
	if err != nil {
		return Meta{}, fmt.Errorf("parse copilot session updated_at: %w", err)
	}
	return Meta{
		ID:        fields["id"],
		UserID:    userID,
		Title:     fields["title"],
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func messagesKey(sessionID string) string {
	return KeyPrefix + ":" + sessionID
}

func metaKey(sessionID string) string {
	return KeyPrefix + ":" + sessionID + ":meta"
}

func contextKey(sessionID string) string {
	return KeyPrefix + ":" + sessionID + ":ctx"
}

func userSessionsKey(userID uint64) string {
	return UserSessionKeyPrefix + ":" + strconv.FormatUint(userID, 10) + ":sessions"
}
