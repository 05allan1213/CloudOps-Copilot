package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"server-web/internal/copilot/diagnosis"
	"server-web/internal/copilot/llm"
	"server-web/internal/copilot/nlu"
	"server-web/internal/copilot/session"
)

const (
	DefaultMaxMessageLength = 2000
	IntentUnknown           = "unknown"
)

var (
	ErrMessageRequired  = errors.New("消息不能为空")
	ErrMessageTooLong   = errors.New("消息过长")
	ErrSessionRequired  = errors.New("session_id 不能为空")
	ErrSessionNotFound  = errors.New("Copilot 会话未找到")
	ErrSessionForbidden = errors.New("Copilot 会话属于其他用户")
)

type Config struct {
	MaxMessageLength     int
	SessionTTL           time.Duration
	MaxSessionMessages   int
	Store                session.Store
	Classifier           *nlu.Classifier
	LLM                  LLMClassifier
	Tools                ToolExecutor
	Diagnosis            DiagnosisService
	Summarizer           Summarizer
	SummaryEnabled       bool
	ContextManager       ContextManager
	ToolDefs             []llm.ToolDefinition
	ToolsClassifyEnabled bool
	MultiIntentEnabled   bool
	MultiIntentMax       int
}

type User struct {
	ID       uint64
	Username string
	Role     string
}

type userContextKey struct{}

func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey{}).(User)
	return user, ok
}

type Service struct {
	maxMessageLength     int
	sessionTTL           time.Duration
	maxSessionMessages   int
	store                session.Store
	classifier           *nlu.Classifier
	llm                  LLMClassifier
	tools                ToolExecutor
	diagnosis            DiagnosisService
	summarizer           Summarizer
	summaryEnabled       bool
	contextManager       ContextManager
	toolDefs             []llm.ToolDefinition
	toolsClassifyEnabled bool
	multiIntentEnabled   bool
	multiIntentMax       int
}

type ToolExecutor interface {
	Execute(ctx context.Context, result nlu.Result) ([]ToolCall, string, error)
}

type ToolSchemaLister interface {
	ToolSchemas() []ToolSchema
}

type LLMClassifier interface {
	Classify(ctx context.Context, message string) (nlu.Result, error)
}

type LLMClassifierWithTools interface {
	LLMClassifier
	ClassifyWithTools(ctx context.Context, message string, tools []llm.ToolDefinition) (nlu.Result, error)
	ClassifyWithToolsMulti(ctx context.Context, message string, tools []llm.ToolDefinition) (nlu.Result, error)
}

type DiagnosisService interface {
	Trigger(ctx context.Context, user diagnosis.User, req diagnosis.Request) (diagnosis.ReportResponse, error)
}

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
}

type ChatResponse struct {
	SessionID    string         `json:"session_id"`
	Reply        string         `json:"reply"`
	Intent       string         `json:"intent"`
	Confidence   float64        `json:"confidence"`
	ToolCalls    []ToolCall     `json:"tool_calls"`
	Suggestions  []string       `json:"suggestions"`
	MultiIntents []IntentResult `json:"multi_intents,omitempty"`
}

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
	toolDefs := cfg.ToolDefs
	toolsClassifyEnabled := cfg.ToolsClassifyEnabled
	multiIntentEnabled := cfg.MultiIntentEnabled
	multiIntentMax := cfg.MultiIntentMax
	if multiIntentMax <= 0 {
		multiIntentMax = 3
	}
	return &Service{
		maxMessageLength:     maxMessageLength,
		sessionTTL:           sessionTTL,
		maxSessionMessages:   maxSessionMessages,
		store:                cfg.Store,
		classifier:           defaultClassifier(cfg.Classifier),
		llm:                  cfg.LLM,
		tools:                cfg.Tools,
		diagnosis:            cfg.Diagnosis,
		summarizer:           cfg.Summarizer,
		summaryEnabled:       cfg.SummaryEnabled,
		contextManager:       cfg.ContextManager,
		toolDefs:             toolDefs,
		toolsClassifyEnabled: toolsClassifyEnabled,
		multiIntentEnabled:   multiIntentEnabled,
		multiIntentMax:       multiIntentMax,
	}
}

func (s *Service) Chat(ctx context.Context, user User, req ChatRequest) (ChatResponse, error) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return ChatResponse{}, ErrMessageRequired
	}
	if len([]rune(message)) > s.maxMessageLength {
		return ChatResponse{}, fmt.Errorf("%w: 最多 %d 个字符", ErrMessageTooLong, s.maxMessageLength)
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
	parsed := s.classifier.ClassifyMultiWithMax(message, s.multiIntentMax)
	if !s.multiIntentEnabled {
		parsed.Intents = nil
	}
	parsed = s.classifyWithFallback(ctx, message, parsed)
	parsed = nlu.TrimIntents(parsed, s.multiIntentMax)
	history, contextEntities := s.loadSessionContext(ctx, sessionID)
	parsed = applyContextEntities(parsed, contextEntities)
	ctx = WithUser(ctx, user)
	toolCalls, toolReply, multiIntentResults, err := s.executeIntents(ctx, user, parsed)
	if err != nil {
		return ChatResponse{}, err
	}
	reply, suggestions := s.buildReplyWithSummary(ctx, message, parsed, toolReply, toolCalls, history)
	s.saveSessionContext(ctx, sessionID, contextEntities, s.extractContextEntities(toolCalls, parsed.Intent))

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
		SessionID:    sessionID,
		Reply:        reply,
		Intent:       parsed.Intent,
		Confidence:   parsed.Confidence,
		ToolCalls:    toolCalls,
		Suggestions:  suggestions,
		MultiIntents: multiIntentResults,
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

func (s *Service) GetSession(ctx context.Context, user User, sessionID string) (SessionSummary, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionSummary{}, ErrSessionRequired
	}
	meta, err := s.requireOwnedSession(ctx, user, sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	return SessionSummary{
		ID:        meta.ID,
		Title:     meta.Title,
		UpdatedAt: meta.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
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

func (s *Service) ToolSchemas() []ToolSchema {
	lister, ok := s.tools.(ToolSchemaLister)
	if !ok || lister == nil {
		return []ToolSchema{}
	}
	schemas := lister.ToolSchemas()
	if schemas == nil {
		return []ToolSchema{}
	}
	return schemas
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
