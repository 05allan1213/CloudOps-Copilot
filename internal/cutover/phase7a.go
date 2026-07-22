package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
)

const (
	phase7ALockName    = "cloudops-copilot:phase7a-prepare"
	phase7AConverter   = "phase7a-cutover/v2"
	phase7AControlName = "release-a"
)

// LegacyChangeReconciler is a read-only external boundary. It may query a
// fixed GitHub repository/PR, but it must never create a branch, commit, PR,
// merge, rerun workflow, or mutate Kubernetes/Argo.
type LegacyChangeReconciler interface {
	ReconcilePullRequest(context.Context, LegacyExternalArtifact) (ReconciledPullRequest, error)
}

// PrepareRequest carries externally observed quiesce facts. They are explicit
// because database state alone cannot prove that HTTP writers, old worker
// deployments, or an unknown provider operation are absent.
type PrepareRequest struct {
	PlanVersion                  uint64
	SourceExactSHA               string
	BinaryImageDigest            string
	BackfillBatchSize            uint64
	ObservedIngressWriters       uint64
	ObservedMutationWriters      uint64
	ObservedLegacyWorkers        uint64
	ObservedUnknownExternalWrite uint64
}

func (r PrepareRequest) Validate() error {
	if r.PlanVersion == 0 || !isExactSHA(r.SourceExactSHA) || !imageDigestPattern.MatchString(r.BinaryImageDigest) {
		return errors.New("phase7a prepare requires a positive plan version, exact lowercase SHA, and exact sha256 image digest")
	}
	if r.BackfillBatchSize > maxBackfillBatchSize {
		return fmt.Errorf("phase7a backfill batch size=%d exceeds %d", r.BackfillBatchSize, maxBackfillBatchSize)
	}
	if r.ObservedIngressWriters+r.ObservedMutationWriters+r.ObservedLegacyWorkers+r.ObservedUnknownExternalWrite != 0 {
		return fmt.Errorf("quiesce observations must all be zero: ingress=%d mutations=%d legacy_workers=%d unknown_external_writes=%d",
			r.ObservedIngressWriters, r.ObservedMutationWriters, r.ObservedLegacyWorkers, r.ObservedUnknownExternalWrite)
	}
	return nil
}

type PrepareReport struct {
	PlanVersion                  uint64            `json:"plan_version"`
	SourceExactSHA               string            `json:"source_exact_sha"`
	BinaryImageDigest            string            `json:"binary_image_digest"`
	SourceSchemaVersion          uint64            `json:"source_schema_version"`
	TargetSchemaVersion          uint64            `json:"target_schema_version"`
	QuiesceLedgerPublicID        string            `json:"quiesce_ledger_public_id"`
	ReconciliationLedgerPublicID string            `json:"reconciliation_ledger_public_id"`
	ConverterAuditLedgerPublicID string            `json:"converter_audit_ledger_public_id"`
	Backfill                     BackfillReport    `json:"backfill"`
	Counts                       map[string]uint64 `json:"counts"`
	PreparedAt                   time.Time         `json:"prepared_at"`
}

type Phase7APreparer struct {
	db            *sql.DB
	lockTimeout   time.Duration
	now           func() time.Time
	reconciler    LegacyChangeReconciler
	backfillFault BackfillFaultInjector
}

func NewPhase7APreparer(db *sql.DB, lockTimeout time.Duration) (*Phase7APreparer, error) {
	if db == nil || lockTimeout <= 0 {
		return nil, errors.New("phase7a prepare requires a database and positive lock timeout")
	}
	return &Phase7APreparer{db: db, lockTimeout: lockTimeout, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (p *Phase7APreparer) WithLegacyChangeReconciler(reconciler LegacyChangeReconciler) *Phase7APreparer {
	if p != nil {
		p.reconciler = reconciler
	}
	return p
}

func (p *Phase7APreparer) WithBackfillFaultInjector(injector BackfillFaultInjector) *Phase7APreparer {
	if p != nil {
		p.backfillFault = injector
	}
	return p
}

func (p *Phase7APreparer) Prepare(ctx context.Context, request PrepareRequest) (report PrepareReport, retErr error) {
	if p == nil || p.db == nil {
		return report, errors.New("phase7a preparer is not initialized")
	}
	if request.BackfillBatchSize == 0 {
		request.BackfillBatchSize = defaultBackfillBatchSize
	}
	if err := request.Validate(); err != nil {
		return report, err
	}
	backfillIdentity := ReleaseIdentity{PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SourceSchema: uint64(schemaversion.Latest - 1), TargetSchema: uint64(schemaversion.Latest)}
	cutoverIdentity := ReleaseIdentity{PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SourceSchema: uint64(schemaversion.Latest), TargetSchema: uint64(schemaversion.Latest)}
	backfiller, err := NewPhase7ABackfiller(p.db, p.lockTimeout)
	if err != nil {
		return report, err
	}
	backfiller.now, backfiller.fault = p.now, p.backfillFault
	backfillReport, err := backfiller.Run(ctx, BackfillRequest{Identity: backfillIdentity, BatchSize: request.BackfillBatchSize})
	if err != nil {
		return report, fmt.Errorf("run BACKFILL-V3: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, recordPreparationFailure(ctx, p.db, p.lockTimeout, cutoverIdentity, retErr))
		}
	}()

	conn, err := p.db.Conn(ctx)
	if err != nil {
		return report, fmt.Errorf("reserve phase7a database connection: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, conn.Close()) }()
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", phase7ALockName, int64(math.Ceil(p.lockTimeout.Seconds()))).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return report, fmt.Errorf("acquire phase7a cutover lock: %w", err)
	}
	defer func() {
		var released sql.NullInt64
		if err := conn.QueryRowContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", phase7ALockName).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			retErr = errors.Join(retErr, errors.New("release phase7a cutover lock failed"))
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return report, fmt.Errorf("begin phase7a transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	version, err := databaseSchemaVersion(ctx, tx)
	if err != nil || version != uint64(schemaversion.Latest) {
		return report, fmt.Errorf("phase7a schema version=%d want=%d: %w", version, schemaversion.Latest, err)
	}
	if existing, found, err := existingPreparationV2(ctx, tx, request, version); err != nil {
		return report, err
	} else if found {
		existing.Backfill = backfillReport
		if err := tx.Commit(); err != nil {
			return report, fmt.Errorf("commit idempotent phase7a inspection: %w", err)
		}
		return existing, nil
	}
	if err := validateQuiescePrerequisites(ctx, tx, request); err != nil {
		return report, err
	}
	now, err := databaseTime(ctx, tx)
	if err != nil {
		return report, err
	}
	if err := persistCutoverControl(ctx, tx, request, now); err != nil {
		return report, err
	}
	if err := ensureOutboxRegistry(ctx, tx); err != nil {
		return report, err
	}
	_, err = archiveOutboxRows(ctx, tx, p.reconciler != nil, now)
	if err != nil {
		return report, err
	}
	if err := archiveLegacyPlansAndApprovals(ctx, tx, now); err != nil {
		return report, err
	}
	_, err = convertAgentRuns(ctx, tx, now)
	if err != nil {
		return report, err
	}
	_, err = convertChangeRequests(ctx, tx, p.reconciler, now)
	if err != nil {
		return report, err
	}
	_, err = convertVerificationRuns(ctx, tx, now)
	if err != nil {
		return report, err
	}
	if err := backfillChangeCandidates(ctx, tx, now); err != nil {
		return report, err
	}
	if err := convertIncidentStates(ctx, tx, now); err != nil {
		return report, err
	}
	if err := archivePostmortemsV2(ctx, tx, now); err != nil {
		return report, err
	}
	counts, err := collectCutoverCountsV2(ctx, tx, request)
	if err != nil {
		return report, err
	}
	if err := validatePhase7ACounts(counts, request); err != nil {
		return report, err
	}
	ledgerIDs, err := persistPhase7ALedgers(ctx, tx, cutoverIdentity, request, counts, now)
	if err != nil {
		return report, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE cutover_controls SET completed_at=? WHERE control_name=?", now, phase7AControlName); err != nil {
		return report, fmt.Errorf("complete phase7a control: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("commit phase7a preparation: %w", err)
	}
	return PrepareReport{PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SourceSchemaVersion: cutoverIdentity.SourceSchema, TargetSchemaVersion: version,
		QuiesceLedgerPublicID: ledgerIDs.Quiesce, ReconciliationLedgerPublicID: ledgerIDs.Reconciliation,
		ConverterAuditLedgerPublicID: ledgerIDs.ConverterAudit, Backfill: backfillReport, Counts: counts, PreparedAt: now}, nil
}

func databaseSchemaVersion(ctx context.Context, tx *sql.Tx) (uint64, error) {
	var version uint64
	err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1").Scan(&version)
	return version, err
}

func databaseTime(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	return now.UTC(), nil
}

func validateQuiescePrerequisites(ctx context.Context, tx *sql.Tx, request PrepareRequest) error {
	var markerCount, activeLegacyLeases, runningTasks uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_ledger WHERE operation=?", MarkerOperation).Scan(&markerCount); err != nil || markerCount != 0 {
		return fmt.Errorf("CUTOVER-V3 marker count=%d want zero: %w", markerCount, err)
	}
	if err := tx.QueryRowContext(ctx, legacyActiveLeaseCountSQL).Scan(&activeLegacyLeases); err != nil || activeLegacyLeases != 0 {
		return fmt.Errorf("legacy active leases=%d want zero: %w", activeLegacyLeases, err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM async_tasks WHERE status='running'").Scan(&runningTasks); err != nil || runningTasks != 0 {
		return fmt.Errorf("running V3 tasks=%d want zero: %w", runningTasks, err)
	}
	if request.ObservedIngressWriters+request.ObservedMutationWriters+request.ObservedLegacyWorkers+request.ObservedUnknownExternalWrite != 0 {
		return errors.New("external quiesce observations are not zero")
	}
	return nil
}

func persistCutoverControl(ctx context.Context, tx *sql.Tx, request PrepareRequest, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO cutover_controls (
control_name,plan_version,source_exact_sha,binary_image_digest,ingress_quiesced,mutations_quiesced,legacy_workers_quiesced,
observed_ingress_writers,observed_mutation_writers,observed_legacy_workers,observed_unknown_external_writes,prepared_at,completed_at)
VALUES (?,?,?,?,TRUE,TRUE,TRUE,?,?,?,?,?,NULL)
ON DUPLICATE KEY UPDATE plan_version=VALUES(plan_version),source_exact_sha=VALUES(source_exact_sha),
binary_image_digest=VALUES(binary_image_digest),ingress_quiesced=TRUE,mutations_quiesced=TRUE,legacy_workers_quiesced=TRUE,
observed_ingress_writers=VALUES(observed_ingress_writers),observed_mutation_writers=VALUES(observed_mutation_writers),
observed_legacy_workers=VALUES(observed_legacy_workers),observed_unknown_external_writes=VALUES(observed_unknown_external_writes),
prepared_at=VALUES(prepared_at),completed_at=NULL`, phase7AControlName, request.PlanVersion, request.SourceExactSHA,
		request.BinaryImageDigest, request.ObservedIngressWriters, request.ObservedMutationWriters,
		request.ObservedLegacyWorkers, request.ObservedUnknownExternalWrite, now.UTC())
	if err != nil {
		return fmt.Errorf("persist phase7a quiesce control: %w", err)
	}
	return nil
}
