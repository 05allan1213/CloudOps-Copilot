package monitoringgateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
)

type providerStub struct {
	catalogRequest observability.ProviderCatalogRequest
	queryRequest   observability.ProviderQueryRequest
	err            error
}

func (p *providerStub) Catalog(_ context.Context, request observability.ProviderCatalogRequest) (observability.ProviderCatalog, error) {
	p.catalogRequest = request
	if p.err != nil {
		return observability.ProviderCatalog{}, p.err
	}
	return observability.ProviderCatalog{MetricNames: []string{"up"}}, nil
}

func (p *providerStub) Query(_ context.Context, request observability.ProviderQueryRequest) (observability.ProviderQueryResult, error) {
	p.queryRequest = request
	if p.err != nil {
		return observability.ProviderQueryResult{}, p.err
	}
	return observability.ProviderQueryResult{Result: observability.QueryResult{ResultType: "matrix", Series: []observability.QuerySeries{}}}, nil
}

func TestTypedClientServerRoundTripAndErrorMapping(t *testing.T) {
	provider := &providerStub{}
	handler, err := NewServer(provider)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	catalogRequest := observability.ProviderCatalogRequest{ConfigurationRevision: "revision-a", Bounds: observability.QueryBounds{MaxSeries: 10}}
	catalog, err := client.Catalog(context.Background(), catalogRequest)
	if err != nil || len(catalog.MetricNames) != 1 || provider.catalogRequest.ConfigurationRevision != "revision-a" {
		t.Fatalf("Catalog() value=%#v request=%#v err=%v", catalog, provider.catalogRequest, err)
	}
	queryRequest := observability.ProviderQueryRequest{ConfigurationRevision: "revision-b", Query: "up"}
	result, err := client.Query(context.Background(), queryRequest)
	if err != nil || result.Result.Series == nil || provider.queryRequest.ConfigurationRevision != "revision-b" {
		t.Fatalf("Query() value=%#v request=%#v err=%v", result, provider.queryRequest, err)
	}

	provider.err = observability.ErrBoundExceeded
	if _, err := client.Query(context.Background(), queryRequest); !errors.Is(err, observability.ErrBoundExceeded) {
		t.Fatalf("bounded gateway error=%v", err)
	}
	provider.err = errors.New("credential token=must-not-leak")
	if _, err := client.Query(context.Background(), queryRequest); !errors.Is(err, observability.ErrUnavailable) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("unavailable gateway error=%v", err)
	}
}

func TestGatewayRejectsUnsafeInputs(t *testing.T) {
	for _, endpoint := range []string{"file:///tmp/worker.sock", "https://user:pass@example.invalid", "https://example.invalid?target=other"} {
		if _, err := NewClient(endpoint, time.Second); err == nil {
			t.Fatalf("unsafe endpoint %q accepted", endpoint)
		}
	}
	handler, err := NewServer(&providerStub{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path, contentType, body string
		status                          int
	}{
		{http.MethodGet, catalogPath, "", "", http.StatusMethodNotAllowed},
		{http.MethodPost, queryPath, "", `{}`, http.StatusUnsupportedMediaType},
		{http.MethodPost, queryPath, "application/json", `{"query":"up","proxy_url":"https://example.invalid"}`, http.StatusBadRequest},
		{http.MethodPost, basePath + "/proxy", "application/json", `{}`, http.StatusNotFound},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", test.contentType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}
