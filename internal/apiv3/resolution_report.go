package apiv3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxResolutionTriggerJSONBytes      = 8192
	maxResolutionDiagnosisJSONBytes    = 16384
	maxResolutionEvidenceJSONBytes     = 32768
	maxResolutionPlanJSONBytes         = 16384
	maxResolutionDecisionJSONBytes     = 8192
	maxResolutionDeliveryJSONBytes     = 16384
	maxResolutionVerificationJSONBytes = 32768
	maxResolutionTimelineJSONBytes     = 32768
	maxResolutionAgentUsageJSONBytes   = 8192
	maxResolutionJSONDepth             = 64
)

func validateResolutionReportView(item *ResolutionReportView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil {
		return err
	}
	if item.Kind != string(QueryResolutionReport) || item.Status != "resolved" || item.Cycle == 0 {
		return fmt.Errorf("%w: invalid ResolutionReport identity", ErrInvalidArgument)
	}
	if !validResolutionText(item.Service, 255, true) ||
		!validResolutionText(item.Workload, 255, true) ||
		!validResolutionText(item.Environment, 255, true) ||
		!validResolutionText(item.ImpactSummary, 2048, false) ||
		!validResolutionText(item.Summary, 2048, true) {
		return fmt.Errorf("%w: invalid ResolutionReport text", ErrInvalidArgument)
	}
	if validateExpectedHash(item.Hash) != nil || validateExpectedHash(item.VerificationProfile.Hash) != nil ||
		!validResolutionRevision(item.Revisions.SourceRevision) ||
		!validResolutionRevision(item.Revisions.GitOpsRevision) ||
		!validResolutionImageDigest(item.Revisions.ImageDigest) {
		return fmt.Errorf("%w: invalid ResolutionReport revision or hash", ErrInvalidArgument)
	}
	if item.CycleStartedAt.IsZero() || item.ResolvedAt.IsZero() || item.GeneratedAt.IsZero() ||
		item.Stability.CommonWindowStartedAt.IsZero() || item.Stability.CommonWindowCompletedAt.IsZero() ||
		item.ResolvedAt.Before(item.CycleStartedAt) || item.GeneratedAt.Before(item.ResolvedAt) ||
		item.Stability.CommonWindowStartedAt.Before(item.CycleStartedAt) ||
		item.Stability.CommonWindowCompletedAt.Before(item.Stability.CommonWindowStartedAt) ||
		item.Stability.CommonWindowCompletedAt.Sub(item.Stability.CommonWindowStartedAt) < time.Minute ||
		item.Stability.CommonWindowCompletedAt.After(item.ResolvedAt) {
		return fmt.Errorf("%w: invalid ResolutionReport time window", ErrInvalidArgument)
	}
	if item.MeasuredDurationMS != uint64(item.ResolvedAt.Sub(item.CycleStartedAt).Milliseconds()) {
		return fmt.Errorf("%w: invalid ResolutionReport measured duration", ErrInvalidArgument)
	}

	item.TriggerSignal, err = projectResolutionJSON(item.TriggerSignal, maxResolutionTriggerJSONBytes, true)
	if err != nil {
		return err
	}
	item.Diagnosis, err = projectResolutionJSON(item.Diagnosis, maxResolutionDiagnosisJSONBytes, false)
	if err != nil {
		return err
	}
	item.Evidence, err = projectResolutionJSON(item.Evidence, maxResolutionEvidenceJSONBytes, true)
	if err != nil {
		return err
	}
	item.RemediationPlan, err = projectResolutionJSON(item.RemediationPlan, maxResolutionPlanJSONBytes, false)
	if err != nil {
		return err
	}
	item.RemediationDecision, err = projectResolutionJSON(item.RemediationDecision, maxResolutionDecisionJSONBytes, false)
	if err != nil {
		return err
	}
	item.Delivery, err = projectResolutionJSON(item.Delivery, maxResolutionDeliveryJSONBytes, false)
	if err != nil {
		return err
	}
	item.Verification, err = projectResolutionJSON(item.Verification, maxResolutionVerificationJSONBytes, true)
	if err != nil {
		return err
	}
	item.Timeline, err = projectResolutionJSON(item.Timeline, maxResolutionTimelineJSONBytes, true)
	if err != nil {
		return err
	}
	item.AgentUsage, err = projectResolutionJSON(item.AgentUsage, maxResolutionAgentUsageJSONBytes, true)
	if err != nil {
		return err
	}
	if err := validateResolutionPath(item); err != nil {
		return err
	}

	item.ID = id
	item.CycleStartedAt = item.CycleStartedAt.UTC()
	item.ResolvedAt = item.ResolvedAt.UTC()
	item.GeneratedAt = item.GeneratedAt.UTC()
	item.Stability.CommonWindowStartedAt = item.Stability.CommonWindowStartedAt.UTC()
	item.Stability.CommonWindowCompletedAt = item.Stability.CommonWindowCompletedAt.UTC()
	return nil
}

func validateResolutionPath(item *ResolutionReportView) error {
	switch item.TriggerType {
	case "post_delivery":
		if item.ResolutionReason != "recovered_after_change" && item.ResolutionReason != "recovered_after_remediation" {
			return fmt.Errorf("%w: invalid post-delivery resolution reason", ErrInvalidArgument)
		}
		if item.VerificationProfile.ID != "golden-required-env/v1" ||
			!validResolutionRevision(item.Revisions.BadGitOpsRevision) ||
			!validResolutionRevision(item.Revisions.FixGitOpsRevision) ||
			len(item.Diagnosis) == 0 || len(item.RemediationPlan) == 0 ||
			len(item.RemediationDecision) == 0 || len(item.Delivery) == 0 {
			return fmt.Errorf("%w: incomplete post-delivery ResolutionReport", ErrInvalidArgument)
		}
	case "no_change_signal":
		if item.VerificationProfile.ID != "no-change/v1" ||
			item.Revisions.BadGitOpsRevision != "" || item.Revisions.FixGitOpsRevision != "" ||
			len(item.RemediationPlan) != 0 || len(item.RemediationDecision) != 0 || len(item.Delivery) != 0 {
			return fmt.Errorf("%w: invalid no-change ResolutionReport path", ErrInvalidArgument)
		}
		switch item.ResolutionReason {
		case "recovered_before_diagnosis", "recovered_without_change":
		default:
			return fmt.Errorf("%w: invalid no-change resolution reason", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: invalid ResolutionReport trigger type", ErrInvalidArgument)
	}
	return nil
}

func validResolutionText(value string, maxBytes int, required bool) bool {
	if required && value == "" {
		return false
	}
	return len(value) <= maxBytes && utf8.ValidString(value) && !containsControl(value)
}

func validResolutionRevision(value string) bool {
	if len(value) < 40 || len(value) > 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			if value[index] < 'a' || value[index] > 'f' {
				return false
			}
		}
	}
	return true
}

func validResolutionImageDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && len(value) == 71 && validateExpectedHash(strings.TrimPrefix(value, "sha256:")) == nil
}

func projectResolutionJSON(raw json.RawMessage, maxBytes int, required bool) (json.RawMessage, error) {
	if len(raw) == 0 {
		if required {
			return nil, fmt.Errorf("%w: required ResolutionReport JSON is missing", ErrInvalidArgument)
		}
		return nil, nil
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("%w: ResolutionReport JSON exceeds its bound", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: malformed ResolutionReport JSON", ErrInvalidArgument)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: malformed ResolutionReport JSON", ErrInvalidArgument)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: ResolutionReport JSON must be an object", ErrInvalidArgument)
	}
	projected, err := sanitizeResolutionJSONValue(root, 0)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(projected)
	if err != nil || len(encoded) > maxBytes {
		return nil, fmt.Errorf("%w: invalid bounded ResolutionReport JSON", ErrInvalidArgument)
	}
	return json.RawMessage(encoded), nil
}

func sanitizeResolutionJSONValue(value any, depth int) (any, error) {
	if depth > maxResolutionJSONDepth {
		return nil, fmt.Errorf("%w: ResolutionReport JSON nesting exceeds its bound", ErrInvalidArgument)
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized := normalizeResolutionJSONKey(key)
			if forbiddenResolutionJSONKey(normalized) {
				return nil, fmt.Errorf("%w: forbidden ResolutionReport JSON field %q", ErrInvalidArgument, key)
			}
			if normalized == "check_id" {
				switch identity := child.(type) {
				case json.Number, nil:
					// The durable writer currently records the internal numeric
					// verification-check key beside each public sample UUID. It
					// is intentionally omitted from the public projection.
					continue
				case string:
					id, err := ParsePublicUUID(identity)
					if err != nil {
						return nil, fmt.Errorf("%w: invalid public check identity", ErrInvalidArgument)
					}
					result[key] = id
					continue
				default:
					return nil, fmt.Errorf("%w: invalid check identity", ErrInvalidArgument)
				}
			}
			if publicResolutionJSONIDKey(normalized) {
				identity, ok := child.(string)
				if !ok {
					return nil, fmt.Errorf("%w: numeric or malformed public identity", ErrInvalidArgument)
				}
				id, err := ParsePublicUUID(identity)
				if err != nil {
					return nil, fmt.Errorf("%w: invalid public identity", ErrInvalidArgument)
				}
				result[key] = id
				continue
			}
			if strings.HasSuffix(normalized, "_id") {
				if _, numeric := child.(json.Number); numeric {
					return nil, fmt.Errorf("%w: numeric identity in ResolutionReport JSON", ErrInvalidArgument)
				}
			}
			projected, err := sanitizeResolutionJSONValue(child, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = projected
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			projected, err := sanitizeResolutionJSONValue(typed[index], depth+1)
			if err != nil {
				return nil, err
			}
			result[index] = projected
		}
		return result, nil
	case string, json.Number, bool, nil:
		return typed, nil
	default:
		return nil, fmt.Errorf("%w: unsupported ResolutionReport JSON value", ErrInvalidArgument)
	}
}

func normalizeResolutionJSONKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(value)
}

func forbiddenResolutionJSONKey(key string) bool {
	switch key {
	case "numeric_id", "internal_id", "raw_result", "raw_results", "prompt", "secret", "password",
		"token", "access_token", "refresh_token", "auth_token", "bearer_token", "api_token",
		"private_key", "authorization", "cookie", "set_cookie":
		return true
	}
	return strings.HasPrefix(key, "lease_") || key == "lease" ||
		strings.HasPrefix(key, "checkpoint_") || key == "checkpoint" ||
		strings.HasPrefix(key, "raw_result_") || strings.HasSuffix(key, "_raw_result") ||
		(strings.HasPrefix(key, "prompt_") && key != "prompt_version") || strings.HasSuffix(key, "_prompt") ||
		strings.HasPrefix(key, "secret_") || strings.HasSuffix(key, "_secret") ||
		strings.HasPrefix(key, "password_") || strings.HasSuffix(key, "_password") ||
		strings.HasSuffix(key, "_token") || strings.HasPrefix(key, "private_key_") ||
		strings.HasSuffix(key, "_private_key") || strings.HasSuffix(key, "_numeric_id") ||
		strings.HasSuffix(key, "_internal_id")
}

func publicResolutionJSONIDKey(key string) bool {
	switch key {
	case "id", "public_id", "run_id", "incident_id", "verification_run_id", "creator_agent_run_id":
		return true
	default:
		return false
	}
}
