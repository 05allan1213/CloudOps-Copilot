package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	clusterIdentityPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	namespacePattern       = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	purposePattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	labelNamePattern       = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

const maximumProviderTimeoutMS = 300000

func normalizeDraft(input Draft) (Draft, []FieldError, string) {
	result := input
	result.Summary = strings.TrimSpace(result.Summary)
	result.Scope = normalizeScope(result.Scope)
	if len(result.Scopes) == 0 {
		result.Scopes = []OperationalScope{result.Scope}
	} else {
		for index := range result.Scopes {
			result.Scopes[index] = normalizeScope(result.Scopes[index])
		}
	}
	sort.Slice(result.Scopes, func(i, j int) bool {
		if result.Scopes[i].ClusterID != result.Scopes[j].ClusterID {
			return result.Scopes[i].ClusterID < result.Scopes[j].ClusterID
		}
		return result.Scopes[i].Name < result.Scopes[j].Name
	})
	result.SecretRefs = normalizeSecretReferences(result.SecretRefs)
	result.EscalationPolicies = normalizeEscalationPolicies(result.EscalationPolicies)

	providers := make(map[Provider]ProviderConfiguration, len(result.Providers))
	for _, item := range result.Providers {
		item.Endpoint = strings.TrimRight(strings.TrimSpace(item.Endpoint), "/")
		item.Model = strings.TrimSpace(item.Model)
		item.ContextLinkBase = strings.TrimRight(strings.TrimSpace(item.ContextLinkBase), "/")
		providers[item.Provider] = item
	}
	result.Providers = make([]ProviderConfiguration, 0, len(operationalProviders))
	for _, provider := range operationalProviders {
		item, ok := providers[provider]
		if !ok {
			item = defaultProviderConfiguration(provider)
		}
		result.Providers = append(result.Providers, item)
	}

	errors := validateNormalizedDraft(result)
	encoded, err := json.Marshal(result)
	if err != nil {
		errors = append(errors, FieldError{Field: "draft", Code: "ENCODING_FAILED", Message: "配置草稿无法生成稳定内容标识"})
		return result, errors, ""
	}
	digest := sha256.Sum256(encoded)
	return result, errors, hex.EncodeToString(digest[:])
}

func normalizeScope(value OperationalScope) OperationalScope {
	value.ID = ""
	value.RevisionID = ""
	value.RevisionHash = ""
	value.Active = false
	value.Name = strings.TrimSpace(value.Name)
	value.ClusterID = strings.TrimSpace(value.ClusterID)
	value.Environment = strings.TrimSpace(value.Environment)
	value.Namespaces = normalizeStrings(value.Namespaces)
	return value
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeSecretReferences(values []SecretReference) []SecretReference {
	result := make([]SecretReference, 0, len(values))
	for _, value := range values {
		value.Purpose = strings.TrimSpace(value.Purpose)
		value.SecretVersionID = strings.TrimSpace(value.SecretVersionID)
		value.State = ""
		value.Fingerprint = ""
		if value.Provider.Operational() && value.Purpose != "" && value.SecretVersionID != "" {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Purpose < result[j].Purpose
	})
	return result
}

func normalizeEscalationPolicies(values []EscalationPolicy) []EscalationPolicy {
	result := make([]EscalationPolicy, 0, len(values))
	for _, value := range values {
		value.ID = ""
		value.ConfigurationRevisionID = ""
		value.Name = strings.TrimSpace(value.Name)
		value.Severities = normalizeLowerStrings(value.Severities)
		value.Namespaces = normalizeStrings(value.Namespaces)
		matchers := make(map[string]string, len(value.LabelMatchers))
		for name, expected := range value.LabelMatchers {
			matchers[strings.TrimSpace(name)] = strings.TrimSpace(expected)
		}
		value.LabelMatchers = matchers
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].Name), strings.ToLower(result[j].Name)
		if left != right {
			return left < right
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func normalizeLowerStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.ToLower(value)
	}
	return normalizeStrings(result)
}

func validateNormalizedDraft(draft Draft) []FieldError {
	var result []FieldError
	add := func(field, code, message string) {
		result = append(result, FieldError{Field: field, Code: code, Message: message})
	}
	if len(draft.Summary) < 3 || len(draft.Summary) > 255 {
		add("summary", "INVALID_LENGTH", "变更摘要需为 3 至 255 个字符")
	}
	validateScope := func(prefix string, scope OperationalScope) {
		if len(scope.Name) < 2 || len(scope.Name) > 128 {
			add(prefix+".name", "INVALID_SCOPE_NAME", "Scope 名称需为 2 至 128 个字符")
		}
		if !clusterIdentityPattern.MatchString(scope.ClusterID) {
			add(prefix+".cluster_id", "INVALID_CLUSTER", "Cluster identity 格式无效")
		}
		if !clusterIdentityPattern.MatchString(scope.Environment) || len(scope.Environment) > 64 {
			add(prefix+".environment", "INVALID_ENVIRONMENT", "Environment identity 格式无效")
		}
		if len(scope.Namespaces) < 1 || len(scope.Namespaces) > 100 {
			add(prefix+".namespaces", "INVALID_NAMESPACE_COUNT", "至少选择 1 个且最多选择 100 个 Namespace")
		}
		for _, namespace := range scope.Namespaces {
			if !namespacePattern.MatchString(namespace) {
				add(prefix+".namespaces", "INVALID_NAMESPACE", fmt.Sprintf("Namespace %q 格式无效", namespace))
			}
		}
	}
	validateScope("scope", draft.Scope)
	if len(draft.Scopes) < 1 || len(draft.Scopes) > 10 {
		add("scopes", "INVALID_SCOPE_COUNT", "必须注册 1 至 10 个 Cluster Scope")
	}
	seenClusters := make(map[string]struct{}, len(draft.Scopes))
	activeMatches := 0
	for index, scope := range draft.Scopes {
		prefix := fmt.Sprintf("scopes.%d", index)
		validateScope(prefix, scope)
		if _, exists := seenClusters[scope.ClusterID]; exists {
			add(prefix+".cluster_id", "DUPLICATE_CLUSTER", "同一 Configuration Revision 只能注册 1 个同名 Cluster")
		}
		seenClusters[scope.ClusterID] = struct{}{}
		if scope.ClusterID == draft.Scope.ClusterID {
			activeMatches++
			if scope.Name != draft.Scope.Name || scope.Environment != draft.Scope.Environment || !slices.Equal(scope.Namespaces, draft.Scope.Namespaces) {
				add("scope", "ACTIVE_SCOPE_MISMATCH", "活动 Scope 必须与注册的 Cluster Scope 完全一致")
			}
		}
	}
	if activeMatches != 1 {
		add("scope.cluster_id", "ACTIVE_SCOPE_NOT_REGISTERED", "活动 Cluster 必须精确匹配 1 个已注册 Scope")
	}
	if draft.General.QueryMaxLookbackSeconds < 60 || draft.General.QueryMaxLookbackSeconds > 30*24*60*60 {
		add("general.query_max_lookback_seconds", "INVALID_QUERY_BOUND", "查询时间范围需为 60 至 2592000 秒")
	}
	if draft.General.QueryMaxResults < 1 || draft.General.QueryMaxResults > 10000 {
		add("general.query_max_results", "INVALID_QUERY_BOUND", "查询结果上限需为 1 至 10000")
	}
	if draft.General.TelemetryRetentionDays < 1 || draft.General.TelemetryRetentionDays > 365 {
		add("general.telemetry_retention_days", "INVALID_RETENTION", "Telemetry 保留天数需为 1 至 365")
	}
	if len(draft.EscalationPolicies) > 50 {
		add("escalation_policies", "INVALID_POLICY_COUNT", "一个 Configuration Revision 最多包含 50 条 Escalation Policy")
	}
	seenPolicyNames := make(map[string]struct{}, len(draft.EscalationPolicies))
	validEnabledPolicies := 0
	for index, policy := range draft.EscalationPolicies {
		prefix := fmt.Sprintf("escalation_policies.%d", index)
		errorCount := len(result)
		if len(policy.Name) < 1 || len(policy.Name) > 128 {
			add(prefix+".name", "INVALID_POLICY_NAME", "Policy 名称需为 1 至 128 个字符")
		}
		nameKey := strings.ToLower(policy.Name)
		if _, exists := seenPolicyNames[nameKey]; exists {
			add(prefix+".name", "DUPLICATE_POLICY_NAME", "同一 Configuration Revision 的 Policy 名称必须唯一")
		}
		seenPolicyNames[nameKey] = struct{}{}
		if len(policy.Severities) < 1 || len(policy.Severities) > 4 {
			add(prefix+".severities", "INVALID_SEVERITY_COUNT", "Policy 必须选择 1 至 4 个 severity")
		}
		for _, severity := range policy.Severities {
			if !slices.Contains([]string{"unknown", "info", "warning", "critical"}, severity) {
				add(prefix+".severities", "INVALID_SEVERITY", fmt.Sprintf("severity %q 无效", severity))
			}
		}
		if len(policy.Namespaces) > 100 {
			add(prefix+".namespaces", "INVALID_NAMESPACE_COUNT", "Policy 最多匹配 100 个 Namespace")
		}
		for _, namespace := range policy.Namespaces {
			if !namespacePattern.MatchString(namespace) {
				add(prefix+".namespaces", "INVALID_NAMESPACE", fmt.Sprintf("Namespace %q 格式无效", namespace))
			}
		}
		if len(policy.LabelMatchers) > 8 {
			add(prefix+".label_matchers", "INVALID_MATCHER_COUNT", "Policy 最多包含 8 个 exact label matcher")
		}
		for name, expected := range policy.LabelMatchers {
			if !labelNamePattern.MatchString(name) || expected == "" || len(expected) > 1024 {
				add(prefix+".label_matchers", "INVALID_MATCHER", "Label matcher 必须使用有效名称和 1 至 1024 字符的 exact value")
			}
		}
		if policy.MinimumFiringSeconds < 0 || policy.MinimumFiringSeconds > 7*24*60*60 {
			add(prefix+".minimum_firing_seconds", "INVALID_FIRING_DURATION", "持续 firing 时间需为 0 至 604800 秒")
		}
		if policy.MinimumRecurrenceCount < 1 || policy.MinimumRecurrenceCount > 100 {
			add(prefix+".minimum_recurrence_count", "INVALID_RECURRENCE", "复发次数需为 1 至 100")
		}
		if !policy.CreateIncident {
			add(prefix+".create_incident", "INVALID_POLICY_ACTION", "当前 Escalation Policy 只允许显式创建 Incident")
		}
		if policy.Enabled && len(result) == errorCount {
			validEnabledPolicies++
		}
	}
	if draft.General.AutomaticEscalationEnabled && validEnabledPolicies == 0 {
		add("general.automatic_escalation_enabled", "POLICY_REQUIRED", "启用自动 escalation 前必须存在至少 1 条已启用且有效的 Policy")
	}
	if len(draft.Providers) != len(operationalProviders) {
		add("providers", "INCOMPLETE_PROVIDERS", "Provider 配置集合不完整")
	}
	for _, item := range draft.Providers {
		prefix := "providers." + string(item.Provider)
		if !item.Provider.Operational() {
			add(prefix, "UNKNOWN_PROVIDER", "Provider identity 无效")
			continue
		}
		if item.TimeoutMS < 1000 || item.TimeoutMS > maximumProviderTimeoutMS {
			add(prefix+".timeout_ms", "INVALID_TIMEOUT", "Provider timeout 需为 1000 至 300000 ms")
		}
		if item.MaxResults < 1 || item.MaxResults > 10000 {
			add(prefix+".max_results", "INVALID_LIMIT", "Provider 结果上限需为 1 至 10000")
		}
		if item.Enabled && item.Provider != ProviderKubernetes && item.Endpoint == "" {
			add(prefix+".endpoint", "ENDPOINT_REQUIRED", "启用 Provider 前必须配置 endpoint")
		}
		if item.Provider == ProviderLLM && item.Model == "" {
			add(prefix+".model", "MODEL_REQUIRED", "LLM model 不得为空")
		}
		if item.Endpoint != "" {
			if err := validateHTTPBase(item.Endpoint, item.Provider == ProviderGitHub); err != nil {
				add(prefix+".endpoint", "INVALID_ENDPOINT", err.Error())
			}
		}
		if item.ContextLinkBase != "" {
			if err := validateContextLinkBase(item.ContextLinkBase); err != nil {
				add(prefix+".context_link_base", "INVALID_CONTEXT_LINK", err.Error())
			}
		}
	}
	seenRefs := map[string]struct{}{}
	for _, ref := range draft.SecretRefs {
		key := string(ref.Provider) + "\x00" + ref.Purpose
		if !ref.Provider.Operational() || !purposePattern.MatchString(ref.Purpose) {
			add("secret_references", "INVALID_SECRET_REFERENCE", "Secret reference 的 Provider 或 purpose 无效")
		}
		if _, exists := seenRefs[key]; exists {
			add("secret_references", "DUPLICATE_SECRET_REFERENCE", "同一 Provider purpose 只能引用 1 个 secret version")
		}
		seenRefs[key] = struct{}{}
		if len(ref.SecretVersionID) != 36 {
			add("secret_references", "INVALID_SECRET_ID", "Secret version identity 无效")
		}
	}
	return result
}

func validateContextLinkBase(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("必须是无 credential、query 或 fragment 的固定 HTTP URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "http" && (host == "127.0.0.1" || host == "localhost" || host == "::1") {
		return nil
	}
	return fmt.Errorf("非 loopback Context Link 必须使用 https")
}

func defaultProviderConfiguration(provider Provider) ProviderConfiguration {
	item := ProviderConfiguration{Provider: provider, TimeoutMS: 10000, MaxResults: 200}
	switch provider {
	case ProviderLLM:
		item.Endpoint, item.Model, item.TimeoutMS, item.MaxResults = "https://api.deepseek.com/v1", "deepseek-chat", 60000, 800
	case ProviderPrometheus, ProviderElasticsearch:
		item.MaxResults = 1000
	case ProviderGitHub:
		item.Endpoint, item.ContextLinkBase = "https://api.github.com", "https://github.com"
	}
	return item
}

func validateHTTPBase(raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("必须是无 credential、query 或 fragment 的固定 HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme 必须是 http 或 https")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("该 URL 必须使用 https")
	}
	return nil
}

func secretPurposeFor(provider Provider) string {
	switch provider {
	case ProviderLLM:
		return "api_key"
	case ProviderGitHub, ProviderArgoCD, ProviderPrometheus, ProviderAlertmanager, ProviderElasticsearch, ProviderTempo:
		return "token"
	default:
		return ""
	}
}

func providerNeedsSecret(provider Provider) bool {
	return slices.Contains([]Provider{ProviderLLM, ProviderGitHub, ProviderArgoCD}, provider)
}
