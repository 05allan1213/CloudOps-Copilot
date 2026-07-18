package asyncjob

import (
	"strings"
	"testing"
)

func TestClaimSQLUsesSkipLockedAndMySQLClock(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"ready":    claimReadySelectSQL,
		"takeover": takeoverSelectSQL,
	} {
		if !strings.Contains(query, "FOR UPDATE SKIP LOCKED") || !strings.Contains(query, "NOW(6)") || !strings.Contains(query, "WHERE queue = ?") {
			t.Fatalf("%s claim SQL lacks queue-scoped SKIP LOCKED/NOW(6):\n%s", name, query)
		}
	}
	if !strings.Contains(claimReadySelectSQL, "FORCE INDEX (idx_async_tasks_ready_claim)") || !strings.Contains(takeoverSelectSQL, "FORCE INDEX (idx_async_tasks_expired_takeover)") {
		t.Fatal("claim SQL does not force the migration-owned target index")
	}
	if !strings.Contains(claimReadySelectSQL, "attempt < max_attempts") || !strings.Contains(takeoverSelectSQL, "lease_expires_at <= NOW(6)") {
		t.Fatal("claim predicates do not separate ready and expired takeover paths")
	}
}

func TestRunningWritesCarryCompleteFence(t *testing.T) {
	t.Parallel()
	queries := map[string]string{
		"heartbeat":  heartbeatUpdateSQL,
		"checkpoint": checkpointUpdateSQL,
		"succeeded":  resolveSucceededSQL,
		"retry":      resolveRetrySQL,
		"dead":       resolveDeadSQL,
	}
	required := []string{
		"WHERE id = ?",
		"status = 'running'",
		"lease_owner = ?",
		"lease_generation = ?",
		"expected_subject_version = ?",
		"lease_expires_at > NOW(6)",
	}
	for name, query := range queries {
		for _, fragment := range required {
			if !strings.Contains(query, fragment) {
				t.Fatalf("%s SQL lacks %q:\n%s", name, fragment, query)
			}
		}
	}
}

func TestLeaseTimeWritesUseMySQLNow(t *testing.T) {
	t.Parallel()
	queries := []string{claimReadyUpdateSQL, takeoverUpdateSQL, heartbeatUpdateSQL, resolveRetrySQL, resolveSucceededSQL, resolveDeadSQL, checkpointUpdateSQL}
	for _, query := range queries {
		if !strings.Contains(query, "NOW(6)") {
			t.Fatalf("SQL uses no MySQL NOW(6):\n%s", query)
		}
	}
}
