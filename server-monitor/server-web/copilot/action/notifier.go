package action

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"server-web/model"
	ws "server-web/websocket"
)

type StatusNotifier interface {
	NotifyActionPending(ctx context.Context, action model.PendingAction)
	NotifyActionStatus(ctx context.Context, action model.PendingAction, result string)
}

type WebSocketNotifier struct {
	hub *ws.Hub
}

func NewWebSocketNotifier(hub *ws.Hub) *WebSocketNotifier {
	return &WebSocketNotifier{hub: hub}
}

func (n *WebSocketNotifier) NotifyActionPending(ctx context.Context, action model.PendingAction) {
	n.broadcast(ctx, map[string]interface{}{
		"type": "action_pending",
		"data": map[string]interface{}{
			"action_id":           action.ID,
			"diagnosis_report_id": action.DiagnosisReportID,
			"action_type":         action.ActionType,
			"target":              action.Namespace + "/" + action.TargetName,
			"risk_level":          action.RiskLevel,
			"requested_by":        action.RequestedBy,
		},
	})
}

func (n *WebSocketNotifier) NotifyActionStatus(ctx context.Context, action model.PendingAction, result string) {
	n.broadcast(ctx, map[string]interface{}{
		"type": "action_status",
		"data": map[string]interface{}{
			"action_id":  action.ID,
			"status":     action.Status,
			"result":     result,
			"updated_at": time.Now().UTC(),
		},
	})
}

func (n *WebSocketNotifier) broadcast(ctx context.Context, message interface{}) {
	if n == nil || n.hub == nil {
		return
	}
	data, err := json.Marshal(message)
	if err != nil {
		zap.L().Warn("marshal action websocket message failed", zap.Error(err))
		return
	}
	if err := n.hub.BroadcastBlocking(ctx, data); err != nil {
		zap.L().Warn("broadcast action websocket message failed", zap.Error(err))
	}
}
