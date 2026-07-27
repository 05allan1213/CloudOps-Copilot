package alert

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/google/uuid"
)

type insertedSignal struct {
	Input SignalInput
	ID    uint64
	New   bool
}

func (s *Service) IngestBatch(ctx context.Context, signals []SignalInput) ([]IngestResult, error) {
	if len(signals) == 0 || len(signals) > maxBatchSignals {
		return nil, fmt.Errorf("signal batch must contain 1..%d alerts", maxBatchSignals)
	}
	groups := make(map[string][]SignalInput)
	for _, signal := range signals {
		if err := validateSignal(signal); err != nil {
			return nil, err
		}
		key := hashCanonical("alert", signal.Source, signal.Fingerprint)
		groups[key] = append(groups[key], signal)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]IngestResult, 0, len(signals))
	for _, key := range keys {
		group := groups[key]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].OccurredAt.Equal(group[j].OccurredAt) {
				return group[i].Status == domain.SignalStatusFiring
			}
			return group[i].OccurredAt.Before(group[j].OccurredAt)
		})
		groupResults, err := s.ingestGroup(ctx, key, group)
		if err != nil {
			return nil, err
		}
		results = append(results, groupResults...)
	}
	return results, nil
}

func (s *Service) ingestGroup(ctx context.Context, alertKey string, inputs []SignalInput) ([]IngestResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_ingress_locks (alert_key, touched_at)
VALUES (?, NOW(6)) ON DUPLICATE KEY UPDATE touched_at = NOW(6)`, alertKey); err != nil {
		return nil, err
	}
	var lockedKey string
	if err := tx.QueryRowContext(ctx, `SELECT alert_key FROM alert_ingress_locks WHERE alert_key = ? FOR UPDATE`, alertKey).Scan(&lockedKey); err != nil || lockedKey != alertKey {
		return nil, fmt.Errorf("lock Alert identity: %w", err)
	}

	results := make([]IngestResult, 0, len(inputs))
	for _, input := range inputs {
		signal, duplicate, err := insertSignal(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		if duplicate {
			result, err := duplicateIngestResult(ctx, tx, input.Source, input.SourceEventID)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
			continue
		}
		result, err := applySignal(ctx, tx, alertKey, signal)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := s.evaluateEscalationPolicies(ctx, tx, alertKey); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func insertSignal(ctx context.Context, tx *sql.Tx, input SignalInput) (insertedSignal, bool, error) {
	labels, annotations := input.Labels, input.Annotations
	if len(labels) == 0 {
		labels = json.RawMessage(`{}`)
	}
	if len(annotations) == 0 {
		annotations = json.RawMessage(`{}`)
	}
	input.Labels = labels
	input.Annotations = annotations
	var endsAt any
	if input.EndsAt != nil {
		endsAt = input.EndsAt.UTC()
	}
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO incident_signals
(public_id, incident_id, cycle_no, source, source_event_id, fingerprint, alert_instance_key,
 status, severity, cluster, namespace, service_name, environment, target_kind, target_name,
 category, occurred_at, starts_at, ends_at, received_at, summary, labels_json, annotations_json, raw_payload)
VALUES (?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6), ?, ?, ?, NULL)`,
		uuid.NewString(), input.Source, input.SourceEventID, input.Fingerprint, input.AlertInstanceKey,
		input.Status, input.Severity, input.Cluster, input.Namespace, input.ServiceName, input.Environment,
		input.TargetKind, input.TargetName, input.Category, input.OccurredAt.UTC(), input.StartsAt.UTC(),
		endsAt, input.Summary, labels, annotations)
	if err != nil {
		return insertedSignal{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return insertedSignal{}, false, err
	}
	if affected == 0 {
		return insertedSignal{Input: input}, true, nil
	}
	id, err := result.LastInsertId()
	return insertedSignal{Input: input, ID: uint64(id), New: true}, false, err
}

func duplicateIngestResult(ctx context.Context, tx *sql.Tx, source, sourceEventID string) (IngestResult, error) {
	result := IngestResult{SourceEventID: sourceEventID, Duplicate: true}
	var alertID, incidentID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT alert.public_id,
 (SELECT incident.public_id FROM alert_incident_links relation
  JOIN incidents incident ON incident.id = relation.incident_id
  WHERE relation.alert_id = alert.id ORDER BY relation.id DESC LIMIT 1)
FROM incident_signals source_signal
LEFT JOIN alert_signal_links link ON link.signal_id = source_signal.id
LEFT JOIN alerts alert ON alert.id = link.alert_id
WHERE source_signal.source = ? AND source_signal.source_event_id = ?`, source, sourceEventID).Scan(&alertID, &incidentID)
	if err != nil {
		return IngestResult{}, err
	}
	result.AlertPublicID = alertID.String
	result.IncidentPublicID = incidentID.String
	return result, nil
}

func applySignal(ctx context.Context, tx *sql.Tx, alertKey string, signal insertedSignal) (IngestResult, error) {
	input := signal.Input
	row, err := loadAlertByKey(ctx, tx, alertKey, true)
	if errors.Is(err, sql.ErrNoRows) && input.Status == domain.SignalStatusResolved {
		if err := rejectUnmatchedResolved(ctx, tx, signal); err != nil {
			return IngestResult{}, err
		}
		return IngestResult{SourceEventID: input.SourceEventID, Rejected: true, RejectionReason: "unmatched_resolved"}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return IngestResult{}, err
	}
	created := errors.Is(err, sql.ErrNoRows)
	previousInstanceKey := row.InstanceKey
	previousSeverity := row.Severity
	if created {
		row, err = createAlert(ctx, tx, alertKey, signal)
	} else {
		err = updateAlertFromSignal(ctx, tx, &row, signal)
	}
	if err != nil {
		return IngestResult{}, err
	}
	provenance := "signal_normalization"
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_signal_links
(public_id, alert_id, signal_id, provenance, created_at) VALUES (?, ?, ?, ?, NOW(6))`,
		uuid.NewString(), row.ID, signal.ID, provenance); err != nil {
		return IngestResult{}, err
	}
	var eventType string
	if created {
		eventType = "alert_created"
	} else if input.Status == domain.SignalStatusResolved && input.AlertInstanceKey == previousInstanceKey {
		eventType = "alert_resolved"
	} else if input.Status == domain.SignalStatusResolved {
		eventType = "alert_stale_resolution_observed"
	} else if input.AlertInstanceKey == previousInstanceKey {
		eventType = "alert_firing_observed"
	} else {
		eventType = "alert_recurred"
	}
	if err := appendAlertEvent(ctx, tx, row, eventType, "source", input.SourceEventID, input.Summary,
		signal.ID, input.OccurredAt, map[string]any{"source": input.Source, "source_event_id": input.SourceEventID, "status": input.Status},
		hashCanonical("alert-event", input.Source, input.SourceEventID)); err != nil {
		return IngestResult{}, err
	}
	severityIncreased := severityRank(row.Severity) > severityRank(previousSeverity)
	if (created || eventType == "alert_recurred" || severityIncreased) && row.Status == "firing" {
		if err := createOwnerNotification(ctx, tx, row); err != nil {
			return IngestResult{}, err
		}
	}
	return IngestResult{SourceEventID: input.SourceEventID, AlertPublicID: row.PublicID}, nil
}

func severityRank(value string) int {
	switch value {
	case "critical":
		return 4
	case "warning":
		return 3
	case "info":
		return 2
	default:
		return 1
	}
}

func createAlert(ctx context.Context, tx *sql.Tx, alertKey string, signal insertedSignal) (alertRow, error) {
	input := signal.Input
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO alerts
(public_id, source, alert_key, current_alert_instance_key, correlation_key, correlation_key_version,
 fingerprint, status, severity, cluster, environment, namespace, service_name, target_kind,
 target_name, category, summary, labels_json, annotations_json, first_seen_at, last_seen_at,
 starts_at, resolved_at, recurrence_count, signal_count, row_version, migrated_legacy,
 migrated_legacy_context, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 2, ?, 'firing', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 1, 1, 1, 0, 0, NOW(6), NOW(6))`,
		publicID, input.Source, alertKey, input.AlertInstanceKey, input.CorrelationKey, input.Fingerprint,
		input.Severity, input.Cluster, input.Environment, input.Namespace, input.ServiceName, input.TargetKind,
		input.TargetName, input.Category, input.Summary, input.Labels, input.Annotations,
		input.OccurredAt.UTC(), input.OccurredAt.UTC(), input.StartsAt.UTC())
	if err != nil {
		return alertRow{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return alertRow{}, err
	}
	return alertRow{
		ID: uint64(id), PublicID: publicID, Source: input.Source, AlertKey: alertKey,
		InstanceKey: input.AlertInstanceKey, CorrelationKey: input.CorrelationKey, Fingerprint: input.Fingerprint,
		Status: "firing", Severity: string(input.Severity), Cluster: input.Cluster, Environment: input.Environment,
		Namespace: input.Namespace, Service: input.ServiceName, TargetKind: input.TargetKind,
		TargetName: input.TargetName, Category: input.Category, Summary: input.Summary,
		Labels: input.Labels, Annotations: input.Annotations, FirstSeen: input.OccurredAt.UTC(),
		LastSeen: input.OccurredAt.UTC(), StartsAt: input.StartsAt.UTC(), Recurrence: 1, SignalCount: 1,
		Version: 1,
	}, nil
}

func updateAlertFromSignal(ctx context.Context, tx *sql.Tx, row *alertRow, signal insertedSignal) error {
	input := signal.Input
	row.SignalCount++
	row.LastSeen = input.OccurredAt.UTC()
	if input.Status == domain.SignalStatusFiring {
		if input.AlertInstanceKey != row.InstanceKey {
			row.Recurrence++
			row.InstanceKey = input.AlertInstanceKey
			row.StartsAt = input.StartsAt.UTC()
		}
		row.Status = "firing"
		row.ResolvedAt = sql.NullTime{}
		row.Severity = string(input.Severity)
		row.Summary, row.Labels, row.Annotations = input.Summary, input.Labels, input.Annotations
		row.Cluster, row.Environment, row.Namespace = input.Cluster, input.Environment, input.Namespace
		row.Service, row.TargetKind, row.TargetName, row.Category = input.ServiceName, input.TargetKind, input.TargetName, input.Category
	} else if input.AlertInstanceKey == row.InstanceKey {
		row.Status = "resolved"
		row.ResolvedAt = sql.NullTime{Time: input.EndsAt.UTC(), Valid: true}
		row.Summary, row.Labels, row.Annotations = input.Summary, input.Labels, input.Annotations
	}
	row.Version++
	result, err := tx.ExecContext(ctx, `UPDATE alerts SET current_alert_instance_key = ?, status = ?, severity = ?,
cluster = ?, environment = ?, namespace = ?, service_name = ?, target_kind = ?, target_name = ?,
category = ?, summary = ?, labels_json = ?, annotations_json = ?, last_seen_at = ?, starts_at = ?,
resolved_at = ?, recurrence_count = ?, signal_count = ?, row_version = ?, updated_at = NOW(6)
WHERE id = ? AND row_version = ?`, row.InstanceKey, row.Status, row.Severity, row.Cluster, row.Environment,
		row.Namespace, row.Service, row.TargetKind, row.TargetName, row.Category, row.Summary, row.Labels,
		row.Annotations, row.LastSeen, row.StartsAt, nullTimeValue(row.ResolvedAt), row.Recurrence,
		row.SignalCount, row.Version, row.ID, row.Version-1)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrConflict
	}
	return nil
}

func loadAlertByKey(ctx context.Context, tx *sql.Tx, alertKey string, lock bool) (alertRow, error) {
	query := "SELECT " + alertColumns + " FROM alerts WHERE alert_key = ?"
	if lock {
		query += " FOR UPDATE"
	}
	return scanAlertRow(tx.QueryRowContext(ctx, query, alertKey))
}

func rejectUnmatchedResolved(ctx context.Context, tx *sql.Tx, signal insertedSignal) error {
	details, _ := json.Marshal(map[string]string{"status": "resolved", "reason": "no matching firing Alert"})
	_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO signal_rejections
(public_id, source, source_event_id, fingerprint, alert_instance_key, correlation_key,
 reason_code, dedupe_key, payload_hash, details_json, received_at)
VALUES (?, ?, ?, ?, ?, ?, 'unmatched_resolved', ?, ?, ?, NOW(6))`, uuid.NewString(), signal.Input.Source,
		signal.Input.SourceEventID, signal.Input.Fingerprint, signal.Input.AlertInstanceKey,
		signal.Input.CorrelationKey, hashCanonical("rejection", signal.Input.Source, signal.Input.SourceEventID, "unmatched_resolved"),
		hashCanonical("payload", signal.Input.SourceEventID, signal.Input.Summary), details)
	return err
}

func createOwnerNotification(ctx context.Context, tx *sql.Tx, row alertRow) error {
	severity := ""
	switch row.Severity {
	case "critical":
		severity = "P1"
	case "warning":
		severity = "P2"
	default:
		return nil
	}
	var scopeID string
	err := tx.QueryRowContext(ctx, `SELECT scope.public_id
FROM active_configuration active
JOIN operational_scopes scope ON scope.configuration_revision_id = active.configuration_revision_id
WHERE active.singleton_id = 1 AND scope.cluster_id = ?
ORDER BY scope.is_default DESC, scope.id LIMIT 1`, row.Cluster).Scan(&scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	query, _ := json.Marshal(map[string]string{"cluster_id": row.Cluster, "namespace": row.Namespace})
	state := fmt.Sprintf("firing:%d", row.Recurrence)
	dedupe := hashCanonical("alert", row.PublicID, state)
	_, err = tx.ExecContext(ctx, `INSERT INTO owner_notifications
(public_id, source_type, source_public_id, source_state, severity, reason, context_workspace,
 context_path, context_query_json, operational_scope_public_id, dedupe_identity, created_at, updated_at)
VALUES (?, 'alert', ?, ?, ?, ?, 'alerts', ?, ?, ?, ?, NOW(6), NOW(6))
ON DUPLICATE KEY UPDATE severity = VALUES(severity), reason = VALUES(reason), updated_at = NOW(6)`,
		uuid.NewString(), row.PublicID, state, severity, row.Summary, "/alerts/"+row.PublicID,
		query, scopeID, dedupe)
	return err
}

func validateSignal(signal SignalInput) error {
	for name, value := range map[string]string{
		"source": signal.Source, "source event id": signal.SourceEventID, "correlation key": signal.CorrelationKey,
		"fingerprint": signal.Fingerprint, "cluster": signal.Cluster, "environment": signal.Environment,
		"namespace": signal.Namespace, "service": signal.ServiceName, "target kind": signal.TargetKind,
		"target name": signal.TargetName,
	} {
		if strings.TrimSpace(value) == "" || strings.EqualFold(value, "unknown") {
			return fmt.Errorf("%s is missing or unknown", name)
		}
	}
	if len(signal.Source) > 64 || len(signal.SourceEventID) > 67 || len(signal.CorrelationKey) > 67 || len(signal.Fingerprint) > 128 {
		return ErrInvalid
	}
	for _, digest := range []string{signal.AlertInstanceKey, signal.CorrelationKey, signal.SourceEventID} {
		if len(digest) != 64 {
			return ErrInvalid
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return ErrInvalid
		}
	}
	if signal.Status != domain.SignalStatusFiring && signal.Status != domain.SignalStatusResolved {
		return ErrInvalid
	}
	if !domain.IsValidSeverity(signal.Severity) || signal.StartsAt.IsZero() || signal.OccurredAt.IsZero() {
		return ErrInvalid
	}
	if signal.Status == domain.SignalStatusResolved {
		if signal.EndsAt == nil || signal.EndsAt.IsZero() || signal.EndsAt.Before(signal.StartsAt) {
			return ErrInvalid
		}
	} else if signal.EndsAt != nil {
		return ErrInvalid
	}
	if len(signal.Summary) > 2048 || len(signal.Labels) > 8192 || len(signal.Annotations) > 8192 ||
		(len(signal.Labels) > 0 && !json.Valid(signal.Labels)) || (len(signal.Annotations) > 0 && !json.Valid(signal.Annotations)) {
		return ErrInvalid
	}
	return nil
}

func nullTimeValue(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC()
}
