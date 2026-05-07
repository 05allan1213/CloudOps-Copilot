package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"server-web/copilot/nlu"
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
	Classifier         *nlu.Classifier
	LLM                LLMClassifier
	Tools              ToolExecutor
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
	classifier         *nlu.Classifier
	llm                LLMClassifier
	tools              ToolExecutor
}

type ToolExecutor interface {
	Execute(ctx context.Context, result nlu.Result) ([]ToolCall, string, error)
}

type LLMClassifier interface {
	Classify(ctx context.Context, message string) (nlu.Result, error)
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
	Name   string      `json:"name"`
	Status string      `json:"status"`
	Error  string      `json:"error,omitempty"`
	Result interface{} `json:"result,omitempty"`
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
		classifier:         defaultClassifier(cfg.Classifier),
		llm:                cfg.LLM,
		tools:              cfg.Tools,
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
	parsed := s.classifier.Classify(message)
	parsed = s.classifyWithFallback(ctx, message, parsed)
	toolCalls, toolReply, err := s.executeTools(ctx, parsed)
	if err != nil {
		return ChatResponse{}, err
	}
	reply := buildReply(parsed, toolReply)

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
		SessionID:   sessionID,
		Reply:       reply,
		Intent:      parsed.Intent,
		Confidence:  parsed.Confidence,
		ToolCalls:   toolCalls,
		Suggestions: buildSuggestions(parsed),
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

func defaultClassifier(classifier *nlu.Classifier) *nlu.Classifier {
	if classifier != nil {
		return classifier
	}
	return nlu.NewClassifier()
}

func (s *Service) executeTools(ctx context.Context, result nlu.Result) ([]ToolCall, string, error) {
	if s.tools == nil || result.Intent == nlu.IntentUnknown || result.Intent == nlu.IntentGeneralChat {
		return []ToolCall{}, "", nil
	}
	return s.tools.Execute(ctx, result)
}

func (s *Service) classifyWithFallback(ctx context.Context, message string, parsed nlu.Result) nlu.Result {
	if s.llm == nil || parsed.Confidence >= 0.6 {
		return parsed
	}
	llmResult, err := s.llm.Classify(ctx, message)
	if err != nil {
		return parsed
	}
	if llmResult.Intent == "" {
		return parsed
	}
	if llmResult.Entities == nil {
		llmResult.Entities = map[string]string{}
	}
	return llmResult
}

func buildReply(result nlu.Result, toolReply string) string {
	if toolReply != "" {
		return toolReply
	}
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return "I recognized this as an active alert query. Read-only alert tools will return live data in the next module."
	case nlu.IntentAlertEventQuery:
		return "I recognized this as an alert event query. Read-only alert event tools will return live data in the next module."
	case nlu.IntentAlertHistoryQuery:
		return "I recognized this as an alert history query. Read-only history tools will return live data in the next module."
	case nlu.IntentHostQuery:
		return "I recognized this as a host query. Read-only host tools will return live data in the next module."
	case nlu.IntentMetricQuery:
		return "I recognized this as a metric query. Read-only metric tools will return live data in the next module."
	case nlu.IntentGeneralChat:
		return "I can help query hosts, metrics, active alerts, alert events, and alert history through read-only tools as Phase 1 comes online."
	default:
		return "I could not confidently identify the operation yet. Please clarify whether you want to query hosts, metrics, active alerts, alert events, or alert history."
	}
}

func buildSuggestions(result nlu.Result) []string {
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return []string{"Show current active alerts", "List critical firing alerts"}
	case nlu.IntentAlertEventQuery:
		return []string{"Show latest alert events", "Show recent resolved alerts"}
	case nlu.IntentAlertHistoryQuery:
		return []string{"Show CPU alert history for the last week", "Show warning alert history"}
	case nlu.IntentHostQuery:
		return []string{"List current hosts", "Show offline hosts"}
	case nlu.IntentMetricQuery:
		return []string{"Show node-1 CPU for 1h", "Show memory trend for 24h"}
	case nlu.IntentGeneralChat:
		return []string{"What alerts are firing?", "Which hosts are offline?", "Show CPU trend for node-1"}
	default:
		return []string{"What alerts are firing?", "Which hosts are offline?", "Show CPU trend for node-1"}
	}
}
