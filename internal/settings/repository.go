package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type revisionRecord struct {
	internalID uint64
	revision   Revision
}

func (s *Service) ActiveRevision(ctx context.Context) (Revision, error) {
	record, err := s.loadRevision(ctx, s.db, `revision.id = active.configuration_revision_id`)
	if err != nil {
		return Revision{}, err
	}
	return record.revision, nil
}

func (s *Service) Revision(ctx context.Context, publicID string) (Revision, error) {
	record, err := s.loadRevision(ctx, s.db, `revision.public_id = ?`, publicID)
	if err != nil {
		return Revision{}, err
	}
	return record.revision, nil
}

func (s *Service) Revisions(ctx context.Context, limit int) ([]Revision, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT revision.public_id
FROM configuration_revisions AS revision ORDER BY revision.revision_number DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list configuration revisions: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan configuration revision identity: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]Revision, 0, len(ids))
	for _, id := range ids {
		revision, err := s.Revision(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, revision)
	}
	return result, nil
}

func (s *Service) Scopes(ctx context.Context) ([]OperationalScope, error) {
	active, err := s.ActiveRevision(ctx)
	if err != nil {
		return nil, err
	}
	return []OperationalScope{active.Scope}, nil
}

func (s *Service) loadRevision(ctx context.Context, db queryer, where string, args ...any) (revisionRecord, error) {
	query := `SELECT revision.id, revision.public_id, revision.revision_number,
revision.configuration_hash, revision.summary, revision.query_max_lookback_seconds,
revision.query_max_results, revision.telemetry_retention_days,
revision.browser_notifications_enabled, revision.automatic_escalation_enabled,
revision.created_by, revision.created_at,
(revision.id = active.configuration_revision_id) AS is_active
FROM configuration_revisions AS revision
JOIN active_configuration AS active ON active.singleton_id = 1
WHERE ` + where
	var record revisionRecord
	if err := db.QueryRowContext(ctx, query, args...).Scan(
		&record.internalID, &record.revision.ID, &record.revision.Number, &record.revision.Hash,
		&record.revision.Summary, &record.revision.General.QueryMaxLookbackSeconds,
		&record.revision.General.QueryMaxResults, &record.revision.General.TelemetryRetentionDays,
		&record.revision.General.BrowserNotificationsEnabled,
		&record.revision.General.AutomaticEscalationEnabled,
		&record.revision.CreatedBy, &record.revision.CreatedAt, &record.revision.Active,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return revisionRecord{}, ErrNotFound
		}
		return revisionRecord{}, fmt.Errorf("load configuration revision: %w", err)
	}
	record.revision.CreatedAt = record.revision.CreatedAt.UTC()

	var namespacesJSON []byte
	if err := db.QueryRowContext(ctx, `SELECT public_id, name, cluster_id, environment, namespaces_json
FROM operational_scopes WHERE configuration_revision_id = ?`, record.internalID).Scan(
		&record.revision.Scope.ID, &record.revision.Scope.Name, &record.revision.Scope.ClusterID,
		&record.revision.Scope.Environment, &namespacesJSON,
	); err != nil {
		return revisionRecord{}, fmt.Errorf("load operational scope: %w", err)
	}
	if err := json.Unmarshal(namespacesJSON, &record.revision.Scope.Namespaces); err != nil {
		return revisionRecord{}, fmt.Errorf("decode operational scope namespaces: %w", err)
	}
	record.revision.Scope.RevisionID = record.revision.ID
	record.revision.Scope.RevisionHash = record.revision.Hash

	providerRows, err := db.QueryContext(ctx, `SELECT provider, enabled, endpoint, model,
timeout_ms, max_results, context_link_base
FROM provider_configurations WHERE configuration_revision_id = ? ORDER BY provider`, record.internalID)
	if err != nil {
		return revisionRecord{}, fmt.Errorf("load provider configurations: %w", err)
	}
	record.revision.Providers = make([]ProviderConfiguration, 0, len(operationalProviders))
	for providerRows.Next() {
		var item ProviderConfiguration
		if err := providerRows.Scan(&item.Provider, &item.Enabled, &item.Endpoint, &item.Model, &item.TimeoutMS, &item.MaxResults, &item.ContextLinkBase); err != nil {
			_ = providerRows.Close()
			return revisionRecord{}, fmt.Errorf("scan provider configuration: %w", err)
		}
		record.revision.Providers = append(record.revision.Providers, item)
	}
	if err := providerRows.Close(); err != nil {
		return revisionRecord{}, err
	}
	sortedProviders(record.revision.Providers)

	secretRows, err := db.QueryContext(ctx, `SELECT reference.provider, reference.purpose,
secret.public_id, secret.state, secret.fingerprint
FROM configuration_secret_references AS reference
JOIN secret_versions AS secret ON secret.id = reference.secret_version_id
WHERE reference.configuration_revision_id = ? ORDER BY reference.provider, reference.purpose`, record.internalID)
	if err != nil {
		return revisionRecord{}, fmt.Errorf("load configuration secret references: %w", err)
	}
	record.revision.SecretRefs = []SecretReference{}
	for secretRows.Next() {
		var item SecretReference
		if err := secretRows.Scan(&item.Provider, &item.Purpose, &item.SecretVersionID, &item.State, &item.Fingerprint); err != nil {
			_ = secretRows.Close()
			return revisionRecord{}, fmt.Errorf("scan configuration secret reference: %w", err)
		}
		record.revision.SecretRefs = append(record.revision.SecretRefs, item)
	}
	if err := secretRows.Close(); err != nil {
		return revisionRecord{}, err
	}

	var activation ActivationStatus
	var workerID, observedHash, lastError sql.NullString
	var observedAt sql.NullTime
	err = db.QueryRowContext(ctx, `SELECT task.public_id, revision.public_id, task.status,
task.worker_id, task.observed_hash, task.observed_at, task.last_error
FROM configuration_activation_tasks AS task
JOIN configuration_revisions AS revision ON revision.id = task.configuration_revision_id
WHERE task.configuration_revision_id = ?`, record.internalID).Scan(
		&activation.TaskID, &activation.RevisionID, &activation.Status,
		&workerID, &observedHash, &observedAt, &lastError,
	)
	if err == nil {
		activation.WorkerID = workerID.String
		activation.ObservedHash = observedHash.String
		activation.ObservedAt = timePointer(observedAt)
		activation.LastError = lastError.String
		record.revision.WorkerBoundary = &activation
	} else if !errors.Is(err, sql.ErrNoRows) {
		return revisionRecord{}, fmt.Errorf("load configuration activation: %w", err)
	}
	return record, nil
}
