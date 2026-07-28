package api

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
		plans.RemediationPlans[0].Decision.Actor.Role != "owner" ||
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

func TestMySQLIncidentCoordinationProjectionIsTypedAndCurrentCycle(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := openWorkbenchIntegrationDB(t, ctx, adminDSN)
	var now time.Time
	if err := db.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&now); err != nil {
		t.Fatal(err)
	}
	now = now.UTC().Truncate(time.Microsecond)

	var configurationRevisionID uint64
	var operationalScopeID string
	if err := db.QueryRowContext(ctx, `SELECT active.configuration_revision_id, scope.public_id
FROM active_configuration active
JOIN operational_scopes scope ON scope.configuration_revision_id = active.configuration_revision_id
WHERE active.singleton_id = 1 AND scope.cluster_id = 'cloudops-local'`).Scan(
		&configurationRevisionID, &operationalScopeID,
	); err != nil {
		t.Fatal(err)
	}
	snapshotResult, err := db.ExecContext(ctx, `INSERT INTO topology_snapshots (
public_id, configuration_revision_id, cluster_id, environment, namespaces_json,
scope_hash, content_hash, provider_state, source_identity, server_version,
partial, truncated, node_count, edge_count, projection_json, collected_at,
fresh_until, last_observed_at
) VALUES (?, ?, 'cloudops-local', 'local', JSON_ARRAY('demo'), ?, ?, 'available',
          'integration://task-7', 'v1.36.1', FALSE, FALSE, 1, 0, JSON_OBJECT(),
          ?, ?, ?)`, uuid.NewString(), configurationRevisionID, strings.Repeat("a", 64),
		strings.Repeat("b", 64), now.Add(-time.Minute), now.Add(time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, _ := snapshotResult.LastInsertId()
	resourceID := "k8s://cloudops-local/apps/v1/namespaces/demo/deployments/checkout"
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_identities (
resource_id, cluster_id, api_version, kind, namespace, name, source_uid,
health_state, last_snapshot_id, first_seen_at, last_seen_at
) VALUES (?, 'cloudops-local', 'apps/v1', 'Deployment', 'demo', 'checkout',
          'uid-task-7', 'warning', ?, ?, ?)`, resourceID, snapshotID,
		now.Add(-15*time.Minute), now); err != nil {
		t.Fatal(err)
	}

	incidentPublicID := uuid.NewString()
	incidentResult, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster,
namespace, service_name, environment, target_kind, target_name, severity,
status, summary, first_seen_at, last_seen_at, version, cycle_no,
needs_attention, blocking_reason_code, blocked_at
) VALUES (?, ?, ?, 2, 'cloudops-local', 'demo', 'checkout', 'local',
          'Deployment', 'checkout', 'critical', 'investigating',
          'Task 7 current-cycle coordination fixture', ?, ?, 7, 2, TRUE,
          'verification_failed', ?)`, incidentPublicID,
		"task-7-incident-"+incidentPublicID, strings.Repeat("c", 64),
		now.Add(-10*time.Minute), now, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	incidentID64, _ := incidentResult.LastInsertId()
	incidentID := uint64(incidentID64)

	insertAlert := func(keyByte string, summary string) (uint64, string) {
		t.Helper()
		publicID := uuid.NewString()
		result, insertErr := db.ExecContext(ctx, `INSERT INTO alerts (
public_id, source, alert_key, current_alert_instance_key, correlation_key,
correlation_key_version, fingerprint, status, severity, cluster, environment,
namespace, service_name, target_kind, target_name, category, summary,
labels_json, annotations_json, first_seen_at, last_seen_at, starts_at
) VALUES (?, 'alertmanager', ?, ?, ?, 2, ?, 'firing', 'critical',
          'cloudops-local', 'local', 'demo', 'checkout', 'Deployment', 'checkout',
          'availability', ?, JSON_OBJECT(), JSON_OBJECT(), ?, ?, ?)`, publicID,
			strings.Repeat(keyByte, 64), strings.Repeat(keyByte, 64),
			strings.Repeat(keyByte, 64), "task-7-alert-"+publicID, summary,
			now.Add(-9*time.Minute), now, now.Add(-9*time.Minute))
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		id, _ := result.LastInsertId()
		return uint64(id), publicID
	}
	firstAlertID, firstAlertPublicID := insertAlert("1", "checkout unavailable")
	secondAlertID, secondAlertPublicID := insertAlert("2", "checkout errors elevated")
	historicalAlertID, historicalAlertPublicID := insertAlert("3", "previous cycle only")
	insertRelation := func(alertID uint64, cycle uint64, provenance string) string {
		t.Helper()
		publicID := uuid.NewString()
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO alert_incident_links (
public_id, alert_id, incident_id, incident_cycle_no, provenance
) VALUES (?, ?, ?, ?, ?)`, publicID, alertID, incidentID, cycle, provenance); insertErr != nil {
			t.Fatal(insertErr)
		}
		return publicID
	}
	firstRelationID := insertRelation(firstAlertID, 2, "owner_created")
	secondRelationID := insertRelation(secondAlertID, 2, "owner_attached")
	_ = insertRelation(historicalAlertID, 1, "owner_attached")

	insertEvent := func(cycle uint64, eventType string, occurredAt time.Time) string {
		t.Helper()
		publicID := uuid.NewString()
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO incident_events (
public_id, incident_id, cycle_no, event_schema_version, event_type, actor_type,
actor_id, summary, metadata_json, occurred_at
) VALUES (?, ?, ?, 1, ?, 'system', 'task-7-integration', ?,
          JSON_OBJECT('cycle', ?), ?)`, publicID, incidentID, cycle, eventType,
			"event "+eventType, cycle, occurredAt); insertErr != nil {
			t.Fatal(insertErr)
		}
		return publicID
	}
	currentEventID := insertEvent(2, "verification_failed", now.Add(-time.Minute))
	_ = insertEvent(1, "historical_cycle_event", now.Add(-20*time.Minute))

	insertEvidence := func(cycle uint64, keyByte, summary string, collectedAt time.Time) string {
		t.Helper()
		publicID := uuid.NewString()
		hash := strings.Repeat(keyByte, 64)
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO evidence_items (
public_id, incident_id, cycle_no, type, source, producer_type,
producer_dedupe_key, tool_name, resource_ref, query_text, summary, facts_json,
result_hash, content_hash, raw_ref, truncated, valid, collected_at, observed_at
) VALUES (?, ?, ?, 'metric', 'prometheus', 'system_enrichment', ?, 'prom.query_range',
          ?, 'rate(checkout_errors_total[5m])', ?, JSON_OBJECT('facts', JSON_ARRAY()),
          ?, ?, '', FALSE, TRUE, ?, ?)`, publicID, incidentID, cycle,
			"task-7-evidence-"+publicID, resourceID, summary, hash, hash,
			collectedAt, collectedAt); insertErr != nil {
			t.Fatal(insertErr)
		}
		return publicID
	}
	currentEvidenceID := insertEvidence(2, "4", "current-cycle error rate", now.Add(-2*time.Minute))
	_ = insertEvidence(1, "5", "historical-cycle error rate", now.Add(-20*time.Minute))

	insertInvestigation := func(cycle uint64, summary string, completedAt time.Time) string {
		t.Helper()
		publicID := uuid.NewString()
		diagnosis, _ := json.Marshal(map[string]any{"summary": summary})
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, status, model, prompt_version, max_steps, used_steps,
objective, final_diagnosis, failure_code, completed_at, cycle_no,
expected_incident_version
) VALUES (?, ?, 'completed', 'fixture-model', 'incident-agent/v1', 4, 2,
          'investigate checkout', ?, '', ?, ?, 7)`, publicID, incidentID,
			diagnosis, completedAt, cycle); insertErr != nil {
			t.Fatal(insertErr)
		}
		return publicID
	}
	currentInvestigationID := insertInvestigation(2, "no change; wait for signal recovery", now.Add(-90*time.Second))
	_ = insertInvestigation(1, "historical diagnosis", now.Add(-30*time.Minute))

	signalPublicID := uuid.NewString()
	signalResult, err := db.ExecContext(ctx, `INSERT INTO incident_signals (
public_id, incident_id, cycle_no, source, source_event_id, canonical_schema_version,
correlation_key_version, fingerprint, alert_instance_key, status, severity, cluster,
namespace, service_name, environment, target_kind, target_name, category, occurred_at,
starts_at, received_at, summary, labels_json, annotations_json
) VALUES (?, ?, 2, 'alertmanager', ?, 1, 2, ?, ?, 'firing', 'critical',
          'cloudops-local', 'demo', 'checkout', 'local', 'Deployment', 'checkout',
          'availability', ?, ?, ?, 'verification trigger', JSON_OBJECT(), JSON_OBJECT())`,
		signalPublicID, incidentID, "task-7-signal-"+signalPublicID,
		"task-7-signal-"+signalPublicID, strings.Repeat("6", 64),
		now.Add(-3*time.Minute), now.Add(-3*time.Minute), now.Add(-3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	signalID64, _ := signalResult.LastInsertId()
	verificationPublicID := uuid.NewString()
	revision := strings.Repeat("7", 40)
	if _, err := db.ExecContext(ctx, `INSERT INTO verification_runs (
public_id, incident_id, cycle_no, trigger_signal_id, status, trigger_type,
target_revision, source_revision, image_digest, gitops_revision, plan_json,
verification_profile_id, verification_profile_version, verification_profile_hash,
verification_contract_version, common_stability_window_ms, started_at, deadline_at,
completed_at, attempt, expected_subject_version, result_summary, failure_reason
) VALUES (?, ?, 2, ?, 'failed', 'no_change_signal', ?, ?, ?, ?, JSON_OBJECT(),
          'no-change/v1', 1, ?, 1, 60000, ?, ?, ?, 1, 7,
          'required Alert remains firing', 'required_check_failed')`,
		verificationPublicID, incidentID, signalID64, revision, revision,
		"sha256:"+strings.Repeat("8", 64), revision, strings.Repeat("9", 64),
		now.Add(-80*time.Second), now.Add(5*time.Minute), now.Add(-70*time.Second)); err != nil {
		t.Fatal(err)
	}

	port, err := NewMySQLQueryPort(db)
	if err != nil {
		t.Fatal(err)
	}
	wantAttention := true
	incidents, err := port.Query(ctx, QueryRequest{
		Kind: QueryIncidents, Limit: 10, Attention: &wantAttention,
		Resource: resourceID, RelatedAlertID: firstAlertPublicID,
		From: now.Add(-15 * time.Minute), To: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(incidents.Incidents) != 1 || incidents.Incidents[0].ID != incidentPublicID {
		t.Fatalf("filtered Incident projection=%+v", incidents.Incidents)
	}
	incidentResultView, err := port.Query(ctx, QueryRequest{Kind: QueryIncident, IncidentID: incidentPublicID})
	if err != nil {
		t.Fatal(err)
	}
	incident := incidentResultView.Incident
	if incident == nil || incident.RelatedAlertCount != 2 ||
		incident.OperationalContext.OperationalScopeID != operationalScopeID ||
		incident.OperationalContext.Resource.ID != resourceID ||
		!incident.Attention.Required || incident.Attention.Stage != "investigate" ||
		incident.Recovery.State != "investigate" || incident.Recovery.FailedVerificationCount != 1 ||
		incident.Recovery.LatestVerificationID != verificationPublicID || incident.Recovery.CanClose ||
		incident.Decision == nil || incident.Decision.Kind != "no_change" ||
		incident.Decision.InvestigationID != currentInvestigationID ||
		incident.Decision.VerificationID != verificationPublicID {
		t.Fatalf("Incident coordination projection=%+v", incident)
	}
	workspaces := make(map[string]bool, len(incident.ContextLinks))
	for _, link := range incident.ContextLinks {
		workspaces[link.Workspace] = true
		if link.OperationalScopeID != operationalScopeID || link.External {
			t.Fatalf("Incident Context Link=%+v", link)
		}
	}
	for _, workspace := range []string{"monitoring", "logs", "traces", "agent", "alerts"} {
		if !workspaces[workspace] {
			t.Errorf("missing %s Context Link: %+v", workspace, incident.ContextLinks)
		}
	}
	if workspaces["devops"] {
		t.Fatalf("DevOps Context Link exists without action record: %+v", incident.ContextLinks)
	}

	relations, err := port.Query(ctx, QueryRequest{Kind: QueryAlertRelations, IncidentID: incidentPublicID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(relations.AlertRelations) != 2 || relations.AlertRelations[0].Cycle != 2 ||
		relations.AlertRelations[1].Cycle != 2 {
		t.Fatalf("current-cycle Alert relations=%+v", relations.AlertRelations)
	}
	relationByID := map[string]IncidentAlertRelationView{}
	for _, relation := range relations.AlertRelations {
		relationByID[relation.ID] = relation
	}
	if relationByID[firstRelationID].AlertID != firstAlertPublicID ||
		relationByID[firstRelationID].Provenance != "owner_created" ||
		relationByID[secondRelationID].AlertID != secondAlertPublicID ||
		relationByID[secondRelationID].Provenance != "owner_attached" {
		t.Fatalf("Alert relation provenance=%+v", relationByID)
	}
	for _, relation := range relations.AlertRelations {
		if relation.AlertID == historicalAlertPublicID {
			t.Fatal("historical Alert leaked into current-cycle relations")
		}
	}

	timeline, err := port.Query(ctx, QueryRequest{Kind: QueryTimeline, IncidentID: incidentPublicID, Limit: 10})
	if err != nil || len(timeline.Timeline) != 1 || timeline.Timeline[0].ID != currentEventID || timeline.Timeline[0].Cycle != 2 {
		t.Fatalf("current-cycle timeline=%+v err=%v", timeline.Timeline, err)
	}
	evidence, err := port.Query(ctx, QueryRequest{Kind: QueryEvidence, IncidentID: incidentPublicID, Limit: 10})
	if err != nil || len(evidence.Evidence) != 1 || evidence.Evidence[0].ID != currentEvidenceID || evidence.Evidence[0].Cycle != 2 {
		t.Fatalf("current-cycle Evidence=%+v err=%v", evidence.Evidence, err)
	}
	investigations, err := port.Query(ctx, QueryRequest{Kind: QueryInvestigations, IncidentID: incidentPublicID, Limit: 10})
	if err != nil || len(investigations.Investigations) != 1 ||
		investigations.Investigations[0].ID != currentInvestigationID ||
		investigations.Investigations[0].Cycle != 2 {
		t.Fatalf("current-cycle Investigations=%+v err=%v", investigations.Investigations, err)
	}
	if err := validateIncidentAlertRelations(relations.AlertRelations); err != nil {
		t.Fatalf("Alert relation transport validation: %v", err)
	}
	if err := validateIncidentTimeline(timeline.Timeline); err != nil {
		t.Fatalf("timeline transport validation: %v", err)
	}
	if err := validateIncidentEvidence(evidence.Evidence); err != nil {
		t.Fatalf("Evidence transport validation: %v", err)
	}
	if err := validateIncidentInvestigations(investigations.Investigations); err != nil {
		t.Fatalf("Investigation transport validation: %v", err)
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
status, summary, first_seen_at, last_seen_at, version, cycle_no
) VALUES (?, ?, ?, 2, 'kind-local', 'demo', 'demo', 'local',
          'Deployment', 'demo', 'critical', 'investigating',
          'optional Workbench resources fixture', NOW(6), NOW(6), 1, 1)`, incidentPublicID,
		"workbench-optional-"+incidentPublicID,
		strings.Repeat("a", 64)); err != nil {
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
	databaseName := fmt.Sprintf("cloudops_apiv1_workbench_%d", time.Now().UnixNano())
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
status, summary, first_seen_at, last_seen_at, version, cycle_no
) VALUES (?, ?, ?, 2, 'kind-local', 'demo', 'demo', 'local',
          'Deployment', 'demo', 'critical', 'investigating',
          'typed Workbench fixture', ?, ?, 2, 1)`,
		incidentPublicID, "workbench-"+incidentPublicID, strings.Repeat("1", 64),
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
final_diagnosis, failure_code, completed_at, cycle_no, expected_incident_version
) VALUES (?, ?, 'completed', 'fixture-model', 'incident-agent/v1', 1, ?, '',
          ?, 1, 2)`, agentRunPublicID, incidentID, diagnosisJSON, now.Add(-2*time.Minute))
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
cycle_no
) VALUES (?, ?, ?, 'configuration', 'github', 'agent_step', ?,
          'github://acme/gitops/apps/demo/deployment.yaml', 'exact blob',
          'verified baseline node', JSON_OBJECT('required_env','healthy'),
          ?, ?, '', FALSE, TRUE, ?, 1)`, evidencePublicID, incidentID,
		agentRunID, "workbench-evidence-"+evidencePublicID, evidenceHash, evidenceHash, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	targetRevision := strings.Repeat("6", 40)
	verificationPlan, err := verification.CompilePlan(verification.CompileInput{
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
		MaxDiffBytes: remediation.MaxPlanDiffBytes, MaxPostImageBytes: remediation.MaxPostImageBytes,
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
	repository, err := remediationmysql.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreatePlan(ctx, &plan); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'awaiting_approval', version = 3,
    updated_at = ?
WHERE id = ? AND cycle_no = 1`, now, incidentID); err != nil {
		t.Fatal(err)
	}
	decision := remediation.Approval{
		PublicID: uuid.NewString(), DecisionSchemaVersion: 1,
		IncidentID: incidentID, CycleNo: 1, PlanID: plan.ID, PlanVersion: plan.PlanVersion,
		Decision: remediation.DecisionApproved, ActorProvider: "local", Actor: "owner", Role: "owner",
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
last_observed_at, row_version, incident_id, cycle_no, operation_step,
expected_subject_version, logical_operation_key,
created_at, updated_at
) VALUES (?, ?, 'acme/gitops', ?, 'cloudops/typed-workbench', ?, 42,
          'https://github.com/acme/gitops/pull/42', 'open', ?, ?,
          'rolling_out', 'passing', ?, 'cloudops-demo', 'cloudops-demo', ?,
          'Synced', 'Succeeded', 'Progressing', JSON_OBJECT('deployment','Progressing'),
          'kind-local', 'local', 'demo', 'Deployment', 'demo',
          8, 8, ?, 2, 2, 1, 1, ?, ?, ?, 2, ?, 1, 'observe',
          1, ?, ?, ?)`, deliveryPublicID, plan.ID, plan.TargetBaseRevision,
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
public_id, incident_id, cycle_no, remediation_plan_id,
change_request_id, status, trigger_type, trigger_signal_id,
target_revision, source_revision, image_digest, gitops_revision, plan_json,
verification_profile_version, verification_profile_hash,
verification_contract_version, verification_profile_id,
common_stability_window_ms, started_at, deadline_at, attempt, row_version,
expected_subject_version, result_summary, failure_reason, created_at, updated_at
) VALUES (?, ?, 1, ?, ?, 'running', 'post_delivery', NULL,
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
public_id, verification_run_id, incident_id, cycle_no,
check_type, status, required_check, subject_json, expected_json, observed_json,
source_reference, lookback_ms, stability_window_ms, timeout_ms,
poll_interval_ms, check_spec_schema_version, profile_id, template_id,
template_version, comparison, threshold, source_identity, initial_delay_ms,
min_samples, sample_unit, failure_mode, attempt_count, created_at, updated_at
) VALUES (?, ?, ?, 1, ?, 'running', TRUE, ?, ?, JSON_OBJECT('status','available'),
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
public_id, sample_schema_version, incident_id, cycle_no,
verification_run_id, verification_check_id, sample_sequence, status,
observed_json, source_reference, reason_code, sampled_at, content_hash, created_at
) VALUES (?, 1, ?, 1, ?, ?, 1, 'pending', JSON_OBJECT('status','available'),
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
