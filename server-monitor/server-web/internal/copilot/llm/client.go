package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"server-web/internal/copilot/nlu"
)

const defaultMaxResponseBytes int64 = 64 * 1024
const maxErrorResponseBytes int64 = 4 * 1024
const maxRetries = 2
const initialRetryBackoff = 500 * time.Millisecond

var (
	ErrDisabled        = errors.New("llm classifier disabled")
	ErrInvalidResponse = errors.New("invalid llm classifier response")
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
	Observer         LLMObserver
}

type chatRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatMessage    `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  interface{}      `json:"tool_choice,omitempty"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []toolCallResult `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage *ChatUsage `json:"usage,omitempty"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta   ChatMessage `json:"delta"`
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage *ChatUsage `json:"usage,omitempty"`
}

type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type toolCallResult struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type intentPayload struct {
	Intent     string            `json:"intent"`
	Confidence float64           `json:"confidence"`
	Entities   map[string]string `json:"entities"`
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
	return &Client{
		apiKey:           strings.TrimSpace(options.APIKey),
		apiURL:           strings.TrimSpace(options.APIURL),
		model:            strings.TrimSpace(options.Model),
		timeout:          timeout,
		httpClient:       httpClient,
		maxResponseBytes: maxResponseBytes,
		maxTokens:        options.MaxTokens,
		observer:         options.Observer,
	}
}

func (c *Client) Classify(ctx context.Context, message string) (nlu.Result, error) {
	content, err := c.Generate(ctx, systemPrompt(), strings.TrimSpace(message))
	if err != nil {
		return nlu.Result{}, err
	}
	return parseIntentPayload(content)
}

func parseToolArguments(arguments string) (map[string]string, error) {
	entities := map[string]string{}
	if arguments == "" {
		return entities, nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("invalid tool arguments json: %w", err)
	}
	for k, v := range args {
		switch val := v.(type) {
		case string:
			if val != "" {
				entities[k] = val
			}
		default:
			entities[k] = fmt.Sprintf("%v", val)
		}
	}
	return entities, nil
}

func (c *Client) ClassifyWithTools(ctx context.Context, message string, tools []ToolDefinition) (nlu.Result, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		return nlu.Result{}, ErrDisabled
	}
	if len(tools) == 0 {
		return nlu.Result{}, fmt.Errorf("%w: tools list is empty", ErrInvalidResponse)
	}

	content, usage, err := c.doRequestWithTools(ctx, toolsSystemPrompt(), message, tools)
	if err != nil {
		return nlu.Result{}, err
	}
	if c.observer != nil {
		inputTokens, outputTokens := tokenCountsFromUsage(usage)
		c.observer.ObserveLLMRequest(c.model, "success", 0, inputTokens, outputTokens)
	}

	if len(content.ToolCalls) > 0 {
		tc := content.ToolCalls[0]
		normalized := normalizeToolName(tc.Function.Name)
		intent, ok := toolNameToIntent[normalized]
		if !ok {
			return nlu.Result{}, fmt.Errorf("%w: unsupported tool %q", ErrInvalidResponse, tc.Function.Name)
		}
		entities, parseErr := parseToolArguments(tc.Function.Arguments)
		if parseErr != nil {
			return nlu.Result{}, fmt.Errorf("tool %q: %w", tc.Function.Name, parseErr)
		}
		return nlu.Result{
			Intent:       intent,
			Confidence:   0.8,
			Entities:     entities,
			SelectedTool: normalized,
		}, nil
	}

	if content.Content != "" {
		return parseIntentPayload(content.Content)
	}

	return nlu.Result{}, ErrInvalidResponse
}

func (c *Client) ClassifyWithToolsMulti(ctx context.Context, message string, tools []ToolDefinition) (nlu.Result, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		return nlu.Result{}, ErrDisabled
	}
	if len(tools) == 0 {
		return nlu.Result{}, fmt.Errorf("%w: tools list is empty", ErrInvalidResponse)
	}

	content, usage, err := c.doRequestWithTools(ctx, toolsSystemPrompt(), message, tools)
	if err != nil {
		return nlu.Result{}, err
	}
	if c.observer != nil {
		inputTokens, outputTokens := tokenCountsFromUsage(usage)
		c.observer.ObserveLLMRequest(c.model, "success", 0, inputTokens, outputTokens)
	}

	if len(content.ToolCalls) > 0 {
		var intents []nlu.IntentScore
		primaryIntent := ""
		primaryConfidence := 0.0
		primaryEntities := map[string]string{}
		var primarySelectedTool string

		for _, tc := range content.ToolCalls {
			normalized := normalizeToolName(tc.Function.Name)
			intent, ok := toolNameToIntent[normalized]
			if !ok {
				continue
			}
			entities, parseErr := parseToolArguments(tc.Function.Arguments)
			if parseErr != nil {
				continue
			}
			is := nlu.IntentScore{
				Intent:       intent,
				Confidence:   0.8,
				Entities:     entities,
				SelectedTool: normalized,
			}
			intents = append(intents, is)
			if primaryIntent == "" {
				primaryIntent = intent
				primaryConfidence = 0.8
				primaryEntities = entities
				primarySelectedTool = normalized
			}
		}

		if primaryIntent == "" {
			return nlu.Result{}, fmt.Errorf("%w: no valid tool calls", ErrInvalidResponse)
		}

		return nlu.Result{
			Intent:       primaryIntent,
			Confidence:   primaryConfidence,
			Entities:     primaryEntities,
			Intents:      intents,
			SelectedTool: primarySelectedTool,
		}, nil
	}

	if content.Content != "" {
		return parseMultiIntentPayload(content.Content)
	}

	return nlu.Result{}, ErrInvalidResponse
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
	for attempt := 0; attempt <= maxRetries; attempt++ {
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
	for attempt := 0; attempt <= maxRetries; attempt++ {
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
		Model:       c.model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   c.maxTokens,
	})
	if err != nil {
		return "", usage, err
	}
	return message.Content, usage, nil
}

func (c *Client) doRequestWithTools(ctx context.Context, sysPrompt, userPrompt string, tools []ToolDefinition) (ChatMessage, *ChatUsage, error) {
	return c.doChatRequest(ctx, chatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: strings.TrimSpace(sysPrompt)},
			{Role: "user", Content: strings.TrimSpace(userPrompt)},
		},
		Tools:       tools,
		ToolChoice:  "auto",
		Temperature: 0,
		MaxTokens:   c.maxTokens,
	})
}

func (c *Client) doChatRequest(ctx context.Context, chatReq chatRequest) (ChatMessage, *ChatUsage, error) {
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
	defer resp.Body.Close()
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
	return decoded.Choices[0].Message, decoded.Usage, nil
}

func (c *Client) doChatStreamRequest(ctx context.Context, messages []ChatMessage, onDelta func(string) error) (string, *ChatUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   c.maxTokens,
		Stream:      true,
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
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", nil, fmt.Errorf("llm stream returned status %d%s", resp.StatusCode, responseBodyDetail(resp.Body))
	}

	var builder strings.Builder
	var usage *ChatUsage
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
			return builder.String(), usage, fmt.Errorf("decode llm stream chunk: %w", err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
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
					return builder.String(), usage, fmt.Errorf("handle llm stream delta: %w", err)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return builder.String(), usage, fmt.Errorf("read llm stream: %w", err)
	}
	return builder.String(), usage, nil
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

func parseIntentPayload(content string) (nlu.Result, error) {
	content = strings.TrimSpace(strings.Trim(content, "`"))
	var payload intentPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nlu.Result{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	intent := strings.TrimSpace(payload.Intent)
	if !isAllowedIntent(intent) {
		return nlu.Result{}, fmt.Errorf("%w: unsupported intent %q", ErrInvalidResponse, intent)
	}
	confidence := payload.Confidence
	if confidence <= 0 || confidence > 1 {
		confidence = 0.6
	}
	return nlu.Result{
		Intent:     intent,
		Confidence: confidence,
		Entities:   sanitizeEntities(payload.Entities),
	}, nil
}

func parseMultiIntentPayload(content string) (nlu.Result, error) {
	content = strings.TrimSpace(strings.Trim(content, "`"))

	var multiPayload struct {
		Intents []intentPayload `json:"intents"`
	}
	if err := json.Unmarshal([]byte(content), &multiPayload); err == nil && len(multiPayload.Intents) > 0 {
		var intents []nlu.IntentScore
		for _, ip := range multiPayload.Intents {
			intent := strings.TrimSpace(ip.Intent)
			if !isAllowedIntent(intent) {
				continue
			}
			confidence := ip.Confidence
			if confidence <= 0 || confidence > 1 {
				confidence = 0.6
			}
			intents = append(intents, nlu.IntentScore{
				Intent:     intent,
				Confidence: confidence,
				Entities:   sanitizeEntities(ip.Entities),
			})
		}
		if len(intents) == 0 {
			return nlu.Result{}, fmt.Errorf("%w: no valid intents in multi-intent response", ErrInvalidResponse)
		}
		return nlu.Result{
			Intent:     intents[0].Intent,
			Confidence: intents[0].Confidence,
			Entities:   intents[0].Entities,
			Intents:    intents,
		}, nil
	}

	return parseIntentPayload(content)
}

func sanitizeEntities(entities map[string]string) map[string]string {
	cleaned := map[string]string{}
	for key, value := range entities {
		key = strings.TrimSpace(key)
		if !isAllowedEntity(key) {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cleaned[key] = value
	}
	return cleaned
}

func isAllowedIntent(intent string) bool {
	switch intent {
	case nlu.IntentAlertQuery,
		nlu.IntentAlertEventQuery,
		nlu.IntentAlertHistoryQuery,
		nlu.IntentAlertRuleListQuery,
		nlu.IntentHostQuery,
		nlu.IntentMetricQuery,
		nlu.IntentDiagnosisRequest,
		nlu.IntentGeneralChat,
		nlu.IntentUnknown:
		return true
	default:
		return false
	}
}

func isAllowedEntity(key string) bool {
	switch key {
	case "instance", "severity", "status", "window", "query", "count", "alert_name", "fingerprint", "alert_history_id", "page", "page_size", "search", "sort", "risk", "group_id", "enabled", "metric_keywords", "namespace", "metric_type", "resource_type", "resource_name":
		return true
	default:
		return false
	}
}

var toolNameToIntent = map[string]string{
	"alert_list_active":   nlu.IntentAlertQuery,
	"alert_events":        nlu.IntentAlertEventQuery,
	"alert_history":       nlu.IntentAlertHistoryQuery,
	"alert_rule_list":     nlu.IntentAlertRuleListQuery,
	"host_list":           nlu.IntentHostQuery,
	"host_metrics":        nlu.IntentMetricQuery,
	"prom_query_range":    nlu.IntentMetricQuery,
	"runbook_search":      nlu.IntentMetricQuery,
	"k8s_get_pods":        nlu.IntentMetricQuery,
	"k8s_get_deployments": nlu.IntentMetricQuery,
	"k8s_get_services":    nlu.IntentMetricQuery,
	"k8s_get_nodes":       nlu.IntentMetricQuery,
	"k8s_get_events":      nlu.IntentMetricQuery,
	"k8s_get_logs":        nlu.IntentMetricQuery,
}

func openAIToolNameToRegistryName(name string) string {
	return strings.ReplaceAll(name, "_", ".")
}

func normalizeToolName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}
