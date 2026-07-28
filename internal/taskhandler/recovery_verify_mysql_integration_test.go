package taskhandler_test

import (
	"context"
	"crypto/sha256"
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

	"github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/command"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/recoveryverificationread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestMySQLOperationalRecoveryLifecycleFromAlertsToOwnerClose(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openOperationalRecoveryIntegrationDB(t, adminDSN)
	defer closeOperationalRecoveryIntegrationDB(t, "operational recovery admin", admin)
	databaseName := fmt.Sprintf("cloudops_operational_recovery_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("CLOUDOPS_TEST_KEEP_OPERATIONAL_RECOVERY_DATABASE")), "true") {
			t.Logf("retained operational recovery database %s", databaseName)
			return
		}
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if _, err := admin.ExecContext(dropCtx, "DROP DATABASE IF EXISTS `"+databaseName+"`"); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	}()
	db := openOperationalRecoveryIntegrationDB(t, operationalRecoveryDatabaseDSN(t, adminDSN, databaseName))
	defer closeOperationalRecoveryIntegrationDB(t, "operational recovery", db)
	runner, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}

	alerts, err := alert.NewService(db, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewPort(db)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	query, err := api.NewMySQLQueryPort(db)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	correlationKey := strings.Repeat("c", 64)
	firstSignal := operationalRecoverySignal("1", "a", correlationKey, domain.SignalStatusFiring, now.Add(-5*time.Minute), now.Add(-5*time.Minute), nil)
	secondSignal := operationalRecoverySignal("2", "b", correlationKey, domain.SignalStatusFiring, now.Add(-4*time.Minute), now.Add(-4*time.Minute), nil)
	first := ingestOneOperationalRecoveryAlert(t, ctx, alerts, firstSignal)
	firstView, err := alerts.LinkIncident(ctx, alert.LinkIncidentRequest{
		AlertID: first.AlertPublicID, ExpectedVersion: 1, IdempotencyKey: operationalRecoveryHash("operational-recovery-create-incident"),
		Create: true, Actor: operationalRecoveryAlertOwner(),
	})
	if err != nil || len(firstView.IncidentLinks) != 1 || firstView.IncidentLinks[0].Provenance != "owner_created" {
		t.Fatalf("create Incident link view=%+v err=%v", firstView, err)
	}
	incidentPublicID := firstView.IncidentLinks[0].IncidentID
	second := ingestOneOperationalRecoveryAlert(t, ctx, alerts, secondSignal)
	secondView, err := alerts.LinkIncident(ctx, alert.LinkIncidentRequest{
		AlertID: second.AlertPublicID, ExpectedVersion: 1, IdempotencyKey: operationalRecoveryHash("operational-recovery-attach-second-alert"),
		IncidentID: incidentPublicID, Actor: operationalRecoveryAlertOwner(),
	})
	if err != nil || len(secondView.IncidentLinks) != 1 || secondView.IncidentLinks[0].Provenance != "owner_attached" {
		t.Fatalf("attach second Alert view=%+v err=%v", secondView, err)
	}
	var observationStart, observationEnd time.Time
	if err := db.QueryRowContext(ctx, "SELECT first_seen_at, last_seen_at FROM incidents WHERE public_id = ?", incidentPublicID).
		Scan(&observationStart, &observationEnd); err != nil {
		t.Fatal(err)
	}
	if !observationStart.UTC().Equal(firstSignal.OccurredAt) || !observationEnd.UTC().Equal(secondSignal.OccurredAt) {
		t.Fatalf("Incident observation window=%s..%s want=%s..%s", observationStart, observationEnd, firstSignal.OccurredAt, secondSignal.OccurredAt)
	}

	scenarioSignal := operationalRecoverySignal("3", "f", correlationKey, domain.SignalStatusFiring, now.Add(-3*time.Minute), now.Add(-3*time.Minute), nil)
	scenarioSignal.Labels = json.RawMessage(`{"service":"checkout","scenario_id":"scenario-incident-isolation"}`)
	scenarioAlert := ingestOneOperationalRecoveryAlert(t, ctx, alerts, scenarioSignal)
	_, err = alerts.LinkIncident(ctx, alert.LinkIncidentRequest{
		AlertID: scenarioAlert.AlertPublicID, ExpectedVersion: 1, IdempotencyKey: operationalRecoveryHash("operational-recovery-reject-cross-scenario-attach"),
		IncidentID: incidentPublicID, Actor: operationalRecoveryAlertOwner(),
	})
	if !errors.Is(err, alert.ErrConflict) {
		t.Fatalf("cross-Scenario Incident attach error=%v want=%v", err, alert.ErrConflict)
	}
	scenarioView, err := alerts.LinkIncident(ctx, alert.LinkIncidentRequest{
		AlertID: scenarioAlert.AlertPublicID, ExpectedVersion: 1, IdempotencyKey: operationalRecoveryHash("operational-recovery-create-scenario-incident"),
		Create: true, Actor: operationalRecoveryAlertOwner(),
	})
	if err != nil || len(scenarioView.IncidentLinks) != 1 || scenarioView.IncidentLinks[0].IncidentID == incidentPublicID {
		t.Fatalf("Scenario Incident isolation view=%+v err=%v", scenarioView, err)
	}
	scenarioIncidentPublicID := scenarioView.IncidentLinks[0].IncidentID
	repeatedScenarioSignal := operationalRecoverySignal("4", "9", correlationKey, domain.SignalStatusFiring, now.Add(-2*time.Minute), now.Add(-2*time.Minute), nil)
	repeatedScenarioSignal.Labels = json.RawMessage(`{"service":"checkout","scenario_id":"scenario-incident-isolation"}`)
	repeatedScenarioAlert := ingestOneOperationalRecoveryAlert(t, ctx, alerts, repeatedScenarioSignal)
	repeatedScenarioView, err := alerts.LinkIncident(ctx, alert.LinkIncidentRequest{
		AlertID: repeatedScenarioAlert.AlertPublicID, ExpectedVersion: 1, IdempotencyKey: operationalRecoveryHash("operational-recovery-reuse-scenario-incident"),
		Create: true, Actor: operationalRecoveryAlertOwner(),
	})
	if err != nil || len(repeatedScenarioView.IncidentLinks) != 1 || repeatedScenarioView.IncidentLinks[0].IncidentID != scenarioIncidentPublicID {
		t.Fatalf("same Scenario Incident reuse view=%+v err=%v", repeatedScenarioView, err)
	}

	var incidentID uint64
	if err := db.QueryRowContext(ctx, "SELECT id FROM incidents WHERE public_id = ?", incidentPublicID).Scan(&incidentID); err != nil {
		t.Fatal(err)
	}
	startBody := json.RawMessage(`{"expected_version":1,"reason":"investigate correlated checkout alerts"}`)
	started, err := commands.Execute(ctx, operationalRecoveryCommand(api.CommandStartInvestigation, incidentPublicID, 1, "operational-recovery-start-investigation", startBody))
	if err != nil || started.Status != "investigating" || started.Version != 2 {
		t.Fatalf("start Investigation=%+v err=%v", started, err)
	}
	investigationID, investigationPublicID, evidencePublicID := completeOperationalRecoveryInvestigation(t, ctx, db, incidentID)

	// No Alert, Investigation, or Delivery-side fact can create a resolved
	// projection before a passing Verification and its immutable report.
	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM incidents WHERE id = ?", "investigating", incidentID)
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM resolution_reports WHERE incident_id = ?", 0, incidentID)

	decisionBody := json.RawMessage(`{"decision":"verify_recovery","expected_version":2,"reason":"terminal Investigation supports native recovery verification"}`)
	decided, err := commands.Execute(ctx, operationalRecoveryCommand(api.CommandDecideRecovery, incidentPublicID, 2, "operational-recovery-decide-recovery", decisionBody))
	if err != nil || decided.Status != "verifying" || decided.Version != 3 {
		t.Fatalf("recovery decision=%+v err=%v", decided, err)
	}
	var configurationPublicID, scopePublicID string
	if err := db.QueryRowContext(ctx, `SELECT revision.public_id, scope.public_id
FROM active_configuration active
JOIN configuration_revisions revision ON revision.id = active.configuration_revision_id
JOIN operational_scopes scope ON scope.configuration_revision_id = active.configuration_revision_id
WHERE active.singleton_id = 1 AND scope.cluster_id = 'cloudops-local'`).Scan(&configurationPublicID, &scopePublicID); err != nil {
		t.Fatal(err)
	}

	topology := &operationalRecoveryTopology{now: func() time.Time { return time.Now().UTC() }}
	source, err := recoveryverificationread.New(recoveryverificationread.Config{
		DB: db, Kubernetes: topology, Now: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := taskhandler.NewMySQLRecoveryVerify(taskhandler.MySQLVerificationAdvanceConfig{
		DB: db, Tasks: tasks, Observations: source, Reports: taskhandler.NewMySQLResolutionReportWriter(),
	})
	if err != nil {
		t.Fatal(err)
	}

	firstAttempt := claimAndRunOperationalRecovery(t, ctx, tasks, operation, "operational-recovery-recovery-failure")
	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM verification_runs WHERE id = ?", "failed", firstAttempt.Task.SubjectID)
	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM incidents WHERE id = ?", "investigating", incidentID)
	assertOperationalRecoveryCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
	WHERE incident_id = ? AND cycle_no = 1 AND event_type = 'verification_failed'
	  AND source_status = 'verifying' AND target_status = 'investigating'`, 1, incidentID)
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM resolution_reports WHERE incident_id = ?", 0, incidentID)
	if topology.calls != 0 {
		t.Fatalf("immediate firing-Alert failure performed %d Kubernetes reads", topology.calls)
	}

	firstResolvedAt := now.Add(-time.Minute)
	firstResolved := operationalRecoverySignal("1", "d", correlationKey, domain.SignalStatusResolved, firstSignal.StartsAt, firstResolvedAt, &firstResolvedAt)
	ingestOneOperationalRecoveryAlert(t, ctx, alerts, firstResolved)
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM verification_runs WHERE incident_id = ? AND trigger_type = 'operational_recovery'", 1, incidentID)
	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM incidents WHERE id = ?", "investigating", incidentID)

	secondResolvedAt := now.Add(-30 * time.Second)
	secondResolved := operationalRecoverySignal("2", "e", correlationKey, domain.SignalStatusResolved, secondSignal.StartsAt, secondResolvedAt, &secondResolvedAt)
	ingestOneOperationalRecoveryAlert(t, ctx, alerts, secondResolved)
	if err := db.QueryRowContext(ctx, "SELECT first_seen_at, last_seen_at FROM incidents WHERE public_id = ?", incidentPublicID).
		Scan(&observationStart, &observationEnd); err != nil {
		t.Fatal(err)
	}
	if !observationStart.UTC().Equal(firstSignal.OccurredAt) || !observationEnd.UTC().Equal(secondResolvedAt) {
		t.Fatalf("resolved Incident observation window=%s..%s want=%s..%s", observationStart, observationEnd, firstSignal.OccurredAt, secondResolvedAt)
	}
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM verification_runs WHERE incident_id = ? AND trigger_type = 'operational_recovery'", 2, incidentID)
	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM incidents WHERE id = ?", "verifying", incidentID)

	secondAttemptFirstCheck := claimAndRunOperationalRecovery(t, ctx, tasks, operation, "operational-recovery-recovery-pass-alerts")
	secondRunID := secondAttemptFirstCheck.Task.SubjectID
	claimAndRunOperationalRecovery(t, ctx, tasks, operation, "operational-recovery-recovery-pass-workload")
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM verification_checks WHERE verification_run_id = ? AND status = 'running'", 2, secondRunID)
	if topology.calls != 1 {
		t.Fatalf("healthy recovery performed %d Kubernetes reads before common window, want 1", topology.calls)
	}
	if _, err := db.ExecContext(ctx, `UPDATE verification_checks
	SET created_at = DATE_SUB(NOW(6), INTERVAL 70 SECOND),
	    first_checked_at = DATE_SUB(NOW(6), INTERVAL 61 SECOND),
	    consecutive_success_since = DATE_SUB(NOW(6), INTERVAL 61 SECOND),
	    last_checked_at = DATE_SUB(NOW(6), INTERVAL 6 SECOND)
	WHERE verification_run_id = ? AND status = 'running'`, secondRunID); err != nil {
		t.Fatal(err)
	}
	claimAndRunOperationalRecovery(t, ctx, tasks, operation, "operational-recovery-recovery-common-window")

	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM verification_runs WHERE id = ?", "passed", secondRunID)
	assertOperationalRecoveryValue(t, ctx, db, "SELECT status FROM incidents WHERE id = ?", "resolved", incidentID)
	assertOperationalRecoveryCount(t, ctx, db, `SELECT COUNT(*) FROM resolution_reports
	WHERE incident_id = ? AND verification_run_id = ? AND trigger_type = 'operational_recovery'
	  AND configuration_revision_id IS NOT NULL AND operational_scope_id IS NOT NULL
	  AND investigation_run_id = ? AND decision_event_id IS NOT NULL
	  AND initial_signal_id IS NULL AND trigger_signal_id IS NULL
	  AND remediation_plan_id IS NULL AND remediation_decision_id IS NULL AND change_request_id IS NULL
	  AND source_revision IS NULL AND image_digest IS NULL AND gitops_revision IS NULL
	  AND remediation_plan_json IS NULL AND delivery_json IS NULL
	  AND diagnosis_json IS NOT NULL AND remediation_decision_json IS NOT NULL
	  AND TIMESTAMPDIFF(MICROSECOND, common_window_started_at, common_window_completed_at) >= 60000000`, 1,
		incidentID, secondRunID, investigationID)
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM evidence_items WHERE public_id = ? AND evidence_contract_version = 1 AND producer_type = 'agent_step'", 1, evidencePublicID)

	incidentProjection, err := query.Query(ctx, api.QueryRequest{Kind: api.QueryIncident, IncidentID: incidentPublicID})
	if err != nil || incidentProjection.Incident == nil {
		t.Fatalf("resolved Incident projection=%+v err=%v", incidentProjection.Incident, err)
	}
	incidentView := incidentProjection.Incident
	if incidentView.Status != "resolved" || incidentView.RelatedAlertCount != 2 ||
		incidentView.Recovery.State != "recovered" || incidentView.Recovery.VerificationAttempts != 2 ||
		incidentView.Recovery.FailedVerificationCount != 1 || !incidentView.Recovery.CanClose ||
		incidentView.Recovery.ResolutionReportID == "" || incidentView.Decision == nil ||
		incidentView.Decision.Kind != "recovery" || incidentView.Decision.InvestigationID != investigationPublicID {
		t.Fatalf("resolved Incident projection=%+v", incidentView)
	}
	verificationProjection, err := query.Query(ctx, api.QueryRequest{
		Kind: api.QueryVerifications, IncidentID: incidentPublicID, Limit: 10,
	})
	if err != nil || len(verificationProjection.Verifications) != 2 {
		t.Fatalf("operational Verification projection=%+v err=%v", verificationProjection.Verifications, err)
	}
	passingVerification := verificationProjection.Verifications[0]
	failedVerification := verificationProjection.Verifications[1]
	if passingVerification.ID != incidentView.Recovery.LatestVerificationID || passingVerification.Status != "passed" ||
		passingVerification.Attempt != 2 || passingVerification.Profile.ID != verification.OperationalRecoveryProfileID ||
		passingVerification.CommonWindow.SuccessSince == nil || passingVerification.CommonWindow.CompletedAt == nil ||
		len(passingVerification.Checks) != 2 || failedVerification.Status != "failed" || failedVerification.Attempt != 1 ||
		len(failedVerification.Checks) != 2 {
		t.Fatalf("operational Verification attempts=%+v", verificationProjection.Verifications)
	}
	reportProjection, err := query.Query(ctx, api.QueryRequest{Kind: api.QueryResolutionReport, IncidentID: incidentPublicID})
	if err != nil || reportProjection.ResolutionReport == nil {
		t.Fatalf("operational ResolutionReport projection=%+v err=%v", reportProjection.ResolutionReport, err)
	}
	report := reportProjection.ResolutionReport
	if report.TriggerType != "operational_recovery" || report.Status != "resolved" ||
		report.VerificationProfile.ID != verification.OperationalRecoveryProfileID ||
		report.Revisions != (api.ResolutionRevisionsView{}) || len(report.Delivery) != 0 || len(report.RemediationPlan) != 0 ||
		report.RecoveryProvenance == nil || report.RecoveryProvenance.ConfigurationRevisionID != configurationPublicID ||
		report.RecoveryProvenance.OperationalScopeID != scopePublicID ||
		report.RecoveryProvenance.InvestigationID != investigationPublicID || report.RecoveryProvenance.DecisionID == "" {
		t.Fatalf("operational ResolutionReport=%+v", report)
	}

	closeBody := json.RawMessage(fmt.Sprintf(`{"expected_version":%d,"reason":"recovery report reviewed"}`, incidentView.Version))
	closed, err := commands.Execute(ctx, operationalRecoveryCommand(api.CommandCloseIncident, incidentPublicID, incidentView.Version, "operational-recovery-close", closeBody))
	if err != nil || closed.Status != "closed" || closed.Version != incidentView.Version+1 {
		t.Fatalf("close Incident=%+v err=%v", closed, err)
	}
	assertOperationalRecoveryCount(t, ctx, db, `SELECT COUNT(*) FROM incidents incident
	JOIN resolution_reports report ON report.incident_id = incident.id AND report.cycle_no = incident.cycle_no
	JOIN verification_runs run ON run.id = report.verification_run_id
	WHERE incident.id = ? AND incident.status = 'closed' AND incident.resolved_at IS NOT NULL
	  AND run.status = 'passed' AND report.public_id = ?`, 1, incidentID, incidentView.Recovery.ResolutionReportID)
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM alert_incident_links WHERE incident_id = ? AND incident_cycle_no = 1", 2, incidentID)
	assertOperationalRecoveryCount(t, ctx, db, "SELECT COUNT(*) FROM evidence_items WHERE incident_id = ? AND cycle_no = 1", 2, incidentID)
}

func operationalRecoverySignal(alertHex, eventHex, correlationKey string, status domain.SignalStatus, startsAt, occurredAt time.Time, endsAt *time.Time) alert.SignalInput {
	return alert.SignalInput{
		Source: "alertmanager", SourceEventID: strings.Repeat(eventHex, 64),
		AlertInstanceKey: strings.Repeat(alertHex, 64), CorrelationKey: correlationKey,
		Fingerprint: strings.Repeat(alertHex, 64), Status: status, Severity: domain.SeverityCritical,
		Cluster: "cloudops-local", Environment: "local", Namespace: "demo", ServiceName: "checkout",
		TargetKind: "Deployment", TargetName: "checkout", Category: "availability",
		StartsAt: startsAt, EndsAt: endsAt, OccurredAt: occurredAt,
		Summary: "checkout recovery lifecycle alert " + alertHex, Labels: json.RawMessage(`{"service":"checkout"}`),
		Annotations: json.RawMessage(`{"runbook":"internal"}`),
	}
}

func ingestOneOperationalRecoveryAlert(t *testing.T, ctx context.Context, service *alert.Service, signal alert.SignalInput) alert.IngestResult {
	t.Helper()
	results, err := service.IngestBatch(ctx, []alert.SignalInput{signal})
	if err != nil || len(results) != 1 || results[0].Rejected || results[0].Duplicate || results[0].AlertPublicID == "" {
		t.Fatalf("Alert ingest results=%+v err=%v", results, err)
	}
	return results[0]
}

func operationalRecoveryAlertOwner() alert.Actor {
	return alert.Actor{Provider: "local", Login: "owner", Role: "owner"}
}

func operationalRecoveryCommand(kind api.CommandKind, resourceID string, expectedVersion uint64, idempotencyKey string, body json.RawMessage) api.CommandRequest {
	return api.CommandRequest{
		Kind: kind, ResourceID: resourceID, ExpectedVersion: expectedVersion,
		Actor:          api.OwnerIdentity{Subject: "local-owner", Provider: "local", Login: "owner", Role: "owner"},
		IdempotencyKey: idempotencyKey, CanonicalBody: body, RequestID: uuid.NewString(), TraceID: uuid.NewString(),
	}
}

func completeOperationalRecoveryInvestigation(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64) (uint64, string, string) {
	t.Helper()
	var runID uint64
	var runPublicID string
	if err := db.QueryRowContext(ctx, `SELECT id, public_id FROM agent_runs
	WHERE incident_id = ? AND cycle_no = 1 AND subject_type = 'incident' AND run_kind = 'workspace'
	ORDER BY id DESC LIMIT 1`, incidentID).Scan(&runID, &runPublicID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	stepPublicID := uuid.NewString()
	evidencePublicID := uuid.NewString()
	stepResult, err := db.ExecContext(ctx, `INSERT INTO agent_steps
	(public_id, agent_run_id, incident_id, cycle_no, sequence, step_type, short_reason,
	 selected_tool, arguments_json, arguments_hash, result_summary, result_ref,
	 evidence_public_id, status, retry_count, duration_ms, input_tokens, output_tokens,
	 error_code, started_at, finished_at, created_at)
	VALUES (?, ?, ?, 1, 1, 'execute_tool', 'inspect current workload and related Alerts',
	        'workspace.recovery_read', JSON_OBJECT('scope','current_cycle'), ?,
	        'workload is ready but both related Alerts remain firing', ?, ?, 'completed',
	        0, 20, 0, 0, '', ?, ?, ?)`, stepPublicID, runID, incidentID,
		strings.Repeat("1", 64), "workspace://incident/"+runPublicID, evidencePublicID,
		now.Add(-time.Second), now, now)
	if err != nil {
		t.Fatal(err)
	}
	stepID64, _ := stepResult.LastInsertId()
	stepID := uint64(stepID64)
	diagnosis := json.RawMessage(`{"outcome":"diagnosed","summary":"workload recovered without a delivery; verify current-cycle Alert relations","confidence":"confirmed"}`)
	if _, err := db.ExecContext(ctx, `UPDATE agent_runs
	SET status = 'completed', outcome = 'diagnosed', uncertainty = 'low', final_diagnosis = ?,
	    used_steps = 1, used_evidence_items = 1, completed_at = ?, row_version = row_version + 1,
	    updated_at = ?
	WHERE id = ? AND status = 'pending'`, diagnosis, now, now, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_workspace_tasks
	SET status = 'succeeded', completed_at = ?, updated_at = ?
	WHERE agent_run_id = ? AND status = 'ready'`, now, now, runID); err != nil {
		t.Fatal(err)
	}
	facts := json.RawMessage(`{"schema_version":1,"facts":[{"fact_id":"recovery-state","type":"runtime_observation","value":"workload_ready_alerts_firing"}]}`)
	contentHash := operationalRecoveryHash("operational-recovery-recovery-evidence", evidencePublicID, string(facts))
	if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items
	(public_id, incident_id, evidence_contract_version, cycle_no, agent_run_id, agent_step_id,
	 type, source, producer_type, producer_id, producer_version, producer_dedupe_key,
	 adapter_version, query_template_id, query_template_version, scope_snapshot_hash,
	 arguments_hash, tool_name, resource_ref, query_text, summary, facts_json,
	 fact_schema_version, fact_schema_hash, provenance_json, provenance_hash,
	 trust_axes_json, claim_use, corroboration_groups_json, input_evidence_ids_json,
	 input_sample_ids_json, input_hashes_json, result_hash, content_hash, raw_ref,
	 safe_raw_reference, redaction_json, redaction_policy_version, redaction_counts_json,
	 prompt_safety_flags_json, truncated, valid, idempotency_key, collected_at, observed_at, created_at)
	VALUES (?, ?, 1, 1, ?, ?, 'agent_observation', 'kubernetes', 'agent_step', ?,
	        'agent-step-evidence/v1', ?, 'investigation-read/v1', 'operational-recovery/v1', 'v1',
	        ?, ?, 'workspace.recovery_read', 'kubernetes://cloudops-local/demo/deployment/checkout', '',
	        'workload ready while related Alerts remain firing', ?, 1, ?,
	        JSON_OBJECT('agent_run_id', ?, 'agent_step_id', ?, 'source_system', 'kubernetes'), ?,
	        JSON_OBJECT('authority','runtime_observation','integrity','verified','freshness','fresh','completeness','complete'),
	        'support', JSON_ARRAY('runtime/recovery'), JSON_ARRAY(), JSON_ARRAY(), JSON_ARRAY(),
	        ?, ?, ?, ?, JSON_OBJECT('policy','observation-redaction'), 'observation-redaction/v1',
	        JSON_OBJECT('redacted',0), JSON_OBJECT('unsafe',FALSE), FALSE, TRUE, ?, ?, ?, ?)`,
		evidencePublicID, incidentID, runID, stepID, stepPublicID,
		operationalRecoveryHash("operational-recovery-evidence-producer", stepPublicID), strings.Repeat("2", 64),
		strings.Repeat("3", 64), facts, strings.Repeat("4", 64), runPublicID, stepPublicID,
		strings.Repeat("5", 64), contentHash, contentHash, "workspace://incident/"+runPublicID,
		"workspace://incident/"+runPublicID, operationalRecoveryHash("operational-recovery-evidence-idempotency", evidencePublicID),
		now, now, now); err != nil {
		t.Fatal(err)
	}
	return runID, runPublicID, evidencePublicID
}

func claimAndRunOperationalRecovery(t *testing.T, ctx context.Context, tasks *asyncjob.Repository, operation taskhandler.Operation, owner string) *asyncjob.Execution {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct {
		execution asyncjob.Execution
		result    asyncjob.Result
	}
	completed := make(chan outcome, 1)
	pool := asyncjob.DefaultPoolConfigs()[asyncjob.QueueVerify]
	pool.MaxInFlight = 1
	runner, err := asyncjob.NewRunner(asyncjob.RunnerConfig{
		Owner: owner,
		Store: tasks,
		Handlers: map[asyncjob.TaskType]asyncjob.Handler{
			asyncjob.TaskRecoveryVerify: asyncjob.HandlerFunc(func(handlerCtx context.Context, execution asyncjob.Execution) asyncjob.Result {
				result := operation(handlerCtx, execution)
				completed <- outcome{execution: execution, result: result}
				cancel()
				return result
			}),
		},
		TaskTypes:    []asyncjob.TaskType{asyncjob.TaskRecoveryVerify},
		Pools:        map[asyncjob.Queue]asyncjob.PoolConfig{asyncjob.QueueVerify: pool},
		PollInterval: 5 * time.Millisecond,
		DrainTimeout: 5 * time.Second,
		CancelWait:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	var observed outcome
	select {
	case observed = <-completed:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for recovery.verify")
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
	if observed.result.Disposition != asyncjob.DispositionSucceeded || observed.result.Mutate == nil {
		t.Fatalf("recovery task %s result=%+v", observed.execution.Task.PublicID, observed.result)
	}
	return &observed.execution
}

func openOperationalRecoveryIntegrationDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	config, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ParseTime = true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func closeOperationalRecoveryIntegrationDB(t *testing.T, name string, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("close %s database: %v", name, err)
	}
}

func operationalRecoveryDatabaseDSN(t *testing.T, adminDSN, databaseName string) string {
	t.Helper()
	config, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName = databaseName
	config.ParseTime = true
	return config.FormatDSN()
}

func assertOperationalRecoveryValue(t *testing.T, ctx context.Context, db *sql.DB, query, expected string, args ...any) {
	t.Helper()
	var actual string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query value=%q, want %q", actual, expected)
	}
}

func assertOperationalRecoveryCount(t *testing.T, ctx context.Context, db *sql.DB, query string, expected int, args ...any) {
	t.Helper()
	var actual int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query count=%d, want %d", actual, expected)
	}
}

func operationalRecoveryHash(parts ...any) string {
	digest := sha256.Sum256([]byte(fmt.Sprint(parts...)))
	return fmt.Sprintf("%x", digest[:])
}

type operationalRecoveryTopology struct {
	now   func() time.Time
	calls int
}

func (r *operationalRecoveryTopology) Read(_ context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	r.calls++
	now := r.now().UTC()
	return infrastructure.Projection{
		Source: infrastructure.ProviderSource{
			Provider: "kubernetes", ClusterID: request.ClusterID, Identity: "integration://operational-recovery",
			ServerVersion: "v1.36.1", CollectedAt: now,
		},
		Nodes: []infrastructure.Resource{{
			ID:         "k8s://cloudops-local/apps/v1/namespaces/demo/deployments/checkout",
			APIVersion: "apps/v1", Kind: "Deployment", Layer: infrastructure.LayerWorkload,
			Namespace: "demo", Name: "checkout", Health: infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: "ready"},
		}},
	}, nil
}
