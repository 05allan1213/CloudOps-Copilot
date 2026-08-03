package llm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultMaxResponseBytes int64 = 64 * 1024
const maxErrorResponseBytes int64 = 4 * 1024
const defaultMaxRetries = 2
const initialRetryBackoff = 500 * time.Millisecond

var (
	ErrDisabled        = errors.New("llm client disabled")
	ErrInvalidResponse = errors.New("invalid llm response")
)

type LLMObserver interface {
	ObserveLLMRequest(model, result string, durationSeconds float64, inputTokens, outputTokens int)
}

type Client struct {
	apiKey           string
	apiURL           string
	model            string
	timeout          time.Duration
	httpClient       *http.Client
	maxResponseBytes int64
	maxTokens        int
	maxRetries       int
	reasoningEffort  string
	observer         LLMObserver
}

type Options struct {
	APIKey           string
	APIURL           string
	Model            string
	Timeout          time.Duration
	HTTPClient       *http.Client
	MaxResponseBytes int64
	MaxTokens        int
	ReasoningEffort  string
	// MaxRetries overrides the client retry count when non-nil. Agent Runtime sets zero
	// so its persisted retry policy is the only retry owner.
	MaxRetries *int
	Observer   LLMObserver
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []ChatMessage `json:"messages"`
	Temperature     float64       `json:"temperature"`
	MaxTokens       int           `json:"max_tokens,omitempty"`
	Stream          bool          `json:"stream,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

type chatResponse struct {
	ID      string `json:"id,omitempty"`
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage *ChatUsage `json:"usage,omitempty"`
}

type chatStreamChunk struct {
	ID      string `json:"id,omitempty"`
	Choices []struct {
		Delta   ChatMessage `json:"delta"`
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage *ChatUsage `json:"usage,omitempty"`
}

type ChatUsage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	RequestIDHash    string `json:"-"`
}

func NewClient(options Options) *Client {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	maxResponseBytes := options.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	maxRetries := defaultMaxRetries
	if options.MaxRetries != nil && *options.MaxRetries >= 0 {
		maxRetries = *options.MaxRetries
	}
	return &Client{
		apiKey:           strings.TrimSpace(options.APIKey),
		apiURL:           strings.TrimSpace(options.APIURL),
		model:            strings.TrimSpace(options.Model),
		timeout:          timeout,
		httpClient:       httpClient,
		maxResponseBytes: maxResponseBytes,
		maxTokens:        options.MaxTokens,
		maxRetries:       maxRetries,
		reasoningEffort:  strings.TrimSpace(options.ReasoningEffort),
		observer:         options.Observer,
	}
}

func (c *Client) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		if c != nil && c.observer != nil {
			c.observer.ObserveLLMRequest(c.model, "error", 0, 0, 0)
		}
		return "", ErrDisabled
	}

	start := time.Now()
	var lastErr error
	var lastUsage *ChatUsage
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialRetryBackoff * time.Duration(1<<(attempt-1))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				if c.observer != nil {
					c.observer.ObserveLLMRequest(c.model, "error", time.Since(start).Seconds(), 0, 0)
				}
				return "", ctx.Err()
			case <-timer.C:
			}
		}

		result, usage, err := c.doRequest(ctx, systemPrompt, userPrompt)
		if err == nil {
			if c.observer != nil {
				inputTokens, outputTokens := tokenCountsFromUsage(usage)
				c.observer.ObserveLLMRequest(c.model, "success", time.Since(start).Seconds(), inputTokens, outputTokens)
			}
			return result, nil
		}
		lastErr = err
		lastUsage = usage
		if !isRetryableError(err) {
			if c.observer != nil {
				inputTokens, outputTokens := tokenCountsFromUsage(lastUsage)
				c.observer.ObserveLLMRequest(c.model, "error", time.Since(start).Seconds(), inputTokens, outputTokens)
			}
			return "", err
		}
	}
	if c.observer != nil {
		inputTokens, outputTokens := tokenCountsFromUsage(lastUsage)
		c.observer.ObserveLLMRequest(c.model, "error", time.Since(start).Seconds(), inputTokens, outputTokens)
	}
	return "", lastErr
}

func (c *Client) Chat(ctx context.Context, messages []ChatMessage) (string, *ChatUsage, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		if c != nil && c.observer != nil {
			c.observer.ObserveLLMRequest(c.model, "error", 0, 0, 0)
		}
		return "", nil, ErrDisabled
	}

	start := time.Now()
	var lastErr error
	var lastUsage *ChatUsage
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialRetryBackoff * time.Duration(1<<(attempt-1))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				if c.observer != nil {
					c.observer.ObserveLLMRequest(c.model, "error", time.Since(start).Seconds(), 0, 0)
				}
				return "", nil, ctx.Err()
			case <-timer.C:
			}
		}

		content, usage, err := c.doRequestMessages(ctx, messages, 0.3)
		if err == nil {
			if c.observer != nil {
				inputTokens, outputTokens := tokenCountsFromUsage(usage)
				c.observer.ObserveLLMRequest(c.model, "success", time.Since(start).Seconds(), inputTokens, outputTokens)
			}
			return content, usage, nil
		}
		lastErr = err
		lastUsage = usage
		if !isRetryableError(err) {
			if c.observer != nil {
				inputTokens, outputTokens := tokenCountsFromUsage(lastUsage)
				c.observer.ObserveLLMRequest(c.model, "error", time.Since(start).Seconds(), inputTokens, outputTokens)
			}
			return "", usage, err
		}
	}
	if c.observer != nil {
		inputTokens, outputTokens := tokenCountsFromUsage(lastUsage)
		c.observer.ObserveLLMRequest(c.model, "error", time.Since(start).Seconds(), inputTokens, outputTokens)
	}
	return "", lastUsage, lastErr
}

func (c *Client) ChatStream(ctx context.Context, messages []ChatMessage, onDelta func(string) error) (string, *ChatUsage, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		if c != nil && c.observer != nil {
			c.observer.ObserveLLMRequest(c.model, "error", 0, 0, 0)
		}
		return "", nil, ErrDisabled
	}

	start := time.Now()
	content, usage, err := c.doChatStreamRequest(ctx, messages, onDelta)
	if c.observer != nil {
		inputTokens, outputTokens := tokenCountsFromUsage(usage)
		result := "success"
		if err != nil {
			result = "error"
		}
		c.observer.ObserveLLMRequest(c.model, result, time.Since(start).Seconds(), inputTokens, outputTokens)
	}
	return content, usage, err
}

func tokenCountsFromUsage(usage *ChatUsage) (int, int) {
	if usage == nil {
		return 0, 0
	}
	return usage.PromptTokens, usage.CompletionTokens
}

func isRetryableError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

func (c *Client) doRequest(ctx context.Context, systemPrompt, userPrompt string) (string, *ChatUsage, error) {
	return c.doRequestMessages(ctx, []ChatMessage{
		{Role: "system", Content: strings.TrimSpace(systemPrompt)},
		{Role: "user", Content: strings.TrimSpace(userPrompt)},
	}, 0)
}

func (c *Client) doRequestMessages(ctx context.Context, messages []ChatMessage, temperature float64) (string, *ChatUsage, error) {
	message, usage, err := c.doChatRequest(ctx, chatRequest{
		Model:           c.model,
		Messages:        messages,
		Temperature:     temperature,
		MaxTokens:       c.maxTokens,
		ReasoningEffort: c.reasoningEffort,
	})
	if err != nil {
		return "", usage, err
	}
	return message.Content, usage, nil
}

func (c *Client) doChatRequest(ctx context.Context, chatReq chatRequest) (message ChatMessage, usage *ChatUsage, retErr error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(chatReq)
	if err != nil {
		return ChatMessage{}, nil, fmt.Errorf("marshal llm request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return ChatMessage{}, nil, fmt.Errorf("create llm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ChatMessage{}, nil, fmt.Errorf("call llm: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ChatMessage{}, nil, fmt.Errorf("llm returned status %d%s", resp.StatusCode, responseBodyDetail(resp.Body))
	}

	var decoded chatResponse
	limited := io.LimitReader(resp.Body, c.maxResponseBytes)
	if err := json.NewDecoder(limited).Decode(&decoded); err != nil {
		return ChatMessage{}, nil, fmt.Errorf("decode llm response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return ChatMessage{}, nil, ErrInvalidResponse
	}
	if decoded.Usage == nil {
		decoded.Usage = &ChatUsage{}
	}
	decoded.Usage.RequestIDHash = providerRequestIDHash(resp.Header, decoded.ID)
	return decoded.Choices[0].Message, decoded.Usage, nil
}

func providerRequestIDHash(header http.Header, bodyID string) string {
	for _, name := range []string{"x-request-id", "request-id", "x-trace-id", "trace-id"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			sum := sha256.Sum256([]byte(strings.ToLower(name) + "\x00" + value))
			return hex.EncodeToString(sum[:])
		}
	}
	if value := strings.TrimSpace(bodyID); value != "" {
		sum := sha256.Sum256([]byte("response-id\x00" + value))
		return hex.EncodeToString(sum[:])
	}
	return ""
}

func (c *Client) doChatStreamRequest(ctx context.Context, messages []ChatMessage, onDelta func(string) error) (content string, usageResult *ChatUsage, retErr error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(chatRequest{
		Model:           c.model,
		Messages:        messages,
		Temperature:     0.3,
		MaxTokens:       c.maxTokens,
		Stream:          true,
		ReasoningEffort: c.reasoningEffort,
	})
	if err != nil {
		return "", nil, fmt.Errorf("marshal llm stream request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("create llm stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("call llm stream: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", nil, fmt.Errorf("llm stream returned status %d%s", resp.StatusCode, responseBodyDetail(resp.Body))
	}

	var builder strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	maxScanToken := int(c.maxResponseBytes)
	if maxScanToken < 64*1024 {
		maxScanToken = 64 * 1024
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanToken)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return builder.String(), usageResult, fmt.Errorf("decode llm stream chunk: %w", err)
		}
		if chunk.Usage != nil {
			usageResult = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta.Content
			if delta == "" {
				delta = choice.Message.Content
			}
			if delta == "" {
				continue
			}
			builder.WriteString(delta)
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					return builder.String(), usageResult, fmt.Errorf("handle llm stream delta: %w", err)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return builder.String(), usageResult, fmt.Errorf("read llm stream: %w", err)
	}
	content = builder.String()
	if strings.TrimSpace(content) == "" {
		return content, usageResult, ErrInvalidResponse
	}
	return content, usageResult, nil
}

func responseBodyDetail(body io.Reader) string {
	if body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorResponseBytes))
	if err != nil {
		return ""
	}
	detail := strings.TrimSpace(string(raw))
	if detail == "" {
		return ""
	}
	return ": " + strings.Join(strings.Fields(detail), " ")
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}
