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

var (
	ErrDisabled        = errors.New("llm classifier disabled")
	ErrInvalidResponse = errors.New("invalid llm classifier response")
)

type Client struct {
	apiKey           string
	apiURL           string
	model            string
	timeout          time.Duration
	httpClient       *http.Client
	maxResponseBytes int64
}

type Options struct {
	APIKey           string
	APIURL           string
	Model            string
	Timeout          time.Duration
	HTTPClient       *http.Client
	MaxResponseBytes int64
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
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
	}
}

func (c *Client) Classify(ctx context.Context, message string) (nlu.Result, error) {
	if c == nil || c.apiKey == "" || c.apiURL == "" || c.model == "" {
		return nlu.Result{}, ErrDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt()},
			{Role: "user", Content: strings.TrimSpace(message)},
		},
		Temperature: 0,
	})
	if err != nil {
		return nlu.Result{}, fmt.Errorf("marshal llm request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nlu.Result{}, fmt.Errorf("create llm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nlu.Result{}, fmt.Errorf("call llm classifier: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nlu.Result{}, fmt.Errorf("llm classifier returned status %d", resp.StatusCode)
	}

	var decoded chatResponse
	limited := io.LimitReader(resp.Body, c.maxResponseBytes)
	if err := json.NewDecoder(limited).Decode(&decoded); err != nil {
		return nlu.Result{}, fmt.Errorf("decode llm response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nlu.Result{}, ErrInvalidResponse
	}
	return parseIntentPayload(decoded.Choices[0].Message.Content)
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
		nlu.IntentHostQuery,
		nlu.IntentMetricQuery,
		nlu.IntentGeneralChat,
		nlu.IntentUnknown:
		return true
	default:
		return false
	}
}

func isAllowedEntity(key string) bool {
	switch key {
	case "instance", "severity", "status", "window", "query":
		return true
	default:
		return false
	}
}

func systemPrompt() string {
	return strings.Join([]string{
		"You classify CloudOps Copilot user messages.",
		"Return JSON only, without markdown.",
		"Allowed intents: alert_query, alert_event_query, alert_history_query, host_query, metric_query, general_chat, unknown.",
		"Allowed entities: instance, severity, status, window, query.",
		"Use query only for explicit PromQL or query_range requests.",
		"Never return commands or write actions.",
		`Example: {"intent":"host_query","confidence":0.7,"entities":{"status":"down"}}`,
	}, "\n")
}
