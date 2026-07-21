package migration

import (
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestEvidenceSupersessionMigrationIsForwardOnlyAndCycleBound(t *testing.T) {
	if LatestVersion != 16 {
		t.Fatalf("latest migration=%d, want 16", LatestVersion)
	}
	contents, err := migrations.FS.ReadFile("00011_evidence_supersessions.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"CREATE TABLE evidence_supersessions",
		"uk_evidence_items_v3_owner (id, incident_id, cycle_no)",
		"FOREIGN KEY (superseded_evidence_id, incident_id, cycle_no)",
		"FOREIGN KEY (superseding_evidence_id, incident_id, cycle_no)",
		"UNIQUE KEY uk_evidence_supersessions_superseded",
		"UNIQUE KEY uk_evidence_supersessions_superseding",
		"superseding_evidence_id > superseded_evidence_id",
		"ON DELETE RESTRICT",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}
	if strings.Contains(sqlText, "-- +goose Down") || strings.Contains(sqlText, "UPDATE evidence_items") {
		t.Fatal("Evidence supersession migration is not forward-only append-only")
	}
}
