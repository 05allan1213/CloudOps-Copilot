package apiv3

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

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestMySQLTypedWorkbenchProjections(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := openWorkbenchIntegrationDB(t, ctx, adminDSN)
	fixture := insertWorkbenchIntegrationFixture(t, ctx, db)
	port, err := NewMySQLQueryPort(db)
	if err != nil {
		t.Fatal(err)
	}

	plans, err := port.Query(ctx, QueryRequest{
		Kind: QueryRemediationPlans, IncidentID: fixture.incidentPublicID, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans.RemediationPlans) != 1 || plans.RemediationPlans[0].ID != fixture.planPublicID ||
		plans.RemediationPlans[0].Decision == nil ||
		plans.RemediationPlans[0].Decision.Actor.Role != "operator" ||
		plans.RemediationPlans[0].BoundedDiff == "" ||
		len(plans.RemediationPlans[0].EvidenceBindings) != 1 {
		t.Fatalf("remediation projection=%+v", plans.RemediationPlans)
	}

	delivery, err := port.Query(ctx, QueryRequest{
		Kind: QueryDelivery, IncidentID: fixture.incidentPublicID, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Delivery == nil || delivery.Delivery.ID != fixture.deliveryPublicID ||
		delivery.Delivery.RemediationPlanID != fixture.planPublicID ||
		delivery.Delivery.TargetRevision != fixture.targetRevision ||
		len(delivery.Delivery.ResourceHealth) == 0 {
		t.Fatalf("delivery projection=%+v", delivery.Delivery)
	}

	verifications, err := port.Query(ctx, QueryRequest{
		Kind: QueryVerifications, IncidentID: fixture.incidentPublicID, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(verifications.Verifications) != 1 || verifications.Verifications[0].ID != fixture.runPublicID ||
		verifications.Verifications[0].ChangeRequestID != fixture.deliveryPublicID ||
		len(verifications.Verifications[0].Checks) != 1 ||
		len(verifications.Verifications[0].Checks[0].Samples) != 1 ||
		verifications.Verifications[0].Checks[0].ID != fixture.checkPublicID ||
		verifications.Verifications[0].Checks[0].Samples[0].ID != fixture.samplePublicID {
		t.Fatalf("verification projection=%+v", verifications.Verifications)
	}
}

func TestMySQLTypedWorkbenchOptionalResourcesDistinguishAbsentFromMissingIncident(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := openWorkbenchIntegrationDB(t, ctx, adminDSN)

	incidentPublicID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster,
namespace, service_name, environment, target_kind, target_name, severity,
status, summary, first_seen_at, last_seen_at, version,
domain_schema_version, v3_status, cycle_no
) VALUES (?, ?, ?, 2, 'kind-local', 'demo', 'demo', 'local',
          'Deployment', 'demo', 'critical', 'DIAGNOSING',
          'optional Workbench resources fixture', NOW(6), NOW(6), 1,
          3, 'investigating', 1)`, incidentPublicID,
		"workbench-optional-"+incidentPublicID,
		"v2:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	port, err := NewMySQLQueryPort(db)
	if err != nil {
		t.Fatal(err)
	}

	delivery, err := port.Query(ctx, QueryRequest{Kind: QueryDelivery, IncidentID: incidentPublicID})
	if err != nil {
		t.Fatalf("existing Incident without Delivery: %v", err)
	}
	if delivery.Delivery != nil {
		t.Fatalf("existing Incident without Delivery returned %+v", delivery.Delivery)
	}
	report, err := port.Query(ctx, QueryRequest{Kind: QueryResolutionReport, IncidentID: incidentPublicID})
	if err != nil {
		t.Fatalf("existing Incident without ResolutionReport: %v", err)
	}
	if report.ResolutionReport != nil {
		t.Fatalf("existing Incident without ResolutionReport returned %+v", report.ResolutionReport)
	}

	missingIncidentID := uuid.NewString()
	for _, kind := range []QueryKind{QueryDelivery, QueryResolutionReport} {
		_, err := port.Query(ctx, QueryRequest{Kind: kind, IncidentID: missingIncidentID})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s for missing Incident error=%v, want ErrNotFound", kind, err)
		}
	}
}

type workbenchIntegrationFixture struct {
	incidentPublicID string
	planPublicID     string
	deliveryPublicID string
	runPublicID      string
	checkPublicID    string
	samplePublicID   string
	targetRevision   string
}

func openWorkbenchIntegrationDB(t *testing.T, ctx context.Context, adminDSN string) *sql.DB {
	t.Helper()
	adminConfig, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("cloudops_apiv3_workbench_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	config := adminConfig.Clone()
	config.DBName = databaseName
	config.ParseTime = true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+databaseName+"`")
		_ = admin.Close()
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+databaseName+"`")
		_ = admin.Close()
		t.Fatal(err)
	}
	runner, err := migration.NewRunner(ctx, db, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS `"+databaseName+"`")
		_ = admin.Close()
	})
	return db
}

func insertWorkbenchIntegrationFixture(t *testing.T, ctx context.Context, db *sql.DB) workbenchIntegrationFixture {
	t.Helper()
	var databaseNow time.Time
	if err := db.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
		t.Fatal(err)
	}
	now := databaseNow.UTC().Truncate(time.Microsecond)
	incidentPublicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster,
namespace, service_name, environment, target_kind, target_name, severity,
status, summary, first_seen_at, last_seen_at, version,
domain_schema_version, v3_status, cycle_no
) VALUES (?, ?, ?, 2, 'kind-local', 'demo', 'demo', 'local',
          'Deployment', 'demo', 'critical', 'DIAGNOSING',
          'typed Workbench fixture', ?, ?, 2, 3, 'investigating', 1)`,
		incidentPublicID, "workbench-"+incidentPublicID, "v2:"+strings.Repeat("1", 64),
		now.Add(-10*time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	incidentID64, _ := result.LastInsertId()
	incidentID := uint64(incidentID64)

	agentRunPublicID := uuid.NewString()
	diagnosisHash := strings.Repeat("2", 64)
	diagnosisJSON, _ := json.Marshal(map[string]any{"diagnosis_hash": diagnosisHash, "summary": "missing required env"})
	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, status, model, prompt_version, max_steps,
final_diagnosis, failure_code, completed_at, domain_schema_version,
v3_status, cycle_no, expected_incident_version
) VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, ?, '',
          ?, 3, 'completed', 1, 2)`, agentRunPublicID, incidentID, diagnosisJSON, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	agentRunID64, _ := result.LastInsertId()
	agentRunID := uint64(agentRunID64)

	evidencePublicID := uuid.NewString()
	evidenceHash := strings.Repeat("3", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items (
public_id, incident_id, agent_run_id, type, source, producer_type,
producer_dedupe_key, resource_ref, query_text, summary, facts_json,
result_hash, content_hash, raw_ref, truncated, valid, collected_at,
domain_schema_version, cycle_no
) VALUES (?, ?, ?, 'configuration', 'github', 'agent_step', ?,
          'github://acme/gitops/apps/demo/deployment.yaml', 'exact blob',
          'verified baseline node', JSON_OBJECT('required_env','healthy'),
          ?, ?, '', FALSE, TRUE, ?, 3, 1)`, evidencePublicID, incidentID,
		agentRunID, "workbench-evidence-"+evidencePublicID, evidenceHash, evidenceHash, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	targetRevision := strings.Repeat("6", 40)
	verificationPlan, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{
		TriggerType: "post_delivery", Repository: "acme/gitops", PullRequest: 42,
		TargetRevision: targetRevision, SourceRevision: strings.Repeat("7", 40),
		ImageDigest: "sha256:" + strings.Repeat("8", 64), GitOpsRevision: targetRevision,
		ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo",
		Cluster: "kind-local", Environment: "local", Namespace: "demo",
		Service: "demo", WorkloadName: "demo",
		AlertNames: []string{"RequiredEnvMissing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	verificationPlanJSON, _ := json.Marshal(verificationPlan)
	current := []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\n  namespace: demo\nspec:\n  template:\n    spec:\n      containers:\n        - name: demo\n          image: example/demo@sha256:" + strings.Repeat("9", 64) + "\n")
	baseline := append(append([]byte(nil), current...), []byte("          env:\n            - name: REQUIRED_ENV\n              value: healthy\n")...)
	policy := remediation.RestoreEnvPolicy{
		Version: "restore-required-env-policy/v1", Repository: "acme/gitops", BaseBranch: "main",
		AllowedPath: "apps/demo/deployment.yaml", APIVersion: "apps/v1", Namespace: "demo",
		Workload: "demo", Container: "demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
		VerificationVersion: verification.GoldenRequiredEnvProfileID,
	}
	createdAt := now.Add(-time.Minute)
	plan, err := remediation.CompileRestoreRequiredEnv(remediation.RestoreEnvCompileRequest{
		IncidentPublicID: incidentPublicID, IncidentID: incidentID, CycleNo: 1, IncidentVersion: 2,
		CreatedByAgentRunID: agentRunPublicID, DiagnosisHash: diagnosisHash,
		Repository: policy.Repository, BaseBranch: policy.BaseBranch, BaseRevision: strings.Repeat("a", 40),
		LastKnownGoodRevision: strings.Repeat("b", 40), TargetPath: policy.AllowedPath,
		BaseBlobSHA: strings.Repeat("c", 40), ExpectedTreeHash: strings.Repeat("d", 40), FileMode: "100644",
		Target: remediation.TargetResource{
			APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "demo", Container: "demo",
		},
		EnvKey: "REQUIRED_ENV", CurrentContent: current, BaselineContent: baseline,
		Policy: policy, VerificationPlan: verificationPlanJSON,
		Evidence:           []remediation.EvidenceBinding{{ID: evidencePublicID, ContentHash: evidenceHash}},
		BaselineIsAncestor: true, CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * time.Minute), PlanVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := remediationmysql.NewV3RemediationRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreatePlan(ctx, &plan); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'AWAITING_APPROVAL', v3_status = 'awaiting_approval', version = 3,
    updated_at = ?
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = 1`, now, incidentID); err != nil {
		t.Fatal(err)
	}
	decision := remediation.Approval{
		PublicID: uuid.NewString(), DomainSchemaVersion: 3, DecisionSchemaVersion: 1,
		IncidentID: incidentID, CycleNo: 1, PlanID: plan.ID, PlanVersion: plan.PlanVersion,
		Decision: remediation.DecisionApproved, ActorProvider: "github", Actor: "oncall", Role: "operator",
		Reason: "reviewed exact immutable plan", RequestID: "workbench-integration-decision",
		RequestAuthenticatedAt: now.Add(-30 * time.Second), CreatedAt: now.Add(-29 * time.Second),
		ExpiresAt: plan.ExpiresAt, ApprovedHashSchemaVersion: plan.HashSchemaVersion,
		ApprovedPlanHash: plan.CanonicalPlanHash, ApprovedBaseSHA: plan.TargetBaseRevision,
		ApprovedPostImageHash: plan.ExpectedPostImageHash, ApprovedTreeHash: plan.ExpectedTreeHash,
		ApprovedPatchHash: plan.ProposedPatchHash, ApprovedPolicyHash: plan.PolicySnapshotHash,
		ApprovedVerificationHash: plan.VerificationPlanHash, ApprovedEvidenceSetHash: plan.EvidenceSetHash,
	}
	if err := repository.RecordDecision(ctx, plan.PublicID, plan.RowVersion, &decision); err != nil {
		t.Fatal(err)
	}

	deliveryPublicID := uuid.NewString()
	deliveryStarted := now.Add(-20 * time.Second)
	deliveryDeadline := now.Add(5 * time.Minute)
	result, err = db.ExecContext(ctx, `INSERT INTO change_requests (
public_id, plan_id, repository, base_revision, head_branch, commit_sha,
pr_number, pr_url, pr_state, merged_commit_sha, target_revision, status,
ci_status, idempotency_key, argocd_application, argocd_project,
detected_revision, argocd_sync_status, argocd_operation_phase,
argocd_health_status, resource_health_json, cluster, environment, namespace,
workload_kind, workload_name, deployment_generation, observed_generation,
rollout_revision, desired_replicas, updated_replicas, available_replicas,
unavailable_replicas, delivery_started_at, delivery_deadline_at,
last_observed_at, row_version, domain_schema_version, incident_id, cycle_no,
v3_status, write_phase, expected_subject_version, logical_operation_key,
created_at, updated_at
) VALUES (?, ?, 'acme/gitops', ?, 'cloudops/typed-workbench', ?, 42,
          'https://github.com/acme/gitops/pull/42', 'open', ?, ?,
          'rollout_pending', 'passing', ?, 'cloudops-demo', 'cloudops-demo', ?,
          'Synced', 'Succeeded', 'Progressing', JSON_OBJECT('deployment','Progressing'),
          'kind-local', 'local', 'demo', 'Deployment', 'demo',
          8, 8, ?, 2, 2, 1, 1, ?, ?, ?, 2, 3, ?, 1, 'rolling_out',
          'observe', 1, ?, ?, ?)`, deliveryPublicID, plan.ID, plan.TargetBaseRevision,
		strings.Repeat("5", 40), targetRevision, targetRevision, strings.Repeat("4", 64),
		targetRevision, targetRevision, deliveryStarted, deliveryDeadline,
		now, incidentID, strings.Repeat("f", 64), createdAt, now)
	if err != nil {
		t.Fatal(err)
	}
	deliveryID64, _ := result.LastInsertId()
	deliveryID := uint64(deliveryID64)

	runPublicID := uuid.NewString()
	result, err = db.ExecContext(ctx, `INSERT INTO verification_runs (
public_id, incident_id, domain_schema_version, cycle_no, remediation_plan_id,
change_request_id, status, v3_status, trigger_type, trigger_signal_id,
target_revision, source_revision, image_digest, gitops_revision, plan_json,
verification_profile_version, verification_profile_hash,
verification_contract_version, verification_profile_id,
common_stability_window_ms, started_at, deadline_at, attempt, row_version,
expected_subject_version, result_summary, failure_reason, created_at, updated_at
) VALUES (?, ?, 3, 1, ?, ?, 'running', 'running', 'post_delivery', NULL,
          ?, ?, ?, ?, ?, 1, ?, 1, 'golden-required-env/v1', 60000,
          ?, ?, 1, 1, 2, '', '', ?, ?)`, runPublicID, incidentID, plan.ID,
		deliveryID, verificationPlan.TargetRevision, verificationPlan.SourceRevision,
		verificationPlan.ImageDigest, verificationPlan.GitOpsRevision, verificationPlanJSON,
		verificationPlan.ProfileHash, now, now.Add(5*time.Minute), now, now)
	if err != nil {
		t.Fatal(err)
	}
	runID64, _ := result.LastInsertId()
	runID := uint64(runID64)
	spec := verificationPlan.Checks[0]
	subjectJSON, _ := json.Marshal(spec.Subject)
	checkPublicID := uuid.NewString()
	result, err = db.ExecContext(ctx, `INSERT INTO verification_checks (
public_id, verification_run_id, domain_schema_version, incident_id, cycle_no,
check_type, status, required_check, subject_json, expected_json, observed_json,
source_reference, lookback_ms, stability_window_ms, timeout_ms,
poll_interval_ms, check_spec_schema_version, profile_id, template_id,
template_version, comparison, threshold, source_identity, initial_delay_ms,
min_samples, sample_unit, failure_mode, attempt_count, created_at, updated_at
) VALUES (?, ?, 3, ?, 1, ?, 'running', TRUE, ?, ?, JSON_OBJECT('status','available'),
          '', ?, ?, ?, ?, 1, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, 1, ?, ?)`,
		checkPublicID, runID, incidentID, spec.Type, subjectJSON, spec.Expected,
		spec.Lookback.Milliseconds(), spec.StabilityWindow.Milliseconds(), spec.Timeout.Milliseconds(),
		spec.PollInterval.Milliseconds(), spec.ProfileID, spec.TemplateID, spec.TemplateVersion,
		spec.SourceIdentity, spec.InitialDelay.Milliseconds(), spec.MinSamples, spec.SampleUnit,
		spec.FailureMode, now, now)
	if err != nil {
		t.Fatal(err)
	}
	checkID64, _ := result.LastInsertId()
	checkID := uint64(checkID64)
	samplePublicID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO verification_samples (
public_id, domain_schema_version, sample_schema_version, incident_id, cycle_no,
verification_run_id, verification_check_id, sample_sequence, status,
observed_json, source_reference, reason_code, sampled_at, content_hash, created_at
) VALUES (?, 3, 1, ?, 1, ?, ?, 1, 'pending', JSON_OBJECT('status','available'),
          '', '', ?, ?, ?)`, samplePublicID, incidentID, runID, checkID, now,
		strings.Repeat("e", 64), now); err != nil {
		t.Fatal(err)
	}
	return workbenchIntegrationFixture{
		incidentPublicID: incidentPublicID, planPublicID: plan.PublicID,
		deliveryPublicID: deliveryPublicID, runPublicID: runPublicID,
		checkPublicID: checkPublicID, samplePublicID: samplePublicID,
		targetRevision: targetRevision,
	}
}
