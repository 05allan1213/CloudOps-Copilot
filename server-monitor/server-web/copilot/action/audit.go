package action

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/trace"
)

const redactedValue = "[REDACTED]"

type AuditEntry struct {
	Actor        string
	ActorRole    string
	Action       string
	ResourceType string
	ResourceID   string
	Request      json.RawMessage
	Result       string
	ErrorMessage string
	TraceID      string
}

func SanitizeJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}

	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return mustMarshal(truncateUTF8(trimmed, 2048))
	}
	return mustMarshal(sanitizeValue(value))
}

func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	span := trace.SpanContextFromContext(ctx)
	if !span.HasTraceID() {
		return ""
	}
	return span.TraceID().String()
}

func sanitizeValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			if isSensitiveKey(key) {
				cleaned[key] = redactedValue
				continue
			}
			cleaned[key] = sanitizeValue(val)
		}
		return cleaned
	case []interface{}:
		cleaned := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			cleaned = append(cleaned, sanitizeValue(item))
		}
		return cleaned
	default:
		return typed
	}
}

func containsSensitiveKey(raw json.RawMessage) bool {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return false
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return containsSensitiveValue(value)
}

func containsSensitiveValue(value interface{}) bool {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, val := range typed {
			if isSensitiveKey(key) || containsSensitiveValue(val) {
				return true
			}
		}
	case []interface{}:
		for _, item := range typed {
			if containsSensitiveValue(item) {
				return true
			}
		}
	}
	return false
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, needle := range []string{"password", "passwd", "secret", "token", "api_key", "authorization", "kubeconfig"} {
		if strings.Contains(key, needle) {
			return true
		}
	}
	return false
}

func mustMarshal(value interface{}) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for len(value) > limit && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
