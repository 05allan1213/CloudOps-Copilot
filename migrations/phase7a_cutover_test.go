package migrations

import (
	"strings"
	"testing"
)

func TestPhase7ACutoverMigrationIsArchiveOnlyAndProvenanceBound(t *testing.T) {
	contents, err := FS.ReadFile("00016_phase7a_cutover_archives.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"CREATE TABLE cutover_controls", "CREATE TABLE legacy_outbox_archive",
		"CREATE TABLE legacy_incident_state_archive", "CREATE TABLE legacy_agent_checkpoint_archive",
		"CREATE TABLE legacy_change_request_archive", "CREATE TABLE legacy_verification_archive",
		"CREATE TABLE legacy_postmortem_archive", "CREATE TABLE legacy_conversion_records",
		"migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE", "source_outbox_id",
		"target_task_id", "anti_join_result",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("00016 missing %q", required)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM", "TRUNCATE", "-- +goose Down"} {
		if strings.Contains(sqlText, forbidden) {
			t.Fatalf("00016 contains destructive contract %q", forbidden)
		}
	}
}
