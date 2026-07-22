package cutover

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/pressly/goose/v3"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
	rootmigrations "github.com/05allan1213/CloudOps-Copilot/migrations"
)

func TestMySQLPhase7AMigrationBackfillConversionAuditAndMarkerContracts(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires one disposable MySQL 8.0.x instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	admin := openPhase7ATestDB(t, adminDSN)
	defer admin.Close()
	var mysqlVersion string
	if err := admin.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(mysqlVersion, "8.0.") {
		t.Fatalf("Phase 7A validation requires MySQL 8.0.x, got %q", mysqlVersion)
	}

	suffix := fmt.Sprint(time.Now().UnixNano())
	freshName := "cloudops_phase7a_fresh_" + suffix
	existingName := "cloudops_phase7a_existing_" + suffix
	for _, name := range []string{freshName, existingName} {
		createPhase7ATestDatabase(t, ctx, admin, name)
		defer dropPhase7ATestDatabase(t, admin, name)
	}

	fresh := openPhase7ATestDB(t, phase7ATestDatabaseDSN(t, adminDSN, freshName))
	defer fresh.Close()
	freshProvider := newPhase7ATestProvider(t, fresh)
	if _, err := freshProvider.Up(ctx); err != nil {
		t.Fatalf("fresh 00001->00017: %v", err)
	}
	assertPhase7ASchemaVersion(t, ctx, fresh, uint64(schemaversion.Latest))
	assertFreshPreparationDetectsPostPassDrift(t, ctx, fresh)

	existing := openPhase7ATestDB(t, phase7ATestDatabaseDSN(t, adminDSN, existingName))
	defer existing.Close()
	existingProvider := newPhase7ATestProvider(t, existing)
	if _, err := existingProvider.UpTo(ctx, 16); err != nil {
		t.Fatalf("existing schema through 00016: %v", err)
	}
	fixture := insertPhase7ALegacyFixture(t, ctx, existing)
	if _, err := existingProvider.Up(ctx); err != nil {
		t.Fatalf("existing 00016->00017: %v", err)
	}
	assertPhase7ASchemaVersion(t, ctx, existing, uint64(schemaversion.Latest))

	request := phase7ATestPrepareRequest()
	backfillIdentity := ReleaseIdentity{PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SourceSchema: uint64(schemaversion.Latest - 1), TargetSchema: uint64(schemaversion.Latest)}
	faulted := false
	backfiller, err := NewPhase7ABackfiller(existing, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	backfiller.WithFaultInjector(func(operation string, batchNo uint64, point string) error {
		if !faulted && operation == BackfillOperationPrefix+"incident-signals" && batchNo == 1 && point == "after-target-write" {
			faulted = true
			return errors.New("injected restart boundary")
		}
		return nil
	})
	if _, err := backfiller.Run(ctx, BackfillRequest{Identity: backfillIdentity, BatchSize: 1}); err == nil || !strings.Contains(err.Error(), "injected restart boundary") {
		t.Fatalf("BACKFILL-V3 fault result=%v", err)
	}
	backfiller.WithFaultInjector(nil)
	backfillReport, err := backfiller.Run(ctx, BackfillRequest{Identity: backfillIdentity, BatchSize: 1})
	if err != nil {
		t.Fatalf("BACKFILL-V3 restart: %v", err)
	}
	if !faulted || backfillReport.Counts["incident-signals"] != 1 || backfillReport.Counts["incident-events"] != 1 ||
		backfillReport.Counts["evidence"] == 0 || backfillReport.Counts["agent-steps"] == 0 || backfillReport.Counts["change-candidates"] != 1 {
		t.Fatalf("unexpected BACKFILL-V3 report: %+v", backfillReport)
	}
	assertBackfillRetryChain(t, ctx, existing)
	fixture.nativeTaskID = insertExistingNativeAgentTask(t, ctx, existing, fixture.compatibleAgent)

	reconciler := phase7AFixtureReconciler{items: fixture.pullRequests}
	preparer, err := NewPhase7APreparer(existing, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	preparer.WithLegacyChangeReconciler(reconciler)
	report, err := preparer.Prepare(ctx, request)
	if err != nil {
		t.Fatalf("prepare Phase 7A fixture: %v", err)
	}
	assertPhase7APrepareReport(t, report)
	assertPhase7AConvertedFixture(t, ctx, existing, fixture)

	taskCount := phase7ATestCount(t, ctx, existing, "SELECT COUNT(*) FROM async_tasks")
	conversionCount := phase7ATestCount(t, ctx, existing, "SELECT COUNT(*) FROM legacy_conversion_records")
	ledgerCount := phase7ATestCount(t, ctx, existing, "SELECT COUNT(*) FROM migration_ledger")
	repeated, err := preparer.Prepare(ctx, request)
	if err != nil {
		t.Fatalf("repeat Phase 7A prepare: %v", err)
	}
	if repeated.QuiesceLedgerPublicID != report.QuiesceLedgerPublicID ||
		repeated.ReconciliationLedgerPublicID != report.ReconciliationLedgerPublicID ||
		repeated.ConverterAuditLedgerPublicID != report.ConverterAuditLedgerPublicID ||
		phase7ATestCount(t, ctx, existing, "SELECT COUNT(*) FROM async_tasks") != taskCount ||
		phase7ATestCount(t, ctx, existing, "SELECT COUNT(*) FROM legacy_conversion_records") != conversionCount ||
		phase7ATestCount(t, ctx, existing, "SELECT COUNT(*) FROM migration_ledger") != ledgerCount {
		t.Fatal("repeated prepare changed durable tasks, conversions, or ledgers")
	}

	assertConcurrentPhase7APrepare(t, ctx, existing, request, reconciler, report)
	auditBeforeMarker := exportPhase7ATestAudit(t, ctx, existing)
	for _, forbidden := range []string{fixture.outboxSecret, fixture.checkpointSecret, "current_checkpoint", "payload_json", "narrative"} {
		if strings.Contains(auditBeforeMarker, forbidden) {
			t.Fatalf("audit export leaked forbidden content %q", forbidden)
		}
	}
	if !strings.Contains(auditBeforeMarker, `"outbox_event_counts"`) ||
		!strings.Contains(auditBeforeMarker, `"conversion_counts"`) ||
		!strings.Contains(auditBeforeMarker, `"migrated_legacy_context_tasks"`) {
		t.Fatalf("audit export omitted required count families: %s", auditBeforeMarker)
	}

	writer, err := NewSQLMarkerWriter(existing, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := writer.Write(ctx, WriteRequest{
		PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SourceSchemaVersion: uint64(schemaversion.Latest),
		TargetSchemaVersion: uint64(schemaversion.Latest), QuiesceLedgerPublicID: report.QuiesceLedgerPublicID,
		ReconciliationLedgerPublicID: report.ReconciliationLedgerPublicID,
		ConverterAuditLedgerPublicID: report.ConverterAuditLedgerPublicID,
		OldWorkerCount:               0, Confirmation: IrreversibleConfirmation,
	})
	if err != nil {
		t.Fatalf("write test-only CUTOVER-V3 marker: %v", err)
	}
	reader, err := NewSQLMarkerReader(existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := RefuseCompatibilityRuntime(ctx, reader); !errors.Is(err, ErrCompatibilityRefused) {
		t.Fatalf("compatibility runtime accepted marker: %v", err)
	}
	accepted, err := RequireV3Runtime(ctx, reader)
	if err != nil || accepted.PublicID != marker.PublicID {
		t.Fatalf("V3 runtime rejected valid marker: marker=%+v err=%v", accepted, err)
	}
	if auditAfterMarker := exportPhase7ATestAudit(t, ctx, existing); !strings.Contains(auditAfterMarker, `"marker": {`) ||
		!strings.Contains(auditAfterMarker, `"count": 1`) || !strings.Contains(auditAfterMarker, request.SourceExactSHA) {
		t.Fatalf("audit export omitted marker state: %s", auditAfterMarker)
	}

	t.Logf("mysql_version=%s fresh_schema=00001-%05d existing_schema=00016-%05d tasks=%d conversions=%d ledgers=%d marker=%s",
		mysqlVersion, schemaversion.Latest, schemaversion.Latest, taskCount, conversionCount, ledgerCount, marker.PublicID)
}

type phase7ALegacyFixture struct {
	compatibleAgent          phase7AAgentFixture
	incompatibleAgent        phase7AAgentFixture
	terminalAgent            phase7AAgentFixture
	compatibleVerification   uint64
	terminalVerification     uint64
	incompatibleVerification uint64
	openChangeRequest        uint64
	nativeTaskID             uint64
	fallbackIncidentID       uint64
	pullRequests             map[int64]ReconciledPullRequest
	outboxSecret             string
	checkpointSecret         string
}

type phase7AAgentFixture struct {
	incidentID        uint64
	incidentPublicID  string
	runID             uint64
	runPublicID       string
	runVersion        uint64
	checkpointVersion uint64
}

type phase7AFixtureReconciler struct {
	items map[int64]ReconciledPullRequest
}

func (r phase7AFixtureReconciler) ReconcilePullRequest(_ context.Context, artifact LegacyExternalArtifact) (ReconciledPullRequest, error) {
	item, ok := r.items[artifact.PullRequest]
	if !ok {
		return ReconciledPullRequest{}, fmt.Errorf("fixture PR %d is absent", artifact.PullRequest)
	}
	return item, nil
}

func assertFreshPreparationDetectsPostPassDrift(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	request := phase7ATestPrepareRequest()
	preparer, err := NewPhase7APreparer(db, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preparer.Prepare(ctx, request); err != nil {
		t.Fatalf("empty fresh prepare: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO outbox_events
(event_id,aggregate_type,aggregate_id,event_type,schema_version,payload_json,occurred_at,published_at,attempts,last_error,created_at)
VALUES (?,'incident','fixture-unknown','unknown.phase7a',99,'{}',UTC_TIMESTAMP(6),NULL,0,'',UTC_TIMESTAMP(6))`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := preparer.Prepare(ctx, request); err == nil || !strings.Contains(err.Error(), "outbox archive parity") {
		t.Fatalf("completed prepare accepted source drift: %v", err)
	}
	var status string
	var attempt uint64
	var previous sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT status,attempt,previous_ledger_id FROM migration_ledger
WHERE operation=? ORDER BY attempt DESC LIMIT 1`, ConverterAuditOperation).Scan(&status, &attempt, &previous); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempt != 2 || !previous.Valid || previous.Int64 <= 0 {
		t.Fatalf("post-pass drift ledger status=%s attempt=%d previous=%+v", status, attempt, previous)
	}
}

func insertPhase7ALegacyFixture(t *testing.T, ctx context.Context, db *sql.DB) phase7ALegacyFixture {
	t.Helper()
	base := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	fixture := phase7ALegacyFixture{
		pullRequests:     map[int64]ReconciledPullRequest{},
		outboxSecret:     "phase7a-outbox-secret-body",
		checkpointSecret: "phase7a-checkpoint-secret-objective",
	}
	compatibleIncident := insertPhase7AIncident(t, ctx, db, "DIAGNOSING", false, base)
	fixture.compatibleAgent = insertPhase7AAgentRun(t, ctx, db, compatibleIncident, "RUNNING", true, fixture.checkpointSecret, base)
	fixture.fallbackIncidentID = insertPhase7AIncident(t, ctx, db, "DIAGNOSING", false, base.Add(time.Minute)).id
	fallbackIncident := phase7AIncidentFixture{id: fixture.fallbackIncidentID, publicID: phase7AIncidentPublicID(t, ctx, db, fixture.fallbackIncidentID), version: 3}
	fixture.incompatibleAgent = insertPhase7AAgentRun(t, ctx, db, fallbackIncident, "RUNNING", false, "incompatible checkpoint", base.Add(time.Minute))
	terminalIncident := insertPhase7AIncident(t, ctx, db, "DIAGNOSIS_COMPLETED", false, base.Add(2*time.Minute))
	fixture.terminalAgent = insertPhase7AAgentRun(t, ctx, db, terminalIncident, "COMPLETED", true, "terminal checkpoint", base.Add(2*time.Minute))

	openIncident := insertPhase7AIncident(t, ctx, db, "APPLYING_CHANGE", false, base.Add(3*time.Minute))
	openPlan, openChange := insertPhase7APlanAndChange(t, ctx, db, openIncident, true, "pr_created", "feature/open", strings.Repeat("b", 40), 7, "open", "", base.Add(3*time.Minute))
	_ = openPlan
	fixture.openChangeRequest = openChange.id
	fixture.pullRequests[7] = reconciledPhase7APR(openChange, false, "")

	partialIncident := insertPhase7AIncident(t, ctx, db, "FAILED", false, base.Add(4*time.Minute))
	insertPhase7APlanAndChange(t, ctx, db, partialIncident, true, "delivering", "feature/partial", strings.Repeat("c", 40), 0, "", "", base.Add(4*time.Minute))
	approvalIncident := insertPhase7AIncident(t, ctx, db, "AWAITING_APPROVAL", false, base.Add(5*time.Minute))
	insertPhase7APlanAndChange(t, ctx, db, approvalIncident, true, "pending", "", "", 0, "", "", base.Add(5*time.Minute))

	lokiPlan, lokiChange := insertPhase7APlanAndChange(t, ctx, db, fallbackIncident, false, "delivering", "feature/loki-partial", strings.Repeat("d", 40), 0, "", "", base.Add(6*time.Minute))
	fixture.incompatibleVerification = insertPhase7AVerification(t, ctx, db, fallbackIncident, lokiPlan.id, lokiChange.id, 11, verification.RunRunning, false, true, base.Add(6*time.Minute))

	activeVerificationIncident := insertPhase7AIncident(t, ctx, db, "VERIFYING", false, base.Add(7*time.Minute))
	activePlan, activeChange := insertPhase7APlanAndChange(t, ctx, db, activeVerificationIncident, false, "delivered", "feature/verification", strings.Repeat("e", 40), 8, "merged", strings.Repeat("f", 40), base.Add(7*time.Minute))
	fixture.pullRequests[8] = reconciledPhase7APR(activeChange, true, strings.Repeat("f", 40))
	fixture.compatibleVerification = insertPhase7AVerification(t, ctx, db, activeVerificationIncident, activePlan.id, activeChange.id, 8, verification.RunRunning, false, false, base.Add(7*time.Minute))

	resolvedIncident := insertPhase7AIncident(t, ctx, db, "RESOLVED", true, base.Add(8*time.Minute))
	resolvedPlan, resolvedChange := insertPhase7APlanAndChange(t, ctx, db, resolvedIncident, false, "delivered", "feature/resolved", strings.Repeat("1", 40), 9, "merged", strings.Repeat("2", 40), base.Add(8*time.Minute))
	fixture.pullRequests[9] = reconciledPhase7APR(resolvedChange, true, strings.Repeat("2", 40))
	fixture.terminalVerification = insertPhase7AVerification(t, ctx, db, resolvedIncident, resolvedPlan.id, resolvedChange.id, 9, verification.RunPassed, true, false, base.Add(8*time.Minute))
	insertPhase7APostmortem(t, ctx, db, resolvedIncident.id, fixture.terminalVerification, base.Add(10*time.Minute))
	insertPhase7AIncident(t, ctx, db, "RESOLVED", true, base.Add(9*time.Minute))

	insertPhase7ABackfillFacts(t, ctx, db, compatibleIncident, fixture.compatibleAgent.runID, base)
	if _, err := db.ExecContext(ctx, `INSERT INTO outbox_events
(event_id,aggregate_type,aggregate_id,event_type,schema_version,payload_json,occurred_at,published_at,attempts,last_error,created_at)
VALUES (?,?,?,?,1,?, ?,NULL,0,'',?), (?,?,?,?,1,?, ?,?,1,'delivered',?)`,
		uuid.NewString(), "incident", compatibleIncident.publicID, "incident.created", mustPhase7AJSON(t, map[string]any{"secret": fixture.outboxSecret}), base, base,
		uuid.NewString(), "incident", compatibleIncident.publicID, "incident.updated", mustPhase7AJSON(t, map[string]any{"status": "published"}), base.Add(time.Second), base.Add(2*time.Second), base.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	return fixture
}

type phase7AIncidentFixture struct {
	id       uint64
	publicID string
	version  uint64
}

func insertPhase7AIncident(t *testing.T, ctx context.Context, db *sql.DB, status string, resolved bool, at time.Time) phase7AIncidentFixture {
	t.Helper()
	publicID := uuid.NewString()
	var resolvedAt any
	if resolved {
		resolvedAt = at.UTC()
	}
	result, err := db.ExecContext(ctx, `INSERT INTO incidents
(public_id,fingerprint,correlation_key,cluster,namespace,service_name,environment,target_kind,target_name,severity,
status,summary,first_seen_at,last_seen_at,resolved_at,version,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,? ,?,?,?,?,?,3,?,?)`, publicID, "fp-"+publicID, "corr-"+publicID,
		"kind", "default", "demo", "test", "Deployment", "demo", "critical", status,
		"legacy incident fixture", at.Add(-time.Minute), at, resolvedAt, at.Add(-time.Minute), at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return phase7AIncidentFixture{id: uint64(id), publicID: publicID, version: 3}
}

func phase7AIncidentPublicID(t *testing.T, ctx context.Context, db *sql.DB, id uint64) string {
	t.Helper()
	var publicID string
	if err := db.QueryRowContext(ctx, "SELECT public_id FROM incidents WHERE id=?", id).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	return publicID
}

func insertPhase7AAgentRun(t *testing.T, ctx context.Context, db *sql.DB, incident phase7AIncidentFixture,
	status string, compatible bool, objective string, at time.Time) phase7AAgentFixture {
	t.Helper()
	input, _ := validAgentCheckpointInput(t)
	graph := decodeGraph(t, input.Checkpoint)
	runPublicID := uuid.NewString()
	graph.RunPublicID, graph.IncidentPublicID, graph.Objective = runPublicID, incident.publicID, objective
	graph.Incident.Cluster, graph.Incident.Namespace, graph.Incident.TargetKind, graph.Incident.TargetName = "kind", "default", "Deployment", "demo"
	graph.StartedAt, graph.DeadlineAt = at.Add(-time.Minute), at.Add(4*time.Minute)
	checkpoint := marshalGraph(t, graph)
	checkpointHash := rawSHA256(checkpoint)
	if !compatible {
		checkpointHash = strings.Repeat("0", 64)
	}
	completedAt := any(nil)
	if status != "PENDING" && status != "RUNNING" {
		completedAt = at.UTC()
	}
	result, err := db.ExecContext(ctx, `INSERT INTO agent_runs
(public_id,incident_id,status,objective,model,prompt_version,max_steps,used_steps,max_tool_calls,used_tool_calls,
max_model_calls,used_model_calls,token_budget,input_tokens,output_tokens,max_evidence_items,used_evidence_items,
max_runtime_ms,tool_timeout_ms,max_evidence_bytes,max_checkpoint_bytes,max_step_retries,current_checkpoint,
checkpoint_version,checkpoint_schema_version,checkpoint_hash,failure_code,started_at,completed_at,deadline_at,row_version,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'',?,?,?,2,?,?)`, runPublicID, incident.id, status, objective,
		"legacy-model", "legacy-prompt/v1", input.Limits.MaxSteps, input.Usage.Steps, input.Limits.MaxToolCalls,
		input.Usage.ToolCalls, input.Limits.MaxModelCalls, input.Usage.ModelCalls, input.Limits.TokenBudget,
		input.Usage.InputTokens, input.Usage.OutputTokens, input.Limits.MaxEvidenceItems, input.Usage.Evidence,
		input.Limits.MaxRuntime.Milliseconds(), input.Limits.ToolTimeout.Milliseconds(), input.Limits.MaxEvidenceBytes,
		input.Limits.MaxCheckpointSize, input.Limits.MaxStepRetries, checkpoint, input.CheckpointVersion,
		input.SourceSchemaVersion, checkpointHash, graph.StartedAt, completedAt, graph.DeadlineAt, graph.StartedAt, at)
	if err != nil {
		t.Fatal(err)
	}
	runIDValue, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	runID := uint64(runIDValue)
	if _, err := db.ExecContext(ctx, "UPDATE incidents SET current_agent_run_id=? WHERE id=?", runID, incident.id); err != nil {
		t.Fatal(err)
	}
	if compatible {
		evidencePublicID := uuid.NewString()
		if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items
(public_id,incident_id,agent_run_id,type,source,tool_name,resource_ref,query_text,summary,facts_json,result_hash,raw_ref,
truncated,valid,idempotency_key,collected_at,created_at)
VALUES (?,?,?,'deployment','legacy','inspect_workload','Deployment/default/demo','fixed','legacy evidence','{}',?,'',FALSE,TRUE,?,?,?)`,
			evidencePublicID, incident.id, runID, strings.Repeat("e", 64), canonicalHashFields("evidence", evidencePublicID), graph.StartedAt.Add(30*time.Second), graph.StartedAt.Add(30*time.Second)); err != nil {
			t.Fatal(err)
		}
		argumentHash := graph.Observations[0].ArgumentsHash
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_steps
(public_id,agent_run_id,sequence,step_type,short_reason,selected_tool,arguments_json,arguments_hash,result_summary,
result_ref,evidence_public_id,status,retry_count,duration_ms,input_tokens,output_tokens,error_code,started_at,finished_at,created_at)
VALUES (?,?,1,'tool','legacy','inspect_workload','{}',?,'observed','',?,'COMPLETED',0,10,0,0,'',?,?,?)`,
			uuid.NewString(), runID, argumentHash, evidencePublicID, graph.StartedAt, graph.StartedAt.Add(time.Second), graph.StartedAt); err != nil {
			t.Fatal(err)
		}
	}
	return phase7AAgentFixture{incidentID: incident.id, incidentPublicID: incident.publicID, runID: runID,
		runPublicID: runPublicID, runVersion: 2, checkpointVersion: input.CheckpointVersion}
}

type phase7APlanFixture struct{ id uint64 }
type phase7AChangeFixture struct {
	id           uint64
	repository   string
	prNumber     int64
	prURL        string
	baseRevision string
	headBranch   string
	headRevision string
	state        string
	mergedSHA    string
}

func insertPhase7APlanAndChange(t *testing.T, ctx context.Context, db *sql.DB, incident phase7AIncidentFixture,
	approval bool, status, branch, commit string, prNumber int64, prState, mergedSHA string, at time.Time) (phase7APlanFixture, phase7AChangeFixture) {
	t.Helper()
	baseRevision := strings.Repeat("a", 40)
	planHash, patchHash := strings.Repeat("3", 64), strings.Repeat("4", 64)
	planResult, err := db.ExecContext(ctx, `INSERT INTO remediation_plans
(public_id,incident_id,plan_version,plan_hash,status,operation_type,target_repository,target_base_revision,target_path,
parameters_json,evidence_references_json,risk_level,policy_snapshot_hash,expected_before_hash,proposed_patch_hash,
patch_summary,rollback_plan,validation_plan,row_version,created_at,updated_at)
VALUES (?,?,1,?,'awaiting_approval','rollback_image','acme/app',?,'deploy.yaml','{}','[]','high',?,?,?,
'legacy plan','manual','verify',2,?,?)`, uuid.NewString(), incident.id, planHash, baseRevision,
		strings.Repeat("5", 64), strings.Repeat("6", 64), patchHash, at, at)
	if err != nil {
		t.Fatal(err)
	}
	planIDValue, _ := planResult.LastInsertId()
	planID := uint64(planIDValue)
	if approval {
		if _, err := db.ExecContext(ctx, `INSERT INTO remediation_approvals
(public_id,plan_id,decision,actor,approved_plan_hash,approved_patch_hash,created_at)
VALUES (?,?,'approved','legacy-operator',?,?,?)`, uuid.NewString(), planID, planHash, patchHash, at); err != nil {
			t.Fatal(err)
		}
	}
	prURL := ""
	if prNumber > 0 {
		prURL = fmt.Sprintf("https://github.com/acme/app/pull/%d", prNumber)
	}
	changeResult, err := db.ExecContext(ctx, `INSERT INTO change_requests
(public_id,plan_id,repository,base_revision,head_branch,commit_sha,pr_number,pr_url,pr_state,merged_commit_sha,
status,ci_status,idempotency_key,attempts,failure_code,row_version,created_at,updated_at)
VALUES (? ,?,'acme/app',?,?,?,?,?,?,?,?,'pending',?,0,'',2,?,?)`, uuid.NewString(), planID, baseRevision,
		branch, commit, prNumber, prURL, prState, mergedSHA, status, canonicalHashFields("change", fmt.Sprint(planID)), at, at)
	if err != nil {
		t.Fatal(err)
	}
	changeIDValue, _ := changeResult.LastInsertId()
	return phase7APlanFixture{id: planID}, phase7AChangeFixture{id: uint64(changeIDValue), repository: "acme/app",
		prNumber: prNumber, prURL: prURL, baseRevision: baseRevision, headBranch: branch, headRevision: commit,
		state: prState, mergedSHA: mergedSHA}
}

func reconciledPhase7APR(change phase7AChangeFixture, merged bool, mergedSHA string) ReconciledPullRequest {
	state := change.state
	if merged {
		state = "merged"
	}
	return ReconciledPullRequest{Repository: change.repository, PullRequest: change.prNumber, URL: change.prURL,
		BaseRevision: change.baseRevision, HeadBranch: change.headBranch, HeadRevision: change.headRevision,
		State: state, Merged: merged, MergedCommitSHA: mergedSHA}
}

func insertPhase7AVerification(t *testing.T, ctx context.Context, db *sql.DB, incident phase7AIncidentFixture,
	planID, changeID uint64, pullRequest int64, status verification.RunStatus, passing, loki bool, at time.Time) uint64 {
	t.Helper()
	target, source, gitops := strings.Repeat("7", 40), strings.Repeat("8", 40), strings.Repeat("9", 40)
	digest := "sha256:" + strings.Repeat("a", 64)
	plan, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{TriggerType: "post_delivery",
		Repository: "acme/app", PullRequest: pullRequest, TargetRevision: target, SourceRevision: source,
		ImageDigest: digest, GitOpsRevision: gitops, ArgoApplication: "demo", ArgoProject: "default",
		Cluster: "kind", Environment: "test", Namespace: "default", Service: "demo", WorkloadName: "demo",
		AlertNames: []string{"RequiredEnvMissing"}})
	if err != nil {
		t.Fatal(err)
	}
	if loki {
		plan.Checks[0].SourceIdentity = "loki/legacy"
	}
	planJSON := mustPhase7AJSON(t, plan)
	completed := any(nil)
	if status != verification.RunPending && status != verification.RunRunning {
		completed = at.Add(2 * time.Minute)
	}
	result, err := db.ExecContext(ctx, `INSERT INTO verification_runs
(public_id,incident_id,remediation_plan_id,change_request_id,status,target_revision,plan_json,started_at,deadline_at,
completed_at,attempt,row_version,result_summary,failure_reason,created_at,updated_at)
VALUES (?,?,?,?,?,?,?, ?,?,?,1,2,'','',?,?)`, uuid.NewString(), incident.id, planID, changeID, status,
		plan.TargetRevision, planJSON, at, at.Add(10*time.Minute), completed, at, at)
	if err != nil {
		t.Fatal(err)
	}
	runIDValue, _ := result.LastInsertId()
	runID := uint64(runIDValue)
	commonStart := at
	commonEnd := at.Add(60 * time.Second)
	for _, spec := range plan.Checks {
		checkStatus := verification.CheckPending
		var observed, firstChecked, lastChecked, passedAt, successSince any
		if passing {
			checkStatus = verification.CheckPassed
			observed = mustPhase7AJSON(t, verification.Observation{Status: verification.ObservationAvailable,
				SampleCount: spec.MinSamples, SampledAt: commonEnd, QueryValid: true, SourceHealthy: true, RetentionCovered: true})
			firstChecked, lastChecked, passedAt, successSince = commonStart, commonEnd, commonEnd, commonStart
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO verification_checks
(public_id,verification_run_id,check_type,status,required_check,subject_json,expected_json,observed_json,source_reference,
lookback_ms,stability_window_ms,timeout_ms,poll_interval_ms,first_checked_at,last_checked_at,passed_at,
consecutive_success_since,attempt_count,failure_reason,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?, '',?,?,?,?,?,?,?, ?,1,'',?,?)`, uuid.NewString(), runID, spec.Type, checkStatus,
			spec.Required, mustPhase7AJSON(t, spec.Subject), []byte(spec.Expected), observed, spec.Lookback.Milliseconds(),
			spec.StabilityWindow.Milliseconds(), spec.Timeout.Milliseconds(), spec.PollInterval.Milliseconds(),
			firstChecked, lastChecked, passedAt, successSince, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return runID
}

func insertPhase7APostmortem(t *testing.T, ctx context.Context, db *sql.DB, incidentID, verificationRunID uint64, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO postmortems
(public_id,incident_id,verification_run_id,title,impact_summary,detected_at,mitigated_at,resolved_at,duration_seconds,
service,workload,environment,triggering_signal_json,change_correlation_json,root_cause_json,remediation_summary_json,
approval_summary_json,delivery_revision,verification_summary,checks_json,timeline_json,follow_up_actions_json,
generated_at,generation_version,created_at,updated_at)
VALUES (?,?,?,'legacy postmortem','legacy impact',?,?,?,120,'demo','demo','test','{}','{}','{}','{}','{}',?,
'legacy verification','[]','[]','[]',?,1,?,?)`, uuid.NewString(), incidentID, verificationRunID,
		at.Add(-2*time.Minute), at.Add(-time.Minute), at, strings.Repeat("2", 40), at, at, at); err != nil {
		t.Fatal(err)
	}
}

func insertPhase7ABackfillFacts(t *testing.T, ctx context.Context, db *sql.DB, incident phase7AIncidentFixture, runID uint64, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO incident_signals
(incident_id,source,source_event_id,fingerprint,status,severity,cluster,namespace,service_name,environment,target_kind,
target_name,category,occurred_at,received_at,summary,labels_json,annotations_json,raw_payload,created_at)
VALUES (?,'alertmanager',?,'legacy-fingerprint','firing','critical','kind','default','demo','test','Deployment','demo',
'availability',?,?,'legacy signal','{}','{}','{}',?)`, incident.id, uuid.NewString(), at, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO incident_events
(incident_id,event_type,actor_type,actor_id,summary,metadata_json,occurred_at,created_at)
VALUES (?,'incident_created','system','legacy','legacy event','{}',?,?)`, incident.id, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO changes
(public_id,incident_id,source_type,repository,commit_sha,status,category,correlation_reasons_json,metadata_json,
idempotency_key,created_at,updated_at)
VALUES (?,?,'github_commit','acme/app',?,'candidate','low_confidence','[]','{}',?,?,?)`, uuid.NewString(),
		incident.id, strings.Repeat("b", 40), canonicalHashFields("legacy-change-fact", fmt.Sprint(runID)), at, at); err != nil {
		t.Fatal(err)
	}
}

func insertExistingNativeAgentTask(t *testing.T, ctx context.Context, db *sql.DB, run phase7AAgentFixture) uint64 {
	t.Helper()
	payload, err := canonicalTaskPayload(map[string]any{"mode": "decide", "agent_run_id": run.runPublicID,
		"cycle_no": 1, "basis_checkpoint_version": run.checkpointVersion})
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.ExecContext(ctx, `INSERT INTO async_tasks
(public_id,incident_id,cycle_no,queue,task_type,subject_type,subject_id,transition,expected_subject_version,
payload_schema_version,payload_json,dedupe_key,replay_generation,logical_operation_key,migrated_legacy,migrated_legacy_context,
status,priority,available_at,attempt,max_attempts,lease_generation,created_at,updated_at)
VALUES (?,?,1,'investigate','investigation.advance','agent_run',?,'investigation.step',?,1,?,?,0,NULL,FALSE,FALSE,
'ready',50,UTC_TIMESTAMP(6),0,8,0,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, uuid.NewString(), run.incidentID,
		run.runID, run.runVersion+1, []byte(payload), canonicalHashFields("native-existing-task", fmt.Sprint(run.runID)))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}

func assertBackfillRetryChain(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT attempt,status,previous_ledger_id FROM migration_ledger
WHERE operation=? AND batch_no=1 ORDER BY attempt`, BackfillOperationPrefix+"incident-signals")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type attemptRow struct {
		attempt  uint64
		status   string
		previous sql.NullInt64
	}
	var attempts []attemptRow
	for rows.Next() {
		var row attemptRow
		if err := rows.Scan(&row.attempt, &row.status, &row.previous); err != nil {
			t.Fatal(err)
		}
		attempts = append(attempts, row)
	}
	if len(attempts) != 2 || attempts[0].attempt != 1 || attempts[0].status != "failed" ||
		attempts[1].attempt != 2 || attempts[1].status != "passed" || !attempts[1].previous.Valid {
		t.Fatalf("BACKFILL-V3 retry chain=%+v", attempts)
	}
}

func assertPhase7APrepareReport(t *testing.T, report PrepareReport) {
	t.Helper()
	if report.SourceSchemaVersion != uint64(schemaversion.Latest) || report.TargetSchemaVersion != uint64(schemaversion.Latest) ||
		report.QuiesceLedgerPublicID == "" || report.ReconciliationLedgerPublicID == "" || report.ConverterAuditLedgerPublicID == "" {
		t.Fatalf("incomplete Phase 7A report: %+v", report)
	}
	for _, key := range []string{"outbox_archived_published", "outbox_archived_unpublished", "subject_derived",
		"task_created", "existing_target_task", "anti_join_skipped", "migrated_legacy_evidence",
		"migrated_legacy_context_tasks"} {
		if _, ok := report.Counts[key]; !ok {
			t.Fatalf("Phase 7A report omitted count %s", key)
		}
	}
}

func assertPhase7AConvertedFixture(t *testing.T, ctx context.Context, db *sql.DB, fixture phase7ALegacyFixture) {
	t.Helper()
	var nativeID uint64
	var nativeMigrated, nativeContext bool
	if err := db.QueryRowContext(ctx, `SELECT id,migrated_legacy,migrated_legacy_context FROM async_tasks
WHERE subject_type='agent_run' AND subject_id=? AND transition='investigation.step'`, fixture.compatibleAgent.runID).Scan(
		&nativeID, &nativeMigrated, &nativeContext); err != nil {
		t.Fatal(err)
	}
	if nativeID != fixture.nativeTaskID || nativeMigrated || nativeContext {
		t.Fatalf("existing native Task was rewritten id=%d migrated=%t context=%t", nativeID, nativeMigrated, nativeContext)
	}
	var fallbackTasks uint64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_tasks WHERE incident_id=? AND subject_type='incident'
AND subject_id=? AND transition='investigation.start'`, fixture.fallbackIncidentID, fixture.fallbackIncidentID).Scan(&fallbackTasks); err != nil {
		t.Fatal(err)
	}
	if fallbackTasks != 1 {
		t.Fatalf("multiple incompatible children created %d fallback Tasks", fallbackTasks)
	}
	var fallbackMigrated, fallbackContext bool
	if err := db.QueryRowContext(ctx, `SELECT migrated_legacy,migrated_legacy_context FROM async_tasks
WHERE incident_id=? AND transition='investigation.start'`, fixture.fallbackIncidentID).Scan(&fallbackMigrated, &fallbackContext); err != nil {
		t.Fatal(err)
	}
	if fallbackMigrated || !fallbackContext {
		t.Fatalf("fallback Task provenance migrated=%t context=%t", fallbackMigrated, fallbackContext)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type='agent_run' AND subject_id=?`, fixture.terminalAgent.runID); count != 0 {
		t.Fatalf("terminal AgentRun created %d Tasks", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type='verification_run' AND subject_id=?`, fixture.terminalVerification); count != 0 {
		t.Fatalf("terminal VerificationRun created %d Tasks", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type='verification_run' AND subject_id=? AND task_type='verification.advance'`, fixture.compatibleVerification); count != 1 {
		t.Fatalf("compatible active VerificationRun tasks=%d", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type='change_request' AND subject_id=? AND task_type='delivery.observe'`, fixture.openChangeRequest); count != 1 {
		t.Fatalf("complete Draft PR observe tasks=%d", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE task_type='change.ensure_pr'`); count != 0 {
		t.Fatalf("legacy Approval produced %d write Tasks", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM legacy_outbox_archive WHERE publication_state='published'`); count != 1 {
		t.Fatalf("published outbox archive count=%d", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM legacy_outbox_archive WHERE publication_state='unpublished'`); count != 1 {
		t.Fatalf("unpublished outbox archive count=%d", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM migration_ledger WHERE operation LIKE '%outbox-derived-task%'`); count != 0 {
		t.Fatalf("found %d forbidden outbox-derived-task ledger rows", count)
	}
	if count := phase7ATestCount(t, ctx, db, `SELECT COUNT(*) FROM verification_samples WHERE verification_run_id=? AND migrated_legacy=TRUE`, fixture.terminalVerification); count == 0 {
		t.Fatal("compatible passing Verification did not create migrated sample projections")
	}
	var resolved, unverified uint64
	if err := db.QueryRowContext(ctx, `SELECT
COALESCE(SUM(v3_status='resolved'),0),COALESCE(SUM(v3_status='investigating' AND blocking_reason_code='legacy_resolution_unverified'),0)
FROM incidents WHERE migrated_legacy=TRUE`).Scan(&resolved, &unverified); err != nil {
		t.Fatal(err)
	}
	if resolved != 1 || unverified == 0 {
		t.Fatalf("RESOLVED mapping resolved=%d unverified=%d", resolved, unverified)
	}
	var failedAttempts uint64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_conversion_records
WHERE subject_type IN ('agent_run','verification_run') AND incident_id=? AND attempt>1 AND previous_conversion_id IS NOT NULL`, fixture.fallbackIncidentID).Scan(&failedAttempts); err != nil {
		t.Fatal(err)
	}
	if failedAttempts != 2 {
		t.Fatalf("fallback conversion retry records=%d want=2", failedAttempts)
	}
}

func assertConcurrentPhase7APrepare(t *testing.T, ctx context.Context, db *sql.DB, request PrepareRequest,
	reconciler phase7AFixtureReconciler, expected PrepareReport) {
	t.Helper()
	start := make(chan struct{})
	reports := make([]PrepareReport, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range reports {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			preparer, err := NewPhase7APreparer(db, 5*time.Second)
			if err != nil {
				errs[index] = err
				return
			}
			preparer.WithLegacyChangeReconciler(reconciler)
			<-start
			reports[index], errs[index] = preparer.Prepare(ctx, request)
		}(index)
	}
	close(start)
	wait.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent prepare %d: %v", index, err)
		}
		if reports[index].ConverterAuditLedgerPublicID != expected.ConverterAuditLedgerPublicID {
			t.Fatalf("concurrent prepare %d returned different ledger IDs", index)
		}
	}
}

func phase7ATestPrepareRequest() PrepareRequest {
	return PrepareRequest{PlanVersion: 7, SourceExactSHA: strings.Repeat("a", 40),
		BinaryImageDigest: "sha256:" + strings.Repeat("b", 64), BackfillBatchSize: 1,
		ObservedIngressWriters: 0, ObservedMutationWriters: 0, ObservedLegacyWorkers: 0,
		ObservedUnknownExternalWrite: 0}
}

func exportPhase7ATestAudit(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var output bytes.Buffer
	if err := ExportAudit(ctx, db, &output); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(output.Bytes()) {
		t.Fatalf("invalid audit JSON: %s", output.String())
	}
	return output.String()
}

func newPhase7ATestProvider(t *testing.T, db *sql.DB) *goose.Provider {
	t.Helper()
	provider, err := goose.NewProvider(goose.DialectMySQL, db, rootmigrations.FS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func openPhase7ATestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func phase7ATestDatabaseDSN(t *testing.T, adminDSN, database string) string {
	t.Helper()
	config, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName, config.ParseTime, config.MultiStatements = database, true, true
	return config.FormatDSN()
}

func createPhase7ATestDatabase(t *testing.T, ctx context.Context, admin *sql.DB, name string) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
}

func dropPhase7ATestDatabase(t *testing.T, admin *sql.DB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
		t.Errorf("drop disposable Phase 7A database %s: %v", name, err)
	}
}

func assertPhase7ASchemaVersion(t *testing.T, ctx context.Context, db *sql.DB, expected uint64) {
	t.Helper()
	var version uint64
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != expected {
		t.Fatalf("schema version=%d want=%d", version, expected)
	}
}

func phase7ATestCount(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) uint64 {
	t.Helper()
	var count uint64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func mustPhase7AJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

var _ LegacyChangeReconciler = phase7AFixtureReconciler{}
