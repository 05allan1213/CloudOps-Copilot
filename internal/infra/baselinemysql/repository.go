// Package baselinemysql persists verified Deployment baselines without
// rewriting immutable observations. Activation is serialized per target so
// concurrent verifier Jobs cannot create two active rows.
package baselinemysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	"github.com/google/uuid"
)

const (
	defaultLockTimeout = 5 * time.Second
	lockNamePrefix     = "cloudops-baseline:"
)

type Repository struct {
	db          *sql.DB
	lockTimeout time.Duration
}

var _ baseline.Store = (*Repository)(nil)
var _ baseline.TransactionalStore = (*Repository)(nil)

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("baseline repository database is required")
	}
	return &Repository{db: db, lockTimeout: defaultLockTimeout}, nil
}

func (r *Repository) Activate(ctx context.Context, snapshot baseline.Snapshot) (baseline.ActivationResult, error) {
	if r == nil || r.db == nil {
		return baseline.ActivationResult{}, errors.New("baseline repository is not initialized")
	}
	if err := prepareSnapshot(&snapshot); err != nil {
		return baseline.ActivationResult{}, err
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return baseline.ActivationResult{}, fmt.Errorf("acquire baseline activation connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	lockName := lockNamePrefix + snapshot.TargetIdentityHash[:32]
	if err := acquireLock(ctx, conn, lockName, r.lockTimeout); err != nil {
		return baseline.ActivationResult{}, err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName) }()

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return baseline.ActivationResult{}, fmt.Errorf("begin baseline activation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	activation, err := r.activateIn(ctx, tx, snapshot)
	if err != nil {
		return baseline.ActivationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return baseline.ActivationResult{}, fmt.Errorf("commit baseline activation: %w", err)
	}
	return activation, nil
}

// ActivateIn performs the complete immutable activation using a caller-owned
// transaction. The caller is responsible for commit/rollback. Workflows that
// call this method must already have a durable target to serialize on; the
// generated active-target key remains the final database uniqueness guard.
func (r *Repository) ActivateIn(ctx context.Context, tx baseline.Transaction, snapshot baseline.Snapshot) (baseline.ActivationResult, error) {
	if r == nil || tx == nil {
		return baseline.ActivationResult{}, errors.New("baseline transactional repository is not initialized")
	}
	if err := prepareSnapshot(&snapshot); err != nil {
		return baseline.ActivationResult{}, err
	}
	return r.activateIn(ctx, tx, snapshot)
}

func prepareSnapshot(snapshot *baseline.Snapshot) error {
	if snapshot == nil {
		return errors.New("baseline snapshot is required")
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	return snapshot.Finalize()
}

func (r *Repository) activateIn(ctx context.Context, tx baseline.Transaction, snapshot baseline.Snapshot) (baseline.ActivationResult, error) {
	exact, err := loadExact(ctx, tx, snapshot)
	if err != nil {
		return baseline.ActivationResult{}, err
	}
	if exact != nil {
		return replayActivation(ctx, tx, exact, snapshot)
	}

	active, err := loadActive(ctx, tx, snapshot.TargetIdentityHash)
	if err != nil {
		return baseline.ActivationResult{}, err
	}
	// A concurrent transaction can create the same exact baseline while this
	// transaction waits for the prior active row. Re-read after taking that
	// serialization lock so replay remains idempotent instead of superseding the
	// just-created row and colliding with the immutable revision key.
	exact, err = loadExact(ctx, tx, snapshot)
	if err != nil {
		return baseline.ActivationResult{}, err
	}
	if exact != nil {
		return replayActivation(ctx, tx, exact, snapshot)
	}
	if active != nil {
		if err := supersede(ctx, tx, active); err != nil {
			return baseline.ActivationResult{}, err
		}
	}

	publicID := snapshot.PublicID()
	result, err := tx.ExecContext(ctx, `
	INSERT INTO deployment_baselines (
	    public_id, baseline_schema_version,
    target_identity_hash, cluster, environment, namespace, workload_kind,
    workload_name, container_name, repository, base_branch, target_path,
    source_revision, image_digest, gitops_revision, config_hash,
    verification_policy_version, verification_hash, status, row_version,
    verified_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 1, ?, NOW(6), NOW(6))`,
		publicID, baseline.BaselineSchemaVersion,
		snapshot.TargetIdentityHash, snapshot.Target.Cluster, snapshot.Target.Environment,
		snapshot.Target.Namespace, "Deployment", snapshot.Target.WorkloadName,
		snapshot.Target.ContainerName, snapshot.Target.Repository, snapshot.Target.BaseBranch,
		snapshot.Target.TargetPath, snapshot.SourceRevision, snapshot.ImageDigest,
		snapshot.GitOpsRevision, snapshot.ConfigHash, snapshot.VerificationPolicyVersion,
		snapshot.VerificationHash, snapshot.VerifiedAt.UTC(),
	)
	if err != nil {
		return baseline.ActivationResult{}, fmt.Errorf("insert DeploymentBaseline: %w", err)
	}
	baselineID, err := result.LastInsertId()
	if err != nil || baselineID <= 0 {
		if err == nil {
			err = errors.New("insert returned no id")
		}
		return baseline.ActivationResult{}, fmt.Errorf("read inserted DeploymentBaseline id: %w", err)
	}

	observationIDs := make([]uint64, 0, len(snapshot.Observations))
	for sequence, observation := range snapshot.Observations {
		result, err := tx.ExecContext(ctx, `
	INSERT INTO baseline_observations (
	    public_id, observation_schema_version,
    baseline_id, sequence_no, observation_type, source_identity,
    observed_json, content_hash, dedupe_key, observed_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6))`,
			observationPublicID(publicID, observation), baseline.ObservationSchemaVersion,
			baselineID, sequence+1, observation.Type,
			observation.SourceIdentity, []byte(observation.ObservedJSON), observation.ContentHash,
			observation.DedupeKey, observation.ObservedAt.UTC(),
		)
		if err != nil {
			return baseline.ActivationResult{}, fmt.Errorf("insert baseline observation %s: %w", observation.Type, err)
		}
		id, err := result.LastInsertId()
		if err != nil || id <= 0 {
			if err == nil {
				err = errors.New("insert returned no id")
			}
			return baseline.ActivationResult{}, fmt.Errorf("read inserted baseline observation %s id: %w", observation.Type, err)
		}
		observationIDs = append(observationIDs, uint64(id))
	}
	return baseline.ActivationResult{
		BaselineID: uint64(baselineID), PublicID: publicID, Created: true,
		SupersededBaselineID: activeID(active), ObservationIDs: observationIDs,
	}, nil
}

func replayActivation(ctx context.Context, tx baseline.Transaction, exact *baselineRow, snapshot baseline.Snapshot) (baseline.ActivationResult, error) {
	if exact == nil {
		return baseline.ActivationResult{}, baseline.ErrConflict
	}
	switch exact.Status {
	case "active":
		if err := compareExisting(*exact, snapshot); err != nil {
			return baseline.ActivationResult{}, err
		}
		observations, err := loadObservationIDs(ctx, tx, exact.ID, snapshot)
		if err != nil {
			return baseline.ActivationResult{}, err
		}
		return baseline.ActivationResult{
			BaselineID: exact.ID, PublicID: exact.PublicID, Created: false, ObservationIDs: observations,
		}, nil
	case "superseded":
		return baseline.ActivationResult{}, fmt.Errorf("%w: %s", baseline.ErrSuperseded, exact.PublicID)
	default:
		return baseline.ActivationResult{}, fmt.Errorf("%w: unknown baseline status %q", baseline.ErrConflict, exact.Status)
	}
}

type baselineRow struct {
	ID, RowVersion              uint64
	BaselineSchemaVersion       uint16
	PublicID, Status            string
	TargetIdentityHash          string
	Target                      baseline.Target
	SourceRevision              string
	ImageDigest, GitOpsRevision string
	ConfigHash, PolicyVersion   string
	VerificationHash            string
	VerifiedAt                  time.Time
}

func loadExact(ctx context.Context, tx baseline.Transaction, snapshot baseline.Snapshot) (*baselineRow, error) {
	row := &baselineRow{}
	err := tx.QueryRowContext(ctx, `
	SELECT id, public_id, baseline_schema_version, status, row_version,
       target_identity_hash, cluster, environment, namespace, workload_kind, workload_name,
       container_name, repository, base_branch, target_path, source_revision, image_digest,
       gitops_revision, config_hash, verification_policy_version, verification_hash, verified_at
FROM deployment_baselines
	WHERE target_identity_hash = ? AND gitops_revision = ? AND config_hash = ?
LIMIT 1 FOR UPDATE`,
		snapshot.TargetIdentityHash, snapshot.GitOpsRevision, snapshot.ConfigHash,
	).Scan(&row.ID, &row.PublicID, &row.BaselineSchemaVersion,
		&row.Status, &row.RowVersion, &row.TargetIdentityHash, &row.Target.Cluster,
		&row.Target.Environment, &row.Target.Namespace, &row.Target.WorkloadKind,
		&row.Target.WorkloadName, &row.Target.ContainerName, &row.Target.Repository,
		&row.Target.BaseBranch, &row.Target.TargetPath, &row.SourceRevision,
		&row.ImageDigest, &row.GitOpsRevision, &row.ConfigHash, &row.PolicyVersion,
		&row.VerificationHash, &row.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load exact DeploymentBaseline: %w", err)
	}
	return row, nil
}

func loadActive(ctx context.Context, tx baseline.Transaction, targetHash string) (*baselineRow, error) {
	row := &baselineRow{}
	err := tx.QueryRowContext(ctx, `
	SELECT id, public_id, baseline_schema_version, status, row_version,
       target_identity_hash, cluster, environment, namespace, workload_kind, workload_name,
       container_name, repository, base_branch, target_path, source_revision, image_digest,
       gitops_revision, config_hash, verification_policy_version, verification_hash, verified_at
FROM deployment_baselines
	WHERE target_identity_hash = ? AND status = 'active'
LIMIT 1 FOR UPDATE`, targetHash).Scan(
		&row.ID, &row.PublicID, &row.BaselineSchemaVersion,
		&row.Status, &row.RowVersion, &row.TargetIdentityHash, &row.Target.Cluster,
		&row.Target.Environment, &row.Target.Namespace, &row.Target.WorkloadKind,
		&row.Target.WorkloadName, &row.Target.ContainerName, &row.Target.Repository,
		&row.Target.BaseBranch, &row.Target.TargetPath, &row.SourceRevision,
		&row.ImageDigest, &row.GitOpsRevision, &row.ConfigHash, &row.PolicyVersion,
		&row.VerificationHash, &row.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load active DeploymentBaseline: %w", err)
	}
	return row, nil
}

func supersede(ctx context.Context, tx baseline.Transaction, row *baselineRow) error {
	result, err := tx.ExecContext(ctx, `
UPDATE deployment_baselines
SET status = 'superseded', row_version = row_version + 1,
    superseded_at = NOW(6), updated_at = NOW(6)
WHERE id = ? AND status = 'active' AND row_version = ?`,
		row.ID, row.RowVersion)
	if err != nil {
		return fmt.Errorf("supersede active DeploymentBaseline: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: active DeploymentBaseline changed concurrently", baseline.ErrConflict)
	}
	return nil
}

func compareExisting(row baselineRow, snapshot baseline.Snapshot) error {
	if row.Status != "active" || row.BaselineSchemaVersion != baseline.BaselineSchemaVersion ||
		row.RowVersion != 1 || row.PublicID != snapshot.PublicID() ||
		row.TargetIdentityHash != snapshot.TargetIdentityHash || row.Target != snapshot.Target ||
		row.SourceRevision != snapshot.SourceRevision || row.ImageDigest != snapshot.ImageDigest ||
		row.GitOpsRevision != snapshot.GitOpsRevision || row.ConfigHash != snapshot.ConfigHash ||
		row.PolicyVersion != snapshot.VerificationPolicyVersion || row.VerificationHash != snapshot.VerificationHash ||
		!sameMySQLTime(row.VerifiedAt, snapshot.VerifiedAt) {
		return fmt.Errorf("%w: exact baseline identity differs from the existing row", baseline.ErrConflict)
	}
	return nil
}

func loadObservationIDs(ctx context.Context, tx baseline.Transaction, rowID uint64, snapshot baseline.Snapshot) (ids []uint64, retErr error) {
	rows, err := tx.QueryContext(ctx, `
	SELECT id, public_id, observation_schema_version, sequence_no,
       observation_type, source_identity, CAST(observed_json AS CHAR), content_hash,
       dedupe_key, observed_at
FROM baseline_observations
WHERE baseline_id = ?
ORDER BY sequence_no`, rowID)
	if err != nil {
		return nil, fmt.Errorf("load existing baseline observations: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close existing baseline observation rows: %w", closeErr))
		}
	}()
	ids = make([]uint64, 0, len(snapshot.Observations))
	index := 0
	for rows.Next() {
		var (
			id, sequence             uint64
			observationSchemaVersion uint16
			publicID, typ, source    string
			observed                 []byte
			contentHash, dedupeKey   string
			observedAt               time.Time
		)
		if err := rows.Scan(&id, &publicID, &observationSchemaVersion,
			&sequence, &typ, &source, &observed, &contentHash, &dedupeKey, &observedAt); err != nil {
			return nil, fmt.Errorf("scan existing baseline observation: %w", err)
		}
		if index >= len(snapshot.Observations) {
			return nil, fmt.Errorf("%w: existing baseline has extra observations", baseline.ErrConflict)
		}
		expected := snapshot.Observations[index]
		if observationSchemaVersion != baseline.ObservationSchemaVersion ||
			sequence != uint64(index+1) || publicID != observationPublicID(snapshot.PublicID(), expected) ||
			typ != string(expected.Type) || source != expected.SourceIdentity || !sameJSON(observed, expected.ObservedJSON) ||
			contentHash != expected.ContentHash || dedupeKey != expected.DedupeKey || !sameMySQLTime(observedAt, expected.ObservedAt) {
			return nil, fmt.Errorf("%w: existing baseline observation %s differs", baseline.ErrConflict, expected.Type)
		}
		ids = append(ids, id)
		index++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing baseline observations: %w", err)
	}
	if index != len(snapshot.Observations) {
		return nil, fmt.Errorf("%w: existing baseline observation count differs", baseline.ErrConflict)
	}
	return ids, nil
}

func sameMySQLTime(left, right time.Time) bool {
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}

func observationPublicID(baselinePublicID string, observation baseline.Observation) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(baselinePublicID+"\x00"+string(observation.Type))).String()
}

func sameJSON(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftCanonical, _ := json.Marshal(leftValue)
	rightCanonical, _ := json.Marshal(rightValue)
	return string(leftCanonical) == string(rightCanonical)
}

func activeID(row *baselineRow) uint64 {
	if row == nil {
		return 0
	}
	return row.ID
}

func acquireLock(ctx context.Context, conn *sql.Conn, name string, timeout time.Duration) error {
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, seconds).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire baseline activation lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("%w: %s", baseline.ErrLockUnavailable, name)
	}
	return nil
}
