package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

var guidedCatalog = []CatalogEntry{
	{
		Key: "workload_cpu", Title: "Workload CPU", Unit: "percent",
		Description: "按真实 scrape target 查看进程 CPU 使用率。",
		Query:       "sum by (instance) (rate(process_cpu_seconds_total[5m])) * 100",
	},
	{
		Key: "http_error_rate", Title: "HTTP 错误率", Unit: "ratio",
		Description: "比较 5xx 请求与全部 HTTP 请求速率。",
		Query:       `sum(rate(http_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(http_requests_total[5m])), 0.001)`,
	},
	{
		Key: "http_request_rate", Title: "HTTP 请求速率", Unit: "requests_per_second",
		Description: "查看 Workload 每秒处理的 HTTP 请求数。",
		Query:       "sum(rate(http_requests_total[5m]))",
	},
	{
		Key: "workload_availability", Title: "Scrape 可用性", Unit: "state",
		Description: "查看 Prometheus 对 Workload 指标端点的实时抓取状态。",
		Query:       "max(up)",
	},
}

func GuidedCatalog(scope settings.OperationalScope, resource ResourceReference, maxLookback time.Duration) ([]CatalogEntry, error) {
	result := make([]CatalogEntry, 0, len(guidedCatalog))
	for _, entry := range guidedCatalog {
		normalized, err := normalizePromQL(entry.Query, scope, resource, maxLookback)
		if err != nil {
			return nil, err
		}
		entry.Query = normalized
		result = append(result, entry)
	}
	return result, nil
}

func PrepareOwnerQuery(request StartQueryRequest, revision settings.Revision) (PreparedQuery, error) {
	provider, err := prometheusConfiguration(revision)
	if err != nil {
		return PreparedQuery{}, err
	}
	if !provider.Enabled {
		return PreparedQuery{}, ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if err != nil {
		return PreparedQuery{}, err
	}
	bounds, queryRange, err := boundedRange(request.From, request.To, request.StepSeconds, revision, provider)
	if err != nil {
		return PreparedQuery{}, err
	}
	mode := request.Mode
	var query string
	switch mode {
	case ModeGuided:
		for _, candidate := range guidedCatalog {
			if candidate.Key == strings.TrimSpace(request.CatalogKey) {
				query = candidate.Query
				break
			}
		}
		if query == "" {
			return PreparedQuery{}, fmt.Errorf("%w: guided catalog key is not supported", ErrInvalid)
		}
	case ModeExpert:
		query = strings.TrimSpace(request.Query)
	default:
		return PreparedQuery{}, fmt.Errorf("%w: query mode is not supported", ErrInvalid)
	}
	if len(query) == 0 || len(query) > MaximumQueryBytes {
		return PreparedQuery{}, fmt.Errorf("%w: PromQL must contain 1 to %d bytes", ErrInvalid, MaximumQueryBytes)
	}
	normalized, err := normalizePromQL(query, scope, resource, time.Duration(bounds.MaxLookbackSeconds)*time.Second)
	if err != nil {
		return PreparedQuery{}, err
	}
	digest := sha256.Sum256([]byte(normalized))
	return PreparedQuery{
		ConfigurationRevision: revision.ID,
		DefinitionID:          strings.TrimSpace(request.DefinitionID),
		Actor:                 ActorOwner, Mode: mode, CatalogKey: strings.TrimSpace(request.CatalogKey),
		Query: normalized, QueryHash: hex.EncodeToString(digest[:]), Scope: scope,
		Resource: resource, TimeRange: queryRange, Bounds: bounds,
	}, nil
}

func bindOwnerDefinition(prepared PreparedQuery, definition Definition) (PreparedQuery, error) {
	if prepared.Actor != ActorOwner || prepared.DefinitionID == "" || prepared.DefinitionID != definition.ID ||
		definition.ConfigurationRevision != prepared.ConfigurationRevision || definition.Mode != prepared.Mode ||
		definition.CatalogKey != prepared.CatalogKey || definition.Query != prepared.Query || definition.QueryHash != prepared.QueryHash ||
		definition.Scope.ID != prepared.Scope.ID || definition.Scope.ClusterID != prepared.Scope.ClusterID ||
		definition.Scope.Environment != prepared.Scope.Environment || !slices.Equal(definition.Scope.Namespaces, prepared.Scope.Namespaces) ||
		definition.Resource != prepared.Resource || definition.MaxLookbackSeconds <= 0 || definition.MaxSeries <= 0 || definition.MaxSamples <= 0 {
		return PreparedQuery{}, fmt.Errorf("%w: Query Definition does not match the requested execution", ErrInvalid)
	}
	lookback := prepared.TimeRange.To.Sub(prepared.TimeRange.From)
	if lookback > time.Duration(definition.MaxLookbackSeconds)*time.Second {
		return PreparedQuery{}, fmt.Errorf("%w: requested time range exceeds the Query Definition", ErrBoundExceeded)
	}
	estimatedPoints := int(lookback/time.Second)/prepared.Bounds.StepSeconds + 1
	if estimatedPoints > definition.MaxSamples {
		return PreparedQuery{}, fmt.Errorf("%w: requested time range and step exceed the Query Definition sample limit", ErrBoundExceeded)
	}
	prepared.Bounds.MaxLookbackSeconds = minPositive(prepared.Bounds.MaxLookbackSeconds, definition.MaxLookbackSeconds)
	prepared.Bounds.MaxSeries = minPositive(prepared.Bounds.MaxSeries, definition.MaxSeries)
	prepared.Bounds.MaxSamples = minPositive(prepared.Bounds.MaxSamples, definition.MaxSamples)
	return prepared, nil
}

func PrepareAgentQuery(request AgentQueryRequest, authorization Authorization, revision settings.Revision) (PreparedQuery, error) {
	if strings.TrimSpace(request.AuthorizationID) == "" || request.AuthorizationID != authorization.ID {
		return PreparedQuery{}, fmt.Errorf("%w: exact Query Authorization identity is required", ErrUnauthorized)
	}
	if authorization.RevokedAt != nil {
		return PreparedQuery{}, ErrAuthorizationRevoked
	}
	if authorization.Mode == AuthorizationRunOnce && authorization.ConsumedExecutionID != "" {
		return PreparedQuery{}, ErrAuthorizationUsed
	}
	if revision.ID != authorization.ConfigurationRevision {
		return PreparedQuery{}, fmt.Errorf("%w: Configuration Revision does not match the authorization", ErrUnauthorized)
	}
	provider, err := prometheusConfiguration(revision)
	if err != nil {
		return PreparedQuery{}, err
	}
	if !provider.Enabled {
		return PreparedQuery{}, ErrProviderDisabled
	}
	scope, resource, err := boundedScope(authorization.Scope.ClusterID, authorization.Resource.Namespace, authorization.Resource, revision)
	if err != nil || scope.Environment != authorization.Scope.Environment {
		return PreparedQuery{}, fmt.Errorf("%w: authorization scope is no longer valid", ErrUnauthorized)
	}
	bounds, queryRange, err := boundedRange(request.From, request.To, request.StepSeconds, revision, provider)
	if err != nil {
		return PreparedQuery{}, err
	}
	lookbackSeconds := int(queryRange.To.Sub(queryRange.From) / time.Second)
	if lookbackSeconds > authorization.MaxLookbackSeconds {
		return PreparedQuery{}, fmt.Errorf("%w: requested lookback exceeds the authorization", ErrBoundExceeded)
	}
	bounds.MaxLookbackSeconds = minPositive(bounds.MaxLookbackSeconds, authorization.MaxLookbackSeconds)
	bounds.MaxSeries = minPositive(bounds.MaxSeries, authorization.MaxSeries)
	bounds.MaxSamples = minPositive(bounds.MaxSamples, authorization.MaxSamples)
	estimatedPoints := int(queryRange.To.Sub(queryRange.From)/time.Duration(bounds.StepSeconds)/time.Second) + 1
	if estimatedPoints > bounds.MaxSamples {
		return PreparedQuery{}, fmt.Errorf("%w: time range and step exceed the authorized sample limit", ErrBoundExceeded)
	}
	normalized, err := normalizePromQL(authorization.Query, scope, resource, time.Duration(bounds.MaxLookbackSeconds)*time.Second)
	if err != nil {
		return PreparedQuery{}, fmt.Errorf("%w: authorized PromQL no longer satisfies the bounded contract", ErrUnauthorized)
	}
	digest := sha256.Sum256([]byte(normalized))
	hash := hex.EncodeToString(digest[:])
	if hash != authorization.QueryHash {
		return PreparedQuery{}, fmt.Errorf("%w: authorized PromQL hash does not match", ErrUnauthorized)
	}
	return PreparedQuery{
		ConfigurationRevision: revision.ID, DefinitionID: authorization.DefinitionID,
		AuthorizationID: authorization.ID, Actor: ActorAgent, Mode: authorization.QueryMode,
		CatalogKey: authorization.CatalogKey, Query: normalized, QueryHash: hash,
		Scope: scope, Resource: resource, TimeRange: queryRange, Bounds: bounds,
	}, nil
}

func PrepareCatalog(request CatalogRequest, revision settings.Revision) (ProviderCatalogRequest, []CatalogEntry, error) {
	provider, err := prometheusConfiguration(revision)
	if err != nil {
		return ProviderCatalogRequest{}, nil, err
	}
	if !provider.Enabled {
		return ProviderCatalogRequest{}, nil, ErrProviderDisabled
	}
	scope, resource, err := boundedScope(request.ClusterID, request.Namespace, request.Resource, revision)
	if err != nil {
		return ProviderCatalogRequest{}, nil, err
	}
	bounds := providerBounds(revision, provider, 30)
	queries, err := GuidedCatalog(scope, resource, time.Duration(bounds.MaxLookbackSeconds)*time.Second)
	if err != nil {
		return ProviderCatalogRequest{}, nil, err
	}
	return ProviderCatalogRequest{
		ConfigurationRevision: revision.ID, Scope: scope, Resource: resource, Bounds: bounds,
	}, queries, nil
}

func ValidateProviderCatalog(request ProviderCatalogRequest, revision settings.Revision) error {
	prepared, _, err := PrepareCatalog(CatalogRequest{
		ClusterID: request.Scope.ClusterID, Namespace: request.Resource.Namespace, Resource: request.Resource,
	}, revision)
	if err != nil {
		return err
	}
	if request.ConfigurationRevision != revision.ID || request.Scope.Environment != prepared.Scope.Environment ||
		request.Bounds != prepared.Bounds {
		return fmt.Errorf("%w: Provider catalog request does not match its Configuration Revision", ErrUnauthorized)
	}
	return nil
}

func ValidateProviderQuery(request ProviderQueryRequest, revision settings.Revision) error {
	owner, err := PrepareOwnerQuery(StartQueryRequest{
		Mode: ModeExpert, Query: request.Query, ClusterID: request.Scope.ClusterID,
		Namespace: request.Resource.Namespace, Resource: request.Resource,
		From: request.TimeRange.From, To: request.TimeRange.To, StepSeconds: request.Bounds.StepSeconds,
	}, revision)
	if err != nil {
		return err
	}
	if request.ConfigurationRevision != revision.ID || request.Query != owner.Query || request.QueryHash != owner.QueryHash ||
		request.Scope.Environment != owner.Scope.Environment || request.Bounds.StepSeconds != owner.Bounds.StepSeconds ||
		request.Bounds.MaxLookbackSeconds > owner.Bounds.MaxLookbackSeconds || request.Bounds.TimeoutMS > owner.Bounds.TimeoutMS ||
		request.Bounds.MaxResponseBytes > owner.Bounds.MaxResponseBytes || request.Bounds.MaxSeries > owner.Bounds.MaxSeries ||
		request.Bounds.MaxSamples > owner.Bounds.MaxSamples || request.Bounds.ConcurrencyLimit > owner.Bounds.ConcurrencyLimit {
		return fmt.Errorf("%w: Provider query exceeds its Configuration Revision contract", ErrUnauthorized)
	}
	return nil
}

func boundedScope(clusterID, namespace string, resource ResourceReference, revision settings.Revision) (settings.OperationalScope, ResourceReference, error) {
	clusterID = strings.TrimSpace(clusterID)
	namespace = strings.TrimSpace(namespace)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.Namespace = strings.TrimSpace(resource.Namespace)
	resource.Name = strings.TrimSpace(resource.Name)
	if clusterID == "" || namespace == "" || resource.ID == "" || resource.Kind == "" || resource.Name == "" {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: cluster, namespace, and Workload identity are required", ErrInvalid)
	}
	if resource.Namespace != namespace {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: Workload namespace does not match the requested namespace", ErrInvalid)
	}
	if !slices.Contains([]string{"Deployment", "StatefulSet", "DaemonSet"}, resource.Kind) {
		return settings.OperationalScope{}, ResourceReference{}, fmt.Errorf("%w: Monitoring resource must be a Workload", ErrInvalid)
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

func boundedRange(from, to time.Time, stepSeconds int, revision settings.Revision, provider settings.ProviderConfiguration) (QueryBounds, TimeRange, error) {
	from, to = from.UTC(), to.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: an absolute increasing UTC time range is required", ErrInvalid)
	}
	lookback := to.Sub(from)
	if lookback > time.Duration(revision.General.QueryMaxLookbackSeconds)*time.Second {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: time range exceeds query_max_lookback_seconds", ErrBoundExceeded)
	}
	if stepSeconds < 1 || stepSeconds > 3600 {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: step_seconds must be between 1 and 3600", ErrInvalid)
	}
	maxSamples := minPositive(revision.General.QueryMaxResults, MaximumSamples)
	estimatedPoints := int(lookback/time.Duration(stepSeconds)/time.Second) + 1
	if estimatedPoints > maxSamples {
		return QueryBounds{}, TimeRange{}, fmt.Errorf("%w: time range and step would request %d points; maximum is %d", ErrBoundExceeded, estimatedPoints, maxSamples)
	}
	bounds := providerBounds(revision, provider, stepSeconds)
	return bounds, TimeRange{From: from, To: to}, nil
}

func providerBounds(revision settings.Revision, provider settings.ProviderConfiguration, stepSeconds int) QueryBounds {
	return QueryBounds{
		MaxLookbackSeconds: revision.General.QueryMaxLookbackSeconds,
		TimeoutMS:          minPositive(provider.TimeoutMS, 30_000),
		MaxResponseBytes:   MaximumResponseBytes,
		MaxSeries:          minPositive(provider.MaxResults, revision.General.QueryMaxResults, MaximumSeries),
		MaxSamples:         minPositive(revision.General.QueryMaxResults, MaximumSamples),
		ConcurrencyLimit:   MaximumConcurrent,
		StepSeconds:        stepSeconds,
	}
}

func normalizePromQL(raw string, scope settings.OperationalScope, resource ResourceReference, maxLookback time.Duration) (string, error) {
	promQLParser := parser.NewParser(parser.Options{})
	expression, err := promQLParser.ParseExpr(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: PromQL syntax is invalid: %v", ErrInvalid, err)
	}
	boundLabels := []struct {
		name  string
		value string
	}{
		{name: "cluster_id", value: scope.ClusterID},
		{name: "environment", value: scope.Environment},
		{name: "namespace", value: resource.Namespace},
		{name: "workload_kind", value: resource.Kind},
		{name: "workload", value: resource.Name},
	}
	selectors, nodes := 0, 0
	var validationErr error
	parser.Inspect(expression, func(node parser.Node, _ []parser.Node) error {
		if validationErr != nil || node == nil {
			return nil
		}
		nodes++
		if nodes > MaximumASTNodes {
			validationErr = errors.New("PromQL expression is too complex")
			return nil
		}
		switch value := node.(type) {
		case *parser.VectorSelector:
			selectors++
			if value.Timestamp != nil || value.StartOrEnd != 0 || value.OriginalOffset != 0 || value.OriginalOffsetExpr != nil {
				validationErr = errors.New("@ and offset modifiers are not allowed in bounded queries")
				return nil
			}
			for _, bound := range boundLabels {
				found := false
				for _, matcher := range value.LabelMatchers {
					if matcher.Name != bound.name {
						continue
					}
					found = true
					if matcher.Type != labels.MatchEqual || matcher.Value != bound.value {
						validationErr = fmt.Errorf("label %s must exactly match the Operational Scope", bound.name)
						return nil
					}
				}
				if !found {
					matcher, matcherErr := labels.NewMatcher(labels.MatchEqual, bound.name, bound.value)
					if matcherErr != nil {
						validationErr = matcherErr
						return nil
					}
					value.LabelMatchers = append(value.LabelMatchers, matcher)
				}
			}
		case *parser.MatrixSelector:
			if value.Range <= 0 || value.Range > maxLookback {
				validationErr = errors.New("range selector exceeds the configured lookback")
			}
		case *parser.SubqueryExpr:
			if value.Range <= 0 || value.Range > maxLookback || value.Timestamp != nil || value.StartOrEnd != 0 || value.OriginalOffset != 0 || value.OriginalOffsetExpr != nil {
				validationErr = errors.New("subquery exceeds bounded time semantics")
			}
		}
		return nil
	})
	if validationErr != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, validationErr)
	}
	if selectors == 0 {
		return "", fmt.Errorf("%w: PromQL must select scoped metric series", ErrInvalid)
	}
	normalized := expression.String()
	if len(normalized) > MaximumQueryBytes {
		return "", fmt.Errorf("%w: normalized PromQL exceeds %d bytes", ErrInvalid, MaximumQueryBytes)
	}
	return normalized, nil
}

func prometheusConfiguration(revision settings.Revision) (settings.ProviderConfiguration, error) {
	for _, provider := range revision.Providers {
		if provider.Provider == settings.ProviderPrometheus {
			return provider, nil
		}
	}
	return settings.ProviderConfiguration{}, fmt.Errorf("%w: Prometheus configuration is missing", ErrUnavailable)
}

func minPositive(values ...int) int {
	result := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if result == 0 || value < result {
			result = value
		}
	}
	return result
}
