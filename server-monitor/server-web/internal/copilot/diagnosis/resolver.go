package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"server-web/internal/infra/webhook"
	"server-web/internal/model"
)

type ActiveAlertProvider interface {
	Enabled() bool
	ActiveAlerts(ctx context.Context, severityFilter string) ([]webhook.AlertRecord, error)
}

type Resolver struct {
	db      *gorm.DB
	alerts  ActiveAlertProvider
	timeout time.Duration
	now     func() time.Time
}

type ResolverOptions struct {
	DB           *gorm.DB
	AlertService ActiveAlertProvider
	Timeout      time.Duration
	Now          func() time.Time
}

func NewResolver(options ResolverOptions) *Resolver {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Resolver{
		db:      options.DB,
		alerts:  options.AlertService,
		timeout: timeout,
		now:     now,
	}
}

func (r *Resolver) Resolve(ctx context.Context, req Request) (AlertContext, error) {
	req, err := NormalizeRequest(req)
	if err != nil {
		return AlertContext{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if req.AlertHistoryID != 0 {
		return r.resolveByHistoryID(ctx, req.AlertHistoryID)
	}
	if req.Fingerprint != "" {
		if active, ok, err := r.findActiveByFingerprint(ctx, req.Fingerprint); err != nil {
			return AlertContext{}, err
		} else if ok {
			return active, nil
		}
		return r.resolveLatestHistory(ctx, "fingerprint = ?", req.Fingerprint)
	}
	return r.resolveByNameAndInstance(ctx, req.AlertName, req.Instance)
}

func (r *Resolver) resolveByHistoryID(ctx context.Context, id uint64) (AlertContext, error) {
	if r.db == nil {
		return AlertContext{}, ErrUnavailable
	}
	var history model.AlertHistory
	err := r.db.WithContext(ctx).First(&history, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AlertContext{}, ErrNotFound
	}
	if err != nil {
		return AlertContext{}, err
	}
	return contextFromHistory(history, r.now()), nil
}

func (r *Resolver) resolveLatestHistory(ctx context.Context, query string, args ...interface{}) (AlertContext, error) {
	if r.db == nil {
		return AlertContext{}, ErrUnavailable
	}
	var history model.AlertHistory
	err := r.db.WithContext(ctx).
		Where(query, args...).
		Order("fired_at DESC").
		Order("id DESC").
		First(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AlertContext{}, ErrNotFound
	}
	if err != nil {
		return AlertContext{}, err
	}
	return contextFromHistory(history, r.now()), nil
}

func (r *Resolver) resolveByNameAndInstance(ctx context.Context, alertName, instance string) (AlertContext, error) {
	candidates := make([]DiagnosisCandidate, 0, 4)
	if r.alerts != nil && r.alerts.Enabled() {
		alerts, err := r.alerts.ActiveAlerts(ctx, "")
		if err != nil {
			return AlertContext{}, err
		}
		for _, alert := range alerts {
			if alert.Labels["alertname"] == alertName && alert.Labels["instance"] == instance {
				candidates = append(candidates, candidateFromAlert(alert, "redis:active"))
			}
		}
	}

	if r.db != nil {
		var histories []model.AlertHistory
		err := r.db.WithContext(ctx).
			Where("alert_name = ? AND instance = ?", alertName, instance).
			Order("fired_at DESC").
			Order("id DESC").
			Limit(5).
			Find(&histories).Error
		if err != nil {
			return AlertContext{}, err
		}
		for _, history := range histories {
			candidates = append(candidates, candidateFromHistory(history))
		}
	}

	if len(candidates) == 0 {
		return AlertContext{}, ErrNotFound
	}
	if len(candidates) > 1 {
		return AlertContext{}, ConflictError{Candidates: candidates}
	}
	if candidates[0].Source == "redis:active" {
		active, ok, err := r.findActiveByFingerprint(ctx, candidates[0].Fingerprint)
		if err != nil {
			return AlertContext{}, err
		}
		if ok {
			return active, nil
		}
	}
	return r.resolveByHistoryID(ctx, candidates[0].AlertHistoryID)
}

func (r *Resolver) findActiveByFingerprint(ctx context.Context, fingerprint string) (AlertContext, bool, error) {
	if r.alerts == nil || !r.alerts.Enabled() {
		return AlertContext{}, false, nil
	}
	alerts, err := r.alerts.ActiveAlerts(ctx, "")
	if err != nil {
		return AlertContext{}, false, err
	}
	for _, alert := range alerts {
		if alert.Fingerprint == fingerprint {
			return contextFromAlert(alert, r.now()), true, nil
		}
	}
	return AlertContext{}, false, nil
}

func contextFromAlert(alert webhook.AlertRecord, collectedAt time.Time) AlertContext {
	labels := cloneLabels(alert.Labels)
	annotations := cloneLabels(alert.Annotations)
	instance := labels["instance"]
	namespace := labels["namespace"]
	alertName := labels["alertname"]
	severity := labels["severity"]
	if severity == "" {
		severity = "warning"
	}
	var endsAt *time.Time
	if !alert.EndsAt.IsZero() {
		value := alert.EndsAt.UTC()
		endsAt = &value
	}
	targetKind := labels["target_kind"]
	targetName := labels["target_name"]
	if targetKind == "" {
		targetKind = "host"
		targetName = instance
	}
	if targetName == "" {
		targetName = instance
	}
	return AlertContext{
		Fingerprint: alert.Fingerprint,
		AlertName:   alertName,
		Instance:    instance,
		TargetKind:  targetKind,
		TargetName:  targetName,
		Namespace:   namespace,
		Severity:    severity,
		Status:      alert.Status,
		Summary:     firstNonEmpty(annotations["summary"], annotations["description"]),
		Labels:      labels,
		Annotations: annotations,
		StartsAt:    alert.StartsAt.UTC(),
		EndsAt:      endsAt,
		Source:      "redis:active",
		CollectedAt: collectedAt.UTC(),
	}
}

func contextFromHistory(history model.AlertHistory, collectedAt time.Time) AlertContext {
	labels := map[string]string{}
	if strings.TrimSpace(history.LabelsJSON) != "" {
		_ = json.Unmarshal([]byte(history.LabelsJSON), &labels)
	}
	annotations := map[string]string{}
	if history.AnnotationsJSON != nil && strings.TrimSpace(*history.AnnotationsJSON) != "" {
		_ = json.Unmarshal([]byte(*history.AnnotationsJSON), &annotations)
		annotations = cloneLabels(annotations)
	}
	if labels["alertname"] == "" {
		labels["alertname"] = history.AlertName
	}
	if labels["instance"] == "" {
		labels["instance"] = history.Instance
	}
	if labels["severity"] == "" {
		labels["severity"] = history.Severity
	}
	if annotations["summary"] == "" && history.Summary != "" {
		annotations["summary"] = history.Summary
	}
	var endsAt *time.Time
	if history.ResolvedAt != nil {
		value := history.ResolvedAt.UTC()
		endsAt = &value
	}
	targetKind := labels["target_kind"]
	targetName := labels["target_name"]
	if targetKind == "" {
		targetKind = "host"
		targetName = history.Instance
	}
	if targetName == "" {
		targetName = history.Instance
	}
	return AlertContext{
		AlertHistoryID: history.ID,
		Fingerprint:    history.Fingerprint,
		AlertName:      history.AlertName,
		Instance:       history.Instance,
		TargetKind:     targetKind,
		TargetName:     targetName,
		Namespace:      labels["namespace"],
		Severity:       history.Severity,
		Status:         history.Status,
		Summary:        history.Summary,
		Labels:         labels,
		Annotations:    annotations,
		StartsAt:       history.FiredAt.UTC(),
		EndsAt:         endsAt,
		Source:         "mysql:alert_histories",
		CollectedAt:    collectedAt.UTC(),
	}
}

func candidateFromAlert(alert webhook.AlertRecord, source string) DiagnosisCandidate {
	return DiagnosisCandidate{
		Fingerprint: alert.Fingerprint,
		AlertName:   alert.Labels["alertname"],
		Instance:    alert.Labels["instance"],
		Severity:    alert.Labels["severity"],
		Status:      alert.Status,
		FiredAt:     alert.StartsAt.UTC(),
		Source:      source,
	}
}

func candidateFromHistory(history model.AlertHistory) DiagnosisCandidate {
	return DiagnosisCandidate{
		AlertHistoryID: history.ID,
		Fingerprint:    history.Fingerprint,
		AlertName:      history.AlertName,
		Instance:       history.Instance,
		Severity:       history.Severity,
		Status:         history.Status,
		FiredAt:        history.FiredAt.UTC(),
		Source:         "mysql:alert_histories",
	}
}

func cloneLabels(labels map[string]string) map[string]string {
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		if isSensitiveField(key) {
			continue
		}
		cloned[key] = value
	}
	return cloned
}

func isSensitiveField(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "password") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "api_key")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func wrapNotFound(source string) error {
	return fmt.Errorf("%w: %s", ErrNotFound, source)
}
