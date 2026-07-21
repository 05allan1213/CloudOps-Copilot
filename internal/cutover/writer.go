package cutover

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	"github.com/google/uuid"
)

const (
	QuiesceOperation         = "QUIESCE-V3"
	ReconciliationOperation  = "RECONCILE-EXTERNAL-WRITES-V3"
	ConverterAuditOperation  = "CONVERTER-AUDIT-V3"
	IrreversibleConfirmation = MarkerOperation

	markerLockName = "cloudops-copilot:cutover-v3-marker"
)

// WriteRequest binds the irreversible marker to the exact release and to
// three independently persisted, passed Phase 7A prerequisite ledger units.
// OldWorkerCount is required because Kubernetes process inventory is outside
// MySQL; unknown is represented by a negative value and is rejected.
type WriteRequest struct {
	PlanVersion                  uint64
	SourceExactSHA               string
	BinaryImageDigest            string
	SourceSchemaVersion          uint64
	TargetSchemaVersion          uint64
	QuiesceLedgerPublicID        string
	ReconciliationLedgerPublicID string
	ConverterAuditLedgerPublicID string
	OldWorkerCount               int64
	Confirmation                 string
}

func (r WriteRequest) Validate() error {
	if r.PlanVersion == 0 {
		return errors.New("cutover plan version must be positive")
	}
	if !isExactSHA(r.SourceExactSHA) {
		return errors.New("cutover source exact SHA must be a lowercase 40- or 64-character Git object id")
	}
	if !imageDigestPattern.MatchString(r.BinaryImageDigest) {
		return errors.New("cutover binary image digest must be an exact sha256 digest")
	}
	expectedSchema := uint64(schemaversion.Latest)
	if r.SourceSchemaVersion != expectedSchema || r.TargetSchemaVersion != expectedSchema {
		return fmt.Errorf("cutover schema source=%d target=%d want=%d", r.SourceSchemaVersion, r.TargetSchemaVersion, expectedSchema)
	}
	for label, value := range map[string]string{
		"quiesce": r.QuiesceLedgerPublicID, "reconciliation": r.ReconciliationLedgerPublicID,
		"converter audit": r.ConverterAuditLedgerPublicID,
	} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return fmt.Errorf("%s ledger public ID must be a canonical lowercase UUID", label)
		}
	}
	if r.QuiesceLedgerPublicID == r.ReconciliationLedgerPublicID ||
		r.QuiesceLedgerPublicID == r.ConverterAuditLedgerPublicID ||
		r.ReconciliationLedgerPublicID == r.ConverterAuditLedgerPublicID {
		return errors.New("cutover prerequisite ledger public IDs must be distinct")
	}
	if r.OldWorkerCount < 0 {
		return errors.New("old worker count is unknown")
	}
	if r.OldWorkerCount != 0 {
		return fmt.Errorf("old worker count=%d, want zero", r.OldWorkerCount)
	}
	if r.Confirmation != IrreversibleConfirmation {
		return fmt.Errorf("irreversible confirmation must equal %q", IrreversibleConfirmation)
	}
	return nil
}

type prerequisite struct {
	PublicID            string
	PlanVersion         uint64
	Operation           string
	SourceSchemaVersion uint64
	TargetSchemaVersion uint64
	Status              string
	SourceExactSHA      string
	BinaryImageDigest   string
	CompletedAt         sql.NullTime
}

type markerWriteTx interface {
	DatabaseSchemaVersion(context.Context) (uint64, error)
	MarkerStatusesForUpdate(context.Context) ([]string, error)
	PrerequisiteForUpdate(context.Context, string) (prerequisite, error)
	LegacyActiveLeaseCount(context.Context) (uint64, error)
	DatabaseTime(context.Context) (time.Time, error)
	InsertMarker(context.Context, Marker) error
	MarkerCount(context.Context) (uint64, error)
}

type markerWriteStore interface {
	WithLockedTransaction(context.Context, func(markerWriteTx) error) error
}

// MarkerWriter is the only write-capable cutover component. API and Worker
// startup continue to depend only on MarkerReader/RuntimeGuard.
type MarkerWriter struct {
	store markerWriteStore
}

func NewSQLMarkerWriter(db *sql.DB, lockTimeout time.Duration) (*MarkerWriter, error) {
	if db == nil {
		return nil, errors.New("cutover marker database is required")
	}
	if lockTimeout <= 0 {
		return nil, errors.New("cutover marker lock timeout must be positive")
	}
	return &MarkerWriter{store: &sqlMarkerWriteStore{db: db, lockTimeout: lockTimeout}}, nil
}

func newMarkerWriter(store markerWriteStore) *MarkerWriter { return &MarkerWriter{store: store} }

func (w *MarkerWriter) Write(ctx context.Context, request WriteRequest) (Marker, error) {
	if w == nil || w.store == nil {
		return Marker{}, errors.New("cutover marker writer is not initialized")
	}
	if err := request.Validate(); err != nil {
		return Marker{}, err
	}
	var marker Marker
	err := w.store.WithLockedTransaction(ctx, func(tx markerWriteTx) error {
		version, err := tx.DatabaseSchemaVersion(ctx)
		if err != nil {
			return fmt.Errorf("read database schema version: %w", err)
		}
		if version != request.SourceSchemaVersion || version != request.TargetSchemaVersion {
			return fmt.Errorf("database schema version=%d, request source=%d target=%d", version, request.SourceSchemaVersion, request.TargetSchemaVersion)
		}
		statuses, err := tx.MarkerStatusesForUpdate(ctx)
		if err != nil {
			return fmt.Errorf("inspect existing cutover marker: %w", err)
		}
		if len(statuses) != 0 {
			return fmt.Errorf("irreversible cutover marker already exists with statuses=%s", strings.Join(statuses, ","))
		}

		required := []struct {
			id        string
			operation string
		}{
			{request.QuiesceLedgerPublicID, QuiesceOperation},
			{request.ReconciliationLedgerPublicID, ReconciliationOperation},
			{request.ConverterAuditLedgerPublicID, ConverterAuditOperation},
		}
		for _, expected := range required {
			row, err := tx.PrerequisiteForUpdate(ctx, expected.id)
			if err != nil {
				return fmt.Errorf("read prerequisite %s: %w", expected.operation, err)
			}
			if err := validatePrerequisite(row, expected.operation, request); err != nil {
				return err
			}
		}
		activeLeases, err := tx.LegacyActiveLeaseCount(ctx)
		if err != nil {
			return fmt.Errorf("read legacy active leases: %w", err)
		}
		if activeLeases != 0 {
			return fmt.Errorf("legacy active lease count=%d, want zero", activeLeases)
		}
		if request.OldWorkerCount != 0 {
			return fmt.Errorf("old worker count=%d inside cutover transaction, want zero", request.OldWorkerCount)
		}
		now, err := tx.DatabaseTime(ctx)
		if err != nil {
			return fmt.Errorf("read database time: %w", err)
		}
		marker = buildMarker(request, now.UTC())
		if err := marker.Validate(schemaversion.Latest); err != nil {
			return fmt.Errorf("build cutover marker: %w", err)
		}
		if err := tx.InsertMarker(ctx, marker); err != nil {
			return fmt.Errorf("insert irreversible cutover marker: %w", err)
		}
		count, err := tx.MarkerCount(ctx)
		if err != nil {
			return fmt.Errorf("verify irreversible cutover marker: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("irreversible cutover marker rows=%d after insert, want exactly 1", count)
		}
		return nil
	})
	if err != nil {
		return Marker{}, err
	}
	return marker, nil
}

func validatePrerequisite(row prerequisite, expectedOperation string, request WriteRequest) error {
	if row.Operation != expectedOperation {
		return fmt.Errorf("prerequisite %s operation=%q", row.PublicID, row.Operation)
	}
	if row.Status != "passed" || !row.CompletedAt.Valid {
		return fmt.Errorf("prerequisite %s status=%q completed=%t, want passed", expectedOperation, row.Status, row.CompletedAt.Valid)
	}
	if row.PlanVersion != request.PlanVersion {
		return fmt.Errorf("prerequisite %s plan_version=%d, want %d", expectedOperation, row.PlanVersion, request.PlanVersion)
	}
	if row.SourceSchemaVersion != request.SourceSchemaVersion || row.TargetSchemaVersion != request.TargetSchemaVersion {
		return fmt.Errorf("prerequisite %s schema source=%d target=%d", expectedOperation, row.SourceSchemaVersion, row.TargetSchemaVersion)
	}
	if row.SourceExactSHA != request.SourceExactSHA || row.BinaryImageDigest != request.BinaryImageDigest {
		return fmt.Errorf("prerequisite %s release identity mismatch", expectedOperation)
	}
	return nil
}

func buildMarker(request WriteRequest, now time.Time) Marker {
	canonical := fmt.Sprintf("%s\nplan_version=%d\nsource_exact_sha=%s\nbinary_image_digest=%s\nschema=%d\nquiesce=%s\nreconciliation=%s\nconverter_audit=%s\nlegacy_active_leases=0\nold_workers=%d\n",
		MarkerConverterVersion, request.PlanVersion, request.SourceExactSHA, request.BinaryImageDigest,
		request.SourceSchemaVersion, request.QuiesceLedgerPublicID, request.ReconciliationLedgerPublicID,
		request.ConverterAuditLedgerPublicID, request.OldWorkerCount)
	sourceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	targetHash := fmt.Sprintf("%x", sha256.Sum256([]byte(sourceHash+"\nstatus=passed\noperation="+MarkerOperation+"\n")))
	return Marker{
		PublicID: uuid.NewString(), PlanVersion: request.PlanVersion, Stage: MarkerStage, Operation: MarkerOperation,
		Attempt: 1, SourceSchemaVersion: request.SourceSchemaVersion, TargetSchemaVersion: request.TargetSchemaVersion,
		SourceTable: "migration_ledger", TargetTable: "runtime_generation", BatchNo: 0,
		SourceCount: 1, TargetCount: 1, SourceHash: sourceHash, TargetHash: targetHash,
		ConverterVersion: MarkerConverterVersion, StartedAt: now, CompletedAt: now, Status: "passed",
		BoundedSummary: fmt.Sprintf("prerequisites passed: quiesce=%s reconciliation=%s converter_audit=%s; legacy_active_leases=0 old_workers=0",
			request.QuiesceLedgerPublicID, request.ReconciliationLedgerPublicID, request.ConverterAuditLedgerPublicID),
		SourceExactSHA: request.SourceExactSHA, BinaryImageDigest: request.BinaryImageDigest,
	}
}

type sqlMarkerWriteStore struct {
	db          *sql.DB
	lockTimeout time.Duration
}

func (s *sqlMarkerWriteStore) WithLockedTransaction(ctx context.Context, fn func(markerWriteTx) error) (retErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve cutover database connection: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, conn.Close()) }()
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", markerLockName, int64(math.Ceil(s.lockTimeout.Seconds()))).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire cutover marker lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return errors.New("acquire cutover marker lock timed out")
	}
	defer func() {
		var released sql.NullInt64
		if err := conn.QueryRowContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", markerLockName).Scan(&released); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("release cutover marker lock: %w", err))
		} else if !released.Valid || released.Int64 != 1 {
			retErr = errors.Join(retErr, errors.New("release cutover marker lock failed"))
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin cutover marker transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(sqlMarkerTx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cutover marker transaction: %w", err)
	}
	return nil
}

type sqlMarkerTx struct{ tx *sql.Tx }

func (t sqlMarkerTx) DatabaseSchemaVersion(ctx context.Context) (uint64, error) {
	var version uint64
	err := t.tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1").Scan(&version)
	return version, err
}

func (t sqlMarkerTx) MarkerStatusesForUpdate(ctx context.Context) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, "SELECT status FROM migration_ledger WHERE operation = ? ORDER BY id FOR UPDATE", MarkerOperation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return nil, err
		}
		result = append(result, status)
	}
	return result, rows.Err()
}

func (t sqlMarkerTx) PrerequisiteForUpdate(ctx context.Context, publicID string) (prerequisite, error) {
	var row prerequisite
	err := t.tx.QueryRowContext(ctx, `SELECT public_id, plan_version, operation,
source_schema_version, target_schema_version, status, source_exact_sha,
binary_image_digest, completed_at
FROM migration_ledger WHERE public_id = ? FOR UPDATE`, publicID).Scan(
		&row.PublicID, &row.PlanVersion, &row.Operation, &row.SourceSchemaVersion, &row.TargetSchemaVersion,
		&row.Status, &row.SourceExactSHA, &row.BinaryImageDigest, &row.CompletedAt,
	)
	return row, err
}

func (t sqlMarkerTx) LegacyActiveLeaseCount(ctx context.Context) (uint64, error) {
	var count uint64
	err := t.tx.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM agent_runs WHERE lease_owner <> '' AND lease_expires_at >= UTC_TIMESTAMP(6)) +
  (SELECT COUNT(*) FROM change_requests WHERE lease_owner <> '' AND lease_expires_at >= UTC_TIMESTAMP(6)) +
  (SELECT COUNT(*) FROM verification_runs WHERE lease_owner <> '' AND lease_expires_at >= UTC_TIMESTAMP(6))`).Scan(&count)
	return count, err
}

func (t sqlMarkerTx) DatabaseTime(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := t.tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now)
	return now, err
}

func (t sqlMarkerTx) InsertMarker(ctx context.Context, marker Marker) error {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO migration_ledger (
public_id, plan_version, stage, operation, attempt, previous_ledger_id,
source_schema_version, target_schema_version, source_table, target_table,
batch_no, id_min, id_max, source_count, target_count, skipped_count, rejected_count,
source_hash, target_hash, converter_version, started_at, completed_at, status,
reason_code, bounded_summary, source_exact_sha, binary_image_digest
) VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		marker.PublicID, marker.PlanVersion, marker.Stage, marker.Operation, marker.Attempt,
		marker.SourceSchemaVersion, marker.TargetSchemaVersion, marker.SourceTable, marker.TargetTable,
		marker.BatchNo, marker.SourceCount, marker.TargetCount, marker.SkippedCount, marker.RejectedCount,
		marker.SourceHash, marker.TargetHash, marker.ConverterVersion, marker.StartedAt, marker.CompletedAt,
		marker.Status, marker.BoundedSummary, marker.SourceExactSHA, marker.BinaryImageDigest,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("inserted cutover marker rows=%d, want 1", affected)
	}
	return nil
}

func (t sqlMarkerTx) MarkerCount(ctx context.Context) (uint64, error) {
	var count uint64
	err := t.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_ledger WHERE operation = ?", MarkerOperation).Scan(&count)
	return count, err
}
