package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

type linkedIncident struct {
	ID       uint64
	PublicID string
	Cycle    uint64
	Version  uint64
	Status   string
	Created  bool
	Existing bool
}

func (s *Service) LinkIncident(ctx context.Context, request LinkIncidentRequest) (View, error) {
	if err := validateCommand(request.AlertID, request.ExpectedVersion, request.IdempotencyKey, request.Actor); err != nil {
		return View{}, err
	}
	if request.Create == (strings.TrimSpace(request.IncidentID) != "") {
		return View{}, ErrInvalid
	}
	if request.IncidentID != "" {
		if _, err := uuid.Parse(request.IncidentID); err != nil {
			return View{}, ErrInvalid
		}
	}
	eventKey := hashCanonical("alert-incident-link", request.IdempotencyKey)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return View{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := loadAlertByPublicID(ctx, tx, request.AlertID, true)
	if err != nil {
		return View{}, err
	}
	var replay int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_events WHERE alert_id = ? AND idempotency_key = ?`, row.ID, eventKey).Scan(&replay); err != nil {
		return View{}, err
	}
	if replay > 0 {
		if err := tx.Commit(); err != nil {
			return View{}, err
		}
		return s.alertView(ctx, request.AlertID)
	}
	if row.Version != request.ExpectedVersion {
		return View{}, ErrStaleVersion
	}

	var incident linkedIncident
	provenance := "owner_attached"
	if request.Create {
		incident, err = ensureActiveIncident(ctx, tx, row)
		if incident.Created {
			provenance = "owner_created"
		}
	} else {
		incident, err = loadAttachableIncident(ctx, tx, request.IncidentID)
	}
	if err != nil {
		return View{}, err
	}
	created, err := linkAlertIncident(ctx, tx, row, incident, provenance, nil, nil)
	if err != nil {
		return View{}, err
	}
	if !created {
		if err := tx.Commit(); err != nil {
			return View{}, err
		}
		return s.alertView(ctx, request.AlertID)
	}
	if err := incrementAlertVersion(ctx, tx, &row); err != nil {
		return View{}, err
	}
	if err := appendAlertEvent(ctx, tx, row, "alert_incident_linked", "owner", request.Actor.Login,
		"Alert linked to an Incident by the Owner", nil, s.now().UTC(), map[string]any{
			"incident_id": incident.PublicID, "incident_cycle": incident.Cycle, "provenance": provenance,
		}, eventKey); err != nil {
		return View{}, err
	}
	if err := appendIncidentLinkEvent(ctx, tx, incident, row, provenance, request.Actor.Login, eventKey); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return s.alertView(ctx, request.AlertID)
}

func ensureActiveIncident(ctx context.Context, tx *sql.Tx, row alertRow) (linkedIncident, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO incident_correlation_locks
(correlation_key, correlation_key_version, touched_at) VALUES (?, 2, NOW(6))
ON DUPLICATE KEY UPDATE touched_at = NOW(6)`, row.CorrelationKey); err != nil {
		return linkedIncident{}, err
	}
	var lockVersion uint64
	if err := tx.QueryRowContext(ctx, `SELECT correlation_key_version FROM incident_correlation_locks
WHERE correlation_key = ? FOR UPDATE`, row.CorrelationKey).Scan(&lockVersion); err != nil {
		return linkedIncident{}, err
	}
	if lockVersion != 2 {
		return linkedIncident{}, ErrConflict
	}
	var incident linkedIncident
	err := tx.QueryRowContext(ctx, `SELECT id, public_id, cycle_no, version, status
FROM incidents WHERE active_correlation_key = CONVERT(? USING binary) FOR UPDATE`, row.CorrelationKey).
		Scan(&incident.ID, &incident.PublicID, &incident.Cycle, &incident.Version, &incident.Status)
	if err == nil {
		incident.Existing = true
		return incident, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return linkedIncident{}, err
	}
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO incidents
(public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
 service_name, environment, target_kind, target_name, severity, summary, first_seen_at,
 last_seen_at, version, status, cycle_no, needs_attention, migrated_legacy,
 migrated_legacy_context, created_at, updated_at)
VALUES (?, ?, ?, 2, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 'detected', 1, 0, 0, ?, NOW(6), NOW(6))`,
		publicID, row.Fingerprint, row.CorrelationKey, row.Cluster, row.Namespace, row.Service,
		row.Environment, row.TargetKind, row.TargetName, row.Severity, row.Summary,
		row.FirstSeen.UTC(), row.LastSeen.UTC(), row.MigratedLegacyContext)
	if err != nil {
		return linkedIncident{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return linkedIncident{}, err
	}
	incident = linkedIncident{ID: uint64(id), PublicID: publicID, Cycle: 1, Version: 1, Status: "detected", Created: true}
	metadata, _ := json.Marshal(map[string]any{"status": incident.Status, "cycle_no": incident.Cycle, "source_alert_id": row.PublicID})
	_, err = tx.ExecContext(ctx, `INSERT INTO incident_events
(public_id, incident_id, cycle_no, event_schema_version, event_type, idempotency_key,
 migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json,
 occurred_at, created_at)
VALUES (?, ?, ?, 1, 'incident_created', ?, ?, 0, 'system', 'alert-escalation',
 'Incident created from an explicit Alert escalation', ?, NOW(6), NOW(6))`, uuid.NewString(),
		incident.ID, incident.Cycle, hashCanonical("incident-created-from-alert", row.PublicID, incident.PublicID),
		row.MigratedLegacyContext, metadata)
	return incident, err
}

func loadAttachableIncident(ctx context.Context, tx *sql.Tx, publicID string) (linkedIncident, error) {
	var incident linkedIncident
	err := tx.QueryRowContext(ctx, `SELECT id, public_id, cycle_no, version, status
FROM incidents WHERE public_id = ? FOR UPDATE`, publicID).
		Scan(&incident.ID, &incident.PublicID, &incident.Cycle, &incident.Version, &incident.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return linkedIncident{}, ErrNotFound
	}
	if err != nil {
		return linkedIncident{}, err
	}
	if incident.Status != "detected" && incident.Status != "investigating" &&
		incident.Status != "awaiting_approval" && incident.Status != "delivering" && incident.Status != "verifying" {
		return linkedIncident{}, ErrConflict
	}
	return incident, nil
}

func linkAlertIncident(ctx context.Context, tx *sql.Tx, row alertRow, incident linkedIncident, provenance string, revisionID, policyID any) (bool, error) {
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_incident_links
WHERE alert_id = ? AND incident_id = ? AND incident_cycle_no = ?`, row.ID, incident.ID, incident.Cycle).Scan(&existing); err != nil {
		return false, err
	}
	if existing > 0 {
		return false, nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO alert_incident_links
(public_id, alert_id, incident_id, incident_cycle_no, provenance,
 configuration_revision_id, escalation_policy_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NOW(6))`, uuid.NewString(), row.ID, incident.ID, incident.Cycle,
		provenance, revisionID, policyID)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE incident_signals source_signal
JOIN alert_signal_links relation ON relation.signal_id = source_signal.id
SET source_signal.incident_id = ?, source_signal.cycle_no = ?, source_signal.canonical_schema_version = 2,
    source_signal.correlation_key_version = 2
WHERE relation.alert_id = ? AND source_signal.incident_id IS NULL AND source_signal.cycle_no IS NULL
  AND source_signal.canonical_schema_version IS NULL AND source_signal.correlation_key_version IS NULL`,
		incident.ID, incident.Cycle, row.ID)
	return true, err
}

func appendIncidentLinkEvent(ctx context.Context, tx *sql.Tx, incident linkedIncident, row alertRow, provenance, actorID, idempotencyKey string) error {
	metadata, _ := json.Marshal(map[string]any{
		"alert_id": row.PublicID, "alert_status": row.Status, "provenance": provenance,
	})
	actorType := "owner"
	if provenance == "escalation_policy" {
		actorType, actorID = "system", "escalation-policy"
	}
	_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO incident_events
(public_id, incident_id, cycle_no, event_schema_version, event_type, idempotency_key,
 migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json,
 occurred_at, created_at)
VALUES (?, ?, ?, 1, 'alert_linked', ?, ?, 0, ?, ?, 'Alert linked to Incident', ?, NOW(6), NOW(6))`,
		uuid.NewString(), incident.ID, incident.Cycle, hashCanonical("incident-alert-linked", idempotencyKey),
		row.MigratedLegacyContext, actorType, actorID, metadata)
	return err
}
