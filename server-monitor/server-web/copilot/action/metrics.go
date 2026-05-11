package action

type Observer interface {
	ObserveActionEvent(operation, result string)
	ObserveActionExecutionDuration(actionType, status string, seconds float64)
}
