package change

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

var credentialKey = regexp.MustCompile(`(?i)(credential|password|passwd|token|secret|authorization|api[_-]?key|private[_-]?key|client[_-]?secret)`)

var credentialText = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?)[^\s,;]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|token|password|passwd|client[_-]?secret|private[_-]?key)\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?s)-----BEGIN [^-\r\n]+ PRIVATE KEY-----.*?-----END [^-\r\n]+ PRIVATE KEY-----`),
}

var deniedPathPatterns = []string{
	".env", ".env.*", "**/secrets/**", "**/secret/**", "**/*secret*.yaml", "**/*credentials*", "**/*.pem", "**/*.key", "**/kubeconfig", "**/.ssh/**",
}

func SensitivePath(name string, additional []string) bool {
	name = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(name)), "./")
	for _, pattern := range append(append([]string{}, deniedPathPatterns...), additional...) {
		if doublestarMatch(strings.ToLower(pattern), strings.ToLower(name)) {
			return true
		}
	}
	return false
}

func RedactJSON(raw json.RawMessage, maxBytes int) (json.RawMessage, []string, bool) {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{}`), []string{"invalid_json_removed"}, true
	}
	redactions := []string{}
	value = redactValue(value, 0, &redactions)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`), []string{"unserializable_removed"}, true
	}
	truncated := false
	if maxBytes > 0 && len(encoded) > maxBytes {
		encoded, _ = json.Marshal(map[string]any{"truncated": true, "original_bytes": len(encoded)})
		truncated = true
	}
	return encoded, redactions, truncated
}

// RedactText removes common credential assignments from untrusted provider text.
// It is intentionally conservative: provider prose remains data, while likely
// credential values and private-key blocks never enter Evidence or prompts.
func RedactText(value string, maxBytes int) (string, bool) {
	redacted := false
	for _, pattern := range credentialText {
		updated := pattern.ReplaceAllStringFunc(value, func(match string) string {
			redacted = true
			if index := strings.IndexAny(match, ":="); index >= 0 {
				return match[:index+1] + "[REDACTED]"
			}
			return "[REDACTED_PRIVATE_KEY]"
		})
		value = updated
	}
	bounded := BoundUTF8(value, maxBytes)
	return bounded, redacted || len(bounded) < len(value)
}

func redactValue(value any, depth int, redactions *[]string) any {
	if depth > 8 {
		return "[TRUNCATED_DEPTH]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if credentialKey.MatchString(key) {
				result[key] = "[REDACTED]"
				*redactions = append(*redactions, key)
				continue
			}
			result[key] = redactValue(child, depth+1, redactions)
		}
		return result
	case []any:
		if len(typed) > 200 {
			typed = typed[:200]
		}
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = redactValue(typed[index], depth+1, redactions)
		}
		return result
	case string:
		return BoundUTF8(typed, 4096)
	default:
		return value
	}
}

func BoundUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func doublestarMatch(pattern, name string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	if pattern == name {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		pattern = strings.TrimPrefix(pattern, "**/")
		if matched, _ := filepath.Match(pattern, filepath.Base(name)); matched {
			return true
		}
		for index := strings.Index(name, "/"); index >= 0; index = strings.Index(name, "/") {
			name = name[index+1:]
			if matched, _ := filepath.Match(pattern, name); matched {
				return true
			}
		}
		return false
	}
	matched, _ := filepath.Match(pattern, name)
	return matched
}
