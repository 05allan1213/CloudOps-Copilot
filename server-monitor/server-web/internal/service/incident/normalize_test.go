package incident

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domain "server-web/internal/incident"
	"server-web/internal/infra/webhook"
)

func TestNormalizeAlertmanagerDeterministicKeysAndRedaction(t *testing.T) {
	start := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	received := start.Add(time.Minute)
	payload := webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{{
		Status: "firing", Fingerprint: "abc", StartsAt: start,
		Labels:      map[string]string{"alertname": "HighErrorRate", "severity": "CRITICAL", "cluster": "Prod-A", "namespace": "Payments", "service": "Checkout", "token": "do-not-store"},
		Annotations: map[string]string{"summary": "checkout errors", "authorization": "secret"},
	}}}
	first, err := NormalizeAlertmanager(payload, received)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeAlertmanager(payload, received.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Signal.SourceEventID != second[0].Signal.SourceEventID || first[0].CorrelationKey != second[0].CorrelationKey {
		t.Fatal("keys changed across delivery time")
	}
	if first[0].Signal.Severity != domain.SeverityCritical || first[0].Signal.ServiceName != "checkout" {
		t.Fatalf("unexpected normalized signal: %+v", first[0].Signal)
	}
	var labels map[string]string
	if err := json.Unmarshal(first[0].Signal.Labels, &labels); err != nil {
		t.Fatal(err)
	}
	if _, ok := labels["token"]; ok || strings.Contains(string(first[0].Signal.RawPayload), "do-not-store") {
		t.Fatal("sensitive value was persisted")
	}
}

func TestIdempotencyKeyIgnoresResolvedEndTime(t *testing.T) {
	start := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	base := webhook.AlertRecord{Status: "resolved", Fingerprint: "abc", StartsAt: start, EndsAt: start.Add(time.Hour), Labels: map[string]string{"alertname": "Down"}}
	one, err := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{base}}, start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	base.EndsAt = base.EndsAt.Add(time.Minute)
	two, err := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{base}}, start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if one[0].Signal.SourceEventID != two[0].Signal.SourceEventID {
		t.Fatal("resolved source event ID unexpectedly includes endsAt")
	}
}

func TestCorrelationKeyChangesWithStableDimensions(t *testing.T) {
	start := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	record := webhook.AlertRecord{Status: "firing", Fingerprint: "a", StartsAt: start, Labels: map[string]string{"alertname": "Down", "cluster": "a", "namespace": "n", "service": "s"}}
	one, _ := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{record}}, start)
	record.Fingerprint = "b"
	two, _ := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{record}}, start)
	if one[0].CorrelationKey != two[0].CorrelationKey {
		t.Fatal("fingerprint must not affect correlation key")
	}
	record.Labels["service"] = "other"
	three, _ := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{record}}, start)
	if one[0].CorrelationKey == three[0].CorrelationKey {
		t.Fatal("service must affect correlation key")
	}
}

func TestNormalizeRejectsInvalidSignalsAndLimits(t *testing.T) {
	now := time.Now().UTC()
	tests := []webhook.AlertRecord{
		{Status: "firing", StartsAt: now},
		{Status: "unknown", Fingerprint: "x", StartsAt: now},
		{Status: "firing", Fingerprint: "x"},
		{Status: "resolved", Fingerprint: "x", StartsAt: now},
	}
	for _, record := range tests {
		_, err := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{record}}, now)
		if !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("expected invalid argument for %+v, got %v", record, err)
		}
	}
	oversized := webhook.AlertRecord{Status: "firing", Fingerprint: "x", StartsAt: now, Labels: map[string]string{"alertname": "x", "value": strings.Repeat("x", maxLabelsJSONBytes)}}
	_, err := NormalizeAlertmanager(webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{oversized}}, now)
	if err != nil {
		t.Fatalf("individual values should be safely truncated: %v", err)
	}
}
