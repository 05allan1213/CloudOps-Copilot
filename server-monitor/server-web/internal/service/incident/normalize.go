// Package incident implements Incident application use cases and protocol normalization.
package incident

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	domain "server-web/internal/incident"
	"server-web/internal/infra/webhook"
)

const (
	unknownValue          = "unknown"
	maxAlertsPerRequest   = 100
	maxMapEntries         = 64
	maxMapValueBytes      = 1024
	maxLabelsJSONBytes    = 16 * 1024
	maxAnnotationsBytes   = 16 * 1024
	maxRawPayloadBytes    = 32 * 1024
	maxSummaryBytes       = 2048
	maxDimensionBytes     = 255
	idempotencyVersion    = "v1"
	correlationKeyVersion = "v1"
)

var sensitiveFragments = []string{
	"authorization", "password", "passwd", "secret", "token", "cookie", "api_key", "apikey", "kubeconfig", "credential",
}

// NormalizedSignal combines the persisted signal with its deterministic aggregation key.
type NormalizedSignal struct {
	Signal         domain.Signal
	CorrelationKey string
}

// NormalizeAlertmanager converts a protocol payload into bounded domain signals.
func NormalizeAlertmanager(payload webhook.AlertmanagerWebhookRequest, receivedAt time.Time) ([]NormalizedSignal, error) {
	if receivedAt.IsZero() {
		return nil, fmt.Errorf("%w: received time is required", domain.ErrInvalidArgument)
	}
	if len(payload.Alerts) == 0 {
		return nil, fmt.Errorf("%w: alerts must not be empty", domain.ErrInvalidArgument)
	}
	if len(payload.Alerts) > maxAlertsPerRequest {
		return nil, fmt.Errorf("%w: at most %d alerts are accepted", domain.ErrInvalidArgument, maxAlertsPerRequest)
	}

	result := make([]NormalizedSignal, 0, len(payload.Alerts))
	for index, alert := range payload.Alerts {
		normalized, err := normalizeAlert(alert, receivedAt.UTC())
		if err != nil {
			return nil, fmt.Errorf("alert %d: %w", index, err)
		}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeAlert(alert webhook.AlertRecord, receivedAt time.Time) (NormalizedSignal, error) {
	fingerprint := bounded(strings.TrimSpace(alert.Fingerprint), 128)
	if fingerprint == "" {
		return NormalizedSignal{}, fmt.Errorf("%w: fingerprint is required", domain.ErrInvalidArgument)
	}
	status := domain.SignalStatus(strings.ToLower(strings.TrimSpace(alert.Status)))
	if status != domain.SignalStatusFiring && status != domain.SignalStatusResolved {
		return NormalizedSignal{}, fmt.Errorf("%w: unsupported status %q", domain.ErrInvalidArgument, alert.Status)
	}
	if alert.StartsAt.IsZero() {
		return NormalizedSignal{}, fmt.Errorf("%w: startsAt is required", domain.ErrInvalidArgument)
	}
	if status == domain.SignalStatusResolved && alert.EndsAt.IsZero() {
		return NormalizedSignal{}, fmt.Errorf("%w: endsAt is required for resolved alerts", domain.ErrInvalidArgument)
	}

	labels := sanitizeMap(alert.Labels)
	annotations := sanitizeMap(alert.Annotations)
	labelsJSON, err := marshalBounded(labels, maxLabelsJSONBytes, "labels")
	if err != nil {
		return NormalizedSignal{}, err
	}
	annotationsJSON, err := marshalBounded(annotations, maxAnnotationsBytes, "annotations")
	if err != nil {
		return NormalizedSignal{}, err
	}

	cluster := dimension(labels, "cluster", "cluster_name")
	environment := dimension(labels, "environment", "env")
	namespace := dimension(labels, "namespace")
	serviceName := dimension(labels, "service", "service_name", "job")
	targetKind, targetName := target(labels)
	if serviceName == unknownValue && targetName != unknownValue {
		serviceName = targetName
	}
	category := dimension(labels, "alertname")
	severity := domain.NormalizeSeverity(labels["severity"])
	occurredAt := alert.StartsAt.UTC()
	if status == domain.SignalStatusResolved {
		occurredAt = alert.EndsAt.UTC()
	}

	sourceEventID := hashKey(
		idempotencyVersion, "alertmanager", fingerprint,
		alert.StartsAt.UTC().Format(time.RFC3339Nano), string(status),
	)
	correlationKey := hashKey(
		correlationKeyVersion, cluster, environment, namespace, serviceName, category,
	)
	summary := bounded(strings.TrimSpace(annotations["summary"]), maxSummaryBytes)
	raw, err := marshalBounded(map[string]any{
		"status": status, "fingerprint": fingerprint, "starts_at": alert.StartsAt.UTC(),
		"ends_at": alert.EndsAt.UTC(), "labels": labels, "annotations": annotations,
	}, maxRawPayloadBytes, "raw payload")
	if err != nil {
		return NormalizedSignal{}, err
	}

	return NormalizedSignal{
		CorrelationKey: correlationKey,
		Signal: domain.Signal{
			Source: "alertmanager", SourceEventID: sourceEventID, Fingerprint: fingerprint,
			Status: status, Severity: severity, Cluster: cluster, Namespace: namespace,
			ServiceName: serviceName, Environment: environment, TargetKind: targetKind,
			TargetName: targetName, Category: category, OccurredAt: occurredAt,
			ReceivedAt: receivedAt, Summary: summary, Labels: labelsJSON,
			Annotations: annotationsJSON, RawPayload: raw,
		},
	}, nil
}

func target(labels map[string]string) (string, string) {
	for _, item := range []struct{ kind, key string }{
		{"deployment", "deployment"}, {"statefulset", "statefulset"},
		{"daemonset", "daemonset"}, {"workload", "workload"},
		{"pod", "pod"}, {"instance", "instance"},
	} {
		if value := bounded(strings.TrimSpace(labels[item.key]), maxDimensionBytes); value != "" {
			return item.kind, value
		}
	}
	return unknownValue, unknownValue
}

func dimension(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := bounded(strings.TrimSpace(values[key]), maxDimensionBytes); value != "" {
			return strings.ToLower(value)
		}
	}
	return unknownValue
}

func sanitizeMap(values map[string]string) map[string]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	clean := make(map[string]string, min(len(keys), maxMapEntries))
	for _, key := range keys {
		if len(clean) == maxMapEntries {
			break
		}
		normalizedKey := bounded(strings.TrimSpace(key), 128)
		if normalizedKey == "" || sensitiveKey(normalizedKey) {
			continue
		}
		clean[normalizedKey] = bounded(values[key], maxMapValueBytes)
	}
	return clean
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range sensitiveFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func marshalBounded(value any, limit int, name string) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal %s: %v", domain.ErrInvalidArgument, name, err)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", domain.ErrInvalidArgument, name, limit)
	}
	return data, nil
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for len(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func hashKey(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return parts[0] + ":" + hex.EncodeToString(hash[:])
}
