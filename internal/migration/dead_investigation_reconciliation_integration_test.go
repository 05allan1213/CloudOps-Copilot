package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeadInvestigationRunReconciliationMigration(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := openSQL(t, adminDSN)
	defer func() { _ = admin.Close() }()
	databaseName := fmt.Sprintf("cloudops_dead_run_reconcile_%d", time.Now().UnixNano())
	createDatabase(t, ctx, admin, databaseName)
	defer dropDatabase(t, admin, databaseName)

	db := openSQL(t, databaseDSN(t, adminDSN, databaseName))
	defer func() { _ = db.Close() }()
	runner := newTestRunner(t, ctx, db, 10*time.Second)
	if _, err := runner.provider.UpTo(ctx, 17); err != nil {
		t.Fatalf("apply migrations through 00017: %v", err)
	}

	orphanIncidentID, orphanRunID := insertDeadRunMigrationFixture(t, ctx, db, false)
	liveIncidentID, liveRunID := insertDeadRunMigrationFixture(t, ctx, db, true)
	if _, err := runner.Up(ctx); err != nil {
		t.Fatalf("apply 00018 reconciliation: %v", err)
	}
	assertVersion(t, ctx, runner, LatestVersion)

	assertMigrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
	WHERE id = ? AND status = 'FAILED' AND v3_status = 'failed' AND row_version = 4
	  AND failure_code = 'invalid_agent_run_state'`, 1, orphanRunID)
	assertMigrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
	WHERE incident_id = ? AND event_type = 'agent_run_failed' AND actor_id = 'migration-00018'`, 1, orphanIncidentID)
	assertMigrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
	WHERE id = ? AND status = 'RUNNING' AND v3_status = 'running' AND row_version = 3`, 1, liveRunID)
	assertMigrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
	WHERE incident_id = ? AND event_type = 'agent_run_failed' AND actor_id = 'migration-00018'`, 0, liveIncidentID)

	if results, err := runner.Up(ctx); err != nil || len(results) != 0 {
		t.Fatalf("repeat reconciliation migration results=%d err=%v", len(results), err)
	}
}

func insertDeadRunMigrationFixture(t *testing.T, ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, withLiveReplay bool) (uint64, uint64) {
	t.Helper()
	incidentPublicID := uuid.NewString()
	correlationHash := strings.Repeat(strings.ReplaceAll(incidentPublicID, "-", ""), 2)[:64]
	incidentResult, err := db.ExecContext(ctx, `INSERT INTO incidents
	 (public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
	  service_name, environment, target_kind, target_name, severity, status, summary,
	  first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no,
	  needs_attention, blocking_reason_code, blocked_at)
	VALUES (?, ?, ?, 2, 'kind', 'demo', 'checkout', 'demo', 'Deployment', 'checkout',
	        'warning', 'DIAGNOSING', 'dead task migration fixture', NOW(6), NOW(6), 2, 3,
	        'investigating', 1, TRUE, 'task_dead', NOW(6))`,
		incidentPublicID, "dead-run-"+incidentPublicID, "v2:"+correlationHash)
	if err != nil {
		t.Fatal(err)
	}
	incidentID64, _ := incidentResult.LastInsertId()
	incidentID := uint64(incidentID64)
	runPublicID := uuid.NewString()
	runResult, err := db.ExecContext(ctx, `INSERT INTO agent_runs
	 (public_id, incident_id, status, model, prompt_version, max_steps, failure_code,
	  row_version, domain_schema_version, v3_status, cycle_no, expected_incident_version)
	VALUES (?, ?, 'RUNNING', 'fixture-model', 'incident-agent-v3', 1, '', 3, 3, 'running', 1, 2)`,
		runPublicID, incidentID)
	if err != nil {
		t.Fatal(err)
	}
	runID64, _ := runResult.LastInsertId()
	runID := uint64(runID64)
	dedupe := fmt.Sprintf("%064x", runID)
	deadResult, err := db.ExecContext(ctx, `INSERT INTO async_tasks
	 (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
	  expected_subject_version, payload_schema_version, payload_json, dedupe_key, replay_generation,
	  status, priority, available_at, attempt, max_attempts, lease_generation,
	  last_error_code, last_error_summary, dead_at, created_at, updated_at)
	VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'agent_run', ?, 'investigation.step',
	        3, 1, JSON_OBJECT('mode','synthesize','agent_run_id',?,'cycle_no',1), ?, 0,
	        'dead', 50, NOW(6), 1, 5, 1,
	        'invalid_agent_run_state', 'durable Evidence provenance is invalid', NOW(6), NOW(6), NOW(6))`,
		uuid.NewString(), incidentID, runID, runPublicID, dedupe)
	if err != nil {
		t.Fatal(err)
	}
	if withLiveReplay {
		deadTaskID, _ := deadResult.LastInsertId()
		if _, err := db.ExecContext(ctx, `INSERT INTO async_tasks
		 (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
		  expected_subject_version, payload_schema_version, payload_json, dedupe_key, replay_generation,
		  status, priority, available_at, attempt, max_attempts, lease_generation,
		  replayed_from_task_id, created_at, updated_at)
		VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'agent_run', ?, 'investigation.step',
		        3, 1, JSON_OBJECT('mode','synthesize','agent_run_id',?,'cycle_no',1), ?, 1,
		        'ready', 50, NOW(6), 0, 5, 0, ?, NOW(6), NOW(6))`,
			uuid.NewString(), incidentID, runID, runPublicID, dedupe, deadTaskID); err != nil {
			t.Fatal(err)
		}
	}
	return incidentID, runID
}

func assertMigrationCount(t *testing.T, ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d query=%s", got, want, query)
	}
}
