package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server-web/copilot/diagnosis"
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
	Diagnosis          DiagnosisService
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
	maxMessageLength   int
	sessionTTL         time.Duration
	maxSessionMessages int
	store              session.Store
	classifier         *nlu.Classifier
	llm                LLMClassifier
	tools              ToolExecutor
	diagnosis          DiagnosisService
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

type DiagnosisService interface {
	Trigger(ctx context.Context, user diagnosis.User, req diagnosis.Request) (diagnosis.ReportResponse, error)
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

type ToolSchema struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  []ToolParamSchema `json:"parameters"`
	RiskLevel   string            `json:"risk_level"`
	ReadOnly    bool              `json:"read_only"`
	Timeout     time.Duration     `json:"timeout"`
}

type ToolParamSchema struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
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
		diagnosis:          cfg.Diagnosis,
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
	ctx = WithUser(ctx, user)
	toolCalls, toolReply, err := s.executeIntent(ctx, user, parsed)
	if err != nil {
		return ChatResponse{}, err
	}
	reply := buildReply(parsed, toolReply, toolCalls)

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

func (s *Service) executeIntent(ctx context.Context, user User, result nlu.Result) ([]ToolCall, string, error) {
	if result.Intent == nlu.IntentDiagnosisRequest {
		return s.executeDiagnosis(ctx, user, result)
	}
	return s.executeTools(ctx, result)
}

func (s *Service) executeDiagnosis(ctx context.Context, user User, result nlu.Result) ([]ToolCall, string, error) {
	if s.diagnosis == nil {
		return []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: "diagnosis service unavailable"}}, "", nil
	}
	req := diagnosis.Request{
		Fingerprint: result.Entities["fingerprint"],
		AlertName:   result.Entities["alert_name"],
		Instance:    result.Entities["instance"],
		TriggerType: diagnosis.TriggerChat,
	}
	if rawID := strings.TrimSpace(result.Entities["alert_history_id"]); rawID != "" {
		if id, err := strconv.ParseUint(rawID, 10, 64); err == nil {
			req.AlertHistoryID = id
		}
	}
	report, err := s.diagnosis.Trigger(ctx, diagnosis.User{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, req)
	if err != nil {
		var conflict diagnosis.ConflictError
		if errors.As(err, &conflict) {
			return []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: err.Error(), Result: conflict.Candidates}}, buildDiagnosisCandidatesReply(conflict.Candidates), nil
		}
		return []ToolCall{{Name: "diagnosis.trigger", Status: "error", Error: err.Error()}}, buildDiagnosisErrorReply(err), nil
	}
	reply := fmt.Sprintf("诊断报告已生成：#%d，状态 %s，置信度 %.0f%%。摘要：%s", report.ID, report.Status, report.Confidence*100, report.Summary)
	return []ToolCall{{Name: "diagnosis.trigger", Status: "success", Result: report}}, reply, nil
}

func buildDiagnosisCandidatesReply(candidates []diagnosis.DiagnosisCandidate) string {
	if len(candidates) == 0 {
		return "匹配到多条告警，请提供 fingerprint 或 alert_history_id 后再诊断。"
	}
	var builder strings.Builder
	builder.WriteString("匹配到多条告警，请选择一条后再诊断：")
	for i, candidate := range candidates {
		if i >= 5 {
			builder.WriteString("\n- 还有更多候选，请使用更精确的 fingerprint 或 alert_history_id")
			break
		}
		builder.WriteString(fmt.Sprintf(
			"\n- alert_history_id=%d fingerprint=%s alert=%s instance=%s status=%s",
			candidate.AlertHistoryID,
			candidate.Fingerprint,
			candidate.AlertName,
			candidate.Instance,
			candidate.Status,
		))
	}
	return builder.String()
}

func buildDiagnosisErrorReply(err error) string {
	switch {
	case errors.Is(err, diagnosis.ErrInvalidRequest):
		return "请提供真实的 fingerprint、alert_history_id，或 alert_name + instance 后再诊断。可以先查询最近告警历史，复制其中的 alert_history_id 或 fingerprint。"
	case errors.Is(err, diagnosis.ErrNotFound):
		return "没有找到匹配的告警目标。请先查询最近告警历史或当前 firing 告警，再使用真实的 alert_history_id 或 fingerprint 发起诊断。"
	default:
		return ""
	}
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

func buildReply(result nlu.Result, toolReply string, toolCalls []ToolCall) string {
	if toolReply != "" {
		return toolReply
	}
	for _, call := range toolCalls {
		if call.Status == "error" && call.Error != "" {
			return fmt.Sprintf("Tool %s failed: %s", call.Name, call.Error)
		}
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
	case nlu.IntentDiagnosisRequest:
		return "请提供 fingerprint、alert_history_id，或 alert_name + instance 以生成单条告警诊断。"
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
	case nlu.IntentDiagnosisRequest:
		return []string{"显示最近 5 条告警历史", "显示当前 firing 告警"}
	case nlu.IntentGeneralChat:
		return []string{"What alerts are firing?", "Which hosts are offline?", "Show CPU trend for node-1"}
	default:
		return []string{"What alerts are firing?", "Which hosts are offline?", "Show CPU trend for node-1"}
	}
}
