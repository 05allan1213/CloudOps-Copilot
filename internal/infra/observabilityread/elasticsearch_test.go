package observabilityread

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestElasticsearchUsesFixedReadOnlyDSLAndRedactsSamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/logs-cloudops-*/_search" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		_, _ = fmt.Fprintf(w, `{"hits":{"total":{"value":1},"hits":[{"_source":{"message":"required_env_missing token=secret","kubernetes":{"pod":{"name":"demo-abc-123"}}}}]}}`)
	}))
	defer server.Close()
	client, err := NewElasticsearch(ElasticConfig{
		BaseURL: server.URL, IndexPattern: "logs-cloudops-*", Timeout: time.Second, MaxResponseBytes: 4096,
		MaxSamples: 3, MaxLookback: time.Hour, AllowedServices: map[string]struct{}{"demo": {}},
		AllowedNamespaces: map[string]struct{}{"demo": {}}, AllowedEnvironments: map[string]struct{}{"local": {}}, AllowHTTPForTests: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := client.Search(context.Background(), ElasticQuery{Service: "demo", Namespace: "demo", Environment: "local", Workload: "demo", Lookback: 5 * time.Minute, Severity: "error", Keyword: "required_env_missing", Limit: 3})
	if err != nil || observation.Status != verification.ObservationAvailable || observation.MatchedCount != 1 || len(observation.RedactedExamples) != 1 || strings.Contains(observation.RedactedExamples[0], "secret") {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
	if _, err := client.Search(context.Background(), ElasticQuery{Service: "demo", Namespace: "demo", Environment: "local", Workload: "demo", Lookback: 5 * time.Minute, Severity: "error", Keyword: "*:*", Limit: 3}); err == nil {
		t.Fatal("caller-supplied Elasticsearch query language was accepted")
	}
}
