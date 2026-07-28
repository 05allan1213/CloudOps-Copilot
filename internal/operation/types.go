// Package operation owns execution of exact, authorized Agent actions. The
// immutable proposal and authorization remain owned by package agent.
package operation

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	SubjectActionCard    = "action_card"
	SubjectOperationPlan = "operation_plan"

	ActionSetChangeFreeze = "local.change_freeze.set"
	ActionScaleDeployment = "kubernetes.deployment.scale"
)

var (
	ErrInvalidArgument     = errors.New("invalid operation argument")
	ErrNotFound            = errors.New("operation resource not found")
	ErrConflict            = errors.New("operation state conflict")
	ErrUnauthorized        = errors.New("exact action authorization is required")
	ErrExpired             = errors.New("operation authorization or plan expired")
	ErrRevisionChanged     = errors.New("operational configuration revision changed")
	ErrPreconditionFailed  = errors.New("operation precondition failed")
	ErrProviderUnavailable = errors.New("operation provider unavailable")
	ErrLeaseLost           = errors.New("operation execution lease lost")
)

type ExecuteRequest struct {
	ExpectedHash string `json:"expected_hash"`
}

type ContextLink struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Href   string `json:"href"`
	Status string `json:"status,omitempty"`
}

type Execution struct {
	ID                      string                   `json:"id"`
	SubjectType             string                   `json:"subject_type"`
	SubjectID               string                   `json:"subject_id"`
	RunID                   string                   `json:"run_id"`
	IncidentID              string                   `json:"incident_id,omitempty"`
	ConfigurationRevisionID string                   `json:"configuration_revision_id"`
	OperationType           string                   `json:"operation_type"`
	ExpectedContentHash     string                   `json:"expected_content_hash"`
	Status                  string                   `json:"status"`
	Attempt                 uint32                   `json:"attempt"`
	ExternalEffectStartedAt *time.Time               `json:"external_effect_started_at,omitempty"`
	Result                  json.RawMessage          `json:"result,omitempty"`
	FailureCode             string                   `json:"failure_code,omitempty"`
	FailureSummary          string                   `json:"failure_summary,omitempty"`
	CreatedAt               time.Time                `json:"created_at"`
	StartedAt               *time.Time               `json:"started_at,omitempty"`
	CompletedAt             *time.Time               `json:"completed_at,omitempty"`
	Events                  []AuditEvent             `json:"events"`
	Verification            *VerificationObservation `json:"verification,omitempty"`
	Links                   []ContextLink            `json:"links"`
}

type AuditEvent struct {
	ID          string          `json:"id"`
	Sequence    uint32          `json:"sequence"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	ContentHash string          `json:"content_hash"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

type VerificationObservation struct {
	ID               string          `json:"id"`
	Source           string          `json:"source"`
	Status           string          `json:"status"`
	ProviderIdentity json.RawMessage `json:"provider_identity"`
	Evidence         json.RawMessage `json:"evidence"`
	ContentHash      string          `json:"content_hash"`
	Summary          string          `json:"summary"`
	ObservedAt       time.Time       `json:"observed_at"`
}

type Lease struct {
	ExecutionID       uint64
	ExecutionPublicID string
	Owner             string
	Generation        uint64
	Attempt           uint32
}

// Subject is reconstructed from the immutable Agent tables for every
// execution attempt. No command payload supplied to the execute endpoint is
// trusted as an effect parameter.
type Subject struct {
	ExecutionInternalID             uint64
	ExecutionID                     string
	SubjectInternalID               uint64
	AuthorizationInternalID         uint64
	ConfigurationRevisionInternalID uint64
	SubjectType                     string
	SubjectID                       string
	RunID                           string
	IncidentID                      string
	ConfigurationRevisionID         string
	Authority                       string
	OperationType                   string
	Target                          json.RawMessage
	Parameters                      json.RawMessage
	IntendedState                   json.RawMessage
	Preconditions                   json.RawMessage
	Risk                            string
	VerificationIntent              json.RawMessage
	ContentHash                     string
	ExpectedContentHash             string
	Status                          string
	AuthorizationID                 string
	AuthorizedHash                  string
	ExpiresAt                       time.Time
	AuthorizationExpiresAt          time.Time
}

type PreparedEffect struct {
	External bool
	Before   Observation
	Token    json.RawMessage
}

type Observation struct {
	Source           string
	ProviderIdentity json.RawMessage
	Evidence         json.RawMessage
	Verified         bool
	Summary          string
	ObservedAt       time.Time
}

// Adapter exposes one typed mutation surface. Prepare performs bounded reads
// and precondition checks; Apply performs only the already prepared effect and
// returns a current post-effect observation.
type Adapter interface {
	OperationType() string
	Prepare(context.Context, Subject) (PreparedEffect, error)
	Apply(context.Context, Subject, PreparedEffect) (Observation, error)
}

type AdapterRegistry struct {
	items map[string]Adapter
}

func NewAdapterRegistry(adapters ...Adapter) (*AdapterRegistry, error) {
	result := &AdapterRegistry{items: make(map[string]Adapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil || adapter.OperationType() == "" {
			return nil, ErrInvalidArgument
		}
		if _, exists := result.items[adapter.OperationType()]; exists {
			return nil, ErrConflict
		}
		result.items[adapter.OperationType()] = adapter
	}
	return result, nil
}

func (r *AdapterRegistry) Resolve(operationType string) (Adapter, error) {
	if r == nil {
		return nil, ErrProviderUnavailable
	}
	adapter := r.items[operationType]
	if adapter == nil {
		return nil, ErrProviderUnavailable
	}
	return adapter, nil
}

type OperationTarget struct {
	ClusterID    string `json:"cluster_id"`
	Environment  string `json:"environment"`
	Namespace    string `json:"namespace"`
	WorkloadKind string `json:"workload_kind"`
	WorkloadName string `json:"workload_name"`
	ScenarioID   string `json:"scenario_id,omitempty"`
}

type ChangeFreezeState struct {
	Target     OperationTarget `json:"target"`
	Enabled    bool            `json:"enabled"`
	Reason     string          `json:"reason"`
	RowVersion uint64          `json:"row_version"`
	UpdatedAt  *time.Time      `json:"updated_at,omitempty"`
}

type ChangeFreezeReader interface {
	ChangeFreeze(context.Context, OperationTarget) (ChangeFreezeState, error)
}
