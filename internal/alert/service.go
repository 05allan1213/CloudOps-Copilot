package alert

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentstore"
	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	"github.com/google/uuid"
)

const (
	maxBatchSignals = 100
	maxPageSize     = 100
	defaultPageSize = 50
)

type Service struct {
	db            *sql.DB
	provider      SilenceProvider
	investigation InvestigationStarter
	rejections    *incidentstore.Store
	now           func() time.Time
}

func NewService(db *sql.DB, provider SilenceProvider, investigation InvestigationStarter) (*Service, error) {
	if db == nil {
		return nil, errors.New("alert database is required")
	}
	rejections, err := incidentstore.NewStore(db)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, provider: provider, investigation: investigation, rejections: rejections, now: time.Now}, nil
}

func (s *Service) Ready(ctx context.Context) error {
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if !version.Valid || version.Int64 != schemaversion.Latest {
		return fmt.Errorf("unsupported schema version %d, want %d", version.Int64, schemaversion.Latest)
	}
	for _, table := range []string{"alerts", "alert_signal_links", "alert_events", "alert_acknowledgements", "alert_silences", "alert_incident_links", "escalation_policies"} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("required Alert table %s is missing", table)
		}
	}
	return nil
}

func (s *Service) RecordRejections(ctx context.Context, rejections []incidentstore.RejectionInput) error {
	return s.rejections.RecordRejections(ctx, rejections)
}

type alertRow struct {
	ID                    uint64
	PublicID              string
	Source                string
	AlertKey              string
	InstanceKey           string
	CorrelationKey        string
	Fingerprint           string
	Status                string
	Severity              string
	Cluster               string
	Environment           string
	Namespace             string
	Service               string
	TargetKind            string
	TargetName            string
	Category              string
	Summary               string
	Labels                []byte
	Annotations           []byte
	FirstSeen             time.Time
	LastSeen              time.Time
	StartsAt              time.Time
	ResolvedAt            sql.NullTime
	Recurrence            uint64
	SignalCount           uint64
	Version               uint64
	MigratedLegacy        bool
	MigratedLegacyContext bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func scanAlertRow(scanner interface{ Scan(...any) error }) (alertRow, error) {
	var row alertRow
	err := scanner.Scan(
		&row.ID, &row.PublicID, &row.Source, &row.AlertKey, &row.InstanceKey, &row.CorrelationKey,
		&row.Fingerprint, &row.Status, &row.Severity, &row.Cluster, &row.Environment,
		&row.Namespace, &row.Service, &row.TargetKind, &row.TargetName, &row.Category, &row.Summary,
		&row.Labels, &row.Annotations, &row.FirstSeen, &row.LastSeen, &row.StartsAt, &row.ResolvedAt,
		&row.Recurrence, &row.SignalCount, &row.Version, &row.MigratedLegacy,
		&row.MigratedLegacyContext, &row.CreatedAt, &row.UpdatedAt,
	)
	return row, err
}

const alertColumns = `id, public_id, source, alert_key, current_alert_instance_key, correlation_key,
fingerprint, status, severity, cluster, environment, namespace, service_name, target_kind,
target_name, category, summary, labels_json, annotations_json, first_seen_at, last_seen_at,
starts_at, resolved_at, recurrence_count, signal_count, row_version, migrated_legacy,
migrated_legacy_context, created_at, updated_at`

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

func appendAlertEvent(ctx context.Context, tx *sql.Tx, row alertRow, eventType, actorType, actorID, summary string, signalID any, occurred time.Time, metadata any, idempotencyKey string) error {
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) > 8192 {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO alert_events
(public_id, alert_id, event_type, actor_type, actor_id, source_signal_id, idempotency_key,
 summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6))`, uuid.NewString(), row.ID, eventType,
		actorType, actorID, signalID, idempotencyKey, summary, encoded, occurred.UTC())
	return err
}

func validActor(actor Actor) bool {
	return actor.Provider == "local" && actor.Login == "owner" && actor.Role == "owner"
}

func validateReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 1024 {
		return "", ErrInvalid
	}
	return value, nil
}
