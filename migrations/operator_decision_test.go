package migrations

import (
	"strings"
	"testing"
)

func TestOperatorOnlyRemediationDecisionMigration(t *testing.T) {
	contents, err := FS.ReadFile("00015_operator_only_remediation_decisions.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	for _, required := range []string{
		"-- +goose Up",
		"-- +goose NO TRANSACTION",
		"ALTER TABLE remediation_decisions",
		"DROP CHECK chk_remediation_decisions_identity",
		"actor_provider = 'github'",
		"actor_role = 'operator'",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("00015 missing operator-only Decision contract %q", required)
		}
	}
	if strings.Contains(sqlText, "actor_role IN") || strings.Contains(sqlText, "-- +goose Down") {
		t.Fatal("00015 preserves a multi-role Decision CHECK or adds a reverse migration")
	}
}
