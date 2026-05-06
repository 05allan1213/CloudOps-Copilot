package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	redis       Pinger
	pingTimeout time.Duration
	kafkaReady  atomic.Bool
}

func NewHandler(redis Pinger, pingTimeout time.Duration) *Handler {
	if pingTimeout <= 0 {
		pingTimeout = time.Second
	}
	h := &Handler{
		redis:       redis,
		pingTimeout: pingTimeout,
	}
	h.kafkaReady.Store(false)
	return h
}

func (h *Handler) SetKafkaReady(ready bool) {
	h.kafkaReady.Store(ready)
}

func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"data": map[string]bool{
			"healthy": true,
		},
	})
}

func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	if !h.kafkaReady.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "error",
			"data": map[string]interface{}{
				"ready":  false,
				"kafka":  false,
				"redis":  false,
				"reason": "kafka not ready",
			},
		})
		return
	}
	if h.redis == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "error",
			"data": map[string]interface{}{
				"ready":  false,
				"kafka":  true,
				"redis":  false,
				"reason": "redis not configured",
			},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.pingTimeout)
	defer cancel()
	if err := h.redis.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "error",
			"data": map[string]interface{}{
				"ready":  false,
				"kafka":  true,
				"redis":  false,
				"reason": "redis not ready",
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"data": map[string]bool{
			"ready": true,
			"kafka": true,
			"redis": true,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
