package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// backfillChangeCandidates closes the legacy Change -> V3 read/archive
// projection anti-join. Native V3 change_candidates and assessments are left
// byte-for-byte untouched; legacy Change facts were archived by BACKFILL-V3.
func backfillChangeCandidates(ctx context.Context, tx *sql.Tx, _ time.Time) error {
	if tx == nil {
		return errors.New("legacy Change candidate audit transaction is required")
	}
	var archived, projected, missing uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM legacy_change_candidate_archive").Scan(&archived); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM changes WHERE domain_schema_version=3 AND migrated_legacy=TRUE").Scan(&projected); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_change_candidate_archive a
LEFT JOIN changes c ON c.id=a.source_change_id AND c.domain_schema_version=3 AND c.migrated_legacy=TRUE
WHERE c.id IS NULL`).Scan(&missing); err != nil {
		return err
	}
	if archived != projected || missing != 0 {
		return fmt.Errorf("legacy Change candidate archive parity archived=%d projected=%d missing=%d", archived, projected, missing)
	}
	return nil
}

func archivePostmortemsV2(ctx context.Context, tx *sql.Tx, at time.Time) error {
	if tx == nil {
		return errors.New("legacy Postmortem archive transaction is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,incident_id,public_id,generated_at,CAST(JSON_OBJECT(
'id',id,'public_id',public_id,'incident_id',incident_id,'title',title,'impact_summary',impact_summary,
'root_cause_json',root_cause_json,'remediation_summary_json',remediation_summary_json,
'approval_summary_json',approval_summary_json,'delivery_revision',delivery_revision,
'verification_summary',verification_summary,'checks_json',checks_json,'timeline_json',timeline_json,
'follow_up_actions_json',follow_up_actions_json,'generation_version',generation_version,
'generated_at',generated_at,'created_at',created_at) AS CHAR)
FROM postmortems ORDER BY id FOR SHARE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct {
		id, incidentID uint64
		publicID       string
		generatedAt    time.Time
		content        []byte
		hash           string
	}
	items := make([]item, 0)
	for rows.Next() {
		var row item
		if err := rows.Scan(&row.id, &row.incidentID, &row.publicID, &row.generatedAt, &row.content); err != nil {
			return err
		}
		hash, canonical, err := canonicalHashJSON(row.content)
		if err != nil {
			return fmt.Errorf("canonicalize legacy Postmortem id=%d: %w", row.id, err)
		}
		row.content, row.hash = canonical, hash
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, row := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_postmortem_archive
(source_postmortem_id,incident_id,source_public_id,content_json,content_hash,generated_at,archived_at)
VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE source_postmortem_id=VALUES(source_postmortem_id)`,
			row.id, row.incidentID, row.publicID, row.content, row.hash, row.generatedAt.UTC(), at.UTC()); err != nil {
			return fmt.Errorf("archive legacy Postmortem id=%d: %w", row.id, err)
		}
		var archivedHash string
		if err := tx.QueryRowContext(ctx, "SELECT content_hash FROM legacy_postmortem_archive WHERE source_postmortem_id=? FOR UPDATE", row.id).Scan(&archivedHash); err != nil || archivedHash != row.hash {
			return fmt.Errorf("legacy Postmortem archive hash drift id=%d: %w", row.id, err)
		}
	}
	return nil
}
