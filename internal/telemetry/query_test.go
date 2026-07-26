package telemetry

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func telemetryTestRevision() settings.Revision {
	scope := settings.OperationalScope{
		ID: "scope-a", Name: "local", ClusterID: "cloudops-local", Environment: "local",
		Namespaces: []string{"cloudops-system", "demo"}, Active: true,
	}
	return settings.Revision{
		ID: "123e4567-e89b-42d3-a456-426614174000", Scope: scope,
		Scopes:  []settings.OperationalScope{scope},
		General: settings.GeneralConfiguration{QueryMaxLookbackSeconds: 3600, QueryMaxResults: 1000},
		Providers: []settings.ProviderConfiguration{
			{Provider: settings.ProviderElasticsearch, Enabled: true, Endpoint: "http://elasticsearch:9200", TimeoutMS: 5000, MaxResults: 1000},
			{Provider: settings.ProviderTempo, Enabled: true, Endpoint: "http://tempo:3200", TimeoutMS: 5000, MaxResults: 200},
		},
	}
}

func telemetryTestResource() ResourceReference {
	return ResourceReference{
		ID:   "kubernetes://cloudops-local/apps/v1/namespaces/cloudops-system/deployments/cloudops-api",
		Kind: "Deployment", Namespace: "cloudops-system", Name: "cloudops-api",
	}
}

func telemetryTestRange() (time.Time, time.Time) {
	to := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	return to.Add(-15 * time.Minute), to
}

func TestPrepareLogQueryEnforcesScopeBoundsAndTailIdentity(t *testing.T) {
	from, to := telemetryTestRange()
	request := StartLogQueryRequest{
		Mode: ModeGuided, Filter: LogFilter{Text: "request failed", Levels: []string{"error", "warn"}},
		ClusterID: "cloudops-local", Namespace: "cloudops-system", Resource: telemetryTestResource(),
		From: from, To: to, Limit: 100, Tail: true,
	}
	prepared, err := prepareLogQuery(request, telemetryTestRevision())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Kind != "logs_tail" || prepared.Bounds.MaxResults != 100 {
		t.Fatalf("prepared=%#v", prepared)
	}
	for _, exact := range []string{"cloudops.cluster_id", "cloudops-local", "kubernetes.namespace", "cloudops-system", "kubernetes.deployment.name", "cloudops-api"} {
		if !strings.Contains(prepared.Query, exact) {
			t.Fatalf("normalized query %q is missing %q", prepared.Query, exact)
		}
	}

	request.Filter.Levels = []string{"not-a-level"}
	if _, err := prepareLogQuery(request, telemetryTestRevision()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported level error=%v", err)
	}
	request.Filter.Levels = nil
	request.To = request.From.Add(2 * time.Hour)
	if _, err := prepareLogQuery(request, telemetryTestRevision()); !errors.Is(err, ErrBoundExceeded) {
		t.Fatalf("lookback error=%v", err)
	}
}

func TestExpertElasticsearchAcceptsOneRestrictedClauseOnly(t *testing.T) {
	from, to := telemetryTestRange()
	request := StartLogQueryRequest{
		Mode: ModeExpert, Query: `{"match":{"message":"timeout"}}`, ClusterID: "cloudops-local",
		Namespace: "cloudops-system", Resource: telemetryTestResource(), From: from, To: to, Limit: 20,
	}
	if _, err := prepareLogQuery(request, telemetryTestRevision()); err != nil {
		t.Fatal(err)
	}
	request.Query = `{"match_all":{}} {"match_all":{}}`
	if _, err := prepareLogQuery(request, telemetryTestRevision()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("multiple JSON values error=%v", err)
	}
	request.Query = `{"script":{"source":"return true"}}`
	if _, err := prepareLogQuery(request, telemetryTestRevision()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("script query error=%v", err)
	}
}

func TestExpertTraceQLCannotPrecedeOrReplaceExactScope(t *testing.T) {
	from, to := telemetryTestRange()
	request := StartTraceSearchRequest{
		Mode: ModeExpert, Query: `{ status = error || duration > 1s }`, ClusterID: "cloudops-local",
		Namespace: "cloudops-system", Resource: telemetryTestResource(), From: from, To: to, Limit: 25,
	}
	prepared, err := prepareTraceSearch(request, telemetryTestRevision())
	if err != nil {
		t.Fatal(err)
	}
	prefix := `{ resource.k8s.cluster.name = "cloudops-local" && resource.k8s.namespace.name = "cloudops-system" && resource.k8s.workload.name = "cloudops-api" && (`
	if !strings.HasPrefix(prepared.Query, prefix) || !strings.HasSuffix(prepared.Query, `) }`) {
		t.Fatalf("expert TraceQL is not scope-first and grouped: %q", prepared.Query)
	}
	providerRequest := ProviderTraceSearchRequest{
		ConfigurationRevision: prepared.ConfigurationRevision, Scope: prepared.Scope, Resource: prepared.Resource,
		Query: prepared.Query, TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
	}
	if err := ValidateProviderTraceSearchRequest(providerRequest, telemetryTestRevision()); err != nil {
		t.Fatal(err)
	}
	providerRequest.Query = `{ resource.foo = "resource.k8s.cluster.name = \"cloudops-local\"" && resource.foo = "resource.k8s.namespace.name = \"cloudops-system\"" && resource.foo = "resource.k8s.workload.name = \"cloudops-api\"" }`
	if err := ValidateProviderTraceSearchRequest(providerRequest, telemetryTestRevision()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("embedded scope strings error=%v", err)
	}
}

func TestEphemeralResultsExpireAndStayCapacityBounded(t *testing.T) {
	now := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	service := &Service{
		now: func() time.Time { return now }, logs: make(map[string]cachedLogResult),
		traces: make(map[string]cachedTraceSearchResult), details: make(map[string]cachedTraceDetailResult),
	}
	service.cacheLog("first", ProviderLogResult{Entries: []LogEntry{{ID: "row-a"}}})
	if _, ok := service.cachedLog("first"); !ok {
		t.Fatal("fresh result was not available")
	}
	now = now.Add(ephemeralResultTTL)
	if _, ok := service.cachedLog("first"); ok {
		t.Fatal("expired result remained available")
	}
	for index := 0; index < maximumCachedResults+4; index++ {
		service.cacheTraceSearch(strings.Repeat("x", index+1), ProviderTraceSearchResult{})
	}
	if len(service.traces) > maximumCachedResults {
		t.Fatalf("cache size=%d want <=%d", len(service.traces), maximumCachedResults)
	}
}

func TestEphemeralProjectionKeepsTypedEmptyCollections(t *testing.T) {
	logs := cloneLogResult(ProviderLogResult{})
	if logs.Histogram == nil || logs.Entries == nil || logs.Fields == nil {
		t.Fatalf("log projection contains null collections: %#v", logs)
	}
	search := cloneTraceSearchResult(ProviderTraceSearchResult{})
	if search.Traces == nil {
		t.Fatalf("trace search contains null traces: %#v", search)
	}
	detail := cloneTraceDetailResult(ProviderTraceDetailResult{Detail: TraceDetail{Spans: []Span{{}}}})
	if detail.Detail.Spans == nil || detail.Detail.Spans[0].Events == nil || detail.Detail.Spans[0].Links == nil || detail.Detail.Links == nil {
		t.Fatalf("trace detail contains null collections: %#v", detail)
	}
}
