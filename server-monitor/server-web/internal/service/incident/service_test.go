package incident

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "server-web/internal/incident"
	"server-web/internal/infra/webhook"
)

func TestIngestLifecycleAggregationIdempotencyAndReopen(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)

	first := firingPayload("fp-1", "checkout", clock)
	results, err := service.IngestAlertmanager(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].IncidentPublicID == "" || results[0].Duplicate {
		t.Fatalf("unexpected first result: %+v", results)
	}
	state := store.snapshot()
	if len(state.incidents) != 1 || len(state.signals) != 1 || len(state.timeline) != 3 || len(state.outbox) != 2 {
		t.Fatalf("unexpected first transaction state: %+v", state.counts())
	}
	incidentID := onlyIncident(state).ID
	if onlyIncident(state).Status != domain.StatusCorrelating {
		t.Fatalf("expected correlating, got %s", onlyIncident(state).Status)
	}

	results, err = service.IngestAlertmanager(context.Background(), first)
	if err != nil || !results[0].Duplicate {
		t.Fatalf("expected idempotent duplicate: results=%+v err=%v", results, err)
	}
	if got := store.snapshot().counts(); got != (fakeCounts{1, 1, 3, 2}) {
		t.Fatalf("duplicate produced business effects: %+v", got)
	}

	clock = clock.Add(time.Minute)
	related := firingPayload("fp-2", "checkout", clock)
	_, err = service.IngestAlertmanager(context.Background(), related)
	if err != nil {
		t.Fatal(err)
	}
	state = store.snapshot()
	if len(state.incidents) != 1 || len(state.signals) != 2 || len(state.timeline) != 5 || len(state.outbox) != 3 {
		t.Fatalf("related signal did not aggregate: %+v", state.counts())
	}
	if onlyIncident(state).ID != incidentID || onlyIncident(state).Severity != domain.SeverityCritical {
		t.Fatalf("unexpected severity/incident merge: %+v", onlyIncident(state))
	}

	clock = clock.Add(time.Minute)
	unrelated := firingPayload("fp-3", "catalog", clock)
	if _, err := service.IngestAlertmanager(context.Background(), unrelated); err != nil {
		t.Fatal(err)
	}
	if len(store.snapshot().incidents) != 2 {
		t.Fatal("unrelated signal did not create a new Incident")
	}

	clock = clock.Add(time.Minute)
	resolved := first.Alerts[0]
	resolved.Status = "resolved"
	resolved.EndsAt = clock
	if _, err := service.IngestAlertmanager(context.Background(), webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{resolved}}); err != nil {
		t.Fatal(err)
	}
	resolvedIncident := store.snapshot().incidents[incidentID]
	if resolvedIncident.Status != domain.StatusResolved || resolvedIncident.ResolvedAt == nil {
		t.Fatalf("incident was not resolved: %+v", resolvedIncident)
	}

	clock = clock.Add(time.Minute)
	reopen := firingPayload("fp-4", "checkout", clock)
	if _, err := service.IngestAlertmanager(context.Background(), reopen); err != nil {
		t.Fatal(err)
	}
	reopened := store.snapshot().incidents[incidentID]
	if reopened.Status != domain.StatusDiagnosing || reopened.ResolvedAt != nil {
		t.Fatalf("incident was not reopened: %+v", reopened)
	}
}

func TestResolvedWithoutIncidentIsExplicitNoOp(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	record := firingPayload("missing", "checkout", clock).Alerts[0]
	record.Status, record.EndsAt = "resolved", clock.Add(time.Minute)
	result, err := service.IngestAlertmanager(context.Background(), webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{record}})
	if err != nil {
		t.Fatal(err)
	}
	if !result[0].NoMatchingIncident || result[0].IncidentPublicID != "" {
		t.Fatalf("unexpected unmatched result: %+v", result)
	}
	state := store.snapshot()
	if len(state.signals) != 1 || len(state.incidents) != 0 || len(state.timeline) != 0 || len(state.outbox) != 0 {
		t.Fatalf("unmatched resolution produced aggregate effects: %+v", state.counts())
	}
}

func TestResolvedSignalDefersIncidentClosureAfterInvestigationStarts(t *testing.T) {
	tests := []struct {
		name   string
		status domain.Status
	}{
		{name: "diagnosing", status: domain.StatusDiagnosing},
		{name: "awaiting approval", status: domain.StatusAwaitingApproval},
		{name: "applying change", status: domain.StatusApplyingChange},
		{name: "verifying", status: domain.StatusVerifying},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeUnitOfWork()
			clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
			service := mustTestService(t, store, &clock)
			payload := firingPayload("change-in-flight-"+tt.name, "checkout", clock)
			result, err := service.IngestAlertmanager(context.Background(), payload)
			if err != nil {
				t.Fatal(err)
			}
			advanceIncidentTo(t, service, result[0].IncidentPublicID, tt.status, &clock)
			before := store.snapshot().counts()

			resolved := payload.Alerts[0]
			resolved.Status, resolved.EndsAt = "resolved", clock.Add(time.Second)
			if _, err := service.IngestAlertmanager(context.Background(), webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{resolved}}); err != nil {
				t.Fatal(err)
			}
			state := store.snapshot()
			if item := onlyIncident(state); item.Status != tt.status || item.ResolvedAt != nil {
				t.Fatalf("resolved signal must preserve %s until Verification closure: %+v", tt.status, item)
			}
			if state.counts().signals != before.signals+1 || state.counts().timeline != before.timeline+1 || state.counts().outbox != before.outbox+1 {
				t.Fatalf("resolved recovery fact was not persisted exactly once: before=%+v after=%+v", before, state.counts())
			}
			metadata := string(state.timeline[len(state.timeline)-1].Metadata)
			if !strings.Contains(metadata, `"signal_recovered":true`) || !strings.Contains(metadata, `"resolution_deferred_to_verification":true`) {
				t.Fatalf("recovery timeline metadata missing: %s", metadata)
			}
		})
	}
}

func TestDuplicateResolvedSignalIsIdempotent(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	payload := firingPayload("duplicate-resolved", "checkout", clock)
	result, err := service.IngestAlertmanager(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	advanceIncidentTo(t, service, result[0].IncidentPublicID, domain.StatusDiagnosing, &clock)
	resolved := payload.Alerts[0]
	resolved.Status, resolved.EndsAt = "resolved", clock.Add(time.Second)
	request := webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{resolved}}
	if _, err := service.IngestAlertmanager(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	afterFirst := store.snapshot().counts()
	duplicate, err := service.IngestAlertmanager(context.Background(), request)
	if err != nil || len(duplicate) != 1 || !duplicate[0].Duplicate {
		t.Fatalf("duplicate resolved result=%+v err=%v", duplicate, err)
	}
	if afterSecond := store.snapshot().counts(); afterSecond != afterFirst {
		t.Fatalf("duplicate resolved produced business effects: first=%+v second=%+v", afterFirst, afterSecond)
	}
}

func TestConcurrentFiringAndResolvedPreserveInvestigation(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	payload := firingPayload("concurrent-base", "checkout", clock)
	result, err := service.IngestAlertmanager(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	advanceIncidentTo(t, service, result[0].IncidentPublicID, domain.StatusDiagnosing, &clock)

	firing := firingPayload("concurrent-followup", "checkout", clock.Add(time.Second))
	resolved := payload.Alerts[0]
	resolved.Status, resolved.EndsAt = "resolved", clock.Add(2*time.Second)
	requests := []webhook.AlertmanagerWebhookRequest{firing, {Alerts: []webhook.AlertRecord{resolved}}}
	errCh := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ingestErr := service.IngestAlertmanager(context.Background(), request)
			errCh <- ingestErr
		}()
	}
	wg.Wait()
	close(errCh)
	for ingestErr := range errCh {
		if ingestErr != nil {
			t.Fatal(ingestErr)
		}
	}
	state := store.snapshot()
	if item := onlyIncident(state); item.Status != domain.StatusDiagnosing || item.ResolvedAt != nil {
		t.Fatalf("concurrent recovery bypassed investigation: %+v", item)
	}
	if len(state.incidents) != 1 || len(state.signals) != 3 {
		t.Fatalf("unexpected concurrent aggregation state: %+v", state.counts())
	}
}

func TestResolvedSignalStaleVersionRollsBack(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	payload := firingPayload("stale-resolved", "checkout", clock)
	result, err := service.IngestAlertmanager(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	advanceIncidentTo(t, service, result[0].IncidentPublicID, domain.StatusDiagnosing, &clock)
	before := store.snapshot().counts()
	store.failAt = "incident_update_conflict"
	resolved := payload.Alerts[0]
	resolved.Status, resolved.EndsAt = "resolved", clock.Add(time.Second)
	_, err = service.IngestAlertmanager(context.Background(), webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{resolved}})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected stale version conflict, got %v", err)
	}
	if after := store.snapshot().counts(); after != before {
		t.Fatalf("stale resolved write was not rolled back: before=%+v after=%+v", before, after)
	}
}

func advanceIncidentTo(t *testing.T, service *Service, publicID string, target domain.Status, clock *time.Time) {
	t.Helper()
	path := []domain.Status{domain.StatusDiagnosing, domain.StatusDiagnosisCompleted, domain.StatusPlanningRemediation, domain.StatusAwaitingApproval, domain.StatusApplyingChange, domain.StatusVerifying}
	for _, next := range path {
		*clock = clock.Add(time.Second)
		if err := service.TransitionIncident(context.Background(), publicID, next, domain.ActorSystem, "test"); err != nil {
			t.Fatal(err)
		}
		if next == target {
			return
		}
	}
	t.Fatalf("unsupported target status %s", target)
}

func TestTransactionFailureRollsBackAllEffects(t *testing.T) {
	store := newFakeUnitOfWork()
	store.failAt = "outbox"
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	_, err := service.IngestAlertmanager(context.Background(), firingPayload("fp", "checkout", clock))
	if err == nil {
		t.Fatal("expected injected transaction failure")
	}
	if got := store.snapshot().counts(); got != (fakeCounts{}) {
		t.Fatalf("transaction left partial state: %+v", got)
	}
}

func TestConcurrentDuplicateDeliveryCreatesOneIncident(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	payload := firingPayload("same", "checkout", clock)
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	var duplicates atomic.Int64
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.IngestAlertmanager(context.Background(), payload)
			if err != nil {
				errs <- err
				return
			}
			if result[0].Duplicate {
				duplicates.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if duplicates.Load() != workers-1 {
		t.Fatalf("duplicates=%d want=%d", duplicates.Load(), workers-1)
	}
	if got := store.snapshot().counts(); got != (fakeCounts{1, 1, 3, 2}) {
		t.Fatalf("concurrent duplicate produced extra state: %+v", got)
	}
}

func TestTransitionIncidentPersistsStateTimelineAndOutbox(t *testing.T) {
	store := newFakeUnitOfWork()
	clock := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	service := mustTestService(t, store, &clock)
	result, err := service.IngestAlertmanager(context.Background(), firingPayload("transition", "checkout", clock))
	if err != nil {
		t.Fatal(err)
	}
	before := store.snapshot().counts()
	clock = clock.Add(time.Minute)
	if err := service.TransitionIncident(context.Background(), result[0].IncidentPublicID, domain.StatusClosedNoAction, domain.ActorUser, "operator"); err != nil {
		t.Fatal(err)
	}
	state := store.snapshot()
	item := onlyIncident(state)
	if item.Status != domain.StatusClosedNoAction || state.counts().timeline != before.timeline+1 || state.counts().outbox != before.outbox+1 {
		t.Fatalf("transition invariants not persisted: incident=%+v counts=%+v", item, state.counts())
	}
	if err := service.TransitionIncident(context.Background(), result[0].IncidentPublicID, domain.StatusResolved, domain.ActorUser, "operator"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func firingPayload(fingerprint, service string, startsAt time.Time) webhook.AlertmanagerWebhookRequest {
	return webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{{
		Status: "firing", Fingerprint: fingerprint, StartsAt: startsAt,
		Labels:      map[string]string{"alertname": "HighErrorRate", "cluster": "prod", "namespace": "payments", "service": service, "severity": "critical"},
		Annotations: map[string]string{"summary": service + " error rate is high"},
	}}}
}

func mustTestService(t *testing.T, uow *fakeUnitOfWork, clock *time.Time) *Service {
	t.Helper()
	var ids atomic.Int64
	service, err := NewService(Config{UnitOfWork: uow, AggregationWindow: 4 * time.Hour, Now: func() time.Time { return *clock }, NewID: func() string { return fmt.Sprintf("id-%d", ids.Add(1)) }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type fakeCounts struct{ incidents, signals, timeline, outbox int }

type fakeState struct {
	incidents map[uint64]domain.Incident
	signals   map[string]domain.Signal
	timeline  []domain.TimelineEvent
	outbox    []domain.OutboxEvent
	nextID    uint64
}

func newFakeState() fakeState {
	return fakeState{incidents: map[uint64]domain.Incident{}, signals: map[string]domain.Signal{}, nextID: 1}
}

func (s fakeState) clone() fakeState {
	clone := newFakeState()
	clone.nextID = s.nextID
	for key, value := range s.incidents {
		clone.incidents[key] = value
	}
	for key, value := range s.signals {
		clone.signals[key] = value
	}
	clone.timeline = append([]domain.TimelineEvent(nil), s.timeline...)
	clone.outbox = append([]domain.OutboxEvent(nil), s.outbox...)
	return clone
}

func (s fakeState) counts() fakeCounts {
	return fakeCounts{len(s.incidents), len(s.signals), len(s.timeline), len(s.outbox)}
}

func onlyIncident(state fakeState) domain.Incident {
	for _, item := range state.incidents {
		return item
	}
	return domain.Incident{}
}

type fakeUnitOfWork struct {
	mu     sync.Mutex
	state  fakeState
	failAt string
}

func newFakeUnitOfWork() *fakeUnitOfWork { return &fakeUnitOfWork{state: newFakeState()} }

func (u *fakeUnitOfWork) snapshot() fakeState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state.clone()
}

func (u *fakeUnitOfWork) WithinTransaction(_ context.Context, work func(domain.Repositories) error) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	before := u.state.clone()
	repos := u.repositories()
	if err := work(repos); err != nil {
		u.state = before
		return err
	}
	return nil
}

func (u *fakeUnitOfWork) ReadRepositories() domain.Repositories { return u.repositories() }

func (u *fakeUnitOfWork) repositories() domain.Repositories {
	return domain.Repositories{Incidents: fakeIncidents{u}, Signals: fakeSignals{u}, Timeline: fakeTimeline{u}, Evidence: fakeEvidence{}, AgentRuns: fakeAgentRuns{}, AgentSteps: fakeAgentSteps{}, Outbox: fakeOutbox{u}, Correlations: fakeLocks{}}
}

type fakeIncidents struct{ u *fakeUnitOfWork }

func (r fakeIncidents) Create(_ context.Context, item *domain.Incident) error {
	item.ID = r.u.state.nextID
	r.u.state.nextID++
	r.u.state.incidents[item.ID] = *item
	return nil
}
func (r fakeIncidents) Update(_ context.Context, item *domain.Incident, expected uint64) error {
	if r.u.failAt == "incident_update_conflict" {
		return domain.ErrConflict
	}
	current, ok := r.u.state.incidents[item.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if current.Version != expected {
		return domain.ErrConflict
	}
	r.u.state.incidents[item.ID] = *item
	return nil
}
func (r fakeIncidents) FindOpenByFingerprint(_ context.Context, fingerprint string, since time.Time) (*domain.Incident, error) {
	return r.find(func(item domain.Incident) bool {
		return item.Fingerprint == fingerprint && open(item.Status) && within(item, since)
	})
}
func (r fakeIncidents) FindOpenByCorrelationKey(_ context.Context, key string, since time.Time) (*domain.Incident, error) {
	return r.find(func(item domain.Incident) bool {
		return item.CorrelationKey == key && open(item.Status) && within(item, since)
	})
}
func (r fakeIncidents) FindRecentResolvedByCorrelationKey(_ context.Context, key string, since time.Time) (*domain.Incident, error) {
	return r.find(func(item domain.Incident) bool {
		return item.CorrelationKey == key && item.Status == domain.StatusResolved && within(item, since)
	})
}
func (r fakeIncidents) find(match func(domain.Incident) bool) (*domain.Incident, error) {
	for _, item := range r.u.state.incidents {
		if match(item) {
			copy := item
			return &copy, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r fakeIncidents) GetByPublicID(_ context.Context, publicID string) (*domain.Incident, error) {
	return r.find(func(item domain.Incident) bool { return item.PublicID == publicID })
}
func (r fakeIncidents) List(_ context.Context, filter domain.ListFilter) (domain.Page, error) {
	items := make([]domain.Incident, 0)
	for _, item := range r.u.state.incidents {
		if (filter.Status == "" || item.Status == filter.Status) && (filter.Severity == "" || item.Severity == filter.Severity) {
			items = append(items, item)
		}
	}
	return domain.Page{Items: items, Total: int64(len(items)), Page: filter.Page, PageSize: filter.PageSize}, nil
}

func open(status domain.Status) bool {
	return status != domain.StatusResolved && status != domain.StatusFailed && status != domain.StatusClosedNoAction
}
func within(item domain.Incident, since time.Time) bool {
	return since.IsZero() || !item.LastSeenAt.Before(since)
}

type fakeSignals struct{ u *fakeUnitOfWork }

func (r fakeSignals) CreateIfAbsent(_ context.Context, item *domain.Signal) (bool, error) {
	key := item.Source + "/" + item.SourceEventID
	if _, ok := r.u.state.signals[key]; ok {
		return false, nil
	}
	item.ID = r.u.state.nextID
	r.u.state.nextID++
	r.u.state.signals[key] = *item
	return true, nil
}
func (r fakeSignals) AttachToIncident(_ context.Context, signalID, incidentID uint64) error {
	for key, item := range r.u.state.signals {
		if item.ID == signalID {
			item.IncidentID = &incidentID
			r.u.state.signals[key] = item
			return nil
		}
	}
	return domain.ErrNotFound
}
func (r fakeSignals) ListByIncident(_ context.Context, incidentID uint64, _ int) ([]domain.Signal, error) {
	var items []domain.Signal
	for _, item := range r.u.state.signals {
		if item.IncidentID != nil && *item.IncidentID == incidentID {
			items = append(items, item)
		}
	}
	return items, nil
}

type fakeTimeline struct{ u *fakeUnitOfWork }

func (r fakeTimeline) Append(_ context.Context, item *domain.TimelineEvent) error {
	item.ID = r.u.state.nextID
	r.u.state.nextID++
	r.u.state.timeline = append(r.u.state.timeline, *item)
	return nil
}
func (r fakeTimeline) ListByIncident(_ context.Context, incidentID uint64, _ int) ([]domain.TimelineEvent, error) {
	var items []domain.TimelineEvent
	for _, item := range r.u.state.timeline {
		if item.IncidentID == incidentID {
			items = append(items, item)
		}
	}
	return items, nil
}

type fakeOutbox struct{ u *fakeUnitOfWork }

func (r fakeOutbox) Add(_ context.Context, item *domain.OutboxEvent) error {
	if r.u.failAt == "outbox" {
		return errors.New("injected outbox failure")
	}
	item.ID = r.u.state.nextID
	r.u.state.nextID++
	r.u.state.outbox = append(r.u.state.outbox, *item)
	return nil
}
func (r fakeOutbox) PendingCount(context.Context) (int64, error) {
	return int64(len(r.u.state.outbox)), nil
}

type fakeLocks struct{}

func (fakeLocks) Lock(context.Context, string, time.Time) error { return nil }

type fakeEvidence struct{}

func (fakeEvidence) Create(context.Context, *domain.EvidenceItem) error { return nil }
func (fakeEvidence) ListByIncident(context.Context, uint64, int) ([]domain.EvidenceItem, error) {
	return nil, nil
}

type fakeAgentRuns struct{}

func (fakeAgentRuns) Create(context.Context, *domain.AgentRun) error { return nil }
func (fakeAgentRuns) GetByPublicID(context.Context, string) (*domain.AgentRun, error) {
	return nil, domain.ErrNotFound
}
func (fakeAgentRuns) Transition(context.Context, uint64, domain.AgentRunStatus, domain.AgentRunStatus, time.Time) error {
	return nil
}

type fakeAgentSteps struct{}

func (fakeAgentSteps) Create(context.Context, *domain.AgentStep) error { return nil }
func (fakeAgentSteps) ListByRun(context.Context, uint64, int) ([]domain.AgentStep, error) {
	return nil, nil
}
