package alert

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"alert-service/kafka"
)

const (
	StatusFiring   = "firing"
	StatusResolved = "resolved"
)

type Event = kafka.AlertEvent

func validateEvent(event Event) error {
	if event.Fingerprint == "" {
		return errors.New("alert fingerprint is required")
	}
	if event.Status != StatusFiring && event.Status != StatusResolved {
		return fmt.Errorf("unsupported alert status %q", event.Status)
	}
	return nil
}

func DedupKey(event Event) string {
	parts := []string{
		"alert",
		"dedup",
		event.Fingerprint,
		event.Status,
	}
	if !event.StartsAt.IsZero() {
		parts = append(parts, formatDedupTime(event.StartsAt))
	}
	if !event.EndsAt.IsZero() {
		parts = append(parts, formatDedupTime(event.EndsAt))
	}
	return strings.Join(parts, ":")
}

func formatDedupTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func AggregateKey(event Event) string {
	alertName := strings.TrimSpace(event.Labels["alertname"])
	instance := strings.TrimSpace(event.Labels["instance"])
	switch {
	case alertName != "" && instance != "":
		return alertName + ":" + instance
	case alertName != "":
		return alertName
	default:
		return event.Fingerprint
	}
}

func alertNameOrFallback(event Event) string {
	alertName := strings.TrimSpace(event.Labels["alertname"])
	if alertName != "" {
		return alertName
	}
	return event.Fingerprint
}

func receivedAtOrNow(event Event) time.Time {
	if !event.ReceivedAt.IsZero() {
		return event.ReceivedAt
	}
	return time.Now().UTC()
}
