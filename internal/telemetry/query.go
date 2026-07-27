package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

type preparedQuery struct {
	Provider              string
	Kind                  string
	ConfigurationRevision string
	Mode                  QueryMode
	Query                 string
	QueryHash             string
	Scope                 settings.OperationalScope
	Resource              ResourceReference
	TimeRange             TimeRange
	Bounds                QueryBounds
}

func prepareLogQuery(request StartLogQueryRequest, revision settings.Revision) (preparedQuery, error) {
	provider, err := providerConfiguration(revision, settings.ProviderElasticsearch)
	if err != nil {
		return preparedQuery{}, err
	}
	if !provider.Enabled {
		return preparedQuery{}, ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if err != nil {
		return preparedQuery{}, err
	}
	bounds, window, err := boundedRange(request.From, request.To, request.Limit, revision, provider, MaximumLogRows)
	if err != nil {
		return preparedQuery{}, err
	}
	query, err := normalizeLogQuery(request.Mode, request.Query, request.Filter, scope, resource)
	if err != nil {
		return preparedQuery{}, err
	}
	kind := "logs"
	if request.Tail {
		kind = "logs_tail"
	}
	return newPrepared("elasticsearch", kind, revision.ID, request.Mode, query, scope, resource, window, bounds), nil
}

func prepareTraceSearch(request StartTraceSearchRequest, revision settings.Revision) (preparedQuery, error) {
	provider, err := providerConfiguration(revision, settings.ProviderTempo)
	if err != nil {
		return preparedQuery{}, err
	}
	if !provider.Enabled {
		return preparedQuery{}, ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if err != nil {
		return preparedQuery{}, err
	}
	bounds, window, err := boundedRange(request.From, request.To, request.Limit, revision, provider, MaximumTraceCount)
	if err != nil {
		return preparedQuery{}, err
	}
	query, err := normalizeTraceQL(request.Mode, request.Query, request.Filter, scope, resource)
	if err != nil {
		return preparedQuery{}, err
	}
	return newPrepared("tempo", "trace_search", revision.ID, request.Mode, query, scope, resource, window, bounds), nil
}

// PrepareBoundedLogToolRequest creates the fixed guided Logs request available
// to Agent read tools. It cannot carry expert Elasticsearch query text.
func PrepareBoundedLogToolRequest(clusterID, namespace string, resource ResourceReference, from, to time.Time, limit int, revision settings.Revision) (ProviderLogRequest, error) {
	prepared, err := prepareLogQuery(StartLogQueryRequest{
		Mode: ModeGuided, ClusterID: clusterID, Namespace: namespace,
		Resource: resource, From: from, To: to, Limit: limit,
	}, revision)
	if err != nil {
		return ProviderLogRequest{}, err
	}
	return ProviderLogRequest{
		ConfigurationRevision: prepared.ConfigurationRevision,
		Scope:                 prepared.Scope, Resource: prepared.Resource, Query: prepared.Query,
		TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
	}, nil
}

// PrepareBoundedTraceToolRequest creates the fixed guided Trace search request
// available to Agent read tools. Expert TraceQL remains outside this contract.
func PrepareBoundedTraceToolRequest(clusterID, namespace string, resource ResourceReference, from, to time.Time, limit int, revision settings.Revision) (ProviderTraceSearchRequest, error) {
	prepared, err := prepareTraceSearch(StartTraceSearchRequest{
		Mode: ModeGuided, ClusterID: clusterID, Namespace: namespace,
		Resource: resource, From: from, To: to, Limit: limit,
	}, revision)
	if err != nil {
		return ProviderTraceSearchRequest{}, err
	}
	return ProviderTraceSearchRequest{
		ConfigurationRevision: prepared.ConfigurationRevision,
		Scope:                 prepared.Scope, Resource: prepared.Resource, Query: prepared.Query,
		TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
	}, nil
}

func prepareTraceDetail(request TraceDetailRequest, revision settings.Revision) (preparedQuery, string, error) {
	traceID := strings.ToLower(strings.TrimSpace(request.TraceID))
	if !traceIDPattern.MatchString(traceID) {
		return preparedQuery{}, "", fmt.Errorf("%w: trace_id must contain 32 lowercase hexadecimal characters", ErrInvalid)
	}
	provider, err := providerConfiguration(revision, settings.ProviderTempo)
	if err != nil {
		return preparedQuery{}, "", err
	}
	if !provider.Enabled {
		return preparedQuery{}, "", ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if err != nil {
		return preparedQuery{}, "", err
	}
	bounds, window, err := boundedRange(request.From, request.To, provider.MaxResults, revision, provider, MaximumTraceCount)
	if err != nil {
		return preparedQuery{}, "", err
	}
	return newPrepared("tempo", "trace_detail", revision.ID, ModeGuided, "trace_id="+traceID, scope, resource, window, bounds), traceID, nil
}

func ValidateProviderCatalog(request ProviderCatalogRequest, revision settings.Revision) error {
	providerName := strings.TrimSpace(request.Provider)
	var provider settings.Provider
	switch providerName {
	case "elasticsearch":
		provider = settings.ProviderElasticsearch
	case "tempo":
		provider = settings.ProviderTempo
	default:
		return fmt.Errorf("%w: unsupported telemetry Provider", ErrInvalid)
	}
	configuration, err := providerConfiguration(revision, provider)
	if err != nil || !configuration.Enabled {
		return ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.Scope.ClusterID, request.Resource.Namespace, request.Resource, revision)
	if err != nil {
		return err
	}
	expected := providerBounds(revision, configuration, MaximumLogRows)
	if provider == settings.ProviderTempo {
		expected = providerBounds(revision, configuration, MaximumTraceCount)
	}
	if request.ConfigurationRevision != revision.ID || request.Scope.Environment != scope.Environment || request.Resource != resource ||
		request.Bounds.MaxLookbackSeconds > expected.MaxLookbackSeconds || request.Bounds.TimeoutMS > expected.TimeoutMS ||
		request.Bounds.MaxResponseBytes > expected.MaxResponseBytes || request.Bounds.MaxResults > expected.MaxResults ||
		request.Bounds.ConcurrencyLimit > expected.ConcurrencyLimit {
		return fmt.Errorf("%w: Provider catalog exceeds its Configuration Revision contract", ErrInvalid)
	}
	return nil
}

func ValidateProviderLogRequest(request ProviderLogRequest, revision settings.Revision) error {
	provider, err := providerConfiguration(revision, settings.ProviderElasticsearch)
	if err != nil || !provider.Enabled {
		return ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.Scope.ClusterID, request.Resource.Namespace, request.Resource, revision)
	if err != nil {
		return err
	}
	bounds, window, err := boundedRange(request.TimeRange.From, request.TimeRange.To, request.Bounds.MaxResults, revision, provider, MaximumLogRows)
	if err != nil {
		return err
	}
	if err := validateNormalizedElasticsearchQuery(request.Query, scope, resource); err != nil {
		return err
	}
	kind := "logs"
	if request.Tail {
		kind = "logs_tail"
	}
	prepared := newPrepared("elasticsearch", kind, revision.ID, ModeExpert, request.Query, scope, resource, window, bounds)
	return compareProviderRequest(request.ConfigurationRevision, request.Query, request.Scope, request.Resource, request.TimeRange, request.Bounds, prepared)
}

func ValidateProviderTraceSearchRequest(request ProviderTraceSearchRequest, revision settings.Revision) error {
	provider, err := providerConfiguration(revision, settings.ProviderTempo)
	if err != nil || !provider.Enabled {
		return ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.Scope.ClusterID, request.Resource.Namespace, request.Resource, revision)
	if err != nil {
		return err
	}
	bounds, window, err := boundedRange(request.TimeRange.From, request.TimeRange.To, request.Bounds.MaxResults, revision, provider, MaximumTraceCount)
	if err != nil {
		return err
	}
	if err := validateNormalizedTraceQL(request.Query, scope, resource); err != nil {
		return err
	}
	prepared := newPrepared("tempo", "trace_search", revision.ID, ModeExpert, request.Query, scope, resource, window, bounds)
	return compareProviderRequest(request.ConfigurationRevision, request.Query, request.Scope, request.Resource, request.TimeRange, request.Bounds, prepared)
}

func ValidateProviderTraceDetailRequest(request ProviderTraceDetailRequest, revision settings.Revision) error {
	prepared, traceID, err := prepareTraceDetail(TraceDetailRequest{
		TraceID: request.TraceID, ClusterID: request.Scope.ClusterID, Namespace: request.Resource.Namespace,
		Resource: request.Resource, From: request.TimeRange.From, To: request.TimeRange.To,
	}, revision)
	if err != nil {
		return err
	}
	if traceID != request.TraceID {
		return fmt.Errorf("%w: trace identity is not canonical", ErrInvalid)
	}
	return compareProviderRequest(request.ConfigurationRevision, prepared.Query, request.Scope, request.Resource, request.TimeRange, request.Bounds, prepared)
}

func compareProviderRequest(revisionID, query string, scope settings.OperationalScope, resource ResourceReference, window TimeRange, bounds QueryBounds, prepared preparedQuery) error {
	if revisionID != prepared.ConfigurationRevision || query != prepared.Query || scope.ClusterID != prepared.Scope.ClusterID ||
		scope.Environment != prepared.Scope.Environment || !slices.Equal(scope.Namespaces, prepared.Scope.Namespaces) ||
		resource != prepared.Resource || !window.From.Equal(prepared.TimeRange.From) || !window.To.Equal(prepared.TimeRange.To) ||
		bounds.MaxLookbackSeconds < 1 || bounds.TimeoutMS < 1 || bounds.MaxResponseBytes < 1 || bounds.MaxResults < 1 || bounds.ConcurrencyLimit < 1 ||
		bounds.MaxLookbackSeconds > prepared.Bounds.MaxLookbackSeconds || bounds.TimeoutMS > prepared.Bounds.TimeoutMS ||
		bounds.MaxResponseBytes > prepared.Bounds.MaxResponseBytes || bounds.MaxResults > prepared.Bounds.MaxResults ||
		bounds.ConcurrencyLimit > prepared.Bounds.ConcurrencyLimit {
		return fmt.Errorf("%w: Provider query exceeds its Configuration Revision contract", ErrInvalid)
	}
	return nil
}

func newPrepared(provider, kind, revisionID string, mode QueryMode, query string, scope settings.OperationalScope, resource ResourceReference, window TimeRange, bounds QueryBounds) preparedQuery {
	digest := sha256.Sum256([]byte(query))
	return preparedQuery{
		Provider: provider, Kind: kind, ConfigurationRevision: revisionID, Mode: mode,
		Query: query, QueryHash: hex.EncodeToString(digest[:]), Scope: scope,
		Resource: resource, TimeRange: window, Bounds: bounds,
	}
}

func boundedScope(clusterID, namespace string, resource ResourceReference, revision settings.Revision) (settings.OperationalScope, ResourceReference, error) {
	clusterID, namespace = strings.TrimSpace(clusterID), strings.TrimSpace(namespace)
	resource.ID, resource.Kind = strings.TrimSpace(resource.ID), strings.TrimSpace(resource.Kind)
	resource.Namespace, resource.Name = strings.TrimSpace(resource.Namespace), strings.TrimSpace(resource.Name)
	if clusterID == "" || namespace == "" || resource.ID == "" || resource.Kind == "" || resource.Name == "" {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: cluster, Namespace, and Workload identity are required", ErrInvalid)
	}
	if resource.Namespace != namespace || !slices.Contains([]string{"Deployment", "StatefulSet", "DaemonSet"}, resource.Kind) {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: resource must be a Workload in the requested Namespace", ErrInvalid)
	}
	if len(resource.ID) > 512 || len(resource.Name) > 253 || len(namespace) > 253 || len(clusterID) > 128 {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: Workload identity exceeds the bounded size", ErrInvalid)
	}
	var matched *settings.OperationalScope
	for index := range revision.Scopes {
		if revision.Scopes[index].ClusterID == clusterID {
			candidate := revision.Scopes[index]
			matched = &candidate
			break
		}
	}
	if matched == nil || !slices.Contains(matched.Namespaces, namespace) {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: requested Workload is outside the Configuration Revision scope", ErrInvalid)
	}
	scope := *matched
	scope.Namespaces = []string{namespace}
	scope.Active = revision.Scope.ClusterID == clusterID
	return scope, resource, nil
}

func boundedRange(from, to time.Time, requestedLimit int, revision settings.Revision, provider settings.ProviderConfiguration, hardLimit int) (QueryBounds, TimeRange, error) {
	from, to = from.UTC(), to.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: an absolute increasing UTC time range is required", ErrInvalid)
	}
	if to.Sub(from) > time.Duration(revision.General.QueryMaxLookbackSeconds)*time.Second {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: time range exceeds query_max_lookback_seconds", ErrBoundExceeded)
	}
	bounds := providerBounds(revision, provider, hardLimit)
	if requestedLimit < 1 || requestedLimit > bounds.MaxResults {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: result limit must be between 1 and %d", ErrBoundExceeded, bounds.MaxResults)
	}
	bounds.MaxResults = requestedLimit
	return bounds, TimeRange{From: from, To: to}, nil
}

func providerBounds(revision settings.Revision, provider settings.ProviderConfiguration, hardLimit int) QueryBounds {
	return QueryBounds{
		MaxLookbackSeconds: revision.General.QueryMaxLookbackSeconds,
		TimeoutMS:          minPositive(provider.TimeoutMS, 30_000),
		MaxResponseBytes:   MaximumResponseBytes,
		MaxResults:         minPositive(provider.MaxResults, revision.General.QueryMaxResults, hardLimit),
		ConcurrencyLimit:   2,
	}
}

func normalizeLogQuery(mode QueryMode, raw string, filter LogFilter, scope settings.OperationalScope, resource ResourceReference) (string, error) {
	workloadField := map[string]string{
		"Deployment": "kubernetes.deployment.name", "StatefulSet": "kubernetes.statefulset.name",
		"DaemonSet": "kubernetes.daemonset.name",
	}[resource.Kind]
	fixed := []any{
		map[string]any{"term": map[string]any{"cloudops.cluster_id": scope.ClusterID}},
		map[string]any{"term": map[string]any{"kubernetes.namespace": resource.Namespace}},
		map[string]any{"term": map[string]any{workloadField: resource.Name}},
	}
	var user any
	switch mode {
	case ModeGuided:
		filter.Text = strings.TrimSpace(filter.Text)
		filter.TraceID = strings.ToLower(strings.TrimSpace(filter.TraceID))
		must := []any{}
		if filter.Text != "" {
			if len(filter.Text) > 512 || !utf8.ValidString(filter.Text) {
				return "", fmt.Errorf("%w: guided log text is invalid", ErrInvalid)
			}
			must = append(must, map[string]any{"simple_query_string": map[string]any{
				"query": filter.Text, "fields": []string{"message", "msg", "error.message"}, "default_operator": "and",
			}})
		}
		levels, err := normalizeLevels(filter.Levels)
		if err != nil {
			return "", err
		}
		if len(levels) > 0 {
			fixed = append(fixed, map[string]any{"terms": map[string]any{"level": levels}})
		}
		if filter.TraceID != "" {
			if !traceIDPattern.MatchString(filter.TraceID) {
				return "", fmt.Errorf("%w: trace_id must contain 32 lowercase hexadecimal characters", ErrInvalid)
			}
			fixed = append(fixed, map[string]any{"bool": map[string]any{"should": []any{
				map[string]any{"term": map[string]any{"trace_id": filter.TraceID}},
				map[string]any{"term": map[string]any{"trace.id": filter.TraceID}},
			}, "minimum_should_match": 1}})
		}
		user = map[string]any{"bool": map[string]any{"must": must}}
	case ModeExpert:
		if err := decodeBoundedElasticsearchQuery(raw, &user); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("%w: query mode is not supported", ErrInvalid)
	}
	query := map[string]any{"bool": map[string]any{"filter": fixed, "must": []any{user}}}
	encoded, err := json.Marshal(query)
	if err != nil || len(encoded) > MaximumQueryBytes {
		return "", fmt.Errorf("%w: normalized Elasticsearch query exceeds %d bytes", ErrInvalid, MaximumQueryBytes)
	}
	return string(encoded), nil
}

func decodeBoundedElasticsearchQuery(raw string, target *any) error {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || len(raw) > MaximumQueryBytes || !utf8.ValidString(raw) {
		return fmt.Errorf("%w: Elasticsearch query must contain 2 to %d bytes", ErrInvalid, MaximumQueryBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: Elasticsearch query must be valid JSON", ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: Elasticsearch query must contain one JSON value", ErrInvalid)
	}
	root, ok := (*target).(map[string]any)
	if !ok || len(root) != 1 {
		return fmt.Errorf("%w: Elasticsearch expert input must be one query clause", ErrInvalid)
	}
	if err := validateJSONTree(root, 0, new(int)); err != nil {
		return err
	}
	return nil
}

func validateJSONTree(value any, depth int, nodes *int) error {
	(*nodes)++
	if depth > 16 || *nodes > 256 {
		return fmt.Errorf("%w: Elasticsearch query is too complex", ErrBoundExceeded)
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(strings.TrimSpace(key))
			if slices.Contains([]string{"script", "script_score", "percolate", "runtime_mappings", "knn", "rank", "rescore", "collapse"}, lower) {
				return fmt.Errorf("%w: Elasticsearch clause %q is not allowed", ErrInvalid, key)
			}
			if len(key) == 0 || len(key) > 256 {
				return fmt.Errorf("%w: Elasticsearch field name is invalid", ErrInvalid)
			}
			if err := validateJSONTree(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) > 64 {
			return fmt.Errorf("%w: Elasticsearch array exceeds 64 items", ErrBoundExceeded)
		}
		for _, child := range typed {
			if err := validateJSONTree(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case string:
		if len(typed) > 1024 || !utf8.ValidString(typed) {
			return fmt.Errorf("%w: Elasticsearch string value exceeds its bound", ErrBoundExceeded)
		}
	case json.Number, bool, nil:
	default:
		return fmt.Errorf("%w: Elasticsearch query contains an unsupported value", ErrInvalid)
	}
	return nil
}

func validateNormalizedElasticsearchQuery(query string, scope settings.OperationalScope, resource ResourceReference) error {
	var decoded any
	if err := decodeBoundedElasticsearchQuery(query, &decoded); err != nil {
		return err
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: normalized Elasticsearch query is invalid", ErrInvalid)
	}
	boolClause, ok := root["bool"].(map[string]any)
	if !ok {
		return fmt.Errorf("%w: normalized Elasticsearch query must use a bool clause", ErrInvalid)
	}
	filters, ok := boolClause["filter"].([]any)
	if !ok {
		return fmt.Errorf("%w: normalized Elasticsearch query is missing scope filters", ErrInvalid)
	}
	workloadField := map[string]string{
		"Deployment": "kubernetes.deployment.name", "StatefulSet": "kubernetes.statefulset.name",
		"DaemonSet": "kubernetes.daemonset.name",
	}[resource.Kind]
	required := map[string]string{
		"cloudops.cluster_id":  scope.ClusterID,
		"kubernetes.namespace": resource.Namespace,
		workloadField:          resource.Name,
	}
	for _, filter := range filters {
		clause, ok := filter.(map[string]any)
		if !ok {
			continue
		}
		term, ok := clause["term"].(map[string]any)
		if !ok {
			continue
		}
		for field, value := range required {
			if actual, exists := term[field]; exists && actual == value {
				delete(required, field)
			}
		}
	}
	if len(required) != 0 {
		return fmt.Errorf("%w: Elasticsearch query does not preserve the Operational Scope", ErrInvalid)
	}
	return nil
}

func normalizeTraceQL(mode QueryMode, raw string, filter TraceFilter, scope settings.OperationalScope, resource ResourceReference) (string, error) {
	clauses := traceScopeClauses(scope, resource)
	userClauses := []string{}
	switch mode {
	case ModeGuided:
		if value := strings.TrimSpace(filter.Service); value != "" {
			if len(value) > 256 {
				return "", fmt.Errorf("%w: service filter is too long", ErrInvalid)
			}
			userClauses = append(userClauses, `resource.service.name = "`+escapeTraceQL(value)+`"`)
		}
		if value := strings.TrimSpace(filter.Operation); value != "" {
			if len(value) > 256 {
				return "", fmt.Errorf("%w: operation filter is too long", ErrInvalid)
			}
			userClauses = append(userClauses, `name = "`+escapeTraceQL(value)+`"`)
		}
		switch strings.ToLower(strings.TrimSpace(filter.Status)) {
		case "", "all":
		case "error":
			userClauses = append(userClauses, "status = error")
		case "ok":
			userClauses = append(userClauses, "status = ok")
		default:
			return "", fmt.Errorf("%w: trace status filter is invalid", ErrInvalid)
		}
		if filter.MinMS < 0 || filter.MaxMS < 0 || (filter.MaxMS > 0 && filter.MinMS > filter.MaxMS) {
			return "", fmt.Errorf("%w: trace duration bounds are invalid", ErrInvalid)
		}
		if filter.MinMS > 0 {
			userClauses = append(userClauses, fmt.Sprintf("duration >= %dms", filter.MinMS))
		}
		if filter.MaxMS > 0 {
			userClauses = append(userClauses, fmt.Sprintf("duration <= %dms", filter.MaxMS))
		}
	case ModeExpert:
		expert, err := parseRestrictedTraceQL(raw)
		if err != nil {
			return "", err
		}
		if expert != "" {
			userClauses = append(userClauses, "("+expert+")")
		}
	default:
		return "", fmt.Errorf("%w: query mode is not supported", ErrInvalid)
	}
	clauses = append(clauses, userClauses...)
	query := "{ " + strings.Join(clauses, " && ") + " }"
	if len(query) > MaximumQueryBytes {
		return "", fmt.Errorf("%w: normalized TraceQL exceeds %d bytes", ErrInvalid, MaximumQueryBytes)
	}
	return query, nil
}

func traceScopeClauses(scope settings.OperationalScope, resource ResourceReference) []string {
	return []string{
		`resource.k8s.cluster.name = "` + escapeTraceQL(scope.ClusterID) + `"`,
		`resource.k8s.namespace.name = "` + escapeTraceQL(resource.Namespace) + `"`,
		`resource.k8s.workload.name = "` + escapeTraceQL(resource.Name) + `"`,
	}
}

func parseRestrictedTraceQL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || len(raw) > MaximumQueryBytes || raw[0] != '{' || raw[len(raw)-1] != '}' {
		return "", fmt.Errorf("%w: expert TraceQL must be one bounded span selector", ErrInvalid)
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return "", nil
	}
	depth, quoted, escaped := 0, false, false
	for _, char := range inner {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quoted {
			escaped = true
			continue
		}
		if char == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		switch char {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		}
		if depth < 0 {
			return "", fmt.Errorf("%w: TraceQL delimiters are unbalanced", ErrInvalid)
		}
	}
	if quoted || escaped || depth != 0 || strings.ContainsAny(inner, "{};\n\r") {
		return "", fmt.Errorf("%w: TraceQL selector is not a bounded expression", ErrInvalid)
	}
	return inner, nil
}

func validateNormalizedTraceQL(query string, scope settings.OperationalScope, resource ResourceReference) error {
	if len(query) > MaximumQueryBytes || !strings.HasPrefix(query, "{ ") || !strings.HasSuffix(query, " }") {
		return fmt.Errorf("%w: normalized TraceQL is invalid", ErrInvalid)
	}
	prefix := "{ " + strings.Join(traceScopeClauses(scope, resource), " && ")
	if query != prefix+" }" && !strings.HasPrefix(query, prefix+" && ") {
		return fmt.Errorf("%w: TraceQL does not preserve the Operational Scope", ErrInvalid)
	}
	return nil
}

func normalizeLevels(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !slices.Contains([]string{"debug", "info", "warn", "warning", "error", "fatal"}, value) {
			return nil, fmt.Errorf("%w: log level %q is not supported", ErrInvalid, value)
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result, nil
}

func providerConfiguration(revision settings.Revision, name settings.Provider) (settings.ProviderConfiguration, error) {
	for _, provider := range revision.Providers {
		if provider.Provider == name {
			return provider, nil
		}
	}
	return settings.ProviderConfiguration{}, fmt.Errorf("%w: %s configuration is missing", ErrUnavailable, name)
}

func escapeTraceQL(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "", "\r", "").Replace(value)
}

func minPositive(values ...int) int {
	result := 0
	for _, value := range values {
		if value > 0 && (result == 0 || value < result) {
			result = value
		}
	}
	return result
}

func validSpanID(value string) bool {
	return spanIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}
