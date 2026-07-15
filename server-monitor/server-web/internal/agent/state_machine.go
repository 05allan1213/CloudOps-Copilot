package agent

import domain "server-web/internal/incident"

func CanTransitionRun(from, to RunStatus) bool { return domain.CanTransitionAgentRun(from, to) }

func CanTransitionStep(from, to StepStatus) bool { return domain.CanTransitionAgentStep(from, to) }

func IsTerminalRun(status RunStatus) bool {
	return status == RunCompleted || status == RunFailed || status == RunCancelled
}

func IsActiveRun(status RunStatus) bool { return status == RunPending || status == RunRunning }
