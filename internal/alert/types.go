// Package alert owns the Signal-to-Alert lifecycle, triage actions, and
// explicit Alert-to-Incident relationships.
package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

var (
	ErrInvalid             = errors.New("invalid Alert operation")
	ErrNotFound            = errors.New("alert resource not found")
	ErrConflict            = errors.New("alert operation conflicts with current state")
	ErrStaleVersion        = errors.New("alert expected version is stale")
	ErrProviderDisabled    = errors.New("alertmanager provider is disabled")
	ErrProviderUnavailable = errors.New("alertmanager provider is unavailable")
)

const (
	MinimumSilenceDuration = 5 * time.Minute
	MaximumSilenceDuration = 24 * time.Hour
)

// SignalInput is a validated, allowlist-resolved immutable source fact.
type SignalInput struct {
	Source           string
	SourceEventID    string
	AlertInstanceKey string
	CorrelationKey   string
	Fingerprint      string
	Status           domain.SignalStatus
	Severity         domain.Severity
	Cluster          string
	Environment      string
	Namespace        string
	ServiceName      string
	TargetKind       string
	TargetName       string
	Category         string
	StartsAt         time.Time
	EndsAt           *time.Time
	OccurredAt       time.Time
	Summary          string
	Labels           json.RawMessage
	Annotations      json.RawMessage
}

type IngestResult struct {
	SourceEventID    string
	AlertPublicID    string
	IncidentPublicID string
	Duplicate        bool
	Rejected         bool
	RejectionReason  string
}

type Actor struct {
	Provider string
	Login    string
	Role     string
}

type Acknowledgement struct {
	ID           string    `json:"id"`
	RecurrenceNo uint64    `json:"recurrence_no"`
	AlertVersion uint64    `json:"alert_version"`
	Actor        Actor     `json:"actor"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}

type Matcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"is_regex"`
	IsEqual bool   `json:"is_equal"`
}

type Silence struct {
	ID                      string     `json:"id"`
	ProviderSilenceID       string     `json:"provider_silence_id,omitempty"`
	Status                  string     `json:"status"`
	Matchers                []Matcher  `json:"matchers"`
	Reason                  string     `json:"reason"`
	ConfigurationRevisionID string     `json:"configuration_revision_id"`
	StartsAt                time.Time  `json:"starts_at"`
	EndsAt                  time.Time  `json:"ends_at"`
	ExpiredAt               *time.Time `json:"expired_at,omitempty"`
	ProviderErrorCode       string     `json:"provider_error_code,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

type IncidentLink struct {
	ID                      string    `json:"id"`
	IncidentID              string    `json:"incident_id"`
	IncidentCycle           uint64    `json:"incident_cycle"`
	IncidentStatus          string    `json:"incident_status"`
	Provenance              string    `json:"provenance"`
	ConfigurationRevisionID string    `json:"configuration_revision_id,omitempty"`
	EscalationPolicyID      string    `json:"escalation_policy_id,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
}

type InvestigationLink struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incident_id,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type ContextLink struct {
	Workspace          string            `json:"workspace"`
	Path               string            `json:"path"`
	Query              map[string]string `json:"query"`
	OperationalScopeID string            `json:"operational_scope_id"`
	External           bool              `json:"external"`
}

type View struct {
	ID                    string              `json:"id"`
	Status                string              `json:"status"`
	Severity              string              `json:"severity"`
	Summary               string              `json:"summary"`
	Category              string              `json:"category"`
	Source                string              `json:"source"`
	Fingerprint           string              `json:"fingerprint"`
	CorrelationKey        string              `json:"correlation_key"`
	Cluster               string              `json:"cluster"`
	Environment           string              `json:"environment"`
	Namespace             string              `json:"namespace"`
	ServiceName           string              `json:"service_name"`
	TargetKind            string              `json:"target_kind"`
	TargetName            string              `json:"target_name"`
	FirstSeenAt           time.Time           `json:"first_seen_at"`
	LastSeenAt            time.Time           `json:"last_seen_at"`
	StartsAt              time.Time           `json:"starts_at"`
	ResolvedAt            *time.Time          `json:"resolved_at,omitempty"`
	RecurrenceCount       uint64              `json:"recurrence_count"`
	SignalCount           uint64              `json:"signal_count"`
	Version               uint64              `json:"version"`
	Acknowledgement       *Acknowledgement    `json:"acknowledgement,omitempty"`
	Silence               *Silence            `json:"silence,omitempty"`
	IncidentLinks         []IncidentLink      `json:"incident_links"`
	Investigations        []InvestigationLink `json:"investigations"`
	ContextLink           ContextLink         `json:"context_link"`
	MigratedLegacy        bool                `json:"migrated_legacy"`
	MigratedLegacyContext bool                `json:"migrated_legacy_context"`
	CreatedAt             time.Time           `json:"created_at"`
	UpdatedAt             time.Time           `json:"updated_at"`
}

type SignalView struct {
	ID               string          `json:"id"`
	SourceEventID    string          `json:"source_event_id"`
	AlertInstanceKey string          `json:"alert_instance_key"`
	Status           string          `json:"status"`
	Severity         string          `json:"severity"`
	Summary          string          `json:"summary"`
	Labels           json.RawMessage `json:"labels"`
	Annotations      json.RawMessage `json:"annotations"`
	StartsAt         time.Time       `json:"starts_at"`
	EndsAt           *time.Time      `json:"ends_at,omitempty"`
	OccurredAt       time.Time       `json:"occurred_at"`
	ReceivedAt       time.Time       `json:"received_at"`
	Provenance       string          `json:"provenance"`
}

type EventView struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	ActorType  string          `json:"actor_type"`
	ActorID    string          `json:"actor_id"`
	Summary    string          `json:"summary"`
	Metadata   json.RawMessage `json:"metadata"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type Detail struct {
	Alert   View         `json:"alert"`
	Signals []SignalView `json:"signals"`
	Events  []EventView  `json:"events"`
}

type Page struct {
	Items      []View `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type ListRequest struct {
	Cursor    string
	Limit     int
	Status    string
	Severity  string
	Namespace string
	Search    string
}

type AcknowledgeRequest struct {
	AlertID         string
	ExpectedVersion uint64
	IdempotencyKey  string
	Reason          string
	Actor           Actor
}

type CreateSilenceRequest struct {
	AlertID         string
	ExpectedVersion uint64
	IdempotencyKey  string
	Duration        time.Duration
	Reason          string
	Actor           Actor
}

type ExpireSilenceRequest struct {
	SilenceID       string
	ExpectedVersion uint64
	IdempotencyKey  string
	Actor           Actor
}

type LinkIncidentRequest struct {
	AlertID         string
	ExpectedVersion uint64
	IdempotencyKey  string
	IncidentID      string
	Create          bool
	Actor           Actor
}

type StartInvestigationRequest struct {
	AlertID         string
	ExpectedVersion uint64
	IdempotencyKey  string
	Reason          string
	Actor           Actor
}

type SilenceProviderRequest struct {
	ExternalID              string    `json:"external_id"`
	ConfigurationRevisionID string    `json:"configuration_revision_id"`
	Matchers                []Matcher `json:"matchers"`
	StartsAt                time.Time `json:"starts_at"`
	EndsAt                  time.Time `json:"ends_at"`
	Comment                 string    `json:"comment"`
	CreatedBy               string    `json:"created_by"`
}

type SilenceProvider interface {
	CreateSilence(context.Context, SilenceProviderRequest) (string, error)
	ExpireSilence(context.Context, string, string) error
}

type InvestigationStarter interface {
	StartAlertInvestigationTx(context.Context, *sql.Tx, string, string, string) (string, error)
}
