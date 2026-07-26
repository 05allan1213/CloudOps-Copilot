package api

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	errIdempotencyKeyReused = errors.New("idempotency key reused with a different payload")
	errIdempotencyCapacity  = errors.New("idempotency cache capacity is exhausted")
)

const (
	defaultIdempotencyCapacity = 4096
	defaultIdempotencyTTL      = 24 * time.Hour
)

type idempotencyRecord struct {
	payloadHash string
	done        chan struct{}
	response    *storedHTTPResponse
	createdAt   time.Time
	expiresAt   time.Time
}

type idempotencyReservation struct {
	key    string
	record *idempotencyRecord
}

type idempotencyStore struct {
	mu       sync.Mutex
	records  map[string]*idempotencyRecord
	capacity int
	ttl      time.Duration
	now      func() time.Time
}

func newIdempotencyStore(now func() time.Time) *idempotencyStore {
	if now == nil {
		now = time.Now
	}
	return &idempotencyStore{
		records: make(map[string]*idempotencyRecord), capacity: defaultIdempotencyCapacity,
		ttl: defaultIdempotencyTTL, now: now,
	}
}

// reserve serializes concurrent requests for one actor/scope/key. A duplicate
// with the same canonical payload waits for and reuses the original response;
// a different payload fails immediately with 409 at the transport boundary.
func (s *idempotencyStore) reserve(ctx context.Context, key, payloadHash string) (idempotencyReservation, *storedHTTPResponse, error) {
	for {
		s.mu.Lock()
		now := s.now().UTC()
		s.pruneExpiredLocked(now)
		record, ok := s.records[key]
		if !ok {
			if len(s.records) >= s.capacity && !s.evictOldestCompletedLocked() {
				s.mu.Unlock()
				return idempotencyReservation{}, nil, errIdempotencyCapacity
			}
			record = &idempotencyRecord{payloadHash: payloadHash, done: make(chan struct{}), createdAt: now}
			s.records[key] = record
			s.mu.Unlock()
			return idempotencyReservation{key: key, record: record}, nil, nil
		}
		if record.payloadHash != payloadHash {
			s.mu.Unlock()
			return idempotencyReservation{}, nil, errIdempotencyKeyReused
		}
		if record.response != nil {
			response := copyStoredResponse(record.response)
			s.mu.Unlock()
			return idempotencyReservation{}, response, nil
		}
		done := record.done
		s.mu.Unlock()

		select {
		case <-done:
		case <-ctx.Done():
			return idempotencyReservation{}, nil, ctx.Err()
		}
		// A failed 5xx execution removes the reservation. Loop so exactly one
		// waiter retries while all others continue to share its result.
	}
}

func (s *idempotencyStore) complete(reservation idempotencyReservation, response storedHTTPResponse, retain bool) {
	if reservation.record == nil {
		return
	}
	s.mu.Lock()
	current, ok := s.records[reservation.key]
	if !ok || current != reservation.record {
		s.mu.Unlock()
		return
	}
	if retain {
		current.response = copyStoredResponse(&response)
		current.expiresAt = s.now().UTC().Add(s.ttl)
	} else {
		delete(s.records, reservation.key)
	}
	close(current.done)
	s.mu.Unlock()
}

func (s *idempotencyStore) pruneExpiredLocked(now time.Time) {
	for key, record := range s.records {
		if record.response != nil && !record.expiresAt.IsZero() && !record.expiresAt.After(now) {
			delete(s.records, key)
		}
	}
}

func (s *idempotencyStore) evictOldestCompletedLocked() bool {
	oldestKey := ""
	var oldest time.Time
	for key, record := range s.records {
		if record.response == nil {
			continue
		}
		if oldestKey == "" || record.createdAt.Before(oldest) {
			oldestKey, oldest = key, record.createdAt
		}
	}
	if oldestKey == "" {
		return false
	}
	delete(s.records, oldestKey)
	return true
}

func copyStoredResponse(response *storedHTTPResponse) *storedHTTPResponse {
	if response == nil {
		return nil
	}
	copyResponse := *response
	copyResponse.Body = append([]byte(nil), response.Body...)
	return &copyResponse
}
