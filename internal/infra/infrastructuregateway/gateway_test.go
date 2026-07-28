package infrastructuregateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
)

type readerStub struct {
	request       infrastructure.ReadRequest
	probeCluster  string
	eventCluster  string
	eventResource infrastructure.Resource
	eventLimit    int
}

func (r *readerStub) Probe(_ context.Context, clusterID string) (infrastructure.ProviderSource, error) {
	r.probeCluster = clusterID
	return infrastructure.ProviderSource{Provider: "kubernetes", ClusterID: clusterID, Identity: "kubernetes://" + clusterID, ServerVersion: "v1.36.1"}, nil
}

func (r *readerStub) Read(_ context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	r.request = request
	return infrastructure.Projection{Source: infrastructure.ProviderSource{Provider: "kubernetes", ClusterID: request.ClusterID, Identity: "kubernetes://" + request.ClusterID}, Nodes: []infrastructure.Resource{}, Edges: []infrastructure.TopologyEdge{}, Issues: []infrastructure.ProviderIssue{}}, nil
}

func (r *readerStub) Events(_ context.Context, clusterID string, resource infrastructure.Resource, limit int) ([]infrastructure.Event, bool, error) {
	r.eventCluster = clusterID
	r.eventResource = resource
	r.eventLimit = limit
	return []infrastructure.Event{{ID: "event-1", ResourceKind: resource.Kind, ResourceName: resource.Name}}, true, nil
}

func TestClientAndServerRoundTripTypedContracts(t *testing.T) {
	t.Parallel()
	reader := &readerStub{}
	serverHandler, err := NewServer(reader)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	server := httptest.NewServer(serverHandler)
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source, err := client.Probe(context.Background(), "cluster-a")
	if err != nil || source.ClusterID != "cluster-a" || source.ServerVersion != "v1.36.1" {
		t.Fatalf("Probe() source=%#v error=%v", source, err)
	}
	request := infrastructure.ReadRequest{ClusterID: "cluster-a", Namespaces: []string{"ops"}, Limit: 25}
	projection, err := client.Read(context.Background(), request)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if reader.request.ClusterID != request.ClusterID || len(reader.request.Namespaces) != 1 || reader.request.Namespaces[0] != "ops" || projection.Nodes == nil {
		t.Fatalf("typed topology request=%#v projection=%#v", reader.request, projection)
	}
	resource := infrastructure.Resource{ID: "k8s-resource", Kind: "Pod", Namespace: "ops", Name: "api-pod"}
	events, truncated, err := client.Events(context.Background(), "cluster-a", resource, 7)
	if err != nil || !truncated || len(events) != 1 {
		t.Fatalf("Events() values=%#v truncated=%v error=%v", events, truncated, err)
	}
	if reader.probeCluster != "cluster-a" || reader.eventCluster != "cluster-a" || reader.eventResource.ID != resource.ID || reader.eventLimit != 7 {
		t.Fatalf("typed requests probe=%q event_cluster=%q resource=%#v limit=%d", reader.probeCluster, reader.eventCluster, reader.eventResource, reader.eventLimit)
	}
}

func TestServerRejectsNonContractMethodsAndBodies(t *testing.T) {
	t.Parallel()
	handler, err := NewServer(&readerStub{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "probe without cluster", method: http.MethodGet, path: probePath, wantStatus: http.StatusBadRequest, wantCode: "INVALID_CLUSTER"},
		{name: "topology get", method: http.MethodGet, path: topologyPath, wantStatus: http.StatusMethodNotAllowed, wantCode: "METHOD_NOT_ALLOWED"},
		{name: "missing content type", method: http.MethodPost, path: topologyPath, body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "INVALID_CONTENT_TYPE"},
		{name: "unknown field", method: http.MethodPost, path: topologyPath, contentType: "application/json", body: `{"cluster_id":"cluster-a","namespaces":["ops"],"limit":10,"proxy_url":"https://example.invalid"}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "multiple values", method: http.MethodPost, path: eventsPath, contentType: "application/json", body: `{"resource":{},"limit":1}{}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "unknown route", method: http.MethodGet, path: basePath + "/proxy", wantStatus: http.StatusNotFound, wantCode: "ROUTE_NOT_FOUND"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantCode) {
				t.Fatalf("status=%d body=%q, want status=%d code=%s", response.Code, response.Body.String(), test.wantStatus, test.wantCode)
			}
		})
	}
}

func TestClientRejectsUnsafeEndpointAndOversizedResponse(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"file:///tmp/worker.sock", "https://user:secret@example.invalid", "https://example.invalid?target=other"} {
		if _, err := NewClient(endpoint, time.Second); err == nil {
			t.Fatalf("NewClient(%q) succeeded, want fixed endpoint rejection", endpoint)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"padding":"` + strings.Repeat("x", maxReplyBytes) + `"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Probe(context.Background(), "cluster-a")
	if err == nil || !strings.Contains(err.Error(), "exceeds the bounded size") {
		t.Fatalf("Probe() error = %v, want bounded response error", err)
	}
}

func TestServerReturnsUnavailableWithoutLeakingReaderError(t *testing.T) {
	t.Parallel()
	handler, err := NewServer(errorReader{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, probePath+"?cluster_id=cluster-a", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "credential") {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

type errorReader struct{}

func (errorReader) Probe(context.Context, string) (infrastructure.ProviderSource, error) {
	return infrastructure.ProviderSource{}, errors.New("credential token=do-not-leak")
}
func (errorReader) Read(context.Context, infrastructure.ReadRequest) (infrastructure.Projection, error) {
	return infrastructure.Projection{}, errors.New("unavailable")
}
func (errorReader) Events(context.Context, string, infrastructure.Resource, int) ([]infrastructure.Event, bool, error) {
	return nil, false, errors.New("unavailable")
}
