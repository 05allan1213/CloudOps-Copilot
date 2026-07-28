package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type MySQLRepository struct {
	db  *sql.DB
	now func() time.Time
}

func NewMySQLRepository(db *sql.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("infrastructure database is required")
	}
	return &MySQLRepository{db: db, now: time.Now}, nil
}

func (r *MySQLRepository) Store(ctx context.Context, revisionPublicID string, snapshot *TopologySnapshot) error {
	if snapshot == nil {
		return errors.New("topology snapshot is required")
	}
	if snapshot.ProviderState != ProviderAvailable && snapshot.ProviderState != ProviderPartial {
		return errors.New("only available or partial topology snapshots are durable")
	}
	if len(snapshot.Nodes) > MaximumLimit || len(snapshot.Edges) > 2000 {
		return errors.New("topology snapshot exceeds durable bounds")
	}
	contentHash, err := ProjectionHash(snapshot.Nodes, snapshot.Edges)
	if err != nil {
		return fmt.Errorf("hash topology projection: %w", err)
	}
	scopeHash, err := ScopeHash(snapshot.Scope)
	if err != nil {
		return fmt.Errorf("hash topology scope: %w", err)
	}
	namespacesJSON, err := json.Marshal(snapshot.Scope.Namespaces)
	if err != nil {
		return fmt.Errorf("encode topology namespaces: %w", err)
	}
	projectionJSON, err := json.Marshal(struct {
		Nodes  []Resource      `json:"nodes"`
		Edges  []TopologyEdge  `json:"edges"`
		Issues []ProviderIssue `json:"issues"`
	}{
		Nodes: snapshot.Nodes, Edges: snapshot.Edges, Issues: snapshot.Issues,
	})
	if err != nil {
		return fmt.Errorf("encode topology projection: %w", err)
	}
	if len(projectionJSON) > 1024*1024 {
		return errors.New("topology projection exceeds one MiB")
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin topology snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var revisionID uint64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM configuration_revisions WHERE public_id = ?`, revisionPublicID).Scan(&revisionID); err != nil {
		return fmt.Errorf("resolve topology configuration revision: %w", err)
	}
	var snapshotID uint64
	var snapshotPublicID string
	err = tx.QueryRowContext(ctx, `SELECT id, public_id FROM topology_snapshots
WHERE configuration_revision_id = ? AND scope_hash = ? AND content_hash = ? FOR UPDATE`,
		revisionID, scopeHash, contentHash).Scan(&snapshotID, &snapshotPublicID)
	observedAt := r.now().UTC()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		snapshotPublicID = uuid.NewString()
		result, insertErr := tx.ExecContext(ctx, `INSERT INTO topology_snapshots (
public_id, configuration_revision_id, cluster_id, environment, namespaces_json,
scope_hash, content_hash, provider_state, source_identity, server_version,
partial, truncated, node_count, edge_count, projection_json,
collected_at, fresh_until, last_observed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
id = LAST_INSERT_ID(id), cluster_id = VALUES(cluster_id), environment = VALUES(environment),
namespaces_json = VALUES(namespaces_json), provider_state = VALUES(provider_state),
source_identity = VALUES(source_identity), server_version = VALUES(server_version),
partial = VALUES(partial), truncated = VALUES(truncated), node_count = VALUES(node_count),
edge_count = VALUES(edge_count), projection_json = VALUES(projection_json),
collected_at = VALUES(collected_at), fresh_until = VALUES(fresh_until),
last_observed_at = VALUES(last_observed_at)`,
			snapshotPublicID, revisionID, snapshot.Scope.ClusterID, snapshot.Scope.Environment, namespacesJSON,
			scopeHash, contentHash, snapshot.ProviderState, snapshot.Source.Identity, snapshot.Source.ServerVersion,
			snapshot.Partial, snapshot.Truncated, len(snapshot.Nodes), len(snapshot.Edges), projectionJSON,
			snapshot.CollectedAt.UTC(), snapshot.Freshness.FreshUntil.UTC(), observedAt)
		if insertErr != nil {
			return fmt.Errorf("insert topology snapshot: %w", insertErr)
		}
		insertedID, insertErr := result.LastInsertId()
		if insertErr != nil || insertedID <= 0 {
			return fmt.Errorf("read topology snapshot identity: %w", insertErr)
		}
		snapshotID = uint64(insertedID)
		if err := tx.QueryRowContext(ctx, `SELECT public_id FROM topology_snapshots WHERE id = ?`, snapshotID).Scan(&snapshotPublicID); err != nil {
			return fmt.Errorf("read concurrent topology snapshot public identity: %w", err)
		}
	case err != nil:
		return fmt.Errorf("load topology snapshot: %w", err)
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE topology_snapshots
SET provider_state = ?, source_identity = ?, server_version = ?, partial = ?, truncated = ?,
projection_json = ?, collected_at = ?, fresh_until = ?, last_observed_at = ?
WHERE id = ?`,
			snapshot.ProviderState, snapshot.Source.Identity, snapshot.Source.ServerVersion,
			snapshot.Partial, snapshot.Truncated, projectionJSON, snapshot.CollectedAt.UTC(),
			snapshot.Freshness.FreshUntil.UTC(), observedAt, snapshotID); err != nil {
			return fmt.Errorf("refresh topology snapshot: %w", err)
		}
	}

	for _, resource := range snapshot.Nodes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_identities (
resource_id, cluster_id, api_version, kind, namespace, name, source_uid,
health_state, last_snapshot_id, first_seen_at, last_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
api_version = VALUES(api_version), kind = VALUES(kind), namespace = VALUES(namespace),
name = VALUES(name), source_uid = VALUES(source_uid), health_state = VALUES(health_state),
last_snapshot_id = VALUES(last_snapshot_id), last_seen_at = VALUES(last_seen_at)`,
			resource.ID, snapshot.Scope.ClusterID, resource.APIVersion, resource.Kind, resource.Namespace,
			resource.Name, resource.SourceUID, resource.Health.State, snapshotID,
			snapshot.CollectedAt.UTC(), snapshot.CollectedAt.UTC()); err != nil {
			return fmt.Errorf("upsert Kubernetes resource identity %q: %w", resource.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit topology snapshot: %w", err)
	}
	snapshot.ID = snapshotPublicID
	snapshot.ContentHash = contentHash
	return nil
}

var _ SnapshotRepository = (*MySQLRepository)(nil)
