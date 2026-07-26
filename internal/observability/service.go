package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type RevisionStore interface {
	ActiveRevision(context.Context) (settings.Revision, error)
	Revision(context.Context, string) (settings.Revision, error)
}

type Store interface {
	CreateExecution(context.Context, PreparedQuery) (Execution, error)
	CreateAuthorizedExecution(context.Context, PreparedQuery) (Execution, error)
	MarkRunning(context.Context, string) error
	Complete(context.Context, string, ProviderQueryResult, error) error
	Cancel(context.Context, string) error
	RecoverInFlight(context.Context) error
	Execution(context.Context, string) (Execution, error)
	Executions(context.Context, HistoryFilter) ([]Execution, error)
	CreateDefinition(context.Context, SaveDefinitionRequest) (Definition, error)
	Definition(context.Context, string) (Definition, error)
	Definitions(context.Context, int) ([]Definition, error)
	CreateAuthorization(context.Context, CreateAuthorizationRequest) (Authorization, error)
	Authorization(context.Context, string) (Authorization, error)
	Authorizations(context.Context, int) ([]Authorization, error)
	RevokeAuthorization(context.Context, string) error
}

type Service struct {
	store     Store
	revisions RevisionStore
	provider  Provider
	semaphore chan struct{}

	mu      sync.RWMutex
	results map[string]QueryResult
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func NewService(ctx context.Context, store Store, revisions RevisionStore, provider Provider) (*Service, error) {
	if store == nil || revisions == nil || provider == nil {
		return nil, errors.New("observability service requires store, Configuration Revision store, and Provider")
	}
	if err := store.RecoverInFlight(ctx); err != nil {
		return nil, fmt.Errorf("recover interrupted Query Executions: %w", err)
	}
	return &Service{
		store: store, revisions: revisions, provider: provider,
		semaphore: make(chan struct{}, MaximumConcurrent), results: make(map[string]QueryResult),
		cancels: make(map[string]context.CancelFunc),
	}, nil
}

func (s *Service) Catalog(ctx context.Context, request CatalogRequest) (Catalog, error) {
	revision, err := s.revisions.ActiveRevision(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("load active Configuration Revision: %w", err)
	}
	providerConfig, configErr := prometheusConfiguration(revision)
	if configErr != nil {
		return Catalog{}, configErr
	}
	scope, resource, scopeErr := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if scopeErr != nil {
		return Catalog{}, scopeErr
	}
	bounds := providerBounds(revision, providerConfig, 30)
	queries, queryErr := GuidedCatalog(scope, resource, time.Duration(bounds.MaxLookbackSeconds)*time.Second)
	if queryErr != nil {
		return Catalog{}, queryErr
	}
	collectedAt := time.Now().UTC()
	catalog := Catalog{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: resource,
		ProviderState: ProviderAvailable, ProviderDetail: "Prometheus bounded adapter available",
		Source: ProviderSource{Provider: "prometheus", CollectedAt: collectedAt}, Queries: queries,
		MetricNames: []string{}, Bounds: bounds, CollectedAt: collectedAt,
	}
	if !providerConfig.Enabled {
		catalog.ProviderState = ProviderDisabled
		catalog.ProviderDetail = "Prometheus is disabled in the active Configuration Revision"
		return catalog, nil
	}
	providerCatalog, err := s.provider.Catalog(ctx, ProviderCatalogRequest{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: resource, Bounds: bounds,
	})
	if err != nil {
		catalog.ProviderState = ProviderUnavailable
		catalog.ProviderDetail = "Prometheus Provider Gateway is unavailable"
		catalog.Source.Identity = providerConfig.Endpoint
		return catalog, nil
	}
	catalog.Source = providerCatalog.Source
	catalog.MetricNames = nonNilStrings(providerCatalog.MetricNames)
	catalog.Partial, catalog.ProviderState = providerCatalog.Partial, ProviderAvailable
	if providerCatalog.Partial {
		catalog.ProviderState = ProviderPartial
		catalog.ProviderDetail = "Prometheus metric catalog is partial"
	}
	if catalog.Source.CollectedAt.IsZero() {
		catalog.Source.CollectedAt = collectedAt
	}
	catalog.CollectedAt = catalog.Source.CollectedAt
	return catalog, nil
}

func (s *Service) StartOwner(ctx context.Context, request StartQueryRequest) (Execution, error) {
	revision, err := s.revisions.ActiveRevision(ctx)
	if err != nil {
		return Execution{}, fmt.Errorf("load active Configuration Revision: %w", err)
	}
	prepared, err := PrepareOwnerQuery(request, revision)
	if err != nil {
		return Execution{}, err
	}
	if prepared.DefinitionID != "" {
		definition, definitionErr := s.store.Definition(ctx, prepared.DefinitionID)
		if definitionErr != nil {
			return Execution{}, definitionErr
		}
		prepared, err = bindOwnerDefinition(prepared, definition)
		if err != nil {
			return Execution{}, err
		}
	}
	execution, err := s.store.CreateExecution(ctx, prepared)
	if err != nil {
		return Execution{}, err
	}
	s.launch(execution.ID, prepared)
	return s.decorateExecution(ctx, execution)
}

func (s *Service) StartAgent(ctx context.Context, request AgentQueryRequest) (Execution, error) {
	authorization, err := s.store.Authorization(ctx, strings.TrimSpace(request.AuthorizationID))
	if err != nil {
		return Execution{}, err
	}
	revision, err := s.revisions.Revision(ctx, authorization.ConfigurationRevision)
	if err != nil {
		return Execution{}, fmt.Errorf("load authorized Configuration Revision: %w", err)
	}
	prepared, err := PrepareAgentQuery(request, authorization, revision)
	if err != nil {
		return Execution{}, err
	}
	execution, err := s.store.CreateAuthorizedExecution(ctx, prepared)
	if err != nil {
		return Execution{}, err
	}
	s.launch(execution.ID, prepared)
	return s.decorateExecution(ctx, execution)
}

func (s *Service) launch(executionID string, prepared PreparedQuery) {
	queryCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[executionID] = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.cancels, executionID)
			s.mu.Unlock()
		}()
		select {
		case s.semaphore <- struct{}{}:
			defer func() { <-s.semaphore }()
		case <-queryCtx.Done():
			return
		}
		if err := s.store.MarkRunning(queryCtx, executionID); err != nil {
			return
		}
		boundedCtx, boundedCancel := context.WithTimeout(queryCtx, time.Duration(prepared.Bounds.TimeoutMS)*time.Millisecond)
		result, queryErr := s.provider.Query(boundedCtx, ProviderQueryRequest{
			ConfigurationRevision: prepared.ConfigurationRevision, Scope: prepared.Scope,
			Resource: prepared.Resource, Query: prepared.Query, QueryHash: prepared.QueryHash,
			TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
		})
		boundedCancel()
		if completeErr := s.store.Complete(context.Background(), executionID, result, queryErr); completeErr != nil {
			return
		}
		if queryErr == nil {
			s.mu.Lock()
			s.results[executionID] = cloneQueryResult(result.Result)
			s.mu.Unlock()
		}
	}()
}

func (s *Service) Execution(ctx context.Context, publicID string) (Execution, error) {
	execution, err := s.store.Execution(ctx, strings.TrimSpace(publicID))
	if err != nil {
		return Execution{}, err
	}
	if execution.Status == ExecutionSucceeded {
		s.mu.RLock()
		result, exists := s.results[execution.ID]
		s.mu.RUnlock()
		if exists {
			copy := cloneQueryResult(result)
			execution.Result = &copy
		} else {
			execution.ResultExpired = true
		}
	}
	return s.decorateExecution(ctx, execution)
}

func (s *Service) Executions(ctx context.Context, filter HistoryFilter) ([]Execution, error) {
	items, err := s.store.Executions(ctx, filter)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Links = []ContextLink{internalExecutionLink(items[index])}
		items[index].Events = []ExecutionEvent{}
	}
	return items, nil
}

func (s *Service) Cancel(ctx context.Context, publicID string) (Execution, error) {
	publicID = strings.TrimSpace(publicID)
	if err := s.store.Cancel(ctx, publicID); err != nil {
		return Execution{}, err
	}
	s.mu.RLock()
	cancel := s.cancels[publicID]
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	return s.Execution(ctx, publicID)
}

func (s *Service) SaveDefinition(ctx context.Context, request SaveDefinitionRequest) (Definition, error) {
	definition, err := s.store.CreateDefinition(ctx, request)
	if err != nil {
		return Definition{}, err
	}
	definition.Links = []ContextLink{internalDefinitionLink(definition)}
	return definition, nil
}

func (s *Service) Definitions(ctx context.Context, limit int) ([]Definition, error) {
	items, err := s.store.Definitions(ctx, limit)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Links = []ContextLink{internalDefinitionLink(items[index])}
	}
	return items, nil
}

func (s *Service) CreateAuthorization(ctx context.Context, request CreateAuthorizationRequest) (Authorization, error) {
	return s.store.CreateAuthorization(ctx, request)
}

func (s *Service) Authorizations(ctx context.Context, limit int) ([]Authorization, error) {
	return s.store.Authorizations(ctx, limit)
}

func (s *Service) RevokeAuthorization(ctx context.Context, publicID string) error {
	return s.store.RevokeAuthorization(ctx, publicID)
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.RLock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.mu.RUnlock()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) decorateExecution(ctx context.Context, execution Execution) (Execution, error) {
	execution.Links = []ContextLink{internalExecutionLink(execution)}
	revision, err := s.revisions.Revision(ctx, execution.ConfigurationRevision)
	if err != nil {
		return execution, nil
	}
	provider, err := prometheusConfiguration(revision)
	if err != nil || strings.TrimSpace(provider.ContextLinkBase) == "" {
		return execution, nil
	}
	link, err := grafanaExecutionLink(provider.ContextLinkBase, execution)
	if err == nil {
		execution.Links = append(execution.Links, link)
	} else {
		execution.Links = append(execution.Links, ContextLink{
			Kind: "provider", Label: "在 Grafana 中打开", Target: "external", Provider: "grafana",
			ResourceRef: execution.Resource.ID, From: execution.TimeRange.From, To: execution.TimeRange.To,
			Availability: "misconfigured",
		})
	}
	return execution, nil
}

func internalExecutionLink(execution Execution) ContextLink {
	query := url.Values{
		"cluster": []string{execution.Scope.ClusterID}, "namespace": []string{execution.Resource.Namespace},
		"resource": []string{execution.Resource.ID}, "from": []string{execution.TimeRange.From.UTC().Format(time.RFC3339Nano)},
		"to": []string{execution.TimeRange.To.UTC().Format(time.RFC3339Nano)}, "execution": []string{execution.ID},
	}
	return ContextLink{
		Kind: "internal", Label: "打开监控查询", Href: "/monitoring?" + query.Encode(), Target: "current",
		Provider: "prometheus", ResourceRef: execution.Resource.ID, From: execution.TimeRange.From,
		To: execution.TimeRange.To, Availability: "available",
	}
}

func internalDefinitionLink(definition Definition) ContextLink {
	query := url.Values{
		"cluster": []string{definition.Scope.ClusterID}, "namespace": []string{definition.Resource.Namespace},
		"resource": []string{definition.Resource.ID}, "definition": []string{definition.ID},
	}
	return ContextLink{
		Kind: "internal", Label: "打开 Query Definition", Href: "/monitoring?" + query.Encode(),
		Target: "current", Provider: "prometheus", ResourceRef: definition.Resource.ID,
		Availability: "available",
	}
}

func grafanaExecutionLink(rawBase string, execution Execution) (ContextLink, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBase), "/"))
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return ContextLink{}, errors.New("invalid Grafana Context Link base")
	}
	host := strings.ToLower(base.Hostname())
	if base.Scheme != "https" && (base.Scheme != "http" || !isLoopbackHost(host)) {
		return ContextLink{}, errors.New("grafana Context Link base is not allowed")
	}
	state := struct {
		Datasource string `json:"datasource"`
		Queries    []struct {
			Expr  string `json:"expr"`
			RefID string `json:"refId"`
			Range bool   `json:"range"`
		} `json:"queries"`
		Range struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"range"`
	}{Datasource: "Prometheus"}
	state.Queries = append(state.Queries, struct {
		Expr  string `json:"expr"`
		RefID string `json:"refId"`
		Range bool   `json:"range"`
	}{Expr: execution.Query, RefID: "A", Range: true})
	state.Range.From, state.Range.To = execution.TimeRange.From.UTC().Format(time.RFC3339Nano), execution.TimeRange.To.UTC().Format(time.RFC3339Nano)
	encoded, _ := json.Marshal(state)
	base.Path = strings.TrimRight(base.Path, "/") + "/explore"
	base.RawQuery = url.Values{"orgId": []string{"1"}, "left": []string{string(encoded)}}.Encode()
	return ContextLink{
		Kind: "provider", Label: "在 Grafana 中打开精确查询", Href: base.String(), Target: "external",
		Provider: "grafana", ResourceRef: execution.Resource.ID, From: execution.TimeRange.From,
		To: execution.TimeRange.To, Availability: "available",
	}, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func cloneQueryResult(value QueryResult) QueryResult {
	result := QueryResult{ResultType: value.ResultType, Series: make([]QuerySeries, len(value.Series))}
	for index, series := range value.Series {
		labels := make(map[string]string, len(series.Labels))
		for key, item := range series.Labels {
			labels[key] = item
		}
		result.Series[index] = QuerySeries{Labels: labels, Points: append([]QueryPoint(nil), series.Points...)}
	}
	return result
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

var _ Store = (*Repository)(nil)
var _ RevisionStore = (*settings.Service)(nil)
