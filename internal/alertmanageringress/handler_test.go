package alertmanageringress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentv3mysql"
)

func TestWebhookStrictBoundedBatchAndDurableTargetRejection(t *testing.T) {
	store := &fakeStore{}
	handler := mustHandler(t, store, nil, 32*1024)
	valid := testAlert("firing", "abc123", "WorkloadNotReady", "critical")
	unknown := testAlert("firing", "def456", "CloudOpsSelfAlert", "warning")
	unknown.Labels["deployment"] = "cloudops-api"
	body := marshalEnvelope(t, testEnvelope(valid, unknown))
	response := serveWebhook(handler, body, "application/json", "")
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"ingested":1`) || !strings.Contains(response.Body.String(), `"rejected":1`) {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if len(store.signals) != 1 || len(store.rejections) != 1 || store.rejections[0].ReasonCode != "target_not_allowlisted" {
		t.Fatalf("store signals=%+v rejections=%+v", store.signals, store.rejections)
	}

	strict := bytes.Replace(body, []byte(`"receiver":"cloudops-demo"`), []byte(`"receiver":"cloudops-demo","unexpected":true`), 1)
	store.reset()
	response = serveWebhook(handler, strict, "application/json", "")
	if response.Code != http.StatusBadRequest || store.ingestCalls != 0 || store.rejectionCalls != 0 {
		t.Fatalf("strict response=%d calls=%d/%d", response.Code, store.ingestCalls, store.rejectionCalls)
	}
	duplicateKey := bytes.Replace(body, []byte(`"receiver":"cloudops-demo"`), []byte(`"receiver":"cloudops-demo","receiver":"attacker"`), 1)
	response = serveWebhook(handler, duplicateKey, "application/json", "")
	if response.Code != http.StatusBadRequest || store.ingestCalls != 0 || store.rejectionCalls != 0 {
		t.Fatalf("duplicate-key response=%d calls=%d/%d", response.Code, store.ingestCalls, store.rejectionCalls)
	}
	caseVariant := bytes.Replace(body, []byte(`"receiver":"cloudops-demo"`), []byte(`"Receiver":"cloudops-demo"`), 1)
	response = serveWebhook(handler, caseVariant, "application/json", "")
	if response.Code != http.StatusBadRequest || store.ingestCalls != 0 || store.rejectionCalls != 0 {
		t.Fatalf("case-variant response=%d calls=%d/%d", response.Code, store.ingestCalls, store.rejectionCalls)
	}

	invalidSecond := testEnvelope(valid, testAlert("broken", "def456", "HighErrorRate", "warning"))
	store.reset()
	response = serveWebhook(handler, marshalEnvelope(t, invalidSecond), "application/json", "")
	if response.Code != http.StatusBadRequest || store.ingestCalls != 0 {
		t.Fatalf("partial batch reached store: status=%d calls=%d", response.Code, store.ingestCalls)
	}
}

func TestWebhookContentTypeBodyLimitBearerAndDuplicateContract(t *testing.T) {
	store := &fakeStore{results: []incidentv3mysql.IngestResult{{Duplicate: true}}}
	handler := mustHandler(t, store, []byte("0123456789abcdef0123456789abcdef"), 4096)
	body := marshalEnvelope(t, testEnvelope(testAlert("firing", "abc123", "WorkloadNotReady", "critical")))
	for _, test := range []struct {
		name, contentType, authorization string
		body                             []byte
		want                             int
	}{
		{"missing bearer", "application/json", "", body, http.StatusUnauthorized},
		{"wrong bearer", "application/json", "Bearer wrong-wrong-wrong", body, http.StatusUnauthorized},
		{"wrong content type", "text/plain", "Bearer 0123456789abcdef0123456789abcdef", body, http.StatusUnsupportedMediaType},
		{"oversized", "application/json", "Bearer 0123456789abcdef0123456789abcdef", bytes.Repeat([]byte("x"), 4097), http.StatusRequestEntityTooLarge},
		{"accepted duplicate", "application/json; charset=utf-8", "Bearer 0123456789abcdef0123456789abcdef", body, http.StatusAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serveWebhook(handler, test.body, test.contentType, test.authorization)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if test.want == http.StatusAccepted && !strings.Contains(response.Body.String(), `"duplicates":1`) {
				t.Fatalf("duplicate response=%s", response.Body.String())
			}
		})
	}
	chunkedBody := append([]byte(`{"version":"`), bytes.Repeat([]byte("x"), 4097)...)
	chunked := httptest.NewRequest(http.MethodPost, "/webhooks/alertmanager", bytes.NewReader(chunkedBody))
	chunked.ContentLength = -1
	chunked.Header.Set("Content-Type", "application/json")
	chunked.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.Webhook(response, chunked)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked oversized status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWebhookReportsStoreLevelUnmatchedResolvedRejection(t *testing.T) {
	store := &fakeStore{results: []incidentv3mysql.IngestResult{{Rejected: true, RejectionReason: "unmatched_resolved"}}}
	handler := mustHandler(t, store, nil, 4096)
	resolved := testAlert("resolved", "abc123", "WorkloadNotReady", "critical")
	resolved.EndsAt = resolved.StartsAt.Add(time.Minute)
	response := serveWebhook(handler, marshalEnvelope(t, testEnvelope(resolved)), "application/json", "")
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"ingested":0`) || !strings.Contains(response.Body.String(), `"rejected":1`) {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}

func TestInternalProbesAreRedacted(t *testing.T) {
	store := &fakeStore{readyErr: errors.New("mysql://user:secret@db/schema migration detail")}
	handler := mustHandler(t, store, nil, 1024)
	ready := httptest.NewRecorder()
	handler.Readyz(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable || ready.Body.String() != "{\"status\":\"not_ready\"}\n" || strings.Contains(ready.Body.String(), "mysql") {
		t.Fatalf("ready response=%d %s", ready.Code, ready.Body.String())
	}
	live := httptest.NewRecorder()
	handler.Livez(live, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if live.Code != http.StatusOK || live.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("live response=%d %s", live.Code, live.Body.String())
	}
}

type fakeStore struct {
	readyErr       error
	ingestErr      error
	rejectionErr   error
	results        []incidentv3mysql.IngestResult
	signals        []incidentv3mysql.SignalInput
	rejections     []incidentv3mysql.RejectionInput
	ingestCalls    int
	rejectionCalls int
}

func (f *fakeStore) Ready(context.Context) error { return f.readyErr }

func (f *fakeStore) IngestBatch(_ context.Context, signals []incidentv3mysql.SignalInput) ([]incidentv3mysql.IngestResult, error) {
	f.ingestCalls++
	f.signals = append([]incidentv3mysql.SignalInput(nil), signals...)
	if f.results == nil {
		f.results = make([]incidentv3mysql.IngestResult, len(signals))
	}
	return f.results, f.ingestErr
}

func (f *fakeStore) RecordRejections(_ context.Context, rejections []incidentv3mysql.RejectionInput) error {
	f.rejectionCalls++
	f.rejections = append([]incidentv3mysql.RejectionInput(nil), rejections...)
	return f.rejectionErr
}

func (f *fakeStore) reset() {
	f.signals = nil
	f.rejections = nil
	f.ingestCalls = 0
	f.rejectionCalls = 0
}

func mustHandler(t *testing.T, store Store, token []byte, maxBody int64) *Handler {
	t.Helper()
	handler, err := NewHandler(Config{
		Store: store, Targets: mustTargets(t), MaxBodyBytes: maxBody,
		RequestTimeout: time.Second, BearerToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func marshalEnvelope(t *testing.T, input envelope) []byte {
	t.Helper()
	result, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func serveWebhook(handler *Handler, body []byte, contentType, authorization string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/webhooks/alertmanager", bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	handler.Webhook(response, request)
	return response
}
