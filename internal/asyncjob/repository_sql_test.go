package asyncjob

import (
	"errors"
	"fmt"
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

func TestDeterministicMutationErrorsBecomeBoundedDeadResults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		err     error
		code    string
		summary string
	}{
		{"subject version", fmt.Errorf("wrapped: %w", ErrSubjectVersionMismatch), "subject_version_mismatch", "task subject version or Incident cycle no longer matches"},
		{"invalid", fmt.Errorf("wrapped: %w", ErrInvalidMutation), "invalid_mutation", "task domain mutation rejected invalid input"},
		{"policy", fmt.Errorf("wrapped: %w", ErrPolicyViolation), "policy_violation", "task domain mutation was rejected by policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !isDeterministicMutationError(test.err) {
				t.Fatalf("error was not classified deterministic: %v", test.err)
			}
			result := deterministicMutationDead(test.err)
			if result.Disposition != DispositionDead || result.ErrorCode != test.code || result.ErrorSummary != test.summary || result.Mutate != nil {
				t.Fatalf("dead result=%+v", result)
			}
			if err := result.Validate(); err != nil {
				t.Fatalf("dead result invalid: %v", err)
			}
		})
	}
	if isDeterministicMutationError(errors.New("database unavailable")) {
		t.Fatal("transient/unknown error was classified deterministic")
	}
}
