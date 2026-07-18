package migrations

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestImmutableMigrationHistory(t *testing.T) {
	expected := map[string]string{
		"00001_incident_foundation.sql":                   "8f7dd2e188fba00f6a7cce45f7c2f2319a4f80c7df7882c1a3e4c6471bce080d",
		"00002_agent_runtime.sql":                         "f515765f604391c933bc59b6fd3b7c7d3cfd17f3c429a403bcc014b82d370411",
		"00003_change_intelligence.sql":                   "584fda2c41ca7657228aba5f0b4b0e2d62bd2a4cfd54ec386198c22e163fd0f6",
		"00004_gitops_remediation.sql":                    "fc4773b65f89626bcfb678526d3c631bef89188007df00cda7a96813d7f95c84",
		"00005_delivery_verification.sql":                 "8168e75a60f18ba5b8818c82b6dda71131adc8b9cca1f52eb93abce111f4a61d",
		"00006_observability_verification_postmortem.sql": "8e1a0b1f0a1125ffb54f8049be17617c1130cebbf446ecc371a281b4a7301fdb",
		"00007_expand_legacy_schema.sql":                  "e254655698086f7ff3679fe615d0d7b6c2bd58158eb44501086ca37f44c54f45",
	}
	for name, expectedHash := range expected {
		contents, err := FS.ReadFile(name)
		if err != nil {
			t.Fatalf("read immutable migration %s: %v", name, err)
		}
		actualHash := fmt.Sprintf("%x", sha256.Sum256(contents))
		if actualHash != expectedHash {
			t.Errorf("immutable migration %s sha256=%s, want %s", name, actualHash, expectedHash)
		}
	}
}

func TestPhase2MigrationIsForwardExpandOnly(t *testing.T) {
	contents, err := FS.ReadFile("00008_expand_v3_async_runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(contents)
	if !strings.HasPrefix(sqlText, "-- +goose Up") || !strings.Contains(sqlText, "-- +goose NO TRANSACTION") {
		t.Fatal("00008 must be an explicit forward Goose migration")
	}
	forbidden := regexp.MustCompile(`(?im)^\s*(UPDATE|INSERT|DELETE|TRUNCATE|DROP\s+TABLE|DROP\s+COLUMN|RENAME\s+TABLE)\b`)
	if match := forbidden.FindString(sqlText); match != "" {
		t.Fatalf("00008 contains non-expand statement %q", strings.TrimSpace(match))
	}
	for _, required := range []string{
		"CREATE TABLE async_tasks",
		"CREATE TABLE async_task_attempts",
		"CREATE TABLE signal_rejections",
		"CREATE TABLE command_idempotency_records",
		"CREATE TABLE migration_ledger",
		"active_correlation_key",
		"active_incident_cycle_key",
		"trigger_identity",
		"expected_subject_version",
		"idx_async_tasks_ready_claim",
		"idx_async_tasks_expired_takeover",
		"chk_async_tasks_queue_type",
		"chk_async_tasks_subject_transition",
	} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("00008 missing required Phase 2 contract %q", required)
		}
	}
	if strings.Contains(sqlText, "CUTOVER_V3") {
		t.Fatal("00008 must not write a CUTOVER_V3 marker")
	}
}
