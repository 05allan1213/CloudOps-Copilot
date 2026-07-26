package migration

import (
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestBaselineEvidenceContractUsesRealOwnersAndFrozenProducers(t *testing.T) {
	contents, err := migrations.FS.ReadFile("00001_cloudops_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	up := sqlText
	for _, required := range []string{
		"evidence_contract_version", "producer_id", "producer_version", "producer_dedupe_key",
		"_ascii'agent_step',_ascii'verification_check',_ascii'delivery_observation',_ascii'system_enrichment'",
		"fact_schema_version", "fact_schema_hash", "provenance_json", "trust_axes_json", "claim_use",
		"corroboration_groups_json", "input_evidence_ids_json", "input_sample_ids_json", "input_hashes_json",
		"redaction_policy_version", "prompt_safety_flags_json", "safe_raw_reference",
		"FOREIGN KEY (`agent_step_id`, `agent_run_id`, `incident_id`, `cycle_no`)",
		"FOREIGN KEY (`verification_check_id`, `verification_run_id`, `incident_id`, `cycle_no`)",
		"FOREIGN KEY (`change_request_id`, `incident_id`, `cycle_no`)",
		"REFERENCES `agent_steps`", "REFERENCES `verification_checks`", "REFERENCES `change_requests`",
		"evidence_supersessions", "system_enrichment",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("baseline missing durable Evidence contract %q", required)
		}
	}
	if strings.Contains(up, "REFERENCES baseline_observations") || strings.Contains(up, "REFERENCES verification_samples") {
		t.Fatal("baseline fabricated a cross-domain derived-input foreign key")
	}
}
