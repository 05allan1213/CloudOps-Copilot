package alertmanageringress

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentv3mysql"
)

const (
	canonicalIdentityVersion = "2"
	alertmanagerSource       = "alertmanager"
	maxAlerts                = 100
	maxMapEntries            = 64
	maxLabelKeyBytes         = 128
	maxLabelValueBytes       = 1024
	maxSummaryBytes          = 2048
)

var (
	privateKeyPattern  = regexp.MustCompile(`(?is)-----BEGIN [^-\r\n]{0,64}PRIVATE KEY-----.*?-----END [^-\r\n]{0,64}PRIVATE KEY-----`)
	bearerPattern      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{8,}`)
	assignmentPattern  = regexp.MustCompile(`(?i)\b(authorization|api[_-]?key|token|password|secret)\b\s*[:=]\s*["']?[^\s,"';]+`)
	highEntropyPattern = regexp.MustCompile(`[A-Za-z0-9_+/=-]{24,}`)
)

type envelope struct {
	Version            string            `json:"version"`
	GroupKey           string            `json:"groupKey"`
	TruncatedAlerts    int               `json:"truncatedAlerts"`
	Status             string            `json:"status"`
	Receiver           string            `json:"receiver"`
	NotificationReason string            `json:"notification_reason"`
	GroupLabels        map[string]string `json:"groupLabels"`
	CommonLabels       map[string]string `json:"commonLabels"`
	CommonAnnotations  map[string]string `json:"commonAnnotations"`
	ExternalURL        string            `json:"externalURL"`
	Alerts             []alert           `json:"alerts"`
}

type alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type normalizedBatch struct {
	Signals    []incidentv3mysql.SignalInput
	Rejections []incidentv3mysql.RejectionInput
}

func decodeEnvelope(reader io.Reader) (envelope, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return envelope{}, err
	}
	if !utf8.Valid(contents) {
		return envelope{}, errors.New("webhook body must be valid UTF-8")
	}
	if err := validateUniqueJSONKeys(contents); err != nil {
		return envelope{}, err
	}
	if err := validateExactEnvelopeFields(contents); err != nil {
		return envelope{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var result envelope
	if err := decoder.Decode(&result); err != nil {
		return envelope{}, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return envelope{}, err
	}
	return result, nil
}

func validateExactEnvelopeFields(contents []byte) error {
	var rawEnvelope map[string]json.RawMessage
	if err := json.Unmarshal(contents, &rawEnvelope); err != nil {
		return err
	}
	envelopeFields := map[string]struct{}{
		"version": {}, "groupKey": {}, "truncatedAlerts": {}, "status": {}, "receiver": {},
		"notification_reason": {},
		"groupLabels":         {}, "commonLabels": {}, "commonAnnotations": {}, "externalURL": {}, "alerts": {},
	}
	for field := range rawEnvelope {
		if _, allowed := envelopeFields[field]; !allowed {
			return fmt.Errorf("unknown Alertmanager envelope field %q", field)
		}
	}
	rawAlerts, exists := rawEnvelope["alerts"]
	if !exists {
		return nil
	}
	var alerts []map[string]json.RawMessage
	if err := json.Unmarshal(rawAlerts, &alerts); err != nil {
		return err
	}
	alertFields := map[string]struct{}{
		"status": {}, "labels": {}, "annotations": {}, "startsAt": {},
		"endsAt": {}, "generatorURL": {}, "fingerprint": {},
	}
	for index, rawAlert := range alerts {
		for field := range rawAlert {
			if _, allowed := alertFields[field]; !allowed {
				return fmt.Errorf("unknown Alertmanager alert %d field %q", index, field)
			}
		}
	}
	return nil
}

func validateUniqueJSONKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			keys[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("invalid JSON object closing delimiter")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("invalid JSON array closing delimiter")
		}
	default:
		return errors.New("invalid JSON opening delimiter")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON documents are not allowed")
	}
	return err
}

func normalizeEnvelope(input envelope, targets []Target) (normalizedBatch, error) {
	if len(input.Alerts) == 0 || len(input.Alerts) > maxAlerts {
		return normalizedBatch{}, fmt.Errorf("alerts must contain 1..%d entries", maxAlerts)
	}
	if input.TruncatedAlerts != 0 {
		return normalizedBatch{}, errors.New("truncated Alertmanager envelopes are not accepted")
	}
	if input.Status != "firing" && input.Status != "resolved" {
		return normalizedBatch{}, errors.New("envelope status must be firing or resolved")
	}
	if input.Version != "4" {
		return normalizedBatch{}, errors.New("envelope version must be 4")
	}
	if strings.TrimSpace(input.Receiver) == "" || len(input.Receiver) > 128 {
		return normalizedBatch{}, errors.New("envelope receiver must contain 1..128 bytes")
	}
	for name, value := range map[string]string{
		"groupKey":    input.GroupKey,
		"externalURL": input.ExternalURL,
	} {
		if len(value) > 2048 || hasControl(value) {
			return normalizedBatch{}, fmt.Errorf("%s exceeds its safe bound", name)
		}
	}
	for name, values := range map[string]map[string]string{
		"groupLabels": input.GroupLabels, "commonLabels": input.CommonLabels,
		"commonAnnotations": input.CommonAnnotations,
	} {
		if err := validateStringMap(values); err != nil {
			return normalizedBatch{}, fmt.Errorf("%s: %w", name, err)
		}
	}

	result := normalizedBatch{Signals: make([]incidentv3mysql.SignalInput, 0, len(input.Alerts))}
	for index, item := range input.Alerts {
		normalized, rejection, err := normalizeAlert(item, targets)
		if err != nil {
			return normalizedBatch{}, fmt.Errorf("alert %d: %w", index, err)
		}
		if rejection != nil {
			result.Rejections = append(result.Rejections, *rejection)
			continue
		}
		result.Signals = append(result.Signals, normalized)
	}
	return result, nil
}

func normalizeAlert(input alert, targets []Target) (incidentv3mysql.SignalInput, *incidentv3mysql.RejectionInput, error) {
	status := domain.SignalStatus(strings.ToLower(strings.TrimSpace(input.Status)))
	if status != domain.SignalStatusFiring && status != domain.SignalStatusResolved {
		return incidentv3mysql.SignalInput{}, nil, errors.New("status must be firing or resolved")
	}
	if input.StartsAt.IsZero() {
		return incidentv3mysql.SignalInput{}, nil, errors.New("startsAt is required")
	}
	startsAt := input.StartsAt.UTC()
	var endsAt *time.Time
	resolvedEndsAt := ""
	if status == domain.SignalStatusResolved {
		if input.EndsAt.IsZero() || input.EndsAt.Before(input.StartsAt) {
			return incidentv3mysql.SignalInput{}, nil, errors.New("resolved alert requires endsAt at or after startsAt")
		}
		resolved := input.EndsAt.UTC()
		endsAt = &resolved
		resolvedEndsAt = canonicalTime(resolved)
	}
	fingerprint := strings.ToLower(strings.TrimSpace(input.Fingerprint))
	if fingerprint == "" || len(fingerprint) > 128 {
		return incidentv3mysql.SignalInput{}, nil, errors.New("fingerprint must contain 1..128 hex characters")
	}
	if _, err := hex.DecodeString(evenHex(fingerprint)); err != nil {
		return incidentv3mysql.SignalInput{}, nil, errors.New("fingerprint must be hexadecimal")
	}
	if err := validateStringMap(input.Labels); err != nil {
		return incidentv3mysql.SignalInput{}, nil, fmt.Errorf("labels: %w", err)
	}
	if err := validateStringMap(input.Annotations); err != nil {
		return incidentv3mysql.SignalInput{}, nil, fmt.Errorf("annotations: %w", err)
	}

	sourceEventID := "v2:" + hashCanonical(
		canonicalIdentityVersion, alertmanagerSource, fingerprint, canonicalTime(startsAt), string(status), resolvedEndsAt,
	)
	instanceKey := hashCanonical(canonicalIdentityVersion, alertmanagerSource, fingerprint, canonicalTime(startsAt))
	target, reason := resolveTarget(input.Labels, targets)
	if reason != "" {
		labelsHash := hashStringMap(input.Labels)
		return incidentv3mysql.SignalInput{}, &incidentv3mysql.RejectionInput{
			Source: alertmanagerSource, SourceEventID: sourceEventID, Fingerprint: fingerprint,
			AlertInstanceKey: instanceKey, ReasonCode: reason,
			Details: map[string]string{"labels_hash": labelsHash, "status": string(status)},
		}, nil
	}

	correlationKey := "v2:" + hashCanonical(
		target.ClusterID, target.Environment, target.Namespace, target.WorkloadKind, target.WorkloadName,
	)
	severity := domain.NormalizeSeverity(strings.ToLower(strings.TrimSpace(input.Labels["severity"])))
	if severity == domain.SeverityUnknown {
		severity = domain.SeverityInfo
	}
	occurredAt := startsAt
	if endsAt != nil {
		occurredAt = *endsAt
	}
	labelsJSON, err := json.Marshal(safeLabels(input.Labels))
	if err != nil {
		return incidentv3mysql.SignalInput{}, nil, err
	}
	annotationsJSON, err := json.Marshal(safeAnnotations(input.Annotations))
	if err != nil {
		return incidentv3mysql.SignalInput{}, nil, err
	}
	alertName := redactExternalText(input.Labels["alertname"], 128)
	if alertName == "" {
		alertName = "alertmanager"
	}
	summary := redactExternalText(input.Annotations["summary"], maxSummaryBytes)
	if summary == "" {
		summary = bounded(alertName+" "+string(status), maxSummaryBytes)
	}
	return incidentv3mysql.SignalInput{
		Source: alertmanagerSource, SourceEventID: sourceEventID, AlertInstanceKey: instanceKey,
		CorrelationKey: correlationKey, Fingerprint: fingerprint, Status: status, Severity: severity,
		Cluster: target.ClusterID, Environment: target.Environment, Namespace: target.Namespace,
		ServiceName: target.ServiceName, TargetKind: target.WorkloadKind, TargetName: target.WorkloadName,
		Category: alertName, StartsAt: startsAt, EndsAt: endsAt, OccurredAt: occurredAt,
		Summary: summary, Labels: labelsJSON, Annotations: annotationsJSON,
	}, nil, nil
}

func resolveTarget(labels map[string]string, targets []Target) (Target, string) {
	matches := make([]Target, 0, 1)
	for _, target := range targets {
		matched := true
		for key, want := range target.MatchLabels {
			if strings.TrimSpace(labels[key]) != want {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, target)
		}
	}
	if len(matches) == 0 {
		return Target{}, "target_not_allowlisted"
	}
	if len(matches) != 1 {
		return Target{}, "target_selector_ambiguous"
	}
	target := matches[0]
	checks := []struct {
		keys []string
		want string
	}{
		{[]string{"cluster", "cluster_id", "cluster_name"}, target.ClusterID},
		{[]string{"environment", "env"}, target.Environment},
		{[]string{"namespace"}, target.Namespace},
		{[]string{"service", "service_name"}, target.ServiceName},
		{[]string{"workload_kind", "target_kind"}, target.WorkloadKind},
		{[]string{"workload", "workload_name", "target_name", "deployment", "statefulset", "daemonset"}, target.WorkloadName},
	}
	for _, check := range checks {
		for _, key := range check.keys {
			if value := strings.TrimSpace(labels[key]); value != "" && value != check.want {
				return Target{}, "target_label_conflict"
			}
		}
	}
	return target, ""
}

func validateStringMap(values map[string]string) error {
	if len(values) > maxMapEntries {
		return fmt.Errorf("map contains more than %d entries", maxMapEntries)
	}
	for key, value := range values {
		if key == "" || len(key) > maxLabelKeyBytes || len(value) > maxLabelValueBytes || hasControl(key) {
			return errors.New("map contains an invalid or oversized key/value")
		}
	}
	return nil
}

func safeLabels(labels map[string]string) map[string]string {
	return selectMap(labels, []string{
		"alertname", "severity", "cluster", "cluster_id", "cluster_name", "environment", "env",
		"namespace", "service", "service_name", "workload_kind", "target_kind", "workload", "workload_name",
		"target_name", "deployment", "statefulset", "daemonset",
	})
}

func safeAnnotations(annotations map[string]string) map[string]string {
	return selectMap(annotations, []string{"summary", "description"})
}

func selectMap(source map[string]string, keys []string) map[string]string {
	result := make(map[string]string)
	for _, key := range keys {
		if value := redactExternalText(source[key], maxLabelValueBytes); value != "" {
			result[key] = value
		}
	}
	return result
}

func redactExternalText(value string, maximum int) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = privateKeyPattern.ReplaceAllString(cleaned, "[REDACTED_PRIVATE_KEY]")
	cleaned = bearerPattern.ReplaceAllString(cleaned, "Bearer [REDACTED]")
	cleaned = assignmentPattern.ReplaceAllString(cleaned, "$1=[REDACTED]")
	cleaned = highEntropyPattern.ReplaceAllStringFunc(cleaned, func(candidate string) string {
		if looksHighEntropy(candidate) {
			return "[REDACTED_HIGH_ENTROPY]"
		}
		return candidate
	})
	return bounded(strings.TrimSpace(cleaned), maximum)
}

func looksHighEntropy(value string) bool {
	if len(value) < 24 {
		return false
	}
	var frequencies [256]int
	distinct := 0
	var lower, upper, digit, symbol bool
	for index := 0; index < len(value); index++ {
		byteValue := value[index]
		if frequencies[byteValue] == 0 {
			distinct++
		}
		frequencies[byteValue]++
		r := rune(byteValue)
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	entropy := 0.0
	for _, count := range frequencies {
		if count == 0 {
			continue
		}
		probability := float64(count) / float64(len(value))
		entropy -= probability * math.Log2(probability)
	}
	classes := 0
	for _, present := range []bool{lower, upper, digit, symbol} {
		if present {
			classes++
		}
	}
	if distinct < 12 {
		return false
	}
	return entropy >= 4.2 || (classes >= 3 && entropy >= 3.5) || (len(value) >= 40 && entropy >= 3.75)
}

func hashStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		parts = append(parts, key, values[key])
	}
	return hashCanonical(parts...)
}

func hashCanonical(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func canonicalTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func bounded(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func evenHex(value string) string {
	if len(value)%2 == 0 {
		return value
	}
	return "0" + value
}
