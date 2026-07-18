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

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/webhook"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	authpkg "github.com/05allan1213/CloudOps-Copilot/internal/service/auth"
	appincident "github.com/05allan1213/CloudOps-Copilot/internal/service/incident"
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

func TestIncidentWorkbenchSafeBoundedReadContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	publicID := "7f6c7e54-937a-4a12-9fb4-a315908fd0fd"
	application := &mockIncidentApplication{
		incident: &domain.Incident{ID: 99, PublicID: publicID, Status: domain.StatusVerifying, Severity: domain.SeverityCritical, Summary: "Checkout latency", ServiceName: "checkout", Environment: "prod", Namespace: "payments", TargetKind: "Deployment", TargetName: "checkout", FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now, Version: 7},
		page:     domain.Page{Items: []domain.Incident{{ID: 99, PublicID: publicID, Status: domain.StatusVerifying, Severity: domain.SeverityCritical, Summary: "Checkout latency", FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now, Version: 7}}, Total: 1, Page: 1, PageSize: 50},
		timeline: []domain.TimelineEvent{
			{EventType: domain.EventIncidentUpdated, ActorType: domain.ActorSystem, Summary: "later", OccurredAt: now.Add(time.Second), CreatedAt: now.Add(time.Second)},
			{EventType: domain.EventIncidentCreated, ActorType: domain.ActorSystem, Summary: "first", OccurredAt: now, CreatedAt: now},
		},
		evidence: []domain.EvidenceItem{{ID: 41, PublicID: "d84a7380-0257-4d4a-a68b-35cd312a429d", Type: "metrics", Source: "prometheus", Query: "raw-promql-secret", Facts: json.RawMessage(`{"token":"secret"}`), RawRef: "s3://private", Summary: "bounded evidence", Valid: true, CollectedAt: now}},
	}
	handler := &Handler{incidentService: application}
	router := gin.New()
	router.GET("/api/v2/workbench/incidents", handler.ListWorkbenchIncidents)
	router.GET("/api/v2/workbench/incidents/:id", handler.GetWorkbenchIncident)
	router.GET("/api/v2/workbench/incidents/:id/timeline", handler.ListWorkbenchTimeline)
	router.GET("/api/v2/workbench/incidents/:id/evidence", handler.ListWorkbenchEvidence)

	assertHTTP(t, router, "/api/v2/workbench/incidents?page_size=200&environment=prod&workload=checkout&q=latency", http.StatusOK, `"id":"`+publicID+`"`)
	if application.lastFilter.PageSize != workbenchMaxPageSize || application.lastFilter.Environment != "prod" || application.lastFilter.Workload != "checkout" || application.lastFilter.Search != "latency" {
		t.Fatalf("unexpected workbench filter: %+v", application.lastFilter)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/workbench/incidents/"+publicID, nil))
	for _, forbidden := range []string{`"version"`, `"correlation_key"`, `"ID":99`, `"lease"`, `"idempotency`} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("safe incident response leaked %s: %s", forbidden, recorder.Body.String())
		}
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/workbench/incidents/"+publicID+"/evidence", nil))
	for _, forbidden := range []string{"raw-promql-secret", "s3://private", `"query"`, `"facts"`, `"token"`, `"raw_ref"`, `"id":41`} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("safe evidence response leaked %s: %s", forbidden, recorder.Body.String())
		}
	}
	assertHTTP(t, router, "/api/v2/workbench/incidents/"+publicID+"/timeline?page_size=51", http.StatusBadRequest, "invalid page")
	assertHTTP(t, router, "/api/v2/workbench/incidents/not-a-uuid", http.StatusBadRequest, "invalid")
	assertHTTP(t, router, "/api/v2/workbench/incidents?q="+strings.Repeat("x", 129), http.StatusBadRequest, "exceeds maximum length")

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/workbench/incidents/"+publicID+"/timeline", nil))
	body := recorder.Body.String()
	if strings.Index(body, `"summary":"first"`) > strings.Index(body, `"summary":"later"`) {
		t.Fatalf("timeline ordering is not deterministic: %s", body)
	}
}

func TestIncidentWorkbenchAuthenticationAuthorizationAndNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	publicID := "7f6c7e54-937a-4a12-9fb4-a315908fd0fd"
	application := &mockIncidentApplication{getErr: domain.ErrNotFound}
	handler := &Handler{incidentService: application}
	router := gin.New()
	router.GET("/api/v2/workbench/incidents/:id", middleware.Auth(workbenchAuthVerifier{}), handler.GetWorkbenchIncident)
	assertHTTP(t, router, "/api/v2/workbench/incidents/"+publicID, http.StatusUnauthorized, "invalid or expired token")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v2/workbench/incidents/"+publicID, nil)
	request.Header.Set("Authorization", "Bearer valid")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("foreign/not-found incident status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	application.getErr = nil
	application.incident = &domain.Incident{PublicID: publicID}
	remediationRouter := gin.New()
	remediationRouter.Use(func(c *gin.Context) { c.Set(middleware.ContextRole, "viewer") })
	remediationRouter.GET("/api/v2/workbench/incidents/:id/remediation", handler.GetWorkbenchRemediation)
	assertHTTP(t, remediationRouter, "/api/v2/workbench/incidents/"+publicID+"/remediation", http.StatusForbidden, "forbidden")
}

func TestSafeObservedIsAllowlistedScalarAndBounded(t *testing.T) {
	result := safeObserved(json.RawMessage(`{"value":"` + strings.Repeat("x", 300) + `","status":"passed","query":"up{}","nested":{"token":"secret"},"sample_count":2}`))
	if len(result["value"].(string)) != 256 || result["status"] != "passed" || result["sample_count"] != float64(2) {
		t.Fatalf("unexpected safe observed result: %#v", result)
	}
	if _, exists := result["query"]; exists {
		t.Fatal("provider query leaked")
	}
	if _, exists := result["nested"]; exists {
		t.Fatal("nested provider payload leaked")
	}
}

type workbenchAuthVerifier struct{}

func (workbenchAuthVerifier) AuthenticateBearer(value string) (authpkg.Identity, error) {
	if value != "Bearer valid" {
		return authpkg.Identity{}, authpkg.ErrInvalidToken
	}
	return authpkg.Identity{ID: 1, Username: "viewer", Role: "viewer"}, nil
}

func (workbenchAuthVerifier) AuthenticateToken(string) (authpkg.Identity, error) {
	return authpkg.Identity{}, authpkg.ErrInvalidToken
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
