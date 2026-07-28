package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProcessHealthAndReadiness(t *testing.T) {
	ready := false
	handler := NewHandler(Options{
		Process: "cloudops-worker",
		Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("cloudops_worker_up 1\n"))
		}),
		Ready: func(_ context.Context) error {
			if !ready {
				return errors.New("not ready")
			}
			return nil
		},
	})

	for _, path := range []string{"/livez", "/healthz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d, want %d", path, response.Code, http.StatusOK)
		}
	}

	metrics := httptest.NewRecorder()
	handler.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "cloudops_worker_up 1") {
		t.Fatalf("metrics status=%d body=%q", metrics.Code, metrics.Body.String())
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready status=%d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	ready = true
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ready status=%d, want %d", response.Code, http.StatusOK)
	}
}
