package apiv3

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMySQLQueryPortRequiresDatabase(t *testing.T) {
	if _, err := NewMySQLQueryPort(nil); err == nil {
		t.Fatal("nil database was accepted")
	}
	port, err := NewMySQLQueryPort(new(sql.DB))
	if err != nil || port == nil {
		t.Fatalf("query port=%v err=%v", port, err)
	}
	var uninitialized *MySQLQueryPort
	if _, err := uninitialized.Query(context.Background(), QueryRequest{Kind: QueryIncidents}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("uninitialized query err=%v", err)
	}
}

func TestMySQLQueryCursorMustBePublicUUID(t *testing.T) {
	port := &MySQLQueryPort{db: new(sql.DB)}
	if _, err := port.listIncidents(context.Background(), QueryRequest{
		Kind: QueryIncidents, Cursor: "eyJpZCI6NDJ9", Limit: 10,
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("encoded numeric cursor err=%v", err)
	}
}

func TestIncidentListFiltersReachDurableQueryPort(t *testing.T) {
	queries := &captureQueryPort{}
	engine := newContractEngine(NewHandler(Config{Queries: queries}))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/api/v3/incidents?status=investigating&severity=critical&service=checkout&limit=25", nil)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	got := queries.request
	if got.Kind != QueryIncidents || got.Status != "investigating" || got.Severity != "critical" || got.Service != "checkout" || got.Limit != 25 {
		t.Fatalf("query request=%+v", got)
	}

	for _, query := range []string{"status=failed", "severity=sev0", "service=" + strings.Repeat("x", 256)} {
		response = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/api/v3/incidents?"+query, nil)
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query=%q status=%d body=%s", query, response.Code, response.Body.String())
		}
		assertProblem(t, response, "INVALID_FILTER")
	}
}

func TestEventRefreshResourcesAndBoundedProjectionText(t *testing.T) {
	for eventType, want := range map[string]string{
		"signal_received":              "signals",
		"evidence_persisted":           "evidence",
		"investigation_start_enqueued": "investigations",
		"remediation_plan_created":     "remediation_plans",
		"delivery_observed":            "delivery",
		"verification_started":         "verifications",
		"resolution_report_created":    "resolution_report",
		"incident_closed":              "incident",
	} {
		if got := eventRefreshResource(eventType); got != want {
			t.Errorf("event %q resource=%q want=%q", eventType, got, want)
		}
	}
	value := "abc\u754cxyz"
	if got := boundProjectionText(value, 5); got != "abc" {
		t.Fatalf("UTF-8 bounded value=%q", got)
	}
}

type captureQueryPort struct {
	request QueryRequest
}

func (p *captureQueryPort) Query(_ context.Context, request QueryRequest) (QueryResponse, error) {
	p.request = request
	return QueryResponse{Incidents: []IncidentView{}}, nil
}
