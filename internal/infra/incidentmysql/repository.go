// Package incidentmysql implements Incident persistence with explicit GORM transactions.
package incidentmysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

var terminalIncidentStatuses = []string{
	string(domain.StatusResolved), string(domain.StatusFailed), string(domain.StatusClosedNoAction),
}

// Store is both the Unit of Work and the transaction-scoped repository adapter.
type Store struct {
	db *gorm.DB
}

var _ domain.UnitOfWork = (*Store)(nil)
var _ domain.IncidentRepository = (*Store)(nil)
var _ domain.OutboxRepository = (*Store)(nil)
var _ domain.CorrelationLocker = (*Store)(nil)
var _ domain.SignalRepository = signalAdapter{}
var _ domain.TimelineRepository = timelineAdapter{}
var _ domain.EvidenceRepository = evidenceAdapter{}
var _ domain.AgentRunRepository = agentRunAdapter{}
var _ domain.AgentStepRepository = agentStepAdapter{}

// NewStore creates a MySQL-backed Incident store.
func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: gorm database is required", domain.ErrInvalidArgument)
	}
	return &Store{db: db}, nil
}

// WithinTransaction executes work with repository instances bound to one DB transaction.
func (s *Store) WithinTransaction(ctx context.Context, work func(domain.Repositories) error) error {
	if s == nil || s.db == nil {
		return domain.ErrUnavailable
	}
	if work == nil {
		return fmt.Errorf("%w: transaction callback is required", domain.ErrInvalidArgument)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return work((&Store{db: tx}).repositories())
	})
}

// ReadRepositories returns repositories bound to the root connection pool.
func (s *Store) ReadRepositories() domain.Repositories { return s.repositories() }

func (s *Store) repositories() domain.Repositories {
	return domain.Repositories{
		Incidents: s, Signals: signalAdapter{s}, Timeline: timelineAdapter{s}, Evidence: evidenceAdapter{s}, AgentRuns: agentRunAdapter{s},
		AgentSteps: agentStepAdapter{s}, Outbox: s, Correlations: s,
	}
}

// Lock serializes one correlation key until the current transaction completes.
func (s *Store) Lock(ctx context.Context, key string, at time.Time) error {
	if key == "" || len(key) > 67 {
		return fmt.Errorf("%w: invalid correlation key", domain.ErrInvalidArgument)
	}
	row := correlationLockRow{CorrelationKey: key, TouchedAt: at.UTC()}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "correlation_key"}},
		DoUpdates: clause.Assignments(map[string]any{"touched_at": row.TouchedAt}),
	}).Create(&row).Error; err != nil {
		return classify(err)
	}
	return classify(s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("correlation_key = ?", key).First(&row).Error)
}

// Create persists a new Incident aggregate.
func (s *Store) Create(ctx context.Context, item *domain.Incident) error {
	if item == nil {
		return fmt.Errorf("%w: incident is required", domain.ErrInvalidArgument)
	}
	row := incidentToRow(item)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classify(err)
	}
	item.ID, item.CreatedAt, item.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return nil
}

// Update persists an aggregate only when its expected version still matches.
func (s *Store) Update(ctx context.Context, item *domain.Incident, expectedVersion uint64) error {
	if item == nil || item.ID == 0 || item.Version <= expectedVersion {
		return fmt.Errorf("%w: invalid optimistic update", domain.ErrInvalidArgument)
	}
	updates := map[string]any{
		"fingerprint": item.Fingerprint, "correlation_key": item.CorrelationKey,
		"cluster": item.Cluster, "namespace": item.Namespace, "service_name": item.ServiceName,
		"environment": item.Environment, "target_kind": item.TargetKind, "target_name": item.TargetName,
		"severity": item.Severity, "status": item.Status, "summary": item.Summary,
		"first_seen_at": item.FirstSeenAt, "last_seen_at": item.LastSeenAt,
		"resolved_at": item.ResolvedAt, "current_agent_run_id": item.CurrentAgentRunID,
		"version": item.Version, "updated_at": item.UpdatedAt,
	}
	result := s.db.WithContext(ctx).Model(&incidentRow{}).Where("id = ? AND version = ?", item.ID, expectedVersion).Updates(updates)
	if result.Error != nil {
		return classify(result.Error)
	}
	if result.RowsAffected != 1 {
		return domain.ErrConflict
	}
	return nil
}

// FindOpenByFingerprint locks and returns the newest matching non-terminal Incident.
func (s *Store) FindOpenByFingerprint(ctx context.Context, fingerprint string, since time.Time) (*domain.Incident, error) {
	query := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("fingerprint = ? AND status NOT IN ?", fingerprint, terminalIncidentStatuses)
	return s.findIncident(query, since)
}

// FindOpenByCorrelationKey locks and returns the newest matching non-terminal Incident.
func (s *Store) FindOpenByCorrelationKey(ctx context.Context, key string, since time.Time) (*domain.Incident, error) {
	query := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("correlation_key = ? AND status NOT IN ?", key, terminalIncidentStatuses)
	return s.findIncident(query, since)
}

// FindRecentResolvedByCorrelationKey locks a recently resolved Incident for reopen.
func (s *Store) FindRecentResolvedByCorrelationKey(ctx context.Context, key string, since time.Time) (*domain.Incident, error) {
	query := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("correlation_key = ? AND status = ?", key, domain.StatusResolved)
	return s.findIncident(query, since)
}

func (s *Store) findIncident(query *gorm.DB, since time.Time) (*domain.Incident, error) {
	if !since.IsZero() {
		query = query.Where("last_seen_at >= ?", since.UTC())
	}
	var row incidentRow
	err := query.Order("last_seen_at DESC").Order("id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, classify(err)
	}
	item := incidentFromRow(row)
	return &item, nil
}

// GetByPublicID returns one Incident without exposing its numeric key to callers.
func (s *Store) GetByPublicID(ctx context.Context, publicID string) (*domain.Incident, error) {
	var row incidentRow
	err := s.db.WithContext(ctx).Where("public_id = ?", publicID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, classify(err)
	}
	item := incidentFromRow(row)
	return &item, nil
}

// List applies bounded filters and stable reverse chronological ordering.
func (s *Store) List(ctx context.Context, filter domain.ListFilter) (domain.Page, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return domain.Page{}, fmt.Errorf("%w: invalid page", domain.ErrInvalidArgument)
	}
	query := s.db.WithContext(ctx).Model(&incidentRow{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Cluster != "" {
		query = query.Where("cluster = ?", filter.Cluster)
	}
	if filter.Namespace != "" {
		query = query.Where("namespace = ?", filter.Namespace)
	}
	if filter.ServiceName != "" {
		query = query.Where("service_name = ?", filter.ServiceName)
	}
	if filter.Environment != "" {
		query = query.Where("environment = ?", filter.Environment)
	}
	if filter.Workload != "" {
		query = query.Where("target_name = ?", filter.Workload)
	}
	if filter.Search != "" {
		pattern := "%" + escapeLike(filter.Search) + "%"
		query = query.Where("(summary LIKE ? ESCAPE '\\\\' OR service_name LIKE ? ESCAPE '\\\\' OR target_name LIKE ? ESCAPE '\\\\' OR namespace LIKE ? ESCAPE '\\\\')", pattern, pattern, pattern, pattern)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", filter.CreatedFrom.UTC())
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at <= ?", filter.CreatedTo.UTC())
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.Page{}, classify(err)
	}
	var rows []incidentRow
	if err := query.Order("updated_at DESC").Order("id DESC").Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Find(&rows).Error; err != nil {
		return domain.Page{}, classify(err)
	}
	items := make([]domain.Incident, 0, len(rows))
	for _, row := range rows {
		items = append(items, incidentFromRow(row))
	}
	return domain.Page{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// CreateIfAbsent inserts a signal at most once by source and source event ID.
func (s *Store) CreateIfAbsent(ctx context.Context, signal *domain.Signal) (bool, error) {
	if signal == nil {
		return false, fmt.Errorf("%w: signal is required", domain.ErrInvalidArgument)
	}
	row := signalToRow(signal)
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if result.Error != nil {
		return false, classify(result.Error)
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	signal.ID, signal.CreatedAt = row.ID, row.CreatedAt
	return true, nil
}

// AttachToIncident links a newly inserted signal to its aggregate.
func (s *Store) AttachToIncident(ctx context.Context, signalID, incidentID uint64) error {
	result := s.db.WithContext(ctx).Model(&signalRow{}).Where("id = ? AND incident_id IS NULL", signalID).Update("incident_id", incidentID)
	if result.Error != nil {
		return classify(result.Error)
	}
	if result.RowsAffected != 1 {
		return domain.ErrConflict
	}
	return nil
}

// ListByIncident returns bounded signals in occurrence order.
func (s *Store) ListSignalsByIncident(ctx context.Context, incidentID uint64, limit int) ([]domain.Signal, error) {
	limit = boundedLimit(limit)
	var rows []signalRow
	if err := s.db.WithContext(ctx).Where("incident_id = ?", incidentID).Order("occurred_at ASC").Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, classify(err)
	}
	items := make([]domain.Signal, 0, len(rows))
	for _, row := range rows {
		items = append(items, signalFromRow(row))
	}
	return items, nil
}

// Append writes one bounded timeline event.
func (s *Store) AppendTimeline(ctx context.Context, event *domain.TimelineEvent) error {
	if event == nil || len(event.Metadata) > 8*1024 || len(event.Summary) > 2048 {
		return fmt.Errorf("%w: invalid timeline event", domain.ErrInvalidArgument)
	}
	row := timelineToRow(event)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classify(err)
	}
	event.ID, event.CreatedAt = row.ID, row.CreatedAt
	return nil
}

// ListByIncident returns bounded timeline history.
func (s *Store) ListTimelineByIncident(ctx context.Context, incidentID uint64, limit int) ([]domain.TimelineEvent, error) {
	var rows []timelineRow
	if err := s.db.WithContext(ctx).Where("incident_id = ?", incidentID).Order("occurred_at ASC").Order("id ASC").Limit(boundedLimit(limit)).Find(&rows).Error; err != nil {
		return nil, classify(err)
	}
	items := make([]domain.TimelineEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, timelineFromRow(row))
	}
	return items, nil
}

// Create writes a bounded EvidenceItem contract.
func (s *Store) CreateEvidence(ctx context.Context, item *domain.EvidenceItem) error {
	return s.createEvidence(ctx, item)
}

// Create persists an EvidenceItem. This method is named Create to implement EvidenceRepository.
func (s *Store) createEvidence(ctx context.Context, item *domain.EvidenceItem) error {
	if item == nil || len(item.TimeRange) > 8*1024 || len(item.Facts) > 16*1024 || len(item.Query) > 4096 || len(item.Summary) > 4096 {
		return fmt.Errorf("%w: invalid evidence item", domain.ErrInvalidArgument)
	}
	row := evidenceToRow(item)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classify(err)
	}
	item.ID, item.CreatedAt = row.ID, row.CreatedAt
	return nil
}

// ListEvidenceByIncident returns bounded Evidence metadata.
func (s *Store) ListEvidenceByIncident(ctx context.Context, incidentID uint64, limit int) ([]domain.EvidenceItem, error) {
	var rows []evidenceRow
	if err := s.db.WithContext(ctx).Where("incident_id = ?", incidentID).Order("collected_at ASC").Order("id ASC").Limit(boundedLimit(limit)).Find(&rows).Error; err != nil {
		return nil, classify(err)
	}
	items := make([]domain.EvidenceItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, evidenceFromRow(row))
	}
	return items, nil
}

// CreateAgentRun persists a future AgentRun contract.
func (s *Store) CreateAgentRun(ctx context.Context, run *domain.AgentRun) error {
	if run == nil || run.MaxSteps <= 0 || run.UsedSteps < 0 || run.UsedSteps > run.MaxSteps || len(run.CurrentCheckpoint) > 32*1024 || len(run.FinalDiagnosis) > 32*1024 {
		return fmt.Errorf("%w: invalid agent run", domain.ErrInvalidArgument)
	}
	row := agentRunToRow(run)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classify(err)
	}
	run.ID, run.CreatedAt, run.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return nil
}

// GetAgentRunByPublicID returns one future AgentRun contract.
func (s *Store) GetAgentRunByPublicID(ctx context.Context, publicID string) (*domain.AgentRun, error) {
	var row agentRunRow
	err := s.db.WithContext(ctx).Where("public_id = ?", publicID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, classify(err)
	}
	run := agentRunFromRow(row)
	return &run, nil
}

// TransitionAgentRun enforces AgentRun status transitions with compare-and-set semantics.
func (s *Store) TransitionAgentRun(ctx context.Context, id uint64, expected, next domain.AgentRunStatus, at time.Time) error {
	if !domain.CanTransitionAgentRun(expected, next) {
		return fmt.Errorf("%w: agent run %s -> %s", domain.ErrInvalidTransition, expected, next)
	}
	updates := map[string]any{"status": next, "updated_at": at.UTC()}
	if next == domain.AgentRunRunning {
		updates["started_at"] = at.UTC()
	}
	if next == domain.AgentRunCompleted || next == domain.AgentRunFailed || next == domain.AgentRunCancelled {
		updates["completed_at"] = at.UTC()
	}
	result := s.db.WithContext(ctx).Model(&agentRunRow{}).Where("id = ? AND status = ?", id, expected).Updates(updates)
	if result.Error != nil {
		return classify(result.Error)
	}
	if result.RowsAffected != 1 {
		return domain.ErrConflict
	}
	return nil
}

// CreateAgentStep persists a bounded AgentStep and relies on run+sequence uniqueness.
func (s *Store) CreateAgentStep(ctx context.Context, step *domain.AgentStep) error {
	if step == nil || step.Sequence <= 0 || len(step.ShortReason) > 1024 || len(step.Arguments) > 8*1024 || len(step.ResultSummary) > 4096 {
		return fmt.Errorf("%w: invalid agent step", domain.ErrInvalidArgument)
	}
	row := agentStepToRow(step)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classify(err)
	}
	step.ID, step.CreatedAt = row.ID, row.CreatedAt
	return nil
}

// ListAgentStepsByRun returns bounded AgentStep summaries.
func (s *Store) ListAgentStepsByRun(ctx context.Context, runID uint64, limit int) ([]domain.AgentStep, error) {
	var rows []agentStepRow
	if err := s.db.WithContext(ctx).Where("agent_run_id = ?", runID).Order("sequence ASC").Limit(boundedLimit(limit)).Find(&rows).Error; err != nil {
		return nil, classify(err)
	}
	items := make([]domain.AgentStep, 0, len(rows))
	for _, row := range rows {
		items = append(items, agentStepFromRow(row))
	}
	return items, nil
}

// Add stores one bounded transactional outbox record.
func (s *Store) Add(ctx context.Context, event *domain.OutboxEvent) error {
	if event == nil || event.SchemaVersion <= 0 || len(event.Payload) > 32*1024 {
		return fmt.Errorf("%w: invalid outbox event", domain.ErrInvalidArgument)
	}
	row := outboxToRow(event)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classify(err)
	}
	event.ID, event.CreatedAt = row.ID, row.CreatedAt
	return nil
}

// PendingCount counts unpublished outbox records for telemetry.
func (s *Store) PendingCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&outboxRow{}).Where("published_at IS NULL").Count(&count).Error
	return count, classify(err)
}

// Adapter methods disambiguate repository interfaces that share Create/List method names.
type signalAdapter struct{ *Store }
type timelineAdapter struct{ *Store }
type evidenceAdapter struct{ *Store }
type agentRunAdapter struct{ *Store }
type agentStepAdapter struct{ *Store }

func (a signalAdapter) CreateIfAbsent(ctx context.Context, item *domain.Signal) (bool, error) {
	return a.Store.CreateIfAbsent(ctx, item)
}
func (a signalAdapter) AttachToIncident(ctx context.Context, signalID, incidentID uint64) error {
	return a.Store.AttachToIncident(ctx, signalID, incidentID)
}
func (a signalAdapter) ListByIncident(ctx context.Context, incidentID uint64, limit int) ([]domain.Signal, error) {
	return a.ListSignalsByIncident(ctx, incidentID, limit)
}
func (a timelineAdapter) Append(ctx context.Context, item *domain.TimelineEvent) error {
	return a.AppendTimeline(ctx, item)
}
func (a timelineAdapter) ListByIncident(ctx context.Context, incidentID uint64, limit int) ([]domain.TimelineEvent, error) {
	return a.ListTimelineByIncident(ctx, incidentID, limit)
}

func (a evidenceAdapter) Create(ctx context.Context, item *domain.EvidenceItem) error {
	return a.createEvidence(ctx, item)
}
func (a evidenceAdapter) ListByIncident(ctx context.Context, incidentID uint64, limit int) ([]domain.EvidenceItem, error) {
	return a.ListEvidenceByIncident(ctx, incidentID, limit)
}
func (a agentRunAdapter) Create(ctx context.Context, run *domain.AgentRun) error {
	return a.CreateAgentRun(ctx, run)
}
func (a agentRunAdapter) GetByPublicID(ctx context.Context, publicID string) (*domain.AgentRun, error) {
	return a.GetAgentRunByPublicID(ctx, publicID)
}
func (a agentRunAdapter) Transition(ctx context.Context, id uint64, expected, next domain.AgentRunStatus, at time.Time) error {
	return a.TransitionAgentRun(ctx, id, expected, next, at)
}
func (a agentStepAdapter) Create(ctx context.Context, step *domain.AgentStep) error {
	return a.CreateAgentStep(ctx, step)
}
func (a agentStepAdapter) ListByRun(ctx context.Context, runID uint64, limit int) ([]domain.AgentStep, error) {
	return a.ListAgentStepsByRun(ctx, runID, limit)
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *drivermysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%w: duplicate key", domain.ErrConflict)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	return err
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func incidentToRow(item *domain.Incident) incidentRow {
	return incidentRow{ID: item.ID, PublicID: item.PublicID, Fingerprint: item.Fingerprint, CorrelationKey: item.CorrelationKey, Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, Environment: item.Environment, TargetKind: item.TargetKind, TargetName: item.TargetName, Severity: string(item.Severity), Status: string(item.Status), Summary: item.Summary, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, ResolvedAt: item.ResolvedAt, CurrentAgentRunID: item.CurrentAgentRunID, Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func incidentFromRow(row incidentRow) domain.Incident {
	return domain.Incident{ID: row.ID, PublicID: row.PublicID, Fingerprint: row.Fingerprint, CorrelationKey: row.CorrelationKey, Cluster: row.Cluster, Namespace: row.Namespace, ServiceName: row.ServiceName, Environment: row.Environment, TargetKind: row.TargetKind, TargetName: row.TargetName, Severity: domain.Severity(row.Severity), Status: domain.Status(row.Status), Summary: row.Summary, FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, ResolvedAt: row.ResolvedAt, CurrentAgentRunID: row.CurrentAgentRunID, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func signalToRow(item *domain.Signal) signalRow {
	return signalRow{ID: item.ID, IncidentID: item.IncidentID, Source: item.Source, SourceEventID: item.SourceEventID, Fingerprint: item.Fingerprint, Status: string(item.Status), Severity: string(item.Severity), Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, Environment: item.Environment, TargetKind: item.TargetKind, TargetName: item.TargetName, Category: item.Category, OccurredAt: item.OccurredAt, ReceivedAt: item.ReceivedAt, Summary: item.Summary, LabelsJSON: item.Labels, AnnotationsJSON: item.Annotations, RawPayload: item.RawPayload, CreatedAt: item.CreatedAt}
}

func signalFromRow(row signalRow) domain.Signal {
	return domain.Signal{ID: row.ID, IncidentID: row.IncidentID, Source: row.Source, SourceEventID: row.SourceEventID, Fingerprint: row.Fingerprint, Status: domain.SignalStatus(row.Status), Severity: domain.Severity(row.Severity), Cluster: row.Cluster, Namespace: row.Namespace, ServiceName: row.ServiceName, Environment: row.Environment, TargetKind: row.TargetKind, TargetName: row.TargetName, Category: row.Category, OccurredAt: row.OccurredAt, ReceivedAt: row.ReceivedAt, Summary: row.Summary, Labels: row.LabelsJSON, Annotations: row.AnnotationsJSON, RawPayload: row.RawPayload, CreatedAt: row.CreatedAt}
}

func timelineToRow(item *domain.TimelineEvent) timelineRow {
	return timelineRow{ID: item.ID, IncidentID: item.IncidentID, EventType: string(item.EventType), ActorType: string(item.ActorType), ActorID: item.ActorID, Summary: item.Summary, MetadataJSON: item.Metadata, OccurredAt: item.OccurredAt, CreatedAt: item.CreatedAt}
}
func timelineFromRow(row timelineRow) domain.TimelineEvent {
	return domain.TimelineEvent{ID: row.ID, IncidentID: row.IncidentID, EventType: domain.EventType(row.EventType), ActorType: domain.ActorType(row.ActorType), ActorID: row.ActorID, Summary: row.Summary, Metadata: row.MetadataJSON, OccurredAt: row.OccurredAt, CreatedAt: row.CreatedAt}
}
func evidenceToRow(item *domain.EvidenceItem) evidenceRow {
	return evidenceRow{ID: item.ID, PublicID: item.PublicID, IncidentID: item.IncidentID, AgentRunID: item.AgentRunID, Type: item.Type, Source: item.Source, ToolName: item.ToolName, ResourceRef: item.ResourceRef, TimeRangeJSON: item.TimeRange, QueryText: item.Query, Summary: item.Summary, FactsJSON: item.Facts, ResultHash: item.ResultHash, RawRef: item.RawRef, RedactionJSON: item.Redaction, Truncated: item.Truncated, Valid: item.Valid, CollectedAt: item.CollectedAt, CreatedAt: item.CreatedAt}
}
func evidenceFromRow(row evidenceRow) domain.EvidenceItem {
	return domain.EvidenceItem{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, AgentRunID: row.AgentRunID, Type: row.Type, Source: row.Source, ToolName: row.ToolName, ResourceRef: row.ResourceRef, TimeRange: row.TimeRangeJSON, Query: row.QueryText, Summary: row.Summary, Facts: row.FactsJSON, ResultHash: row.ResultHash, RawRef: row.RawRef, Redaction: row.RedactionJSON, Truncated: row.Truncated, Valid: row.Valid, CollectedAt: row.CollectedAt, CreatedAt: row.CreatedAt}
}
func agentRunToRow(item *domain.AgentRun) agentRunRow {
	return agentRunRow{ID: item.ID, PublicID: item.PublicID, IncidentID: item.IncidentID, Attempt: 1, Status: string(item.Status), Model: item.Model, PromptVersion: item.PromptVersion, MaxSteps: item.MaxSteps, UsedSteps: item.UsedSteps, MaxToolCalls: 1, MaxModelCalls: 1, TokenBudget: maxInt64(1, item.InputTokens+item.OutputTokens), InputTokens: item.InputTokens, OutputTokens: item.OutputTokens, MaxEvidenceItems: 1, MaxRuntimeMS: 120000, ToolTimeoutMS: 15000, MaxEvidenceBytes: 16384, MaxCheckpointBytes: 32768, MaxStepRetries: 1, CurrentCheckpoint: item.CurrentCheckpoint, CheckpointSchemaVersion: 1, FinalDiagnosis: item.FinalDiagnosis, FailureCode: item.FailureCode, RowVersion: 1, StartedAt: item.StartedAt, CompletedAt: item.CompletedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
func agentRunFromRow(row agentRunRow) domain.AgentRun {
	return domain.AgentRun{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, Status: domain.AgentRunStatus(row.Status), Model: row.Model, PromptVersion: row.PromptVersion, MaxSteps: row.MaxSteps, UsedSteps: row.UsedSteps, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, CurrentCheckpoint: row.CurrentCheckpoint, FinalDiagnosis: row.FinalDiagnosis, FailureCode: row.FailureCode, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func agentStepToRow(item *domain.AgentStep) agentStepRow {
	return agentStepRow{ID: item.ID, PublicID: item.PublicID, AgentRunID: item.AgentRunID, Sequence: item.Sequence, StepType: item.StepType, ShortReason: item.ShortReason, SelectedTool: item.SelectedTool, ArgumentsJSON: item.Arguments, ResultSummary: item.ResultSummary, ResultRef: item.ResultRef, Status: string(item.Status), DurationMS: item.DurationMS, InputTokens: item.InputTokens, OutputTokens: item.OutputTokens, CreatedAt: item.CreatedAt}
}
func agentStepFromRow(row agentStepRow) domain.AgentStep {
	return domain.AgentStep{ID: row.ID, PublicID: row.PublicID, AgentRunID: row.AgentRunID, Sequence: row.Sequence, StepType: row.StepType, ShortReason: row.ShortReason, SelectedTool: row.SelectedTool, Arguments: row.ArgumentsJSON, ResultSummary: row.ResultSummary, ResultRef: row.ResultRef, Status: domain.AgentStepStatus(row.Status), DurationMS: row.DurationMS, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, CreatedAt: row.CreatedAt}
}
func outboxToRow(item *domain.OutboxEvent) outboxRow {
	return outboxRow{ID: item.ID, EventID: item.EventID, AggregateType: item.AggregateType, AggregateID: item.AggregateID, EventType: item.EventType, SchemaVersion: item.SchemaVersion, PayloadJSON: item.Payload, OccurredAt: item.OccurredAt, PublishedAt: item.PublishedAt, Attempts: item.Attempts, LastError: item.LastError, CreatedAt: item.CreatedAt}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
