package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type telemetryPortStub struct {
	err          error
	catalog      telemetry.Catalog
	logQuery     telemetry.LogQuery
	trace        telemetry.TraceDetail
	evidence     []telemetry.Evidence
	lastLog      telemetry.StartLogQueryRequest
	lastSnapshot telemetry.AttachContextSnapshotRequest
	snapshot     telemetry.ContextSnapshot
}

func (s *telemetryPortStub) Catalog(context.Context, string, telemetry.CatalogRequest) (telemetry.Catalog, error) {
	return s.catalog, s.err
}
func (s *telemetryPortStub) QueryLogs(_ context.Context, request telemetry.StartLogQueryRequest) (telemetry.LogQuery, error) {
	s.lastLog = request
	return s.logQuery, s.err
}
func (s *telemetryPortStub) LogQuery(context.Context, string) (telemetry.LogQuery, error) {
	return s.logQuery, s.err
}
func (s *telemetryPortStub) LogQueries(context.Context, string, string, string, int) ([]telemetry.LogQuery, error) {
	return []telemetry.LogQuery{}, s.err
}
func (s *telemetryPortStub) SearchTraces(context.Context, telemetry.StartTraceSearchRequest) (telemetry.TraceSearch, error) {
	return telemetry.TraceSearch{Traces: []telemetry.TraceSummary{}}, s.err
}
func (s *telemetryPortStub) TraceSearch(context.Context, string) (telemetry.TraceSearch, error) {
	return telemetry.TraceSearch{Traces: []telemetry.TraceSummary{}}, s.err
}
func (s *telemetryPortStub) TraceSearches(context.Context, string, string, string, int) ([]telemetry.TraceSearch, error) {
	return []telemetry.TraceSearch{}, s.err
}
func (s *telemetryPortStub) Trace(context.Context, telemetry.TraceDetailRequest) (telemetry.TraceDetail, error) {
	return s.trace, s.err
}
func (s *telemetryPortStub) LogEvidence(context.Context, string) ([]telemetry.Evidence, error) {
	return s.evidence, s.err
}
func (s *telemetryPortStub) SaveLogEvidence(context.Context, string, telemetry.SaveEvidenceRequest) (telemetry.Evidence, error) {
	return telemetry.Evidence{}, s.err
}
func (s *telemetryPortStub) TraceEvidence(context.Context, string) ([]telemetry.Evidence, error) {
	return s.evidence, s.err
}
func (s *telemetryPortStub) SaveTraceEvidence(context.Context, string, string, telemetry.SaveEvidenceRequest) (telemetry.Evidence, error) {
	return telemetry.Evidence{}, s.err
}
func (s *telemetryPortStub) CreateConsultation(context.Context, telemetry.CreateConsultationRequest) (telemetry.Consultation, error) {
	return telemetry.Consultation{}, s.err
}
func (s *telemetryPortStub) AttachContextSnapshot(_ context.Context, _ string, request telemetry.AttachContextSnapshotRequest) (telemetry.ContextSnapshot, error) {
	s.lastSnapshot = request
	return s.snapshot, s.err
}

func TestTelemetryHTTPContractProjectsUnavailableEmptyAndTruncatedStates(t *testing.T) {
	stub := &telemetryPortStub{catalog: telemetry.Catalog{
		Provider: "elasticsearch", ProviderState: telemetry.ProviderUnavailable,
		ProviderDetail: "Elasticsearch Provider Gateway is unavailable",
	}, logQuery: telemetry.LogQuery{Entries: []telemetry.LogEntry{}, Histogram: []telemetry.HistogramBucket{}, Fields: []string{}, Truncated: true}}
	engine := newContractEngine(NewHandler(Config{Telemetry: stub}))

	catalog := httptest.NewRecorder()
	engine.ServeHTTP(catalog, httptest.NewRequest(http.MethodGet,
		"/api/v1/logs/catalog?cluster_id=cloudops-local&namespace=cloudops-system&resource_id=workload-1&resource_kind=Deployment&resource_name=cloudops-api", nil))
	if catalog.Code != http.StatusOK || !strings.Contains(catalog.Body.String(), `"provider_state":"unavailable"`) {
		t.Fatalf("catalog status=%d body=%s", catalog.Code, catalog.Body.String())
	}

	query := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/logs/queries", strings.NewReader(validLogRequest))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(query, request)
	if query.Code != http.StatusAccepted || !strings.Contains(query.Body.String(), `"entries":[]`) || !strings.Contains(query.Body.String(), `"truncated":true`) || !stub.lastLog.Tail {
		t.Fatalf("query status=%d request=%#v body=%s", query.Code, stub.lastLog, query.Body.String())
	}
}

func TestTelemetryHTTPContractListsDurableEvidenceByOwningExecution(t *testing.T) {
	evidenceID := "8be63d14-aa13-4a41-9711-1e48cb9e79f3"
	queryID := "74e7d032-cd87-4931-8704-1f8b974273f2"
	stub := &telemetryPortStub{evidence: []telemetry.Evidence{{ID: evidenceID, QueryID: queryID}}}
	engine := newContractEngine(NewHandler(Config{Telemetry: stub}))

	for _, path := range []string{
		"/api/v1/logs/queries/" + queryID + "/evidence",
		"/api/v1/traces/searches/" + queryID + "/evidence",
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), evidenceID) || !strings.Contains(response.Body.String(), `"items"`) {
			t.Fatalf("evidence path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestTelemetryHTTPContractHidesProviderErrorsAndRejectsActorInjection(t *testing.T) {
	stub := &telemetryPortStub{err: errors.New("provider authorization=must-not-leak")}
	engine := newContractEngine(NewHandler(Config{Telemetry: stub}))

	unavailable := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/logs/queries", strings.NewReader(validLogRequest))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(unavailable, request)
	if unavailable.Code != http.StatusServiceUnavailable || strings.Contains(unavailable.Body.String(), "must-not-leak") {
		t.Fatalf("unavailable status=%d body=%s", unavailable.Code, unavailable.Body.String())
	}
	assertProblem(t, unavailable, "TELEMETRY_PROVIDER_UNAVAILABLE")

	stub.err = nil
	injected := httptest.NewRecorder()
	body := strings.TrimSuffix(validLogRequest, "}") + `,"actor":"agent"}`
	request = httptest.NewRequest(http.MethodPost, "/api/v1/logs/queries", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(injected, request)
	if injected.Code != http.StatusBadRequest {
		t.Fatalf("actor injection status=%d body=%s", injected.Code, injected.Body.String())
	}
	assertProblem(t, injected, "INVALID_REQUEST")
}

const validLogRequest = `{
  "mode":"guided",
  "filter":{"text":"request failed","levels":["error"]},
  "cluster_id":"cloudops-local",
  "namespace":"cloudops-system",
  "resource":{"id":"workload-1","kind":"Deployment","namespace":"cloudops-system","name":"cloudops-api"},
  "from":"2026-07-26T13:45:00Z",
  "to":"2026-07-26T14:00:00Z",
  "limit":100,
  "tail":true
}`
