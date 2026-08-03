package migrations

import (
	"regexp"
	"strings"
	"testing"
)

func TestSemanticMigrationContract(t *testing.T) {
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
	if len(migrationNames) != 12 || migrationNames[0] != "00001_cloudops_baseline.sql" ||
		migrationNames[1] != "00002_platform_foundation.sql" || migrationNames[2] != "00003_infrastructure_topology.sql" ||
		migrationNames[3] != "00004_operational_scope_registry.sql" || migrationNames[4] != "00005_observability_queries.sql" ||
		migrationNames[5] != "00006_telemetry_evidence_context.sql" || migrationNames[6] != "00007_alert_lifecycle.sql" ||
		migrationNames[7] != "00008_agent_workspace.sql" || migrationNames[8] != "00009_agent_workspace_tasks.sql" ||
		migrationNames[9] != "00010_incident_recovery_loop.sql" ||
		migrationNames[10] != "00011_controlled_operations.sql" ||
		migrationNames[11] != "00012_provider_timeout_contract.sql" {
		t.Fatalf("embedded migrations=%v, want exact semantic history through provider timeout contract", migrationNames)
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

	platformContents, err := FS.ReadFile(migrationNames[1])
	if err != nil {
		t.Fatal(err)
	}
	platformSQL := string(platformContents)
	if !strings.HasPrefix(platformSQL, "-- +goose Up\n-- +goose NO TRANSACTION") || strings.Contains(platformSQL, "-- +goose Down") {
		t.Fatal("platform foundation must be an explicit forward-only Goose migration")
	}
	for _, required := range []string{
		"CREATE TABLE `configuration_revisions`", "CREATE TABLE `secret_versions`",
		"CREATE TABLE `provider_configurations`", "CREATE TABLE `operational_scopes`",
		"CREATE TABLE `active_configuration`", "CREATE TABLE `configuration_validations`",
		"CREATE TABLE `provider_health`", "CREATE TABLE `configuration_activation_tasks`",
		"CREATE TABLE `owner_notifications`", "CREATE TABLE `backup_records`",
		"ADD COLUMN `configuration_revision_id`", "fk_async_tasks_configuration_revision",
		"fk_async_task_attempts_configuration_revision", "applied_revision_id",
	} {
		if !strings.Contains(platformSQL, required) {
			t.Errorf("platform foundation missing %q", required)
		}
	}
	for _, forbidden := range []string{"secret_value", "raw_secret", "v2", "v3", "phase_1", "phase 1"} {
		if strings.Contains(strings.ToLower(platformSQL), forbidden) {
			t.Errorf("platform foundation retains forbidden implementation identity %q", forbidden)
		}
	}

	infrastructureContents, err := FS.ReadFile(migrationNames[2])
	if err != nil {
		t.Fatal(err)
	}
	infrastructureSQL := string(infrastructureContents)
	if !strings.HasPrefix(infrastructureSQL, "-- +goose Up\n-- +goose NO TRANSACTION") || strings.Contains(infrastructureSQL, "-- +goose Down") {
		t.Fatal("infrastructure topology must be an explicit forward-only Goose migration")
	}
	for _, required := range []string{
		"CREATE TABLE `topology_snapshots`", "CREATE TABLE `resource_identities`",
		"fk_topology_snapshots_revision", "fk_resource_identities_snapshot",
		"projection_json", "content_hash", "source_uid", "last_observed_at",
	} {
		if !strings.Contains(infrastructureSQL, required) {
			t.Errorf("infrastructure topology missing %q", required)
		}
	}
	for _, forbidden := range []string{"raw_yaml", "secret_value", "v2", "v3", "phase_2", "phase 2"} {
		if strings.Contains(strings.ToLower(infrastructureSQL), forbidden) {
			t.Errorf("infrastructure topology retains forbidden implementation identity %q", forbidden)
		}
	}

	scopeRegistryContents, err := FS.ReadFile(migrationNames[3])
	if err != nil {
		t.Fatal(err)
	}
	scopeRegistrySQL := string(scopeRegistryContents)
	for _, required := range []string{
		"CREATE TABLE `active_operational_scope`", "uk_operational_scopes_revision_cluster",
		"fk_active_operational_scope_scope", "is_default", "DROP INDEX `uk_operational_scopes_revision`",
	} {
		if !strings.Contains(scopeRegistrySQL, required) {
			t.Errorf("operational scope registry missing %q", required)
		}
	}
	for _, forbidden := range []string{"kubeconfig_data", "secret_value", "raw_yaml", "v2", "v3", "phase_2", "phase 2"} {
		if strings.Contains(strings.ToLower(scopeRegistrySQL), forbidden) {
			t.Errorf("operational scope registry retains forbidden implementation identity %q", forbidden)
		}
	}

	observabilityContents, err := FS.ReadFile(migrationNames[4])
	if err != nil {
		t.Fatal(err)
	}
	observabilitySQL := string(observabilityContents)
	for _, required := range []string{
		"CREATE TABLE `query_definitions`", "CREATE TABLE `query_authorizations`",
		"CREATE TABLE `query_executions`", "CREATE TABLE `query_execution_events`",
		"fk_query_definitions_configuration_revision", "fk_query_authorizations_configuration_revision",
		"fk_query_executions_authorization", "uk_query_execution_events_sequence",
		"exact_query_hash", "max_response_bytes", "provider_collected_at",
	} {
		if !strings.Contains(observabilitySQL, required) {
			t.Errorf("observability queries migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"raw_result", "secret_value", "bearer", "v2", "v3", "phase_3", "phase 3"} {
		if strings.Contains(strings.ToLower(observabilitySQL), forbidden) {
			t.Errorf("observability queries migration retains forbidden implementation or telemetry field %q", forbidden)
		}
	}

	telemetryContents, err := FS.ReadFile(migrationNames[5])
	if err != nil {
		t.Fatal(err)
	}
	telemetrySQL := string(telemetryContents)
	for _, required := range []string{
		"CREATE TABLE `agent_consultations`", "CREATE TABLE `context_snapshots`",
		"ADD COLUMN `query_execution_id`", "fk_evidence_items_query_execution",
		"uk_evidence_items_query_content", "'prometheus',_ascii'elasticsearch',_ascii'tempo'",
		"query_execution_refs_json", "evidence_refs_json", "content_hash",
	} {
		if !strings.Contains(telemetrySQL, required) {
			t.Errorf("telemetry Evidence/context migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"raw_result", "provider_response", "secret_value", "bearer", "fixture", "phase_4", "phase 4"} {
		if strings.Contains(strings.ToLower(telemetrySQL), forbidden) {
			t.Errorf("telemetry Evidence/context migration retains forbidden telemetry field or implementation identity %q", forbidden)
		}
	}

	alertContents, err := FS.ReadFile(migrationNames[6])
	if err != nil {
		t.Fatal(err)
	}
	alertSQL := string(alertContents)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS `alert_ingress_locks`", "CREATE TABLE IF NOT EXISTS `alerts`", "CREATE TABLE IF NOT EXISTS `alert_signal_links`", "CREATE TABLE IF NOT EXISTS `alert_events`",
		"CREATE TABLE IF NOT EXISTS `alert_acknowledgements`", "CREATE TABLE IF NOT EXISTS `alert_silences`",
		"CREATE TABLE IF NOT EXISTS `alert_incident_links`", "CREATE TABLE IF NOT EXISTS `escalation_policies`",
		"legacy_automatic_ingress", "DROP CHECK `chk_configuration_revisions_escalation`",
	} {
		if !strings.Contains(alertSQL, required) {
			t.Errorf("Alert lifecycle migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"raw_webhook", "secret_value", "phase_5", "phase 5", "CREATE TABLE `alert_assignments`"} {
		if strings.Contains(strings.ToLower(alertSQL), strings.ToLower(forbidden)) {
			t.Errorf("Alert lifecycle migration retains forbidden field or implementation identity %q", forbidden)
		}
	}

	providerTimeoutContents, err := FS.ReadFile(migrationNames[11])
	if err != nil {
		t.Fatal(err)
	}
	providerTimeoutSQL := string(providerTimeoutContents)
	if !strings.HasPrefix(providerTimeoutSQL, "-- +goose Up\n-- +goose NO TRANSACTION") || strings.Contains(providerTimeoutSQL, "-- +goose Down") {
		t.Fatal("provider timeout contract must be an explicit forward-only Goose migration")
	}
	for _, required := range []string{
		"ALTER TABLE `provider_configurations`", "DROP CHECK `chk_provider_configurations_limits`",
		"between 1000 and 300000", "between 1 and 10000",
	} {
		if !strings.Contains(providerTimeoutSQL, required) {
			t.Errorf("provider timeout contract migration missing %q", required)
		}
	}
}
