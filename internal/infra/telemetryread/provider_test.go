package telemetryread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type providerAccessStub struct{ revision settings.Revision }

func (s providerAccessStub) Revision(context.Context, string) (settings.Revision, error) {
	return s.revision, nil
}

func (s providerAccessStub) ProviderAccess(_ context.Context, _ string, provider settings.Provider) (settings.ProviderAccess, error) {
	for _, item := range s.revision.Providers {
		if item.Provider == provider {
			return settings.ProviderAccess{Revision: s.revision, Configuration: item}, nil
		}
	}
	return settings.ProviderAccess{}, settings.ErrNotFound
}

func providerTestContext(endpoint string) (settings.Revision, settings.OperationalScope, telemetry.ResourceReference, telemetry.TimeRange, telemetry.QueryBounds) {
	scope := settings.OperationalScope{
		ID: "scope-a", Name: "local", ClusterID: "cloudops-local", Environment: "local",
		Namespaces: []string{"cloudops-system"}, Active: true,
	}
	revision := settings.Revision{
		ID: "123e4567-e89b-42d3-a456-426614174000", Scope: scope, Scopes: []settings.OperationalScope{scope},
		General: settings.GeneralConfiguration{QueryMaxLookbackSeconds: 3600, QueryMaxResults: 1000},
		Providers: []settings.ProviderConfiguration{
			{Provider: settings.ProviderElasticsearch, Enabled: true, Endpoint: endpoint, TimeoutMS: 5000, MaxResults: 1000},
			{Provider: settings.ProviderTempo, Enabled: true, Endpoint: endpoint, TimeoutMS: 5000, MaxResults: 200},
		},
	}
	resource := telemetry.ResourceReference{
		ID:   "kubernetes://cloudops-local/apps/v1/namespaces/cloudops-system/deployments/cloudops-api",
		Kind: "Deployment", Namespace: "cloudops-system", Name: "cloudops-api",
	}
	to := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	window := telemetry.TimeRange{From: to.Add(-15 * time.Minute), To: to}
	bounds := telemetry.QueryBounds{MaxLookbackSeconds: 3600, TimeoutMS: 5000, MaxResponseBytes: 1024 * 1024, MaxResults: 1, ConcurrencyLimit: 2}
	return revision, scope, resource, window, bounds
}

func TestElasticsearchAdapterBoundsProjectsAndRedactsRows(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/logs-cloudops-*/_search" {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "timed_out":false,"_shards":{"failed":0},"hits":{"total":{"value":2},"hits":[
            {"_id":"a","_source":{"@timestamp":"2026-07-26T13:50:00Z","level":"error","message":"request failed authorization=private-value","service":{"name":"cloudops-api"},"trace":{"id":"0123456789abcdef0123456789abcdef"},"span":{"id":"0123456789abcdef"},"kubernetes":{"pod":{"name":"cloudops-api-a"}}}},
            {"_id":"b","_source":{"@timestamp":"2026-07-26T13:51:00Z","level":"info","message":"second row"}}
          ]}}
        `))
	}))
	defer server.Close()

	revision, scope, resource, window, bounds := providerTestContext(server.URL)
	provider, err := New(providerAccessStub{revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	query := `{"bool":{"filter":[{"term":{"cloudops.cluster_id":"cloudops-local"}},{"term":{"kubernetes.namespace":"cloudops-system"}},{"term":{"kubernetes.deployment.name":"cloudops-api"}}],"must":[{"bool":{"must":[]}}]}}`
	result, err := provider.QueryLogs(context.Background(), telemetry.ProviderLogRequest{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: resource, Query: query,
		TimeRange: window, Bounds: bounds, Tail: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || !result.Truncated || len(result.Histogram) != 24 {
		t.Fatalf("result=%#v", result)
	}
	entry := result.Entries[0]
	if strings.Contains(entry.Message, "private-value") || !strings.Contains(entry.Message, "[REDACTED]") ||
		entry.TraceID != "0123456789abcdef0123456789abcdef" || entry.Resource != resource {
		t.Fatalf("entry=%#v", entry)
	}
	sortItems := captured["sort"].([]any)
	firstSort := sortItems[0].(map[string]any)["@timestamp"].(map[string]any)
	secondSort := sortItems[1].(map[string]any)["_shard_doc"].(map[string]any)
	if captured["size"] != float64(2) || firstSort["order"] != "desc" || secondSort["order"] != "desc" {
		t.Fatalf("bounded request=%#v", captured)
	}
}

func TestTempoAdapterSearchAndDetailKeepExactKubernetesIdentity(t *testing.T) {
	const traceID = "0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/search":
			if request.URL.Query().Get("limit") != "2" || !strings.Contains(request.URL.Query().Get("q"), `resource.k8s.workload.name = "cloudops-api"`) {
				t.Fatalf("search query=%s", request.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"traces":[
              {"traceID":"` + traceID + `","rootServiceName":"cloudops-api","rootTraceName":"GET /api/v1/bootstrap","startTimeUnixNano":"1785073800000000000","durationMs":25,"spanSets":[{"spans":[{},{}]}]},
              {"traceID":"abcdef0123456789abcdef0123456789","rootServiceName":"cloudops-api","rootTraceName":"GET /readyz","startTimeUnixNano":"1785073801000000000","durationMs":2}
            ],"metrics":{"completedJobs":1,"totalJobs":1}}`))
		case "/api/traces/" + traceID:
			_, _ = w.Write([]byte(`{"batches":[{"resource":{"attributes":[
              {"key":"service.name","value":{"stringValue":"cloudops-api"}},
              {"key":"k8s.cluster.name","value":{"stringValue":"cloudops-local"}},
              {"key":"k8s.namespace.name","value":{"stringValue":"cloudops-system"}},
              {"key":"k8s.workload.kind","value":{"stringValue":"Deployment"}},
              {"key":"k8s.workload.name","value":{"stringValue":"cloudops-api"}}
            ]},"scopeSpans":[{"spans":[
			  {"traceId":"ASNFZ4mrze8BI0VniavN7w==","spanId":"ERERERERERE=","name":"GET /api/v1/bootstrap","kind":"SPAN_KIND_SERVER","startTimeUnixNano":"1785073800000000000","endTimeUnixNano":"1785073800025000000","status":{"code":"STATUS_CODE_OK"}},
			  {"traceId":"ASNFZ4mrze8BI0VniavN7w==","spanId":"IiIiIiIiIiI=","parentSpanId":"ERERERERERE=","name":"mysql.query","kind":"SPAN_KIND_CLIENT","startTimeUnixNano":"1785073800005000000","endTimeUnixNano":"1785073800020000000","status":{"code":"STATUS_CODE_ERROR"},"events":[{"name":"db.timeout","timeUnixNano":"1785073800010000000","attributes":[]}]}
            ]}]}]}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	revision, scope, resource, window, bounds := providerTestContext(server.URL)
	provider, err := New(providerAccessStub{revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	query := `{ resource.k8s.cluster.name = "cloudops-local" && resource.k8s.namespace.name = "cloudops-system" && resource.k8s.workload.name = "cloudops-api" }`
	search, err := provider.SearchTraces(context.Background(), telemetry.ProviderTraceSearchRequest{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: resource, Query: query,
		TimeRange: window, Bounds: bounds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Traces) != 1 || !search.Truncated || search.Traces[0].TraceID != traceID {
		t.Fatalf("search=%#v", search)
	}
	detail, err := provider.Trace(context.Background(), telemetry.ProviderTraceDetailRequest{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: resource, TraceID: traceID,
		TimeRange: window, Bounds: bounds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Detail.Spans) != 2 || detail.Detail.RootService != "cloudops-api" || detail.Detail.Resource != resource {
		t.Fatalf("detail=%#v", detail)
	}
	if !detail.Detail.Spans[0].CriticalPath || !detail.Detail.Spans[1].CriticalPath || detail.Detail.Spans[1].Depth != 1 || detail.Detail.Spans[1].Status != "error" {
		t.Fatalf("waterfall=%#v", detail.Detail.Spans)
	}

	wrongResource := resource
	wrongResource.Kind = "StatefulSet"
	if _, err := provider.Trace(context.Background(), telemetry.ProviderTraceDetailRequest{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: wrongResource, TraceID: traceID,
		TimeRange: window, Bounds: bounds,
	}); err != telemetry.ErrNotFound {
		t.Fatalf("wrong Kubernetes identity error=%v", err)
	}
}
