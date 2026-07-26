// Package settings owns immutable Operational Configuration and its active pointer.
package settings

import (
	"errors"
	"time"
)

var (
	ErrInvalidDraft      = errors.New("operational configuration draft is invalid")
	ErrValidationFailed  = errors.New("operational configuration validation failed")
	ErrValidationExpired = errors.New("operational configuration validation expired")
	ErrValidationStale   = errors.New("operational configuration validation does not match the draft")
	ErrNotFound          = errors.New("settings resource not found")
	ErrUnavailable       = errors.New("settings dependency unavailable")
)

type Provider string

const (
	ProviderLLM           Provider = "llm"
	ProviderKubernetes    Provider = "kubernetes"
	ProviderPrometheus    Provider = "prometheus"
	ProviderAlertmanager  Provider = "alertmanager"
	ProviderElasticsearch Provider = "elasticsearch"
	ProviderTempo         Provider = "tempo"
	ProviderGitHub        Provider = "github"
	ProviderArgoCD        Provider = "argocd"
	ProviderMySQL         Provider = "mysql"
)

var operationalProviders = [...]Provider{
	ProviderLLM,
	ProviderKubernetes,
	ProviderPrometheus,
	ProviderAlertmanager,
	ProviderElasticsearch,
	ProviderTempo,
	ProviderGitHub,
	ProviderArgoCD,
}

func OperationalProviders() []Provider {
	result := make([]Provider, len(operationalProviders))
	copy(result, operationalProviders[:])
	return result
}

func (p Provider) Operational() bool {
	for _, candidate := range operationalProviders {
		if p == candidate {
			return true
		}
	}
	return false
}

type GeneralConfiguration struct {
	QueryMaxLookbackSeconds     int  `json:"query_max_lookback_seconds"`
	QueryMaxResults             int  `json:"query_max_results"`
	TelemetryRetentionDays      int  `json:"telemetry_retention_days"`
	BrowserNotificationsEnabled bool `json:"browser_notifications_enabled"`
	AutomaticEscalationEnabled  bool `json:"automatic_escalation_enabled"`
}

type OperationalScope struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	ClusterID    string   `json:"cluster_id"`
	Environment  string   `json:"environment"`
	Namespaces   []string `json:"namespaces"`
	RevisionID   string   `json:"configuration_revision_id,omitempty"`
	RevisionHash string   `json:"configuration_revision_hash,omitempty"`
}

type ProviderConfiguration struct {
	Provider        Provider `json:"provider"`
	Enabled         bool     `json:"enabled"`
	Endpoint        string   `json:"endpoint"`
	Model           string   `json:"model"`
	TimeoutMS       int      `json:"timeout_ms"`
	MaxResults      int      `json:"max_results"`
	ContextLinkBase string   `json:"context_link_base"`
}

type SecretReference struct {
	Provider        Provider `json:"provider"`
	Purpose         string   `json:"purpose"`
	SecretVersionID string   `json:"secret_version_id"`
	State           string   `json:"state,omitempty"`
	Fingerprint     string   `json:"fingerprint,omitempty"`
}

type Draft struct {
	Summary    string                  `json:"summary"`
	General    GeneralConfiguration    `json:"general"`
	Scope      OperationalScope        `json:"scope"`
	Providers  []ProviderConfiguration `json:"providers"`
	SecretRefs []SecretReference       `json:"secret_references"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProviderResult struct {
	Provider  Provider   `json:"provider"`
	State     string     `json:"state"`
	Detail    string     `json:"detail"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

type Validation struct {
	ID              string           `json:"id"`
	DraftHash       string           `json:"draft_hash"`
	Valid           bool             `json:"valid"`
	Errors          []FieldError     `json:"errors"`
	ProviderResults []ProviderResult `json:"provider_results"`
	CreatedAt       time.Time        `json:"created_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
}

type Revision struct {
	ID             string                  `json:"id"`
	Number         uint64                  `json:"number"`
	Hash           string                  `json:"hash"`
	Summary        string                  `json:"summary"`
	General        GeneralConfiguration    `json:"general"`
	Scope          OperationalScope        `json:"scope"`
	Providers      []ProviderConfiguration `json:"providers"`
	SecretRefs     []SecretReference       `json:"secret_references"`
	CreatedBy      string                  `json:"created_by"`
	CreatedAt      time.Time               `json:"created_at"`
	Active         bool                    `json:"active"`
	WorkerBoundary *ActivationStatus       `json:"worker_boundary,omitempty"`
}

type SecretVersion struct {
	ID           string    `json:"id"`
	Provider     Provider  `json:"provider"`
	Purpose      string    `json:"purpose"`
	State        string    `json:"state"`
	Fingerprint  string    `json:"fingerprint"`
	CreatedAt    time.Time `json:"created_at"`
	ReferencedBy []string  `json:"referenced_by"`
}

type SecretInput struct {
	Provider Provider `json:"provider"`
	Purpose  string   `json:"purpose"`
	Value    string   `json:"value"`
}

type ActivationStatus struct {
	TaskID       string     `json:"task_id"`
	RevisionID   string     `json:"revision_id"`
	Status       string     `json:"status"`
	WorkerID     string     `json:"worker_id,omitempty"`
	ObservedHash string     `json:"observed_hash,omitempty"`
	ObservedAt   *time.Time `json:"observed_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

type ProviderHealth struct {
	Provider   Provider   `json:"provider"`
	RevisionID string     `json:"configuration_revision_id,omitempty"`
	State      string     `json:"state"`
	Detail     string     `json:"detail"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type BootstrapDiagnostics struct {
	ListenBoundary         string `json:"listen_boundary"`
	MySQLDatabase          string `json:"mysql_database"`
	DataDirectory          string `json:"data_directory"`
	WorkerManagementTarget string `json:"worker_management_target"`
	Lifecycle              string `json:"lifecycle"`
}

type SettingsSnapshot struct {
	Bootstrap      BootstrapDiagnostics `json:"bootstrap"`
	ActiveRevision Revision             `json:"active_revision"`
	History        []Revision           `json:"history"`
	ProviderHealth []ProviderHealth     `json:"provider_health"`
}

type StorageStatus struct {
	DatabaseTables         int        `json:"database_tables"`
	ConfigurationCount     int        `json:"configuration_count"`
	NotificationCount      int        `json:"notification_count"`
	SecretVersionCount     int        `json:"secret_version_count"`
	DataCapacityBytes      uint64     `json:"data_capacity_bytes"`
	DataAvailableBytes     uint64     `json:"data_available_bytes"`
	LatestBackupName       string     `json:"latest_backup_name,omitempty"`
	LatestBackupAt         *time.Time `json:"latest_backup_at,omitempty"`
	TelemetryRetentionDays int        `json:"telemetry_retention_days"`
}

type BootstrapSnapshot struct {
	Product        string           `json:"product"`
	Contract       string           `json:"contract"`
	ActiveRevision Revision         `json:"active_revision"`
	ActiveScope    OperationalScope `json:"active_scope"`
	ProviderHealth []ProviderHealth `json:"provider_health"`
	ScenarioState  string           `json:"scenario_state"`
	Capabilities   []string         `json:"capabilities"`
	CollectedAt    time.Time        `json:"collected_at"`
}
