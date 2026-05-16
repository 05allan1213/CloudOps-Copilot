package diagnosis

import (
	"context"
	"encoding/json"
	"time"
)

const (
	StatusSkipped = "skipped"

	DiagnosisUpdateMessageType = "diagnosis_update"
)

type Notifier interface {
	NotifyDiagnosis(ctx context.Context, update DiagnosisUpdate) error
}

type DiagnosisUpdate struct {
	Fingerprint string    `json:"fingerprint"`
	AlertName   string    `json:"alert_name"`
	Instance    string    `json:"instance"`
	Status      string    `json:"status"`
	TriggerType string    `json:"trigger_type"`
	ReportID    uint64    `json:"report_id,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Error       string    `json:"error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type broadcastHub interface {
	BroadcastBlocking(ctx context.Context, message []byte) error
}

type WebSocketNotifier struct {
	hub broadcastHub
}

func NewWebSocketNotifier(hub broadcastHub) *WebSocketNotifier {
	return &WebSocketNotifier{hub: hub}
}

func (n *WebSocketNotifier) NotifyDiagnosis(ctx context.Context, update DiagnosisUpdate) error {
	if n == nil || n.hub == nil {
		return nil
	}
	payload, err := json.Marshal(struct {
		Type string          `json:"type"`
		Data DiagnosisUpdate `json:"data"`
	}{
		Type: DiagnosisUpdateMessageType,
		Data: update,
	})
	if err != nil {
		return err
	}
	return n.hub.BroadcastBlocking(ctx, payload)
}
