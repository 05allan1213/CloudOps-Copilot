package summary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"server-web/internal/copilot/llm"
	"server-web/internal/copilot/nlu"
	"server-web/internal/copilot/service"
)

var _ service.Summarizer = (*Summarizer)(nil)

const (
	defaultTimeout   = 8 * time.Second
	defaultMaxPrompt = 16 * 1024
)

var ErrFallback = errors.New("summary fallback")

type LLMChatClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage) (string, *llm.ChatUsage, error)
}

var _ LLMChatClient = (*llm.Client)(nil)

type Summarizer struct {
	llm       LLMChatClient
	timeout   time.Duration
	maxPrompt int
}

type Options struct {
	LLM       LLMChatClient
	Timeout   time.Duration
	MaxPrompt int
}

func NewSummarizer(opts Options) *Summarizer {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxPrompt := opts.MaxPrompt
	if maxPrompt <= 0 {
		maxPrompt = defaultMaxPrompt
	}
	return &Summarizer{
		llm:       opts.LLM,
		timeout:   timeout,
		maxPrompt: maxPrompt,
	}
}

func (s *Summarizer) Summarize(ctx context.Context, input service.SummaryInput) (service.SummaryResult, error) {
	if s == nil || s.llm == nil {
		return service.SummaryResult{}, ErrFallback
	}

	userPrompt, err := buildUserPrompt(input, s.maxPrompt)
	if err != nil {
		return service.SummaryResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	content, _, err := s.llm.Chat(callCtx, buildMessages(input.History, userPrompt))
	if err != nil {
		return service.SummaryResult{}, fmt.Errorf("%w: %v", ErrFallback, err)
	}
	result, err := parseSummaryResult(content)
	if err != nil {
		return service.SummaryResult{}, err
	}
	return result, nil
}

func buildMessages(history []service.ChatHistoryItem, userPrompt string) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0, len(history)+2)
	messages = append(messages, llm.ChatMessage{Role: "system", Content: summarySystemPrompt()})
	for _, item := range history {
		role := strings.TrimSpace(item.Role)
		content := strings.TrimSpace(item.Content)
		if content == "" || (role != "user" && role != "assistant") {
			continue
		}
		messages = append(messages, llm.ChatMessage{Role: role, Content: content})
	}
	messages = append(messages, llm.ChatMessage{Role: "user", Content: userPrompt})
	return messages
}

func buildUserPrompt(input service.SummaryInput, maxPrompt int) (string, error) {
	payload := struct {
		UserMessage string             `json:"user_message"`
		Intent      string             `json:"intent"`
		ToolCalls   []service.ToolCall `json:"tool_calls"`
	}{
		UserMessage: input.UserMessage,
		Intent:      input.Intent,
		ToolCalls:   input.ToolCalls,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal summary prompt: %w", err)
	}

	var sections []string
	switch input.Intent {
	case nlu.IntentGeneralChat:
		sections = append(sections, chatFallbackPrompt())
	case nlu.IntentUnknown:
		sections = append(sections, unknownIntentPrompt())
	}
	sections = append(sections,
		"请阅读下面 JSON 输入，输出符合系统要求的 JSON 摘要。",
		string(raw),
	)
	return truncateByBytes(strings.Join(sections, "\n\n"), maxPrompt), nil
}

func parseSummaryResult(content string) (service.SummaryResult, error) {
	content = strings.TrimSpace(strings.Trim(content, "`"))
	if content == "" {
		return service.SummaryResult{}, ErrFallback
	}

	var payload struct {
		Reply       string   `json:"reply"`
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err == nil {
		payload.Reply = strings.TrimSpace(payload.Reply)
		if payload.Reply == "" {
			return service.SummaryResult{}, ErrFallback
		}
		return service.SummaryResult{
			Reply:       payload.Reply,
			Suggestions: filterSuggestions(payload.Suggestions),
		}, nil
	}

	return service.SummaryResult{Reply: content}, nil
}

func filterSuggestions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func truncateByBytes(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	if maxBytes <= len("...") {
		return value[:maxBytes]
	}
	limit := maxBytes - len("...")
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit] + "..."
}
