package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
)

type monitoringPortStub struct {
	err       error
	catalog   observability.Catalog
	execution observability.Execution
}

func (s *monitoringPortStub) Catalog(context.Context, observability.CatalogRequest) (observability.Catalog, error) {
	return s.catalog, s.err
}
func (s *monitoringPortStub) StartOwner(context.Context, observability.StartQueryRequest) (observability.Execution, error) {
	return s.execution, s.err
}
func (s *monitoringPortStub) Execution(context.Context, string) (observability.Execution, error) {
	return s.execution, s.err
}
func (s *monitoringPortStub) Executions(context.Context, observability.HistoryFilter) ([]observability.Execution, error) {
	return []observability.Execution{}, s.err
}
func (s *monitoringPortStub) Cancel(context.Context, string) (observability.Execution, error) {
	return s.execution, s.err
}
func (s *monitoringPortStub) SaveDefinition(context.Context, observability.SaveDefinitionRequest) (observability.Definition, error) {
	return observability.Definition{}, s.err
}
func (s *monitoringPortStub) Definitions(context.Context, int) ([]observability.Definition, error) {
	return []observability.Definition{}, s.err
}
func (s *monitoringPortStub) CreateAuthorization(context.Context, observability.CreateAuthorizationRequest) (observability.Authorization, error) {
	return observability.Authorization{}, s.err
}
func (s *monitoringPortStub) Authorizations(context.Context, int) ([]observability.Authorization, error) {
	return []observability.Authorization{}, s.err
}
func (s *monitoringPortStub) RevokeAuthorization(context.Context, string) error { return s.err }

func TestMonitoringHTTPContractUsesExplicitUnavailableAndBoundedErrors(t *testing.T) {
	stub := &monitoringPortStub{catalog: observability.Catalog{
		ProviderState: observability.ProviderUnavailable, ProviderDetail: "Prometheus Provider Gateway is unavailable",
		MetricNames: []string{}, Queries: []observability.CatalogEntry{},
	}}
	engine := newContractEngine(NewHandler(Config{Monitoring: stub}))
	catalog := httptest.NewRecorder()
	engine.ServeHTTP(catalog, httptest.NewRequest(http.MethodGet,
		"/api/v1/monitoring/catalog?cluster_id=cloudops-local&namespace=cloudops-system&resource_id=workload-1&resource_kind=Deployment&resource_name=cloudops-api", nil))
	if catalog.Code != http.StatusOK || !strings.Contains(catalog.Body.String(), `"provider_state":"unavailable"`) {
		t.Fatalf("catalog status=%d body=%s", catalog.Code, catalog.Body.String())
	}

	stub.err = observability.ErrBoundExceeded
	query := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/monitoring/queries", strings.NewReader(validMonitoringRequest))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(query, request)
	if query.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bounded query status=%d body=%s", query.Code, query.Body.String())
	}
	assertProblem(t, query, "INVALID_MONITORING_QUERY")

	stub.err = errors.New("provider token=must-not-leak")
	unavailable := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/monitoring/queries", strings.NewReader(validMonitoringRequest))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(unavailable, request)
	if unavailable.Code != http.StatusServiceUnavailable || strings.Contains(unavailable.Body.String(), "token=") {
		t.Fatalf("unavailable status=%d body=%s", unavailable.Code, unavailable.Body.String())
	}
	assertProblem(t, unavailable, "PROMETHEUS_PROVIDER_UNAVAILABLE")
}

func TestMonitoringBrowserCannotClaimAgentActor(t *testing.T) {
	stub := &monitoringPortStub{}
	engine := newContractEngine(NewHandler(Config{Monitoring: stub}))
	response := httptest.NewRecorder()
	body := strings.TrimSuffix(validMonitoringRequest, "}") + `,"actor":"agent"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/monitoring/queries", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("actor injection status=%d body=%s", response.Code, response.Body.String())
	}
	assertProblem(t, response, "INVALID_REQUEST")
}

const validMonitoringRequest = `{
  "mode":"expert",
  "query":"up",
  "cluster_id":"cloudops-local",
  "namespace":"cloudops-system",
  "resource":{"id":"workload-1","kind":"Deployment","namespace":"cloudops-system","name":"cloudops-api"},
  "from":"2026-07-26T00:00:00Z",
  "to":"2026-07-26T00:05:00Z",
  "step_seconds":30
}`
