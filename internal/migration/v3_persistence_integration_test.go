package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestV3RemediationVerificationExpansion(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openSQL(t, adminDSN)
	defer func() { _ = admin.Close() }()

	name := fmt.Sprintf("cloudops_v3_persistence_%d", time.Now().UnixNano())
	createDatabase(t, ctx, admin, name)
	defer dropDatabase(t, admin, name)
	db := openSQL(t, databaseDSN(t, adminDSN, name))
	defer func() { _ = db.Close() }()
	runner := newTestRunner(t, ctx, db, 5*time.Second)
	if _, err := runner.provider.UpTo(ctx, 8); err != nil {
		t.Fatalf("prepare schema 8: %v", err)
	}

	incidentID := insertPhase2Incident(t, ctx, db, 900, phase2CorrelationKey(900), "investigating")
	if err := insertPhase2AgentRunE(ctx, db, incidentID, 900); err != nil {
		t.Fatal(err)
	}
	var agentRunID uint64
	if err := db.QueryRowContext(ctx, "SELECT id FROM agent_runs WHERE public_id = ?", phase2PublicID("agent", 900)).Scan(&agentRunID); err != nil {
		t.Fatal(err)
	}
	planID := insertPhase2Plan(t, ctx, db, incidentID, 900, 1, "awaiting_approval")
	signalID := insertPhase2Signal(t, ctx, db, incidentID, 900)
	insertPhase2Verification(t, ctx, db, incidentID, signalID, 1, "passed", 900)

	if _, err := runner.Up(ctx); err != nil {
		t.Fatalf("upgrade partial V3 rows through 00009: %v", err)
	}
	assertVersion(t, ctx, runner, LatestVersion)
	assertV3PersistenceSchema(t, ctx, db)

	var planContract, verificationContract sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT plan_content_schema_version FROM remediation_plans WHERE id = ?", planID).Scan(&planContract); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT verification_contract_version FROM verification_runs WHERE public_id = ?", phase2PublicID("verify", 900)).Scan(&verificationContract); err != nil {
		t.Fatal(err)
	}
	if planContract.Valid || verificationContract.Valid {
		t.Fatalf("00008 partial row unexpectedly opted into 00009 contract: plan=%v verification=%v", planContract, verificationContract)
	}
	if _, err := db.ExecContext(ctx, "UPDATE remediation_plans SET plan_content_schema_version = 1 WHERE id = ?", planID); err == nil {
		t.Fatal("partial immutable Plan contract unexpectedly accepted")
	}

	hash := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `UPDATE remediation_plans
SET plan_content_schema_version = 1,
    incident_version = 1,
    created_by_agent_run_id = ?,
    diagnosis_hash = ?,
    operation_type = 'restore_required_env',
    target_base_branch = 'main',
    last_known_good_sha = ?,
    base_blob_sha = ?,
    file_mode = '100644',
    target_resource_json = JSON_OBJECT('api_version','apps/v1','kind','Deployment','namespace','default','name','demo','container','demo'),
    target_field_ref = 'spec.template.spec.containers[name=demo].env[name=REQUIRED_ENV]',
    expected_post_image_hash = ?,
    expected_tree_hash = ?,
    canonical_change_manifest_json = JSON_OBJECT('path',target_path,'base_blob_sha',?,'file_mode','100644','post_image_hash',?),
    bounded_diff = '--- a/deploy/demo.yaml\n+++ b/deploy/demo.yaml\n',
    policy_version = 'restore-required-env/v1',
    policy_snapshot_json = JSON_OBJECT('version','restore-required-env/v1'),
    verification_plan_hash = ?,
    evidence_bindings_json = JSON_ARRAY(JSON_OBJECT('id','00000000-0000-4000-8000-000000000001','content_hash',?)),
    evidence_set_hash = ?,
    expires_at = TIMESTAMPADD(HOUR, 1, created_at)
WHERE id = ?`, agentRunID, hash, hash, hash, hash, hash, hash, hash, hash, hash, hash, planID); err != nil {
		t.Fatalf("complete immutable Plan opt-in: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO remediation_decisions (
public_id, domain_schema_version, decision_schema_version, incident_id, cycle_no,
plan_id, plan_version, decision, actor_provider, actor_login, actor_role, reason,
request_id, request_authenticated_at, expires_at, approved_hash_schema_version,
approved_plan_hash, approved_base_sha, approved_post_image_hash, approved_tree_hash,
approved_patch_hash, approved_policy_hash, approved_verification_hash,
approved_evidence_set_hash
) VALUES ('00000000-0000-4000-8000-000000000009', 3, 1, ?, 1, ?, 1,
          'approved', 'github', 'phase5-operator', 'operator', 'approve exact immutable plan',
          'phase5-request-0009', NOW(6), TIMESTAMPADD(MINUTE, 10, NOW(6)), 1,
          ?, ?, ?, ?, ?, ?, ?, ?)`,
		incidentID, planID, hash, hash, hash, hash, hash, hash, hash, hash); err != nil {
		t.Fatalf("insert hash-bound Decision: %v", err)
	}

	var verificationRunID uint64
	if err := db.QueryRowContext(ctx, "SELECT id FROM verification_runs WHERE public_id = ?", phase2PublicID("verify", 900)).Scan(&verificationRunID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE verification_runs
SET verification_contract_version = 1,
    verification_profile_id = 'no-change/v1',
    common_stability_window_ms = 60000
WHERE id = ?`, verificationRunID); err != nil {
		t.Fatalf("complete VerificationRun opt-in: %v", err)
	}
	checkResult, err := db.ExecContext(ctx, `INSERT INTO verification_checks (
public_id, verification_run_id, check_type, status, required_check, subject_json,
expected_json, observed_json, source_reference, lookback_ms, stability_window_ms,
timeout_ms, poll_interval_ms, domain_schema_version, incident_id, cycle_no,
check_spec_schema_version, profile_id, template_id, template_version, comparison,
threshold, source_identity, initial_delay_ms, min_samples, sample_unit, failure_mode
) VALUES ('00000000-0000-4000-8000-000000000010', ?, 'workload_ready', 'running', TRUE,
          JSON_OBJECT('workload','demo'), JSON_OBJECT('ready',TRUE), NULL, '', 0,
          60000, 240000, 5000, 3, ?, 1, 1, 'no-change/v1', 'workload_ready/v1',
          'v1', NULL, NULL, 'kubernetes_read', 0, 2, 'pods', 'resets')`, verificationRunID, incidentID)
	if err != nil {
		t.Fatalf("insert frozen CheckSpec: %v", err)
	}
	checkID, err := checkResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO verification_samples (
public_id, domain_schema_version, sample_schema_version, incident_id, cycle_no,
verification_run_id, verification_check_id, sample_sequence, status, observed_json,
source_reference, reason_code, sampled_at, content_hash
) VALUES ('00000000-0000-4000-8000-000000000011', 3, 1, ?, 1, ?, ?, 1,
          'passed', JSON_OBJECT('status','available','sample_count',2,'ready',TRUE),
          'kubernetes://default/deployment/demo', 'threshold_satisfied', NOW(6), ?)`,
		incidentID, verificationRunID, checkID, hash); err != nil {
		t.Fatalf("insert bounded VerificationSample: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resolution_reports (
public_id, domain_schema_version, report_schema_version, incident_id, cycle_no,
verification_run_id, initial_signal_id, trigger_signal_id, trigger_type,
resolution_reason, service, workload, environment, impact_summary, cycle_started_at,
resolved_at, measured_duration_ms, source_revision, image_digest, gitops_revision,
verification_profile_id, verification_profile_hash, common_window_started_at,
common_window_completed_at, trigger_signal_json, diagnosis_json, evidence_json,
remediation_plan_json, remediation_decision_json, delivery_json, verification_json,
timeline_json, agent_usage_json, summary, content_hash, generated_at
) VALUES ('00000000-0000-4000-8000-000000000012', 3, 1, ?, 1, ?, ?, ?,
          'no_change_signal', 'recovered_before_diagnosis', 'demo', 'demo', 'development',
          'Recovered before a remediation write', TIMESTAMPADD(SECOND,-120,NOW(6)),
          NOW(6), 120000, ?, CONCAT('sha256:',?), ?, 'no-change/v1', ?,
          TIMESTAMPADD(SECOND,-61,NOW(6)), NOW(6), JSON_OBJECT('signal_id',?), NULL,
          JSON_ARRAY(), NULL, NULL, NULL, JSON_OBJECT('run_id',?), JSON_ARRAY(),
          JSON_OBJECT('agent_runs',1), 'No-change recovery verified', ?, NOW(6))`,
		incidentID, verificationRunID, signalID, signalID, hash, hash, hash, hash,
		signalID, verificationRunID, hash); err != nil {
		t.Fatalf("insert no-change ResolutionReport: %v", err)
	}

	var decisionCount, sampleCount, reportCount int
	for table, destination := range map[string]*int{
		"remediation_decisions": &decisionCount,
		"verification_samples":  &sampleCount,
		"resolution_reports":    &reportCount,
	} {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if decisionCount != 1 || sampleCount != 1 || reportCount != 1 {
		t.Fatalf("persisted Decision/Sample/Report counts=%d/%d/%d", decisionCount, sampleCount, reportCount)
	}
}

func assertV3PersistenceSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	required := map[string][]string{
		"remediation_plans":     {"plan_content_schema_version", "created_by_agent_run_id", "diagnosis_hash", "expected_post_image_hash", "expected_tree_hash", "bounded_diff", "verification_plan_hash", "evidence_set_hash", "expires_at"},
		"remediation_decisions": {"approved_plan_hash", "approved_base_sha", "approved_post_image_hash", "approved_tree_hash", "approved_patch_hash", "approved_policy_hash", "approved_verification_hash", "approved_evidence_set_hash"},
		"verification_runs":     {"verification_contract_version", "verification_profile_id", "common_stability_window_ms", "common_success_since"},
		"verification_checks":   {"check_spec_schema_version", "template_id", "template_version", "initial_delay_ms", "min_samples", "sample_unit", "failure_mode", "source_identity"},
		"verification_samples":  {"verification_check_id", "sample_sequence", "observed_json", "content_hash"},
		"resolution_reports":    {"resolution_reason", "trigger_signal_json", "verification_json", "agent_usage_json", "content_hash"},
	}
	for table, columns := range required {
		for _, column := range columns {
			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("missing V3 persistence column %s.%s", table, column)
			}
		}
	}
}
