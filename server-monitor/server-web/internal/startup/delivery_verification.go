package startup

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"server-web/internal/config"
	"server-web/internal/di"
	"server-web/internal/infra/deliveryread"
	"server-web/internal/infra/incidentmysql"
	"server-web/internal/service/changeintelligence"
	"server-web/internal/service/deliveryverification"
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
	service, err := deliveryverification.New(deliveryverification.Config{DeliveryEnabled: cfg.DeliveryTrackingEnabled, VerificationEnabled: cfg.VerificationEnabled, DeliveryWorkerID: cfg.DeliveryWorkerID, VerificationWorkerID: cfg.VerificationWorkerID, PollInterval: cfg.DeliveryPollInterval, DeliveryTimeout: cfg.DeliveryTimeout, VerificationTimeout: cfg.VerificationTimeout, StabilityWindow: cfg.VerificationStabilityWindow, LeaseDuration: cfg.VerificationLeaseDuration, MaxAttempts: cfg.VerificationMaxAttempts, Repository: repository, GitHub: deliveryread.GitHub{Reader: container.ChangeGitHub}, ArgoCD: deliveryread.Argo{Reader: container.ChangeArgoCD}, Rollout: container.DeliveryRollout, Alerts: repository, Mappings: mappings, Observer: container.Metrics})
	if err != nil {
		return nil, err
	}
	container.Handler.SetDeliveryVerification(service)
	return service.NewWorker(), nil
}
