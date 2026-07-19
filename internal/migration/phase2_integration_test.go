package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
)

func TestPhase2MigrationGeneratedKeysAndClaimIndexes(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	admin := openSQL(t, adminDSN)
	defer func() { _ = admin.Close() }()

	name := fmt.Sprintf("cloudops_phase2_schema_%d", time.Now().UnixNano())
	createDatabase(t, ctx, admin, name)
	defer dropDatabase(t, admin, name)
	db := openSQL(t, databaseDSN(t, adminDSN, name))
	defer func() { _ = db.Close() }()
	runner := newTestRunner(t, ctx, db, 5*time.Second)
	if _, err := runner.Up(ctx); err != nil {
		t.Fatalf("migrate fresh database through current forward schema: %v", err)
	}
	assertVersion(t, ctx, runner, LatestVersion)
	t.Run("generated expressions match v3 active states", func(t *testing.T) {
		assertPhase2GeneratedExpression(t, ctx, db, "incidents", "active_correlation_key",
			[]string{"detected", "investigating", "awaiting_approval", "delivering", "verifying"},
			[]string{"resolved", "closed"})
		assertPhase2GeneratedExpression(t, ctx, db, "agent_runs", "active_incident_cycle_key",
			[]string{"pending", "running"}, []string{"completed", "failed", "cancelled"})
		assertPhase2GeneratedExpression(t, ctx, db, "remediation_plans", "active_incident_cycle_key",
			[]string{"awaiting_approval", "approved"}, []string{"consumed", "invalidated", "rejected"})
		assertPhase2GeneratedExpression(t, ctx, db, "change_requests", "active_incident_cycle_key",
			[]string{"pending", "pr_open", "merged", "syncing", "rolling_out"},
			[]string{"delivered", "failed", "cancelled", "superseded"})
		assertPhase2GeneratedExpression(t, ctx, db, "verification_runs", "active_incident_cycle_key",
			[]string{"pending", "running"}, []string{"passed", "failed", "inconclusive", "timed_out", "cancelled"})
		assertPhase2GeneratedExpression(t, ctx, db, "verification_runs", "trigger_identity",
			[]string{"post_delivery", "no_change_signal", "change_request_id", "trigger_signal_id"}, nil)
	})

	t.Run("active incident generated key", func(t *testing.T) {
		correlationKey := phase2CorrelationKey(1)
		first := insertPhase2Incident(t, ctx, db, 1, correlationKey, "detected")
		expectPhase2DuplicateKey(t, func() error {
			_, err := insertPhase2IncidentE(ctx, db, 2, correlationKey, "investigating")
			return err
		}, "uk_incidents_v3_active_correlation")
		if _, err := db.ExecContext(ctx, `UPDATE incidents
SET v3_status = 'resolved', resolved_at = NOW(6), terminal_at = NOW(6), version = version + 1
WHERE id = ? AND domain_schema_version = 3 AND v3_status = 'detected'`, first); err != nil {
			t.Fatal(err)
		}
		insertPhase2Incident(t, ctx, db, 3, correlationKey, "investigating")
	})

	incidentID := insertPhase2Incident(t, ctx, db, 10, phase2CorrelationKey(10), "investigating")
	t.Run("active child generated keys", func(t *testing.T) {
		insertPhase2AgentRun(t, ctx, db, incidentID, 1)
		expectPhase2DuplicateKey(t, func() error {
			return insertPhase2AgentRunE(ctx, db, incidentID, 2)
		}, "uk_agent_runs_v3_active_cycle")

		insertPhase2Plan(t, ctx, db, incidentID, 1, 1, "awaiting_approval")
		expectPhase2DuplicateKey(t, func() error {
			return insertPhase2PlanE(ctx, db, incidentID, 2, 2, "approved")
		}, "uk_remediation_plans_v3_active_cycle")

		planA := insertPhase2LegacyPlan(t, ctx, db, incidentID, 10, 10)
		planB := insertPhase2LegacyPlan(t, ctx, db, incidentID, 11, 11)
		insertPhase2ChangeRequest(t, ctx, db, incidentID, planA, 1)
		expectPhase2DuplicateKey(t, func() error {
			return insertPhase2ChangeRequestE(ctx, db, incidentID, planB, 2)
		}, "uk_change_requests_v3_active_cycle")

		signalA := insertPhase2Signal(t, ctx, db, incidentID, 1)
		insertPhase2Verification(t, ctx, db, incidentID, signalA, 1, "passed", 20)
		expectPhase2DuplicateKey(t, func() error {
			return insertPhase2VerificationE(ctx, db, incidentID, signalA, 1, "failed", 21)
		}, "uk_verification_runs_v3_trigger_attempt")

		signalB := insertPhase2Signal(t, ctx, db, incidentID, 2)
		signalC := insertPhase2Signal(t, ctx, db, incidentID, 3)
		insertPhase2Verification(t, ctx, db, incidentID, signalB, 1, "pending", 22)
		expectPhase2DuplicateKey(t, func() error {
			return insertPhase2VerificationE(ctx, db, incidentID, signalC, 1, "running", 23)
		}, "uk_verification_runs_v3_active_cycle")
	})

	t.Run("claim and takeover explain", func(t *testing.T) {
		insertPhase2ExplainTasks(t, ctx, db, incidentID)
		if _, err := db.ExecContext(ctx, "ANALYZE TABLE async_tasks"); err != nil {
			t.Fatal(err)
		}
		readyPlan := phase2Explain(t, ctx, db, `EXPLAIN FORMAT=JSON
SELECT id, lease_generation
FROM async_tasks FORCE INDEX (idx_async_tasks_ready_claim)
WHERE queue = 'investigate'
  AND status = 'ready'
  AND available_at <= NOW(6)
  AND attempt < max_attempts
ORDER BY priority DESC, available_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED`)
		assertPhase2ExplainIndex(t, readyPlan, "idx_async_tasks_ready_claim")

		takeoverPlan := phase2Explain(t, ctx, db, `EXPLAIN FORMAT=JSON
SELECT id, lease_generation, attempt, max_attempts
FROM async_tasks FORCE INDEX (idx_async_tasks_expired_takeover)
WHERE queue = 'investigate'
  AND status = 'running'
  AND lease_expires_at <= NOW(6)
ORDER BY lease_expires_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED`)
		assertPhase2ExplainIndex(t, takeoverPlan, "idx_async_tasks_expired_takeover")
		t.Logf("ready_explain=%s", readyPlan)
		t.Logf("takeover_explain=%s", takeoverPlan)
	})
}

func TestPhase2MigrationExistingPhase1RowsPreserved(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openSQL(t, adminDSN)
	defer func() { _ = admin.Close() }()

	name := fmt.Sprintf("cloudops_phase2_existing_%d", time.Now().UnixNano())
	createDatabase(t, ctx, admin, name)
	defer dropDatabase(t, admin, name)
	db := openSQL(t, databaseDSN(t, adminDSN, name))
	defer func() { _ = db.Close() }()
	runner := newTestRunner(t, ctx, db, 5*time.Second)
	if _, err := runner.provider.UpTo(ctx, 7); err != nil {
		t.Fatalf("prepare Phase 1 schema: %v", err)
	}
	insertPhase2LegacyDomainGraph(t, ctx, db)

	tables := []string{
		"incidents", "incident_signals", "incident_events", "agent_runs", "agent_steps",
		"evidence_items", "changes", "remediation_plans", "remediation_approvals",
		"change_requests", "verification_runs", "verification_checks", "incident_correlation_locks",
		"outbox_events",
	}
	columns := phase2ColumnsBeforeExpand(t, ctx, db, tables)
	before := phase2LegacyDomainSnapshot(t, ctx, db, tables, columns)
	if _, err := runner.Up(ctx); err != nil {
		t.Fatalf("upgrade existing Phase 1 database through current forward schema: %v", err)
	}
	assertVersion(t, ctx, runner, LatestVersion)
	after := phase2LegacyDomainSnapshot(t, ctx, db, tables, columns)
	if before != after {
		t.Fatalf("pre-existing Phase 1 rows changed across forward expansions: before=%s after=%s", before, after)
	}
	t.Logf("legacy_domain_data_sha256=%s", after)
}

func insertPhase2Incident(t *testing.T, ctx context.Context, db *sql.DB, sequence int, correlationKey, status string) uint64 {
	t.Helper()
	id, err := insertPhase2IncidentE(ctx, db, sequence, correlationKey, status)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertPhase2LegacyDomainGraph(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	hash := strings.Repeat("a", 64)
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, cluster, namespace, service_name,
environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version
) VALUES ('legacy-phase2-incident-00000000001', 'legacy-fingerprint', ?, 'cluster-a',
          'default', 'demo', 'development', 'Deployment', 'demo', 'warning',
          'DETECTED', 'legacy phase1 incident', NOW(6), NOW(6), 7)`, "v1:"+hash)
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, status, model, prompt_version, max_steps,
current_checkpoint, failure_code, checkpoint_version, checkpoint_schema_version,
checkpoint_hash, lease_owner, lease_expires_at, heartbeat_at, row_version
) VALUES ('legacy-phase2-agent-run-0000000001', ?, 'PENDING', 'legacy-model',
          'legacy-prompt', 2, JSON_OBJECT('legacy', TRUE), '', 2, 1, ?,
          'legacy-worker', TIMESTAMPADD(SECOND, 30, NOW(6)), NOW(6), 3)`, incidentID, hash)
	if err != nil {
		t.Fatal(err)
	}
	agentRunID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET current_agent_run_id = ? WHERE id = ?", agentRunID, incidentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_steps (
public_id, agent_run_id, sequence, step_type, short_reason, selected_tool,
arguments_json, result_summary, result_ref, status
) VALUES ('legacy-phase2-agent-step-000000001', ?, 1, 'tool', 'legacy',
          'prom.query_range', JSON_OBJECT(), 'legacy result', 'legacy-ref', 'completed')`, agentRunID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO incident_signals (
incident_id, source, source_event_id, fingerprint, status, severity, cluster,
namespace, service_name, environment, target_kind, target_name, category,
occurred_at, received_at, summary, labels_json, annotations_json
) VALUES (?, 'alertmanager', 'legacy:phase2:event', 'legacy-fingerprint', 'firing',
          'warning', 'cluster-a', 'default', 'demo', 'development', 'Deployment',
          'demo', 'readiness', NOW(6), NOW(6), 'legacy signal', JSON_OBJECT(), JSON_OBJECT())`, incidentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO incident_events (
incident_id, event_type, idempotency_key, actor_type, actor_id, summary,
metadata_json, occurred_at
) VALUES (?, 'legacy.phase2', ?, 'system', 'legacy', 'legacy event', JSON_OBJECT(), NOW(6))`, incidentID, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items (
public_id, incident_id, agent_run_id, type, source, resource_ref, query_text,
summary, facts_json, raw_ref, collected_at
) VALUES ('legacy-phase2-evidence-00000000001', ?, ?, 'metric', 'prometheus',
          'deployment/demo', 'up', 'legacy evidence', JSON_OBJECT(), '', NOW(6))`, incidentID, agentRunID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO changes (
public_id, incident_id, source_type, status, category, correlation_reasons_json,
metadata_json, idempotency_key
) VALUES ('legacy-phase2-change-0000000000001', ?, 'github_commit', 'candidate',
          'low_confidence', JSON_ARRAY(), JSON_OBJECT(), ?)`, incidentID, hash); err != nil {
		t.Fatal(err)
	}

	planID := insertPhase2LegacyPlan(t, ctx, db, uint64(incidentID), 90, 1)
	result, err = db.ExecContext(ctx, `INSERT INTO remediation_approvals (
public_id, plan_id, decision, actor, approved_plan_hash, approved_patch_hash
) VALUES ('legacy-phase2-approval-00000000001', ?, 'approved', 'legacy-operator', ?, ?)`, planID, hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.LastInsertId(); err != nil {
		t.Fatal(err)
	}
	result, err = db.ExecContext(ctx, `INSERT INTO change_requests (
public_id, plan_id, repository, base_revision, head_branch, status, ci_status,
idempotency_key, lease_owner, lease_expires_at, heartbeat_at, attempts, row_version
) VALUES ('legacy-phase2-change-request-00001', ?, 'acme/demo', ?,
          'cloudops/legacy-phase2', 'pending', 'pending', ?, 'legacy-worker',
          TIMESTAMPADD(SECOND, 30, NOW(6)), NOW(6), 2, 4)`, planID, hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	changeRequestID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = db.ExecContext(ctx, `INSERT INTO verification_runs (
public_id, incident_id, remediation_plan_id, change_request_id, status,
target_revision, plan_json, deadline_at, attempt, lease_owner,
lease_expires_at, heartbeat_at, row_version
) VALUES ('legacy-phase2-verification-0000001', ?, ?, ?, 'pending', ?,
          JSON_OBJECT(), TIMESTAMPADD(MINUTE, 5, NOW(6)), 1, 'legacy-worker',
          TIMESTAMPADD(SECOND, 30, NOW(6)), NOW(6), 2)`, incidentID, planID, changeRequestID, hash)
	if err != nil {
		t.Fatal(err)
	}
	verificationID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO verification_checks (
public_id, verification_run_id, check_type, status, subject_json, expected_json,
stability_window_ms, timeout_ms, poll_interval_ms
) VALUES ('legacy-phase2-check-0000000000001', ?, 'argocd_revision', 'pending',
          JSON_OBJECT(), JSON_OBJECT(), 1000, 2000, 500)`, verificationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO incident_correlation_locks (correlation_key, touched_at)
VALUES (?, NOW(6))`, "v1:"+hash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO outbox_events (
event_id, aggregate_type, aggregate_id, event_type, schema_version,
payload_json, occurred_at, attempts, last_error
) VALUES ('legacy-phase2-outbox-0000000000001', 'incident', 'legacy',
          'legacy.phase2', 1, JSON_OBJECT(), NOW(6), 3, 'legacy error')`); err != nil {
		t.Fatal(err)
	}
}

func phase2ColumnsBeforeExpand(t *testing.T, ctx context.Context, db *sql.DB, tables []string) map[string][]string {
	t.Helper()
	result := make(map[string][]string, len(tables))
	for _, table := range tables {
		rows, err := db.QueryContext(ctx, `SELECT column_name
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = ?
ORDER BY ordinal_position`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			result[table] = append(result[table], column)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		_ = rows.Close()
		if len(result[table]) == 0 {
			t.Fatalf("Phase 1 table %s has no columns", table)
		}
	}
	return result
}

func phase2LegacyDomainSnapshot(t *testing.T, ctx context.Context, db *sql.DB, tables []string, columns map[string][]string) string {
	t.Helper()
	parts := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted := make([]string, len(columns[table]))
		for index, column := range columns[table] {
			quoted[index] = "`" + column + "`"
		}
		parts = append(parts, "table:"+table+"\n"+querySnapshot(t, ctx, db,
			"SELECT "+strings.Join(quoted, ",")+" FROM `"+table+"`"))
	}
	return hashString(strings.Join(parts, "\n--\n"))
}

func insertPhase2IncidentE(ctx context.Context, db *sql.DB, sequence int, correlationKey, status string) (uint64, error) {
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version,
cluster, namespace, service_name, environment, target_kind, target_name,
severity, status, summary, first_seen_at, last_seen_at, resolved_at, version,
domain_schema_version, v3_status, cycle_no, terminal_at
) VALUES (?, ?, ?, 2, 'cluster-a', 'default', 'demo', 'development', 'Deployment', 'demo',
          'warning', 'DETECTED', 'phase2 generated-key fixture', NOW(6), NOW(6),
          IF(? = 'resolved', NOW(6), NULL), 1, 3, ?, 1,
          IF(? IN ('resolved','closed'), NOW(6), NULL))`,
		phase2PublicID("incident", sequence), fmt.Sprintf("phase2-fingerprint-%d", sequence), correlationKey,
		status, status, status)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func insertPhase2AgentRun(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, sequence int) {
	t.Helper()
	if err := insertPhase2AgentRunE(ctx, db, incidentID, sequence); err != nil {
		t.Fatal(err)
	}
}

func insertPhase2AgentRunE(ctx context.Context, db *sql.DB, incidentID uint64, sequence int) error {
	_, err := db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, status, model, prompt_version, max_steps, failure_code,
domain_schema_version, v3_status, cycle_no, expected_incident_version
) VALUES (?, ?, 'LEGACY_COMPAT', 'fixture-model', 'fixture-prompt', 1, '', 3, 'pending', 1, 1)`,
		phase2PublicID("agent", sequence), incidentID)
	return err
}

func insertPhase2Plan(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, sequence, planVersion int, status string) uint64 {
	t.Helper()
	id, err := insertPhase2PlanRow(ctx, db, incidentID, sequence, planVersion, status, true)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertPhase2PlanE(ctx context.Context, db *sql.DB, incidentID uint64, sequence, planVersion int, status string) error {
	_, err := insertPhase2PlanRow(ctx, db, incidentID, sequence, planVersion, status, true)
	return err
}

func insertPhase2LegacyPlan(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, sequence, planVersion int) uint64 {
	t.Helper()
	id, err := insertPhase2PlanRow(ctx, db, incidentID, sequence, planVersion, "", false)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertPhase2PlanRow(ctx context.Context, db *sql.DB, incidentID uint64, sequence, planVersion int, v3Status string, v3 bool) (uint64, error) {
	hash := fmt.Sprintf("%064x", sequence+1000)
	query := `INSERT INTO remediation_plans (
public_id, incident_id, plan_version, plan_hash, status, operation_type,
target_repository, target_base_revision, target_path, parameters_json,
evidence_references_json, risk_level, policy_snapshot_hash, expected_before_hash,
proposed_patch_hash, patch_summary, rollback_plan, validation_plan`
	args := []any{
		phase2PublicID("plan", sequence), incidentID, planVersion, hash, "draft", "rollback_image",
		"acme/demo", hash, "deploy/demo.yaml", []byte(`{}`), []byte(`[]`), "low", hash, hash, hash,
		"fixture", "fixture", "fixture",
	}
	if v3 {
		query += `, domain_schema_version, cycle_no, v3_status, hash_schema_version,
canonical_plan_hash, verification_plan_json`
		args = append(args, 3, 1, v3Status, 1, hash, []byte(`{}`))
	}
	query += `) VALUES (` + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + `)`
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func insertPhase2ChangeRequest(t *testing.T, ctx context.Context, db *sql.DB, incidentID, planID uint64, sequence int) {
	t.Helper()
	if err := insertPhase2ChangeRequestE(ctx, db, incidentID, planID, sequence); err != nil {
		t.Fatal(err)
	}
}

func insertPhase2ChangeRequestE(ctx context.Context, db *sql.DB, incidentID, planID uint64, sequence int) error {
	hash := fmt.Sprintf("%064x", sequence+2000)
	_, err := db.ExecContext(ctx, `INSERT INTO change_requests (
public_id, plan_id, repository, base_revision, head_branch, status, ci_status,
idempotency_key, domain_schema_version, incident_id, cycle_no, v3_status,
write_phase, expected_subject_version, logical_operation_key
) VALUES (?, ?, 'acme/demo', ?, ?, 'pending', 'pending', ?, 3, ?, 1,
          'pending', 'ensure_branch', 1, ?)`,
		phase2PublicID("change", sequence), planID, hash, fmt.Sprintf("cloudops/phase2-%d", sequence),
		hash, incidentID, fmt.Sprintf("%064x", sequence+3000))
	return err
}

func insertPhase2Signal(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, sequence int) uint64 {
	t.Helper()
	hash := fmt.Sprintf("%064x", sequence+4000)
	result, err := db.ExecContext(ctx, `INSERT INTO incident_signals (
public_id, incident_id, source, source_event_id, fingerprint, status, severity, cluster,
namespace, service_name, environment, target_kind, target_name, category,
occurred_at, received_at, summary, labels_json, annotations_json,
domain_schema_version, cycle_no, canonical_schema_version,
correlation_key_version, alert_instance_key, starts_at, ends_at
) VALUES (?, ?, 'alertmanager', ?, ?, 'resolved', 'warning', 'cluster-a', 'default',
          'demo', 'development', 'Deployment', 'demo', 'readiness', NOW(6), NOW(6),
          'phase2 signal', JSON_OBJECT(), JSON_OBJECT(), 3, 1, 1, 2, ?,
          TIMESTAMPADD(SECOND, -1, NOW(6)), NOW(6))`,
		phase2PublicID("signal", sequence), incidentID, "v1:"+hash, fmt.Sprintf("fp-%d", sequence), hash)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id)
}

func insertPhase2Verification(t *testing.T, ctx context.Context, db *sql.DB, incidentID, signalID uint64, attempt int, status string, sequence int) {
	t.Helper()
	if err := insertPhase2VerificationE(ctx, db, incidentID, signalID, attempt, status, sequence); err != nil {
		t.Fatal(err)
	}
}

func insertPhase2VerificationE(ctx context.Context, db *sql.DB, incidentID, signalID uint64, attempt int, status string, sequence int) error {
	hash := fmt.Sprintf("%064x", sequence+5000)
	legacyStatus := status
	if status == "pending" || status == "running" {
		legacyStatus = "cancelled"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO verification_runs (
public_id, incident_id, remediation_plan_id, change_request_id, status,
target_revision, plan_json, deadline_at, completed_at, attempt,
domain_schema_version, cycle_no, v3_status, trigger_type, trigger_signal_id,
source_revision, image_digest, gitops_revision, verification_profile_version,
verification_profile_hash, expected_subject_version
) VALUES (?, ?, NULL, NULL, ?, ?, JSON_OBJECT(), TIMESTAMPADD(MINUTE, 5, NOW(6)),
          IF(? IN ('passed','failed','inconclusive','timed_out','cancelled'), NOW(6), NULL), ?,
          3, 1, ?, 'no_change_signal', ?, ?, CONCAT('sha256:', ?), ?, 1, ?, 1)`,
		phase2PublicID("verify", sequence), incidentID, legacyStatus, hash, status, attempt,
		status, signalID, hash, hash, hash, hash)
	return err
}

func insertPhase2ExplainTasks(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64) {
	t.Helper()
	ready, err := db.PrepareContext(ctx, `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
dedupe_key, status, priority, available_at, attempt, max_attempts, lease_generation
) VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'incident', ?,
          'investigation.start', 1, 1, JSON_OBJECT(), ?, 'ready', ?,
          TIMESTAMPADD(SECOND, -1, NOW(6)), 0, 3, 0)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ready.Close() }()
	running, err := db.PrepareContext(ctx, `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
dedupe_key, status, priority, available_at, attempt, max_attempts, lease_owner,
lease_generation, lease_expires_at, heartbeat_at
) VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'incident', ?,
          'investigation.start', 1, 1, JSON_OBJECT(), ?, 'running', ?,
          TIMESTAMPADD(SECOND, -2, NOW(6)), 1, 3, 'phase2-worker', 1,
          TIMESTAMPADD(SECOND, -1, NOW(6)), TIMESTAMPADD(SECOND, -2, NOW(6)))`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close() }()
	for index := 0; index < 256; index++ {
		if _, err := ready.ExecContext(ctx, phase2PublicID("ready", index), incidentID, incidentID,
			fmt.Sprintf("%064x", index+10000), index%7); err != nil {
			t.Fatalf("insert ready explain fixture %d: %v", index, err)
		}
		if _, err := running.ExecContext(ctx, phase2PublicID("running", index), incidentID, incidentID,
			fmt.Sprintf("%064x", index+20000), index%7); err != nil {
			t.Fatalf("insert running explain fixture %d: %v", index, err)
		}
	}
}

func phase2Explain(t *testing.T, ctx context.Context, db *sql.DB, query string) string {
	t.Helper()
	var plan string
	if err := db.QueryRowContext(ctx, query).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertPhase2ExplainIndex(t *testing.T, plan, index string) {
	t.Helper()
	if !strings.Contains(plan, `"key": "`+index+`"`) {
		t.Fatalf("EXPLAIN did not select %s: %s", index, plan)
	}
	if strings.Contains(plan, `"access_type": "ALL"`) {
		t.Fatalf("EXPLAIN used a full table scan for %s: %s", index, plan)
	}
}

func assertPhase2GeneratedExpression(t *testing.T, ctx context.Context, db *sql.DB, table, column string, required, forbidden []string) {
	t.Helper()
	var expression string
	if err := db.QueryRowContext(ctx, `SELECT generation_expression
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&expression); err != nil {
		t.Fatal(err)
	}
	expression = strings.ToLower(expression)
	for _, token := range required {
		if !strings.Contains(expression, strings.ToLower(token)) {
			t.Fatalf("%s.%s generated expression missing %q: %s", table, column, token, expression)
		}
	}
	for _, token := range forbidden {
		if strings.Contains(expression, strings.ToLower(token)) {
			t.Fatalf("%s.%s generated expression unexpectedly contains terminal state %q: %s", table, column, token, expression)
		}
	}
}

func expectPhase2DuplicateKey(t *testing.T, operation func() error, key string) {
	t.Helper()
	err := operation()
	if err == nil {
		t.Fatalf("expected duplicate generated-key error for %s", key)
	}
	var mysqlError *drivermysql.MySQLError
	if !errors.As(err, &mysqlError) || mysqlError.Number != 1062 || !strings.Contains(err.Error(), key) {
		t.Fatalf("expected MySQL 1062 for %s, got %v", key, err)
	}
}

func phase2CorrelationKey(sequence int) string {
	return "v2:" + fmt.Sprintf("%064x", sequence)
}

func phase2PublicID(kind string, sequence int) string {
	prefix := fmt.Sprintf("p2-%-8s", kind)
	prefix = strings.ReplaceAll(prefix, " ", "0")
	return fmt.Sprintf("%-16.16s%020d", prefix, sequence)
}
