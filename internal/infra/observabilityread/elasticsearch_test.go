package observabilityread

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	observation, err := client.Search(context.Background(), ElasticQuery{Service: "demo", Namespace: "demo", Environment: "local", Workload: "demo", Lookback: 30 * time.Second, Severity: "error", Keyword: "required_env_missing", Limit: 3})
	if err != nil || observation.Status != verification.ObservationAvailable || observation.MatchedCount != 1 || len(observation.RedactedExamples) != 1 || strings.Contains(observation.RedactedExamples[0], "secret") {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
	if _, err := client.Search(context.Background(), ElasticQuery{Service: "demo", Namespace: "demo", Environment: "local", Workload: "demo", Lookback: 5 * time.Minute, Severity: "error", Keyword: "*:*", Limit: 3}); err == nil {
		t.Fatal("caller-supplied Elasticsearch query language was accepted")
	}
}

func TestElasticsearchWarningMatchesStructuredAndJSONMessageLevels(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprint(w, `{"hits":{"total":{"value":0},"hits":[]}}`)
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
	if _, err := client.Search(context.Background(), ElasticQuery{Service: "demo", Namespace: "demo", Environment: "local", Workload: "demo", Lookback: 5 * time.Minute, Severity: "warning", Keyword: "required_env_missing", Limit: 3}); err != nil {
		t.Fatal(err)
	}
	filters := requestBody["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
	should := filters[len(filters)-1].(map[string]any)["bool"].(map[string]any)["should"].([]any)
	want := []any{
		map[string]any{"term": map[string]any{"log.level": "warn"}},
		map[string]any{"term": map[string]any{"log.level": "warning"}},
		map[string]any{"match_phrase": map[string]any{"message": `"level":"warn"`}},
		map[string]any{"match_phrase": map[string]any{"message": `"level":"warning"`}},
	}
	if !reflect.DeepEqual(should, want) {
		t.Fatalf("warning filters=%#v want=%#v", should, want)
	}
}
