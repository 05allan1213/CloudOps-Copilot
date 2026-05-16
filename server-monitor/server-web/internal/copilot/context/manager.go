package context

import (
	stdcontext "context"
	"strings"
	"time"

	"server-web/internal/copilot/llm"
	"server-web/internal/copilot/service"
	"server-web/internal/copilot/session"
)

const defaultMaxRounds = 10

type Manager struct {
	store     sessionStore
	maxRounds int
}

type sessionStore interface {
	ListMessages(ctx stdcontext.Context, sessionID string) ([]session.Message, error)
	GetContext(ctx stdcontext.Context, sessionID string) (session.SessionContext, error)
	SetContext(ctx stdcontext.Context, sessionID string, ctxData session.SessionContext, ttl time.Duration) error
}

type Options struct {
	Store     sessionStore
	MaxRounds int
}

func NewManager(opts Options) *Manager {
	maxRounds := opts.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultMaxRounds
	}
	return &Manager{
		store:     opts.Store,
		maxRounds: maxRounds,
	}
}

func (m *Manager) LoadHistory(ctx stdcontext.Context, sessionID string) ([]service.ChatHistoryItem, error) {
	if m == nil || m.store == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}

	messages, err := m.store.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	maxMessages := m.maxRounds * 2
	if maxMessages > 0 && len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}

	history := make([]service.ChatHistoryItem, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if content == "" || (role != "user" && role != "assistant") {
			continue
		}
		history = append(history, service.ChatHistoryItem{
			Role:    role,
			Content: content,
		})
	}
	return history, nil
}

func (m *Manager) LoadEntities(ctx stdcontext.Context, sessionID string) (map[string]string, error) {
	if m == nil || m.store == nil || strings.TrimSpace(sessionID) == "" {
		return map[string]string{}, nil
	}

	ctxData, err := m.store.GetContext(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return copyNonEmptyEntities(ctxData.LastEntities), nil
}

func (m *Manager) SaveEntities(ctx stdcontext.Context, sessionID string, entities map[string]string, ttl time.Duration) error {
	if m == nil || m.store == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}

	copied := copyNonEmptyEntities(entities)
	if len(copied) == 0 {
		return nil
	}
	return m.store.SetContext(ctx, sessionID, session.SessionContext{LastEntities: copied}, ttl)
}

func (m *Manager) BuildMessages(systemPrompt string, history []service.ChatHistoryItem, userPrompt string) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0, len(history)+2)
	if systemPrompt = strings.TrimSpace(systemPrompt); systemPrompt != "" {
		messages = append(messages, llm.ChatMessage{Role: "system", Content: systemPrompt})
	}
	for _, item := range history {
		role := strings.TrimSpace(item.Role)
		content := strings.TrimSpace(item.Content)
		if content == "" || (role != "user" && role != "assistant") {
			continue
		}
		messages = append(messages, llm.ChatMessage{Role: role, Content: content})
	}
	if userPrompt = strings.TrimSpace(userPrompt); userPrompt != "" {
		messages = append(messages, llm.ChatMessage{Role: "user", Content: userPrompt})
	}
	return messages
}

func copyNonEmptyEntities(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}
