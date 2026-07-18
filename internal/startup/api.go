package startup

import (
	"fmt"

	"k8s.io/client-go/kubernetes"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	agentapplication "github.com/05allan1213/CloudOps-Copilot/internal/agent/application"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	remediationapplication "github.com/05allan1213/CloudOps-Copilot/internal/remediation/application"
	"github.com/05allan1213/CloudOps-Copilot/internal/service/changeintelligence"
	verificationapplication "github.com/05allan1213/CloudOps-Copilot/internal/verification/application"
)

// InitAPIApplications wires the existing V2 Query and Command contracts
// without constructing any Agent, remediation, delivery, or verification loop.
func InitAPIApplications(cfg config.Config, container *di.Container, k8sReader k8sread.Reader, k8sClient kubernetes.Interface) error {
	container.Handler.SetIncidentK8sReader(k8sReader)

	if cfg.ChangeIntelligenceEnabled {
		if container.DB == nil {
			return fmt.Errorf("change intelligence API requires MySQL")
		}
		store, err := incidentmysql.NewStore(container.DB)
		if err != nil {
			return err
		}
		repository, err := incidentmysql.NewChangeRepository(container.DB)
		if err != nil {
			return err
		}
		service, err := changeintelligence.New(changeintelligence.Config{
			Enabled:       true,
			Lookback:      cfg.ChangeLookback,
			MaxCandidates: cfg.ChangeMaxCandidates,
			Incidents:     store,
			Changes:       repository,
			Observer:      container.Metrics,
		})
		if err != nil {
			return err
		}
		container.Handler.SetChangeIntelligence(service)
	}

	if cfg.IncidentAgentEnabled {
		if container.DB == nil {
			return fmt.Errorf("incident agent API requires MySQL")
		}
		store, err := incidentmysql.NewStore(container.DB)
		if err != nil {
			return err
		}
		service, err := agentapplication.New(store, agentApplicationConfig(cfg, container))
		if err != nil {
			return err
		}
		container.Handler.SetAgentRuntime(service)
	}

	if cfg.RemediationEnabled {
		if container.DB == nil {
			return fmt.Errorf("remediation API requires MySQL")
		}
		repository, err := incidentmysql.NewRemediationRepository(container.DB)
		if err != nil {
			return err
		}
		service, err := remediationapplication.New(remediationapplication.Config{Enabled: true, Repository: repository})
		if err != nil {
			return err
		}
		container.Handler.SetRemediation(service)
	}

	if cfg.DeliveryTrackingEnabled || cfg.VerificationEnabled {
		if container.DB == nil {
			return fmt.Errorf("delivery and verification API requires MySQL")
		}
		repository, err := incidentmysql.NewVerificationRepository(container.DB)
		if err != nil {
			return err
		}
		service, err := verificationapplication.New(verificationapplication.Config{
			DeliveryEnabled:     cfg.DeliveryTrackingEnabled,
			VerificationEnabled: cfg.VerificationEnabled,
			Repository:          repository,
		})
		if err != nil {
			return err
		}
		container.Handler.SetDeliveryVerification(service)
	}

	if cfg.FastDemoEnabled {
		if _, err := InitFastDemo(cfg, container, k8sClient); err != nil {
			return err
		}
	}
	return nil
}

func agentApplicationConfig(cfg config.Config, container *di.Container) agentapplication.Config {
	return agentapplication.Config{
		Enabled:       cfg.IncidentAgentEnabled,
		Model:         cfg.LLMModel,
		PromptVersion: "incident-agent-v3-change-readonly",
		Observer:      container.Metrics,
		Limits: agent.Limits{
			MaxSteps:          cfg.IncidentAgentMaxSteps,
			MaxToolCalls:      cfg.IncidentAgentMaxToolCalls,
			MaxModelCalls:     cfg.IncidentAgentMaxModelCalls,
			TokenBudget:       cfg.IncidentAgentTokenBudget,
			MaxEvidenceItems:  cfg.IncidentAgentMaxEvidenceItems,
			MaxRuntime:        cfg.IncidentAgentMaxRuntime,
			ToolTimeout:       cfg.IncidentAgentToolTimeout,
			MaxEvidenceBytes:  cfg.IncidentAgentMaxEvidenceBytes,
			MaxCheckpointSize: cfg.IncidentAgentMaxCheckpointBytes,
			MaxStepRetries:    cfg.IncidentAgentMaxStepRetries,
		},
	}
}
