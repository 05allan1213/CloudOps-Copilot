package migration

import (
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestAgentWorkspaceTaskMigrationOwnsDurableClaimBoundary(t *testing.T) {
	contents, err := migrations.FS.ReadFile("00009_agent_workspace_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"CREATE TABLE `agent_workspace_tasks`",
		"CREATE TABLE `agent_workspace_task_attempts`",
		"UNIQUE KEY `uk_agent_workspace_tasks_run`",
		"`configuration_revision_id` bigint unsigned NOT NULL",
		"`lease_generation` bigint unsigned NOT NULL",
		"`claim_kind` in (_ascii'ready',_ascii'takeover')",
		"`status` = _ascii'running' AND `lease_owner` is not null",
		"FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs`",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("Workspace task migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{"incident_id", "cycle_no", "shell_command", "chain_of_thought"} {
		if strings.Contains(sqlText, forbidden) {
			t.Fatalf("Workspace task migration contains forbidden coupling %q", forbidden)
		}
	}
}
