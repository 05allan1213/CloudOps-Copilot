package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"server-web/copilot/nlu"
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
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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

func (c *Client) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		if c != nil && c.observer != nil {
			c.observer.ObserveLLMRequest(c.model, "error", 0, 0, 0)
		}
		return "", ErrDisabled
	}

	start := time.Now()
	var lastErr error
	var lastUsage *chatUsage
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

func tokenCountsFromUsage(usage *chatUsage) (int, int) {
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

func (c *Client) doRequest(ctx context.Context, systemPrompt, userPrompt string) (string, *chatUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: strings.TrimSpace(systemPrompt)},
			{Role: "user", Content: strings.TrimSpace(userPrompt)},
		},
		Temperature: 0,
		MaxTokens:   c.maxTokens,
	})
	if err != nil {
		return "", nil, fmt.Errorf("marshal llm request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("create llm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("call llm: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", nil, fmt.Errorf("llm returned status %d%s", resp.StatusCode, responseBodyDetail(resp.Body))
	}

	var decoded chatResponse
	limited := io.LimitReader(resp.Body, c.maxResponseBytes)
	if err := json.NewDecoder(limited).Decode(&decoded); err != nil {
		return "", nil, fmt.Errorf("decode llm response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", nil, ErrInvalidResponse
	}
	return decoded.Choices[0].Message.Content, decoded.Usage, nil
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

func systemPrompt() string {
	return strings.Join([]string{
		"You classify CloudOps Copilot user messages.",
		"Return JSON only, without markdown.",
		"Allowed intents: alert_query, alert_event_query, alert_history_query, alert_rule_list_query, diagnosis_request, host_query, metric_query, general_chat, unknown.",
		"Allowed entities: instance, severity, status, window, query, count, alert_name, fingerprint, alert_history_id, page, page_size, search, sort, risk, group_id, namespace, metric_type, resource_type, resource_name.",
		"Use query only for explicit PromQL or query_range requests.",
		"Never return commands or write actions.",
		`Example: {"intent":"host_query","confidence":0.7,"entities":{"status":"down"}}`,
	}, "\n")
}
