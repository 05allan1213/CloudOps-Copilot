package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultMaxMessageLength = 2000
	IntentUnknown           = "unknown"
)

var (
	ErrMessageRequired = errors.New("message is required")
	ErrMessageTooLong  = errors.New("message is too long")
	ErrSessionRequired = errors.New("session_id is required")
)

type Config struct {
	MaxMessageLength int
}

type User struct {
	ID       uint64
	Username string
	Role     string
}

type Service struct {
	maxMessageLength int
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

type SessionSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}

type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func NewService(cfg Config) *Service {
	maxMessageLength := cfg.MaxMessageLength
	if maxMessageLength <= 0 {
		maxMessageLength = DefaultMaxMessageLength
	}
	return &Service{maxMessageLength: maxMessageLength}
}

func (s *Service) Chat(ctx context.Context, user User, req ChatRequest) (ChatResponse, error) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return ChatResponse{}, ErrMessageRequired
	}
	if len(message) > s.maxMessageLength {
		return ChatResponse{}, fmt.Errorf("%w: max %d characters", ErrMessageTooLong, s.maxMessageLength)
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		generated, err := generateSessionID()
		if err != nil {
			return ChatResponse{}, fmt.Errorf("generate session id: %w", err)
		}
		sessionID = generated
	}

	return ChatResponse{
		SessionID:  sessionID,
		Reply:      "Copilot chat API is ready. Session storage, intent parsing, and read-only tools will be enabled in the next Phase 1 modules.",
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
	return []SessionSummary{}, nil
}

func (s *Service) ListMessages(ctx context.Context, user User, sessionID string) ([]Message, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrSessionRequired
	}
	return []Message{}, nil
}

func (s *Service) DeleteSession(ctx context.Context, user User, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrSessionRequired
	}
	return nil
}

func generateSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(raw[:]), nil
}
