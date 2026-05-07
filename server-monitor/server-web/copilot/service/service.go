package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"server-web/copilot/session"
)

const (
	DefaultMaxMessageLength = 2000
	IntentUnknown           = "unknown"
)

var (
	ErrMessageRequired  = errors.New("message is required")
	ErrMessageTooLong   = errors.New("message is too long")
	ErrSessionRequired  = errors.New("session_id is required")
	ErrSessionNotFound  = errors.New("copilot session not found")
	ErrSessionForbidden = errors.New("copilot session belongs to another user")
)

type Config struct {
	MaxMessageLength   int
	SessionTTL         time.Duration
	MaxSessionMessages int
	Store              session.Store
}

type User struct {
	ID       uint64
	Username string
	Role     string
}

type Service struct {
	maxMessageLength   int
	sessionTTL         time.Duration
	maxSessionMessages int
	store              session.Store
}

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
}

type ChatResponse struct {
	SessionID   string     `json:"session_id"`
	Reply       string     `json:"reply"`
	Intent      string     `json:"intent"`
	Confidence  float64    `json:"confidence"`
	ToolCalls   []ToolCall `json:"tool_calls"`
	Suggestions []string   `json:"suggestions"`
}

type ToolCall struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type SessionSummary = session.Summary
type Message = session.Message

func NewService(cfg Config) *Service {
	maxMessageLength := cfg.MaxMessageLength
	if maxMessageLength <= 0 {
		maxMessageLength = DefaultMaxMessageLength
	}
	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = session.DefaultTTL
	}
	maxSessionMessages := cfg.MaxSessionMessages
	if maxSessionMessages <= 0 {
		maxSessionMessages = session.DefaultMaxMessages
	}
	return &Service{
		maxMessageLength:   maxMessageLength,
		sessionTTL:         sessionTTL,
		maxSessionMessages: maxSessionMessages,
		store:              cfg.Store,
	}
}

func (s *Service) Chat(ctx context.Context, user User, req ChatRequest) (ChatResponse, error) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return ChatResponse{}, ErrMessageRequired
	}
	if len([]rune(message)) > s.maxMessageLength {
		return ChatResponse{}, fmt.Errorf("%w: max %d characters", ErrMessageTooLong, s.maxMessageLength)
	}
	if s.store == nil {
		return ChatResponse{}, session.ErrUnavailable
	}

	sessionID := strings.TrimSpace(req.SessionID)
	now := time.Now().UTC()
	meta := session.Meta{
		UserID:    user.ID,
		Title:     buildTitle(message),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if sessionID == "" {
		generated, err := generateSessionID()
		if err != nil {
			return ChatResponse{}, fmt.Errorf("generate session id: %w", err)
		}
		sessionID = generated
	} else {
		existing, err := s.requireOwnedSession(ctx, user, sessionID)
		if err != nil {
			return ChatResponse{}, err
		}
		meta.Title = existing.Title
		meta.CreatedAt = existing.CreatedAt
	}
	meta.ID = sessionID
	reply := "Copilot chat API is ready. Session storage is enabled. Intent parsing and read-only tools will be enabled in the next Phase 1 modules."

	if err := s.store.AppendMessages(ctx, meta, []session.Message{
		{
			Role:      "user",
			Content:   message,
			CreatedAt: now.Format(time.RFC3339),
		},
		{
			Role:      "assistant",
			Content:   reply,
			CreatedAt: now.Format(time.RFC3339),
		},
	}, s.sessionTTL, s.maxSessionMessages); err != nil {
		return ChatResponse{}, err
	}

	return ChatResponse{
		SessionID:  sessionID,
		Reply:      reply,
		Intent:     IntentUnknown,
		Confidence: 0,
		ToolCalls:  []ToolCall{},
		Suggestions: []string{
			"Try asking for active alerts after the read-only tools module is enabled.",
			"Try asking for host status after the read-only tools module is enabled.",
		},
	}, nil
}

func (s *Service) ListSessions(ctx context.Context, user User) ([]SessionSummary, error) {
	if s.store == nil {
		return nil, session.ErrUnavailable
	}
	return s.store.ListSessions(ctx, user.ID)
}

func (s *Service) ListMessages(ctx context.Context, user User, sessionID string) ([]Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrSessionRequired
	}
	if _, err := s.requireOwnedSession(ctx, user, sessionID); err != nil {
		return nil, err
	}
	return s.store.ListMessages(ctx, sessionID)
}

func (s *Service) DeleteSession(ctx context.Context, user User, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrSessionRequired
	}
	if _, err := s.requireOwnedSession(ctx, user, sessionID); err != nil {
		return err
	}
	return s.store.DeleteSession(ctx, user.ID, sessionID)
}

func (s *Service) requireOwnedSession(ctx context.Context, user User, sessionID string) (session.Meta, error) {
	if s.store == nil {
		return session.Meta{}, session.ErrUnavailable
	}
	meta, ok, err := s.store.GetMeta(ctx, sessionID)
	if err != nil {
		return session.Meta{}, err
	}
	if !ok {
		return session.Meta{}, ErrSessionNotFound
	}
	if meta.UserID != user.ID {
		return session.Meta{}, ErrSessionForbidden
	}
	return meta, nil
}

func generateSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(raw[:]), nil
}

func buildTitle(message string) string {
	const maxTitleRunes = 40
	runes := []rune(message)
	if len(runes) <= maxTitleRunes {
		return message
	}
	return string(runes[:maxTitleRunes])
}
