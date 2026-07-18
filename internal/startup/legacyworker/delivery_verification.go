package legacyworker

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/deliveryread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/service/changeintelligence"
	"github.com/05allan1213/CloudOps-Copilot/internal/service/deliveryverification"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func InitDeliveryVerification(cfg config.Config, container *di.Container) (*deliveryverification.Worker, error) {
	if !cfg.DeliveryTrackingEnabled && !cfg.VerificationEnabled {
		return nil, nil
	}
	if container.DB == nil || container.ChangeGitHub == nil || container.ChangeArgoCD == nil || container.DeliveryRollout == nil {
		return nil, fmt.Errorf("delivery verification requires initialized Phase 3 read adapters and MySQL")
	}
	repository, err := incidentmysql.NewVerificationRepository(container.DB)
	if err != nil {
		return nil, err
	}
	var sourceMappings map[string]changeintelligence.ServiceMapping
	decoder := json.NewDecoder(strings.NewReader(cfg.ChangeServiceMappingsJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sourceMappings); err != nil {
		return nil, fmt.Errorf("decode delivery mappings: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode delivery mappings: trailing JSON value")
	}
	mappings := make(map[string]deliveryverification.Mapping, len(sourceMappings))
	for key, mapping := range sourceMappings {
		mappings[strings.ToLower(strings.TrimSpace(key))] = deliveryverification.Mapping{ArgoApplication: mapping.ArgoApplication, ArgoProject: mapping.ArgoProject}
	}
	var profiles verification.Profiles
	profileDecoder := json.NewDecoder(strings.NewReader(cfg.VerificationProfilesJSON))
	profileDecoder.DisallowUnknownFields()
	if err := profileDecoder.Decode(&profiles); err != nil {
		return nil, fmt.Errorf("decode verification profiles: %w", err)
	}
	if err := profileDecoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode verification profiles: trailing JSON value")
	}
	if err := profiles.Validate(); err != nil {
		return nil, err
	}
	var metricReader verification.MetricReader
	var logReader verification.LogReader
	var traceReader verification.TraceReader
	if len(profiles.Items) > 0 {
		services, namespaces, environments := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
		for _, profile := range profiles.Items {
			services[profile.Service], namespaces[profile.Namespace], environments[profile.Environment] = struct{}{}, struct{}{}, struct{}{}
		}
		base := observabilityread.Config{Timeout: cfg.ObservabilityRequestTimeout, MaxResponseBytes: cfg.ObservabilityMaxResponseBytes, MaxSamples: cfg.ObservabilityMaxSamples, MaxSeries: cfg.ObservabilityMaxSeries, MaxTraces: cfg.ObservabilityMaxTraces, MaxLookback: cfg.ObservabilityMaxLookback, Retries: cfg.ObservabilityMaxRetries, AllowedServices: services, AllowedNamespaces: namespaces, AllowedEnvironments: environments}
		promCfg := base
		promCfg.BaseURL, promCfg.TokenFile = cfg.ObservabilityPrometheusURL, cfg.ObservabilityPromTokenFile
		lokiCfg := base
		lokiCfg.BaseURL, lokiCfg.TokenFile, lokiCfg.Tenant = cfg.ObservabilityLokiURL, cfg.ObservabilityLokiTokenFile, cfg.ObservabilityLokiTenant
		tempoCfg := base
		tempoCfg.BaseURL, tempoCfg.TokenFile = cfg.ObservabilityTempoURL, cfg.ObservabilityTempoTokenFile
		prom, err := observabilityread.NewPrometheus(promCfg)
		if err != nil {
			return nil, err
		}
		loki, err := observabilityread.NewLoki(lokiCfg)
		if err != nil {
			return nil, err
		}
		tempo, err := observabilityread.NewTempo(tempoCfg)
		if err != nil {
			return nil, err
		}
		metricReader, logReader, traceReader = prom, loki, tempo
	}
	service, err := deliveryverification.New(deliveryverification.Config{DeliveryEnabled: cfg.DeliveryTrackingEnabled, VerificationEnabled: cfg.VerificationEnabled, DeliveryWorkerID: cfg.DeliveryWorkerID, VerificationWorkerID: cfg.VerificationWorkerID, PollInterval: cfg.DeliveryPollInterval, DeliveryTimeout: cfg.DeliveryTimeout, VerificationTimeout: cfg.VerificationTimeout, StabilityWindow: cfg.VerificationStabilityWindow, LeaseDuration: cfg.VerificationLeaseDuration, MaxAttempts: cfg.VerificationMaxAttempts, Repository: repository, GitHub: deliveryread.GitHub{Reader: container.ChangeGitHub}, ArgoCD: deliveryread.Argo{Reader: container.ChangeArgoCD}, Rollout: container.DeliveryRollout, Alerts: repository, Metrics: metricReader, Logs: logReader, Traces: traceReader, Profiles: profiles, Mappings: mappings, Observer: container.Metrics})
	if err != nil {
		return nil, err
	}
	container.Handler.SetDeliveryVerification(service)
	return service.NewWorker(), nil
}
