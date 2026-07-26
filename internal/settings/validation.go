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
)

func normalizeDraft(input Draft) (Draft, []FieldError, string) {
	result := input
	result.Summary = strings.TrimSpace(result.Summary)
	result.Scope.ID = ""
	result.Scope.RevisionID = ""
	result.Scope.RevisionHash = ""
	result.Scope.Name = strings.TrimSpace(result.Scope.Name)
	result.Scope.ClusterID = strings.TrimSpace(result.Scope.ClusterID)
	result.Scope.Environment = strings.TrimSpace(result.Scope.Environment)
	result.Scope.Namespaces = normalizeStrings(result.Scope.Namespaces)
	result.SecretRefs = normalizeSecretReferences(result.SecretRefs)

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

func validateNormalizedDraft(draft Draft) []FieldError {
	var result []FieldError
	add := func(field, code, message string) {
		result = append(result, FieldError{Field: field, Code: code, Message: message})
	}
	if len(draft.Summary) < 3 || len(draft.Summary) > 255 {
		add("summary", "INVALID_LENGTH", "变更摘要需为 3 至 255 个字符")
	}
	if len(draft.Scope.Name) < 2 || len(draft.Scope.Name) > 128 {
		add("scope.name", "INVALID_SCOPE_NAME", "Scope 名称需为 2 至 128 个字符")
	}
	if !clusterIdentityPattern.MatchString(draft.Scope.ClusterID) {
		add("scope.cluster_id", "INVALID_CLUSTER", "Cluster identity 格式无效")
	}
	if !clusterIdentityPattern.MatchString(draft.Scope.Environment) || len(draft.Scope.Environment) > 64 {
		add("scope.environment", "INVALID_ENVIRONMENT", "Environment identity 格式无效")
	}
	if len(draft.Scope.Namespaces) < 1 || len(draft.Scope.Namespaces) > 100 {
		add("scope.namespaces", "INVALID_NAMESPACE_COUNT", "至少选择 1 个且最多选择 100 个 Namespace")
	}
	for _, namespace := range draft.Scope.Namespaces {
		if !namespacePattern.MatchString(namespace) {
			add("scope.namespaces", "INVALID_NAMESPACE", fmt.Sprintf("Namespace %q 格式无效", namespace))
		}
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
	if draft.General.AutomaticEscalationEnabled {
		add("general.automatic_escalation_enabled", "UNSUPPORTED_AUTOMATION", "当前合同不允许自动 escalation")
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
		if item.TimeoutMS < 1000 || item.TimeoutMS > 60000 {
			add(prefix+".timeout_ms", "INVALID_TIMEOUT", "Provider timeout 需为 1000 至 60000 ms")
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
			if err := validateHTTPBase(item.ContextLinkBase, true); err != nil {
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
