package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const snapshotFreshness = 30 * time.Second

type Service struct {
	configuration ConfigurationSource
	reader        Reader
	repository    SnapshotRepository
	bootstrapErr  error
	now           func() time.Time
}

func NewService(configuration ConfigurationSource, reader Reader, repository SnapshotRepository, bootstrapErr error) (*Service, error) {
	if configuration == nil {
		return nil, errors.New("infrastructure configuration source is required")
	}
	if repository == nil {
		return nil, errors.New("infrastructure snapshot repository is required")
	}
	return &Service{configuration: configuration, reader: reader, repository: repository, bootstrapErr: bootstrapErr, now: time.Now}, nil
}

func (s *Service) Topology(ctx context.Context, query Query) (TopologySnapshot, error) {
	revision, err := s.configuration.ActiveRevision(ctx)
	if err != nil {
		return TopologySnapshot{}, fmt.Errorf("load active configuration: %w", err)
	}
	query, namespaces, err := normalizeQuery(query, revision)
	if err != nil {
		return TopologySnapshot{}, err
	}
	provider, ok := providerConfiguration(revision, settings.ProviderKubernetes)
	if !ok || !provider.Enabled {
		snapshot := unavailableSnapshot(revision, namespaces, ProviderDisabled, "Kubernetes Provider 在活动 Configuration Revision 中已停用", s.now())
		_ = s.observeHealth(ctx, revision, snapshot.ProviderState, snapshot.ProviderDetail, nil)
		return snapshot, nil
	}
	if s.reader == nil {
		detail := "Kubernetes Provider client 未构建"
		if s.bootstrapErr != nil {
			detail = "Kubernetes Provider bootstrap 不可用"
		}
		snapshot := unavailableSnapshot(revision, namespaces, ProviderUnavailable, detail, s.now())
		_ = s.observeHealth(ctx, revision, snapshot.ProviderState, snapshot.ProviderDetail, &snapshot.CollectedAt)
		return snapshot, nil
	}

	limit := minPositive(query.Limit, provider.MaxResults, revision.General.QueryMaxResults, MaximumLimit)
	projection, readErr := s.reader.Read(ctx, ReadRequest{ClusterID: revision.Scope.ClusterID, Namespaces: namespaces, Limit: limit})
	if readErr != nil {
		now := s.now().UTC()
		snapshot := unavailableSnapshot(revision, namespaces, ProviderUnavailable, "Kubernetes API 查询失败", now)
		snapshot.Issues = []ProviderIssue{{Operation: "topology.read", Code: "PROVIDER_UNAVAILABLE", Detail: boundedDetail(readErr.Error())}}
		_ = s.observeHealth(ctx, revision, snapshot.ProviderState, snapshot.ProviderDetail, &now)
		return snapshot, nil
	}
	collectedAt := projection.Source.CollectedAt.UTC()
	if collectedAt.IsZero() {
		collectedAt = s.now().UTC()
		projection.Source.CollectedAt = collectedAt
	}
	state, detail := ProviderAvailable, "Kubernetes typed topology projection 可用"
	if projection.Partial || len(projection.Issues) > 0 {
		state, detail = ProviderPartial, "Kubernetes topology 部分可用；查看 Provider issues"
	}
	snapshot := TopologySnapshot{
		ConfigurationRevision: revision.ID,
		Scope:                 scopedRevision(revision, namespaces),
		ProviderState:         state, ProviderDetail: detail,
		Source:    projection.Source,
		Freshness: Freshness{State: "fresh", FreshUntil: collectedAt.Add(snapshotFreshness), AgeSeconds: maxInt64(0, int64(s.now().Sub(collectedAt).Seconds()))},
		Nodes:     nonNilResources(projection.Nodes), Edges: nonNilEdges(projection.Edges), Issues: nonNilIssues(projection.Issues),
		Partial: projection.Partial || len(projection.Issues) > 0, Truncated: projection.Truncated, CollectedAt: collectedAt,
	}
	for index := range snapshot.Nodes {
		snapshot.Nodes[index].Links = resourceLinks(snapshot.Nodes[index], query)
	}
	if err := s.repository.Store(ctx, revision.ID, &snapshot); err != nil {
		return TopologySnapshot{}, fmt.Errorf("persist topology snapshot: %w", err)
	}
	checkedAt := collectedAt
	_ = s.observeHealth(ctx, revision, state, detail, &checkedAt)
	return snapshot, nil
}

func (s *Service) Resources(ctx context.Context, query Query) (ResourcePage, error) {
	snapshot, err := s.Topology(ctx, query)
	if err != nil {
		return ResourcePage{}, err
	}
	items := filterResources(snapshot.Nodes, query.Kinds, query.Search)
	after, err := decodeCursor(query.Cursor, snapshot.ContentHash)
	if err != nil {
		return ResourcePage{}, err
	}
	start := 0
	if after != "" {
		start = sort.Search(len(items), func(index int) bool { return items[index].ID > after })
	}
	limit := query.Limit
	if limit <= 0 || limit > DefaultLimit {
		limit = DefaultLimit
	}
	end := minInt(len(items), start+limit)
	pageItems := nonNilResources(items[start:end])
	nextCursor := ""
	if end < len(items) && end > start {
		nextCursor = encodeCursor(items[end-1].ID, snapshot.ContentHash)
	}
	return ResourcePage{
		SnapshotID: snapshot.ID, Scope: snapshot.Scope, ProviderState: snapshot.ProviderState,
		Source: snapshot.Source, Freshness: snapshot.Freshness, Items: pageItems, NextCursor: nextCursor,
		Partial: snapshot.Partial, Truncated: snapshot.Truncated || end < len(items), CollectedAt: snapshot.CollectedAt,
	}, nil
}

func (s *Service) Resource(ctx context.Context, id string, query Query) (ResourceDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 512 {
		return ResourceDetail{}, fmt.Errorf("%w: resource id is invalid", ErrInvalid)
	}
	snapshot, err := s.Topology(ctx, query)
	if err != nil {
		return ResourceDetail{}, err
	}
	if snapshot.ProviderState == ProviderDisabled || snapshot.ProviderState == ProviderUnavailable {
		return ResourceDetail{}, ErrUnavailable
	}
	var selected *Resource
	for index := range snapshot.Nodes {
		if snapshot.Nodes[index].ID == id {
			selected = &snapshot.Nodes[index]
			break
		}
	}
	if selected == nil {
		return ResourceDetail{}, ErrNotFound
	}
	relatedIDs := map[string]struct{}{}
	var edges []TopologyEdge
	for _, edge := range snapshot.Edges {
		if edge.SourceID == id || edge.TargetID == id {
			edges = append(edges, edge)
			if edge.SourceID != id {
				relatedIDs[edge.SourceID] = struct{}{}
			}
			if edge.TargetID != id {
				relatedIDs[edge.TargetID] = struct{}{}
			}
		}
	}
	var related []Resource
	for _, item := range snapshot.Nodes {
		if _, ok := relatedIDs[item.ID]; ok {
			related = append(related, item)
		}
	}
	return ResourceDetail{
		SnapshotID: snapshot.ID, Scope: snapshot.Scope, ProviderState: snapshot.ProviderState,
		Source: snapshot.Source, Freshness: snapshot.Freshness, Resource: *selected,
		Related: nonNilResources(related), Edges: nonNilEdges(edges), Partial: snapshot.Partial, CollectedAt: snapshot.CollectedAt,
	}, nil
}

func (s *Service) ResourceEvents(ctx context.Context, id string, query Query) (EventPage, error) {
	detail, err := s.Resource(ctx, id, query)
	if err != nil {
		return EventPage{}, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	items, truncated, err := s.reader.Events(ctx, detail.Scope.ClusterID, detail.Resource, limit)
	if err != nil {
		return EventPage{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if items == nil {
		items = []Event{}
	}
	return EventPage{
		SnapshotID: detail.SnapshotID, Scope: detail.Scope, ProviderState: detail.ProviderState,
		Source: detail.Source, ResourceID: id, Items: items, Partial: detail.Partial,
		Truncated: truncated, CollectedAt: s.now().UTC(),
	}, nil
}

func (s *Service) Probe(ctx context.Context) (string, error) {
	revision, err := s.configuration.ActiveRevision(ctx)
	if err != nil {
		return "", fmt.Errorf("load active configuration: %w", err)
	}
	return s.ProbeCluster(ctx, revision.Scope.ClusterID)
}

func (s *Service) ProbeCluster(ctx context.Context, expectedClusterID string) (string, error) {
	if s.reader == nil {
		if s.bootstrapErr != nil {
			return "", s.bootstrapErr
		}
		return "", ErrUnavailable
	}
	expectedClusterID = strings.TrimSpace(expectedClusterID)
	if expectedClusterID == "" {
		return "", fmt.Errorf("%w: Kubernetes cluster identity is required", ErrInvalid)
	}
	source, err := s.reader.Probe(ctx, expectedClusterID)
	if err != nil {
		return "", err
	}
	if source.ClusterID != expectedClusterID {
		return "", fmt.Errorf("%w: Kubernetes reader cluster identity does not match the active Operational Scope", ErrInvalid)
	}
	detail := "Kubernetes API typed client 连接可用"
	detail += " · " + source.ClusterID
	if source.ServerVersion != "" {
		detail += " · " + source.ServerVersion
	}
	return detail, nil
}

func normalizeQuery(query Query, revision settings.Revision) (Query, []string, error) {
	if query.ClusterID != "" && query.ClusterID != revision.Scope.ClusterID {
		return Query{}, nil, fmt.Errorf("%w: cluster is not the active Operational Scope", ErrInvalid)
	}
	query.ClusterID = revision.Scope.ClusterID
	allowed := make(map[string]struct{}, len(revision.Scope.Namespaces))
	for _, namespace := range revision.Scope.Namespaces {
		allowed[namespace] = struct{}{}
	}
	var namespaces []string
	if query.Namespace != "" {
		if _, ok := allowed[query.Namespace]; !ok {
			return Query{}, nil, fmt.Errorf("%w: namespace is outside the active Operational Scope", ErrInvalid)
		}
		namespaces = []string{query.Namespace}
	} else {
		namespaces = append(namespaces, revision.Scope.Namespaces...)
	}
	if len(namespaces) == 0 {
		return Query{}, nil, fmt.Errorf("%w: active Operational Scope has no namespaces", ErrInvalid)
	}
	sort.Strings(namespaces)
	now := time.Now().UTC()
	if query.To.IsZero() {
		query.To = now
	}
	if query.From.IsZero() {
		query.From = query.To.Add(-time.Hour)
	}
	if !query.To.After(query.From) {
		return Query{}, nil, fmt.Errorf("%w: time range is invalid", ErrInvalid)
	}
	if query.To.Sub(query.From) > time.Duration(revision.General.QueryMaxLookbackSeconds)*time.Second {
		return Query{}, nil, fmt.Errorf("%w: time range exceeds the active query bound", ErrInvalid)
	}
	if query.Limit < 0 || query.Limit > MaximumLimit {
		return Query{}, nil, fmt.Errorf("%w: limit is outside 1-%d", ErrInvalid, MaximumLimit)
	}
	query.Search = strings.TrimSpace(query.Search)
	if len(query.Search) > 128 {
		return Query{}, nil, fmt.Errorf("%w: search is too long", ErrInvalid)
	}
	return query, namespaces, nil
}

func unavailableSnapshot(revision settings.Revision, namespaces []string, state ProviderState, detail string, now time.Time) TopologySnapshot {
	now = now.UTC()
	return TopologySnapshot{
		ConfigurationRevision: revision.ID, Scope: scopedRevision(revision, namespaces),
		ProviderState: state, ProviderDetail: detail,
		Source:    ProviderSource{Provider: "kubernetes", ClusterID: revision.Scope.ClusterID, Identity: "kubernetes://" + revision.Scope.ClusterID, CollectedAt: now},
		Freshness: Freshness{State: "unavailable", FreshUntil: now, AgeSeconds: 0},
		Nodes:     []Resource{}, Edges: []TopologyEdge{}, Issues: []ProviderIssue{},
		Partial: true, CollectedAt: now,
	}
}

func scopedRevision(revision settings.Revision, namespaces []string) settings.OperationalScope {
	scope := revision.Scope
	scope.Namespaces = append([]string(nil), namespaces...)
	return scope
}

func providerConfiguration(revision settings.Revision, provider settings.Provider) (settings.ProviderConfiguration, bool) {
	for _, item := range revision.Providers {
		if item.Provider == provider {
			return item, true
		}
	}
	return settings.ProviderConfiguration{}, false
}

func (s *Service) observeHealth(ctx context.Context, revision settings.Revision, state ProviderState, detail string, checkedAt *time.Time) error {
	return s.configuration.ObserveProviderHealth(ctx, revision.ID, settings.ProviderResult{
		Provider: settings.ProviderKubernetes, State: string(state), Detail: detail, CheckedAt: checkedAt,
	})
}

func filterResources(values []Resource, kinds []string, search string) []Resource {
	kindSet := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind != "" {
			kindSet[kind] = struct{}{}
		}
	}
	search = strings.ToLower(strings.TrimSpace(search))
	result := make([]Resource, 0, len(values))
	for _, value := range values {
		if len(kindSet) > 0 {
			if _, ok := kindSet[strings.ToLower(value.Kind)]; !ok {
				continue
			}
		}
		if search != "" && !strings.Contains(strings.ToLower(value.Name), search) &&
			!strings.Contains(strings.ToLower(value.Namespace), search) &&
			!strings.Contains(strings.ToLower(value.Kind), search) {
			continue
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func resourceLinks(resource Resource, query Query) []ContextLink {
	values := url.Values{}
	values.Set("cluster", query.ClusterID)
	if resource.Namespace != "" {
		values.Set("namespace", resource.Namespace)
	}
	values.Set("resource", resource.ID)
	values.Set("from", query.From.UTC().Format(time.RFC3339))
	values.Set("to", query.To.UTC().Format(time.RFC3339))
	links := []ContextLink{{
		Kind: "internal", Label: "在基础设施中打开", Href: "/infrastructure?" + values.Encode(),
		Target: "current", Provider: "kubernetes", ResourceRef: resource.ID,
		From: query.From.UTC(), To: query.To.UTC(), Availability: "available",
	}}
	for _, workspace := range []struct{ path, label string }{
		{"/monitoring", "查看相关 Metrics"}, {"/logs", "查看相关 Logs"}, {"/traces", "查看相关 Traces"}, {"/agent", "附加到 Agent 上下文"},
	} {
		links = append(links, ContextLink{
			Kind: "internal", Label: workspace.label, Href: workspace.path + "?" + values.Encode(),
			Target: "current", Provider: "kubernetes", ResourceRef: resource.ID,
			From: query.From.UTC(), To: query.To.UTC(), Availability: "unavailable",
		})
	}
	return links
}

type cursorPayload struct {
	After        string `json:"after"`
	SnapshotHash string `json:"snapshot_hash"`
}

func encodeCursor(after, snapshotHash string) string {
	encoded, _ := json.Marshal(cursorPayload{After: after, SnapshotHash: snapshotHash})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(value, snapshotHash string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("%w: cursor is invalid", ErrInvalid)
	}
	var payload cursorPayload
	if json.Unmarshal(decoded, &payload) != nil || payload.After == "" || payload.SnapshotHash != snapshotHash {
		return "", fmt.Errorf("%w: cursor does not match the current topology snapshot", ErrInvalid)
	}
	return payload.After, nil
}

func ProjectionHash(nodes []Resource, edges []TopologyEdge) (string, error) {
	copyNodes := append([]Resource(nil), nodes...)
	copyEdges := append([]TopologyEdge(nil), edges...)
	for index := range copyNodes {
		copyNodes[index].Links = nil
	}
	sort.Slice(copyNodes, func(i, j int) bool { return copyNodes[i].ID < copyNodes[j].ID })
	sort.Slice(copyEdges, func(i, j int) bool { return copyEdges[i].ID < copyEdges[j].ID })
	encoded, err := json.Marshal(struct {
		Nodes []Resource     `json:"nodes"`
		Edges []TopologyEdge `json:"edges"`
	}{copyNodes, copyEdges})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func ScopeHash(scope settings.OperationalScope) (string, error) {
	namespaces := append([]string(nil), scope.Namespaces...)
	sort.Strings(namespaces)
	encoded, err := json.Marshal(struct {
		ClusterID   string   `json:"cluster_id"`
		Environment string   `json:"environment"`
		Namespaces  []string `json:"namespaces"`
	}{scope.ClusterID, scope.Environment, namespaces})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func nonNilResources(values []Resource) []Resource {
	if values == nil {
		return []Resource{}
	}
	return values
}
func nonNilEdges(values []TopologyEdge) []TopologyEdge {
	if values == nil {
		return []TopologyEdge{}
	}
	return values
}
func nonNilIssues(values []ProviderIssue) []ProviderIssue {
	if values == nil {
		return []ProviderIssue{}
	}
	return values
}
func minPositive(values ...int) int {
	result := MaximumLimit
	for _, value := range values {
		if value > 0 && value < result {
			result = value
		}
	}
	return result
}
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func boundedDetail(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
