package observability

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("observability repository database is required")
	}
	return &Repository{db: db, now: time.Now}, nil
}

func (r *Repository) CreateExecution(ctx context.Context, prepared PreparedQuery) (Execution, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Execution{}, fmt.Errorf("begin Query Execution: %w", err)
	}
	defer rollbackObservability(tx)
	publicID := uuid.NewString()
	if _, err := r.insertExecution(ctx, tx, publicID, prepared); err != nil {
		return Execution{}, err
	}
	if err := r.insertEvent(ctx, tx, publicID, "created", string(prepared.Actor), "bounded query accepted"); err != nil {
		return Execution{}, err
	}
	if err := tx.Commit(); err != nil {
		return Execution{}, fmt.Errorf("commit Query Execution: %w", err)
	}
	return r.Execution(ctx, publicID)
}

func (r *Repository) CreateAuthorizedExecution(ctx context.Context, prepared PreparedQuery) (Execution, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Execution{}, fmt.Errorf("begin authorized Query Execution: %w", err)
	}
	defer rollbackObservability(tx)

	var internalID uint64
	var mode AuthorizationMode
	var revisionID, definitionID, query, queryHash, clusterID, environment, resourceID, resourceKind, resourceNamespace, resourceName string
	var queryMode QueryMode
	var catalogKey string
	var maxLookback, maxSeries, maxSamples int
	var consumed sql.NullString
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT authorization.id, authorization.authorization_mode,
revision.public_id, COALESCE(definition.public_id, ''), authorization.query_mode,
authorization.catalog_key, authorization.exact_query_text, authorization.exact_query_hash,
authorization.cluster_id, authorization.environment, authorization.resource_id,
authorization.resource_kind, authorization.resource_namespace, authorization.resource_name,
authorization.max_lookback_seconds, authorization.max_series, authorization.max_samples,
authorization.consumed_execution_public_id, authorization.revoked_at
FROM query_authorizations AS authorization
JOIN configuration_revisions AS revision ON revision.id = authorization.configuration_revision_id
LEFT JOIN query_definitions AS definition ON definition.id = authorization.query_definition_id
WHERE authorization.public_id = ? FOR UPDATE`, prepared.AuthorizationID).Scan(
		&internalID, &mode, &revisionID, &definitionID, &queryMode, &catalogKey, &query, &queryHash,
		&clusterID, &environment, &resourceID, &resourceKind, &resourceNamespace, &resourceName,
		&maxLookback, &maxSeries, &maxSamples, &consumed, &revoked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrUnauthorized
	}
	if err != nil {
		return Execution{}, fmt.Errorf("lock Query Authorization: %w", err)
	}
	if revoked.Valid {
		return Execution{}, ErrAuthorizationRevoked
	}
	if mode == AuthorizationRunOnce && consumed.Valid && consumed.String != "" {
		return Execution{}, ErrAuthorizationUsed
	}
	if prepared.Actor != ActorAgent || prepared.ConfigurationRevision != revisionID || prepared.DefinitionID != definitionID ||
		prepared.Mode != queryMode || prepared.CatalogKey != catalogKey || prepared.Query != query || prepared.QueryHash != queryHash ||
		prepared.Scope.ClusterID != clusterID || prepared.Scope.Environment != environment || prepared.Resource.ID != resourceID ||
		prepared.Resource.Kind != resourceKind || prepared.Resource.Namespace != resourceNamespace || prepared.Resource.Name != resourceName ||
		prepared.Bounds.MaxLookbackSeconds > maxLookback || prepared.Bounds.MaxSeries > maxSeries || prepared.Bounds.MaxSamples > maxSamples {
		return Execution{}, fmt.Errorf("%w: prepared Agent query does not match its authorization", ErrUnauthorized)
	}
	publicID := uuid.NewString()
	if _, err := r.insertExecution(ctx, tx, publicID, prepared); err != nil {
		return Execution{}, err
	}
	if mode == AuthorizationRunOnce {
		result, updateErr := tx.ExecContext(ctx, `UPDATE query_authorizations
SET consumed_execution_public_id = ? WHERE id = ? AND consumed_execution_public_id IS NULL`, publicID, internalID)
		if updateErr != nil {
			return Execution{}, fmt.Errorf("consume one-time Query Authorization: %w", updateErr)
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return Execution{}, ErrAuthorizationUsed
		}
	}
	if err := r.insertEvent(ctx, tx, publicID, "created", string(ActorAgent), "authorized bounded query accepted"); err != nil {
		return Execution{}, err
	}
	if err := tx.Commit(); err != nil {
		return Execution{}, fmt.Errorf("commit authorized Query Execution: %w", err)
	}
	return r.Execution(ctx, publicID)
}

func (r *Repository) insertExecution(ctx context.Context, tx *sql.Tx, publicID string, prepared PreparedQuery) (uint64, error) {
	namespaces, _ := json.Marshal(prepared.Scope.Namespaces)
	var revisionInternalID uint64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM configuration_revisions WHERE public_id = ?`, prepared.ConfigurationRevision).Scan(&revisionInternalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("resolve Configuration Revision: %w", err)
	}
	definitionID, err := optionalInternalID(ctx, tx, "query_definitions", prepared.DefinitionID)
	if err != nil {
		return 0, err
	}
	authorizationID, err := optionalInternalID(ctx, tx, "query_authorizations", prepared.AuthorizationID)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO query_executions (
public_id, configuration_revision_id, query_definition_id, query_authorization_id,
actor, provider, mode, catalog_key, query_text, query_hash, cluster_id, environment,
namespaces_json, resource_id, resource_kind, resource_namespace, resource_name,
range_start, range_end, step_seconds, timeout_ms, max_response_bytes, max_series,
max_samples, concurrency_limit, status, created_at
) VALUES (?, ?, ?, ?, ?, 'prometheus', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		publicID, revisionInternalID, definitionID, authorizationID, prepared.Actor, prepared.Mode,
		prepared.CatalogKey, prepared.Query, prepared.QueryHash, prepared.Scope.ClusterID,
		prepared.Scope.Environment, namespaces, prepared.Resource.ID, prepared.Resource.Kind,
		prepared.Resource.Namespace, prepared.Resource.Name, prepared.TimeRange.From.UTC(),
		prepared.TimeRange.To.UTC(), prepared.Bounds.StepSeconds, prepared.Bounds.TimeoutMS,
		prepared.Bounds.MaxResponseBytes, prepared.Bounds.MaxSeries, prepared.Bounds.MaxSamples,
		prepared.Bounds.ConcurrencyLimit, r.now().UTC(),
	)
	if err != nil {
		return 0, mapRepositoryError("insert Query Execution", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("read Query Execution id: %w", err)
	}
	return uint64(id), nil
}

func optionalInternalID(ctx context.Context, tx *sql.Tx, table, publicID string) (any, error) {
	if strings.TrimSpace(publicID) == "" {
		return nil, nil
	}
	if table != "query_definitions" && table != "query_authorizations" {
		return nil, errors.New("unsupported observability identity table")
	}
	var id uint64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE public_id = ?`, publicID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return id, nil
}

func (r *Repository) MarkRunning(ctx context.Context, publicID string) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer rollbackObservability(tx)
	result, err := tx.ExecContext(ctx, `UPDATE query_executions
SET status = 'running', started_at = ? WHERE public_id = ? AND status = 'pending'`, r.now().UTC(), publicID)
	if err != nil {
		return fmt.Errorf("start Query Execution: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrConflict
	}
	if err := r.insertEvent(ctx, tx, publicID, "started", "system", "Provider request started"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Complete(ctx context.Context, publicID string, result ProviderQueryResult, executionErr error) error {
	status := ExecutionSucceeded
	errorCode, errorDetail := "", ""
	if executionErr != nil {
		status = ExecutionFailed
		errorCode, errorDetail = executionError(executionErr)
	}
	completed := r.now().UTC()
	collectedAt := any(nil)
	if !result.Source.CollectedAt.IsZero() {
		collectedAt = result.Source.CollectedAt.UTC()
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer rollbackObservability(tx)
	update, err := tx.ExecContext(ctx, `UPDATE query_executions SET status = ?, provider_identity = ?,
provider_server_version = ?, provider_collected_at = ?, result_type = ?, series_count = ?,
sample_count = ?, response_bytes = ?, partial = ?, truncated = ?, error_code = ?,
error_detail = ?, completed_at = ? WHERE public_id = ? AND status = 'running'`,
		status, boundedString(result.Source.Identity, 512), boundedString(result.Source.ServerVersion, 128),
		collectedAt, result.Result.ResultType, result.SeriesCount, result.SampleCount, result.ResponseBytes,
		result.Partial, result.Truncated, errorCode, errorDetail, completed, publicID,
	)
	if err != nil {
		return fmt.Errorf("complete Query Execution: %w", err)
	}
	if rows, _ := update.RowsAffected(); rows != 1 {
		return ErrConflict
	}
	eventType, detail := "succeeded", "bounded Provider result completed"
	if status == ExecutionFailed {
		eventType, detail = "failed", errorCode
	}
	if err := r.insertEvent(ctx, tx, publicID, eventType, "system", detail); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Cancel(ctx context.Context, publicID string) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer rollbackObservability(tx)
	result, err := tx.ExecContext(ctx, `UPDATE query_executions SET status = 'cancelled',
error_code = 'QUERY_CANCELLED', error_detail = 'Query cancelled by local Owner', completed_at = ?
WHERE public_id = ? AND status IN ('pending','running')`, r.now().UTC(), publicID)
	if err != nil {
		return fmt.Errorf("cancel Query Execution: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		var exists bool
		if scanErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM query_executions WHERE public_id = ?)`, publicID).Scan(&exists); scanErr != nil {
			return scanErr
		}
		if !exists {
			return ErrNotFound
		}
		return ErrConflict
	}
	if err := r.insertEvent(ctx, tx, publicID, "cancel_requested", string(ActorOwner), "local Owner requested cancellation"); err != nil {
		return err
	}
	if err := r.insertEvent(ctx, tx, publicID, "cancelled", "system", "Query cancellation recorded"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) RecoverInFlight(ctx context.Context) error {
	completed := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin interrupted Query Execution recovery: %w", err)
	}
	defer rollbackObservability(tx)
	rows, err := tx.QueryContext(ctx, `SELECT public_id FROM query_executions
WHERE status IN ('pending','running') ORDER BY id FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("list interrupted Query Executions: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		result, updateErr := tx.ExecContext(ctx, `UPDATE query_executions SET status = 'failed',
error_code = 'QUERY_INTERRUPTED', error_detail = 'Query execution interrupted by API restart',
completed_at = ? WHERE public_id = ? AND status IN ('pending','running')`, completed, id)
		if updateErr != nil {
			return fmt.Errorf("recover interrupted Query Execution: %w", updateErr)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrConflict
		}
		if err := r.insertEvent(ctx, tx, id, "failed", "system", "QUERY_INTERRUPTED"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const executionSelect = `SELECT execution.public_id, revision.public_id,
COALESCE(definition.public_id, ''), COALESCE(authorization.public_id, ''), execution.actor,
	execution.provider, execution.mode, execution.catalog_key, execution.query_text,
	execution.query_hash, execution.cluster_id, execution.environment, scope.public_id, scope.name,
	revision.configuration_hash,
	(revision.id = active.configuration_revision_id AND scope.id = selected.operational_scope_id),
	execution.namespaces_json,
execution.resource_id, execution.resource_kind, execution.resource_namespace,
execution.resource_name, execution.range_start, execution.range_end, execution.step_seconds,
execution.timeout_ms, execution.max_response_bytes, execution.max_series, execution.max_samples,
execution.concurrency_limit, execution.status, execution.provider_identity,
execution.provider_server_version, execution.provider_collected_at, execution.result_type,
execution.series_count, execution.sample_count, execution.response_bytes, execution.partial,
execution.truncated, execution.error_code, execution.error_detail, execution.created_at,
execution.started_at, execution.completed_at
FROM query_executions AS execution
JOIN configuration_revisions AS revision ON revision.id = execution.configuration_revision_id
JOIN operational_scopes AS scope ON scope.configuration_revision_id = revision.id
	AND scope.cluster_id = execution.cluster_id AND scope.environment = execution.environment
JOIN active_configuration AS active ON active.singleton_id = 1
LEFT JOIN active_operational_scope AS selected ON selected.singleton_id = 1
LEFT JOIN query_definitions AS definition ON definition.id = execution.query_definition_id
LEFT JOIN query_authorizations AS authorization ON authorization.id = execution.query_authorization_id`

func scanExecution(scanner interface{ Scan(...any) error }) (Execution, error) {
	var item Execution
	var namespaces []byte
	var providerCollected, started, completed sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.ConfigurationRevision, &item.DefinitionID, &item.AuthorizationID,
		&item.Actor, &item.Provider, &item.Mode, &item.CatalogKey, &item.Query, &item.QueryHash,
		&item.Scope.ClusterID, &item.Scope.Environment, &item.Scope.ID, &item.Scope.Name,
		&item.Scope.RevisionHash, &item.Scope.Active, &namespaces, &item.Resource.ID,
		&item.Resource.Kind, &item.Resource.Namespace, &item.Resource.Name, &item.TimeRange.From,
		&item.TimeRange.To, &item.Bounds.StepSeconds, &item.Bounds.TimeoutMS,
		&item.Bounds.MaxResponseBytes, &item.Bounds.MaxSeries, &item.Bounds.MaxSamples,
		&item.Bounds.ConcurrencyLimit, &item.Status, &item.Source.Identity, &item.Source.ServerVersion,
		&providerCollected, &item.Source.Provider, &item.SeriesCount, &item.SampleCount,
		&item.ResponseBytes, &item.Partial, &item.Truncated, &item.ErrorCode, &item.ErrorDetail,
		&item.CreatedAt, &started, &completed,
	); err != nil {
		return Execution{}, err
	}
	if err := json.Unmarshal(namespaces, &item.Scope.Namespaces); err != nil {
		return Execution{}, fmt.Errorf("decode Query Execution scope: %w", err)
	}
	item.Scope.RevisionID = item.ConfigurationRevision
	item.Resource.Namespace = strings.TrimSpace(item.Resource.Namespace)
	item.TimeRange.From, item.TimeRange.To, item.CreatedAt = item.TimeRange.From.UTC(), item.TimeRange.To.UTC(), item.CreatedAt.UTC()
	item.StartedAt, item.CompletedAt = timePointer(started), timePointer(completed)
	if providerCollected.Valid {
		item.Source.CollectedAt = providerCollected.Time.UTC()
	}
	item.Source.Provider = "prometheus"
	item.Bounds.MaxLookbackSeconds = int(item.TimeRange.To.Sub(item.TimeRange.From) / time.Second)
	item.Links, item.Events = []ContextLink{}, []ExecutionEvent{}
	return item, nil
}

func (r *Repository) Execution(ctx context.Context, publicID string) (Execution, error) {
	item, err := scanExecution(r.db.QueryRowContext(ctx, executionSelect+` WHERE execution.public_id = ?`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if err != nil {
		return Execution{}, fmt.Errorf("load Query Execution: %w", err)
	}
	item.Events, err = r.executionEvents(ctx, publicID)
	return item, err
}

func (r *Repository) Executions(ctx context.Context, filter HistoryFilter) ([]Execution, error) {
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}
	query := executionSelect + ` WHERE 1 = 1`
	args := make([]any, 0, 4)
	if strings.TrimSpace(filter.ClusterID) != "" {
		query += ` AND execution.cluster_id = ?`
		args = append(args, strings.TrimSpace(filter.ClusterID))
	}
	if strings.TrimSpace(filter.Namespace) != "" {
		query += ` AND execution.resource_namespace = ?`
		args = append(args, strings.TrimSpace(filter.Namespace))
	}
	if strings.TrimSpace(filter.ResourceID) != "" {
		query += ` AND execution.resource_id = ?`
		args = append(args, strings.TrimSpace(filter.ResourceID))
	}
	query += ` ORDER BY execution.created_at DESC, execution.id DESC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list Query Executions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]Execution, 0, filter.Limit)
	for rows.Next() {
		item, scanErr := scanExecution(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) executionEvents(ctx context.Context, executionID string) ([]ExecutionEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT event.public_id, event.sequence, event.event_type,
event.actor, event.detail, event.created_at FROM query_execution_events AS event
JOIN query_executions AS execution ON execution.id = event.query_execution_id
WHERE execution.public_id = ? ORDER BY event.sequence`, executionID)
	if err != nil {
		return nil, fmt.Errorf("list Query Execution events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []ExecutionEvent{}
	for rows.Next() {
		var item ExecutionEvent
		if err := rows.Scan(&item.ID, &item.Sequence, &item.Type, &item.Actor, &item.Detail, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) insertEvent(ctx context.Context, tx *sql.Tx, executionID, eventType, actor, detail string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO query_execution_events (
public_id, query_execution_id, sequence, event_type, actor, detail, created_at
) SELECT ?, execution.id, COALESCE(MAX(event.sequence), 0) + 1, ?, ?, ?, ?
FROM query_executions AS execution
LEFT JOIN query_execution_events AS event ON event.query_execution_id = execution.id
WHERE execution.public_id = ? GROUP BY execution.id`, uuid.NewString(), eventType, actor,
		boundedString(detail, 512), r.now().UTC(), executionID)
	if err != nil {
		return fmt.Errorf("append Query Execution audit event: %w", err)
	}
	return nil
}

func (r *Repository) CreateDefinition(ctx context.Context, request SaveDefinitionRequest) (Definition, error) {
	title, description := strings.TrimSpace(request.Title), strings.TrimSpace(request.Description)
	if len(title) < 1 || len(title) > 128 || len(description) > 512 {
		return Definition{}, fmt.Errorf("%w: Query Definition title or description is invalid", ErrInvalid)
	}
	execution, err := r.Execution(ctx, strings.TrimSpace(request.ExecutionID))
	if err != nil {
		return Definition{}, err
	}
	if execution.Status != ExecutionSucceeded || execution.Actor != ActorOwner {
		return Definition{}, fmt.Errorf("%w: only a successful Owner query can be saved", ErrConflict)
	}
	definitionKey, revisionNumber := uuid.NewString(), uint64(1)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Definition{}, err
	}
	defer rollbackObservability(tx)
	if previous := strings.TrimSpace(request.PreviousDefinitionID); previous != "" {
		var previousInternalID uint64
		if err := tx.QueryRowContext(ctx, `SELECT id, definition_key, revision_number
FROM query_definitions WHERE public_id = ? FOR UPDATE`, previous).Scan(&previousInternalID, &definitionKey, &revisionNumber); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Definition{}, ErrNotFound
			}
			return Definition{}, err
		}
		revisionNumber++
		if _, err := tx.ExecContext(ctx, `UPDATE query_authorizations AS authorization
JOIN query_definitions AS definition ON definition.id = authorization.query_definition_id
SET authorization.revoked_at = ?, authorization.revoked_by = 'local-owner'
WHERE definition.definition_key = ? AND authorization.revoked_at IS NULL`, r.now().UTC(), definitionKey); err != nil {
			return Definition{}, fmt.Errorf("revoke previous Query Definition authorizations: %w", err)
		}
	}
	contentHash := definitionContentHash(title, description, execution)
	namespaces, _ := json.Marshal(execution.Scope.Namespaces)
	publicID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO query_definitions (
public_id, definition_key, revision_number, configuration_revision_id, provider, mode,
catalog_key, title, description, query_text, query_hash, cluster_id, environment,
namespaces_json, resource_id, resource_kind, resource_namespace, resource_name,
max_lookback_seconds, max_series, max_samples, content_hash, created_by, created_at
) SELECT ?, ?, ?, revision.id, 'prometheus', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
'local-owner', ? FROM configuration_revisions AS revision WHERE revision.public_id = ?`,
		publicID, definitionKey, revisionNumber, execution.Mode, execution.CatalogKey, title, description,
		execution.Query, execution.QueryHash, execution.Scope.ClusterID, execution.Scope.Environment,
		namespaces, execution.Resource.ID, execution.Resource.Kind, execution.Resource.Namespace,
		execution.Resource.Name, execution.Bounds.MaxLookbackSeconds, execution.Bounds.MaxSeries,
		execution.Bounds.MaxSamples, contentHash, r.now().UTC(), execution.ConfigurationRevision,
	)
	if err != nil {
		return Definition{}, mapRepositoryError("insert Query Definition", err)
	}
	if err := tx.Commit(); err != nil {
		return Definition{}, err
	}
	return r.Definition(ctx, publicID)
}

const definitionSelect = `SELECT definition.public_id, definition.definition_key,
definition.revision_number, revision.public_id, definition.provider, definition.mode,
	definition.catalog_key, definition.title, definition.description, definition.query_text,
	definition.query_hash, definition.cluster_id, definition.environment, scope.public_id, scope.name,
	revision.configuration_hash,
	(revision.id = active.configuration_revision_id AND scope.id = selected.operational_scope_id),
	definition.namespaces_json,
definition.resource_id, definition.resource_kind, definition.resource_namespace,
definition.resource_name, definition.max_lookback_seconds, definition.max_series,
definition.max_samples, definition.content_hash, definition.created_by, definition.created_at
FROM query_definitions AS definition
JOIN configuration_revisions AS revision ON revision.id = definition.configuration_revision_id
JOIN operational_scopes AS scope ON scope.configuration_revision_id = revision.id
	AND scope.cluster_id = definition.cluster_id AND scope.environment = definition.environment
JOIN active_configuration AS active ON active.singleton_id = 1
LEFT JOIN active_operational_scope AS selected ON selected.singleton_id = 1`

func scanDefinition(scanner interface{ Scan(...any) error }) (Definition, error) {
	var item Definition
	var namespaces []byte
	if err := scanner.Scan(&item.ID, &item.DefinitionKey, &item.Revision, &item.ConfigurationRevision,
		&item.Provider, &item.Mode, &item.CatalogKey, &item.Title, &item.Description, &item.Query,
		&item.QueryHash, &item.Scope.ClusterID, &item.Scope.Environment, &item.Scope.ID,
		&item.Scope.Name, &item.Scope.RevisionHash, &item.Scope.Active, &namespaces,
		&item.Resource.ID, &item.Resource.Kind, &item.Resource.Namespace, &item.Resource.Name,
		&item.MaxLookbackSeconds, &item.MaxSeries, &item.MaxSamples, &item.ContentHash,
		&item.CreatedBy, &item.CreatedAt); err != nil {
		return Definition{}, err
	}
	if err := json.Unmarshal(namespaces, &item.Scope.Namespaces); err != nil {
		return Definition{}, err
	}
	item.Scope.RevisionID = item.ConfigurationRevision
	item.CreatedAt = item.CreatedAt.UTC()
	item.Links = []ContextLink{}
	return item, nil
}

func (r *Repository) Definition(ctx context.Context, publicID string) (Definition, error) {
	item, err := scanDefinition(r.db.QueryRowContext(ctx, definitionSelect+` WHERE definition.public_id = ?`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Definition{}, ErrNotFound
	}
	if err != nil {
		return Definition{}, fmt.Errorf("load Query Definition: %w", err)
	}
	return item, nil
}

func (r *Repository) Definitions(ctx context.Context, limit int) ([]Definition, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, definitionSelect+` ORDER BY definition.created_at DESC, definition.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list Query Definitions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]Definition, 0, limit)
	for rows.Next() {
		item, scanErr := scanDefinition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func definitionContentHash(title, description string, execution Execution) string {
	value := struct {
		Title, Description, Revision, QueryHash, Cluster, Environment, Resource string
		Mode                                                                    QueryMode
		Bounds                                                                  QueryBounds
	}{title, description, execution.ConfigurationRevision, execution.QueryHash, execution.Scope.ClusterID,
		execution.Scope.Environment, execution.Resource.ID, execution.Mode, execution.Bounds}
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (r *Repository) CreateAuthorization(ctx context.Context, request CreateAuthorizationRequest) (Authorization, error) {
	var binding Authorization
	switch request.Mode {
	case AuthorizationRunOnce:
		execution, err := r.Execution(ctx, strings.TrimSpace(request.ExecutionID))
		if err != nil {
			return Authorization{}, err
		}
		if execution.Status != ExecutionSucceeded || execution.Actor != ActorOwner {
			return Authorization{}, fmt.Errorf("%w: run-once authorization requires a successful Owner query", ErrConflict)
		}
		binding = authorizationFromExecution(execution)
	case AuthorizationDefinition:
		definition, err := r.Definition(ctx, strings.TrimSpace(request.DefinitionID))
		if err != nil {
			return Authorization{}, err
		}
		binding = authorizationFromDefinition(definition)
	default:
		return Authorization{}, fmt.Errorf("%w: Query Authorization mode is invalid", ErrInvalid)
	}
	binding.ID, binding.Mode, binding.CreatedBy, binding.CreatedAt = uuid.NewString(), request.Mode, "local-owner", r.now().UTC()
	namespaces, _ := json.Marshal(binding.Scope.Namespaces)
	var definitionID any
	if binding.DefinitionID != "" {
		definitionID = binding.DefinitionID
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO query_authorizations (
public_id, authorization_mode, configuration_revision_id, query_definition_id, provider,
query_mode, catalog_key, exact_query_text, exact_query_hash, cluster_id, environment,
namespaces_json, resource_id, resource_kind, resource_namespace, resource_name,
max_lookback_seconds, max_series, max_samples, created_by, created_at
) SELECT ?, ?, revision.id, definition.id, 'prometheus', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
'local-owner', ? FROM configuration_revisions AS revision
LEFT JOIN query_definitions AS definition ON definition.public_id = ?
WHERE revision.public_id = ?`, binding.ID, binding.Mode, binding.QueryMode, binding.CatalogKey,
		binding.Query, binding.QueryHash, binding.Scope.ClusterID, binding.Scope.Environment,
		namespaces, binding.Resource.ID, binding.Resource.Kind, binding.Resource.Namespace,
		binding.Resource.Name, binding.MaxLookbackSeconds, binding.MaxSeries, binding.MaxSamples,
		binding.CreatedAt, definitionID, binding.ConfigurationRevision)
	if err != nil {
		return Authorization{}, mapRepositoryError("insert Query Authorization", err)
	}
	return r.Authorization(ctx, binding.ID)
}

func authorizationFromExecution(execution Execution) Authorization {
	return Authorization{
		ConfigurationRevision: execution.ConfigurationRevision, Provider: "prometheus",
		QueryMode: execution.Mode, CatalogKey: execution.CatalogKey, Query: execution.Query,
		QueryHash: execution.QueryHash, Scope: execution.Scope, Resource: execution.Resource,
		MaxLookbackSeconds: execution.Bounds.MaxLookbackSeconds, MaxSeries: execution.Bounds.MaxSeries,
		MaxSamples: execution.Bounds.MaxSamples,
	}
}

func authorizationFromDefinition(definition Definition) Authorization {
	return Authorization{
		ConfigurationRevision: definition.ConfigurationRevision, DefinitionID: definition.ID,
		Provider: "prometheus", QueryMode: definition.Mode, CatalogKey: definition.CatalogKey,
		Query: definition.Query, QueryHash: definition.QueryHash, Scope: definition.Scope,
		Resource: definition.Resource, MaxLookbackSeconds: definition.MaxLookbackSeconds,
		MaxSeries: definition.MaxSeries, MaxSamples: definition.MaxSamples,
	}
}

const authorizationSelect = `SELECT authorization.public_id, revision.public_id,
authorization.authorization_mode, COALESCE(definition.public_id, ''), authorization.provider,
	authorization.query_mode, authorization.catalog_key, authorization.exact_query_text,
	authorization.exact_query_hash, authorization.cluster_id, authorization.environment,
	scope.public_id, scope.name, revision.configuration_hash,
	(revision.id = active.configuration_revision_id AND scope.id = selected.operational_scope_id),
	authorization.namespaces_json, authorization.resource_id, authorization.resource_kind,
authorization.resource_namespace, authorization.resource_name,
authorization.max_lookback_seconds, authorization.max_series, authorization.max_samples,
authorization.consumed_execution_public_id, authorization.revoked_at,
authorization.created_by, authorization.created_at
FROM query_authorizations AS authorization
JOIN configuration_revisions AS revision ON revision.id = authorization.configuration_revision_id
JOIN operational_scopes AS scope ON scope.configuration_revision_id = revision.id
	AND scope.cluster_id = authorization.cluster_id AND scope.environment = authorization.environment
JOIN active_configuration AS active ON active.singleton_id = 1
LEFT JOIN active_operational_scope AS selected ON selected.singleton_id = 1
LEFT JOIN query_definitions AS definition ON definition.id = authorization.query_definition_id`

func scanAuthorization(scanner interface{ Scan(...any) error }) (Authorization, error) {
	var item Authorization
	var namespaces []byte
	var consumed sql.NullString
	var revoked sql.NullTime
	if err := scanner.Scan(&item.ID, &item.ConfigurationRevision, &item.Mode, &item.DefinitionID,
		&item.Provider, &item.QueryMode, &item.CatalogKey, &item.Query, &item.QueryHash,
		&item.Scope.ClusterID, &item.Scope.Environment, &item.Scope.ID, &item.Scope.Name,
		&item.Scope.RevisionHash, &item.Scope.Active, &namespaces, &item.Resource.ID,
		&item.Resource.Kind, &item.Resource.Namespace, &item.Resource.Name,
		&item.MaxLookbackSeconds, &item.MaxSeries, &item.MaxSamples, &consumed, &revoked,
		&item.CreatedBy, &item.CreatedAt); err != nil {
		return Authorization{}, err
	}
	if err := json.Unmarshal(namespaces, &item.Scope.Namespaces); err != nil {
		return Authorization{}, err
	}
	item.Scope.RevisionID = item.ConfigurationRevision
	item.ConsumedExecutionID = consumed.String
	item.RevokedAt = timePointer(revoked)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func (r *Repository) Authorization(ctx context.Context, publicID string) (Authorization, error) {
	item, err := scanAuthorization(r.db.QueryRowContext(ctx, authorizationSelect+` WHERE authorization.public_id = ?`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Authorization{}, ErrNotFound
	}
	if err != nil {
		return Authorization{}, fmt.Errorf("load Query Authorization: %w", err)
	}
	return item, nil
}

func (r *Repository) Authorizations(ctx context.Context, limit int) ([]Authorization, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, authorizationSelect+` ORDER BY authorization.created_at DESC, authorization.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list Query Authorizations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]Authorization, 0, limit)
	for rows.Next() {
		item, scanErr := scanAuthorization(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) RevokeAuthorization(ctx context.Context, publicID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE query_authorizations
SET revoked_at = COALESCE(revoked_at, ?), revoked_by = COALESCE(revoked_by, 'local-owner')
WHERE public_id = ?`, r.now().UTC(), strings.TrimSpace(publicID))
	if err != nil {
		return fmt.Errorf("revoke Query Authorization: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM query_authorizations WHERE public_id = ?)`, publicID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func executionError(err error) (string, string) {
	switch {
	case errors.Is(err, context.Canceled):
		return "QUERY_CANCELLED", "Query execution was cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "PROVIDER_TIMEOUT", "Prometheus did not complete within the bounded timeout"
	case errors.Is(err, ErrBoundExceeded):
		return "QUERY_BOUND_EXCEEDED", "Prometheus result exceeded the configured query bounds"
	case errors.Is(err, ErrProviderDisabled):
		return "PROMETHEUS_PROVIDER_DISABLED", "Prometheus is disabled in the bound Configuration Revision"
	default:
		return "PROMETHEUS_PROVIDER_UNAVAILABLE", "Prometheus query execution is unavailable"
	}
}

func boundedString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func mapRepositoryError(operation string, err error) error {
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		return fmt.Errorf("%w: %s conflicts with an existing immutable record", ErrConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func rollbackObservability(tx *sql.Tx) {
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
