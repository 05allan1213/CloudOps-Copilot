package diagnosis

import (
	"fmt"
	"strings"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

func NormalizeRequest(req Request) (Request, error) {
	req.Fingerprint = strings.TrimSpace(req.Fingerprint)
	req.AlertName = strings.TrimSpace(req.AlertName)
	req.Instance = strings.TrimSpace(req.Instance)
	req.TriggerType = strings.TrimSpace(req.TriggerType)
	if req.TriggerType == "" {
		req.TriggerType = TriggerManual
	}
	if !isAllowedTriggerType(req.TriggerType) {
		return Request{}, fmt.Errorf("%w: invalid trigger_type", ErrInvalidRequest)
	}
	if req.Fingerprint == "" && req.AlertHistoryID == 0 && (req.AlertName == "" || req.Instance == "") {
		return Request{}, fmt.Errorf("%w: fingerprint, alert_history_id, or alert_name with instance is required", ErrInvalidRequest)
	}
	return req, nil
}

func NormalizeListFilter(filter ListFilter) (ListFilter, error) {
	if filter.Status != "" && !isAllowedStatus(filter.Status) {
		return ListFilter{}, fmt.Errorf("%w: invalid status", ErrInvalidRequest)
	}
	filter.TriggerType = strings.TrimSpace(filter.TriggerType)
	if filter.TriggerType != "" && !isAllowedTriggerType(filter.TriggerType) {
		return ListFilter{}, fmt.Errorf("%w: invalid trigger_type", ErrInvalidRequest)
	}
	if filter.Page <= 0 {
		filter.Page = defaultPage
	}
	if filter.PageSize <= 0 {
		filter.PageSize = defaultPageSize
	}
	if filter.PageSize > maxPageSize {
		filter.PageSize = maxPageSize
	}
	return filter, nil
}

func isAllowedTriggerType(triggerType string) bool {
	switch triggerType {
	case TriggerManual, TriggerChat, TriggerAuto:
		return true
	default:
		return false
	}
}

func isAllowedStatus(status string) bool {
	switch status {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

func ConfidenceLevel(confidence float64) string {
	switch {
	case confidence >= 0.8:
		return ConfidenceHigh
	case confidence >= 0.5:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}
