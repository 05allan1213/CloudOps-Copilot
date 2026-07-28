package alert

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const commandFinalizationTimeout = 5 * time.Second

func validateCommand(alertID string, expectedVersion uint64, idempotencyKey string, actor Actor) error {
	if _, err := uuid.Parse(strings.TrimSpace(alertID)); err != nil || expectedVersion == 0 || !validActor(actor) {
		return ErrInvalid
	}
	if len(idempotencyKey) != 64 {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(idempotencyKey); err != nil {
		return ErrInvalid
	}
	return nil
}

func loadAlertByPublicID(ctx context.Context, tx *sql.Tx, publicID string, lock bool) (alertRow, error) {
	query := "SELECT " + alertColumns + " FROM alerts WHERE public_id = ?"
	if lock {
		query += " FOR UPDATE"
	}
	row, err := scanAlertRow(tx.QueryRowContext(ctx, query, publicID))
	if errors.Is(err, sql.ErrNoRows) {
		return alertRow{}, ErrNotFound
	}
	return row, err
}

func incrementAlertVersion(ctx context.Context, tx *sql.Tx, row *alertRow) error {
	result, err := tx.ExecContext(ctx, `UPDATE alerts SET row_version = row_version + 1,
updated_at = NOW(6) WHERE id = ? AND row_version = ?`, row.ID, row.Version)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrStaleVersion
	}
	row.Version++
	return nil
}

func (s *Service) Acknowledge(ctx context.Context, request AcknowledgeRequest) (View, error) {
	if err := validateCommand(request.AlertID, request.ExpectedVersion, request.IdempotencyKey, request.Actor); err != nil {
		return View{}, err
	}
	reason, err := validateReason(request.Reason)
	if err != nil {
		return View{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return View{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := loadAlertByPublicID(ctx, tx, request.AlertID, true)
	if err != nil {
		return View{}, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT acknowledgement.public_id
FROM alert_acknowledgements acknowledgement
WHERE acknowledgement.alert_id = ? AND acknowledgement.idempotency_key = ?`, row.ID, request.IdempotencyKey).Scan(&existing)
	if err == nil {
		if commitErr := tx.Commit(); commitErr != nil {
			return View{}, commitErr
		}
		return s.alertView(ctx, request.AlertID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return View{}, err
	}
	if row.Version != request.ExpectedVersion {
		return View{}, ErrStaleVersion
	}
	if row.Status != "firing" {
		return View{}, ErrConflict
	}
	acknowledgementID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_acknowledgements
(public_id, alert_id, recurrence_no, alert_version, actor_provider, actor_login, actor_role,
 reason, idempotency_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6))`,
		acknowledgementID, row.ID, row.Recurrence, row.Version, request.Actor.Provider,
		request.Actor.Login, request.Actor.Role, reason, request.IdempotencyKey)
	if err != nil {
		return View{}, err
	}
	if err := incrementAlertVersion(ctx, tx, &row); err != nil {
		return View{}, err
	}
	if err := appendAlertEvent(ctx, tx, row, "alert_acknowledged", "owner", request.Actor.Login,
		"Alert acknowledged by the Owner", nil, s.now().UTC(), map[string]any{
			"acknowledgement_id": acknowledgementID, "recurrence_no": row.Recurrence, "reason": reason,
		}, hashCanonical("alert-acknowledged", request.IdempotencyKey)); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return s.alertView(ctx, request.AlertID)
}

func (s *Service) CreateSilence(ctx context.Context, request CreateSilenceRequest) (Silence, error) {
	if err := validateCommand(request.AlertID, request.ExpectedVersion, request.IdempotencyKey, request.Actor); err != nil {
		return Silence{}, err
	}
	reason, err := validateReason(request.Reason)
	if err != nil || request.Duration < MinimumSilenceDuration || request.Duration > MaximumSilenceDuration {
		return Silence{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Silence{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := loadAlertByPublicID(ctx, tx, request.AlertID, true)
	if err != nil {
		return Silence{}, err
	}
	existing, exists, err := loadSilenceByIdempotency(ctx, tx, row.ID, request.IdempotencyKey)
	if err != nil {
		return Silence{}, err
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return Silence{}, err
		}
		persisted, err := s.silence(ctx, existing.ID)
		if err != nil {
			return Silence{}, err
		}
		if persisted.Reason != reason || persisted.EndsAt.Sub(persisted.StartsAt) != request.Duration {
			return Silence{}, ErrConflict
		}
		if persisted.Status != "pending" {
			return persisted, nil
		}
		if s.provider == nil {
			return persisted, ErrProviderUnavailable
		}
		return s.executePendingSilence(ctx, persisted, request.Actor)
	}
	if row.Version != request.ExpectedVersion {
		return Silence{}, ErrStaleVersion
	}
	if row.Status != "firing" {
		return Silence{}, ErrConflict
	}
	if s.provider == nil {
		return Silence{}, ErrProviderUnavailable
	}
	var revisionInternalID uint64
	var revisionPublicID string
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT revision.id, revision.public_id, provider.enabled
FROM active_configuration active JOIN configuration_revisions revision ON revision.id = active.configuration_revision_id
JOIN provider_configurations provider ON provider.configuration_revision_id = revision.id
WHERE active.singleton_id = 1 AND provider.provider = 'alertmanager'`).
		Scan(&revisionInternalID, &revisionPublicID, &enabled); err != nil {
		return Silence{}, err
	}
	if !enabled {
		return Silence{}, ErrProviderDisabled
	}
	matchers, err := silenceMatchers(row.Labels)
	if err != nil {
		return Silence{}, err
	}
	encodedMatchers, _ := json.Marshal(matchers)
	now := s.now().UTC()
	endsAt := now.Add(request.Duration)
	silenceID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_silences
(public_id, alert_id, recurrence_no, configuration_revision_id, status, matchers_json,
 reason, starts_at, ends_at, idempotency_key, created_at, updated_at)
VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, NOW(6), NOW(6))`, silenceID, row.ID,
		row.Recurrence, revisionInternalID, encodedMatchers, reason, now, endsAt, request.IdempotencyKey)
	if err != nil {
		return Silence{}, err
	}
	if err := incrementAlertVersion(ctx, tx, &row); err != nil {
		return Silence{}, err
	}
	if err := appendAlertEvent(ctx, tx, row, "alert_silence_requested", "owner", request.Actor.Login,
		"Bounded Alert silence requested", nil, now, map[string]any{
			"silence_id": silenceID, "configuration_revision_id": revisionPublicID,
			"duration_seconds": int64(request.Duration.Seconds()), "reason": reason,
		}, hashCanonical("alert-silence-requested", request.IdempotencyKey)); err != nil {
		return Silence{}, err
	}
	if err := tx.Commit(); err != nil {
		return Silence{}, err
	}
	return s.executePendingSilence(ctx, Silence{
		ID: silenceID, Status: "pending", Matchers: matchers, Reason: reason,
		ConfigurationRevisionID: revisionPublicID, StartsAt: now, EndsAt: endsAt,
	}, request.Actor)
}

func (s *Service) executePendingSilence(ctx context.Context, pending Silence, actor Actor) (Silence, error) {
	providerID, providerErr := s.provider.CreateSilence(ctx, SilenceProviderRequest{
		ExternalID:              "cloudops-silence:" + pending.ID,
		ConfigurationRevisionID: pending.ConfigurationRevisionID,
		Matchers:                pending.Matchers, StartsAt: pending.StartsAt, EndsAt: pending.EndsAt,
		Comment: "cloudops-silence:" + pending.ID, CreatedBy: actor.Login,
	})
	status := "active"
	errorCode := ""
	if providerErr != nil {
		status = "failed"
		errorCode = providerErrorCode(providerErr)
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandFinalizationTimeout)
	defer cancel()
	if err := s.finalizeSilence(finalizeCtx, pending.ID, status, providerID, errorCode, actor); err != nil {
		return Silence{}, err
	}
	result, err := s.silence(finalizeCtx, pending.ID)
	if err != nil {
		return Silence{}, err
	}
	if providerErr != nil {
		return result, providerErr
	}
	return result, nil
}

func silenceMatchers(labels []byte) ([]Matcher, error) {
	var values map[string]string
	if err := json.Unmarshal(labels, &values); err != nil {
		return nil, ErrInvalid
	}
	priority := []string{"alertname", "cluster", "cluster_id", "environment", "namespace", "service", "service_name", "workload_kind", "target_kind", "workload", "workload_name", "target_name", "deployment", "statefulset", "daemonset"}
	result := make([]Matcher, 0, 8)
	for _, name := range priority {
		value := strings.TrimSpace(values[name])
		if value == "" {
			continue
		}
		result = append(result, Matcher{Name: name, Value: value, IsRegex: false, IsEqual: true})
		if len(result) == 8 {
			break
		}
	}
	if len(result) == 0 {
		return nil, ErrInvalid
	}
	return result, nil
}

func providerErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrProviderDisabled):
		return "provider_disabled"
	case errors.Is(err, context.DeadlineExceeded):
		return "provider_timeout"
	default:
		return "provider_unavailable"
	}
}

func (s *Service) finalizeSilence(ctx context.Context, publicID, status, providerID, errorCode string, actor Actor) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var alertID uint64
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT alert_id, status FROM alert_silences WHERE public_id = ? FOR UPDATE`, publicID).Scan(&alertID, &currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentStatus != "pending" {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE alert_silences SET status = ?, provider_silence_id = NULLIF(?, ''),
provider_error_code = NULLIF(?, ''), updated_at = NOW(6) WHERE public_id = ? AND status = 'pending'`,
		status, providerID, errorCode, publicID)
	if err != nil {
		return err
	}
	row, err := scanAlertRow(tx.QueryRowContext(ctx, "SELECT "+alertColumns+" FROM alerts WHERE id = ?", alertID))
	if err != nil {
		return err
	}
	eventType, summary := "alert_silence_activated", "Alertmanager silence is active"
	if status == "failed" {
		eventType, summary = "alert_silence_failed", "Alertmanager silence request failed"
	}
	if err := appendAlertEvent(ctx, tx, row, eventType, "provider", "alertmanager", summary, nil,
		s.now().UTC(), map[string]any{"silence_id": publicID, "provider_silence_id": providerID, "error_code": errorCode},
		hashCanonical(eventType, publicID)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ExpireSilence(ctx context.Context, request ExpireSilenceRequest) (Silence, error) {
	if _, err := uuid.Parse(strings.TrimSpace(request.SilenceID)); err != nil || request.ExpectedVersion == 0 ||
		len(request.IdempotencyKey) != 64 || !validActor(request.Actor) {
		return Silence{}, ErrInvalid
	}
	if _, err := hex.DecodeString(request.IdempotencyKey); err != nil {
		return Silence{}, ErrInvalid
	}
	if s.provider == nil {
		return Silence{}, ErrProviderUnavailable
	}
	preflight, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Silence{}, err
	}
	defer func() { _ = preflight.Rollback() }()
	var alertID string
	var providerID, revisionID, status string
	var version uint64
	err = preflight.QueryRowContext(ctx, `SELECT alert.public_id, alert.row_version,
silence.provider_silence_id, revision.public_id, silence.status
FROM alert_silences silence JOIN alerts alert ON alert.id = silence.alert_id
JOIN configuration_revisions revision ON revision.id = silence.configuration_revision_id
WHERE silence.public_id = ? FOR UPDATE`, request.SilenceID).Scan(&alertID, &version, &providerID, &revisionID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return Silence{}, ErrNotFound
	}
	if err != nil {
		return Silence{}, err
	}
	eventKey := hashCanonical("alert-silence-expired", request.IdempotencyKey)
	var replay int
	if err := preflight.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_events event JOIN alerts alert ON alert.id = event.alert_id
WHERE alert.public_id = ? AND event.idempotency_key = ?`, alertID, eventKey).Scan(&replay); err != nil {
		return Silence{}, err
	}
	if replay > 0 || status == "expired" {
		if err := preflight.Commit(); err != nil {
			return Silence{}, err
		}
		return s.silence(ctx, request.SilenceID)
	}
	if version != request.ExpectedVersion {
		return Silence{}, ErrStaleVersion
	}
	if status != "active" || providerID == "" {
		return Silence{}, ErrConflict
	}
	if err := preflight.Commit(); err != nil {
		return Silence{}, err
	}
	if err := s.provider.ExpireSilence(ctx, providerID, revisionID); err != nil {
		return Silence{}, err
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandFinalizationTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(finalizeCtx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Silence{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := loadAlertByPublicID(finalizeCtx, tx, alertID, true)
	if err != nil {
		return Silence{}, err
	}
	var finalStatus string
	if err := tx.QueryRowContext(finalizeCtx, `SELECT status FROM alert_silences WHERE public_id = ? AND alert_id = ? FOR UPDATE`, request.SilenceID, row.ID).Scan(&finalStatus); err != nil {
		return Silence{}, err
	}
	if finalStatus == "expired" {
		if err := tx.Commit(); err != nil {
			return Silence{}, err
		}
		return s.silence(finalizeCtx, request.SilenceID)
	}
	if finalStatus != "active" {
		return Silence{}, ErrConflict
	}
	result, err := tx.ExecContext(finalizeCtx, `UPDATE alert_silences SET status = 'expired', expired_at = NOW(6),
updated_at = NOW(6) WHERE public_id = ? AND status = 'active'`, request.SilenceID)
	if err != nil {
		return Silence{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Silence{}, ErrConflict
	}
	if err := incrementAlertVersion(finalizeCtx, tx, &row); err != nil {
		return Silence{}, err
	}
	if err := appendAlertEvent(finalizeCtx, tx, row, "alert_silence_expired", "owner", request.Actor.Login,
		"Alertmanager silence expired by the Owner", nil, s.now().UTC(),
		map[string]any{"silence_id": request.SilenceID, "provider_silence_id": providerID}, eventKey); err != nil {
		return Silence{}, err
	}
	if err := tx.Commit(); err != nil {
		return Silence{}, err
	}
	return s.silence(finalizeCtx, request.SilenceID)
}

func loadSilenceByIdempotency(ctx context.Context, queryer alertQueryer, alertID uint64, key string) (Silence, bool, error) {
	var publicID string
	err := queryer.QueryRowContext(ctx, `SELECT public_id FROM alert_silences WHERE alert_id = ? AND idempotency_key = ?`, alertID, key).Scan(&publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return Silence{}, false, nil
	}
	if err != nil {
		return Silence{}, false, err
	}
	return Silence{ID: publicID}, true, nil
}

func (s *Service) silence(ctx context.Context, publicID string) (Silence, error) {
	var result Silence
	var providerID, errorCode sql.NullString
	var matchers []byte
	var expiredAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT silence.public_id, silence.provider_silence_id,
silence.status, silence.matchers_json, silence.reason, revision.public_id, silence.starts_at,
silence.ends_at, silence.expired_at, silence.provider_error_code, silence.created_at
FROM alert_silences silence JOIN configuration_revisions revision ON revision.id = silence.configuration_revision_id
WHERE silence.public_id = ?`, publicID).Scan(&result.ID, &providerID, &result.Status, &matchers,
		&result.Reason, &result.ConfigurationRevisionID, &result.StartsAt, &result.EndsAt, &expiredAt,
		&errorCode, &result.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Silence{}, ErrNotFound
	}
	if err != nil {
		return Silence{}, err
	}
	if err := json.Unmarshal(matchers, &result.Matchers); err != nil {
		return Silence{}, fmt.Errorf("decode silence matchers: %w", err)
	}
	result.ProviderSilenceID, result.ProviderErrorCode = providerID.String, errorCode.String
	if expiredAt.Valid {
		value := expiredAt.Time.UTC()
		result.ExpiredAt = &value
	}
	return result, nil
}

func (s *Service) alertView(ctx context.Context, publicID string) (View, error) {
	detail, err := s.Detail(ctx, publicID)
	return detail.Alert, err
}

func (s *Service) StartInvestigation(ctx context.Context, request StartInvestigationRequest) (View, error) {
	if err := validateCommand(request.AlertID, request.ExpectedVersion, request.IdempotencyKey, request.Actor); err != nil {
		return View{}, err
	}
	reason, err := validateReason(request.Reason)
	if err != nil {
		return View{}, err
	}
	if s.investigation == nil {
		return View{}, ErrProviderUnavailable
	}
	eventKey := hashCanonical("alert-investigation-requested", request.IdempotencyKey)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
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
	runID, err := s.investigation.StartAlertInvestigationTx(ctx, tx, request.AlertID, request.IdempotencyKey, reason)
	if err != nil {
		return View{}, err
	}
	if err := incrementAlertVersion(ctx, tx, &row); err != nil {
		return View{}, err
	}
	if err := appendAlertEvent(ctx, tx, row, "alert_investigation_requested", "owner", request.Actor.Login,
		"Agent Investigation requested from Alert context", nil, s.now().UTC(),
		map[string]any{"agent_run_id": runID, "reason": reason}, eventKey); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return s.alertView(ctx, request.AlertID)
}
