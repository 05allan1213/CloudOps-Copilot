package taskhandler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	incidentmysql "github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestMySQLVerificationAdvanceTimeoutRequeuesInvestigation(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openVerificationIntegrationDB(t, adminDSN)
	defer admin.Close()
	databaseName := fmt.Sprintf("cloudops_verification_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if _, err := admin.ExecContext(dropCtx, "DROP DATABASE IF EXISTS `"+databaseName+"`"); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	}()
	db := openVerificationIntegrationDB(t, verificationDatabaseDSN(t, adminDSN, databaseName))
	defer db.Close()
	runner, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	incidentID, signalID, runID := insertVerificationIntegrationFixture(t, ctx, db, now)
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	runPublicID := uuid.NewString()
	if _, err := db.ExecContext(ctx, "UPDATE verification_runs SET public_id = ? WHERE id = ?", runPublicID, runID); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(verificationAdvancePayload{VerificationRunID: runPublicID, CycleNo: 1})
	if _, err := tasks.Enqueue(ctx, asyncjob.NewTask{
		IncidentID: incidentID, CycleNo: 1, Type: asyncjob.TaskVerificationAdvance,
		SubjectType: "verification_run", SubjectID: runID, Transition: "verification.advance",
		ExpectedSubjectVersion: 1, PayloadSchemaVersion: 1, Payload: payload,
		DedupeKey: hashVerificationTask("integration", runID), MaxAttempts: 3,
	}); err != nil {
		t.Fatal(err)
	}
	execution, err := tasks.Claim(ctx, asyncjob.ClaimRequest{Queue: asyncjob.QueueVerify, Owner: "verification-integration", LeaseDuration: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := NewMySQLVerificationAdvance(MySQLVerificationAdvanceConfig{
		DB: db, Tasks: tasks, Observations: verificationIntegrationObservation{},
		Reports: verificationIntegrationReport{},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := operation(ctx, *execution)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if err := tasks.Resolve(ctx, execution.Lease, result); err != nil {
		t.Fatal(err)
	}

	assertVerificationIntegrationValue(t, ctx, db, "SELECT v3_status FROM verification_runs WHERE id = ?", "timed_out", runID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM verification_checks WHERE verification_run_id = ? AND status = 'timed_out'", 1, runID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM verification_runs WHERE id = ? AND completed_at IS NOT NULL AND common_window_completed_at IS NULL", 1, runID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM verification_samples WHERE verification_run_id = ? AND status = 'timed_out'", 1, runID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM evidence_items WHERE incident_id = ? AND cycle_no = 1 AND producer_type = 'verification'", 1, incidentID)
	assertVerificationIntegrationValue(t, ctx, db, "SELECT v3_status FROM incidents WHERE id = ?", "investigating", incidentID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND cycle_no = 1 AND transition = 'investigation.start' AND status = 'ready'", 1, incidentID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE id = ? AND incident_id = ?", 1, signalID, incidentID)
}

func TestMySQLVerificationAdvancePassesWithBoundedNoChangeReport(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openVerificationIntegrationDB(t, adminDSN)
	defer admin.Close()
	databaseName := fmt.Sprintf("cloudops_verification_report_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if _, err := admin.ExecContext(dropCtx, "DROP DATABASE IF EXISTS `"+databaseName+"`"); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	}()
	db := openVerificationIntegrationDB(t, verificationDatabaseDSN(t, adminDSN, databaseName))
	defer db.Close()
	runner, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	incidentID, signalID, runID := insertVerificationIntegrationFixture(t, ctx, db, now)
	runPublicID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `UPDATE verification_runs SET public_id = ?, started_at = ?, deadline_at = ? WHERE id = ?`, runPublicID, now.Add(-2*time.Minute), now.Add(3*time.Minute), runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE verification_checks SET status = 'running', first_checked_at = ?, last_checked_at = ?, consecutive_success_since = ?, attempt_count = 1 WHERE verification_run_id = ?`, now.Add(-70*time.Second), now, now.Add(-70*time.Second), runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE verification_checks SET last_checked_at = ? WHERE verification_run_id = ? AND check_type = 'metric_error_rate_below'`, now.Add(-11*time.Second), runID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 50; index++ {
		metadata, _ := json.Marshal(map[string]any{"sequence": index})
		if _, err := db.ExecContext(ctx, `INSERT INTO incident_events
 (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
  event_type, idempotency_key, actor_type, actor_id, summary, metadata_json,
  occurred_at, created_at)
VALUES (?, ?, 3, 1, 1, 'verification_fixture', ?, 'system', 'integration-test', ?, ?, ?, ?)`,
			uuid.NewString(), incidentID, hashVerificationTask("timeline", index),
			fmt.Sprintf("fixture event %d", index), metadata, now.Add(time.Duration(index)*time.Microsecond), now); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{
		TriggerType: "no_change", TargetRevision: strings.Repeat("b", 40), SourceRevision: strings.Repeat("c", 40),
		ImageDigest: "sha256:" + strings.Repeat("d", 64), GitOpsRevision: strings.Repeat("b", 40),
		ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo", Cluster: "kind-local", Environment: "local",
		Namespace: "cloudops-demo", Service: "cloudops-demo", WorkloadName: "cloudops-demo", AlertNames: []string{"RequiredEnvMissing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := asyncjob.Task{IncidentID: incidentID, CycleNo: 1, SubjectID: runID, ExpectedSubjectVersion: 1}
	checks, err := loadVerificationChecks(ctx, db, task)
	if err != nil {
		t.Fatal(err)
	}
	selected := -1
	for index := range checks {
		if checks[index].Type == verification.CheckMetricErrorRateBelow {
			selected = index
			break
		}
	}
	if selected < 0 {
		t.Fatal("metric_error_rate_below check is missing")
	}
	observation := verification.Observation{
		Status: verification.ObservationAvailable, Value: .001, Denominator: 50, SampleCount: 50,
		SampledAt: now, QueryValid: true, SourceHealthy: true, RetentionCovered: true,
		SourceReference: "integration://prometheus/error-rate",
	}
	sample := verification.EvaluateV3Observation(checks[selected], observation, now)
	if sample.Status != verification.SamplePassed {
		t.Fatalf("sample=%+v", sample)
	}
	check := checks[selected]
	if err := verification.ApplyV3Sample(&check, sample, now); err != nil {
		t.Fatal(err)
	}
	checks[selected] = check
	startedAt := now.Add(-2 * time.Minute)
	commonStart := now.Add(-70 * time.Second)
	snapshot := VerificationAdvanceSnapshot{
		Run: verification.Run{
			ID: runID, PublicID: runPublicID, IncidentID: incidentID, Status: verification.RunRunning,
			TargetRevision: plan.TargetRevision, Plan: plan, StartedAt: &startedAt, DeadlineAt: now.Add(3 * time.Minute), RowVersion: 1,
		},
		Checks: checks, CheckID: check.PublicID, Now: now, CycleNo: 1, IncidentVersion: 1, IncidentStatus: "verifying",
		TriggerType: "no_change_signal", TriggerSignalID: signalID,
		SourceRevision: plan.SourceRevision, ImageDigest: plan.ImageDigest, GitOpsRevision: plan.GitOpsRevision,
		ProfileID: plan.ProfileID, ProfileHash: plan.ProfileHash, ContractVersion: verificationContractVersion,
		CommonStabilityWindow: verification.V3CommonStabilityWindow,
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &mysqlVerificationAdvanceStore{tasks: tasks, reports: NewMySQLResolutionReportWriter(), now: func() time.Time { return now }, maxAgentRuns: DefaultAgentRunBudget}
	if err := store.PersistIn(ctx, tx, task, snapshot, check, sample, verification.RunPassed, "all_required_checks_common_window_passed", &commonStart); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	reportWriter := NewMySQLResolutionReportWriter()
	replayTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reportWriter.PersistIn(ctx, replayTx, task, snapshot, nil, &commonStart, now); err != nil {
		_ = replayTx.Rollback()
		t.Fatalf("idempotent report replay: %v", err)
	}
	if err := replayTx.Commit(); err != nil {
		t.Fatal(err)
	}
	conflictTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflictErr := reportWriter.PersistIn(ctx, conflictTx, task, snapshot, nil, &commonStart, now.Add(time.Microsecond))
	_ = conflictTx.Rollback()
	if !errors.Is(conflictErr, asyncjob.ErrInvalidMutation) {
		t.Fatalf("conflicting report replay err=%v", conflictErr)
	}

	var runStatus, resultSummary, failureReason string
	if err := db.QueryRowContext(ctx, "SELECT v3_status, result_summary, failure_reason FROM verification_runs WHERE id = ?", runID).Scan(&runStatus, &resultSummary, &failureReason); err != nil {
		t.Fatal(err)
	}
	if runStatus != "passed" {
		rows, err := db.QueryContext(ctx, "SELECT check_type, status, failure_reason, last_checked_at, consecutive_success_since FROM verification_checks WHERE verification_run_id = ? ORDER BY id", runID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		states := make([]string, 0, 8)
		for rows.Next() {
			var checkType, status, reason string
			var lastChecked, successSince sql.NullTime
			if err := rows.Scan(&checkType, &status, &reason, &lastChecked, &successSince); err != nil {
				t.Fatal(err)
			}
			states = append(states, fmt.Sprintf("%s:%s:%s:last=%v:success=%v", checkType, status, reason, lastChecked, successSince))
		}
		t.Fatalf("run=%s summary=%s failure=%s checks=%v", runStatus, resultSummary, failureReason, states)
	}
	assertVerificationIntegrationValue(t, ctx, db, "SELECT v3_status FROM incidents WHERE id = ?", "resolved", incidentID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM verification_checks WHERE verification_run_id = ? AND status = 'passed'", 8, runID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM resolution_reports WHERE incident_id = ? AND verification_run_id = ? AND trigger_signal_id = ?", 1, incidentID, runID, signalID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM resolution_reports WHERE incident_id = ? AND diagnosis_json IS NULL AND remediation_plan_json IS NULL AND delivery_json IS NULL AND JSON_EXTRACT(evidence_json, '$.evidence_count') = 0 AND JSON_EXTRACT(timeline_json, '$.event_count') = 51 AND JSON_SEARCH(timeline_json, 'one', 'incident_resolved') IS NOT NULL AND CHAR_LENGTH(content_hash) = 64", 1, incidentID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM verification_runs WHERE id = ? AND completed_at IS NOT NULL AND common_success_since IS NOT NULL AND common_window_completed_at IS NOT NULL", 1, runID)
}

func TestMySQLVerificationAdvancePersistsBoundPostDeliveryReport(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openVerificationIntegrationDB(t, adminDSN)
	defer admin.Close()
	databaseName := fmt.Sprintf("cloudops_verification_delivery_report_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if _, err := admin.ExecContext(dropCtx, "DROP DATABASE IF EXISTS `"+databaseName+"`"); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	}()
	db := openVerificationIntegrationDB(t, verificationDatabaseDSN(t, adminDSN, databaseName))
	defer db.Close()
	runner, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	incidentID, _, runID := insertVerificationIntegrationFixture(t, ctx, db, now)
	var incidentPublicID, runPublicID string
	if err := db.QueryRowContext(ctx, "SELECT public_id FROM incidents WHERE id = ?", incidentID).Scan(&incidentPublicID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT public_id FROM verification_runs WHERE id = ?", runID).Scan(&runPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET status = 'DIAGNOSING', v3_status = 'investigating', version = 2 WHERE id = ?", incidentID); err != nil {
		t.Fatal(err)
	}
	diagnosisHash := strings.Repeat("f", 64)
	diagnosisJSON, _ := json.Marshal(map[string]any{
		"candidate":            map[string]any{"claim_type": "configuration", "summary": "required environment variable was removed", "confidence": "confirmed", "evidence_fact_ids": []string{"required-env"}, "remediation_hint": "restore_required_env"},
		"claim_policy_version": "claim-policy/v1", "claim_policy_hash": strings.Repeat("1", 64),
		"evidence_ids": []string{"required-env"}, "diagnosis_hash": diagnosisHash,
	})
	agentRunPublicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, final_diagnosis,
  failure_code, completed_at, domain_schema_version, v3_status, cycle_no,
  expected_incident_version)
VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, ?, '', ?, 3,
        'completed', 1, 2)`, agentRunPublicID, incidentID, diagnosisJSON, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	agentRunID64, _ := result.LastInsertId()
	agentRunID := uint64(agentRunID64)
	baselineFacts := json.RawMessage(`{"required_env":"missing","source":"exact_git_blob"}`)
	baselineContentHash := sha256Hex(baselineFacts)
	baselineEvidencePublicID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items
 (public_id, incident_id, agent_run_id, type, source, producer_type,
  producer_dedupe_key, resource_ref, query_text, summary, facts_json, result_hash,
  content_hash, raw_ref, truncated, valid, collected_at, domain_schema_version, cycle_no)
VALUES (?, ?, ?, 'configuration', 'github', 'agent_step', ?,
        'github://acme/gitops/apps/demo.yaml', 'exact blob', 'verified missing required env',
        ?, ?, ?, '', FALSE, TRUE, ?, 3, 1)`, baselineEvidencePublicID, incidentID, agentRunID,
		hashVerificationTask("baseline-evidence", baselineEvidencePublicID), baselineFacts,
		baselineContentHash, baselineContentHash, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	targetRevision := strings.Repeat("b", 40)
	sourceRevision := strings.Repeat("c", 40)
	imageDigest := "sha256:" + strings.Repeat("d", 64)
	verificationPlan, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{
		TriggerType: "post_delivery", Repository: "acme/gitops", PullRequest: 7,
		TargetRevision: targetRevision, SourceRevision: sourceRevision, ImageDigest: imageDigest, GitOpsRevision: targetRevision,
		ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo", Cluster: "kind-local", Environment: "local",
		Namespace: "cloudops-demo", Service: "cloudops-demo", WorkloadName: "cloudops-demo", AlertNames: []string{"RequiredEnvMissing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	verificationPlanJSON, _ := json.Marshal(verificationPlan)
	planRequest := postDeliveryRemediationCompileRequest(incidentID, incidentPublicID, agentRunPublicID, diagnosisHash, baselineEvidencePublicID, baselineContentHash, verificationPlanJSON, now)
	remediationPlan, err := remediation.CompileRestoreRequiredEnv(planRequest)
	if err != nil {
		t.Fatal(err)
	}
	remediationRepository, err := incidentmysql.NewV3RemediationRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := remediationRepository.CreatePlan(ctx, &remediationPlan); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET status = 'AWAITING_APPROVAL', v3_status = 'awaiting_approval', version = 3 WHERE id = ?", incidentID); err != nil {
		t.Fatal(err)
	}
	decision, err := remediation.NewV3Approval(remediationPlan, "github", "operator-login", "operator", "reviewed exact immutable plan", "post-delivery-report-approval", now, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := remediationRepository.RecordDecision(ctx, remediationPlan.PublicID, remediationPlan.RowVersion, &decision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE remediation_plans SET status = 'approved', v3_status = 'consumed', row_version = row_version + 1 WHERE id = ?", remediationPlan.ID); err != nil {
		t.Fatal(err)
	}
	changePublicID := uuid.NewString()
	result, err = db.ExecContext(ctx, `INSERT INTO change_requests
 (public_id, plan_id, domain_schema_version, incident_id, cycle_no, repository,
  base_revision, head_branch, commit_sha, pr_number, pr_url, status, v3_status,
  write_phase, ci_status, pr_state, merged_commit_sha, target_revision,
  argocd_application, argocd_project, detected_revision, argocd_sync_status,
  argocd_operation_phase, argocd_health_status, resource_health_json,
  cluster, environment, namespace, workload_kind, workload_name,
  deployment_generation, observed_generation, rollout_revision, desired_replicas,
  updated_replicas, available_replicas, unavailable_replicas, delivery_started_at,
  delivery_deadline_at, delivery_completed_at, last_observed_at, idempotency_key,
  logical_operation_key, row_version, expected_subject_version)
VALUES (?, ?, 3, ?, 1, 'acme/gitops', ?, 'cloudops/restore-required-env', ?, 7,
        'https://github.example/acme/gitops/pull/7', 'delivered', 'delivered', 'observe',
        'passing', 'closed', ?, ?, 'cloudops-demo', 'cloudops-demo', ?, 'Synced',
        'Succeeded', 'Healthy', JSON_ARRAY(JSON_OBJECT('kind','Deployment','health','Healthy')),
        'kind-local', 'local', 'cloudops-demo', 'Deployment', 'cloudops-demo',
        7, 7, '7', 2, 2, 2, 0, ?, ?, ?, ?, ?, ?, 5, 5)`,
		changePublicID, remediationPlan.ID, incidentID, remediationPlan.TargetBaseRevision,
		targetRevision, targetRevision, targetRevision, targetRevision,
		now.Add(-time.Minute), now.Add(5*time.Minute), now.Add(-30*time.Second), now.Add(-30*time.Second),
		hashVerificationTask("change", changePublicID), hashVerificationTask("change-operation", changePublicID))
	if err != nil {
		t.Fatal(err)
	}
	changeID64, _ := result.LastInsertId()
	changeID := uint64(changeID64)
	for _, kind := range []DeliveryObservationKind{DeliveryObservePullRequest, DeliveryObserveCI, DeliveryObserveArgo} {
		insertVerificationDeliveryEvidence(t, ctx, db, incidentID, changePublicID, kind, now)
	}
	insertVerificationDeliveryEvidence(t, ctx, db, incidentID, uuid.NewString(), DeliveryObserveRollout, now)
	if _, err := reportDeliveryObservations(ctx, db, asyncjob.Task{IncidentID: incidentID, CycleNo: 1}, changePublicID); !errors.Is(err, asyncjob.ErrInvalidMutation) {
		t.Fatalf("old Change rollout evidence completed current delivery set: %v", err)
	}
	insertVerificationDeliveryEvidence(t, ctx, db, incidentID, changePublicID, DeliveryObserveRollout, now.Add(time.Microsecond))
	if observations, err := reportDeliveryObservations(ctx, db, asyncjob.Task{IncidentID: incidentID, CycleNo: 1}, changePublicID); err != nil || !json.Valid(observations) {
		t.Fatalf("bound delivery observations=%s err=%v", observations, err)
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM verification_checks WHERE verification_run_id = ?", runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE verification_runs
SET remediation_plan_id = ?, change_request_id = ?, status = 'running', v3_status = 'running',
    trigger_type = 'post_delivery', trigger_signal_id = NULL, target_revision = ?,
    source_revision = ?, image_digest = ?, gitops_revision = ?, plan_json = ?,
    verification_profile_version = 1, verification_profile_hash = ?,
    verification_contract_version = 1, verification_profile_id = ?,
    common_stability_window_ms = 60000, started_at = ?, deadline_at = ?,
    completed_at = NULL, row_version = 1, expected_subject_version = 1,
    result_summary = '', failure_reason = '', updated_at = ?
WHERE id = ?`, remediationPlan.ID, changeID, verificationPlan.TargetRevision,
		verificationPlan.SourceRevision, verificationPlan.ImageDigest, verificationPlan.GitOpsRevision,
		verificationPlanJSON, verificationPlan.ProfileHash, verificationPlan.ProfileID,
		now.Add(-2*time.Minute), now.Add(3*time.Minute), now, runID); err != nil {
		t.Fatal(err)
	}
	insertVerificationIntegrationChecks(t, ctx, db, incidentID, runID, verificationPlan, now)
	if _, err := db.ExecContext(ctx, `UPDATE verification_checks SET status = 'running', first_checked_at = ?, last_checked_at = ?, consecutive_success_since = ?, attempt_count = 1 WHERE verification_run_id = ?`, now.Add(-70*time.Second), now, now.Add(-70*time.Second), runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE verification_checks SET last_checked_at = ? WHERE verification_run_id = ? AND check_type = 'metric_error_rate_below'`, now.Add(-11*time.Second), runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET status = 'VERIFYING', v3_status = 'verifying', version = 4 WHERE id = ?", incidentID); err != nil {
		t.Fatal(err)
	}
	task := asyncjob.Task{IncidentID: incidentID, CycleNo: 1, SubjectID: runID, ExpectedSubjectVersion: 1}
	checks, err := loadVerificationChecks(ctx, db, task)
	if err != nil {
		t.Fatal(err)
	}
	selected := verificationCheckIndex(checks, verification.CheckMetricErrorRateBelow)
	if selected < 0 {
		t.Fatal("metric_error_rate_below check is missing")
	}
	observation := verification.Observation{Status: verification.ObservationAvailable, Value: .001, Denominator: 50, SampleCount: 50, SampledAt: now, QueryValid: true, SourceHealthy: true, RetentionCovered: true, SourceReference: "integration://prometheus/error-rate"}
	sample := verification.EvaluateV3Observation(checks[selected], observation, now)
	check := checks[selected]
	if sample.Status != verification.SamplePassed || verification.ApplyV3Sample(&check, sample, now) != nil {
		t.Fatalf("sample=%+v check=%+v", sample, check)
	}
	checks[selected] = check
	startedAt, commonStart := now.Add(-2*time.Minute), now.Add(-70*time.Second)
	snapshot := VerificationAdvanceSnapshot{
		Run:    verification.Run{ID: runID, PublicID: runPublicID, IncidentID: incidentID, RemediationPlanID: remediationPlan.ID, ChangeRequestID: changeID, Status: verification.RunRunning, TargetRevision: verificationPlan.TargetRevision, Plan: verificationPlan, StartedAt: &startedAt, DeadlineAt: now.Add(3 * time.Minute), RowVersion: 1},
		Checks: checks, CheckID: check.PublicID, Now: now, CycleNo: 1, IncidentVersion: 4, IncidentStatus: "verifying",
		TriggerType: "post_delivery", RemediationPlanID: remediationPlan.ID, ChangeRequestID: changeID,
		SourceRevision: verificationPlan.SourceRevision, ImageDigest: verificationPlan.ImageDigest, GitOpsRevision: verificationPlan.GitOpsRevision,
		ProfileID: verificationPlan.ProfileID, ProfileHash: verificationPlan.ProfileHash, ContractVersion: verificationContractVersion, CommonStabilityWindow: verification.V3CommonStabilityWindow,
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &mysqlVerificationAdvanceStore{tasks: tasks, reports: NewMySQLResolutionReportWriter(), now: func() time.Time { return now }, maxAgentRuns: DefaultAgentRunBudget}
	if err := store.PersistIn(ctx, tx, task, snapshot, check, sample, verification.RunPassed, "all_required_checks_common_window_passed", &commonStart); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertVerificationIntegrationValue(t, ctx, db, "SELECT v3_status FROM verification_runs WHERE id = ?", "passed", runID)
	assertVerificationIntegrationValue(t, ctx, db, "SELECT v3_status FROM incidents WHERE id = ?", "resolved", incidentID)
	assertVerificationIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM verification_checks WHERE verification_run_id = ? AND status = 'passed'", 10, runID)
	assertVerificationIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM resolution_reports
WHERE incident_id = ? AND verification_run_id = ? AND remediation_plan_id = ?
  AND remediation_decision_id = ? AND change_request_id = ?
  AND diagnosis_json IS NOT NULL AND remediation_plan_json IS NOT NULL
  AND remediation_decision_json IS NOT NULL AND delivery_json IS NOT NULL
  AND bad_gitops_revision = ? AND fix_gitops_revision = ?
  AND JSON_EXTRACT(delivery_json, '$.observations.observation_count') = 4
  AND JSON_EXTRACT(evidence_json, '$.evidence_count') = 6
  AND CHAR_LENGTH(content_hash) = 64`, 1, incidentID, runID, remediationPlan.ID,
		decision.ID, changeID, remediationPlan.TargetBaseRevision, targetRevision)
}

type verificationIntegrationObservation struct{}

func (verificationIntegrationObservation) Observe(context.Context, verification.Run, verification.Check) (verification.Observation, error) {
	return verification.Observation{}, fmt.Errorf("expired checks must not call an observation adapter")
}

type verificationIntegrationReport struct{}

func (verificationIntegrationReport) PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, VerificationAdvanceSnapshot, []verification.Check, *time.Time, time.Time) error {
	return fmt.Errorf("timed-out runs must not create a resolution report")
}

func insertVerificationIntegrationFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) (uint64, uint64, uint64) {
	t.Helper()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents
 (public_id, fingerprint, correlation_key, cluster, namespace, service_name, environment,
  target_kind, target_name, severity, status, summary, first_seen_at, last_seen_at,
  version, domain_schema_version, v3_status, cycle_no, needs_attention,
  correlation_key_version, created_at, updated_at)
VALUES (?, 'verification-fingerprint', 'sha256:verification-correlation', 'kind-local',
        'cloudops-demo', 'cloudops-demo', 'local', 'Deployment', 'cloudops-demo',
        'critical', 'VERIFYING', 'verification integration fixture', ?, ?, 1, 3,
        'verifying', 1, FALSE, 2, ?, ?)`, uuid.NewString(), now.Add(-10*time.Minute), now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	incidentID64, _ := result.LastInsertId()
	incidentID := uint64(incidentID64)
	hash := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO incident_signals
 (public_id, incident_id, domain_schema_version, cycle_no, canonical_schema_version,
  correlation_key_version, source, source_event_id, fingerprint, alert_instance_key,
  status, severity, cluster, namespace, service_name, environment, target_kind,
  target_name, category, occurred_at, starts_at, ends_at, received_at, summary,
  labels_json, annotations_json, raw_payload, created_at)
VALUES (?, ?, 3, 1, 2, 2, 'alertmanager', 'verification-firing',
        'verification-fingerprint', ?, 'firing', 'critical', 'kind-local',
        'cloudops-demo', 'cloudops-demo', 'local', 'Deployment', 'cloudops-demo',
        'availability', ?, ?, NULL, ?, 'firing signal', JSON_OBJECT(), JSON_OBJECT(),
        JSON_OBJECT(), ?)`, uuid.NewString(), incidentID, hash, now.Add(-10*time.Minute), now.Add(-10*time.Minute), now.Add(-10*time.Minute), now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	result, err = db.ExecContext(ctx, `INSERT INTO incident_signals
 (public_id, incident_id, domain_schema_version, cycle_no, canonical_schema_version,
  correlation_key_version, source, source_event_id, fingerprint, alert_instance_key,
  status, severity, cluster, namespace, service_name, environment, target_kind,
  target_name, category, occurred_at, starts_at, ends_at, received_at, summary,
  labels_json, annotations_json, raw_payload, created_at)
VALUES (?, ?, 3, 1, 2, 2, 'alertmanager', 'verification-resolved',
        'verification-fingerprint', ?, 'resolved', 'critical', 'kind-local',
        'cloudops-demo', 'cloudops-demo', 'local', 'Deployment', 'cloudops-demo',
        'availability', ?, ?, ?, ?, 'resolved signal', JSON_OBJECT(), JSON_OBJECT(),
        JSON_OBJECT(), ?)`, uuid.NewString(), incidentID, hash, now, now.Add(-10*time.Minute), now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	signalID64, _ := result.LastInsertId()
	signalID := uint64(signalID64)
	profile, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{
		TriggerType:    "no_change",
		TargetRevision: strings.Repeat("b", 40), SourceRevision: strings.Repeat("c", 40),
		ImageDigest: "sha256:" + strings.Repeat("d", 64), GitOpsRevision: strings.Repeat("b", 40),
		ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo",
		Cluster: "kind-local", Environment: "local", Namespace: "cloudops-demo",
		Service: "cloudops-demo", WorkloadName: "cloudops-demo",
		AlertNames: []string{"RequiredEnvMissing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	planJSON, _ := json.Marshal(profile)
	result, err = db.ExecContext(ctx, `INSERT INTO verification_runs
 (public_id, incident_id, domain_schema_version, cycle_no, remediation_plan_id,
  change_request_id, status, v3_status, trigger_type, trigger_signal_id,
  target_revision, source_revision, image_digest, gitops_revision, plan_json,
  verification_profile_version, verification_profile_hash,
  verification_contract_version, verification_profile_id, common_stability_window_ms,
  started_at, deadline_at, attempt, row_version, expected_subject_version,
  result_summary, failure_reason, created_at, updated_at)
VALUES (?, ?, 3, 1, NULL, NULL, 'running', 'running', 'no_change_signal', ?,
        ?, ?, ?, ?, ?, 1, ?, 1, 'no-change/v1', 60000, ?, ?, 1, 1, 1, '', '', ?, ?)`,
		uuid.NewString(), incidentID, signalID, profile.TargetRevision, profile.SourceRevision,
		profile.ImageDigest, profile.GitOpsRevision, planJSON, profile.ProfileHash,
		now.Add(-6*time.Minute), now.Add(-time.Minute), now.Add(-6*time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	runID64, _ := result.LastInsertId()
	runID := uint64(runID64)
	for _, spec := range profile.Checks {
		subjectJSON, _ := json.Marshal(spec.Subject)
		var comparison any
		var threshold any
		if spec.Comparison != "" {
			comparison, threshold = spec.Comparison, spec.Threshold
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO verification_checks
 (public_id, verification_run_id, domain_schema_version, incident_id, cycle_no,
  check_type, status, required_check, subject_json, expected_json, source_reference,
  lookback_ms, stability_window_ms, timeout_ms, poll_interval_ms,
  check_spec_schema_version, profile_id, template_id, template_version,
  comparison, threshold, source_identity, initial_delay_ms, min_samples,
  sample_unit, failure_mode, attempt_count, created_at, updated_at)
VALUES (?, ?, 3, ?, 1, ?, 'pending', TRUE, ?, ?, '', ?, ?, ?, ?, 1,
        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`, uuid.NewString(), runID, incidentID,
			spec.Type, subjectJSON, spec.Expected, spec.Lookback.Milliseconds(),
			spec.StabilityWindow.Milliseconds(), spec.Timeout.Milliseconds(), spec.PollInterval.Milliseconds(),
			spec.ProfileID, spec.TemplateID, spec.TemplateVersion, comparison, threshold,
			spec.SourceIdentity, spec.InitialDelay.Milliseconds(), spec.MinSamples,
			spec.SampleUnit, spec.FailureMode, now.Add(-6*time.Minute), now); err != nil {
			t.Fatal(err)
		}
	}
	return incidentID, signalID, runID
}

func postDeliveryRemediationCompileRequest(incidentID uint64, incidentPublicID, agentRunPublicID, diagnosisHash, evidencePublicID, evidenceContentHash string, verificationPlan json.RawMessage, now time.Time) remediation.RestoreEnvCompileRequest {
	current := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudops-demo
  namespace: cloudops-demo
spec:
  template:
    spec:
      containers:
        - name: cloudops-demo
          image: example/cloudops-demo@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
`)
	baseline := append(append([]byte(nil), current...), []byte("          env:\n            - name: REQUIRED_ENV\n              value: healthy\n")...)
	policy := remediation.RestoreEnvPolicy{
		Version: "restore-required-env-policy/v1", Repository: "acme/gitops", BaseBranch: "main",
		AllowedPath: "apps/cloudops-demo.yaml", APIVersion: "apps/v1", Namespace: "cloudops-demo",
		Workload: "cloudops-demo", Container: "cloudops-demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
		VerificationVersion: verification.GoldenRequiredEnvProfileID,
	}
	createdAt := now.Add(-30 * time.Second).Truncate(time.Microsecond)
	return remediation.RestoreEnvCompileRequest{
		IncidentPublicID: incidentPublicID, IncidentID: incidentID, CycleNo: 1, IncidentVersion: 2,
		CreatedByAgentRunID: agentRunPublicID, DiagnosisHash: diagnosisHash,
		Repository: policy.Repository, BaseBranch: policy.BaseBranch, BaseRevision: strings.Repeat("a", 40),
		LastKnownGoodRevision: strings.Repeat("9", 40), TargetPath: policy.AllowedPath,
		BaseBlobSHA: strings.Repeat("8", 40), ExpectedTreeHash: strings.Repeat("e", 40), FileMode: "100644",
		Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "cloudops-demo", Name: "cloudops-demo", Container: "cloudops-demo"},
		EnvKey: "REQUIRED_ENV", CurrentContent: current, BaselineContent: baseline, Policy: policy,
		VerificationPlan:   verificationPlan,
		Evidence:           []remediation.EvidenceBinding{{ID: evidencePublicID, ContentHash: evidenceContentHash}},
		BaselineIsAncestor: true, CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * time.Minute), PlanVersion: 1,
	}
}

func insertVerificationDeliveryEvidence(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, changeRequestPublicID string, kind DeliveryObservationKind, collectedAt time.Time) {
	t.Helper()
	facts, err := json.Marshal(map[string]any{
		"kind": kind, "observation": map[string]any{"status": "observed"},
		"source_revision": strings.Repeat("c", 40), "image_digest": "sha256:" + strings.Repeat("d", 64),
		"baseline_gitops_revision": strings.Repeat("a", 40), "target_gitops_revision": strings.Repeat("b", 40),
		"failure_code": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	contentHash := sha256Hex(facts)
	producerKey := hashCanonical("delivery.observe", changeRequestPublicID, string(kind), contentHash)
	if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items
 (public_id, incident_id, domain_schema_version, cycle_no, type, source,
  producer_type, producer_dedupe_key, resource_ref, query_text, summary,
  facts_json, result_hash, content_hash, raw_ref, truncated, valid, collected_at)
VALUES (?, ?, 3, 1, 'delivery_observation', 'integration', 'delivery.observe', ?,
        ?, '', ?, ?, ?, ?, '', FALSE, TRUE, ?)`, uuid.NewString(), incidentID,
		producerKey, "change-request:"+changeRequestPublicID, string(kind)+" delivery observation",
		facts, contentHash, contentHash, collectedAt.UTC()); err != nil {
		t.Fatal(err)
	}
}

func insertVerificationIntegrationChecks(t *testing.T, ctx context.Context, db *sql.DB, incidentID, runID uint64, plan verification.Plan, now time.Time) {
	t.Helper()
	for _, spec := range plan.Checks {
		subjectJSON, _ := json.Marshal(spec.Subject)
		var comparison any
		var threshold any
		if spec.Comparison != "" {
			comparison, threshold = spec.Comparison, spec.Threshold
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO verification_checks
 (public_id, verification_run_id, domain_schema_version, incident_id, cycle_no,
  check_type, status, required_check, subject_json, expected_json, source_reference,
  lookback_ms, stability_window_ms, timeout_ms, poll_interval_ms,
  check_spec_schema_version, profile_id, template_id, template_version,
  comparison, threshold, source_identity, initial_delay_ms, min_samples,
  sample_unit, failure_mode, attempt_count, created_at, updated_at)
VALUES (?, ?, 3, ?, 1, ?, 'pending', ?, ?, ?, '', ?, ?, ?, ?, 1,
        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`, uuid.NewString(), runID, incidentID,
			spec.Type, spec.Required, subjectJSON, spec.Expected, spec.Lookback.Milliseconds(),
			spec.StabilityWindow.Milliseconds(), spec.Timeout.Milliseconds(), spec.PollInterval.Milliseconds(),
			spec.ProfileID, spec.TemplateID, spec.TemplateVersion, comparison, threshold,
			spec.SourceIdentity, spec.InitialDelay.Milliseconds(), spec.MinSamples,
			spec.SampleUnit, spec.FailureMode, now, now); err != nil {
			t.Fatal(err)
		}
	}
}

func verificationCheckIndex(checks []verification.Check, checkType verification.CheckType) int {
	for index := range checks {
		if checks[index].Type == checkType {
			return index
		}
	}
	return -1
}

func openVerificationIntegrationDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func verificationDatabaseDSN(t *testing.T, adminDSN, databaseName string) string {
	t.Helper()
	cfg, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = databaseName
	cfg.ParseTime = true
	cfg.MultiStatements = true
	return cfg.FormatDSN()
}

func assertVerificationIntegrationValue(t *testing.T, ctx context.Context, db *sql.DB, query, expected string, args ...any) {
	t.Helper()
	var actual string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("value=%q want=%q query=%s", actual, expected, query)
	}
}

func assertVerificationIntegrationCount(t *testing.T, ctx context.Context, db *sql.DB, query string, expected int, args ...any) {
	t.Helper()
	var actual int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("count=%d want=%d query=%s", actual, expected, query)
	}
}
