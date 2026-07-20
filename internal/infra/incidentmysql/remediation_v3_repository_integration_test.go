package incidentmysql

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

	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestMySQLV3RemediationRepositoryFencesAndReplays(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := openV3RemediationSQL(t, adminDSN)
	name := fmt.Sprintf("cloudops_v3_remediation_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	db := openV3RemediationSQL(t, v3RemediationDatabaseDSN(t, adminDSN, name))
	defer func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		_ = admin.Close()
	}()
	runner, err := migrationrunner.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatalf("migrate V3 remediation database: %v", err)
	}

	fixture := insertV3RemediationFixture(t, ctx, db)
	repository, err := NewV3RemediationRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	request := v3RemediationCompileRequest(fixture)
	expiredRequest := request
	expiredRequest.CreatedAt = time.Now().UTC().Add(-time.Hour)
	expiredRequest.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	expiredPlan, err := remediation.CompileRestoreRequiredEnv(expiredRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreatePlan(ctx, &expiredPlan); !errors.Is(err, remediation.ErrConflict) {
		t.Fatalf("expired Plan accepted: %v", err)
	}

	staleRequest := request
	staleRequest.IncidentVersion++
	stalePlan, err := remediation.CompileRestoreRequiredEnv(staleRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreatePlan(ctx, &stalePlan); !errors.Is(err, remediation.ErrConflict) {
		t.Fatalf("stale Incident version accepted: %v", err)
	}

	plan, err := remediation.CompileRestoreRequiredEnv(request)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	resolvedRunID, err := repository.ResolveAgentRunID(ctx, tx, fixture.agentRunPublicID, fixture.incidentID, 1)
	if err != nil || resolvedRunID != fixture.agentRunID {
		_ = tx.Rollback()
		t.Fatalf("resolved AgentRun id=%d want=%d err=%v", resolvedRunID, fixture.agentRunID, err)
	}
	if err := repository.CreatePlanIn(ctx, tx, &plan); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if plan.ID == 0 {
		_ = tx.Rollback()
		t.Fatal("transactional Plan insert did not return its internal ID")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetPlan(ctx, plan.PublicID); !errors.Is(err, remediation.ErrNotFound) {
		t.Fatalf("rolled-back Plan is visible: %v", err)
	}
	plan.ID = 0
	if err := repository.CreatePlan(ctx, &plan); err != nil {
		t.Fatal(err)
	}
	createdID := plan.ID
	if err := repository.CreatePlan(ctx, &plan); err != nil || plan.ID != createdID {
		t.Fatalf("Plan replay id=%d want=%d err=%v", plan.ID, createdID, err)
	}
	loaded, err := repository.GetPlan(ctx, plan.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CreatedByAgentRunID != fixture.agentRunPublicID || loaded.CanonicalPlanHash != plan.CanonicalPlanHash || !loaded.CreatedAt.Equal(plan.CreatedAt) || string(loaded.PostImage) != string(plan.PostImage) {
		t.Fatalf("Plan round trip lost immutable bindings: loaded=%+v", loaded)
	}
	var persistedRunID uint64
	if err := db.QueryRowContext(ctx, "SELECT created_by_agent_run_id FROM remediation_plans WHERE id = ?", plan.ID).Scan(&persistedRunID); err != nil || persistedRunID != fixture.agentRunID {
		t.Fatalf("Plan creator FK=%d want=%d err=%v", persistedRunID, fixture.agentRunID, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'AWAITING_APPROVAL', v3_status = 'awaiting_approval', version = version + 1
WHERE id = ? AND cycle_no = 1 AND domain_schema_version = 3`, fixture.incidentID); err != nil {
		t.Fatal(err)
	}
	decisionAt := plan.CreatedAt.Add(3*time.Minute + 987*time.Nanosecond)
	decision, err := remediation.NewV3Approval(plan, "github", " operator-login ", "operator", " reviewed exact diff ", "request-v3-1", decisionAt, decisionAt.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if decision.CreatedAt.Nanosecond()%1000 != 0 {
		t.Fatalf("Decision time was not normalized to MySQL precision: %s", decision.CreatedAt)
	}
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET version = version + 1 WHERE id = ?", fixture.incidentID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordDecision(ctx, plan.PublicID, plan.RowVersion, &decision); !errors.Is(err, remediation.ErrConflict) {
		t.Fatalf("stale Incident version accepted for Decision: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET version = version - 1 WHERE id = ?", fixture.incidentID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordDecision(ctx, plan.PublicID, plan.RowVersion+1, &decision); !errors.Is(err, remediation.ErrConflict) {
		t.Fatalf("stale Plan row version accepted: %v", err)
	}

	decisionTx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordDecisionIn(ctx, decisionTx, plan.PublicID, plan.RowVersion, &decision); err != nil {
		_ = decisionTx.Rollback()
		t.Fatal(err)
	}
	if decision.ID == 0 {
		_ = decisionTx.Rollback()
		t.Fatal("transactional Decision insert did not return its internal ID")
	}
	if err := decisionTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetDecision(ctx, plan.PublicID); !errors.Is(err, remediation.ErrNotFound) {
		t.Fatalf("rolled-back Decision is visible: %v", err)
	}
	decision.ID = 0
	if err := repository.RecordDecision(ctx, plan.PublicID, plan.RowVersion, &decision); err != nil {
		t.Fatal(err)
	}
	decisionID := decision.ID
	if err := repository.RecordDecision(ctx, plan.PublicID, plan.RowVersion, &decision); err != nil || decision.ID != decisionID {
		t.Fatalf("Decision replay id=%d want=%d err=%v", decision.ID, decisionID, err)
	}
	loadedDecision, err := repository.GetDecision(ctx, plan.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedDecision.PublicID != decision.PublicID || loadedDecision.ApprovedPlanHash != plan.CanonicalPlanHash || !loadedDecision.CreatedAt.Equal(decision.CreatedAt) {
		t.Fatalf("Decision round trip lost immutable bindings: loaded=%+v", loadedDecision)
	}
	approvedPlan, err := repository.GetPlan(ctx, plan.PublicID)
	if err != nil || approvedPlan.Status != remediation.PlanApproved || approvedPlan.RowVersion != plan.RowVersion+1 {
		t.Fatalf("approved Plan=%+v err=%v", approvedPlan, err)
	}

	conflict, err := remediation.NewV3Decision(plan, remediation.DecisionRejected, "github", "other-operator", "operator", "reject", "request-v3-2", decisionAt, decisionAt.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordDecision(ctx, plan.PublicID, approvedPlan.RowVersion, &conflict); !errors.Is(err, remediation.ErrConflict) {
		t.Fatalf("second terminal Decision accepted: %v", err)
	}
}

type v3RemediationFixture struct {
	incidentID          uint64
	incidentPublicID    string
	agentRunID          uint64
	agentRunPublicID    string
	evidencePublicID    string
	evidenceContentHash string
}

func insertV3RemediationFixture(t *testing.T, ctx context.Context, db *sql.DB) v3RemediationFixture {
	t.Helper()
	fixture := v3RemediationFixture{
		incidentPublicID: uuid.NewString(), agentRunPublicID: uuid.NewString(),
		evidencePublicID: uuid.NewString(), evidenceContentHash: strings.Repeat("1", 64),
	}
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no
) VALUES (?, 'v3-remediation-fixture', ?, 2, 'kind-v3', 'cloudops-demo', 'demo',
          'development', 'Deployment', 'demo', 'warning', 'DIAGNOSING',
          'V3 remediation repository fixture', NOW(6), NOW(6), 2, 3, 'investigating', 1)`,
		fixture.incidentPublicID, "v2:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	fixture.incidentID = uint64(incidentID)
	diagnosisHash := strings.Repeat("d", 64)
	diagnosisJSON, _ := json.Marshal(map[string]any{"diagnosis_hash": diagnosisHash})
	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, status, model, prompt_version, max_steps, final_diagnosis,
failure_code, completed_at, domain_schema_version, v3_status, cycle_no,
expected_incident_version
) VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, ?, '',
          NOW(6), 3, 'completed', 1, 2)`, fixture.agentRunPublicID, fixture.incidentID, diagnosisJSON)
	if err != nil {
		t.Fatal(err)
	}
	agentRunID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	fixture.agentRunID = uint64(agentRunID)
	_, err = db.ExecContext(ctx, `INSERT INTO evidence_items (
public_id, incident_id, agent_run_id, type, source, producer_type,
producer_dedupe_key, resource_ref, query_text, summary, facts_json, result_hash,
content_hash, raw_ref, truncated, valid, collected_at, domain_schema_version, cycle_no
) VALUES (?, ?, ?, 'configuration', 'github', 'agent_step', 'fixture-evidence',
          'github://acme/gitops/apps/demo.yaml', 'exact blob', 'verified baseline node',
          JSON_OBJECT('required_env','healthy'), ?, ?, '', FALSE, TRUE, NOW(6), 3, 1)`,
		fixture.evidencePublicID, fixture.incidentID, fixture.agentRunID,
		fixture.evidenceContentHash, fixture.evidenceContentHash)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func v3RemediationCompileRequest(fixture v3RemediationFixture) remediation.RestoreEnvCompileRequest {
	current := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: cloudops-demo
spec:
  template:
    spec:
      containers:
        - name: demo
          image: example/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`)
	baseline := append(append([]byte(nil), current...), []byte("          env:\n            - name: REQUIRED_ENV\n              value: healthy\n")...)
	policy := remediation.RestoreEnvPolicy{
		Version: "restore-required-env-policy/v1", Repository: "acme/gitops", BaseBranch: "main",
		AllowedPath: "apps/demo.yaml", APIVersion: "apps/v1", Namespace: "cloudops-demo",
		Workload: "demo", Container: "demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
		VerificationVersion: "golden-required-env/v1",
	}
	createdAt := time.Now().UTC().Add(-time.Minute).Add(987 * time.Nanosecond)
	return remediation.RestoreEnvCompileRequest{
		IncidentPublicID: fixture.incidentPublicID, IncidentID: fixture.incidentID,
		CycleNo: 1, IncidentVersion: 2, CreatedByAgentRunID: fixture.agentRunPublicID,
		DiagnosisHash: strings.Repeat("d", 64), Repository: policy.Repository, BaseBranch: policy.BaseBranch,
		BaseRevision: strings.Repeat("a", 40), LastKnownGoodRevision: strings.Repeat("b", 40),
		TargetPath: policy.AllowedPath, BaseBlobSHA: strings.Repeat("c", 40),
		ExpectedTreeHash: strings.Repeat("e", 40), FileMode: "100644",
		Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "cloudops-demo", Name: "demo", Container: "demo"},
		EnvKey: "REQUIRED_ENV", CurrentContent: current, BaselineContent: baseline, Policy: policy,
		VerificationPlan:   json.RawMessage(`{"profile":"golden-required-env/v1","stability_window_seconds":60}`),
		Evidence:           []remediation.EvidenceBinding{{ID: fixture.evidencePublicID, ContentHash: fixture.evidenceContentHash}},
		BaselineIsAncestor: true, CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * time.Minute), PlanVersion: 1,
	}
}

func openV3RemediationSQL(t *testing.T, dsn string) *sql.DB {
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

func v3RemediationDatabaseDSN(t *testing.T, adminDSN, name string) string {
	t.Helper()
	config, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName = name
	config.ParseTime = true
	config.MultiStatements = true
	config.Loc = time.UTC
	return config.FormatDSN()
}
