package migration

import (
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestBaselineIncludesDurableBusinessBudgetLineage(t *testing.T) {
	contents, err := migrations.FS.ReadFile("00001_cloudops_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"CREATE TABLE `incident_cycle_budget_authorizations`",
		"slot_no` in (4,5)",
		"uk_cycle_budget_authorizations_slot",
		"business_budget_authorization_id",
		"originating_agent_run_id",
		"uk_agent_runs_budget_authorization",
		"uk_remediation_plans_budget_authorization",
		"uk_verification_runs_budget_authorization",
		"FOREIGN KEY (`business_budget_authorization_id`, `incident_id`, `cycle_no`)",
		"REFERENCES `incident_cycle_budget_authorizations` (`id`, `incident_id`, `cycle_no`)",
		"FOREIGN KEY (`originating_agent_run_id`, `incident_id`, `cycle_no`)",
		"ON DELETE RESTRICT",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("baseline is missing %q", required)
		}
	}
}
