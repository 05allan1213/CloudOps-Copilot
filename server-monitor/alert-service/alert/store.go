package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"alert-service/kafka"
)

const (
	RedisActiveAlertsKey = "alert:active"
	// RedisStatsKey stores cumulative firing counts by alert name. Resolved
	// events only clear active state; they do not decrement these counters.
	RedisStatsKey   = "alert:stats"
	DefaultDedupTTL = 5 * time.Minute
)

type RedisClient interface {
	ApplyFiringEvent(ctx context.Context, dedupKey string, ttl time.Duration, fingerprint string, payload []byte, statsField string) (bool, error)
	ApplyResolvedEvent(ctx context.Context, dedupKey string, ttl time.Duration, fingerprint string) (bool, error)
}

type StoreObserver interface {
	ObserveAlertEvent(status, result string)
}

const (
	EventStored  = "stored"
	EventDeduped = "deduped"
	EventFailed  = "failed"
)

type Store struct {
	client   RedisClient
	dedupTTL time.Duration
	observer StoreObserver
}

var _ kafka.AlertProcessor = (*Store)(nil)

func NewStore(client RedisClient, dedupTTL time.Duration, observers ...StoreObserver) *Store {
	if dedupTTL <= 0 {
		dedupTTL = DefaultDedupTTL
	}
	store := &Store{
		client:   client,
		dedupTTL: dedupTTL,
	}
	if len(observers) > 0 {
		store.observer = observers[0]
	}
	return store
}

func (s *Store) Process(ctx context.Context, event kafka.AlertEvent) error {
	if s == nil {
		return errors.New("alert store is nil")
	}
	if s.client == nil {
		return errors.New("redis client is required")
	}
	if err := validateEvent(event); err != nil {
		s.observe(event.Status, EventFailed)
		return kafka.Permanent(err)
	}

	dedupKey := DedupKey(event)
	switch event.Status {
	case StatusFiring:
		payload, err := json.Marshal(event)
		if err != nil {
			s.observe(event.Status, EventFailed)
			return fmt.Errorf("marshal active alert: %w", err)
		}
		stored, err := s.client.ApplyFiringEvent(ctx, dedupKey, s.dedupTTL, event.Fingerprint, payload, alertNameOrFallback(event))
		if err != nil {
			s.observe(event.Status, EventFailed)
			return err
		}
		if !stored {
			s.observe(event.Status, EventDeduped)
			return nil
		}
	case StatusResolved:
		stored, err := s.client.ApplyResolvedEvent(ctx, dedupKey, s.dedupTTL, event.Fingerprint)
		if err != nil {
			s.observe(event.Status, EventFailed)
			return err
		}
		if !stored {
			s.observe(event.Status, EventDeduped)
			return nil
		}
	}

	s.observe(event.Status, EventStored)
	return nil
}

func (s *Store) observe(status, result string) {
	if s == nil || s.observer == nil {
		return
	}
	s.observer.ObserveAlertEvent(normalizeStatus(status), result)
}

func normalizeStatus(status string) string {
	switch status {
	case StatusFiring, StatusResolved:
		return status
	default:
		return "unknown"
	}
}
