package legacyworker

import (
	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	agentruntime "github.com/05allan1213/CloudOps-Copilot/internal/service/agentruntime"
)

func agentRuntimeConfig(cfg config.Config, container *di.Container) agentruntime.Config {
	return agentruntime.Config{
		Enabled:          cfg.IncidentAgentEnabled,
		WorkerID:         cfg.IncidentAgentWorkerID,
		PollInterval:     cfg.IncidentAgentPollInterval,
		LeaseDuration:    cfg.IncidentAgentLeaseDuration,
		HeartbeatPeriod:  cfg.IncidentAgentHeartbeatPeriod,
		Model:            cfg.LLMModel,
		PromptVersion:    "incident-agent-v3-change-readonly",
		MaxGraphRunSteps: 96,
		Observer:         container.Metrics,
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
