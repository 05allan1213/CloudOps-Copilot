package alertmanageringress

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"time"
)

const defaultRequestTimeout = 10 * time.Second

// Handler serves only the INTERNAL Alertmanager ingress and its redacted
// liveness/readiness endpoints.
type Handler struct {
	store          Store
	targets        []Target
	maxBodyBytes   int64
	requestTimeout time.Duration
	bearer         bearerVerifier
	runtimeReady   func(context.Context) error
}

func NewHandler(config Config) (*Handler, error) {
	if config.Store == nil {
		return nil, errors.New("V3 Alertmanager ingress store is required")
	}
	if len(config.Targets) == 0 || len(config.Targets) > maxTargets {
		return nil, errors.New("V3 Alertmanager ingress requires a bounded target allowlist")
	}
	targets := make([]Target, len(config.Targets))
	selectors := make(map[string]struct{}, len(config.Targets))
	for index, target := range config.Targets {
		var err error
		targets[index], err = normalizeTarget(target)
		if err != nil {
			return nil, err
		}
		if err := validateTarget(targets[index]); err != nil {
			return nil, err
		}
		selector := selectorKey(targets[index].MatchLabels)
		if _, exists := selectors[selector]; exists {
			return nil, errors.New("V3 Alertmanager ingress target selectors must be unique")
		}
		selectors[selector] = struct{}{}
	}
	if config.MaxBodyBytes <= 0 {
		return nil, errors.New("V3 Alertmanager ingress body limit must be positive")
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.RuntimeReady == nil {
		return nil, errors.New("V3 Alertmanager ingress runtime generation guard is required")
	}
	if len(config.BearerToken) > 0 {
		if err := validateBearerToken(config.BearerToken); err != nil {
			return nil, err
		}
	}
	return &Handler{
		store: config.Store, targets: targets,
		maxBodyBytes: config.MaxBodyBytes, requestTimeout: config.RequestTimeout,
		bearer: newBearerVerifier(config.BearerToken), runtimeReady: config.RuntimeReady,
	}, nil
}

// Webhook consumes one complete Alertmanager envelope. Authentication and
// structural errors fail the request; target mapping rejections remain durable
// per-alert facts and do not suppress valid alerts in the same envelope.
func (h *Handler) Webhook(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"status": "error", "code": "method_not_allowed"})
		return
	}
	if !h.bearer.verify(request.Header.Get("Authorization")) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="cloudops-alertmanager"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "error", "code": "unauthorized"})
		return
	}
	runtimeCtx, runtimeCancel := context.WithTimeout(request.Context(), h.requestTimeout)
	runtimeErr := h.runtimeReady(runtimeCtx)
	runtimeCancel()
	if runtimeErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "error", "code": "runtime_generation_refused"})
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"status": "error", "code": "unsupported_media_type"})
		return
	}
	if request.ContentLength > h.maxBodyBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"status": "error", "code": "request_too_large"})
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, h.maxBodyBytes)
	decoded, err := decodeEnvelope(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"status": "error", "code": "request_too_large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "code": "invalid_webhook"})
		return
	}
	batch, err := normalizeEnvelope(decoded, h.targets)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "code": "invalid_webhook"})
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	duplicateCount := 0
	storeRejectionCount := 0
	if len(batch.Signals) > 0 {
		results, ingestErr := h.store.IngestBatch(ctx, batch.Signals)
		if ingestErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "error", "code": "ingress_unavailable"})
			return
		}
		if len(results) != len(batch.Signals) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "error", "code": "ingress_unavailable"})
			return
		}
		for _, result := range results {
			if result.Rejected {
				storeRejectionCount++
			} else if result.Duplicate {
				duplicateCount++
			}
		}
	}
	if len(batch.Rejections) > 0 {
		if err := h.store.RecordRejections(ctx, batch.Rejections); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "error", "code": "ingress_unavailable"})
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "accepted", "alerts": len(decoded.Alerts), "ingested": len(batch.Signals) - storeRejectionCount,
		"duplicates": duplicateCount, "rejected": len(batch.Rejections) + storeRejectionCount,
	})
}

func (h *Handler) Livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) Readyz(w http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	if err := h.runtimeReady(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
		return
	}
	if err := h.store.Ready(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
