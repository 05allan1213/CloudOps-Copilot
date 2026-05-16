package pubsub

import (
	"context"
	"errors"
	"sync"
)

var ErrHubClosed = errors.New("pubsub hub is closed")

type Hub struct {
	messages chan []byte
	mu       sync.RWMutex
	closed   bool
}

func NewHub(bufferSize int) *Hub {
	return &Hub{
		messages: make(chan []byte, bufferSize),
	}
}

func (h *Hub) Messages() <-chan []byte {
	return h.messages
}

func (h *Hub) PublishLocal(ctx context.Context, message []byte) error {
	if h == nil {
		return ErrHubClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return ErrHubClosed
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case h.messages <- message:
		return nil
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.messages)
}
