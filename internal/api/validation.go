package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
	maxCursorBytes  = 256
	maxHeaderBytes  = 128
)

// ParsePublicUUID accepts only canonical hyphenated UUIDs. Case is normalized
// to lowercase so the transport never falls back to an internal numeric key.
func ParsePublicUUID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return "", fmt.Errorf("%w: id must be a public UUID", ErrInvalidArgument)
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != strings.ToLower(value) {
		return "", fmt.Errorf("%w: id must be a public UUID", ErrInvalidArgument)
	}
	return parsed.String(), nil
}

func parseListOptions(request *http.Request) (cursor, afterID string, limit int, err error) {
	if request == nil {
		return "", "", 0, fmt.Errorf("%w: request is required", ErrInvalidArgument)
	}
	values := request.URL.Query()
	cursor = strings.TrimSpace(values.Get("cursor"))
	afterID = strings.TrimSpace(values.Get("after_id"))
	if len(cursor) > maxCursorBytes || len(afterID) > maxCursorBytes || containsControl(cursor) || containsControl(afterID) {
		return "", "", 0, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	limit = defaultPageSize
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > maxPageSize {
			return "", "", 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidArgument, maxPageSize)
		}
		limit = parsed
	}
	return cursor, afterID, limit, nil
}

func parseIncidentFilters(request *http.Request) (status, severity, service string, err error) {
	if request == nil {
		return "", "", "", fmt.Errorf("%w: request is required", ErrInvalidArgument)
	}
	values := request.URL.Query()
	status = strings.TrimSpace(values.Get("status"))
	severity = strings.TrimSpace(values.Get("severity"))
	service = strings.TrimSpace(values.Get("service"))
	if status != "" && !validIncidentStatus(status) {
		return "", "", "", fmt.Errorf("%w: invalid Incident status filter", ErrInvalidArgument)
	}
	if severity != "" && !validSeverity(severity) {
		return "", "", "", fmt.Errorf("%w: invalid Incident severity filter", ErrInvalidArgument)
	}
	if len(service) > 255 || containsControl(service) {
		return "", "", "", fmt.Errorf("%w: invalid service filter", ErrInvalidArgument)
	}
	return status, severity, service, nil
}

func validateIdempotencyKey(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%w: Idempotency-Key is required", ErrInvalidArgument)
	}
	if value != strings.TrimSpace(value) || len(value) > maxHeaderBytes || containsControl(value) {
		return "", fmt.Errorf("%w: Idempotency-Key is invalid", ErrInvalidArgument)
	}
	return value, nil
}

func validateExpectedHash(value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("%w: expected_hash must be a lowercase SHA-256 hex digest", ErrInvalidArgument)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return fmt.Errorf("%w: expected_hash must be a lowercase SHA-256 hex digest", ErrInvalidArgument)
	}
	return nil
}

func requireJSON(request *http.Request) error {
	if request == nil {
		return fmt.Errorf("%w: request is required", ErrInvalidArgument)
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != JSONMediaType {
		return fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidArgument)
	}
	return nil
}

func canonicalPayload(value any) (json.RawMessage, string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:]), nil
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
