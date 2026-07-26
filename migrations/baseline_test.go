package migrations

import (
	"regexp"
	"strings"
	"testing"
)

func TestSingleSemanticBaselineContract(t *testing.T) {
	entries, err := FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var migrationNames []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			migrationNames = append(migrationNames, entry.Name())
		}
	}
	if len(migrationNames) != 1 || migrationNames[0] != "00001_cloudops_baseline.sql" {
		t.Fatalf("embedded migrations=%v, want one semantic baseline", migrationNames)
	}

	contents, err := FS.ReadFile(migrationNames[0])
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	if !strings.HasPrefix(sqlText, "-- +goose Up\n-- +goose NO TRANSACTION") {
		t.Fatal("baseline must be an explicit forward-only Goose migration")
	}
	if strings.Contains(sqlText, "-- +goose Down") {
		t.Fatal("baseline unexpectedly contains a reverse migration")
	}
	if count := strings.Count(sqlText, "CREATE TABLE `"); count != 30 {
		t.Fatalf("baseline table count=%d want 30", count)
	}

	for _, required := range []string{
		"CREATE TABLE `incidents`", "CREATE TABLE `incident_signals`", "CREATE TABLE `incident_events`",
		"CREATE TABLE `agent_runs`", "CREATE TABLE `agent_steps`", "CREATE TABLE `evidence_items`",
		"CREATE TABLE `evidence_supersessions`", "CREATE TABLE `async_tasks`", "CREATE TABLE `async_task_attempts`",
		"CREATE TABLE `incident_cycle_budget_authorizations`", "CREATE TABLE `remediation_plans`",
		"CREATE TABLE `remediation_decisions`", "CREATE TABLE `change_requests`", "CREATE TABLE `verification_runs`",
		"CREATE TABLE `verification_checks`", "CREATE TABLE `verification_samples`", "CREATE TABLE `data_import_audits`",
		"actor_provider` = _latin1'local'", "actor_login` = _latin1'owner'", "actor_role` = _latin1'owner'",
		"FOREIGN KEY (`business_budget_authorization_id`, `incident_id`, `cycle_no`)",
		"FOREIGN KEY (`agent_step_id`, `agent_run_id`, `incident_id`, `cycle_no`)",
		"FOREIGN KEY (`verification_check_id`, `verification_run_id`, `incident_id`, `cycle_no`)",
		"UNIQUE KEY `uk_evidence_supersessions_superseded`", "UNIQUE KEY `uk_async_tasks_dedupe_generation`",
		"SET FOREIGN_KEY_CHECKS = 0", "SET FOREIGN_KEY_CHECKS = 1",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("baseline missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"domain_schema_version", "v3_status", "write_phase", "generation_version",
		"CREATE TABLE `users`", "CREATE TABLE `migration_ledger`", "CREATE TABLE `cutover_controls`",
		"CREATE TABLE `legacy_", "actor_role` = _utf8mb4'operator'", "bearer", "csrf",
	} {
		if strings.Contains(strings.ToLower(sqlText), strings.ToLower(forbidden)) {
			t.Errorf("baseline retains forbidden compatibility surface %q", forbidden)
		}
	}
	if match := regexp.MustCompile(`(?i)(^|[^a-z0-9])(v2|v3|phase[_ -]?[0-9]+)([^a-z0-9]|$)`).FindString(sqlText); match != "" {
		t.Errorf("baseline retains generation identity %q", match)
	}
}
