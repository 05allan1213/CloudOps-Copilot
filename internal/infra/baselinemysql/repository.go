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
	"strings"
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
	if err := snapshot.Validate(); err != nil {
		return baseline.ActivationResult{}, err
	}
	if err := snapshot.Finalize(); err != nil {
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

	exact, err := loadExact(ctx, tx, snapshot)
	if err != nil {
		return baseline.ActivationResult{}, err
	}
	if exact != nil {
		switch exact.Status {
		case "active":
			if err := compareExisting(*exact, snapshot); err != nil {
				return baseline.ActivationResult{}, err
			}
			observations, err := loadObservationIDs(ctx, tx, exact.ID, snapshot)
			if err != nil {
				return baseline.ActivationResult{}, err
			}
			if err := tx.Commit(); err != nil {
				return baseline.ActivationResult{}, fmt.Errorf("commit idempotent baseline activation: %w", err)
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

	active, err := loadActive(ctx, tx, snapshot.TargetIdentityHash)
	if err != nil {
		return baseline.ActivationResult{}, err
	}
	if active != nil {
		if err := supersede(ctx, tx, active); err != nil {
			return baseline.ActivationResult{}, err
		}
	}

	publicID := snapshot.PublicID()
	result, err := tx.ExecContext(ctx, `
INSERT INTO deployment_baselines (
    public_id, domain_schema_version, baseline_schema_version,
    target_identity_hash, cluster, environment, namespace, workload_kind,
    workload_name, container_name, repository, base_branch, target_path,
    source_revision, image_digest, gitops_revision, config_hash,
    verification_policy_version, verification_hash, status, row_version,
    verified_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 1, ?, NOW(6), NOW(6))`,
		publicID, baseline.DomainSchemaVersion, baseline.BaselineSchemaVersion,
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
    public_id, domain_schema_version, observation_schema_version,
    baseline_id, sequence_no, observation_type, source_identity,
    observed_json, content_hash, dedupe_key, observed_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6))`,
			observationPublicID(publicID, observation), baseline.DomainSchemaVersion,
			baseline.ObservationSchemaVersion, baselineID, sequence+1, observation.Type,
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
	if err := tx.Commit(); err != nil {
		return baseline.ActivationResult{}, fmt.Errorf("commit baseline activation: %w", err)
	}
	return baseline.ActivationResult{
		BaselineID: uint64(baselineID), PublicID: publicID, Created: true,
		SupersededBaselineID: activeID(active), ObservationIDs: observationIDs,
	}, nil
}

type baselineRow struct {
	ID, RowVersion              uint64
	PublicID, Status            string
	SourceRevision              string
	ImageDigest, GitOpsRevision string
	ConfigHash, PolicyVersion   string
	VerificationHash            string
}

func loadExact(ctx context.Context, tx *sql.Tx, snapshot baseline.Snapshot) (*baselineRow, error) {
	row := &baselineRow{}
	err := tx.QueryRowContext(ctx, `
SELECT id, public_id, status, row_version, source_revision, image_digest,
       gitops_revision, config_hash, verification_policy_version, verification_hash
FROM deployment_baselines
WHERE domain_schema_version = 3
  AND target_identity_hash = ? AND gitops_revision = ? AND config_hash = ?
LIMIT 1 FOR UPDATE`,
		snapshot.TargetIdentityHash, snapshot.GitOpsRevision, snapshot.ConfigHash,
	).Scan(&row.ID, &row.PublicID, &row.Status, &row.RowVersion, &row.SourceRevision,
		&row.ImageDigest, &row.GitOpsRevision, &row.ConfigHash, &row.PolicyVersion,
		&row.VerificationHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load exact DeploymentBaseline: %w", err)
	}
	return row, nil
}

func loadActive(ctx context.Context, tx *sql.Tx, targetHash string) (*baselineRow, error) {
	row := &baselineRow{}
	err := tx.QueryRowContext(ctx, `
SELECT id, public_id, status, row_version, source_revision, image_digest,
       gitops_revision, config_hash, verification_policy_version, verification_hash
FROM deployment_baselines
WHERE domain_schema_version = 3 AND target_identity_hash = ? AND status = 'active'
LIMIT 1 FOR UPDATE`, targetHash).Scan(
		&row.ID, &row.PublicID, &row.Status, &row.RowVersion, &row.SourceRevision,
		&row.ImageDigest, &row.GitOpsRevision, &row.ConfigHash, &row.PolicyVersion,
		&row.VerificationHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load active DeploymentBaseline: %w", err)
	}
	return row, nil
}

func supersede(ctx context.Context, tx *sql.Tx, row *baselineRow) error {
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
	if row.Status != "active" || !strings.EqualFold(row.SourceRevision, snapshot.SourceRevision) ||
		!strings.EqualFold(row.ImageDigest, snapshot.ImageDigest) ||
		!strings.EqualFold(row.GitOpsRevision, snapshot.GitOpsRevision) ||
		!strings.EqualFold(row.ConfigHash, snapshot.ConfigHash) ||
		row.PolicyVersion != snapshot.VerificationPolicyVersion {
		return fmt.Errorf("%w: exact baseline identity differs from the existing row", baseline.ErrConflict)
	}
	return nil
}

func loadObservationIDs(ctx context.Context, tx *sql.Tx, rowID uint64, snapshot baseline.Snapshot) ([]uint64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, observation_type
FROM baseline_observations
WHERE baseline_id = ?
ORDER BY sequence_no`, rowID)
	if err != nil {
		return nil, fmt.Errorf("load existing baseline observations: %w", err)
	}
	defer rows.Close()
	ids := make([]uint64, 0, len(snapshot.Observations))
	index := 0
	for rows.Next() {
		var id uint64
		var typ string
		if err := rows.Scan(&id, &typ); err != nil {
			return nil, fmt.Errorf("scan existing baseline observation: %w", err)
		}
		if index >= len(snapshot.Observations) {
			return nil, fmt.Errorf("%w: existing baseline has extra observations", baseline.ErrConflict)
		}
		expected := snapshot.Observations[index]
		if typ != string(expected.Type) {
			return nil, fmt.Errorf("%w: existing baseline observation type differs", baseline.ErrConflict)
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
