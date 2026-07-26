package telemetrygateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type providerStub struct{ err error }

func (s providerStub) Catalog(context.Context, telemetry.ProviderCatalogRequest) (telemetry.ProviderCatalog, error) {
	return telemetry.ProviderCatalog{}, s.err
}
func (s providerStub) QueryLogs(context.Context, telemetry.ProviderLogRequest) (telemetry.ProviderLogResult, error) {
	return telemetry.ProviderLogResult{}, s.err
}
func (s providerStub) SearchTraces(context.Context, telemetry.ProviderTraceSearchRequest) (telemetry.ProviderTraceSearchResult, error) {
	return telemetry.ProviderTraceSearchResult{}, s.err
}
func (s providerStub) Trace(context.Context, telemetry.ProviderTraceDetailRequest) (telemetry.ProviderTraceDetailResult, error) {
	return telemetry.ProviderTraceDetailResult{}, s.err
}

func TestGatewayRoundTripKeepsTypedEmptyCollections(t *testing.T) {
	handler, err := NewServer(providerStub{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.QueryLogs(context.Background(), telemetry.ProviderLogRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entries == nil || result.Histogram == nil || result.Fields == nil {
		t.Fatalf("empty result is not explicit: %#v", result)
	}

	response, err := http.Get(server.URL + logsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", response.StatusCode)
	}
}

func TestGatewayMapsProviderFailuresWithoutLeakingDetails(t *testing.T) {
	handler, err := NewServer(providerStub{err: errors.Join(telemetry.ErrProviderDisabled, errors.New("token=must-not-leak"))})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SearchTraces(context.Background(), telemetry.ProviderTraceSearchRequest{}); !errors.Is(err, telemetry.ErrProviderDisabled) {
		t.Fatalf("mapped error=%v", err)
	}
}
