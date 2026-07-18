package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Readiness func(context.Context) error

type Options struct {
	Process string
	Timeout time.Duration
	Ready   Readiness
	Metrics http.Handler
}

func NewHandler(options Options) http.Handler {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	mux := http.NewServeMux()
	livez := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"healthy": true, "process": options.Process})
	}
	mux.HandleFunc("/livez", livez)
	mux.HandleFunc("/healthz", livez)
	if options.Metrics != nil {
		mux.Handle("/metrics", options.Metrics)
	}
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if options.Ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			err := options.Ready(ctx)
			cancel()
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false, "process": options.Process})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ready": true, "process": options.Process})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
