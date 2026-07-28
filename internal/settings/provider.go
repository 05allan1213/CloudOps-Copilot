package settings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Service) testProvider(ctx context.Context, config ProviderConfiguration, refs []SecretReference, clusterID string) ProviderResult {
	result := ProviderResult{Provider: config.Provider}
	if !config.Enabled {
		result.State = "disabled"
		result.Detail = "Provider 在当前配置中未启用"
		return result
	}
	now := s.now().UTC()
	result.CheckedAt = &now
	if config.Provider == ProviderKubernetes {
		probe := s.providerProbes[ProviderKubernetes]
		if probe == nil {
			result.State = "unavailable"
			result.Detail = "Kubernetes typed Provider Gateway 未装配"
			return result
		}
		detail, err := probe(ctx, strings.TrimSpace(clusterID))
		if err != nil {
			result.State = "unavailable"
			result.Detail = "Kubernetes typed Provider Gateway 连接失败"
			return result
		}
		result.State = "available"
		result.Detail = detail
		return result
	}
	endpoint, err := providerProbeURL(config)
	if err != nil {
		result.State = "unavailable"
		result.Detail = "Provider endpoint 无法构造有界健康检查"
		return result
	}
	requestCtx, cancel := context.WithTimeout(ctx, minDuration(time.Duration(config.TimeoutMS)*time.Millisecond, s.httpTimeout))
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.State = "unavailable"
		result.Detail = "Provider 健康检查请求无效"
		return result
	}
	request.Header.Set("Accept", "application/json")
	secret, secretErr := s.secretValue(ctx, config.Provider, refs)
	if secretErr == nil && len(secret) > 0 {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
		zeroBytes(secret)
	} else if providerNeedsSecret(config.Provider) {
		result.State = "unavailable"
		result.Detail = "Provider secret version 缺失或不可读取"
		return result
	}
	client := &http.Client{
		Timeout: minDuration(time.Duration(config.TimeoutMS)*time.Millisecond, s.httpTimeout),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("provider probe redirects are disabled")
		},
	}
	response, err := client.Do(request)
	if err != nil {
		result.State = "unavailable"
		result.Detail = "Provider 连接失败；检查 endpoint、网络与 secret 状态"
		return result
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.State = "unavailable"
		result.Detail = fmt.Sprintf("Provider 健康检查返回 HTTP %d", response.StatusCode)
		return result
	}
	result.State = "available"
	result.Detail = "Provider 有界连接测试通过"
	return result
}

func (s *Service) testKubernetesScopes(ctx context.Context, config ProviderConfiguration, refs []SecretReference, scopes []OperationalScope) (ProviderResult, []FieldError) {
	result := ProviderResult{Provider: ProviderKubernetes, State: "available"}
	var fieldErrors []FieldError
	available := 0
	for index, scope := range scopes {
		probeResult := s.testProvider(ctx, config, refs, scope.ClusterID)
		result.CheckedAt = probeResult.CheckedAt
		if probeResult.State == "available" {
			available++
			continue
		}
		fieldErrors = append(fieldErrors, FieldError{
			Field: fmt.Sprintf("scopes.%d.cluster_id", index), Code: "KUBERNETES_READER_UNAVAILABLE",
			Message: fmt.Sprintf("Cluster %q 未注册可用的 Kubernetes typed reader", scope.ClusterID),
		})
	}
	if len(fieldErrors) > 0 {
		result.State = "unavailable"
		result.Detail = fmt.Sprintf("Kubernetes typed readers %d/%d 可用", available, len(scopes))
		return result, fieldErrors
	}
	result.Detail = fmt.Sprintf("Kubernetes typed readers %d/%d 可用", available, len(scopes))
	return result, nil
}

func providerProbeURL(config ProviderConfiguration) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(config.Endpoint, "/"))
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", errors.New("invalid provider endpoint")
	}
	suffix := ""
	switch config.Provider {
	case ProviderLLM:
		parsed.Path = strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/chat/completions")
		suffix = "/models"
	case ProviderPrometheus, ProviderAlertmanager:
		suffix = "/-/ready"
	case ProviderTempo:
		suffix = "/ready"
	case ProviderGitHub:
		suffix = "/rate_limit"
	case ProviderArgoCD:
		suffix = "/api/version"
	case ProviderElasticsearch:
		suffix = ""
	default:
		return "", errors.New("provider has no HTTP probe")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix
	return parsed.String(), nil
}

func minDuration(left, right time.Duration) time.Duration {
	if left <= 0 || left > right {
		return right
	}
	return left
}
