package monitoringprometheus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type accessStub struct {
	access settings.ProviderAccess
}

func (s accessStub) ProviderAccess(_ context.Context, revisionID string, provider settings.Provider) (settings.ProviderAccess, error) {
	if revisionID != s.access.Revision.ID || provider != settings.ProviderPrometheus {
		return settings.ProviderAccess{}, settings.ErrNotFound
	}
	result := s.access
	result.Credential = append([]byte(nil), s.access.Credential...)
	return result, nil
}

func TestAdapterQueriesRealPrometheusHTTPContractWithWorkerValidation(t *testing.T) {
	var observedQuery, authorization string
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"3.13.1"}}`))
		case "/api/v1/label/__name__/values":
			_, _ = w.Write([]byte(`{"status":"success","data":["process_cpu_seconds_total","up"]}`))
		case "/api/v1/query_range":
			observedQuery = r.URL.Query().Get("query")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"instance":"api"},"values":[[1710000000,"1.5"],[1710000030,"2.5"]]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer prometheus.Close()

	revision := monitoringRevision(prometheus.URL)
	adapter, err := New(accessStub{access: settings.ProviderAccess{
		Revision: revision, Configuration: revision.Providers[0], Credential: []byte("test-token"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	resource := observability.ResourceReference{ID: "workload-1", Kind: "Deployment", Namespace: "cloudops-system", Name: "cloudops-api"}
	prepared, err := observability.PrepareOwnerQuery(observability.StartQueryRequest{
		Mode: observability.ModeExpert, Query: "process_cpu_seconds_total", ClusterID: "cloudops-local",
		Namespace: resource.Namespace, Resource: resource, From: time.Unix(1710000000, 0),
		To: time.Unix(1710000060, 0), StepSeconds: 30,
	}, revision)
	if err != nil {
		t.Fatal(err)
	}
	request := observability.ProviderQueryRequest{
		ConfigurationRevision: revision.ID, Scope: prepared.Scope, Resource: prepared.Resource,
		Query: prepared.Query, QueryHash: prepared.QueryHash, TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
	}
	result, err := adapter.Query(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.ServerVersion != "3.13.1" || result.SeriesCount != 1 || result.SampleCount != 2 || len(result.Result.Series) != 1 {
		t.Fatalf("result=%#v", result)
	}
	for _, required := range []string{`cluster_id="cloudops-local"`, `environment="local"`, `namespace="cloudops-system"`, `workload_kind="Deployment"`, `workload="cloudops-api"`} {
		if !strings.Contains(observedQuery, required) {
			t.Errorf("Prometheus query %q missing %s", observedQuery, required)
		}
	}
	if authorization != "Bearer test-token" {
		t.Fatalf("Authorization=%q", authorization)
	}

	request.Query = "up"
	digest := sha256.Sum256([]byte(request.Query))
	request.QueryHash = hex.EncodeToString(digest[:])
	if _, err := adapter.Query(context.Background(), request); err == nil {
		t.Fatal("Worker accepted an API request that bypassed deterministic Scope normalization")
	}
}

func monitoringRevision(endpoint string) settings.Revision {
	scope := settings.OperationalScope{Name: "Local", ClusterID: "cloudops-local", Environment: "local", Namespaces: []string{"cloudops-system"}, Active: true}
	return settings.Revision{
		ID: "11111111-1111-4111-8111-111111111111", General: settings.GeneralConfiguration{QueryMaxLookbackSeconds: 3600, QueryMaxResults: 1000},
		Scope: scope, Scopes: []settings.OperationalScope{scope}, Providers: []settings.ProviderConfiguration{{
			Provider: settings.ProviderPrometheus, Enabled: true, Endpoint: endpoint, TimeoutMS: 5000, MaxResults: 200,
		}},
	}
}
