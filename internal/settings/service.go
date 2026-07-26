package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const validationLifetime = 10 * time.Minute

type Service struct {
	db             *sql.DB
	dataDir        string
	bootstrap      BootstrapDiagnostics
	now            func() time.Time
	httpTimeout    time.Duration
	providerProbes map[Provider]func(context.Context, string) (string, error)
}

func NewService(db *sql.DB, dataDir string, bootstrap BootstrapDiagnostics) (*Service, error) {
	if db == nil {
		return nil, errors.New("settings database is required")
	}
	dataDir = strings.TrimSpace(dataDir)
	if err := ensurePrivateDataDirectory(dataDir); err != nil {
		return nil, err
	}
	return &Service{
		db: db, dataDir: dataDir, bootstrap: bootstrap,
		now: time.Now, httpTimeout: 5 * time.Second,
		providerProbes: make(map[Provider]func(context.Context, string) (string, error)),
	}, nil
}

// SetProviderProbe installs a bounded runtime-owned connection probe during
// process assembly. It does not expose Provider credentials to Settings.
func (s *Service) SetProviderProbe(provider Provider, probe func(context.Context, string) (string, error)) {
	if s == nil || !provider.Operational() || probe == nil {
		return
	}
	s.providerProbes[provider] = probe
}

func (s *Service) Validate(ctx context.Context, input Draft) (Validation, error) {
	draft, fieldErrors, draftHash := normalizeDraft(input)
	fieldErrors = append(fieldErrors, s.validateSecretReferences(ctx, draft)...)
	providerResults := make([]ProviderResult, 0, len(draft.Providers))
	for _, provider := range draft.Providers {
		if !provider.Enabled {
			providerResults = append(providerResults, ProviderResult{
				Provider: provider.Provider, State: "disabled", Detail: "Provider 在当前草稿中未启用",
			})
			continue
		}
		result := ProviderResult{}
		var clusterErrors []FieldError
		if provider.Provider == ProviderKubernetes {
			result, clusterErrors = s.testKubernetesScopes(ctx, provider, draft.SecretRefs, draft.Scopes)
		} else {
			result = s.testProvider(ctx, provider, draft.SecretRefs, draft.Scope.ClusterID)
		}
		providerResults = append(providerResults, result)
		if result.State != "available" {
			if len(clusterErrors) > 0 {
				fieldErrors = append(fieldErrors, clusterErrors...)
			} else {
				fieldErrors = append(fieldErrors, FieldError{
					Field: "providers." + string(provider.Provider), Code: "PROVIDER_UNAVAILABLE", Message: result.Detail,
				})
			}
		}
	}
	if fieldErrors == nil {
		fieldErrors = []FieldError{}
	}
	if providerResults == nil {
		providerResults = []ProviderResult{}
	}
	now := s.now().UTC()
	validation := Validation{
		ID: uuid.NewString(), DraftHash: draftHash, Valid: len(fieldErrors) == 0,
		Errors: fieldErrors, ProviderResults: providerResults,
		CreatedAt: now, ExpiresAt: now.Add(validationLifetime),
	}
	if err := s.persistValidation(ctx, validation, draft); err != nil {
		return Validation{}, err
	}
	return validation, nil
}

func (s *Service) persistValidation(ctx context.Context, validation Validation, draft Draft) error {
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		return fmt.Errorf("encode configuration validation draft: %w", err)
	}
	errorsJSON, err := json.Marshal(validation.Errors)
	if err != nil {
		return fmt.Errorf("encode configuration validation errors: %w", err)
	}
	providersJSON, err := json.Marshal(validation.ProviderResults)
	if err != nil {
		return fmt.Errorf("encode configuration provider results: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO configuration_validations (
public_id, draft_hash, draft_json, valid, errors_json, provider_results_json,
created_by, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, 'local-owner', ?, ?)`,
		validation.ID, validation.DraftHash, draftJSON, validation.Valid, errorsJSON, providersJSON,
		validation.CreatedAt, validation.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("persist configuration validation: %w", err)
	}
	return nil
}

func (s *Service) Apply(ctx context.Context, validationID string, input Draft) (Revision, error) {
	draft, fieldErrors, draftHash := normalizeDraft(input)
	if len(fieldErrors) != 0 || draftHash == "" {
		return Revision{}, errors.Join(ErrInvalidDraft, fieldErrorsError(fieldErrors))
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Revision{}, fmt.Errorf("begin configuration apply: %w", err)
	}
	defer rollback(tx)

	var validatedHash string
	var valid bool
	var expiresAt time.Time
	var appliedRevisionPublicID string
	if err := tx.QueryRowContext(ctx, `SELECT validation.draft_hash, validation.valid,
validation.expires_at, COALESCE(revision.public_id, '')
FROM configuration_validations AS validation
LEFT JOIN configuration_revisions AS revision ON revision.id = validation.applied_revision_id
WHERE validation.public_id = ? FOR UPDATE`, strings.TrimSpace(validationID)).
		Scan(&validatedHash, &valid, &expiresAt, &appliedRevisionPublicID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Revision{}, ErrNotFound
		}
		return Revision{}, fmt.Errorf("load configuration validation: %w", err)
	}
	if !valid {
		return Revision{}, ErrValidationFailed
	}
	if validatedHash != draftHash {
		return Revision{}, ErrValidationStale
	}
	if appliedRevisionPublicID != "" {
		_ = tx.Rollback()
		return s.Revision(ctx, appliedRevisionPublicID)
	}
	if !expiresAt.After(s.now().UTC()) {
		return Revision{}, ErrValidationExpired
	}
	if errs := s.validateSecretReferencesWith(ctx, tx, draft); len(errs) != 0 {
		return Revision{}, errors.Join(ErrInvalidDraft, fieldErrorsError(errs))
	}

	var activeRevisionID uint64
	if err := tx.QueryRowContext(ctx, `SELECT configuration_revision_id
FROM active_configuration WHERE singleton_id = 1 FOR UPDATE`).Scan(&activeRevisionID); err != nil {
		return Revision{}, fmt.Errorf("lock active configuration: %w", err)
	}
	var nextNumber uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision_number), 0) + 1 FROM configuration_revisions`).Scan(&nextNumber); err != nil {
		return Revision{}, fmt.Errorf("allocate configuration revision number: %w", err)
	}
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO configuration_revisions (
public_id, revision_number, configuration_hash, summary, query_max_lookback_seconds,
query_max_results, telemetry_retention_days, browser_notifications_enabled,
automatic_escalation_enabled, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'local-owner', NOW(6))`,
		publicID, nextNumber, draftHash, draft.Summary,
		draft.General.QueryMaxLookbackSeconds, draft.General.QueryMaxResults,
		draft.General.TelemetryRetentionDays, draft.General.BrowserNotificationsEnabled,
		draft.General.AutomaticEscalationEnabled,
	)
	if err != nil {
		return Revision{}, fmt.Errorf("insert configuration revision: %w", err)
	}
	revisionID, err := result.LastInsertId()
	if err != nil || revisionID <= 0 {
		return Revision{}, fmt.Errorf("read configuration revision id: %w", err)
	}
	for _, provider := range draft.Providers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_configurations (
configuration_revision_id, provider, enabled, endpoint, model, timeout_ms, max_results, context_link_base
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			revisionID, provider.Provider, provider.Enabled, provider.Endpoint, provider.Model,
			provider.TimeoutMS, provider.MaxResults, provider.ContextLinkBase,
		); err != nil {
			return Revision{}, fmt.Errorf("insert %s provider configuration: %w", provider.Provider, err)
		}
	}
	var activeScopeID int64
	for _, scope := range draft.Scopes {
		namespacesJSON, _ := json.Marshal(scope.Namespaces)
		isDefault := scope.ClusterID == draft.Scope.ClusterID
		scopeResult, err := tx.ExecContext(ctx, `INSERT INTO operational_scopes (
	public_id, configuration_revision_id, name, cluster_id, environment, namespaces_json, is_default
	) VALUES (?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), revisionID, scope.Name, scope.ClusterID, scope.Environment, namespacesJSON, isDefault)
		if err != nil {
			return Revision{}, fmt.Errorf("insert %s operational scope: %w", scope.ClusterID, err)
		}
		if isDefault {
			activeScopeID, err = scopeResult.LastInsertId()
			if err != nil || activeScopeID <= 0 {
				return Revision{}, fmt.Errorf("read active operational scope id: %w", err)
			}
		}
	}
	if activeScopeID <= 0 {
		return Revision{}, errors.New("active operational scope was not inserted")
	}
	for _, ref := range draft.SecretRefs {
		var secretID uint64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM secret_versions
WHERE public_id = ? AND provider = ? AND purpose = ?`, ref.SecretVersionID, ref.Provider, ref.Purpose).Scan(&secretID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Revision{}, ErrNotFound
			}
			return Revision{}, fmt.Errorf("load secret reference: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO configuration_secret_references (
configuration_revision_id, provider, purpose, secret_version_id
) VALUES (?, ?, ?, ?)`, revisionID, ref.Provider, ref.Purpose, secretID); err != nil {
			return Revision{}, fmt.Errorf("insert configuration secret reference: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE active_configuration
	SET configuration_revision_id = ?, updated_at = NOW(6) WHERE singleton_id = 1`, revisionID); err != nil {
		return Revision{}, fmt.Errorf("activate configuration revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE active_operational_scope
	SET operational_scope_id = ?, updated_at = NOW(6) WHERE singleton_id = 1`, activeScopeID); err != nil {
		return Revision{}, fmt.Errorf("activate operational scope: %w", err)
	}
	for _, provider := range draft.Providers {
		state := "disabled"
		detail := "Provider is disabled in the active revision"
		var checkedAt any
		if provider.Enabled {
			state = "available"
			detail = "Provider validation passed before activation"
			checkedAt = s.now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE provider_health
SET configuration_revision_id = ?, state = ?, detail = ?, checked_at = ?, updated_at = NOW(6)
WHERE provider = ?`, revisionID, state, detail, checkedAt, provider.Provider); err != nil {
			return Revision{}, fmt.Errorf("update %s provider health: %w", provider.Provider, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE provider_health
SET configuration_revision_id = ?, state = 'available', detail = 'MySQL schema and durable configuration are available', checked_at = NOW(6), updated_at = NOW(6)
WHERE provider = 'mysql'`, revisionID); err != nil {
		return Revision{}, fmt.Errorf("update MySQL provider health: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO configuration_activation_tasks (
public_id, configuration_revision_id, status, available_at
) VALUES (?, ?, 'ready', NOW(6))`, uuid.NewString(), revisionID); err != nil {
		return Revision{}, fmt.Errorf("enqueue configuration activation: %w", err)
	}
	validationResult, err := tx.ExecContext(ctx, `UPDATE configuration_validations
SET applied_revision_id = ? WHERE public_id = ? AND applied_revision_id IS NULL`, revisionID, strings.TrimSpace(validationID))
	if err != nil {
		return Revision{}, fmt.Errorf("bind configuration validation to revision: %w", err)
	}
	if rows, _ := validationResult.RowsAffected(); rows != 1 {
		return Revision{}, ErrValidationStale
	}
	if err := tx.Commit(); err != nil {
		return Revision{}, fmt.Errorf("commit configuration apply: %w", err)
	}
	return s.Revision(ctx, publicID)
}

func (s *Service) ActivateScope(ctx context.Context, publicID string) (OperationalScope, error) {
	publicID = strings.TrimSpace(publicID)
	if len(publicID) != 36 {
		return OperationalScope{}, ErrNotFound
	}
	active, err := s.ActiveRevision(ctx)
	if err != nil {
		return OperationalScope{}, err
	}
	var candidate *OperationalScope
	for index := range active.Scopes {
		if active.Scopes[index].ID == publicID {
			value := active.Scopes[index]
			candidate = &value
			break
		}
	}
	if candidate == nil {
		return OperationalScope{}, ErrNotFound
	}
	var probeDetail string
	var probeCheckedAt *time.Time
	if kubernetesEnabled(active) {
		probe := s.providerProbes[ProviderKubernetes]
		if probe == nil {
			return OperationalScope{}, ErrUnavailable
		}
		detail, probeErr := probe(ctx, candidate.ClusterID)
		if probeErr != nil {
			return OperationalScope{}, fmt.Errorf("%w: selected Kubernetes cluster is unavailable", ErrUnavailable)
		}
		now := s.now().UTC()
		probeDetail = detail
		probeCheckedAt = &now
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return OperationalScope{}, fmt.Errorf("begin scope activation: %w", err)
	}
	defer rollback(tx)
	var scopeID uint64
	if err := tx.QueryRowContext(ctx, `SELECT scope.id
	FROM operational_scopes AS scope
	JOIN configuration_revisions AS revision ON revision.id = scope.configuration_revision_id
	JOIN active_configuration AS active ON active.singleton_id = 1 AND active.configuration_revision_id = revision.id
	WHERE scope.public_id = ? FOR UPDATE`, publicID).Scan(&scopeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OperationalScope{}, ErrNotFound
		}
		return OperationalScope{}, fmt.Errorf("lock operational scope: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE active_operational_scope
	SET operational_scope_id = ?, updated_at = NOW(6) WHERE singleton_id = 1`, scopeID); err != nil {
		return OperationalScope{}, fmt.Errorf("activate operational scope: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return OperationalScope{}, fmt.Errorf("commit operational scope activation: %w", err)
	}
	if probeCheckedAt != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE provider_health AS health
	JOIN configuration_revisions AS revision ON revision.public_id = ?
	JOIN active_configuration AS active ON active.singleton_id = 1 AND active.configuration_revision_id = revision.id
	SET health.configuration_revision_id = revision.id, health.state = 'available', health.detail = ?, health.checked_at = ?, health.updated_at = NOW(6)
	WHERE health.provider = 'kubernetes'`, active.ID, probeDetail, *probeCheckedAt)
	}
	revision, err := s.ActiveRevision(ctx)
	if err != nil {
		return OperationalScope{}, err
	}
	return revision.Scope, nil
}

func kubernetesEnabled(revision Revision) bool {
	for _, provider := range revision.Providers {
		if provider.Provider == ProviderKubernetes {
			return provider.Enabled
		}
	}
	return false
}

func fieldErrorsError(values []FieldError) error {
	if len(values) == 0 {
		return nil
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Field+": "+value.Message)
	}
	return errors.New(strings.Join(parts, "; "))
}

func (s *Service) Settings(ctx context.Context) (SettingsSnapshot, error) {
	if err := s.refreshMySQLHealth(ctx); err != nil {
		return SettingsSnapshot{}, err
	}
	active, err := s.ActiveRevision(ctx)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	history, err := s.Revisions(ctx, 20)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	health, err := s.ProviderHealth(ctx)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	return SettingsSnapshot{Bootstrap: s.bootstrap, ActiveRevision: active, History: history, ProviderHealth: health}, nil
}

func (s *Service) Bootstrap(ctx context.Context) (BootstrapSnapshot, error) {
	active, err := s.ActiveRevision(ctx)
	if err != nil {
		return BootstrapSnapshot{}, err
	}
	health, err := s.ProviderHealth(ctx)
	if err != nil {
		return BootstrapSnapshot{}, err
	}
	return BootstrapSnapshot{
		Product: "CloudOps", Contract: "V1", ActiveRevision: active, ActiveScope: active.Scope,
		ProviderHealth: health, ScenarioState: "inactive",
		Capabilities: []string{"settings", "operational_scope", "notifications", "incidents", "infrastructure", "operations_atlas"},
		CollectedAt:  s.now().UTC(),
	}, nil
}

func (s *Service) StorageStatus(ctx context.Context) (StorageStatus, error) {
	active, err := s.ActiveRevision(ctx)
	if err != nil {
		return StorageStatus{}, err
	}
	status := StorageStatus{TelemetryRetentionDays: active.General.TelemetryRetentionDays}
	if err := s.db.QueryRowContext(ctx, `SELECT
(SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()),
(SELECT COUNT(*) FROM configuration_revisions),
(SELECT COUNT(*) FROM owner_notifications),
(SELECT COUNT(*) FROM secret_versions)`).Scan(
		&status.DatabaseTables, &status.ConfigurationCount, &status.NotificationCount, &status.SecretVersionCount,
	); err != nil {
		return StorageStatus{}, fmt.Errorf("read storage counts: %w", err)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.dataDir, &stat); err == nil {
		status.DataCapacityBytes = stat.Blocks * uint64(stat.Bsize)
		status.DataAvailableBytes = stat.Bavail * uint64(stat.Bsize)
	}
	var latestAt sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT backup_name, created_at
FROM backup_records ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&status.LatestBackupName, &latestAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return StorageStatus{}, fmt.Errorf("read latest backup: %w", err)
	}
	status.LatestBackupAt = timePointer(latestAt)
	return status, nil
}

func (s *Service) ProviderHealth(ctx context.Context) ([]ProviderHealth, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT health.provider, COALESCE(revision.public_id, ''), health.state,
health.detail, health.checked_at, health.updated_at
FROM provider_health AS health
LEFT JOIN configuration_revisions AS revision ON revision.id = health.configuration_revision_id
ORDER BY FIELD(health.provider, 'mysql','kubernetes','prometheus','alertmanager','elasticsearch','tempo','llm','github','argocd')`)
	if err != nil {
		return nil, fmt.Errorf("list provider health: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ProviderHealth, 0, 9)
	for rows.Next() {
		var item ProviderHealth
		var checked sql.NullTime
		if err := rows.Scan(&item.Provider, &item.RevisionID, &item.State, &item.Detail, &checked, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider health: %w", err)
		}
		item.CheckedAt = timePointer(checked)
		item.UpdatedAt = item.UpdatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

// ObserveProviderHealth records a bounded runtime observation only while the
// referenced immutable Configuration Revision remains active.
func (s *Service) ObserveProviderHealth(ctx context.Context, revisionPublicID string, result ProviderResult) error {
	if !result.Provider.Operational() || strings.TrimSpace(revisionPublicID) == "" {
		return ErrInvalidDraft
	}
	switch result.State {
	case "available", "partial", "unavailable", "disabled", "not_configured":
	default:
		return ErrInvalidDraft
	}
	detail := strings.TrimSpace(result.Detail)
	if detail == "" || len(detail) > 512 {
		return ErrInvalidDraft
	}
	queryResult, err := s.db.ExecContext(ctx, `UPDATE provider_health AS health
JOIN configuration_revisions AS revision ON revision.public_id = ?
JOIN active_configuration AS active ON active.singleton_id = 1 AND active.configuration_revision_id = revision.id
SET health.configuration_revision_id = revision.id, health.state = ?, health.detail = ?,
health.checked_at = ?, health.updated_at = NOW(6)
WHERE health.provider = ?`, strings.TrimSpace(revisionPublicID), result.State, detail, result.CheckedAt, result.Provider)
	if err != nil {
		return fmt.Errorf("observe %s provider health: %w", result.Provider, err)
	}
	if rows, _ := queryResult.RowsAffected(); rows == 0 {
		return ErrValidationStale
	}
	return nil
}

func (s *Service) refreshMySQLHealth(ctx context.Context) error {
	state, detail := "available", "MySQL schema and durable configuration are available"
	if err := s.db.PingContext(ctx); err != nil {
		state, detail = "unavailable", "MySQL ping failed"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE provider_health AS health
JOIN active_configuration AS active ON active.singleton_id = 1
SET health.configuration_revision_id = active.configuration_revision_id,
health.state = ?, health.detail = ?, health.checked_at = NOW(6), health.updated_at = NOW(6)
WHERE health.provider = 'mysql'`, state, detail)
	if err != nil {
		return fmt.Errorf("refresh MySQL provider health: %w", err)
	}
	return nil
}

func (s *Service) TestProvider(ctx context.Context, config ProviderConfiguration, refs []SecretReference, clusterID string) (ProviderResult, error) {
	if !config.Provider.Operational() {
		return ProviderResult{}, ErrInvalidDraft
	}
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		active, err := s.ActiveRevision(ctx)
		if err != nil {
			return ProviderResult{}, err
		}
		clusterID = active.Scope.ClusterID
	}
	draft := Draft{
		Summary:   "Provider connection test",
		General:   GeneralConfiguration{QueryMaxLookbackSeconds: 3600, QueryMaxResults: 100, TelemetryRetentionDays: 7},
		Scope:     OperationalScope{Name: "Connection test", ClusterID: clusterID, Environment: "local", Namespaces: []string{"demo"}},
		Providers: []ProviderConfiguration{config}, SecretRefs: refs,
	}
	_, fieldErrors, _ := normalizeDraft(draft)
	for _, fieldError := range fieldErrors {
		if strings.HasPrefix(fieldError.Field, "providers."+string(config.Provider)) || fieldError.Field == "secret_references" {
			return ProviderResult{}, errors.Join(ErrInvalidDraft, errors.New(fieldError.Message))
		}
	}
	result := s.testProvider(ctx, config, normalizeSecretReferences(refs), draft.Scope.ClusterID)
	if result.State == "available" {
		_, _ = s.db.ExecContext(ctx, `UPDATE provider_health AS health
JOIN active_configuration AS active ON active.singleton_id = 1
SET health.configuration_revision_id = active.configuration_revision_id,
health.state = ?, health.detail = ?, health.checked_at = ?, health.updated_at = NOW(6)
WHERE health.provider = ?`, result.State, result.Detail, result.CheckedAt, result.Provider)
	}
	return result, nil
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func sortedProviders(values []ProviderConfiguration) {
	sort.Slice(values, func(i, j int) bool { return values[i].Provider < values[j].Provider })
}
