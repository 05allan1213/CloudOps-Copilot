package agent

func CanTransitionRun(from, to RunStatus) bool {
	switch from {
	case RunPending:
		return to == RunRunning || to == RunFailed || to == RunCancelled
	case RunRunning:
		return to == RunCompleted || to == RunFailed || to == RunCancelled
	default:
		return false
	}
}

func CanTransitionStep(from, to StepStatus) bool {
	switch from {
	case StepPending:
		return to == StepRunning || to == StepFailed || to == StepCancelled
	case StepRunning:
		return to == StepCompleted || to == StepFailed || to == StepCancelled
	default:
		return false
	}
}

func IsTerminalRun(status RunStatus) bool {
	return status == RunCompleted || status == RunFailed || status == RunCancelled
}

func IsActiveRun(status RunStatus) bool { return status == RunPending || status == RunRunning }
