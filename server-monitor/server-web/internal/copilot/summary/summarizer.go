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
	copilotsuggestion "server-web/internal/copilot/suggestion"
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

type LLMStreamChatClient interface {
	LLMChatClient
	ChatStream(ctx context.Context, messages []llm.ChatMessage, onDelta func(string) error) (string, *llm.ChatUsage, error)
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

func (s *Summarizer) SummarizeStream(ctx context.Context, input service.SummaryInput, onDelta func(string) error) (service.SummaryResult, error) {
	if s == nil || s.llm == nil {
		return service.SummaryResult{}, ErrFallback
	}

	streamClient, ok := s.llm.(LLMStreamChatClient)
	if !ok {
		return s.Summarize(ctx, input)
	}

	userPrompt, err := buildUserPrompt(input, s.maxPrompt)
	if err != nil {
		return service.SummaryResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	content, _, err := streamClient.ChatStream(callCtx, buildMessages(input.History, userPrompt), onDelta)
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
		Reply       string          `json:"reply"`
		Suggestions json.RawMessage `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err == nil {
		payload.Reply = strings.TrimSpace(payload.Reply)
		if payload.Reply == "" {
			return service.SummaryResult{}, ErrFallback
		}
		suggestions, err := parseSuggestions(payload.Suggestions)
		if err != nil {
			return service.SummaryResult{}, err
		}
		return service.SummaryResult{
			Reply:       payload.Reply,
			Suggestions: suggestions,
		}, nil
	}

	return service.SummaryResult{Reply: content}, nil
}

func parseSuggestions(raw json.RawMessage) ([]service.Suggestion, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var structured []service.Suggestion
	if err := json.Unmarshal(raw, &structured); err == nil {
		return filterSuggestions(structured), nil
	}

	var texts []string
	if err := json.Unmarshal(raw, &texts); err != nil {
		return nil, fmt.Errorf("decode summary suggestions: %w", err)
	}
	suggestions := make([]service.Suggestion, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text != "" {
			suggestions = append(suggestions, service.Suggestion{Text: text, Action: text})
		}
	}
	return suggestions, nil
}

func filterSuggestions(values []service.Suggestion) []service.Suggestion {
	result := make([]service.Suggestion, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value.Text)
		if text == "" {
			continue
		}
		action := strings.TrimSpace(value.Action)
		if action == "" {
			action = text
		}
		params := make(map[string]string, len(value.Params))
		for key, paramValue := range value.Params {
			key = strings.TrimSpace(key)
			paramValue = strings.TrimSpace(paramValue)
			if key != "" && paramValue != "" {
				params[key] = paramValue
			}
		}
		if len(params) == 0 {
			params = nil
		}
		result = append(result, service.Suggestion{
			Text:   text,
			Action: action,
			Intent: copilotsuggestion.NormalizeIntent(value.Intent),
			Params: params,
		})
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
