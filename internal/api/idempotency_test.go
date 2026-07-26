package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIdempotencyWaitHonorsRequestCancellation(t *testing.T) {
	store := newIdempotencyStore(time.Now)
	if _, _, err := store.reserve(context.Background(), "key", "hash"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.reserve(ctx, "key", "hash"); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error=%v, want context canceled", err)
	}
}

func TestIdempotencyCacheIsBoundedAndExpiresCompletedRecords(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store := newIdempotencyStore(func() time.Time { return now })
	store.capacity = 2
	store.ttl = time.Minute
	first, _, err := store.reserve(context.Background(), "first", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	store.complete(first, storedHTTPResponse{Status: 202, Body: []byte(`{}`)}, true)
	if _, _, err := store.reserve(context.Background(), "processing", "hash-2"); err != nil {
		t.Fatal(err)
	}
	replacement, _, err := store.reserve(context.Background(), "replacement", "hash-3")
	if err != nil {
		t.Fatalf("completed entry was not evicted at capacity: %v", err)
	}
	store.complete(replacement, storedHTTPResponse{Status: 202, Body: []byte(`{}`)}, true)
	if _, ok := store.records["first"]; ok {
		t.Fatal("oldest completed record remains after capacity eviction")
	}
	now = now.Add(2 * time.Minute)
	if _, _, err := store.reserve(context.Background(), "after-expiry", "hash-4"); err != nil {
		t.Fatalf("expired record was not pruned: %v", err)
	}
	if len(store.records) > store.capacity {
		t.Fatalf("record count=%d capacity=%d", len(store.records), store.capacity)
	}
}
