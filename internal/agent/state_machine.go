package agent

import domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"

func CanTransitionRun(from, to RunStatus) bool { return domain.CanTransitionAgentRun(from, to) }

func CanTransitionStep(from, to StepStatus) bool { return domain.CanTransitionAgentStep(from, to) }

func IsTerminalRun(status RunStatus) bool {
	return status == RunCompleted || status == RunFailed || status == RunCancelled
}

func IsActiveRun(status RunStatus) bool { return status == RunPending || status == RunRunning }
