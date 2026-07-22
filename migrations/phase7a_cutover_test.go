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

func TestPhase7ABackfillContractIsForwardOnlyAndComplete(t *testing.T) {
	contents, err := FS.ReadFile("00017_phase7a_backfill_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"CREATE TABLE migration_backfill_cursors",
		"CREATE TABLE legacy_signal_archive",
		"CREATE TABLE legacy_event_archive",
		"CREATE TABLE legacy_evidence_archive",
		"CREATE TABLE legacy_agent_step_archive",
		"CREATE TABLE legacy_change_candidate_archive",
		"CREATE TABLE legacy_change_assessment_archive",
		"CREATE TABLE legacy_remediation_plan_archive",
		"CREATE TABLE legacy_approval_archive",
		"CREATE TABLE legacy_outbox_event_registry",
		"source_checkpoint_canonical_hash",
		"previous_conversion_id",
		"migrated_legacy_context",
		"ALTER TABLE resolution_reports ADD COLUMN migrated_legacy_context",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("00017 missing %q", required)
		}
	}
	for _, forbidden := range []string{"-- +goose Down", "DROP TABLE", "DROP COLUMN", "DELETE FROM", "TRUNCATE", "CUTOVER-V3'"} {
		if strings.Contains(sqlText, forbidden) {
			t.Fatalf("00017 contains forbidden contract %q", forbidden)
		}
	}
	approvalStart := strings.Index(sqlText, "CREATE TABLE legacy_approval_archive")
	registryStart := strings.Index(sqlText, "CREATE TABLE legacy_outbox_event_registry")
	if approvalStart < 0 || registryStart <= approvalStart ||
		!strings.Contains(sqlText[approvalStart:registryStart], "converter_result VARCHAR(32)") {
		t.Fatal("00017 legacy Approval archive cannot store non_authoritative converter result")
	}
}
