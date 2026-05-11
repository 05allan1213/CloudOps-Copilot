package action

import "time"

type OperationEvent struct {
	Type       string    `json:"type"`
	ActionID   uint64    `json:"action_id"`
	ActionType string    `json:"action_type"`
	Target     string    `json:"target"`
	Status     string    `json:"status"`
	Actor      string    `json:"actor"`
	TraceID    string    `json:"trace_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type OperationEventPublisher interface {
	SendOperationEvent(event OperationEvent) error
}
