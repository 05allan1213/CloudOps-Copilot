package k8sread

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	bearerTokenPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	keyValueSecret     = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|authorization|api[_-]?key|kubeconfig|license)\s*=\s*[^ \n\r\t]+`)
	yamlValueSecret    = regexp.MustCompile(`(?im)^[ \t]*(password|passwd|pwd|token|secret|api[_-]?key|kubeconfig|license):\s+.+$`)
	privateKeyPattern  = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

func SanitizeText(value string, maxBytes int) (string, bool) {
	sanitized := privateKeyPattern.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	sanitized = bearerTokenPattern.ReplaceAllString(sanitized, "Bearer [REDACTED]")
	sanitized = keyValueSecret.ReplaceAllString(sanitized, "$1=[REDACTED]")
	sanitized = yamlValueSecret.ReplaceAllStringFunc(sanitized, func(match string) string {
		idx := strings.Index(match, ":")
		if idx < 0 {
			return match
		}
		return match[:idx+1] + " [REDACTED]"
	})
	if maxBytes <= 0 || len(sanitized) <= maxBytes {
		return sanitized, false
	}
	return truncateUTF8(sanitized, maxBytes), true
}

func sanitizeMessage(value string, maxBytes int) string {
	sanitized, _ := SanitizeText(value, maxBytes)
	return sanitized
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	truncated := value[:maxBytes]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return strings.TrimRight(truncated, "\x00")
}
