package operation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var operationIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func decodeExact(raw json.RawMessage, target any) error {
	if len(raw) == 0 || len(raw) > 16384 {
		return ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalidArgument
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrInvalidArgument
	}
	return nil
}

func DecodeExact(raw json.RawMessage, target any) error { return decodeExact(raw, target) }

func validateTarget(target OperationTarget) error {
	values := []string{target.ClusterID, target.Environment, target.Namespace, target.WorkloadKind, target.WorkloadName}
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 253 || !operationIdentityPattern.MatchString(value) {
			return ErrInvalidArgument
		}
	}
	if target.WorkloadKind != "Deployment" {
		return ErrInvalidArgument
	}
	return nil
}

func ValidateTarget(target OperationTarget) error { return validateTarget(target) }

func hashJSON(value any) (string, []byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), encoded, nil
}

func lowerHex64(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
