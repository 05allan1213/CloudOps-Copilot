package diagnosis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	eventbus "server-monitor/pkg/kafka"
)

type TriggerService interface {
	Trigger(ctx context.Context, user User, req Request) (ReportResponse, error)
}

type WorkerConfig struct {
	Service     TriggerService
	TaskStore   TaskStore
	Notifier    Notifier
	Timeout     time.Duration
	TTL         time.Duration
	Concurrency int
	Now         func() time.Time
	Logger      *zap.Logger
}

type Worker struct {
	service     TriggerService
	taskStore   TaskStore
	notifier    Notifier
	timeout     time.Duration
	ttl         time.Duration
	concurrency int
	now         func() time.Time
	logger      *zap.Logger
	sem         chan struct{}
	wg          sync.WaitGroup
}

func NewWorker(cfg WorkerConfig) *Worker {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = zap.L()
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	return &Worker{
		service:     cfg.Service,
		taskStore:   cfg.TaskStore,
		notifier:    cfg.Notifier,
		timeout:     timeout,
		ttl:         ttl,
		concurrency: concurrency,
		now:         now,
		logger:      logger,
		sem:         make(chan struct{}, concurrency),
	}
}

func (w *Worker) Process(ctx context.Context, event eventbus.AlertEvent) error {
	if w == nil || w.service == nil || w.taskStore == nil {
		return eventbus.Permanent(ErrUnavailable)
	}

	alert := normalizeAlertEvent(event)
	if alert.status != "firing" {
		w.notify(ctx, alert, StatusSkipped, 0, "", "")
		return eventbus.Skipped(nil)
	}
	if alert.dedupeKey == "" {
		err := fmt.Errorf("%w: alert fingerprint or alertname with instance is required", ErrInvalidRequest)
		w.notify(ctx, alert, StatusSkipped, 0, "", err.Error())
		return eventbus.Skipped(err)
	}

	started, err := w.taskStore.TryStart(ctx, alert.dedupeKey, w.ttl)
	if err != nil {
		return err
	}
	if !started {
		w.notify(ctx, alert, StatusSkipped, 0, "", "")
		return eventbus.Skipped(nil)
	}

	w.notify(ctx, alert, StatusPending, 0, "", "")
	if err := w.taskStore.MarkRunning(ctx, alert.dedupeKey, 0, w.ttl); err != nil {
		return err
	}
	w.notify(ctx, alert, StatusRunning, 0, "", "")

	select {
	case w.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	w.wg.Add(1)
	go func() {
		defer func() { <-w.sem }()
		defer w.wg.Done()
		w.processAlert(ctx, alert)
	}()
	return nil
}

func (w *Worker) processAlert(ctx context.Context, alert workerAlert) {
	taskCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	req := Request{
		Fingerprint: alert.fingerprint,
		AlertName:   alert.alertName,
		Instance:    alert.instance,
		TriggerType: TriggerAuto,
	}
	report, err := w.service.Trigger(taskCtx, systemDiagnosisUser(), req)
	if err != nil {
		errText := publicError(err)
		if markErr := w.taskStore.MarkFailed(ctx, alert.dedupeKey, errText, w.ttl); markErr != nil {
			w.logger.Error("mark diagnosis task failed", zap.Error(markErr))
			return
		}
		w.notify(ctx, alert, StatusFailed, report.ID, "", errText)
		return
	}

	if err := w.taskStore.MarkCompleted(ctx, alert.dedupeKey, report.ID, w.ttl); err != nil {
		w.logger.Error("mark diagnosis task completed", zap.Error(err))
		return
	}
	w.notify(ctx, alert, StatusCompleted, report.ID, report.Summary, "")
	w.logger.Info("diagnosis worker processed alert event",
		zap.String("fingerprint", alert.fingerprint),
		zap.String("dedupe_key", alert.dedupeKey),
		zap.String("alert_name", alert.alertName),
		zap.String("instance", alert.instance),
		zap.Uint64("report_id", report.ID),
	)
}

func (w *Worker) Wait() {
	w.wg.Wait()
}

func (w *Worker) notify(ctx context.Context, alert workerAlert, status string, reportID uint64, summary string, errText string) {
	if w == nil || w.notifier == nil {
		return
	}
	if err := w.notifier.NotifyDiagnosis(ctx, DiagnosisUpdate{
		Fingerprint: alert.displayFingerprint(),
		AlertName:   alert.alertName,
		Instance:    alert.instance,
		Status:      status,
		TriggerType: TriggerAuto,
		ReportID:    reportID,
		Summary:     strings.TrimSpace(summary),
		Error:       strings.TrimSpace(errText),
		UpdatedAt:   w.now().UTC(),
	}); err != nil {
		w.logger.Warn("diagnosis update push failed",
			zap.String("fingerprint", alert.displayFingerprint()),
			zap.String("status", status),
			zap.Error(err),
		)
	}
}

func systemDiagnosisUser() User {
	return User{ID: 0, Username: "system", Role: "admin"}
}

func isPermanentWorkerError(err error) bool {
	return errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrForbidden)
}

type workerAlert struct {
	fingerprint string
	dedupeKey   string
	status      string
	alertName   string
	instance    string
}

func (a workerAlert) displayFingerprint() string {
	if a.fingerprint != "" {
		return a.fingerprint
	}
	return a.dedupeKey
}

func normalizeAlertEvent(event eventbus.AlertEvent) workerAlert {
	alertName := strings.TrimSpace(event.Labels["alertname"])
	instance := strings.TrimSpace(event.Labels["instance"])
	fingerprint := strings.TrimSpace(event.Fingerprint)
	dedupeKey := fingerprint
	if dedupeKey == "" && alertName != "" && instance != "" && !event.StartsAt.IsZero() {
		dedupeKey = stableAlertKey(alertName, instance, event.StartsAt)
	}
	return workerAlert{
		fingerprint: fingerprint,
		dedupeKey:   dedupeKey,
		status:      strings.TrimSpace(event.Status),
		alertName:   alertName,
		instance:    instance,
	}
}

func stableAlertKey(alertName, instance string, startsAt time.Time) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(alertName),
		strings.TrimSpace(instance),
		startsAt.UTC().Format(time.RFC3339Nano),
	}, "|")))
	return "generated:" + hex.EncodeToString(sum[:8])
}
