package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"server-monitor/pkg/logger"

	eventbus "server-web/kafka"
	"server-web/model"
	rediscache "server-web/redis"
	"server-web/webhook"
)

const DefaultEventsLimit int64 = 8

var (
	validEventStatuses = map[string]struct{}{
		"firing":   {},
		"resolved": {},
	}

	validEventSeverities = map[string]struct{}{
		"critical": {},
		"warning":  {},
		"info":     {},
	}

	validActiveSeverities = map[string]struct{}{
		"critical": {},
		"warning":  {},
		"info":     {},
	}
)

type CacheClient interface {
	Enabled() bool
	HSet(ctx context.Context, key, field string, value []byte) error
	HDel(ctx context.Context, key, field string) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	AddAlertEventOnce(ctx context.Context, streamKey, dedupeKey string, maxLen int64, value, dedupeValue []byte, ttl time.Duration) (bool, error)
	XRevRangeN(ctx context.Context, key string, count int64) ([]string, error)
	Publish(ctx context.Context, channel string, message []byte) error
}

type Producer interface {
	SendAlertEvent(eventbus.AlertEvent) error
}

type Service struct {
	cache     CacheClient
	db        *gorm.DB
	producer  Producer
	dedupeTTL time.Duration
}

type Options struct {
	DedupeTTL time.Duration
	DB        *gorm.DB
	Producer  Producer
}

type ErrorKind string

const (
	ErrorUnsupportedStatus ErrorKind = "unsupported_status"
	ErrorMarshalAlert      ErrorKind = "marshal_alert"
	ErrorStoreActiveAlert  ErrorKind = "store_active_alert"
	ErrorDeleteActiveAlert ErrorKind = "delete_active_alert"
	ErrorMarshalAlertEvent ErrorKind = "marshal_alert_event"
	ErrorStoreAlertEvent   ErrorKind = "store_alert_event"
	ErrorLoadActiveAlerts  ErrorKind = "load_active_alerts"
	ErrorLoadAlertEvents   ErrorKind = "load_alert_events"
)

type ServiceError struct {
	Kind   ErrorKind
	Status string
	Err    error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Status != "" {
		return e.Status
	}
	return string(e.Kind)
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewService(cache CacheClient, options Options) *Service {
	return &Service{
		cache:     cache,
		db:        options.DB,
		producer:  options.Producer,
		dedupeTTL: options.DedupeTTL,
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.cache != nil && s.cache.Enabled()
}

func (s *Service) HandleWebhook(ctx context.Context, payload webhook.AlertmanagerWebhookRequest, receivedAt time.Time) error {
	for _, alert := range payload.Alerts {
		if alert.Fingerprint == "" {
			continue
		}
		if !IsStatusSupported(alert.Status) {
			return &ServiceError{Kind: ErrorUnsupportedStatus, Status: alert.Status}
		}
	}

	for _, alert := range payload.Alerts {
		if alert.Fingerprint == "" {
			continue
		}

		message, err := json.Marshal(alert)
		if err != nil {
			return &ServiceError{Kind: ErrorMarshalAlert, Err: err}
		}

		switch alert.Status {
		case "firing":
			if err := s.cache.HSet(ctx, rediscache.ActiveAlertsKey, alert.Fingerprint, message); err != nil {
				return &ServiceError{Kind: ErrorStoreActiveAlert, Err: err}
			}
		case "resolved":
			if err := s.cache.HDel(ctx, rediscache.ActiveAlertsKey, alert.Fingerprint); err != nil {
				return &ServiceError{Kind: ErrorDeleteActiveAlert, Err: err}
			}
		}

		event, err := json.Marshal(webhook.NewAlertEvent(alert, receivedAt))
		if err != nil {
			return &ServiceError{Kind: ErrorMarshalAlertEvent, Err: err}
		}

		stored, err := s.cache.AddAlertEventOnce(
			ctx,
			rediscache.AlertEventsKey,
			EventDedupeKey(alert),
			rediscache.AlertEventsMax,
			event,
			[]byte(receivedAt.Format(time.RFC3339Nano)),
			s.dedupeTTL,
		)
		if err != nil {
			return &ServiceError{Kind: ErrorStoreAlertEvent, Err: err}
		}
		if !stored {
			continue
		}

		s.archiveHistory(ctx, alert)

		if err := s.cache.Publish(ctx, rediscache.AlertChannel, event); err != nil {
			zap.L().Warn("publish alert event failed",
				zap.String("fingerprint", alert.Fingerprint),
				zap.String("status", alert.Status),
				zap.Error(err),
			)
		}
		s.sendEventToKafka(ctx, alert, receivedAt)
	}

	return nil
}

func (s *Service) ActiveAlerts(ctx context.Context, severityFilter string) ([]webhook.AlertRecord, error) {
	values, err := s.cache.HGetAll(ctx, rediscache.ActiveAlertsKey)
	if err != nil {
		return nil, &ServiceError{Kind: ErrorLoadActiveAlerts, Err: err}
	}

	alerts := DecodeActiveAlerts(values)
	alerts = FilterActiveAlerts(alerts, severityFilter)

	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].StartsAt.After(alerts[j].StartsAt)
	})

	return alerts, nil
}

func (s *Service) AlertEvents(ctx context.Context, limit int64, statusFilter, severityFilter string) ([]webhook.AlertEvent, error) {
	values, err := s.cache.XRevRangeN(ctx, rediscache.AlertEventsKey, limit)
	if err != nil {
		return nil, &ServiceError{Kind: ErrorLoadAlertEvents, Err: err}
	}

	events := DecodeAlertEvents(values)
	events = FilterAlertEvents(events, statusFilter, severityFilter)
	return events, nil
}

func (s *Service) archiveHistory(ctx context.Context, alert webhook.AlertRecord) {
	if s.db == nil {
		return
	}

	history, err := BuildHistory(alert)
	if err != nil {
		logger.FromContext(ctx).Warn("build alert history failed",
			zap.String("fingerprint", alert.Fingerprint),
			zap.String("status", alert.Status),
			zap.Error(err),
		)
		return
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.AlertHistory
		findErr := tx.
			Where("fingerprint = ? AND fired_at = ?", history.Fingerprint, history.FiredAt).
			Order("id DESC").
			First(&existing).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return tx.Create(&history).Error
		}
		if findErr != nil {
			return findErr
		}

		updates := map[string]interface{}{
			"alert_name":  history.AlertName,
			"instance":    history.Instance,
			"severity":    history.Severity,
			"summary":     history.Summary,
			"labels_json": history.LabelsJSON,
		}
		if history.Status == "resolved" {
			updates["status"] = "resolved"
			updates["resolved_at"] = history.ResolvedAt
		}
		return tx.Model(&model.AlertHistory{}).Where("id = ?", existing.ID).Updates(updates).Error
	})
	if err != nil {
		logger.FromContext(ctx).Warn("archive alert history failed",
			zap.String("fingerprint", alert.Fingerprint),
			zap.String("status", alert.Status),
			zap.Error(err),
		)
	}
}

func (s *Service) sendEventToKafka(ctx context.Context, alert webhook.AlertRecord, receivedAt time.Time) {
	if s.producer == nil {
		return
	}

	event := eventbus.AlertEvent{
		Type:         "alert",
		Fingerprint:  alert.Fingerprint,
		Status:       alert.Status,
		Labels:       alert.Labels,
		Annotations:  alert.Annotations,
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		GeneratorURL: alert.GeneratorURL,
		ReceivedAt:   receivedAt,
	}
	if err := s.producer.SendAlertEvent(event); err != nil {
		logger.FromContext(ctx).Warn("kafka produce alert event failed",
			zap.String("fingerprint", alert.Fingerprint),
			zap.String("status", alert.Status),
			zap.Error(err),
		)
	}
}

func BuildHistory(alert webhook.AlertRecord) (model.AlertHistory, error) {
	labelsJSON, err := marshalStringMap(alert.Labels)
	if err != nil {
		return model.AlertHistory{}, err
	}

	history := model.AlertHistory{
		Fingerprint: alert.Fingerprint,
		AlertName:   strings.TrimSpace(alert.Labels["alertname"]),
		Instance:    strings.TrimSpace(alert.Labels["instance"]),
		Severity:    strings.TrimSpace(alert.Labels["severity"]),
		Status:      alert.Status,
		Summary:     strings.TrimSpace(alert.Annotations["summary"]),
		LabelsJSON:  labelsJSON,
		FiredAt:     alert.StartsAt.UTC(),
	}
	if history.Severity == "" {
		history.Severity = "warning"
	}
	if alert.Status == "resolved" {
		resolvedAt := alert.EndsAt.UTC()
		history.ResolvedAt = &resolvedAt
	}
	return history, nil
}

func DecodeActiveAlerts(values map[string]string) []webhook.AlertRecord {
	alerts := make([]webhook.AlertRecord, 0, len(values))
	for _, value := range values {
		var alert webhook.AlertRecord
		if err := json.Unmarshal([]byte(value), &alert); err != nil {
			zap.L().Warn("skip corrupted alert data", zap.Error(err))
			continue
		}
		alerts = append(alerts, alert)
	}

	return alerts
}

func DecodeAlertEvents(values []string) []webhook.AlertEvent {
	events := make([]webhook.AlertEvent, 0, len(values))
	for _, value := range values {
		var event webhook.AlertEvent
		if err := json.Unmarshal([]byte(value), &event); err != nil {
			zap.L().Warn("skip corrupted alert event", zap.Error(err))
			continue
		}
		events = append(events, event)
	}

	return events
}

func ParseEventsLimit(raw string) int64 {
	if raw == "" {
		return DefaultEventsLimit
	}

	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return DefaultEventsLimit
	}
	if parsed > rediscache.AlertEventsMax {
		return rediscache.AlertEventsMax
	}

	return parsed
}

func IsStatusSupported(status string) bool {
	_, ok := validEventStatuses[status]
	return ok
}

func EventDedupeKey(alert webhook.AlertRecord) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s",
		rediscache.AlertEventDedupeKey,
		alert.Fingerprint,
		alert.Status,
		alert.StartsAt.UTC().Format(time.RFC3339Nano),
		alert.EndsAt.UTC().Format(time.RFC3339Nano),
	)
}

func ParseEventFilter(raw string) string {
	if _, ok := validEventStatuses[raw]; !ok {
		return ""
	}

	return raw
}

func ParseEventSeverityFilter(raw string) string {
	if _, ok := validEventSeverities[raw]; !ok {
		return ""
	}

	return raw
}

func ParseActiveSeverityFilter(raw string) string {
	if _, ok := validActiveSeverities[raw]; !ok {
		return ""
	}

	return raw
}

func FilterAlertEvents(events []webhook.AlertEvent, statusFilter, severityFilter string) []webhook.AlertEvent {
	if statusFilter == "" && severityFilter == "" {
		return events
	}

	filtered := make([]webhook.AlertEvent, 0, len(events))
	for _, event := range events {
		if statusFilter != "" && event.Status != statusFilter {
			continue
		}
		if severityFilter != "" && (event.Labels["severity"] != severityFilter) {
			continue
		}
		filtered = append(filtered, event)
	}

	return filtered
}

func FilterActiveAlerts(alerts []webhook.AlertRecord, severityFilter string) []webhook.AlertRecord {
	if severityFilter == "" {
		return alerts
	}

	filtered := make([]webhook.AlertRecord, 0, len(alerts))
	for _, alert := range alerts {
		if alert.Labels["severity"] != severityFilter {
			continue
		}
		filtered = append(filtered, alert)
	}

	return filtered
}

func marshalStringMap(values map[string]string) (string, error) {
	if values == nil {
		values = map[string]string{}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
