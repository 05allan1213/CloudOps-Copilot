package migration

import (
	"regexp"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestEvidenceDurableContractMigrationUsesRealOwnersAndFrozenProducers(t *testing.T) {
	contents, err := migrations.FS.ReadFile("00014_evidence_durable_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	if strings.Contains(sqlText, "-- +goose Down") {
		t.Fatal("00014 must remain forward-only without a Goose Down section")
	}
	up := strings.Split(sqlText, "-- +goose Down")[0]
	for _, required := range []string{
		"evidence_contract_version", "producer_id", "producer_version", "producer_dedupe_key",
		"'agent_step','verification_check','delivery_observation','system_enrichment'",
		"fact_schema_version", "fact_schema_hash", "provenance_json", "trust_axes_json", "claim_use",
		"corroboration_groups_json", "input_evidence_ids_json", "input_sample_ids_json", "input_hashes_json",
		"redaction_policy_version", "prompt_safety_flags_json", "safe_raw_reference",
		"FOREIGN KEY (agent_step_id, agent_run_id, incident_id, cycle_no)",
		"FOREIGN KEY (verification_check_id, verification_run_id, incident_id, cycle_no)",
		"FOREIGN KEY (change_request_id, incident_id, cycle_no)",
		"REFERENCES agent_steps", "REFERENCES verification_checks", "REFERENCES change_requests",
		"evidence_supersessions", "system_enrichment",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("00014 missing durable Evidence contract %q", required)
		}
	}
	forbidden := regexp.MustCompile(`(?im)^\s*(UPDATE|INSERT|DELETE|TRUNCATE|DROP\s+TABLE|RENAME\s+TABLE)\b`)
	if match := forbidden.FindString(up); match != "" {
		t.Fatalf("00014 up is not additive: %q", strings.TrimSpace(match))
	}
	if strings.Contains(up, "REFERENCES baseline_observations") || strings.Contains(up, "REFERENCES verification_samples") {
		t.Fatal("00014 fabricated a cross-domain derived-input foreign key")
	}
}
