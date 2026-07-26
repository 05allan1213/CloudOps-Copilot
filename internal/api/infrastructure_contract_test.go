package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
)

type infrastructurePortStub struct {
	query       infrastructure.Query
	resourceID  string
	resourceErr error
}

func (s *infrastructurePortStub) Topology(_ context.Context, query infrastructure.Query) (infrastructure.TopologySnapshot, error) {
	s.query = query
	return infrastructure.TopologySnapshot{
		ID: "snapshot-1", ContentHash: "content-hash", ConfigurationRevision: "revision-1",
		ProviderState: infrastructure.ProviderAvailable, ProviderDetail: "typed projection available",
		Source:    infrastructure.ProviderSource{Provider: "kubernetes", ClusterID: "cluster-a", Identity: "kubernetes://cluster-a"},
		Freshness: infrastructure.Freshness{State: "fresh"}, Nodes: []infrastructure.Resource{{ID: "resource-pod", Kind: "Pod", Namespace: "ops", Name: "api-pod", OwnerReferences: []infrastructure.ResourceReference{}, Selector: map[string]string{}, Labels: map[string]string{}, Endpoints: []infrastructure.ResourceEndpoint{}, Ports: []infrastructure.ResourcePort{}, Conditions: []infrastructure.ResourceCondition{}, Addresses: []string{}, Links: []infrastructure.ContextLink{}}},
		Edges: []infrastructure.TopologyEdge{}, Issues: []infrastructure.ProviderIssue{}, CollectedAt: time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC),
	}, nil
}

func (s *infrastructurePortStub) Resources(ctx context.Context, query infrastructure.Query) (infrastructure.ResourcePage, error) {
	topology, err := s.Topology(ctx, query)
	return infrastructure.ResourcePage{SnapshotID: topology.ID, ProviderState: topology.ProviderState, Source: topology.Source, Freshness: topology.Freshness, Items: topology.Nodes, CollectedAt: topology.CollectedAt}, err
}

func (s *infrastructurePortStub) Resource(ctx context.Context, id string, query infrastructure.Query) (infrastructure.ResourceDetail, error) {
	s.resourceID = id
	if s.resourceErr != nil {
		return infrastructure.ResourceDetail{}, s.resourceErr
	}
	topology, err := s.Topology(ctx, query)
	return infrastructure.ResourceDetail{SnapshotID: topology.ID, ProviderState: topology.ProviderState, Source: topology.Source, Freshness: topology.Freshness, Resource: topology.Nodes[0], Related: []infrastructure.Resource{}, Edges: []infrastructure.TopologyEdge{}, CollectedAt: topology.CollectedAt}, err
}

func (s *infrastructurePortStub) ResourceEvents(_ context.Context, id string, query infrastructure.Query) (infrastructure.EventPage, error) {
	s.resourceID = id
	s.query = query
	if s.resourceErr != nil {
		return infrastructure.EventPage{}, s.resourceErr
	}
	return infrastructure.EventPage{SnapshotID: "snapshot-1", ProviderState: infrastructure.ProviderAvailable, Source: infrastructure.ProviderSource{Provider: "kubernetes", ClusterID: "cluster-a"}, ResourceID: id, Items: []infrastructure.Event{}, CollectedAt: time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC)}, nil
}

func (*infrastructurePortStub) Probe(context.Context) (string, error) {
	return "typed probe available", nil
}

func TestInfrastructureRoutesExposeTypedProjectionAndSSE(t *testing.T) {
	t.Parallel()
	port := &infrastructurePortStub{}
	engine := newContractEngine(NewHandler(Config{Infrastructure: port}))
	from := "2026-07-26T04:00:00Z"
	to := "2026-07-26T05:00:00Z"

	topology := httptest.NewRecorder()
	topologyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/topology?cluster=cluster-a&namespace=ops&kind=Pod&search=api&limit=25&from="+from+"&to="+to, nil)
	topologyRequest.Header.Set(RequestIDHeader, "topology-request")
	engine.ServeHTTP(topology, topologyRequest)
	if topology.Code != http.StatusOK || topology.Header().Get("Content-Type") != JSONMediaType || !strings.Contains(topology.Body.String(), `"resource-pod"`) {
		t.Fatalf("topology status=%d content-type=%q body=%s", topology.Code, topology.Header().Get("Content-Type"), topology.Body.String())
	}
	if port.query.ClusterID != "cluster-a" || port.query.Namespace != "ops" || port.query.Limit != 25 || len(port.query.Kinds) != 1 || port.query.Kinds[0] != "Pod" || port.query.Search != "api" || port.query.From.IsZero() || port.query.To.IsZero() {
		t.Fatalf("parsed topology query = %#v", port.query)
	}

	detail := httptest.NewRecorder()
	engine.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/v1/resources/resource-pod?cluster=cluster-a&namespace=ops&from="+from+"&to="+to, nil))
	if detail.Code != http.StatusOK || port.resourceID != "resource-pod" || !strings.Contains(detail.Body.String(), `"related":[]`) {
		t.Fatalf("detail status=%d body=%s id=%q", detail.Code, detail.Body.String(), port.resourceID)
	}

	events := httptest.NewRecorder()
	engine.ServeHTTP(events, httptest.NewRequest(http.MethodGet, "/api/v1/resources/resource-pod/events?cluster=cluster-a&namespace=ops&from="+from+"&to="+to, nil))
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"items":[]`) {
		t.Fatalf("events status=%d body=%s", events.Code, events.Body.String())
	}

	stream := httptest.NewRecorder()
	streamRequest := httptest.NewRequest(http.MethodGet, "/api/v1/topology/events?cluster=cluster-a&namespace=ops&from="+from+"&to="+to, nil)
	streamRequest.Header.Set("Last-Event-ID", "older-hash")
	engine.ServeHTTP(stream, streamRequest)
	if stream.Code != http.StatusOK || stream.Header().Get("Content-Type") != SSEMediaType || !strings.Contains(stream.Body.String(), "event: topology.refresh") || !strings.Contains(stream.Body.String(), "content_hash") {
		t.Fatalf("stream status=%d content-type=%q body=%s", stream.Code, stream.Header().Get("Content-Type"), stream.Body.String())
	}
}

func TestInfrastructureRoutesReturnProblemDetailsForInvalidAndUnavailableQueries(t *testing.T) {
	t.Parallel()
	port := &infrastructurePortStub{resourceErr: infrastructure.ErrUnavailable}
	engine := newContractEngine(NewHandler(Config{Infrastructure: port}))

	invalid := httptest.NewRecorder()
	engine.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/resources?limit=501", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid query status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	assertProblem(t, invalid, "INVALID_INFRASTRUCTURE_QUERY")

	unavailable := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/resources/resource-pod", nil)
	request.Header.Set(RequestIDHeader, "resource-request")
	engine.ServeHTTP(unavailable, request)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d body=%s", unavailable.Code, unavailable.Body.String())
	}
	problem := assertProblem(t, unavailable, "KUBERNETES_PROVIDER_UNAVAILABLE")
	if problem.RequestID != "resource-request" || problem.TraceID == "" {
		t.Fatalf("problem identities = %#v", problem)
	}
}
