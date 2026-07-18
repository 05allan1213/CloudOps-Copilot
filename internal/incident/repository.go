package incident

import (
	"context"
	"time"
)

// IncidentRepository persists and queries Incident aggregates.
type IncidentRepository interface {
	Create(context.Context, *Incident) error
	Update(context.Context, *Incident, uint64) error
	FindOpenByFingerprint(context.Context, string, time.Time) (*Incident, error)
	FindOpenByCorrelationKey(context.Context, string, time.Time) (*Incident, error)
	FindRecentResolvedByCorrelationKey(context.Context, string, time.Time) (*Incident, error)
	GetByPublicID(context.Context, string) (*Incident, error)
	List(context.Context, ListFilter) (Page, error)
}

// SignalRepository persists idempotent normalized signals.
type SignalRepository interface {
	CreateIfAbsent(context.Context, *Signal) (bool, error)
	AttachToIncident(context.Context, uint64, uint64) error
	ListByIncident(context.Context, uint64, int) ([]Signal, error)
}

// TimelineRepository persists typed Incident domain facts.
type TimelineRepository interface {
	Append(context.Context, *TimelineEvent) error
	ListByIncident(context.Context, uint64, int) ([]TimelineEvent, error)
}

// EvidenceRepository persists bounded evidence summaries and references.
type EvidenceRepository interface {
	Create(context.Context, *EvidenceItem) error
	ListByIncident(context.Context, uint64, int) ([]EvidenceItem, error)
}

// AgentRunRepository persists the future Agent execution contract.
type AgentRunRepository interface {
	Create(context.Context, *AgentRun) error
	GetByPublicID(context.Context, string) (*AgentRun, error)
	Transition(context.Context, uint64, AgentRunStatus, AgentRunStatus, time.Time) error
}

// AgentStepRepository persists bounded future Agent step summaries.
type AgentStepRepository interface {
	Create(context.Context, *AgentStep) error
	ListByRun(context.Context, uint64, int) ([]AgentStep, error)
}

// OutboxRepository stores events atomically with aggregate changes.
type OutboxRepository interface {
	Add(context.Context, *OutboxEvent) error
	PendingCount(context.Context) (int64, error)
}

// CorrelationLocker serializes concurrent aggregation for one correlation key.
type CorrelationLocker interface {
	Lock(context.Context, string, time.Time) error
}

// Repositories is the transaction-scoped set of Incident persistence ports.
type Repositories struct {
	Incidents    IncidentRepository
	Signals      SignalRepository
	Timeline     TimelineRepository
	Evidence     EvidenceRepository
	AgentRuns    AgentRunRepository
	AgentSteps   AgentStepRepository
	Outbox       OutboxRepository
	Correlations CorrelationLocker
}

// UnitOfWork provides one explicit transaction across Incident repositories.
type UnitOfWork interface {
	WithinTransaction(context.Context, func(Repositories) error) error
	ReadRepositories() Repositories
}
