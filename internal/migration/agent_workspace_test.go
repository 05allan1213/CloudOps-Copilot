package migration

import (
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestAgentWorkspaceMigrationPreservesEvidenceAndAuthorityBoundaries(t *testing.T) {
	contents, err := migrations.FS.ReadFile("00008_agent_workspace.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"`subject_type` varchar(16)",
		"`run_kind` varchar(16)",
		"`context_snapshot_id`",
		"CREATE TABLE `agent_consultation_messages`",
		"CREATE TABLE `agent_stream_events`",
		"CREATE TABLE `agent_evidence_citations`",
		"CREATE TABLE `knowledge_items`",
		"CREATE TABLE `knowledge_item_revisions`",
		"CREATE TABLE `agent_guidance_citations`",
		"CREATE TABLE `agent_action_cards`",
		"CREATE TABLE `agent_operation_plans`",
		"CREATE TABLE `agent_action_authorizations`",
		"`authorized_content_hash`",
		"`confirmed_by` = _ascii'local-owner'",
		"`producer_type` = _ascii'query_execution'",
		"`producer_type` = _ascii'agent_step'",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("Phase 6 migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{"shell_command", "kubectl_command", "automatic_authorization", "chain_of_thought"} {
		if strings.Contains(sqlText, forbidden) {
			t.Fatalf("Phase 6 migration contains forbidden authority or reasoning field %q", forbidden)
		}
	}
}
