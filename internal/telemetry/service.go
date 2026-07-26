package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const (
	ephemeralResultTTL   = 5 * time.Minute
	maximumCachedResults = 128
)

type cachedLogResult struct {
	value     ProviderLogResult
	expiresAt time.Time
}

type cachedTraceSearchResult struct {
	value     ProviderTraceSearchResult
	expiresAt time.Time
}

type cachedTraceDetailResult struct {
	value     ProviderTraceDetailResult
	expiresAt time.Time
}

type RevisionStore interface {
	ActiveRevision(context.Context) (settings.Revision, error)
	Revision(context.Context, string) (settings.Revision, error)
}

type Service struct {
	repository *Repository
	revisions  RevisionStore
	provider   Provider
	semaphore  chan struct{}
	now        func() time.Time

	mu      sync.RWMutex
	logs    map[string]cachedLogResult
	traces  map[string]cachedTraceSearchResult
	details map[string]cachedTraceDetailResult
}

func NewService(repository *Repository, revisions RevisionStore, provider Provider) (*Service, error) {
	if repository == nil || revisions == nil || provider == nil {
		return nil, errors.New("telemetry service requires repository, Configuration Revision store, and Provider Gateway")
	}
	return &Service{
		repository: repository, revisions: revisions, provider: provider, semaphore: make(chan struct{}, 2),
		now: time.Now, logs: make(map[string]cachedLogResult), traces: make(map[string]cachedTraceSearchResult),
		details: make(map[string]cachedTraceDetailResult),
	}, nil
}

func (s *Service) Catalog(ctx context.Context, providerName string, request CatalogRequest) (Catalog, error) {
	revision, err := s.revisions.ActiveRevision(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("load active Configuration Revision: %w", err)
	}
	provider, providerID, hardLimit, err := telemetryProvider(revision, providerName)
	if err != nil {
		return Catalog{}, err
	}
	scope, resource, err := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if err != nil {
		return Catalog{}, err
	}
	collectedAt := s.now().UTC()
	catalog := Catalog{
		Provider: providerName, ConfigurationRevision: revision.ID, Scope: scope, Resource: resource,
		ProviderState: ProviderAvailable, ProviderDetail: providerName + " bounded adapter available",
		Source: ProviderSource{Provider: providerName, Identity: provider.Endpoint, CollectedAt: collectedAt},
		Bounds: providerBounds(revision, provider, hardLimit), CollectedAt: collectedAt,
	}
	if !provider.Enabled {
		catalog.ProviderState = ProviderDisabled
		catalog.ProviderDetail = providerName + " is disabled in the active Configuration Revision"
		return catalog, nil
	}
	providerCatalog, err := s.provider.Catalog(ctx, ProviderCatalogRequest{
		Provider: providerName, ConfigurationRevision: revision.ID, Scope: scope, Resource: resource,
		Bounds: catalog.Bounds,
	})
	if err != nil {
		catalog.ProviderState = ProviderUnavailable
		catalog.ProviderDetail = providerName + " Provider Gateway is unavailable"
		return catalog, nil
	}
	catalog.Source, catalog.CollectedAt = providerCatalog.Source, providerCatalog.Source.CollectedAt
	if catalog.Source.Provider == "" {
		catalog.Source.Provider = string(providerID)
	}
	if catalog.CollectedAt.IsZero() {
		catalog.CollectedAt = collectedAt
		catalog.Source.CollectedAt = collectedAt
	}
	if providerCatalog.Partial {
		catalog.ProviderState, catalog.ProviderDetail = ProviderPartial, providerName+" Provider catalog is partial"
	}
	return catalog, nil
}

func (s *Service) QueryLogs(ctx context.Context, request StartLogQueryRequest) (LogQuery, error) {
	revision, err := s.revisions.ActiveRevision(ctx)
	if err != nil {
		return LogQuery{}, err
	}
	prepared, err := prepareLogQuery(request, revision)
	if err != nil {
		return LogQuery{}, err
	}
	execution, err := s.repository.CreateExecution(ctx, prepared)
	if err != nil {
		return LogQuery{}, err
	}
	if err = s.repository.MarkRunning(ctx, execution.ID); err != nil {
		return LogQuery{}, err
	}
	result, queryErr := withPermit(ctx, s.semaphore, func(queryCtx context.Context) (ProviderLogResult, error) {
		boundedCtx, cancel := context.WithTimeout(queryCtx, time.Duration(prepared.Bounds.TimeoutMS)*time.Millisecond)
		defer cancel()
		return s.provider.QueryLogs(boundedCtx, ProviderLogRequest{
			ConfigurationRevision: prepared.ConfigurationRevision, Scope: prepared.Scope, Resource: prepared.Resource,
			Query: prepared.Query, TimeRange: prepared.TimeRange, Bounds: prepared.Bounds, Tail: prepared.Kind == "logs_tail",
		})
	})
	completeErr := s.completeExecution(ctx, execution.ID, "logs", result.Source, len(result.Entries), result.ResponseBytes, result.Partial, result.Truncated, queryErr)
	if queryErr != nil {
		return LogQuery{}, queryErr
	}
	if completeErr != nil {
		return LogQuery{}, completeErr
	}
	result.Entries = s.decorateLogEntries(execution.ID, prepared, result.Entries)
	s.cacheLog(execution.ID, result)
	execution, err = s.repository.Execution(ctx, execution.ID, "elasticsearch")
	if err != nil {
		return LogQuery{}, err
	}
	return s.logProjection(ctx, execution, &result), nil
}

func (s *Service) LogQuery(ctx context.Context, id string) (LogQuery, error) {
	execution, err := s.repository.Execution(ctx, strings.TrimSpace(id), "elasticsearch")
	if err != nil {
		return LogQuery{}, err
	}
	result, ok := s.cachedLog(execution.ID)
	if ok {
		return s.logProjection(ctx, execution, &result), nil
	}
	return s.logProjection(ctx, execution, nil), nil
}

func (s *Service) LogQueries(ctx context.Context, clusterID, namespace, resourceID string, limit int) ([]LogQuery, error) {
	items, err := s.repository.Executions(ctx, "elasticsearch", strings.TrimSpace(clusterID), strings.TrimSpace(namespace), strings.TrimSpace(resourceID), limit)
	if err != nil {
		return nil, err
	}
	result := make([]LogQuery, 0, len(items))
	for _, execution := range items {
		result = append(result, s.logProjection(ctx, execution, nil))
	}
	return result, nil
}

func (s *Service) SearchTraces(ctx context.Context, request StartTraceSearchRequest) (TraceSearch, error) {
	revision, err := s.revisions.ActiveRevision(ctx)
	if err != nil {
		return TraceSearch{}, err
	}
	prepared, err := prepareTraceSearch(request, revision)
	if err != nil {
		return TraceSearch{}, err
	}
	execution, err := s.repository.CreateExecution(ctx, prepared)
	if err != nil {
		return TraceSearch{}, err
	}
	if err = s.repository.MarkRunning(ctx, execution.ID); err != nil {
		return TraceSearch{}, err
	}
	result, queryErr := withPermit(ctx, s.semaphore, func(queryCtx context.Context) (ProviderTraceSearchResult, error) {
		boundedCtx, cancel := context.WithTimeout(queryCtx, time.Duration(prepared.Bounds.TimeoutMS)*time.Millisecond)
		defer cancel()
		return s.provider.SearchTraces(boundedCtx, ProviderTraceSearchRequest{
			ConfigurationRevision: prepared.ConfigurationRevision, Scope: prepared.Scope, Resource: prepared.Resource,
			Query: prepared.Query, TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
		})
	})
	completeErr := s.completeExecution(ctx, execution.ID, "traces", result.Source, len(result.Traces), result.ResponseBytes, result.Partial, result.Truncated, queryErr)
	if queryErr != nil {
		return TraceSearch{}, queryErr
	}
	if completeErr != nil {
		return TraceSearch{}, completeErr
	}
	result.Traces = s.decorateTraceSummaries(execution.ID, prepared, result.Traces)
	s.cacheTraceSearch(execution.ID, result)
	execution, err = s.repository.Execution(ctx, execution.ID, "tempo")
	if err != nil {
		return TraceSearch{}, err
	}
	return s.traceSearchProjection(ctx, execution, &result), nil
}

func (s *Service) TraceSearch(ctx context.Context, id string) (TraceSearch, error) {
	execution, err := s.repository.Execution(ctx, strings.TrimSpace(id), "tempo")
	if err != nil {
		return TraceSearch{}, err
	}
	result, ok := s.cachedTraceSearch(execution.ID)
	if ok {
		return s.traceSearchProjection(ctx, execution, &result), nil
	}
	return s.traceSearchProjection(ctx, execution, nil), nil
}

func (s *Service) TraceSearches(ctx context.Context, clusterID, namespace, resourceID string, limit int) ([]TraceSearch, error) {
	items, err := s.repository.Executions(ctx, "tempo", strings.TrimSpace(clusterID), strings.TrimSpace(namespace), strings.TrimSpace(resourceID), limit)
	if err != nil {
		return nil, err
	}
	result := make([]TraceSearch, 0, len(items))
	for _, execution := range items {
		result = append(result, s.traceSearchProjection(ctx, execution, nil))
	}
	return result, nil
}

func (s *Service) Trace(ctx context.Context, request TraceDetailRequest) (TraceDetail, error) {
	var prepared preparedQuery
	traceID := strings.ToLower(strings.TrimSpace(request.TraceID))
	if !traceIDPattern.MatchString(traceID) {
		return TraceDetail{}, ErrInvalid
	}
	if strings.TrimSpace(request.SearchID) != "" {
		parent, err := s.repository.Execution(ctx, request.SearchID, "tempo")
		if err != nil {
			return TraceDetail{}, err
		}
		if parent.Kind != "trace_search" || parent.Status != "succeeded" || parent.ResultType != "traces" {
			return TraceDetail{}, ErrConflict
		}
		parentPrepared := preparedFromExecution(parent)
		prepared = newPrepared("tempo", "trace_detail", parent.ConfigurationRevision, ModeGuided, "trace_id="+traceID,
			parentPrepared.Scope, parent.Resource, parent.TimeRange, parent.Bounds)
	} else {
		revision, err := s.revisions.ActiveRevision(ctx)
		if err != nil {
			return TraceDetail{}, err
		}
		var prepareErr error
		prepared, traceID, prepareErr = prepareTraceDetail(request, revision)
		if prepareErr != nil {
			return TraceDetail{}, prepareErr
		}
	}
	execution, err := s.repository.CreateExecution(ctx, prepared)
	if err != nil {
		return TraceDetail{}, err
	}
	if err = s.repository.MarkRunning(ctx, execution.ID); err != nil {
		return TraceDetail{}, err
	}
	cacheKey := execution.ID + ":" + traceID
	result, queryErr := withPermit(ctx, s.semaphore, func(queryCtx context.Context) (ProviderTraceDetailResult, error) {
		boundedCtx, cancel := context.WithTimeout(queryCtx, time.Duration(prepared.Bounds.TimeoutMS)*time.Millisecond)
		defer cancel()
		return s.provider.Trace(boundedCtx, ProviderTraceDetailRequest{
			ConfigurationRevision: prepared.ConfigurationRevision, Scope: prepared.Scope,
			Resource: prepared.Resource, TraceID: traceID, TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
		})
	})
	completeErr := s.completeExecution(ctx, execution.ID, "trace", result.Source, len(result.Detail.Spans), result.ResponseBytes, result.Partial, result.Truncated, queryErr)
	if completeErr != nil && queryErr == nil {
		return TraceDetail{}, completeErr
	}
	if queryErr != nil {
		return TraceDetail{}, queryErr
	}
	result.Detail.Spans = s.decorateSpans(execution.ID, prepared, traceID, result.Detail.Spans)
	s.cacheTraceDetail(cacheKey, result)
	return s.traceDetailProjection(execution.ID, prepared, result), nil
}

func (s *Service) SaveLogEvidence(ctx context.Context, queryID string, request SaveEvidenceRequest) (Evidence, error) {
	execution, err := s.repository.Execution(ctx, strings.TrimSpace(queryID), "elasticsearch")
	if err != nil {
		return Evidence{}, err
	}
	result, ok := s.cachedLog(execution.ID)
	if !ok {
		return Evidence{}, ErrResultExpired
	}
	selected := selectLogEntries(result.Entries, request.ItemIDs)
	if len(selected) == 0 {
		return Evidence{}, fmt.Errorf("%w: at least one current log row must be selected", ErrInvalid)
	}
	facts, truncated, err := boundedEvidenceFacts("log", selected)
	if err != nil {
		return Evidence{}, err
	}
	return s.retainEvidence(ctx, execution, "log_selection", fmt.Sprintf("保留 %d 条 Elasticsearch 日志片段", len(selected)), facts, len(selected), result.Source, truncated || result.Truncated, selected[0].Timestamp)
}

func (s *Service) SaveTraceEvidence(ctx context.Context, queryID, traceID string, request SaveEvidenceRequest) (Evidence, error) {
	execution, err := s.repository.Execution(ctx, strings.TrimSpace(queryID), "tempo")
	if err != nil {
		return Evidence{}, err
	}
	cacheKey := execution.ID + ":" + strings.ToLower(strings.TrimSpace(traceID))
	result, ok := s.cachedTraceDetail(cacheKey)
	if !ok {
		return Evidence{}, ErrResultExpired
	}
	selected := selectSpans(result.Detail.Spans, request.ItemIDs)
	if len(selected) == 0 {
		return Evidence{}, fmt.Errorf("%w: at least one current span must be selected", ErrInvalid)
	}
	facts, truncated, err := boundedEvidenceFacts("trace", selected)
	if err != nil {
		return Evidence{}, err
	}
	observedAt := result.Detail.StartTime
	return s.retainEvidence(ctx, execution, "trace_selection", fmt.Sprintf("保留 Trace %s 的 %d 个 span", traceID, len(selected)), facts, len(selected), result.Source, truncated || result.Truncated, observedAt)
}

func (s *Service) CreateConsultation(ctx context.Context, request CreateConsultationRequest) (Consultation, error) {
	revision, err := s.revisions.ActiveRevision(ctx)
	if err != nil {
		return Consultation{}, err
	}
	request.Title = strings.TrimSpace(request.Title)
	if len(request.Title) < 2 || len(request.Title) > 128 || request.From.IsZero() || request.To.IsZero() || !request.To.After(request.From) {
		return Consultation{}, ErrInvalid
	}
	request.ClusterID, request.Environment = strings.TrimSpace(request.ClusterID), strings.TrimSpace(request.Environment)
	request.From, request.To = request.From.UTC(), request.To.UTC()
	request.Namespaces = stableStrings(request.Namespaces)
	request.QueryIDs, err = stableUUIDs(request.QueryIDs)
	if err != nil {
		return Consultation{}, err
	}
	request.EvidenceIDs, err = stableUUIDs(request.EvidenceIDs)
	if err != nil {
		return Consultation{}, err
	}
	if request.ClusterID == "" || request.Environment == "" || len(request.Namespaces) == 0 || len(request.Resources) == 0 || len(request.Resources) > 32 ||
		len(request.QueryIDs) > 32 || len(request.EvidenceIDs) > 32 || len(request.QueryIDs)+len(request.EvidenceIDs) == 0 ||
		request.To.Sub(request.From) > time.Duration(revision.General.QueryMaxLookbackSeconds)*time.Second {
		return Consultation{}, ErrInvalid
	}
	var scope *settings.OperationalScope
	for index := range revision.Scopes {
		candidate := revision.Scopes[index]
		if candidate.ClusterID == request.ClusterID && candidate.Environment == request.Environment {
			scope = &candidate
			break
		}
	}
	if scope == nil {
		return Consultation{}, ErrInvalid
	}
	for _, namespace := range request.Namespaces {
		if !slices.Contains(scope.Namespaces, namespace) {
			return Consultation{}, ErrInvalid
		}
	}
	request.Resources, err = normalizeResources(request.Resources, request.Namespaces)
	if err != nil {
		return Consultation{}, err
	}
	if err := s.repository.ValidateSnapshotReferences(ctx, revision.ID, request); err != nil {
		return Consultation{}, err
	}
	canonical, err := json.Marshal(request)
	if err != nil {
		return Consultation{}, err
	}
	return s.repository.CreateConsultation(ctx, revision.ID, request, sha256Bytes(canonical))
}

func (s *Service) retainEvidence(ctx context.Context, execution executionRecord, evidenceType, summary string, facts []byte, count int, source ProviderSource, truncated bool, observedAt time.Time) (Evidence, error) {
	scopeJSON, _ := json.Marshal(settingsScope(execution.Scope))
	arguments, _ := json.Marshal(struct {
		Query     string    `json:"query"`
		TimeRange TimeRange `json:"time_range"`
		Resource  string    `json:"resource_ref"`
	}{execution.Query, execution.TimeRange, execution.Resource.ID})
	provenance, _ := json.Marshal(map[string]any{
		"provider": execution.Provider, "identity": source.Identity, "server_version": source.ServerVersion,
		"collected_at": source.CollectedAt.UTC(), "query_execution_id": execution.ID,
	})
	return s.repository.RetainEvidence(ctx, execution, evidenceInsert{
		QueryID: execution.ID, Type: evidenceType, Summary: summary, Facts: facts, FactCount: count,
		ContentHash: sha256Bytes(facts), ScopeHash: sha256Bytes(scopeJSON), ArgumentsHash: sha256Bytes(arguments),
		Provenance: provenance, ProvenanceHash: sha256Bytes(provenance), Truncated: truncated, ObservedAt: observedAt,
	})
}

func (s *Service) completeExecution(ctx context.Context, executionID, resultType string, source ProviderSource, resultCount, responseBytes int, partial, truncated bool, queryErr error) error {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.repository.Complete(auditCtx, executionID, resultType, source, resultCount, responseBytes, partial, truncated, queryErr)
}

func (s *Service) cacheLog(id string, value ProviderLogResult) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	trimCache(s.logs, now, func(item cachedLogResult) time.Time { return item.expiresAt })
	s.logs[id] = cachedLogResult{value: cloneLogResult(value), expiresAt: now.Add(ephemeralResultTTL)}
}

func (s *Service) cachedLog(id string) (ProviderLogResult, bool) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.logs[id]
	if !ok || !item.expiresAt.After(now) {
		delete(s.logs, id)
		return ProviderLogResult{}, false
	}
	return cloneLogResult(item.value), true
}

func (s *Service) cacheTraceSearch(id string, value ProviderTraceSearchResult) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	trimCache(s.traces, now, func(item cachedTraceSearchResult) time.Time { return item.expiresAt })
	s.traces[id] = cachedTraceSearchResult{value: cloneTraceSearchResult(value), expiresAt: now.Add(ephemeralResultTTL)}
}

func (s *Service) cachedTraceSearch(id string) (ProviderTraceSearchResult, bool) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.traces[id]
	if !ok || !item.expiresAt.After(now) {
		delete(s.traces, id)
		return ProviderTraceSearchResult{}, false
	}
	return cloneTraceSearchResult(item.value), true
}

func (s *Service) cacheTraceDetail(key string, value ProviderTraceDetailResult) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	trimCache(s.details, now, func(item cachedTraceDetailResult) time.Time { return item.expiresAt })
	s.details[key] = cachedTraceDetailResult{value: cloneTraceDetailResult(value), expiresAt: now.Add(ephemeralResultTTL)}
}

func (s *Service) cachedTraceDetail(key string) (ProviderTraceDetailResult, bool) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.details[key]
	if !ok || !item.expiresAt.After(now) {
		delete(s.details, key)
		return ProviderTraceDetailResult{}, false
	}
	return cloneTraceDetailResult(item.value), true
}

func trimCache[T any](items map[string]T, now time.Time, expiresAt func(T) time.Time) {
	for key, item := range items {
		if !expiresAt(item).After(now) {
			delete(items, key)
		}
	}
	if len(items) < maximumCachedResults {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, item := range items {
		expires := expiresAt(item)
		if oldestKey == "" || expires.Before(oldest) {
			oldestKey, oldest = key, expires
		}
	}
	delete(items, oldestKey)
}

func (s *Service) logProjection(ctx context.Context, execution executionRecord, result *ProviderLogResult) LogQuery {
	item := LogQuery{
		ID: execution.ID, ConfigurationRevision: execution.ConfigurationRevision, Provider: execution.Provider,
		Mode: execution.Mode, Query: execution.Query, QueryHash: execution.QueryHash,
		Scope: settingsScope(execution.Scope), Resource: execution.Resource, TimeRange: execution.TimeRange,
		Bounds: execution.Bounds, Status: execution.Status, Source: execution.Source,
		ResultCount: execution.ResultCount, ResponseBytes: execution.ResponseBytes, Partial: execution.Partial,
		Truncated: execution.Truncated, ErrorCode: execution.ErrorCode, ErrorDetail: execution.ErrorDetail,
		CreatedAt: execution.CreatedAt, CompletedAt: execution.CompletedAt,
		Tail:      execution.Kind == "logs_tail",
		Histogram: []HistogramBucket{}, Entries: []LogEntry{}, Fields: []string{},
	}
	item.Links = s.executionLinks(ctx, execution)
	item.Stale = !item.Source.CollectedAt.IsZero() && s.now().Sub(item.Source.CollectedAt) > 5*time.Minute
	if result != nil {
		item.Source, item.Histogram, item.Entries, item.Fields = result.Source, cloneHistogram(result.Histogram), cloneLogEntries(result.Entries), append([]string(nil), result.Fields...)
		item.ResultCount, item.ResponseBytes, item.Partial, item.Truncated = len(result.Entries), result.ResponseBytes, result.Partial, result.Truncated
	} else if execution.Status == "succeeded" {
		item.ResultExpired = true
	}
	return item
}

func (s *Service) traceSearchProjection(ctx context.Context, execution executionRecord, result *ProviderTraceSearchResult) TraceSearch {
	item := TraceSearch{
		ID: execution.ID, ConfigurationRevision: execution.ConfigurationRevision, Provider: execution.Provider,
		Mode: execution.Mode, Query: execution.Query, QueryHash: execution.QueryHash,
		Scope: settingsScope(execution.Scope), Resource: execution.Resource, TimeRange: execution.TimeRange,
		Bounds: execution.Bounds, Status: execution.Status, Source: execution.Source,
		ResultCount: execution.ResultCount, ResponseBytes: execution.ResponseBytes, Partial: execution.Partial,
		Truncated: execution.Truncated, ErrorCode: execution.ErrorCode, ErrorDetail: execution.ErrorDetail,
		CreatedAt: execution.CreatedAt, CompletedAt: execution.CompletedAt, Traces: []TraceSummary{},
	}
	item.Links = s.executionLinks(ctx, execution)
	item.Stale = !item.Source.CollectedAt.IsZero() && s.now().Sub(item.Source.CollectedAt) > 5*time.Minute
	if result != nil {
		item.Source, item.Traces = result.Source, cloneTraceSummaries(result.Traces)
		item.ResultCount, item.ResponseBytes, item.Partial, item.Truncated = len(result.Traces), result.ResponseBytes, result.Partial, result.Truncated
	} else if execution.Status == "succeeded" {
		item.ResultExpired = true
	}
	return item
}

func (s *Service) traceDetailProjection(queryID string, prepared preparedQuery, result ProviderTraceDetailResult) TraceDetail {
	item := cloneTraceDetail(result.Detail)
	item.QueryID, item.ConfigurationRevision = queryID, prepared.ConfigurationRevision
	item.Scope, item.Resource, item.TimeRange, item.Source = prepared.Scope, prepared.Resource, prepared.TimeRange, result.Source
	item.ResponseBytes, item.Partial, item.Truncated = result.ResponseBytes, result.Partial, result.Truncated
	item.Links = []ContextLink{resourceLink(prepared), logLink(prepared, item.TraceID, item.StartTime, item.StartTime.Add(time.Duration(item.DurationMS)*time.Millisecond))}
	if link, ok := s.tempoLink(prepared, item.TraceID); ok {
		item.Links = append(item.Links, link)
	}
	return item
}

func (s *Service) executionLinks(ctx context.Context, execution executionRecord) []ContextLink {
	prepared := preparedFromExecution(execution)
	links := []ContextLink{resourceLink(prepared)}
	query := url.Values{
		"cluster": []string{execution.Scope.ClusterID}, "namespace": []string{execution.Resource.Namespace},
		"resource": []string{execution.Resource.ID}, "from": []string{execution.TimeRange.From.UTC().Format(time.RFC3339Nano)},
		"to": []string{execution.TimeRange.To.UTC().Format(time.RFC3339Nano)}, "query": []string{execution.ID},
	}
	workspace := "/logs"
	label := "打开日志查询"
	if execution.Provider == "tempo" {
		workspace, label = "/traces", "打开 Trace 查询"
	}
	links = append([]ContextLink{{
		Kind: "internal", Label: label, Href: workspace + "?" + query.Encode(), Target: "current",
		Provider: execution.Provider, ResourceRef: execution.Resource.ID, From: execution.TimeRange.From,
		To: execution.TimeRange.To, Availability: "available",
	}}, links...)
	revision, err := s.revisions.Revision(ctx, execution.ConfigurationRevision)
	if err != nil {
		return links
	}
	providerName := settings.ProviderElasticsearch
	if execution.Provider == "tempo" {
		providerName = settings.ProviderTempo
	}
	provider, err := providerConfiguration(revision, providerName)
	if err != nil || strings.TrimSpace(provider.ContextLinkBase) == "" {
		return links
	}
	if execution.Provider == "elasticsearch" {
		if link, ok := kibanaLink(provider.ContextLinkBase, prepared); ok {
			links = append(links, link)
		}
	}
	return links
}

func (s *Service) decorateLogEntries(queryID string, prepared preparedQuery, entries []LogEntry) []LogEntry {
	result := cloneLogEntries(entries)
	for index := range result {
		result[index].Links = []ContextLink{resourceLink(prepared), monitoringLink(prepared, result[index].Timestamp)}
		if traceIDPattern.MatchString(result[index].TraceID) {
			result[index].Links = append(result[index].Links, traceLink(queryID, prepared, result[index].TraceID, result[index].Timestamp))
		}
	}
	return result
}

func (s *Service) decorateTraceSummaries(queryID string, prepared preparedQuery, traces []TraceSummary) []TraceSummary {
	result := cloneTraceSummaries(traces)
	for index := range result {
		result[index].Resource = prepared.Resource
		result[index].Links = []ContextLink{traceLink(queryID, prepared, result[index].TraceID, result[index].StartTime), resourceLink(prepared)}
	}
	return result
}

func (s *Service) decorateSpans(queryID string, prepared preparedQuery, traceID string, spans []Span) []Span {
	result := cloneSpans(spans)
	for index := range result {
		result[index].Resource = prepared.Resource
		end := result[index].StartTime.Add(time.Duration(result[index].DurationMS) * time.Millisecond)
		result[index].Links = []ContextLink{logLink(prepared, traceID, result[index].StartTime, end), resourceLink(prepared)}
	}
	return result
}

func telemetryProvider(revision settings.Revision, name string) (settings.ProviderConfiguration, settings.Provider, int, error) {
	switch strings.TrimSpace(name) {
	case "elasticsearch":
		provider, err := providerConfiguration(revision, settings.ProviderElasticsearch)
		return provider, settings.ProviderElasticsearch, MaximumLogRows, err
	case "tempo":
		provider, err := providerConfiguration(revision, settings.ProviderTempo)
		return provider, settings.ProviderTempo, MaximumTraceCount, err
	default:
		return settings.ProviderConfiguration{}, "", 0, ErrInvalid
	}
}

func preparedFromExecution(execution executionRecord) preparedQuery {
	return preparedQuery{
		Provider: execution.Provider, Kind: execution.Kind, ConfigurationRevision: execution.ConfigurationRevision,
		Mode: execution.Mode, Query: execution.Query, QueryHash: execution.QueryHash,
		Scope: settingsScope(execution.Scope), Resource: execution.Resource,
		TimeRange: execution.TimeRange, Bounds: execution.Bounds,
	}
}

func resourceLink(prepared preparedQuery) ContextLink {
	query := url.Values{
		"cluster": []string{prepared.Scope.ClusterID}, "namespace": []string{prepared.Resource.Namespace},
		"resource": []string{prepared.Resource.ID}, "from": []string{prepared.TimeRange.From.UTC().Format(time.RFC3339Nano)},
		"to": []string{prepared.TimeRange.To.UTC().Format(time.RFC3339Nano)},
	}
	return ContextLink{Kind: "internal", Label: "打开 Workload", Href: "/infrastructure?" + query.Encode(), Target: "current", ResourceRef: prepared.Resource.ID, From: prepared.TimeRange.From, To: prepared.TimeRange.To, Availability: "available"}
}

func monitoringLink(prepared preparedQuery, at time.Time) ContextLink {
	query := contextQuery(prepared, at.Add(-5*time.Minute), at.Add(5*time.Minute))
	return ContextLink{Kind: "internal", Label: "查看相关 Metrics", Href: "/monitoring?" + query.Encode(), Target: "current", Provider: "prometheus", ResourceRef: prepared.Resource.ID, From: at.Add(-5 * time.Minute), To: at.Add(5 * time.Minute), Availability: "available"}
}

func traceLink(queryID string, prepared preparedQuery, traceID string, at time.Time) ContextLink {
	from, to := at.Add(-5*time.Minute), at.Add(5*time.Minute)
	query := contextQuery(prepared, from, to)
	query.Set("trace_id", traceID)
	if prepared.Provider == "tempo" {
		query.Set("search", queryID)
	}
	return ContextLink{Kind: "internal", Label: "打开 Trace", Href: "/traces?" + query.Encode(), Target: "current", Provider: "tempo", ResourceRef: traceID, From: from, To: to, Availability: "available"}
}

func logLink(prepared preparedQuery, traceID string, from, to time.Time) ContextLink {
	from, to = from.Add(-time.Second), to.Add(time.Second)
	query := contextQuery(prepared, from, to)
	query.Set("trace_id", traceID)
	return ContextLink{Kind: "internal", Label: "查看关联日志", Href: "/logs?" + query.Encode(), Target: "current", Provider: "elasticsearch", ResourceRef: prepared.Resource.ID, From: from, To: to, Availability: "available"}
}

func contextQuery(prepared preparedQuery, from, to time.Time) url.Values {
	return url.Values{
		"cluster": []string{prepared.Scope.ClusterID}, "namespace": []string{prepared.Resource.Namespace},
		"resource": []string{prepared.Resource.ID}, "from": []string{from.UTC().Format(time.RFC3339Nano)},
		"to": []string{to.UTC().Format(time.RFC3339Nano)},
	}
}

func kibanaLink(rawBase string, prepared preparedQuery) (ContextLink, bool) {
	base, ok := allowedLinkBase(rawBase)
	if !ok {
		return ContextLink{}, false
	}
	request := "GET logs-cloudops-*/_search\n" + prepared.Query
	base.Path = strings.TrimRight(base.Path, "/") + "/app/dev_tools"
	base.Fragment = "/console/shell?load_from=" + url.QueryEscape("data:text/plain,"+request)
	return ContextLink{Kind: "provider", Label: "在 Kibana 打开精确查询", Href: base.String(), Target: "external", Provider: "kibana", ResourceRef: prepared.Resource.ID, From: prepared.TimeRange.From, To: prepared.TimeRange.To, Availability: "available"}, true
}

func (s *Service) tempoLink(prepared preparedQuery, traceID string) (ContextLink, bool) {
	revision, err := s.revisions.Revision(context.Background(), prepared.ConfigurationRevision)
	if err != nil {
		return ContextLink{}, false
	}
	provider, err := providerConfiguration(revision, settings.ProviderTempo)
	if err != nil {
		return ContextLink{}, false
	}
	base, ok := allowedLinkBase(provider.ContextLinkBase)
	if !ok {
		return ContextLink{}, false
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/traces/" + traceID
	return ContextLink{Kind: "source", Label: "打开 Tempo 原始 Trace", Href: base.String(), Target: "external", Provider: "tempo", ResourceRef: traceID, From: prepared.TimeRange.From, To: prepared.TimeRange.To, Availability: "available"}, true
}

func allowedLinkBase(raw string) (*url.URL, bool) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, false
	}
	host := strings.ToLower(base.Hostname())
	if base.Scheme != "https" && (base.Scheme != "http" || !loopbackHost(host)) {
		return nil, false
	}
	return base, true
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func selectLogEntries(entries []LogEntry, ids []string) []LogEntry {
	ids = stableStrings(ids)
	if len(ids) == 0 || len(ids) > MaximumRetainedItems {
		return nil
	}
	selected := make([]LogEntry, 0, len(ids))
	for _, id := range ids {
		for _, entry := range entries {
			if entry.ID == id {
				entry.Links = nil
				selected = append(selected, entry)
				break
			}
		}
	}
	return selected
}

func selectSpans(spans []Span, ids []string) []Span {
	ids = stableStrings(ids)
	if len(ids) == 0 || len(ids) > MaximumRetainedItems {
		return nil
	}
	for index := range ids {
		ids[index] = strings.ToLower(ids[index])
		if !validSpanID(ids[index]) {
			return nil
		}
	}
	ids = stableStrings(ids)
	selected := make([]Span, 0, len(ids))
	for _, id := range ids {
		for _, span := range spans {
			if span.SpanID == id {
				span.Links = nil
				selected = append(selected, span)
				break
			}
		}
	}
	return selected
}

func boundedEvidenceFacts(kind string, items any) ([]byte, bool, error) {
	envelope := struct {
		Kind  string `json:"kind"`
		Facts any    `json:"facts"`
	}{Kind: kind, Facts: items}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	if len(encoded) <= MaximumRetainedBytes {
		return encoded, false, nil
	}
	return nil, true, fmt.Errorf("%w: selected Evidence exceeds %d bytes; select fewer items", ErrBoundExceeded, MaximumRetainedBytes)
}

func stableStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func stableUUIDs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range stableStrings(values) {
		if _, err := uuid.Parse(value); err != nil {
			return nil, fmt.Errorf("%w: Context Snapshot reference is not a UUID", ErrInvalid)
		}
		result = append(result, value)
	}
	return result, nil
}

func normalizeResources(values []ResourceReference, namespaces []string) ([]ResourceReference, error) {
	seen := make(map[ResourceReference]struct{}, len(values))
	result := make([]ResourceReference, 0, len(values))
	for _, value := range values {
		value.ID, value.Kind = strings.TrimSpace(value.ID), strings.TrimSpace(value.Kind)
		value.Namespace, value.Name = strings.TrimSpace(value.Namespace), strings.TrimSpace(value.Name)
		if value.ID == "" || value.Name == "" || !slices.Contains([]string{"Deployment", "StatefulSet", "DaemonSet"}, value.Kind) ||
			!slices.Contains(namespaces, value.Namespace) {
			return nil, fmt.Errorf("%w: Context Snapshot resource is outside its bounded Workload scope", ErrInvalid)
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func sha256Bytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func sha256Text(value string) string { return sha256Bytes([]byte(value)) }

func withPermit[T any](ctx context.Context, semaphore chan struct{}, action func(context.Context) (T, error)) (T, error) {
	var zero T
	select {
	case semaphore <- struct{}{}:
		defer func() { <-semaphore }()
		return action(ctx)
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func cloneLogResult(value ProviderLogResult) ProviderLogResult {
	value.Histogram = cloneHistogram(value.Histogram)
	value.Entries = cloneLogEntries(value.Entries)
	value.Fields = append([]string{}, value.Fields...)
	return value
}

func cloneTraceSearchResult(value ProviderTraceSearchResult) ProviderTraceSearchResult {
	value.Traces = cloneTraceSummaries(value.Traces)
	return value
}

func cloneTraceDetailResult(value ProviderTraceDetailResult) ProviderTraceDetailResult {
	value.Detail = cloneTraceDetail(value.Detail)
	return value
}

func cloneHistogram(values []HistogramBucket) []HistogramBucket {
	return append([]HistogramBucket{}, values...)
}

func cloneLogEntries(values []LogEntry) []LogEntry {
	result := make([]LogEntry, len(values))
	for index, item := range values {
		item.Attributes = cloneStringMap(item.Attributes)
		item.Links = append([]ContextLink(nil), item.Links...)
		result[index] = item
	}
	return result
}

func cloneTraceSummaries(values []TraceSummary) []TraceSummary {
	result := append([]TraceSummary{}, values...)
	for index := range result {
		result[index].Links = append([]ContextLink{}, result[index].Links...)
	}
	return result
}

func cloneSpans(values []Span) []Span {
	result := make([]Span, len(values))
	for index, item := range values {
		item.Attributes = cloneStringMap(item.Attributes)
		item.Events = append([]SpanEvent{}, item.Events...)
		for eventIndex := range item.Events {
			item.Events[eventIndex].Attributes = cloneStringMap(item.Events[eventIndex].Attributes)
		}
		item.Links = append([]ContextLink{}, item.Links...)
		result[index] = item
	}
	return result
}

func cloneTraceDetail(value TraceDetail) TraceDetail {
	value.Attributes = cloneStringMap(value.Attributes)
	value.Spans = cloneSpans(value.Spans)
	value.Links = append([]ContextLink{}, value.Links...)
	return value
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
