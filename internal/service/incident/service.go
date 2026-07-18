package incident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/webhook"
)

const defaultQueryLimit = 100

// Observer receives low-cardinality Incident ingestion measurements.
type Observer interface {
	ObserveSignal(source, status, result string)
	ObserveIncident(operation string)
	ObserveTransition(from, to, result string)
	ObserveIngestionError(reason string)
	ObserveIngestionDuration(result string, seconds float64)
	ObserveOutboxPending(count int64)
}

// Service owns deterministic Incident ingestion and query use cases.
type Service struct {
	uow               domain.UnitOfWork
	aggregationWindow time.Duration
	observer          Observer
	now               func() time.Time
	newID             func() string
}

// Config configures the Incident application service.
type Config struct {
	UnitOfWork        domain.UnitOfWork
	AggregationWindow time.Duration
	Observer          Observer
	Now               func() time.Time
	NewID             func() string
}

// IngestResult describes one normalized alert outcome.
type IngestResult struct {
	SourceEventID      string `json:"source_event_id"`
	CorrelationKey     string `json:"correlation_key"`
	IncidentPublicID   string `json:"incident_id,omitempty"`
	Duplicate          bool   `json:"duplicate"`
	NoMatchingIncident bool   `json:"no_matching_incident"`
}

// NewService creates an Incident application service.
func NewService(cfg Config) (*Service, error) {
	if cfg.UnitOfWork == nil {
		return nil, fmt.Errorf("%w: unit of work is required", domain.ErrInvalidArgument)
	}
	if cfg.AggregationWindow <= 0 {
		return nil, fmt.Errorf("%w: aggregation window must be positive", domain.ErrInvalidArgument)
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	newID := cfg.NewID
	if newID == nil {
		newID = func() string { return uuid.NewString() }
	}
	return &Service{uow: cfg.UnitOfWork, aggregationWindow: cfg.AggregationWindow, observer: cfg.Observer, now: now, newID: newID}, nil
}

// IngestAlertmanager normalizes and atomically persists all alerts in one webhook.
func (s *Service) IngestAlertmanager(ctx context.Context, payload webhook.AlertmanagerWebhookRequest) (results []IngestResult, err error) {
	started := time.Now()
	ctx, span := otel.Tracer("server-web/incident").Start(ctx, "incident.ingest.alertmanager")
	defer span.End()
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		}
		if s.observer != nil {
			s.observer.ObserveIngestionDuration(result, time.Since(started).Seconds())
		}
	}()

	receivedAt := s.now().UTC()
	_, normalizeSpan := otel.Tracer("server-web/incident").Start(ctx, "incident.signal.normalize")
	signals, normalizeErr := NormalizeAlertmanager(payload, receivedAt)
	normalizeSpan.End()
	if normalizeErr != nil {
		s.observeError("invalid_signal")
		return nil, normalizeErr
	}

	results = make([]IngestResult, 0, len(signals))
	transactionCtx, transactionSpan := otel.Tracer("server-web/incident").Start(ctx, "incident.database.transaction")
	err = s.uow.WithinTransaction(transactionCtx, func(repos domain.Repositories) error {
		for index := range signals {
			result, ingestErr := s.ingestSignal(transactionCtx, repos, &signals[index])
			if ingestErr != nil {
				return ingestErr
			}
			results = append(results, result)
		}
		return nil
	})
	transactionSpan.End()
	if err != nil {
		s.observeError("transaction")
		return nil, err
	}
	if s.observer != nil {
		if count, countErr := s.uow.ReadRepositories().Outbox.PendingCount(ctx); countErr == nil {
			s.observer.ObserveOutboxPending(count)
		}
	}
	return results, nil
}

func (s *Service) ingestSignal(ctx context.Context, repos domain.Repositories, normalized *NormalizedSignal) (IngestResult, error) {
	signal := &normalized.Signal
	result := IngestResult{SourceEventID: signal.SourceEventID, CorrelationKey: normalized.CorrelationKey}

	ctx, span := otel.Tracer("server-web/incident").Start(ctx, "incident.aggregate")
	span.SetAttributes(
		attribute.String("signal.source", signal.Source),
		attribute.String("signal.source_event_id", signal.SourceEventID),
		attribute.String("incident.correlation_key", normalized.CorrelationKey),
	)
	defer span.End()

	if err := repos.Correlations.Lock(ctx, normalized.CorrelationKey, signal.ReceivedAt); err != nil {
		return result, err
	}
	created, err := repos.Signals.CreateIfAbsent(ctx, signal)
	if err != nil {
		return result, err
	}
	if !created {
		result.Duplicate = true
		s.observeSignal(signal, "duplicate")
		return result, nil
	}

	if signal.Status == domain.SignalStatusResolved {
		return s.resolveSignal(ctx, repos, normalized, result)
	}
	return s.fireSignal(ctx, repos, normalized, result)
}

func (s *Service) fireSignal(ctx context.Context, repos domain.Repositories, normalized *NormalizedSignal, result IngestResult) (IngestResult, error) {
	signal := &normalized.Signal
	since := signal.OccurredAt.Add(-s.aggregationWindow)
	item, err := repos.Incidents.FindOpenByCorrelationKey(ctx, normalized.CorrelationKey, since)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return result, err
	}

	if errors.Is(err, domain.ErrNotFound) {
		resolved, findErr := repos.Incidents.FindRecentResolvedByCorrelationKey(ctx, normalized.CorrelationKey, since)
		if findErr == nil {
			return s.reopenIncident(ctx, repos, resolved, normalized, result)
		}
		if !errors.Is(findErr, domain.ErrNotFound) {
			return result, findErr
		}
		return s.createIncident(ctx, repos, normalized, result)
	}
	return s.updateIncident(ctx, repos, item, normalized, result)
}

func (s *Service) createIncident(ctx context.Context, repos domain.Repositories, normalized *NormalizedSignal, result IngestResult) (IngestResult, error) {
	signal := &normalized.Signal
	item := &domain.Incident{
		PublicID: s.newID(), Fingerprint: signal.Fingerprint, CorrelationKey: normalized.CorrelationKey,
		Cluster: signal.Cluster, Namespace: signal.Namespace, ServiceName: signal.ServiceName,
		Environment: signal.Environment, TargetKind: signal.TargetKind, TargetName: signal.TargetName,
		Severity: signal.Severity, Status: domain.StatusDetected, Summary: signal.Summary,
		FirstSeenAt: signal.OccurredAt, LastSeenAt: signal.OccurredAt, Version: 1,
		CreatedAt: signal.ReceivedAt, UpdatedAt: signal.ReceivedAt,
	}
	if err := repos.Incidents.Create(ctx, item); err != nil {
		return result, err
	}
	if err := repos.Signals.AttachToIncident(ctx, signal.ID, item.ID); err != nil {
		return result, err
	}
	if err := s.appendTimeline(ctx, repos, item.ID, domain.EventSignalReceived, domain.ActorSource, signal.SourceEventID, signal.Summary, map[string]any{"status": signal.Status, "source": signal.Source}, signal.OccurredAt); err != nil {
		return result, err
	}
	if err := s.appendTimeline(ctx, repos, item.ID, domain.EventIncidentCreated, domain.ActorSystem, "incident-ingestion", item.Summary, map[string]any{"status": item.Status, "correlation_key": item.CorrelationKey}, signal.ReceivedAt); err != nil {
		return result, err
	}
	if err := s.addOutbox(ctx, repos, item, "incident.created", signal.ReceivedAt); err != nil {
		return result, err
	}
	from := item.Status
	eventType, err := item.Transition(domain.StatusCorrelating, signal.ReceivedAt)
	if err != nil {
		return result, err
	}
	if err := repos.Incidents.Update(ctx, item, 1); err != nil {
		return result, err
	}
	if err := s.recordTransition(ctx, repos, item, from, eventType, signal.ReceivedAt); err != nil {
		return result, err
	}
	result.IncidentPublicID = item.PublicID
	s.observeSignal(signal, "created")
	s.observeIncident("created")
	return result, nil
}

func (s *Service) updateIncident(ctx context.Context, repos domain.Repositories, item *domain.Incident, normalized *NormalizedSignal, result IngestResult) (IngestResult, error) {
	signal := &normalized.Signal
	expected := item.Version
	if err := item.ApplySignalTimes(signal.OccurredAt, signal.ReceivedAt, signal.Severity); err != nil {
		return result, err
	}
	item.Fingerprint = signal.Fingerprint
	if signal.Summary != "" {
		item.Summary = signal.Summary
	}
	if err := repos.Incidents.Update(ctx, item, expected); err != nil {
		return result, err
	}
	if err := repos.Signals.AttachToIncident(ctx, signal.ID, item.ID); err != nil {
		return result, err
	}
	if err := s.appendTimeline(ctx, repos, item.ID, domain.EventSignalReceived, domain.ActorSource, signal.SourceEventID, signal.Summary, map[string]any{"status": signal.Status, "source": signal.Source}, signal.OccurredAt); err != nil {
		return result, err
	}
	if err := s.appendTimeline(ctx, repos, item.ID, domain.EventIncidentUpdated, domain.ActorSystem, "incident-ingestion", item.Summary, map[string]any{"version": item.Version, "severity": item.Severity}, signal.ReceivedAt); err != nil {
		return result, err
	}
	if err := s.addOutbox(ctx, repos, item, "incident.updated", signal.ReceivedAt); err != nil {
		return result, err
	}
	result.IncidentPublicID = item.PublicID
	s.observeSignal(signal, "aggregated")
	s.observeIncident("updated")
	return result, nil
}

func (s *Service) reopenIncident(ctx context.Context, repos domain.Repositories, item *domain.Incident, normalized *NormalizedSignal, result IngestResult) (IngestResult, error) {
	signal := &normalized.Signal
	expected := item.Version
	if err := item.ApplySignalTimes(signal.OccurredAt, signal.ReceivedAt, signal.Severity); err != nil {
		return result, err
	}
	item.Fingerprint = signal.Fingerprint
	from := item.Status
	eventType, err := item.Transition(domain.StatusDiagnosing, signal.ReceivedAt)
	if err != nil {
		return result, err
	}
	if err := repos.Incidents.Update(ctx, item, expected); err != nil {
		return result, err
	}
	if err := repos.Signals.AttachToIncident(ctx, signal.ID, item.ID); err != nil {
		return result, err
	}
	if err := s.appendTimeline(ctx, repos, item.ID, domain.EventSignalReceived, domain.ActorSource, signal.SourceEventID, signal.Summary, map[string]any{"status": signal.Status, "source": signal.Source}, signal.OccurredAt); err != nil {
		return result, err
	}
	if err := s.recordTransition(ctx, repos, item, from, eventType, signal.ReceivedAt); err != nil {
		return result, err
	}
	result.IncidentPublicID = item.PublicID
	s.observeSignal(signal, "reopened")
	s.observeIncident("updated")
	return result, nil
}

func (s *Service) resolveSignal(ctx context.Context, repos domain.Repositories, normalized *NormalizedSignal, result IngestResult) (IngestResult, error) {
	signal := &normalized.Signal
	item, err := repos.Incidents.FindOpenByFingerprint(ctx, signal.Fingerprint, time.Time{})
	if errors.Is(err, domain.ErrNotFound) {
		item, err = repos.Incidents.FindOpenByCorrelationKey(ctx, normalized.CorrelationKey, time.Time{})
	}
	if errors.Is(err, domain.ErrNotFound) {
		result.NoMatchingIncident = true
		s.observeSignal(signal, "unmatched_resolved")
		return result, nil
	}
	if err != nil {
		return result, err
	}

	expected := item.Version
	if signal.OccurredAt.After(item.LastSeenAt) {
		item.LastSeenAt = signal.OccurredAt
	}
	item.Fingerprint = signal.Fingerprint
	if resolutionRequiresVerification(item.Status) {
		item.Version++
		item.UpdatedAt = signal.ReceivedAt.UTC()
		if err := repos.Incidents.Update(ctx, item, expected); err != nil {
			return result, err
		}
		if err := repos.Signals.AttachToIncident(ctx, signal.ID, item.ID); err != nil {
			return result, err
		}
		if err := s.appendTimeline(ctx, repos, item.ID, domain.EventSignalReceived, domain.ActorSource, signal.SourceEventID, signal.Summary, map[string]any{"status": signal.Status, "source": signal.Source, "signal_recovered": true, "resolution_deferred_to_verification": true, "incident_status_preserved": item.Status}, signal.OccurredAt); err != nil {
			return result, err
		}
		if err := s.addOutbox(ctx, repos, item, "incident.signal_resolved", signal.ReceivedAt); err != nil {
			return result, err
		}
		result.IncidentPublicID = item.PublicID
		s.observeSignal(signal, "resolved_deferred_to_verification")
		return result, nil
	}
	from := item.Status
	eventType, err := item.Transition(domain.StatusResolved, signal.ReceivedAt)
	if err != nil {
		return result, err
	}
	if err := repos.Incidents.Update(ctx, item, expected); err != nil {
		return result, err
	}
	if err := repos.Signals.AttachToIncident(ctx, signal.ID, item.ID); err != nil {
		return result, err
	}
	if err := s.appendTimeline(ctx, repos, item.ID, domain.EventSignalReceived, domain.ActorSource, signal.SourceEventID, signal.Summary, map[string]any{"status": signal.Status, "source": signal.Source}, signal.OccurredAt); err != nil {
		return result, err
	}
	if err := s.recordTransition(ctx, repos, item, from, eventType, signal.ReceivedAt); err != nil {
		return result, err
	}
	result.IncidentPublicID = item.PublicID
	s.observeSignal(signal, "resolved")
	s.observeIncident("updated")
	return result, nil
}

func resolutionRequiresVerification(status domain.Status) bool {
	switch status {
	case domain.StatusDiagnosing,
		domain.StatusDiagnosisCompleted,
		domain.StatusPlanningRemediation,
		domain.StatusAwaitingApproval,
		domain.StatusApplyingChange,
		domain.StatusVerifying:
		return true
	default:
		return false
	}
}

func (s *Service) recordTransition(ctx context.Context, repos domain.Repositories, item *domain.Incident, from domain.Status, eventType domain.EventType, at time.Time) error {
	if err := s.appendTimeline(ctx, repos, item.ID, eventType, domain.ActorSystem, "incident-state-machine", fmt.Sprintf("Incident transitioned from %s to %s", from, item.Status), map[string]any{"from": from, "to": item.Status, "version": item.Version}, at); err != nil {
		return err
	}
	if err := s.addOutbox(ctx, repos, item, "incident.status_changed", at); err != nil {
		return err
	}
	if s.observer != nil {
		s.observer.ObserveTransition(string(from), string(item.Status), "success")
	}
	return nil
}

func (s *Service) appendTimeline(ctx context.Context, repos domain.Repositories, incidentID uint64, eventType domain.EventType, actorType domain.ActorType, actorID, summary string, metadata any, at time.Time) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal timeline metadata: %w", err)
	}
	if len(data) > 8*1024 {
		return fmt.Errorf("%w: timeline metadata exceeds 8192 bytes", domain.ErrInvalidArgument)
	}
	_, span := otel.Tracer("server-web/incident").Start(ctx, "incident.timeline.persist")
	defer span.End()
	return repos.Timeline.Append(ctx, &domain.TimelineEvent{IncidentID: incidentID, EventType: eventType, ActorType: actorType, ActorID: actorID, Summary: bounded(summary, maxSummaryBytes), Metadata: data, OccurredAt: at.UTC()})
}

func (s *Service) addOutbox(ctx context.Context, repos domain.Repositories, item *domain.Incident, eventType string, at time.Time) error {
	payload, err := json.Marshal(map[string]any{"incident_id": item.PublicID, "status": item.Status, "version": item.Version, "correlation_key": item.CorrelationKey, "occurred_at": at.UTC()})
	if err != nil {
		return err
	}
	_, span := otel.Tracer("server-web/incident").Start(ctx, "incident.outbox.persist")
	defer span.End()
	return repos.Outbox.Add(ctx, &domain.OutboxEvent{EventID: s.newID(), AggregateType: "incident", AggregateID: item.PublicID, EventType: eventType, SchemaVersion: 1, Payload: payload, OccurredAt: at.UTC()})
}

// GetIncident returns one Incident by public ID.
func (s *Service) GetIncident(ctx context.Context, publicID string) (*domain.Incident, error) {
	return s.uow.ReadRepositories().Incidents.GetByPublicID(ctx, publicID)
}

// ListIncidents returns a normalized bounded page.
func (s *Service) ListIncidents(ctx context.Context, filter domain.ListFilter) (domain.Page, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return s.uow.ReadRepositories().Incidents.List(ctx, filter)
}

// ListSignals returns bounded signals for one public Incident ID.
func (s *Service) ListSignals(ctx context.Context, publicID string) ([]domain.Signal, error) {
	item, err := s.GetIncident(ctx, publicID)
	if err != nil {
		return nil, err
	}
	return s.uow.ReadRepositories().Signals.ListByIncident(ctx, item.ID, defaultQueryLimit)
}

// ListTimeline returns bounded timeline events for one public Incident ID.
func (s *Service) ListTimeline(ctx context.Context, publicID string) ([]domain.TimelineEvent, error) {
	item, err := s.GetIncident(ctx, publicID)
	if err != nil {
		return nil, err
	}
	return s.uow.ReadRepositories().Timeline.ListByIncident(ctx, item.ID, defaultQueryLimit)
}

// ListEvidence returns bounded evidence metadata for one public Incident ID.
func (s *Service) ListEvidence(ctx context.Context, publicID string) ([]domain.EvidenceItem, error) {
	item, err := s.GetIncident(ctx, publicID)
	if err != nil {
		return nil, err
	}
	return s.uow.ReadRepositories().Evidence.ListByIncident(ctx, item.ID, defaultQueryLimit)
}

// TransitionIncident enforces a state change with optimistic locking, timeline and outbox in one transaction.
func (s *Service) TransitionIncident(ctx context.Context, publicID string, to domain.Status, actorType domain.ActorType, actorID string) error {
	at := s.now().UTC()
	return s.uow.WithinTransaction(ctx, func(repos domain.Repositories) error {
		item, err := repos.Incidents.GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		expected, from := item.Version, item.Status
		eventType, err := item.Transition(to, at)
		if err != nil {
			if s.observer != nil {
				s.observer.ObserveTransition(string(from), string(to), "rejected")
			}
			return err
		}
		if err := repos.Incidents.Update(ctx, item, expected); err != nil {
			return err
		}
		if err := s.appendTimeline(ctx, repos, item.ID, eventType, actorType, actorID, fmt.Sprintf("Incident transitioned from %s to %s", from, to), map[string]any{"from": from, "to": to, "version": item.Version}, at); err != nil {
			return err
		}
		if err := s.addOutbox(ctx, repos, item, "incident.status_changed", at); err != nil {
			return err
		}
		if s.observer != nil {
			s.observer.ObserveTransition(string(from), string(to), "success")
		}
		return nil
	})
}

func (s *Service) observeSignal(signal *domain.Signal, result string) {
	if s.observer != nil {
		s.observer.ObserveSignal(signal.Source, string(signal.Status), result)
	}
}

func (s *Service) observeIncident(operation string) {
	if s.observer != nil {
		s.observer.ObserveIncident(operation)
	}
}

func (s *Service) observeError(reason string) {
	if s.observer != nil {
		s.observer.ObserveIngestionError(reason)
	}
}
