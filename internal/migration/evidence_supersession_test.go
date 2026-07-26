package migration

import (
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestBaselineEvidenceSupersessionIsCycleBound(t *testing.T) {
	if LatestVersion != 1 {
		t.Fatalf("latest migration=%d, want 1", LatestVersion)
	}
	contents, err := migrations.FS.ReadFile("00001_cloudops_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"CREATE TABLE `evidence_supersessions`",
		"uk_evidence_items_owner` (`id`,`incident_id`,`cycle_no`)",
		"FOREIGN KEY (`superseded_evidence_id`, `incident_id`, `cycle_no`)",
		"FOREIGN KEY (`superseding_evidence_id`, `incident_id`, `cycle_no`)",
		"UNIQUE KEY `uk_evidence_supersessions_superseded`",
		"UNIQUE KEY `uk_evidence_supersessions_superseding`",
		"superseding_evidence_id` > `superseded_evidence_id",
		"ON DELETE RESTRICT",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("baseline is missing %q", required)
		}
	}
	if strings.Contains(sqlText, "-- +goose Down") {
		t.Fatal("baseline unexpectedly contains a reverse migration")
	}
}
