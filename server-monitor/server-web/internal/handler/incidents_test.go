package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	domain "server-web/internal/incident"
	"server-web/internal/infra/webhook"
	appincident "server-web/internal/service/incident"
)

func TestIncidentWebhookHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &mockIncidentApplication{ingestResults: []appincident.IngestResult{{SourceEventID: "v1:event", CorrelationKey: "v1:key", IncidentPublicID: "public-id"}}}
	handler := &Handler{incidentService: application, requestTimeout: time.Second}
	router := gin.New()
	router.POST("/api/v2/webhook/alertmanager", bodyLimit(512), handler.IncidentAlertmanagerWebhook)

	valid := `{"alerts":[{"status":"firing","fingerprint":"abc","startsAt":"2026-07-14T00:00:00Z","endsAt":"0001-01-01T00:00:00Z","labels":{"alertname":"Down"},"annotations":{}}]}`
	tests := []struct {
		name, contentType, body string
		wantStatus              int
	}{
		{"valid", "application/json", valid, http.StatusAccepted},
		{"valid charset", "application/json; charset=utf-8", valid, http.StatusAccepted},
		{"invalid json", "application/json", `{`, http.StatusBadRequest},
		{"wrong content type", "text/plain", valid, http.StatusUnsupportedMediaType},
		{"body too large", "application/json", `{"alerts":"` + strings.Repeat("x", 1024) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v2/webhook/alertmanager", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.contentType)
			router.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestIncidentWebhookDuplicateIsAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &mockIncidentApplication{ingestResults: []appincident.IngestResult{{SourceEventID: "v1:event", CorrelationKey: "v1:key", IncidentPublicID: "public-id", Duplicate: true}}}
	handler := &Handler{incidentService: application, requestTimeout: time.Second}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v2/webhook/alertmanager", bytes.NewBufferString(`{"alerts":[{}]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.IncidentAlertmanagerWebhook(ctx)
	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), `"duplicate":true`) {
		t.Fatalf("unexpected response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestIncidentReadHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	application := &mockIncidentApplication{
		incident: &domain.Incident{ID: 99, PublicID: "public-id", Status: domain.StatusCorrelating, Severity: domain.SeverityCritical, FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now, Version: 2},
		page:     domain.Page{Items: []domain.Incident{{ID: 99, PublicID: "public-id", Status: domain.StatusCorrelating}}, Total: 1, Page: 2, PageSize: 100},
		timeline: []domain.TimelineEvent{{EventType: domain.EventIncidentCreated, Metadata: json.RawMessage(`{"version":1}`), OccurredAt: now}},
	}
	handler := &Handler{incidentService: application}
	router := gin.New()
	router.GET("/api/v2/incidents", handler.ListIncidents)
	router.GET("/api/v2/incidents/:id", handler.GetIncident)
	router.GET("/api/v2/incidents/:id/timeline", handler.ListIncidentTimeline)

	assertHTTP(t, router, "/api/v2/incidents?page=2&page_size=200&status=correlating&severity=critical", http.StatusOK, `"page_size":100`)
	if application.lastFilter.Page != 2 || application.lastFilter.PageSize != 200 || application.lastFilter.Status != domain.StatusCorrelating {
		t.Fatalf("unexpected parsed filter: %+v", application.lastFilter)
	}
	assertHTTP(t, router, "/api/v2/incidents/public-id", http.StatusOK, `"id":"public-id"`)
	if strings.Contains(application.lastResponse, `"ID":99`) {
		t.Fatal("numeric database ID leaked")
	}
	assertHTTP(t, router, "/api/v2/incidents/public-id/timeline", http.StatusOK, `"event_type":"incident_created"`)
	assertHTTP(t, router, "/api/v2/incidents?page=0", http.StatusBadRequest, `page must be a positive integer`)

	application.getErr = domain.ErrNotFound
	assertHTTP(t, router, "/api/v2/incidents/missing", http.StatusNotFound, `incident not found`)
}

func assertHTTP(t *testing.T, router http.Handler, path string, status int, contains string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != status || !strings.Contains(recorder.Body.String(), contains) {
		t.Fatalf("GET %s: status=%d body=%s", path, recorder.Code, recorder.Body.String())
	}
}

func bodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

type mockIncidentApplication struct {
	ingestResults []appincident.IngestResult
	ingestErr     error
	incident      *domain.Incident
	getErr        error
	page          domain.Page
	lastFilter    domain.ListFilter
	lastResponse  string
	signals       []domain.Signal
	timeline      []domain.TimelineEvent
	evidence      []domain.EvidenceItem
}

func (m *mockIncidentApplication) IngestAlertmanager(_ context.Context, _ webhook.AlertmanagerWebhookRequest) ([]appincident.IngestResult, error) {
	return m.ingestResults, m.ingestErr
}
func (m *mockIncidentApplication) GetIncident(_ context.Context, _ string) (*domain.Incident, error) {
	return m.incident, m.getErr
}
func (m *mockIncidentApplication) ListIncidents(_ context.Context, filter domain.ListFilter) (domain.Page, error) {
	m.lastFilter = filter
	return m.page, nil
}
func (m *mockIncidentApplication) ListSignals(context.Context, string) ([]domain.Signal, error) {
	return m.signals, nil
}
func (m *mockIncidentApplication) ListTimeline(context.Context, string) ([]domain.TimelineEvent, error) {
	return m.timeline, nil
}
func (m *mockIncidentApplication) ListEvidence(context.Context, string) ([]domain.EvidenceItem, error) {
	return m.evidence, nil
}
