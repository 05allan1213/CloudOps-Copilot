package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type executionRecord struct {
	ID                    string
	ConfigurationRevision string
	Provider              string
	Kind                  string
	Mode                  QueryMode
	Query                 string
	QueryHash             string
	Scope                 scopeRecord
	Resource              ResourceReference
	TimeRange             TimeRange
	Bounds                QueryBounds
	Status                string
	Source                ProviderSource
	ResultType            string
	ResultCount           int
	ResponseBytes         int
	Partial               bool
	Truncated             bool
	ErrorCode             string
	ErrorDetail           string
	CreatedAt             time.Time
	CompletedAt           *time.Time
}

type scopeRecord struct {
	ClusterID   string
	Environment string
	Namespaces  []string
}

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("telemetry repository requires MySQL")
	}
	return &Repository{db: db, now: time.Now}, nil
}

func (r *Repository) CreateExecution(ctx context.Context, prepared preparedQuery) (executionRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return executionRecord{}, fmt.Errorf("begin telemetry Query Execution: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	revisionID, err := internalID(ctx, tx, "configuration_revisions", prepared.ConfigurationRevision)
	if err != nil {
		return executionRecord{}, err
	}
	publicID, eventID := uuid.NewString(), uuid.NewString()
	namespaces, err := json.Marshal(prepared.Scope.Namespaces)
	if err != nil {
		return executionRecord{}, fmt.Errorf("encode Query Execution Namespace scope: %w", err)
	}
	createdAt := r.now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO query_executions
 (public_id, configuration_revision_id, actor, provider, mode, catalog_key, query_text, query_hash,
  cluster_id, environment, namespaces_json, resource_id, resource_kind, resource_namespace, resource_name,
  range_start, range_end, step_seconds, timeout_ms, max_response_bytes, max_series, max_samples,
  concurrency_limit, status, created_at)
 VALUES (?, ?, 'owner', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, 1, ?, ?, 'pending', ?)`,
		publicID, revisionID, prepared.Provider, prepared.Mode, prepared.Kind, prepared.Query, prepared.QueryHash,
		prepared.Scope.ClusterID, prepared.Scope.Environment, namespaces, prepared.Resource.ID,
		prepared.Resource.Kind, prepared.Resource.Namespace, prepared.Resource.Name,
		prepared.TimeRange.From, prepared.TimeRange.To, prepared.Bounds.TimeoutMS,
		prepared.Bounds.MaxResponseBytes, prepared.Bounds.MaxResults, prepared.Bounds.ConcurrencyLimit, createdAt)
	if err != nil {
		return executionRecord{}, fmt.Errorf("persist telemetry Query Execution: %w", err)
	}
	executionID, err := result.LastInsertId()
	if err != nil {
		return executionRecord{}, fmt.Errorf("read telemetry Query Execution identity: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO query_execution_events
 (public_id, query_execution_id, sequence, event_type, actor, detail, created_at)
 VALUES (?, ?, 1, 'created', 'owner', 'bounded Workspace query accepted', ?)`, eventID, executionID, createdAt); err != nil {
		return executionRecord{}, fmt.Errorf("persist telemetry Query Execution creation event: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return executionRecord{}, fmt.Errorf("commit telemetry Query Execution: %w", err)
	}
	return executionRecord{
		ID: publicID, ConfigurationRevision: prepared.ConfigurationRevision, Provider: prepared.Provider, Kind: prepared.Kind,
		Mode: prepared.Mode, Query: prepared.Query, QueryHash: prepared.QueryHash,
		Scope:    scopeRecord{ClusterID: prepared.Scope.ClusterID, Environment: prepared.Scope.Environment, Namespaces: append([]string(nil), prepared.Scope.Namespaces...)},
		Resource: prepared.Resource, TimeRange: prepared.TimeRange, Bounds: prepared.Bounds,
		Status: "pending", CreatedAt: createdAt,
	}, nil
}

func (r *Repository) MarkRunning(ctx context.Context, publicID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, status, err := queryExecutionIdentity(ctx, tx, publicID, true)
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrConflict
	}
	now := r.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE query_executions SET status='running', started_at=? WHERE id=? AND status='pending'`, now, id)
	if err != nil {
		return fmt.Errorf("start telemetry Query Execution: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO query_execution_events
 (public_id, query_execution_id, sequence, event_type, actor, detail, created_at)
 VALUES (?, ?, 2, 'started', 'system', 'Provider Gateway query started', ?)`, uuid.NewString(), id, now); err != nil {
		return fmt.Errorf("persist telemetry Query Execution start event: %w", err)
	}
	return tx.Commit()
}

func (r *Repository) Complete(ctx context.Context, publicID, resultType string, source ProviderSource, resultCount, responseBytes int, partial, truncated bool, queryErr error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, status, err := queryExecutionIdentity(ctx, tx, publicID, true)
	if err != nil {
		return err
	}
	if status != "running" {
		return ErrConflict
	}
	now := r.now().UTC()
	terminal, eventType, detail, code := "succeeded", "succeeded", "bounded Provider result projected", ""
	if queryErr != nil {
		terminal, eventType, detail, code = "failed", "failed", boundedDetail(queryErr.Error(), 512), providerErrorCode(queryErr)
		resultType, resultCount, responseBytes, partial, truncated = "", 0, 0, false, false
	}
	collectedAt := source.CollectedAt
	if collectedAt.IsZero() {
		collectedAt = now
	}
	result, err := tx.ExecContext(ctx, `UPDATE query_executions SET
 status=?, provider_identity=?, provider_server_version=?, provider_collected_at=?, result_type=?,
 series_count=0, sample_count=?, response_bytes=?, partial=?, truncated=?, error_code=?, error_detail=?, completed_at=?
 WHERE id=? AND status='running'`, terminal, boundedDetail(source.Identity, 512), boundedDetail(source.ServerVersion, 128),
		collectedAt, resultType, resultCount, responseBytes, partial, truncated, code, detailIfFailed(queryErr, detail), now, id)
	if err != nil {
		return fmt.Errorf("complete telemetry Query Execution: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO query_execution_events
 (public_id, query_execution_id, sequence, event_type, actor, detail, created_at)
 VALUES (?, ?, 3, ?, 'system', ?, ?)`, uuid.NewString(), id, eventType, detail, now); err != nil {
		return fmt.Errorf("persist telemetry Query Execution terminal event: %w", err)
	}
	return tx.Commit()
}

func (r *Repository) Execution(ctx context.Context, publicID, provider string) (executionRecord, error) {
	row := r.db.QueryRowContext(ctx, `SELECT
	 qe.public_id, cr.public_id, qe.provider, qe.catalog_key, qe.mode, qe.query_text, qe.query_hash,
 qe.cluster_id, qe.environment, qe.namespaces_json, qe.resource_id, qe.resource_kind,
 qe.resource_namespace, qe.resource_name, qe.range_start, qe.range_end, qe.timeout_ms,
 qe.max_response_bytes, qe.max_samples, qe.concurrency_limit, qe.status,
 qe.provider_identity, qe.provider_server_version, qe.provider_collected_at, qe.result_type,
 qe.sample_count, qe.response_bytes, qe.partial, qe.truncated, qe.error_code, qe.error_detail,
 qe.created_at, qe.completed_at
 FROM query_executions qe
 JOIN configuration_revisions cr ON cr.id=qe.configuration_revision_id
 WHERE qe.public_id=? AND qe.provider=?`, strings.TrimSpace(publicID), strings.TrimSpace(provider))
	return scanExecution(row)
}

func (r *Repository) Executions(ctx context.Context, provider, clusterID, namespace, resourceID string, limit int) ([]executionRecord, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	kind := "logs%"
	if provider == "tempo" {
		kind = "trace_search"
	}
	rows, err := r.db.QueryContext(ctx, `SELECT
	 qe.public_id, cr.public_id, qe.provider, qe.catalog_key, qe.mode, qe.query_text, qe.query_hash,
 qe.cluster_id, qe.environment, qe.namespaces_json, qe.resource_id, qe.resource_kind,
 qe.resource_namespace, qe.resource_name, qe.range_start, qe.range_end, qe.timeout_ms,
 qe.max_response_bytes, qe.max_samples, qe.concurrency_limit, qe.status,
 qe.provider_identity, qe.provider_server_version, qe.provider_collected_at, qe.result_type,
 qe.sample_count, qe.response_bytes, qe.partial, qe.truncated, qe.error_code, qe.error_detail,
 qe.created_at, qe.completed_at
 FROM query_executions qe
 JOIN configuration_revisions cr ON cr.id=qe.configuration_revision_id
	 WHERE qe.provider=? AND qe.catalog_key LIKE ? AND (?='' OR qe.cluster_id=?) AND (?='' OR qe.resource_namespace=?) AND (?='' OR qe.resource_id=?)
	 ORDER BY qe.created_at DESC, qe.id DESC LIMIT ?`, provider, kind, clusterID, clusterID, namespace, namespace, resourceID, resourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list telemetry Query Executions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []executionRecord{}
	for rows.Next() {
		item, scanErr := scanExecution(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanExecution(row rowScanner) (executionRecord, error) {
	var item executionRecord
	var namespaces []byte
	var collectedAt, completedAt sql.NullTime
	var partial, truncated bool
	err := row.Scan(
		&item.ID, &item.ConfigurationRevision, &item.Provider, &item.Kind, &item.Mode, &item.Query, &item.QueryHash,
		&item.Scope.ClusterID, &item.Scope.Environment, &namespaces, &item.Resource.ID, &item.Resource.Kind,
		&item.Resource.Namespace, &item.Resource.Name, &item.TimeRange.From, &item.TimeRange.To,
		&item.Bounds.TimeoutMS, &item.Bounds.MaxResponseBytes, &item.Bounds.MaxResults, &item.Bounds.ConcurrencyLimit,
		&item.Status, &item.Source.Identity, &item.Source.ServerVersion, &collectedAt, &item.ResultType,
		&item.ResultCount, &item.ResponseBytes, &partial, &truncated, &item.ErrorCode, &item.ErrorDetail,
		&item.CreatedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return executionRecord{}, ErrNotFound
	}
	if err != nil {
		return executionRecord{}, fmt.Errorf("read telemetry Query Execution: %w", err)
	}
	if json.Unmarshal(namespaces, &item.Scope.Namespaces) != nil || len(item.Scope.Namespaces) == 0 {
		return executionRecord{}, fmt.Errorf("%w: persisted Namespace scope is invalid", ErrUnavailable)
	}
	item.Bounds.MaxLookbackSeconds = int(item.TimeRange.To.Sub(item.TimeRange.From) / time.Second)
	item.Partial, item.Truncated = partial, truncated
	item.Source.Provider = item.Provider
	if collectedAt.Valid {
		item.Source.CollectedAt = collectedAt.Time.UTC()
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		item.CompletedAt = &value
	}
	item.CreatedAt, item.TimeRange.From, item.TimeRange.To = item.CreatedAt.UTC(), item.TimeRange.From.UTC(), item.TimeRange.To.UTC()
	return item, nil
}

type evidenceInsert struct {
	QueryID        string
	Type           string
	Summary        string
	Facts          []byte
	FactCount      int
	ContentHash    string
	ScopeHash      string
	ArgumentsHash  string
	Provenance     []byte
	ProvenanceHash string
	Truncated      bool
	ObservedAt     time.Time
}

func (r *Repository) RetainEvidence(ctx context.Context, execution executionRecord, input evidenceInsert) (Evidence, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Evidence{}, err
	}
	defer func() { _ = tx.Rollback() }()
	executionID, _, err := queryExecutionIdentity(ctx, tx, execution.ID, true)
	if err != nil {
		return Evidence{}, err
	}
	if existing, ok, err := existingEvidence(ctx, tx, executionID, input.ContentHash); err != nil {
		return Evidence{}, err
	} else if ok {
		return existing, nil
	}
	publicID := uuid.NewString()
	window, _ := json.Marshal(execution.TimeRange)
	trust := []byte(`{"source_identity":"provider","query_identity":"exact","scope_identity":"exact","freshness":"captured"}`)
	groups := []byte(`[["provider-query"]]`)
	empty := []byte(`[]`)
	redaction := []byte(`{"policy":"workspace-evidence/v1","secrets":"removed","fields":"allowlisted"}`)
	redactionCounts := []byte(`{"secret_fields":0}`)
	promptFlags := []byte(`{"untrusted_content":true,"instruction_text_not_executed":true}`)
	collectedAt := execution.Source.CollectedAt
	if collectedAt.IsZero() {
		collectedAt = r.now().UTC()
	}
	if input.ObservedAt.IsZero() {
		input.ObservedAt = collectedAt
	}
	factSchemaHash := sha256Text("cloudops.workspace-evidence/facts/v1")
	result, err := tx.ExecContext(ctx, `INSERT INTO evidence_items
 (public_id, incident_id, evidence_contract_version, cycle_no, query_execution_id, type, source,
  producer_type, producer_id, producer_version, producer_dedupe_key, adapter_version,
  query_template_id, query_template_version, scope_snapshot_hash, arguments_hash, tool_name,
  resource_ref, time_range_json, query_text, summary, facts_json, fact_schema_version,
  fact_schema_hash, provenance_json, provenance_hash, trust_axes_json, claim_use,
  corroboration_groups_json, input_evidence_ids_json, input_sample_ids_json, input_hashes_json,
  result_hash, content_hash, raw_ref, safe_raw_reference, redaction_json,
  redaction_policy_version, redaction_counts_json, prompt_safety_flags_json, truncated, valid,
  migrated_legacy, migrated_legacy_context, idempotency_key, collected_at, observed_at, created_at)
	 VALUES (?, NULL, 1, NULL, ?, ?, ?, 'query_execution', ?, 'workspace-query/v1', ?,
  'provider-gateway/v1', ?, '1', ?, ?, 'workspace.telemetry.retain', ?, ?, ?, ?, ?, 1, ?,
  ?, ?, ?, 'context', ?, ?, ?, ?, ?, ?, ?, ?, ?, 'workspace-evidence/v1', ?, ?, ?, 1, 0, 0, ?, ?, ?, ?)`,
		publicID, executionID, input.Type, execution.Provider, execution.ID, input.ContentHash,
		execution.Provider+"/bounded-query", input.ScopeHash, input.ArgumentsHash,
		execution.Resource.ID, window, execution.Query, input.Summary, input.Facts, factSchemaHash,
		input.Provenance, input.ProvenanceHash, trust, groups, empty, empty, empty,
		input.ContentHash, input.ContentHash, "query-execution:"+execution.ID, "query-execution:"+execution.ID,
		redaction, redactionCounts, promptFlags, input.Truncated, input.ContentHash,
		collectedAt, input.ObservedAt.UTC(), r.now().UTC())
	if err != nil {
		return Evidence{}, fmt.Errorf("persist retained Workspace Evidence: %w", err)
	}
	if _, err = result.LastInsertId(); err != nil {
		return Evidence{}, err
	}
	if err = tx.Commit(); err != nil {
		return Evidence{}, fmt.Errorf("commit retained Workspace Evidence: %w", err)
	}
	return evidenceProjection(publicID, execution, input, collectedAt), nil
}

func (r *Repository) EvidenceForExecution(ctx context.Context, queryID string) ([]Evidence, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT e.public_id, e.type, e.source, qe.public_id, cr.public_id,
 qe.cluster_id, qe.environment, qe.namespaces_json, qe.resource_id, qe.resource_kind, qe.resource_namespace,
 qe.resource_name, qe.range_start, qe.range_end, e.summary,
 COALESCE(JSON_LENGTH(JSON_EXTRACT(e.facts_json,'$.facts')), 0), e.content_hash, e.truncated, e.collected_at
 FROM evidence_items e JOIN query_executions qe ON qe.id=e.query_execution_id
 JOIN configuration_revisions cr ON cr.id=qe.configuration_revision_id
 WHERE qe.public_id=?
 ORDER BY e.collected_at DESC, e.id DESC
 LIMIT 100`, strings.TrimSpace(queryID))
	if err != nil {
		return nil, fmt.Errorf("list retained Workspace Evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []Evidence{}
	for rows.Next() {
		item, scanErr := scanEvidence(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func existingEvidence(ctx context.Context, tx *sql.Tx, executionID int64, contentHash string) (Evidence, bool, error) {
	item, err := scanEvidence(tx.QueryRowContext(ctx, `SELECT e.public_id, e.type, e.source, qe.public_id, cr.public_id,
 qe.cluster_id, qe.environment, qe.namespaces_json, qe.resource_id, qe.resource_kind, qe.resource_namespace,
	 qe.resource_name, qe.range_start, qe.range_end, e.summary,
	 COALESCE(JSON_LENGTH(JSON_EXTRACT(e.facts_json,'$.facts')), 0),
 e.content_hash, e.truncated, e.collected_at
 FROM evidence_items e JOIN query_executions qe ON qe.id=e.query_execution_id
 JOIN configuration_revisions cr ON cr.id=qe.configuration_revision_id
	 WHERE e.query_execution_id=? AND e.content_hash=? LIMIT 1`, executionID, contentHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Evidence{}, false, nil
	}
	if err != nil {
		return Evidence{}, false, err
	}
	return item, true, nil
}

func scanEvidence(row rowScanner) (Evidence, error) {
	var item Evidence
	var scopeJSON []byte
	var resourceID, resourceKind, resourceNamespace, resourceName string
	var from, to time.Time
	err := row.Scan(
		&item.ID, &item.Type, &item.Source, &item.QueryID, &item.ConfigurationRevision,
		&item.Scope.ClusterID, &item.Scope.Environment, &scopeJSON, &resourceID, &resourceKind,
		&resourceNamespace, &resourceName, &from, &to, &item.Summary, &item.ItemCount,
		&item.ContentHash, &item.Truncated, &item.CollectedAt)
	if err != nil {
		return Evidence{}, err
	}
	if json.Unmarshal(scopeJSON, &item.Scope.Namespaces) != nil {
		return Evidence{}, ErrUnavailable
	}
	item.Resource = ResourceReference{ID: resourceID, Kind: resourceKind, Namespace: resourceNamespace, Name: resourceName}
	item.TimeRange = TimeRange{From: from.UTC(), To: to.UTC()}
	item.CollectedAt = item.CollectedAt.UTC()
	return item, nil
}

func evidenceProjection(publicID string, execution executionRecord, input evidenceInsert, collectedAt time.Time) Evidence {
	return Evidence{
		ID: publicID, Type: input.Type, Source: execution.Provider, QueryID: execution.ID,
		ConfigurationRevision: execution.ConfigurationRevision,
		Scope:                 settingsScope(execution.Scope), Resource: execution.Resource, TimeRange: execution.TimeRange,
		Summary: input.Summary, ItemCount: input.FactCount, ContentHash: input.ContentHash,
		Truncated: input.Truncated, CollectedAt: collectedAt.UTC(),
	}
}

func (r *Repository) CreateConsultation(ctx context.Context, revisionID string, request CreateConsultationRequest, contentHash string) (Consultation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Consultation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	configurationID, err := internalID(ctx, tx, "configuration_revisions", revisionID)
	if err != nil {
		return Consultation{}, err
	}
	consultationID, snapshotID := uuid.NewString(), uuid.NewString()
	namespaces, _ := json.Marshal(request.Namespaces)
	resources, _ := json.Marshal(request.Resources)
	filters := request.Filters
	if len(filters) == 0 {
		filters = json.RawMessage(`{}`)
	}
	definitionIDs := request.DefinitionIDs
	if definitionIDs == nil {
		definitionIDs = []string{}
	}
	filtersJSON, _ := json.Marshal(json.RawMessage(filters))
	definitionIDsJSON, _ := json.Marshal(definitionIDs)
	queryIDs, _ := json.Marshal(nonNilStrings(request.QueryIDs))
	evidenceIDs, _ := json.Marshal(nonNilStrings(request.EvidenceIDs))
	now := r.now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_consultations
 (public_id, title, status, created_by, created_at, updated_at)
 VALUES (?, ?, 'open', 'local-owner', ?, ?)`, consultationID, request.Title, now, now)
	if err != nil {
		return Consultation{}, fmt.Errorf("persist Agent Consultation: %w", err)
	}
	internalConsultationID, err := result.LastInsertId()
	if err != nil {
		return Consultation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO context_snapshots
 (public_id, consultation_id, agent_run_id, subject_type, configuration_revision_id, cluster_id, environment, namespaces_json,
  resource_refs_json, filters_json, range_start, range_end, query_definition_refs_json, query_execution_refs_json, evidence_refs_json,
  content_hash, created_by, created_at)
 VALUES (?, ?, NULL, 'consultation', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'local-owner', ?)`, snapshotID, internalConsultationID,
		configurationID, request.ClusterID, request.Environment, namespaces, resources, filtersJSON,
		request.From.UTC(), request.To.UTC(), definitionIDsJSON, queryIDs, evidenceIDs, contentHash, now); err != nil {
		return Consultation{}, fmt.Errorf("persist Context Snapshot: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Consultation{}, fmt.Errorf("commit Agent Consultation Context Snapshot: %w", err)
	}
	scope := settingsScope(scopeRecord{ClusterID: request.ClusterID, Environment: request.Environment, Namespaces: append([]string(nil), request.Namespaces...)})
	return Consultation{
		ID: consultationID, Title: request.Title, Status: "open", CreatedAt: now,
		Snapshot: ContextSnapshot{
			ID: snapshotID, ConsultationID: consultationID, ConfigurationRevision: revisionID,
			Scope: scope, Resources: append([]ResourceReference(nil), request.Resources...),
			Filters:       append(json.RawMessage(nil), request.Filters...),
			TimeRange:     TimeRange{From: request.From.UTC(), To: request.To.UTC()},
			DefinitionIDs: append([]string(nil), request.DefinitionIDs...), QueryIDs: append([]string(nil), request.QueryIDs...),
			EvidenceIDs: append([]string(nil), request.EvidenceIDs...),
			ContentHash: contentHash, CreatedAt: now,
		},
	}, nil
}

func (r *Repository) AttachContextSnapshot(ctx context.Context, consultationPublicID, revisionID string, request CreateConsultationRequest, contentHash string) (ContextSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ContextSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var consultationID uint64
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT id,status FROM agent_consultations WHERE public_id=? FOR UPDATE`, consultationPublicID).Scan(&consultationID, &status); errors.Is(err, sql.ErrNoRows) {
		return ContextSnapshot{}, ErrNotFound
	} else if err != nil {
		return ContextSnapshot{}, err
	}
	if status != "open" {
		return ContextSnapshot{}, ErrConflict
	}
	configurationID, err := internalID(ctx, tx, "configuration_revisions", revisionID)
	if err != nil {
		return ContextSnapshot{}, err
	}
	namespaces, _ := json.Marshal(request.Namespaces)
	resources, _ := json.Marshal(request.Resources)
	filters := request.Filters
	if len(filters) == 0 {
		filters = json.RawMessage(`{}`)
	}
	definitions := request.DefinitionIDs
	if definitions == nil {
		definitions = []string{}
	}
	filtersJSON, _ := json.Marshal(json.RawMessage(filters))
	definitionsJSON, _ := json.Marshal(definitions)
	queries, _ := json.Marshal(nonNilStrings(request.QueryIDs))
	evidence, _ := json.Marshal(nonNilStrings(request.EvidenceIDs))
	publicID := uuid.NewString()
	now := r.now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO context_snapshots
 (public_id,consultation_id,agent_run_id,subject_type,configuration_revision_id,cluster_id,environment,
  namespaces_json,resource_refs_json,filters_json,range_start,range_end,query_definition_refs_json,
  query_execution_refs_json,evidence_refs_json,content_hash,created_by,created_at)
	 VALUES (?,?,NULL,'consultation',?,?,?,?,?,?,?,?,?,?,?,?,'local-owner',?)
 ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, publicID, consultationID, configurationID,
		request.ClusterID, request.Environment, namespaces, resources, filtersJSON, request.From.UTC(), request.To.UTC(),
		definitionsJSON, queries, evidence, contentHash, now)
	if err != nil {
		return ContextSnapshot{}, fmt.Errorf("persist explicit Context Snapshot: %w", err)
	}
	internalSnapshotID, err := result.LastInsertId()
	if err != nil || internalSnapshotID <= 0 {
		return ContextSnapshot{}, fmt.Errorf("read explicit Context Snapshot identity: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT public_id,created_at FROM context_snapshots WHERE id=?`, internalSnapshotID).Scan(&publicID, &now); err != nil {
		return ContextSnapshot{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_consultations SET updated_at=? WHERE id=?`, now, consultationID); err != nil {
		return ContextSnapshot{}, err
	}
	if err = tx.Commit(); err != nil {
		return ContextSnapshot{}, fmt.Errorf("commit explicit Context Snapshot: %w", err)
	}
	return ContextSnapshot{
		ID: publicID, ConsultationID: consultationPublicID, ConfigurationRevision: revisionID,
		Scope:     settingsScope(scopeRecord{ClusterID: request.ClusterID, Environment: request.Environment, Namespaces: append([]string(nil), request.Namespaces...)}),
		Resources: append([]ResourceReference(nil), request.Resources...), Filters: append(json.RawMessage(nil), request.Filters...),
		TimeRange: TimeRange{From: request.From.UTC(), To: request.To.UTC()}, DefinitionIDs: append([]string(nil), request.DefinitionIDs...),
		QueryIDs: append([]string(nil), request.QueryIDs...), EvidenceIDs: append([]string(nil), request.EvidenceIDs...),
		ContentHash: contentHash, CreatedAt: now.UTC(),
	}, nil
}

func (r *Repository) ValidateSnapshotReferences(ctx context.Context, revisionID string, request CreateConsultationRequest) error {
	for _, id := range request.DefinitionIDs {
		var actualRevision, clusterID, environment, resourceID, resourceNamespace string
		var namespacesJSON []byte
		err := r.db.QueryRowContext(ctx, `SELECT revision.public_id,definition.cluster_id,definition.environment,
definition.namespaces_json,definition.resource_id,definition.resource_namespace
FROM query_definitions definition JOIN configuration_revisions revision ON revision.id=definition.configuration_revision_id
WHERE definition.public_id=?`, id).Scan(&actualRevision, &clusterID, &environment, &namespacesJSON, &resourceID, &resourceNamespace)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("validate Context Snapshot Query Definition reference: %w", err)
		}
		var namespaces []string
		if json.Unmarshal(namespacesJSON, &namespaces) != nil || actualRevision != revisionID || clusterID != request.ClusterID ||
			environment != request.Environment || !slices.Contains(request.Namespaces, resourceNamespace) ||
			!slices.Contains(namespaces, resourceNamespace) || !snapshotHasResource(request.Resources, resourceID) {
			return ErrConflict
		}
	}
	for _, id := range request.QueryIDs {
		var actualRevision, status, clusterID, environment string
		var resource ResourceReference
		var from, to time.Time
		err := r.db.QueryRowContext(ctx, `SELECT cr.public_id, qe.status, qe.cluster_id, qe.environment,
 qe.resource_id, qe.resource_kind, qe.resource_namespace, qe.resource_name, qe.range_start, qe.range_end
 FROM query_executions qe JOIN configuration_revisions cr ON cr.id=qe.configuration_revision_id
 WHERE qe.public_id=?`, id).Scan(&actualRevision, &status, &clusterID, &environment,
			&resource.ID, &resource.Kind, &resource.Namespace, &resource.Name, &from, &to)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("validate Context Snapshot Query Execution reference: %w", err)
		}
		if status != "succeeded" || actualRevision != revisionID || !snapshotContains(request, clusterID, environment, resource, from, to) {
			return ErrConflict
		}
	}
	for _, id := range request.EvidenceIDs {
		var actualRevision, clusterID, environment string
		var resource ResourceReference
		var from, to time.Time
		var valid bool
		err := r.db.QueryRowContext(ctx, `SELECT cr.public_id, e.valid, qe.cluster_id, qe.environment,
 qe.resource_id, qe.resource_kind, qe.resource_namespace, qe.resource_name, qe.range_start, qe.range_end
 FROM evidence_items e JOIN query_executions qe ON qe.id=e.query_execution_id
 JOIN configuration_revisions cr ON cr.id=qe.configuration_revision_id WHERE e.public_id=?`, id).Scan(
			&actualRevision, &valid, &clusterID, &environment, &resource.ID, &resource.Kind,
			&resource.Namespace, &resource.Name, &from, &to)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("validate Context Snapshot Evidence reference: %w", err)
		}
		if !valid || actualRevision != revisionID || !snapshotContains(request, clusterID, environment, resource, from, to) {
			return ErrConflict
		}
	}
	return nil
}

func snapshotHasResource(resources []ResourceReference, id string) bool {
	for _, resource := range resources {
		if resource.ID == id {
			return true
		}
	}
	return false
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func snapshotContains(request CreateConsultationRequest, clusterID, environment string, resource ResourceReference, from, to time.Time) bool {
	if request.ClusterID != clusterID || request.Environment != environment || request.From.After(from) || request.To.Before(to) ||
		!slices.Contains(request.Namespaces, resource.Namespace) {
		return false
	}
	return slices.Contains(request.Resources, resource)
}

func internalID(ctx context.Context, tx *sql.Tx, table, publicID string) (int64, error) {
	if table != "configuration_revisions" {
		return 0, errors.New("unsupported internal identity table")
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM configuration_revisions WHERE public_id=?`, publicID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, fmt.Errorf("resolve Configuration Revision identity: %w", err)
	}
	return id, nil
}

func queryExecutionIdentity(ctx context.Context, tx *sql.Tx, publicID string, lock bool) (int64, string, error) {
	query := `SELECT id, status FROM query_executions WHERE public_id=?`
	if lock {
		query += " FOR UPDATE"
	}
	var id int64
	var status string
	if err := tx.QueryRowContext(ctx, query, publicID).Scan(&id, &status); errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	} else if err != nil {
		return 0, "", fmt.Errorf("resolve Query Execution identity: %w", err)
	}
	return id, status, nil
}

func providerErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrProviderDisabled):
		return "PROVIDER_DISABLED"
	case errors.Is(err, ErrBoundExceeded):
		return "RESULT_BOUND_EXCEEDED"
	case errors.Is(err, context.DeadlineExceeded):
		return "PROVIDER_TIMEOUT"
	case errors.Is(err, context.Canceled):
		return "QUERY_CANCELLED"
	default:
		return "PROVIDER_UNAVAILABLE"
	}
}

func detailIfFailed(err error, detail string) string {
	if err == nil {
		return ""
	}
	return boundedDetail(detail, 512)
}

func boundedDetail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func settingsScope(value scopeRecord) settings.OperationalScope {
	return settings.OperationalScope{
		ClusterID: value.ClusterID, Environment: value.Environment,
		Namespaces: append([]string(nil), value.Namespaces...),
	}
}
