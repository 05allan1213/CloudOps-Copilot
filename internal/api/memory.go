package api

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// MemoryQueryPort is a deterministic, read-only projection used by the API
// skeleton and contract tests. It has no database or external-system access.
type MemoryQueryPort struct {
	mu        sync.RWMutex
	incidents map[string]IncidentView
	children  map[string]map[QueryKind][]ResourceView
	plans     map[string][]RemediationPlanView
	delivery  map[string]DeliveryView
	verify    map[string][]VerificationRunView
	reports   map[string]ResolutionReportView
	events    map[string][]RefreshEvent
}

func NewMemoryQueryPort() *MemoryQueryPort {
	return &MemoryQueryPort{
		incidents: make(map[string]IncidentView),
		children:  make(map[string]map[QueryKind][]ResourceView),
		plans:     make(map[string][]RemediationPlanView),
		delivery:  make(map[string]DeliveryView),
		verify:    make(map[string][]VerificationRunView),
		reports:   make(map[string]ResolutionReportView),
		events:    make(map[string][]RefreshEvent),
	}
}

// PutIncident is intended for local projection fixtures only. It rejects
// numeric/non-canonical public identifiers at the boundary.
func (m *MemoryQueryPort) PutIncident(item IncidentView) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil {
		return err
	}
	item.ID = id
	if item.Cycle == 0 {
		item.Cycle = 1
	}
	if item.Version == 0 {
		item.Version = 1
	}
	if err := validateIncidentView(&item); err != nil {
		return err
	}
	m.mu.Lock()
	if m.incidents == nil {
		m.incidents = make(map[string]IncidentView)
	}
	m.incidents[id] = item
	m.mu.Unlock()
	return nil
}

func (m *MemoryQueryPort) PutChildren(incidentID string, kind QueryKind, items []ResourceView) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(incidentID)
	if err != nil {
		return err
	}
	if !isChildKind(kind) {
		return ErrInvalidArgument
	}
	copyItems := make([]ResourceView, len(items))
	copy(copyItems, items)
	for index := range copyItems {
		childID, childErr := ParsePublicUUID(copyItems[index].ID)
		if childErr != nil {
			return childErr
		}
		copyItems[index].ID = childID
	}
	m.mu.Lock()
	if m.children == nil {
		m.children = make(map[string]map[QueryKind][]ResourceView)
	}
	if m.children[id] == nil {
		m.children[id] = make(map[QueryKind][]ResourceView)
	}
	m.children[id][kind] = copyItems
	m.mu.Unlock()
	return nil
}

func (m *MemoryQueryPort) PutEvents(incidentID string, events []RefreshEvent) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(incidentID)
	if err != nil {
		return err
	}
	copyEvents := make([]RefreshEvent, len(events))
	copy(copyEvents, events)
	for index := range copyEvents {
		copyEvents[index].IncidentID = id
		if copyEvents[index].Cursor == "" {
			copyEvents[index].Cursor = uuid.NewString()
		}
	}
	m.mu.Lock()
	if m.events == nil {
		m.events = make(map[string][]RefreshEvent)
	}
	m.events[id] = copyEvents
	m.mu.Unlock()
	return nil
}

func (m *MemoryQueryPort) PutResolutionReport(incidentID string, item ResolutionReportView) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(incidentID)
	if err != nil {
		return err
	}
	if err := validateResolutionReportView(&item); err != nil {
		return err
	}
	m.mu.Lock()
	if m.reports == nil {
		m.reports = make(map[string]ResolutionReportView)
	}
	m.reports[id] = *copyResolutionReport(&item)
	m.mu.Unlock()
	return nil
}

func (m *MemoryQueryPort) Query(_ context.Context, request QueryRequest) (QueryResponse, error) {
	if m == nil {
		return QueryResponse{}, ErrUnavailable
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if request.Kind == QueryIncidents {
		items := make([]IncidentView, 0, len(m.incidents))
		for _, item := range m.incidents {
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return QueryResponse{Incidents: items}, nil
	}

	incident, ok := m.incidents[request.IncidentID]
	if !ok {
		return QueryResponse{}, ErrNotFound
	}
	if request.Kind == QueryIncident {
		return QueryResponse{Incident: copyIncident(&incident)}, nil
	}
	if request.Kind == QueryEvents {
		events := append([]RefreshEvent(nil), m.events[request.IncidentID]...)
		return QueryResponse{Events: events}, nil
	}
	if request.Kind == QueryResolutionReport {
		item, ok := m.reports[request.IncidentID]
		if !ok {
			return QueryResponse{}, nil
		}
		return QueryResponse{ResolutionReport: copyResolutionReport(&item)}, nil
	}
	if request.Kind == QueryRemediationPlans {
		return QueryResponse{RemediationPlans: copyRemediationPlans(m.plans[request.IncidentID])}, nil
	}
	if request.Kind == QueryDelivery {
		item, ok := m.delivery[request.IncidentID]
		if !ok {
			return QueryResponse{}, nil
		}
		return QueryResponse{Delivery: copyDelivery(&item)}, nil
	}
	if request.Kind == QueryVerifications {
		return QueryResponse{Verifications: copyVerificationRuns(m.verify[request.IncidentID])}, nil
	}

	children := m.children[request.IncidentID]
	items := append([]ResourceView(nil), children[request.Kind]...)
	return QueryResponse{Items: items}, nil
}

func copyIncident(item *IncidentView) *IncidentView {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func copyResolutionReport(item *ResolutionReportView) *ResolutionReportView {
	if item == nil {
		return nil
	}
	copyItem := *item
	copyItem.TriggerSignal = append([]byte(nil), item.TriggerSignal...)
	copyItem.Diagnosis = append([]byte(nil), item.Diagnosis...)
	copyItem.Evidence = append([]byte(nil), item.Evidence...)
	copyItem.RemediationPlan = append([]byte(nil), item.RemediationPlan...)
	copyItem.RemediationDecision = append([]byte(nil), item.RemediationDecision...)
	copyItem.Delivery = append([]byte(nil), item.Delivery...)
	copyItem.Verification = append([]byte(nil), item.Verification...)
	copyItem.Timeline = append([]byte(nil), item.Timeline...)
	copyItem.AgentUsage = append([]byte(nil), item.AgentUsage...)
	return &copyItem
}

// UnsupportedQueryPort is the safe runtime default until a MySQL projection
// adapter is explicitly wired. It prevents an unconfigured API from claiming
// that an empty in-memory result is authoritative durable state.
type UnsupportedQueryPort struct{}

func (UnsupportedQueryPort) Query(context.Context, QueryRequest) (QueryResponse, error) {
	return QueryResponse{}, ErrNotImplemented
}

// UnsupportedCommandPort is the safe read-only default. No domain
// command is silently acknowledged before its transactional implementation is
// wired by a later phase.
type UnsupportedCommandPort struct{}

func (UnsupportedCommandPort) Execute(context.Context, CommandRequest) (CommandResult, error) {
	return CommandResult{}, ErrNotImplemented
}

// MemoryCommandPort is useful for contract tests and local API composition. It
// records no external side effect; it only returns a deterministic 202 shape.
type MemoryCommandPort struct {
	mu       sync.Mutex
	requests []CommandRequest
}

func NewMemoryCommandPort() *MemoryCommandPort {
	return &MemoryCommandPort{}
}

func (m *MemoryCommandPort) Execute(_ context.Context, request CommandRequest) (CommandResult, error) {
	if m == nil {
		return CommandResult{}, ErrUnavailable
	}
	if _, err := ParsePublicUUID(request.ResourceID); err != nil {
		return CommandResult{}, err
	}
	m.mu.Lock()
	m.requests = append(m.requests, request)
	m.mu.Unlock()
	return CommandResult{
		HTTPStatus: httpStatusAccepted,
		ResourceID: request.ResourceID,
		Status:     "accepted",
		Version:    request.ExpectedVersion + 1,
	}, nil
}

func (m *MemoryCommandPort) Requests() []CommandRequest {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]CommandRequest, len(m.requests))
	copy(result, m.requests)
	return result
}

const httpStatusAccepted = 202

func isChildKind(kind QueryKind) bool {
	switch kind {
	case QuerySignals, QueryTimeline, QueryEvidence, QueryInvestigations:
		return true
	default:
		return false
	}
}
