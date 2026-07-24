package investigationread

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/runbook"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const (
	ToolInspectWorkload         = "inspect_workload"
	ToolInspectKubernetesEvents = "inspect_kubernetes_events"
	ToolQueryMetrics            = "query_metrics"
	ToolQueryLogs               = "query_logs"
	ToolQueryTraces             = "query_traces"
	ToolGetDeploymentContext    = "get_deployment_context"
	ToolGetChangeDetail         = "get_change_detail"
	ToolSearchRunbooks          = "search_runbooks"

	TemplateWorkloadV1          = "workload-snapshot/v1"
	TemplateEventsV1            = "kubernetes-events/v1"
	TemplateMetricsV1           = "readiness-and-5xx/v1"
	TemplateLogsV1              = "required-env-logs/v1"
	TemplateTracesV1            = "request-failures/v1"
	TemplateDeploymentContextV1 = "deployment-context/v1"
	TemplateChangeDetailV1      = "change-detail/v1"
	TemplateRunbookV1           = "runbook-search/v1"
)

type prometheusReader interface {
	ObserveV3(context.Context, observabilityread.V3MetricQuery) (verification.Observation, error)
}

type elasticReader interface {
	Search(context.Context, observabilityread.ElasticQuery) (verification.Observation, error)
}

type traceReader interface {
	ObserveTraceErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error)
}

type githubReader interface {
	change.GitHubReader
	GetFileContent(context.Context, change.RepositoryRef, string, string) (change.FileContent, error)
}

type runbookSearcher interface {
	Search(context.Context, runbook.SearchRequest) ([]runbook.SearchResult, error)
}

type Target struct {
	Service         string
	Cluster         string
	Environment     string
	Namespace       string
	Workload        string
	Container       string
	Repository      change.RepositoryRef
	BaseBranch      string
	GitOpsPath      string
	ArgoPath        string
	ArgoApplication string
	ArgoProject     string
	EnvKey          string
	GrafanaURL      string
	KibanaURL       string
	TempoURL        string
}

type Config struct {
	DB             *sql.DB
	Kubernetes     kubernetes.Interface
	Prometheus     prometheusReader
	Elasticsearch  elasticReader
	Tempo          traceReader
	GitHub         githubReader
	Argo           change.ArgoCDReader
	Runtime        change.RuntimeReader
	Registry       change.RegistryMetadataReader
	Runbooks       runbookSearcher
	Target         Target
	RequestTimeout time.Duration
	Now            func() time.Time
}

// Toolset implements all eight V3 read contracts behind one exact dispatcher.
// Provider-specific query languages and mutable endpoints are absent from the
// public action schema.
type Toolset struct{ cfg Config }

var _ agent.InvestigationReadTool = (*Toolset)(nil)

func New(config Config) (*Toolset, error) {
	if config.DB == nil || config.Kubernetes == nil || config.Prometheus == nil || config.Elasticsearch == nil ||
		config.Tempo == nil || config.GitHub == nil || config.Argo == nil || config.Runtime == nil ||
		config.Registry == nil || config.Runbooks == nil {
		return nil, errors.New("V3 investigation tools require MySQL and all eight bounded provider surfaces")
	}
	if err := validateTarget(config.Target); err != nil {
		return nil, err
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 10 * time.Second
	}
	if config.RequestTimeout < time.Second || config.RequestTimeout > 30*time.Second {
		return nil, errors.New("V3 investigation tool timeout is outside 1s..30s")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Toolset{cfg: config}, nil
}

func validateTarget(target Target) error {
	values := []string{target.Service, target.Cluster, target.Environment, target.Namespace, target.Workload, target.Container,
		target.Repository.Owner, target.Repository.Name, target.BaseBranch, target.GitOpsPath, target.ArgoPath,
		target.ArgoApplication, target.ArgoProject, target.EnvKey}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 1024 {
			return errors.New("V3 investigation target identity is incomplete")
		}
	}
	if target.Repository.FullName() == "/" || strings.Trim(target.ArgoPath, "/") == strings.Trim(target.GitOpsPath, "/") ||
		!regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`).MatchString(target.EnvKey) {
		return errors.New("V3 investigation target policy is invalid")
	}
	for _, raw := range []string{target.GrafanaURL, target.KibanaURL, target.TempoURL} {
		if raw != "" && !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
			return errors.New("V3 investigation deep-link base is invalid")
		}
	}
	return nil
}

func GoldenActionPolicies() map[string]agent.ToolActionPolicy {
	return map[string]agent.ToolActionPolicy{
		ToolInspectWorkload: {
			TemplateIDs:       []string{TemplateWorkloadV1},
			ExpectedFactTypes: []string{"workload.subject_confirmed", "kubernetes.required_env_absent", "kubernetes.required_env_present"},
		},
		ToolInspectKubernetesEvents: {
			TemplateIDs: []string{TemplateEventsV1}, ParameterKeys: []string{"window", "limit"},
			ParameterSpecs: map[string]agent.ParameterSpec{
				"window": windowParameterSpec(), "limit": {Type: agent.ParameterInteger},
			},
			ExpectedFactTypes: []string{"kubernetes.warning_events_present", "kubernetes.no_warning_events"},
		},
		ToolQueryMetrics: {
			TemplateIDs: []string{TemplateMetricsV1}, ParameterKeys: []string{"window"},
			ParameterSpecs:    map[string]agent.ParameterSpec{"window": windowParameterSpec()},
			ExpectedFactTypes: []string{"metric.readiness_or_5xx_failure", "metric.symptom_absent"},
		},
		ToolQueryLogs: {
			TemplateIDs: []string{TemplateLogsV1}, ParameterKeys: []string{"window", "severity", "keyword", "trace_id", "limit"},
			ParameterSpecs: map[string]agent.ParameterSpec{
				"window": windowParameterSpec(), "severity": {Type: agent.ParameterString}, "keyword": {Type: agent.ParameterString},
				"trace_id": {Type: agent.ParameterString}, "limit": {Type: agent.ParameterInteger},
			},
			ExpectedFactTypes: []string{"log.required_env_missing", "log.required_env_missing_absent"},
		},
		ToolQueryTraces: {
			TemplateIDs: []string{TemplateTracesV1}, ParameterKeys: []string{"window", "status", "trace_id", "limit"},
			ParameterSpecs: map[string]agent.ParameterSpec{
				"window": windowParameterSpec(), "status": {Type: agent.ParameterString, Enum: []string{"error", "all"}},
				"trace_id": {Type: agent.ParameterString}, "limit": {Type: agent.ParameterInteger},
			},
			ExpectedFactTypes: []string{"trace.request_failure", "trace.request_failure_absent"},
		},
		ToolGetDeploymentContext: {
			TemplateIDs: []string{TemplateDeploymentContextV1}, ParameterKeys: []string{"window"},
			ParameterSpecs:    map[string]agent.ParameterSpec{"window": windowParameterSpec()},
			ExpectedFactTypes: []string{"argocd.bad_revision_deployed", "argocd.bad_revision_not_deployed", "source_revision.unchanged", "image_digest.unchanged", "deployment.source_and_image_changed", "deployment.change_ref"},
		},
		ToolGetChangeDetail: {
			TemplateIDs: []string{TemplateChangeDetailV1}, ParameterKeys: []string{"change_ref"},
			ParameterSpecs:    map[string]agent.ParameterSpec{"change_ref": {Type: agent.ParameterString}},
			ExpectedFactTypes: []string{"gitops.required_env_removed", "gitops.required_env_not_removed", "change.ci_succeeded", "change.ci_not_succeeded"},
		},
		ToolSearchRunbooks: {
			TemplateIDs: []string{TemplateRunbookV1}, ParameterKeys: []string{"query", "limit"},
			ParameterSpecs: map[string]agent.ParameterSpec{
				"query": {Type: agent.ParameterString}, "limit": {Type: agent.ParameterInteger},
			},
			ExpectedFactTypes: []string{"runbook.guidance_found", "runbook.guidance_not_found"},
		},
	}
}

func windowParameterSpec() agent.ParameterSpec {
	return agent.ParameterSpec{Type: agent.ParameterString, Enum: []string{"1m", "5m", "15m", "30m"}}
}

func RequiredSources() []string {
	return []string{"kubernetes", "prometheus", "elasticsearch", "tempo", "argocd", "github", "registry"}
}

func (t *Toolset) Execute(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if t == nil || request.IncidentPublicID == "" || request.CycleNo == 0 || request.Action.ScopeRef == "" ||
		request.Correlation.Cluster != t.cfg.Target.Cluster || request.Correlation.Environment != t.cfg.Target.Environment ||
		request.Correlation.Namespace != t.cfg.Target.Namespace || request.Correlation.Workload != t.cfg.Target.Workload ||
		!strings.EqualFold(request.Correlation.TargetKind, "Deployment") {
		return agent.ToolObservation{}, agent.NewRuntimeError(agent.ErrorPermission, "investigation scope is not the configured Golden target", agent.ErrPermission)
	}
	ctx, cancel := context.WithTimeout(ctx, t.cfg.RequestTimeout)
	defer cancel()
	switch request.Action.Tool {
	case ToolInspectWorkload:
		return t.inspectWorkload(ctx, request)
	case ToolInspectKubernetesEvents:
		return t.inspectEvents(ctx, request)
	case ToolQueryMetrics:
		return t.queryMetrics(ctx, request)
	case ToolQueryLogs:
		return t.queryLogs(ctx, request)
	case ToolQueryTraces:
		return t.queryTraces(ctx, request)
	case ToolGetDeploymentContext:
		return t.deploymentContext(ctx, request)
	case ToolGetChangeDetail:
		return t.changeDetail(ctx, request)
	case ToolSearchRunbooks:
		return t.searchRunbooks(ctx, request)
	default:
		return agent.ToolObservation{}, agent.NewRuntimeError(agent.ErrorPermission, "investigation tool is not allowlisted", agent.ErrPermission)
	}
}

func (t *Toolset) queryMetrics(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateMetricsV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		Window string `json:"window"`
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	lookback, err := boundedWindow(params.Window, 5*time.Minute)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	base := observabilityread.V3MetricQuery{Service: t.cfg.Target.Service, Namespace: t.cfg.Target.Namespace, Environment: t.cfg.Target.Environment, WorkloadName: t.cfg.Target.Workload, Lookback: lookback}
	base.Kind = observabilityread.V3MetricErrorRate
	errorRate, readErr := t.cfg.Prometheus.ObserveV3(ctx, base)
	if readErr != nil {
		return unavailable(request.Action, "prometheus", "prometheus/readiness-and-5xx", readErr), nil
	}
	base.Kind = observabilityread.V3MetricReadiness
	readiness, readyErr := t.cfg.Prometheus.ObserveV3(ctx, base)
	if readyErr != nil {
		return unavailable(request.Action, "prometheus", "prometheus/readiness-and-5xx", readyErr), nil
	}
	if errorRate.Status == verification.ObservationNoData || readiness.Status == verification.ObservationNoData {
		return noData(request.Action, "prometheus", "prometheus/readiness-and-5xx", "fixed readiness or request series has no data", t.cfg.Target.GrafanaURL), nil
	}
	factType := "metric.symptom_absent"
	if errorRate.Value > 0 || readiness.Value < 1 {
		factType = "metric.readiness_or_5xx_failure"
	}
	attributes := map[string]string{
		"error_rate": formatFloat(errorRate.Value), "readiness": formatFloat(readiness.Value),
		"request_count": formatFloat(errorRate.Denominator), "window": lookback.String(),
	}
	fact := typedFact(request, factType, "prometheus", "prometheus/readiness-and-5xx", "runtime_observation", "support", true, attributes)
	return available(request.Action, "prometheus", "prometheus/readiness-and-5xx", "fixed readiness and 5xx templates returned bounded facts", []agent.EvidenceFact{fact}, t.cfg.Target.GrafanaURL), nil
}

func (t *Toolset) queryLogs(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateLogsV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		Window, Severity, Keyword, TraceID string
		Limit                              int
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	lookback, err := boundedWindow(params.Window, 5*time.Minute)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	if params.Severity == "" {
		params.Severity = "error"
	}
	if params.Keyword == "" {
		params.Keyword = "required_env_missing"
	}
	if params.Limit == 0 {
		params.Limit = 3
	}
	observation, readErr := t.cfg.Elasticsearch.Search(ctx, observabilityread.ElasticQuery{
		Service: t.cfg.Target.Service, Namespace: t.cfg.Target.Namespace, Environment: t.cfg.Target.Environment,
		Workload: t.cfg.Target.Workload, Lookback: lookback, Severity: params.Severity, Keyword: params.Keyword,
		TraceID: params.TraceID, Limit: params.Limit,
	})
	if readErr != nil {
		return unavailable(request.Action, "elasticsearch", "elasticsearch/required-env", readErr), nil
	}
	if observation.Status == verification.ObservationNoData {
		return noData(request.Action, "elasticsearch", "elasticsearch/required-env", "valid bounded Elasticsearch query observed no matching logs", t.cfg.Target.KibanaURL), nil
	}
	factType := "log.required_env_missing_absent"
	if observation.MatchedCount > 0 {
		factType = "log.required_env_missing"
	}
	attributes := map[string]string{"matched_count": strconv.Itoa(observation.MatchedCount), "window": lookback.String()}
	if len(observation.RedactedExamples) > 0 {
		attributes["redacted_sample"] = safeText(observation.RedactedExamples[0], 512)
	}
	fact := typedFact(request, factType, "elasticsearch", "elasticsearch/required-env", "runtime_observation", "support", true, attributes)
	return available(request.Action, "elasticsearch", "elasticsearch/required-env", "fixed Elasticsearch query returned redacted structured facts", []agent.EvidenceFact{fact}, t.cfg.Target.KibanaURL), nil
}

func (t *Toolset) queryTraces(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateTracesV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		Window, Status, TraceID string
		Limit                   int
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	lookback, err := boundedWindow(params.Window, 5*time.Minute)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	if params.Status != "" && params.Status != "error" && params.Status != "all" {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	if params.Limit < 0 || params.Limit > 100 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	if params.TraceID != "" && !regexp.MustCompile(`^[a-f0-9]{16}(?:[a-f0-9]{16})?$`).MatchString(strings.ToLower(params.TraceID)) {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	result, readErr := t.cfg.Tempo.ObserveTraceErrorRate(ctx, verification.SignalQuery{
		Template: string(verification.CheckTraceErrorRateBelow), Service: t.cfg.Target.Service,
		Namespace: t.cfg.Target.Namespace, Environment: t.cfg.Target.Environment, Lookback: lookback,
		Step: 10 * time.Second, MaxSeries: 1, MaxSamples: maxInt(20, params.Limit),
	})
	if readErr != nil {
		return unavailable(request.Action, "tempo", "tempo/request-failures", readErr), nil
	}
	observation := result.Observation
	if observation.Status == verification.ObservationNoData {
		return noData(request.Action, "tempo", "tempo/request-failures", "fixed Tempo query observed no request traces", t.cfg.Target.TempoURL), nil
	}
	factType := "trace.request_failure_absent"
	if observation.MatchedCount > 0 || observation.Value > 0 {
		factType = "trace.request_failure"
	}
	fact := typedFact(request, factType, "tempo", "tempo/request-failures", "runtime_observation", "support", true, map[string]string{
		"error_rate": formatFloat(observation.Value), "error_spans": strconv.Itoa(observation.MatchedCount),
		"request_spans": formatFloat(observation.Denominator), "window": lookback.String(),
	})
	return available(request.Action, "tempo", "tempo/request-failures", "fixed Tempo template returned bounded trace facts", []agent.EvidenceFact{fact}, t.cfg.Target.TempoURL), nil
}

func (t *Toolset) searchRunbooks(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateRunbookV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	params.Query = safeText(params.Query, 256)
	if params.Query == "" {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	if params.Limit == 0 {
		params.Limit = 2
	}
	if params.Limit < 1 || params.Limit > 3 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	terms := strings.Fields(params.Query)
	if len(terms) > 12 {
		terms = terms[:12]
	}
	results, err := t.cfg.Runbooks.Search(ctx, runbook.SearchRequest{Keywords: terms, Limit: params.Limit, Rerank: false})
	if err != nil {
		return unavailable(request.Action, "runbook", "runbook/bm25", err), nil
	}
	if len(results) == 0 {
		return noData(request.Action, "runbook", "runbook/bm25", "bounded BM25 search found no guidance", ""), nil
	}
	facts := make([]agent.EvidenceFact, 0, len(results))
	for _, result := range results {
		facts = append(facts, typedFact(request, "runbook.guidance_found", "runbook", "runbook/bm25", "curated_guidance", "forbidden", false, map[string]string{
			"title": safeText(result.Title, 256), "file": safeText(result.File, 256),
			"score": formatFloat(result.Score), "snippet": safeText(result.Snippet, 800),
		}))
	}
	return available(request.Action, "runbook", "runbook/bm25", "bounded BM25 runbook fragments are guidance only", facts, ""), nil
}

func decodeParameters(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return agent.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return agent.ErrInvalidArgument
	}
	return nil
}

func boundedWindow(raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	switch raw {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	default:
		return 0, agent.ErrInvalidArgument
	}
}

func typedFact(request agent.InvestigationToolRequest, factType, source, path, authority, claimUse string, direct bool, attributes map[string]string) agent.EvidenceFact {
	for key, value := range attributes {
		attributes[key] = safeText(value, 1024)
	}
	return agent.EvidenceFact{
		ID: factID(request.Action, factType, attributes), Type: factType, SourceSystem: source, CollectionPath: path,
		CorroborationGroup: source + "/" + path, Authority: authority, Integrity: "verified", Freshness: "fresh",
		Completeness: "complete", ClaimUse: claimUse, CollectionStatus: agent.CollectionAvailable, Direct: direct,
		Attributes: attributes,
	}
}

func factID(action agent.ProposedAction, factType string, attributes map[string]string) string {
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{action.Tool, action.TemplateID, action.ScopeRef, factType}
	for _, key := range keys {
		parts = append(parts, key, attributes[key])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func available(action agent.ProposedAction, source, path, summary string, facts []agent.EvidenceFact, link string) agent.ToolObservation {
	return agent.ToolObservation{Status: agent.CollectionAvailable, SourceSystem: source, CollectionPath: path, TemplateVersion: action.TemplateID, Summary: safeText(summary, 4096), Facts: facts, SafeDeepLink: link}
}
func noData(action agent.ProposedAction, source, path, summary, link string) agent.ToolObservation {
	return agent.ToolObservation{Status: agent.CollectionNoData, SourceSystem: source, CollectionPath: path, TemplateVersion: action.TemplateID, Summary: safeText(summary, 4096), SafeDeepLink: link}
}
func unavailable(action agent.ProposedAction, source, path string, err error) agent.ToolObservation {
	reason := "provider unavailable"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "provider deadline exceeded"
	}
	return agent.ToolObservation{Status: agent.CollectionUnavailable, SourceSystem: source, CollectionPath: path, TemplateVersion: action.TemplateID, Summary: reason, Provenance: map[string]string{"reason": reason}}
}

func safeText(value string, limit int) string {
	value = observabilityread.Redact(strings.ToValidUTF8(strings.TrimSpace(value), "?"))
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8Prefix(value, limit) {
		limit--
	}
	return value[:limit]
}

func utf8Prefix(value string, length int) bool {
	return length == 0 || length <= len(value) && utf8.ValidString(value[:length])
}
func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', 6, 64) }
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func changeReference(repository change.RepositoryRef, revision string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("cloudops-change\x00"+strings.ToLower(repository.FullName())+"\x00"+strings.ToLower(revision))).String()
}

func repositoryFromImage(image string) (string, error) {
	image = strings.TrimSpace(image)
	if index := strings.LastIndex(image, "@"); index >= 0 {
		image = image[:index]
	}
	lastSlash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > lastSlash {
		image = image[:colon]
	}
	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return "", change.ErrInvalidArgument
	}
	return strings.Join(parts[1:], "/"), nil
}

func exactRevision(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func remediationTarget(target Target) remediation.TargetResource {
	return remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: target.Namespace, Name: target.Workload, Container: target.Container}
}
