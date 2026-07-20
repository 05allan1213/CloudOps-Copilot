// Package incidentv3mysql owns new V3 Incident transactions without importing
// the legacy claim repositories.
package incidentv3mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

const (
	domainSchemaVersion    = 3
	correlationKeyVersion  = 2
	canonicalSignalVersion = 2
	eventSchemaVersion     = 1
	maxBatchSignals        = 100
	startTaskMaxAttempts   = 5
	maxTransactionAttempts = 3
)

// SignalInput is a validated, allowlist-resolved signal. Arbitrary Alert labels
// must be mapped to these target fields before entering the store.
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

// IngestResult reports the durable identity selected for one source event.
type IngestResult struct {
	SourceEventID    string
	IncidentPublicID string
	CycleNo          uint64
	Duplicate        bool
	Rejected         bool
	RejectionReason  string
	StartTaskCreated bool
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("v3 incident database is required")
	}
	return &Store{db: db}, nil
}

// Ready verifies the exact forward schema owned by this store.
func (s *Store) Ready(ctx context.Context) error {
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil {
		return fmt.Errorf("read goose version: %w", err)
	}
	if !version.Valid || version.Int64 != schemaversion.Latest {
		return fmt.Errorf("unsupported schema version %d, want %d", version.Int64, schemaversion.Latest)
	}
	for _, table := range []string{"async_tasks", "async_task_attempts", "signal_rejections", "command_idempotency_records", "migration_ledger"} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("required table %s is missing", table)
		}
	}
	return nil
}

// IngestBatch persists every correlation group in one short READ COMMITTED
// transaction. Groups are deterministic and independent, matching the webhook
// retry contract.
func (s *Store) IngestBatch(ctx context.Context, signals []SignalInput) ([]IngestResult, error) {
	if len(signals) == 0 || len(signals) > maxBatchSignals {
		return nil, fmt.Errorf("signal batch must contain 1..%d alerts", maxBatchSignals)
	}
	groups := make(map[string][]SignalInput)
	for _, signal := range signals {
		if err := validateSignal(signal); err != nil {
			return nil, err
		}
		groups[signal.CorrelationKey] = append(groups[signal.CorrelationKey], signal)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]IngestResult, 0, len(signals))
	for _, key := range keys {
		group := groups[key]
		sort.Slice(group, func(i, j int) bool { return group[i].SourceEventID < group[j].SourceEventID })
		var groupResults []IngestResult
		var err error
		for attempt := 1; attempt <= maxTransactionAttempts; attempt++ {
			groupResults, err = s.ingestGroup(ctx, key, group)
			if err == nil || !retryableTransactionError(err) || attempt == maxTransactionAttempts {
				break
			}
			timer := time.NewTimer(time.Duration(attempt) * 10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		if err != nil {
			return nil, err
		}
		results = append(results, groupResults...)
	}
	return results, nil
}

func retryableTransactionError(err error) bool {
	var mysqlError *drivermysql.MySQLError
	return errors.As(err, &mysqlError) && (mysqlError.Number == 1205 || mysqlError.Number == 1213)
}

type insertedSignal struct {
	input      SignalInput
	id         uint64
	incidentID uint64
	cycleNo    uint64
	new        bool
}

type incidentRow struct {
	id          uint64
	publicID    string
	cycleNo     uint64
	severity    domain.Severity
	status      domain.V3Status
	version     uint64
	cluster     string
	environment string
	namespace   string
	service     string
	targetKind  string
	targetName  string
}

func (s *Store) ingestGroup(ctx context.Context, correlationKey string, inputs []SignalInput) ([]IngestResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockCorrelation(ctx, tx, correlationKey); err != nil {
		return nil, err
	}

	inserted := make([]insertedSignal, 0, len(inputs))
	results := make([]IngestResult, 0, len(inputs))
	newCount := 0
	for _, input := range inputs {
		row, duplicateResult, err := insertSignalIdentity(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		inserted = append(inserted, row)
		if row.new {
			newCount++
			results = append(results, IngestResult{SourceEventID: input.SourceEventID})
		} else {
			results = append(results, duplicateResult)
		}
	}
	if newCount == 0 {
		return results, tx.Commit()
	}

	incident, err := selectActiveIncident(ctx, tx, correlationKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	createdOrReopened := false
	if errors.Is(err, sql.ErrNoRows) {
		firstFiring, ok := firstFiring(inserted)
		if !ok {
			for index := range inserted {
				if !inserted[index].new {
					continue
				}
				matched, historical, err := attachResolvedToOriginalFiring(ctx, tx, inserted[index])
				if err != nil {
					return nil, err
				}
				if matched {
					if err := appendSignalEvent(ctx, tx, historical, inserted[index].input); err != nil {
						return nil, err
					}
					results[index].IncidentPublicID = historical.publicID
					results[index].CycleNo = historical.cycleNo
					continue
				}
				if err := rejectUnmatchedResolved(ctx, tx, inserted[index]); err != nil {
					return nil, err
				}
				results[index].Rejected = true
				results[index].RejectionReason = "unmatched_resolved"
			}
			return results, tx.Commit()
		}
		firstFiring.Severity = strongestSeverity(inserted)
		incident, createdOrReopened, err = createOrReopenIncident(ctx, tx, firstFiring)
		if err != nil {
			return nil, err
		}
	}

	// Attach firing first so a firing/resolved pair in the same envelope is
	// resolvable independent of source_event_id ordering.
	for index := range inserted {
		if !inserted[index].new || inserted[index].input.Status != domain.SignalStatusFiring {
			continue
		}
		if err := attachSignal(ctx, tx, inserted[index].id, incident); err != nil {
			return nil, err
		}
		inserted[index].incidentID, inserted[index].cycleNo = incident.id, incident.cycleNo
		if err := appendSignalEvent(ctx, tx, incident, inserted[index].input); err != nil {
			return nil, err
		}
		results[index].IncidentPublicID = incident.publicID
		results[index].CycleNo = incident.cycleNo
	}
	for index := range inserted {
		if !inserted[index].new || inserted[index].input.Status != domain.SignalStatusResolved {
			continue
		}
		matched, original, err := attachResolvedToOriginalFiring(ctx, tx, inserted[index])
		if err != nil {
			return nil, err
		}
		if !matched {
			if err := rejectUnmatchedResolved(ctx, tx, inserted[index]); err != nil {
				return nil, err
			}
			results[index].Rejected = true
			results[index].RejectionReason = "unmatched_resolved"
			continue
		}
		if err := appendSignalEvent(ctx, tx, original, inserted[index].input); err != nil {
			return nil, err
		}
		inserted[index].incidentID, inserted[index].cycleNo = original.id, original.cycleNo
		results[index].IncidentPublicID = original.publicID
		results[index].CycleNo = original.cycleNo
	}

	if !createdOrReopened {
		incoming := strongestSeverity(inserted)
		if rankSeverity(incoming) > rankSeverity(incident.severity) {
			result, err := tx.ExecContext(ctx, `UPDATE incidents SET severity = ?, version = version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ?`, incoming, incident.id, incident.cycleNo, incident.version)
			if err != nil {
				return nil, err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return nil, domain.ErrConflict
			}
			incident.severity = incoming
			incident.version++
			if _, err := refreshInvestigationStartVersion(ctx, tx, incident); err != nil {
				return nil, err
			}
		}
	}

	firing, err := countFiringInstances(ctx, tx, incident)
	if err != nil {
		return nil, err
	}
	if firing == 0 {
		if triggerSignalID, ok := noChangeTriggerSignal(inserted, incident); ok {
			if _, err := startNoChangeVerification(ctx, tx, &incident, triggerSignalID); err != nil {
				return nil, err
			}
		}
	}
	if createdOrReopened && firing > 0 {
		created, _, err := enqueueInvestigationStart(ctx, tx, incident)
		if err != nil {
			return nil, err
		}
		if created {
			if err := appendLifecycleEvent(ctx, tx, incident, "investigation_start_enqueued", "system", "v3-ingress", "investigation start task enqueued"); err != nil {
				return nil, err
			}
			for index := range results {
				if results[index].IncidentPublicID == incident.publicID && !results[index].Duplicate {
					results[index].StartTaskCreated = true
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func lockCorrelation(ctx context.Context, tx *sql.Tx, key string) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO incident_correlation_locks (correlation_key, domain_schema_version, correlation_key_version, touched_at)
VALUES (?, 3, 2, NOW(6))
ON DUPLICATE KEY UPDATE touched_at = NOW(6)`, key); err != nil {
		return err
	}
	var schemaVersion, keyVersion sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT domain_schema_version, correlation_key_version
FROM incident_correlation_locks WHERE correlation_key = ? FOR UPDATE`, key).Scan(&schemaVersion, &keyVersion); err != nil {
		return err
	}
	if !schemaVersion.Valid || schemaVersion.Int64 != domainSchemaVersion || !keyVersion.Valid || keyVersion.Int64 != correlationKeyVersion {
		return errors.New("correlation key collides with a legacy lock identity")
	}
	return nil
}

func insertSignalIdentity(ctx context.Context, tx *sql.Tx, input SignalInput) (insertedSignal, IngestResult, error) {
	labels := input.Labels
	if len(labels) == 0 {
		labels = json.RawMessage(`{}`)
	}
	annotations := input.Annotations
	if len(annotations) == 0 {
		annotations = json.RawMessage(`{}`)
	}
	var ends any
	if input.EndsAt != nil {
		ends = input.EndsAt.UTC()
	}
	result, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_signals
    (public_id, incident_id, source, source_event_id, fingerprint, alert_instance_key, status, severity,
     cluster, namespace, service_name, environment, target_kind, target_name, category,
     occurred_at, starts_at, ends_at, received_at, summary, labels_json, annotations_json, raw_payload)
VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6), ?, ?, ?, NULL)`,
		uuid.NewString(), input.Source, input.SourceEventID, input.Fingerprint, input.AlertInstanceKey, input.Status, input.Severity,
		input.Cluster, input.Namespace, input.ServiceName, input.Environment, input.TargetKind, input.TargetName,
		input.Category, input.OccurredAt.UTC(), input.StartsAt.UTC(), ends, input.Summary, labels, annotations)
	if err != nil {
		return insertedSignal{}, IngestResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return insertedSignal{}, IngestResult{}, err
	}
	if affected == 1 {
		id, err := result.LastInsertId()
		return insertedSignal{input: input, id: uint64(id), new: true}, IngestResult{}, err
	}
	var incidentID sql.NullInt64
	var cycle sql.NullInt64
	var publicID sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT s.incident_id, s.cycle_no, i.public_id
FROM incident_signals s LEFT JOIN incidents i ON i.id = s.incident_id
WHERE s.source = ? AND s.source_event_id = ?`, input.Source, input.SourceEventID).Scan(&incidentID, &cycle, &publicID)
	if err != nil {
		return insertedSignal{}, IngestResult{}, err
	}
	duplicate := IngestResult{SourceEventID: input.SourceEventID, Duplicate: true}
	if publicID.Valid {
		duplicate.IncidentPublicID = publicID.String
	}
	if cycle.Valid {
		duplicate.CycleNo = uint64(cycle.Int64)
	}
	return insertedSignal{input: input}, duplicate, nil
}

func selectActiveIncident(ctx context.Context, tx *sql.Tx, key string) (incidentRow, error) {
	var row incidentRow
	err := tx.QueryRowContext(ctx, `
SELECT id, public_id, cycle_no, severity, v3_status, version,
       cluster, environment, namespace, service_name, target_kind, target_name
FROM incidents
WHERE domain_schema_version = 3 AND active_correlation_key = CONVERT(? USING binary)
FOR UPDATE`, key).Scan(&row.id, &row.publicID, &row.cycleNo, &row.severity, &row.status, &row.version,
		&row.cluster, &row.environment, &row.namespace, &row.service, &row.targetKind, &row.targetName)
	return row, err
}

func createOrReopenIncident(ctx context.Context, tx *sql.Tx, input SignalInput) (incidentRow, bool, error) {
	var latest incidentRow
	var withinWindow bool
	err := tx.QueryRowContext(ctx, `
SELECT id, public_id, cycle_no, severity, v3_status, version,
       (v3_status = 'resolved' AND terminal_at >= TIMESTAMPADD(MINUTE, -30, NOW(6)))
FROM incidents
WHERE domain_schema_version = 3 AND correlation_key = ? AND v3_status IN ('resolved','closed')
ORDER BY terminal_at DESC, id DESC LIMIT 1 FOR UPDATE`, input.CorrelationKey).
		Scan(&latest.id, &latest.publicID, &latest.cycleNo, &latest.severity, &latest.status, &latest.version, &withinWindow)
	if err == nil && latest.status == domain.V3StatusResolved && withinWindow {
		latest.cluster, latest.environment, latest.namespace = input.Cluster, input.Environment, input.Namespace
		latest.service, latest.targetKind, latest.targetName = input.ServiceName, input.TargetKind, input.TargetName
		severity := input.Severity
		result, updateErr := tx.ExecContext(ctx, `
	UPDATE incidents
	SET cycle_no = cycle_no + 1, version = version + 1, v3_status = 'investigating',
	    severity = ?, resolved_at = NULL, terminal_at = NULL, needs_attention = FALSE,
	    blocking_reason_code = NULL, blocked_at = NULL, current_agent_run_id = NULL,
	    last_seen_at = ?, updated_at = NOW(6)
	WHERE id = ? AND domain_schema_version = 3 AND version = ? AND v3_status = 'resolved'
  AND terminal_at >= TIMESTAMPADD(MINUTE, -30, NOW(6))`, severity, input.OccurredAt.UTC(), latest.id, latest.version)
		if updateErr != nil {
			return incidentRow{}, false, updateErr
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return incidentRow{}, false, domain.ErrConflict
		}
		latest.cycleNo++
		latest.version++
		latest.status = domain.V3StatusInvestigating
		latest.severity = severity
		if err := appendLifecycleEvent(ctx, tx, latest, "incident_reopened", "system", "v3-ingress", "incident reopened in a new cycle"); err != nil {
			return incidentRow{}, false, err
		}
		return latest, true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return incidentRow{}, false, err
	}

	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `
INSERT INTO incidents
    (public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
     service_name, environment, target_kind, target_name, severity, status, summary,
     first_seen_at, last_seen_at, resolved_at, current_agent_run_id, version,
     domain_schema_version, v3_status, cycle_no, needs_attention, created_at, updated_at)
VALUES (?, ?, ?, 2, ?, ?, ?, ?, ?, ?, ?, 'DETECTED', ?, ?, ?, NULL, NULL, 1,
        3, 'detected', 1, FALSE, NOW(6), NOW(6))`,
		publicID, input.Fingerprint, input.CorrelationKey, input.Cluster, input.Namespace,
		input.ServiceName, input.Environment, input.TargetKind, input.TargetName, input.Severity,
		input.Summary, input.OccurredAt.UTC(), input.OccurredAt.UTC())
	if err != nil {
		return incidentRow{}, false, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return incidentRow{}, false, err
	}
	row := incidentRow{
		id: uint64(id), publicID: publicID, cycleNo: 1, severity: input.Severity, status: domain.V3StatusDetected, version: 1,
		cluster: input.Cluster, environment: input.Environment, namespace: input.Namespace,
		service: input.ServiceName, targetKind: input.TargetKind, targetName: input.TargetName,
	}
	if err := appendLifecycleEvent(ctx, tx, row, "incident_created", "system", "v3-ingress", "incident created"); err != nil {
		return incidentRow{}, false, err
	}
	return row, true, nil
}

func attachSignal(ctx context.Context, tx *sql.Tx, signalID uint64, incident incidentRow) error {
	result, err := tx.ExecContext(ctx, `
UPDATE incident_signals
SET incident_id = ?, domain_schema_version = 3, cycle_no = ?, canonical_schema_version = 2,
    correlation_key_version = 2
WHERE id = ? AND incident_id IS NULL AND domain_schema_version IS NULL`, incident.id, incident.cycleNo, signalID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.ErrConflict
	}
	return nil
}

func appendSignalEvent(ctx context.Context, tx *sql.Tx, incident incidentRow, input SignalInput) error {
	metadata, _ := json.Marshal(map[string]any{"source": input.Source, "source_event_id": input.SourceEventID, "status": input.Status})
	return appendEvent(ctx, tx, incident, "signal_received", "source", input.SourceEventID, input.Summary, input.OccurredAt, metadata, hashCanonical("event", input.Source, input.SourceEventID))
}

func appendLifecycleEvent(ctx context.Context, tx *sql.Tx, incident incidentRow, eventType, actorType, actorID, summary string) error {
	metadata, _ := json.Marshal(map[string]any{"status": incident.status, "cycle_no": incident.cycleNo})
	return appendEvent(ctx, tx, incident, eventType, actorType, actorID, summary, time.Time{}, metadata, hashCanonical("lifecycle", incident.publicID, fmt.Sprint(incident.cycleNo), eventType))
}

func appendEvent(ctx context.Context, tx *sql.Tx, incident incidentRow, eventType, actorType, actorID, summary string, occurred time.Time, metadata []byte, idempotency string) error {
	var occurredValue any = occurred.UTC()
	if occurred.IsZero() {
		occurredValue = nil
	}
	_, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
    (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
     event_type, idempotency_key, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, 3, ?, 1, ?, ?, ?, ?, ?, ?, COALESCE(?, NOW(6)), NOW(6))`,
		uuid.NewString(), incident.id, incident.cycleNo, eventType, idempotency, actorType, actorID, summary, metadata, occurredValue)
	return err
}

func enqueueInvestigationStart(ctx context.Context, tx *sql.Tx, incident incidentRow) (bool, bool, error) {
	return enqueueInvestigationStartWithAuthorization(ctx, tx, incident, "")
}

func enqueueInvestigationStartWithAuthorization(ctx context.Context, tx *sql.Tx, incident incidentRow, authorizationPublicID string) (bool, bool, error) {
	var budget businessbudget.Result
	var err error
	if authorizationPublicID == "" {
		budget, err = businessbudget.GuardAutomatic(ctx, tx, businessbudget.KindAgentRun, incident.id, uint32(incident.cycleNo))
	} else {
		budget, err = businessbudget.GuardAgentRun(ctx, tx, incident.id, uint32(incident.cycleNo), authorizationPublicID)
	}
	if err != nil {
		return false, false, err
	}
	if !budget.Allowed() {
		if err := businessbudget.MarkExhausted(ctx, tx, budget, incident.id, uint32(incident.cycleNo), "v3-ingress"); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	payloadBody := map[string]any{"mode": "start", "incident_public_id": incident.publicID, "cycle_no": incident.cycleNo}
	dedupeParts := []string{"task", incident.publicID, fmt.Sprint(incident.cycleNo), "investigation.start", fmt.Sprint(incident.version)}
	if authorizationPublicID != "" {
		payloadBody["business_budget_authorization_id"] = authorizationPublicID
		dedupeParts = append(dedupeParts, authorizationPublicID)
	}
	payload, _ := json.Marshal(payloadBody)
	dedupe := hashCanonical(dedupeParts...)
	result, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO async_tasks
    (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
     expected_subject_version, payload_schema_version, payload_json, dedupe_key,
     replay_generation, status, priority, available_at, attempt, max_attempts, lease_generation)
VALUES (?, ?, ?, 'investigate', 'investigation.advance', 'incident', ?, 'investigation.start',
        ?, 1, ?, ?, 0, 'ready', 100, NOW(6), 0, ?, 0)`,
		uuid.NewString(), incident.id, incident.cycleNo, incident.id, incident.version, payload, dedupe, startTaskMaxAttempts)
	if err != nil {
		return false, false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, false, err
}

// refreshInvestigationStartVersion keeps the single live start transition
// aligned with monotonic Incident metadata updates. A claimed start is fenced
// and replaced because its old Worker must not create a Run from stale state.
func refreshInvestigationStartVersion(ctx context.Context, tx *sql.Tx, incident incidentRow) (bool, error) {
	var active int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND cycle_no = ? AND task_type = 'investigation.advance'
  AND subject_type = 'incident' AND subject_id = ? AND transition = 'investigation.start'
  AND status IN ('ready','running')`, incident.id, incident.cycleNo, incident.id).Scan(&active); err != nil {
		return false, err
	}
	if active == 0 {
		return false, nil
	}
	if active != 1 {
		return false, fmt.Errorf("incident %d cycle %d has %d live investigation.start tasks", incident.id, incident.cycleNo, active)
	}

	var taskID, leaseGeneration, attempt, expectedVersion uint64
	var status string
	var leaseOwner sql.NullString
	var payload []byte
	if err := tx.QueryRowContext(ctx, `
SELECT id, status, lease_owner, lease_generation, attempt, expected_subject_version, payload_json
FROM async_tasks
WHERE incident_id = ? AND cycle_no = ? AND task_type = 'investigation.advance'
  AND subject_type = 'incident' AND subject_id = ? AND transition = 'investigation.start'
  AND status IN ('ready','running')
FOR UPDATE`, incident.id, incident.cycleNo, incident.id).
		Scan(&taskID, &status, &leaseOwner, &leaseGeneration, &attempt, &expectedVersion, &payload); err != nil {
		return false, err
	}
	if expectedVersion == incident.version {
		return false, nil
	}

	metadata, _ := json.Marshal(map[string]any{
		"task_id": taskID, "old_expected_version": expectedVersion,
		"new_expected_version": incident.version, "old_status": status,
	})
	authorizationPublicID, err := investigationStartAuthorization(payload)
	if err != nil {
		return false, err
	}
	if status == "ready" {
		dedupeParts := []string{"task", incident.publicID, fmt.Sprint(incident.cycleNo), "investigation.start", fmt.Sprint(incident.version)}
		if authorizationPublicID != "" {
			dedupeParts = append(dedupeParts, authorizationPublicID)
		}
		dedupe := hashCanonical(dedupeParts...)
		result, err := tx.ExecContext(ctx, `
UPDATE async_tasks
SET expected_subject_version = ?, dedupe_key = ?, updated_at = NOW(6)
WHERE id = ? AND status = 'ready' AND lease_generation = ?
  AND expected_subject_version = ?`, incident.version, dedupe, taskID, leaseGeneration, expectedVersion)
		if err != nil {
			return false, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return false, domain.ErrConflict
		}
		if err := appendEvent(ctx, tx, incident, "investigation_start_refreshed", "system", "v3-ingress",
			"investigation start task refreshed after Incident version change", time.Time{}, metadata,
			hashCanonical("lifecycle", incident.publicID, fmt.Sprint(incident.cycleNo), "investigation_start_refreshed", fmt.Sprint(incident.version))); err != nil {
			return false, err
		}
		return false, nil
	}
	if status != "running" || !leaseOwner.Valid {
		return false, errors.New("live investigation.start task has an invalid lease state")
	}
	attemptResult, err := tx.ExecContext(ctx, `
UPDATE async_task_attempts
SET status = 'cancelled', finished_at = NOW(6), error_code = 'subject_version_changed',
    error_summary = 'Incident version changed before investigation.start completed'
WHERE task_id = ? AND attempt = ? AND lease_owner = ? AND lease_generation = ?
	  AND expected_subject_version = ? AND status = 'running'`,
		taskID, attempt, leaseOwner.String, leaseGeneration, expectedVersion)
	if err != nil {
		return false, err
	}
	if affected, _ := attemptResult.RowsAffected(); affected != 1 {
		return false, domain.ErrConflict
	}
	result, err := tx.ExecContext(ctx, `
UPDATE async_tasks
SET status = 'cancelled', lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL,
    lease_generation = lease_generation + 1, last_error_code = 'subject_version_changed',
    last_error_summary = 'superseded by a newer Incident version', cancelled_at = NOW(6), updated_at = NOW(6)
WHERE id = ? AND status = 'running' AND lease_owner = ? AND lease_generation = ?
  AND expected_subject_version = ?`, taskID, leaseOwner.String, leaseGeneration, expectedVersion)
	if err != nil {
		return false, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return false, domain.ErrConflict
	}
	created, blocked, err := enqueueInvestigationStartWithAuthorization(ctx, tx, incident, authorizationPublicID)
	if err != nil {
		return false, err
	}
	if blocked {
		return false, nil
	}
	if !created {
		return false, errors.New("replacement investigation.start task was not created")
	}
	if err := appendEvent(ctx, tx, incident, "investigation_start_replaced", "system", "v3-ingress",
		"running investigation start task fenced and replaced after Incident version change", time.Time{}, metadata,
		hashCanonical("lifecycle", incident.publicID, fmt.Sprint(incident.cycleNo), "investigation_start_replaced", fmt.Sprint(incident.version))); err != nil {
		return false, err
	}
	return true, nil
}

func investigationStartAuthorization(payload []byte) (string, error) {
	var envelope struct {
		Mode                  string `json:"mode"`
		AuthorizationPublicID string `json:"business_budget_authorization_id"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &envelope) != nil || envelope.Mode != "start" {
		return "", errors.New("investigation.start payload is malformed")
	}
	authorizationPublicID := strings.TrimSpace(envelope.AuthorizationPublicID)
	if len(authorizationPublicID) > 64 {
		return "", errors.New("investigation.start authorization identity is invalid")
	}
	return authorizationPublicID, nil
}

func countFiringInstances(ctx context.Context, tx *sql.Tx, incident incidentRow) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM incident_signals firing
WHERE firing.incident_id = ? AND firing.cycle_no = ? AND firing.status = 'firing'
  AND NOT EXISTS (
      SELECT 1 FROM incident_signals resolved
      WHERE resolved.incident_id = firing.incident_id AND resolved.cycle_no = firing.cycle_no
        AND resolved.alert_instance_key = firing.alert_instance_key AND resolved.status = 'resolved'
  )`, incident.id, incident.cycleNo).Scan(&count)
	return count, err
}

func rejectUnmatchedResolved(ctx context.Context, tx *sql.Tx, signal insertedSignal) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM incident_signals WHERE id = ? AND incident_id IS NULL", signal.id); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]any{"status": signal.input.Status})
	_, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO signal_rejections
    (public_id, source, source_event_id, fingerprint, alert_instance_key, correlation_key,
     reason_code, dedupe_key, payload_hash, details_json, received_at)
VALUES (?, ?, ?, ?, ?, ?, 'unmatched_resolved', ?, ?, ?, NOW(6))`,
		uuid.NewString(), signal.input.Source, signal.input.SourceEventID, signal.input.Fingerprint,
		signal.input.AlertInstanceKey, signal.input.CorrelationKey,
		hashCanonical("rejection", signal.input.Source, signal.input.SourceEventID, "unmatched_resolved"),
		hashCanonical("payload", signal.input.SourceEventID, signal.input.Summary), details)
	return err
}

func attachResolvedToOriginalFiring(ctx context.Context, tx *sql.Tx, signal insertedSignal) (bool, incidentRow, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT s.incident_id, s.cycle_no, i.public_id, i.severity, i.v3_status, i.version
FROM incident_signals s
JOIN incidents i ON i.id = s.incident_id
WHERE s.source = ? AND s.fingerprint = ? AND s.starts_at = ? AND s.status = 'firing'
  AND s.domain_schema_version = 3 AND s.incident_id IS NOT NULL AND s.cycle_no IS NOT NULL
  AND i.domain_schema_version = 3 AND i.correlation_key = ?
ORDER BY s.id
LIMIT 2
FOR UPDATE`, signal.input.Source, signal.input.Fingerprint, signal.input.StartsAt.UTC(), signal.input.CorrelationKey)
	if err != nil {
		return false, incidentRow{}, err
	}
	defer func() { _ = rows.Close() }()
	matches := make([]incidentRow, 0, 2)
	for rows.Next() {
		var row incidentRow
		if err := rows.Scan(&row.id, &row.cycleNo, &row.publicID, &row.severity, &row.status, &row.version); err != nil {
			return false, incidentRow{}, err
		}
		matches = append(matches, row)
	}
	if err := rows.Err(); err != nil {
		return false, incidentRow{}, err
	}
	if err := rows.Close(); err != nil {
		return false, incidentRow{}, err
	}
	if len(matches) != 1 {
		return false, incidentRow{}, nil
	}
	if err := attachSignal(ctx, tx, signal.id, matches[0]); err != nil {
		return false, incidentRow{}, err
	}
	return true, matches[0], nil
}

func firstFiring(signals []insertedSignal) (SignalInput, bool) {
	for _, signal := range signals {
		if signal.new && signal.input.Status == domain.SignalStatusFiring {
			return signal.input, true
		}
	}
	return SignalInput{}, false
}

func strongestSeverity(signals []insertedSignal) domain.Severity {
	result := domain.SeverityUnknown
	for _, signal := range signals {
		if signal.new && signal.input.Status == domain.SignalStatusFiring && rankSeverity(signal.input.Severity) > rankSeverity(result) {
			result = signal.input.Severity
		}
	}
	return result
}

func rankSeverity(value domain.Severity) int {
	switch value {
	case domain.SeverityCritical:
		return 3
	case domain.SeverityWarning:
		return 2
	case domain.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func validateSignal(signal SignalInput) error {
	for name, value := range map[string]string{
		"source": signal.Source, "source event id": signal.SourceEventID,
		"correlation key": signal.CorrelationKey, "fingerprint": signal.Fingerprint,
		"cluster": signal.Cluster, "environment": signal.Environment, "namespace": signal.Namespace,
		"service": signal.ServiceName, "target kind": signal.TargetKind, "target name": signal.TargetName,
	} {
		if strings.TrimSpace(value) == "" || strings.EqualFold(value, "unknown") {
			return fmt.Errorf("%s is missing or unknown", name)
		}
	}
	if len(signal.Source) > 64 || len(signal.SourceEventID) > 67 || len(signal.CorrelationKey) > 67 || len(signal.Fingerprint) > 128 {
		return errors.New("signal identity exceeds schema bounds")
	}
	if len(signal.AlertInstanceKey) != 64 {
		return errors.New("alert instance key must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(signal.AlertInstanceKey); err != nil {
		return fmt.Errorf("alert instance key: %w", err)
	}
	if signal.Status != domain.SignalStatusFiring && signal.Status != domain.SignalStatusResolved {
		return errors.New("signal status must be firing or resolved")
	}
	if !domain.IsValidSeverity(signal.Severity) {
		return errors.New("signal severity is outside the bounded enum")
	}
	if signal.StartsAt.IsZero() || signal.OccurredAt.IsZero() {
		return errors.New("signal starts_at and occurred_at are required")
	}
	if signal.Status == domain.SignalStatusResolved {
		if signal.EndsAt == nil || signal.EndsAt.IsZero() || signal.EndsAt.Before(signal.StartsAt) {
			return errors.New("resolved signal requires a valid ends_at")
		}
	} else if signal.EndsAt != nil {
		return errors.New("firing signal must not carry ends_at")
	}
	if len(signal.Summary) > 2048 || (len(signal.Labels) > 8*1024) || (len(signal.Annotations) > 8*1024) {
		return errors.New("signal payload exceeds bounded fields")
	}
	if (len(signal.Labels) > 0 && !json.Valid(signal.Labels)) || (len(signal.Annotations) > 0 && !json.Valid(signal.Annotations)) {
		return errors.New("signal labels and annotations must be valid JSON")
	}
	return nil
}

func hashCanonical(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
