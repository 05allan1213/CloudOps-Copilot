package agent

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

	_ "github.com/go-sql-driver/mysql"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

func TestMySQLAlertInvestigationIsAtomicAndIncidentIndependent(t *testing.T) {
	db := openAgentWorkspaceIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settingsService, err := settings.NewService(db, t.TempDir(), settings.BootstrapDiagnostics{MySQLDatabase: "agent-alert-integration"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := settingsService.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := revision.Scopes[0]
	alertID := insertWorkspaceAlert(t, ctx, db, scope, "atomic-alert")
	repository, err := NewWorkspaceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := alertdomain.NewService(db, nil, repository)
	if err != nil {
		t.Fatal(err)
	}
	request := alertdomain.StartInvestigationRequest{
		AlertID: alertID, ExpectedVersion: 1, IdempotencyKey: workspaceSHA256([]byte("atomic-alert-investigation")), Reason: "inspect the current Alert",
		Actor: alertdomain.Actor{Provider: "local", Login: "owner", Role: "owner"},
	}
	view, err := service.StartInvestigation(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if view.Version != 2 || len(view.Investigations) != 1 || view.Investigations[0].IncidentID != "" {
		t.Fatalf("Alert-first Investigation view=%#v", view)
	}
	request.ExpectedVersion = 2
	replayed, err := service.StartInvestigation(ctx, request)
	if err != nil || replayed.Version != 2 || len(replayed.Investigations) != 1 {
		t.Fatalf("Alert Investigation replay=%#v error=%v", replayed, err)
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs run
JOIN context_snapshots snapshot ON snapshot.agent_run_id=run.id
JOIN agent_workspace_tasks task ON task.agent_run_id=run.id
WHERE run.public_id=? AND run.alert_id IS NOT NULL AND run.incident_id IS NULL AND task.status='ready'`, 1, view.Investigations[0].ID)
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM alert_events event JOIN alerts alert ON alert.id=event.alert_id
WHERE alert.public_id=? AND event.event_type='alert_investigation_requested'`, 1, alertID)
}

func TestMySQLIncidentInvestigationBindsUniqueScenarioIdentity(t *testing.T) {
	db := openAgentWorkspaceIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settingsService, err := settings.NewService(db, t.TempDir(), settings.BootstrapDiagnostics{MySQLDatabase: "agent-incident-scenario-integration"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := settingsService.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := revision.Scopes[0]
	scenarioID := "scenario-incident-context"
	alertID := insertWorkspaceAlert(t, ctx, db, scope, "incident-scenario")
	if _, err = db.ExecContext(ctx, `UPDATE alerts
SET labels_json=JSON_OBJECT('alertname','CloudOpsScenarioRequiredEnvMissing','scenario_id',?)
WHERE public_id=?`, scenarioID, alertID); err != nil {
		t.Fatal(err)
	}
	repository, err := NewWorkspaceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	alertService, err := alertdomain.NewService(db, nil, repository)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := alertService.LinkIncident(ctx, alertdomain.LinkIncidentRequest{
		AlertID: alertID, ExpectedVersion: 1, IdempotencyKey: workspaceSHA256([]byte("incident-scenario-link")), Create: true,
		Actor: alertdomain.Actor{Provider: "local", Login: "owner", Role: "owner"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(linked.IncidentLinks) != 1 {
		t.Fatalf("Incident links=%#v", linked.IncidentLinks)
	}
	incidentID := linked.IncidentLinks[0].IncidentID
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := repository.StartIncidentInvestigationTx(ctx, tx, incidentID, "incident-scenario-investigation", "investigate bounded Scenario", 1, 0)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	run, err := repository.WorkspaceRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ScenarioID != scenarioID {
		t.Fatalf("Incident Investigation Scenario=%q want=%q", run.ScenarioID, scenarioID)
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*)
FROM context_snapshots AS snapshot JOIN agent_runs AS run ON run.context_snapshot_id=snapshot.id
WHERE run.public_id=? AND JSON_UNQUOTE(JSON_EXTRACT(snapshot.filters_json,'$.scenario_id'))=?`, 1, runID, scenarioID)

	conflictingAlertID := insertWorkspaceAlert(t, ctx, db, scope, "incident-scenario-conflict")
	if _, err = db.ExecContext(ctx, `UPDATE alerts
SET labels_json=JSON_OBJECT('alertname','CloudOpsScenarioRequiredEnvMissing','scenario_id','scenario-conflict')
WHERE public_id=?`, conflictingAlertID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO alert_incident_links
(public_id,alert_id,incident_id,incident_cycle_no,provenance,configuration_revision_id,escalation_policy_id,created_at)
SELECT ?,alert.id,incident.id,incident.cycle_no,'owner_attached',NULL,NULL,NOW(6)
FROM alerts AS alert JOIN incidents AS incident ON incident.public_id=?
WHERE alert.public_id=?`, uuid.NewString(), incidentID, conflictingAlertID); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.StartIncidentInvestigationTx(ctx, tx, incidentID, "incident-scenario-conflict", "must fail closed", 2, 0)
	_ = tx.Rollback()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("mixed Scenario identities error=%v", err)
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs WHERE idempotency_key='incident-scenario-conflict'`, 0)
}

func TestMySQLWorkspaceLeaseTakeoverAndCancellation(t *testing.T) {
	db := openAgentWorkspaceIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settingsService, err := settings.NewService(db, t.TempDir(), settings.BootstrapDiagnostics{MySQLDatabase: "agent-lease-integration"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := settingsService.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := revision.Scopes[0]
	alertID := insertWorkspaceAlert(t, ctx, db, scope, "lease-alert")
	repository, err := NewWorkspaceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC().Truncate(time.Microsecond)
	repository.now = func() time.Time { return clock }
	run, err := repository.StartAlertInvestigation(ctx, alertID, "lease-takeover", "test lease takeover")
	if err != nil {
		t.Fatal(err)
	}
	first, claimed, err := repository.ClaimWorkspaceTask(ctx, "worker-a", 10*time.Second)
	if err != nil || !claimed || first.Attempt != 1 || first.Generation != 1 {
		t.Fatalf("first claim=%#v claimed=%v error=%v", first, claimed, err)
	}
	clock = clock.Add(11 * time.Second)
	second, claimed, err := repository.ClaimWorkspaceTask(ctx, "worker-b", 10*time.Second)
	if err != nil || !claimed || second.TaskID != first.TaskID || second.Attempt != 2 || second.Generation != 2 {
		t.Fatalf("takeover claim=%#v claimed=%v error=%v", second, claimed, err)
	}
	if err := repository.HeartbeatWorkspaceTask(ctx, first, 10*time.Second); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale lease heartbeat error=%v", err)
	}
	if _, err := repository.RequestCancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteWorkspaceTask(ctx, second, WorkspaceCompletion{
		Outcome: WorkspaceOutcomeDiagnosed, Uncertainty: "low", Answer: "must be replaced by cancellation",
	}); err != nil {
		t.Fatal(err)
	}
	cancelled, err := repository.WorkspaceRun(ctx, run.ID)
	if err != nil || cancelled.Status != RunCancelled || cancelled.Outcome != WorkspaceOutcomeCancelled {
		t.Fatalf("running cancellation=%#v error=%v", cancelled, err)
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_workspace_tasks WHERE id=? AND status='cancelled'`, 1, second.TaskID)
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_workspace_task_attempts
WHERE task_id=? AND attempt=1 AND status='lease_expired'`, 1, second.TaskID)
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_workspace_task_attempts
WHERE task_id=? AND attempt=2 AND status='cancelled'`, 1, second.TaskID)

	pending, err := repository.StartAlertInvestigation(ctx, alertID, "pending-cancel", "cancel before claim")
	if err != nil {
		t.Fatal(err)
	}
	pending, err = repository.RequestCancel(ctx, pending.ID)
	if err != nil || pending.Status != RunCancelled {
		t.Fatalf("pending cancellation=%#v error=%v", pending, err)
	}
	if _, claimed, err = repository.ClaimWorkspaceTask(ctx, "worker-c", 10*time.Second); err != nil || claimed {
		t.Fatalf("cancelled pending task claimed=%v error=%v", claimed, err)
	}
}

func TestMySQLWorkspaceRunnerCollectsEvidenceBeforeDisabledModelOutcome(t *testing.T) {
	db := openAgentWorkspaceIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settingsService, err := settings.NewService(db, t.TempDir(), settings.BootstrapDiagnostics{MySQLDatabase: "agent-runner-integration"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := settingsService.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for index := range revision.Providers {
		switch revision.Providers[index].Provider {
		case settings.ProviderPrometheus, settings.ProviderElasticsearch, settings.ProviderTempo:
			revision.Providers[index].Enabled = true
			revision.Providers[index].Endpoint = "http://provider.integration.test"
			revision.Providers[index].TimeoutMS = 5_000
			revision.Providers[index].MaxResults = 100
		}
	}
	scope := revision.Scopes[0]
	alertID := insertWorkspaceAlert(t, ctx, db, scope, "runner-alert")
	repository, err := NewWorkspaceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	run, err := repository.StartAlertInvestigation(ctx, alertID, "runner-disabled-model", "collect current Provider Evidence")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	runner, err := NewWorkspaceRunner(WorkspaceRunnerConfig{
		Owner: "workspace-runner-integration", Store: repository, Revisions: fixedWorkspaceRevision{revision: revision},
		Kubernetes: integrationInfrastructureProvider{now: now}, Metrics: integrationMetricsProvider{now: now},
		Telemetry: integrationTelemetryProvider{now: now}, Models: disabledWorkspaceModelFactory{},
		MaxInFlight: 1, PollInterval: 10 * time.Millisecond, LeaseDuration: 3 * time.Second,
		HeartbeatInterval: 500 * time.Millisecond, CancellationPoll: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	runnerContext, stopRunner := context.WithCancel(context.Background())
	if err = runner.Start(runnerContext); err != nil {
		stopRunner()
		t.Fatal(err)
	}
	completed := waitForWorkspaceRunTerminal(t, ctx, repository, run.ID)
	stopRunner()
	runner.StopClaims()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err = runner.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if completed.Status != RunCompleted || completed.Outcome != WorkspaceOutcomeInsufficient ||
		completed.FailureCode != workspaceModelDisabledCode || completed.Uncertainty != "high" ||
		!strings.Contains(completed.Answer, "当前 Configuration Revision 未启用") {
		t.Fatalf("disabled-model outcome=%#v", completed)
	}
	wantSources := map[string]bool{"kubernetes": false, "prometheus": false, "elasticsearch": false, "tempo": false}
	for _, citation := range completed.Evidence {
		if _, ok := wantSources[citation.Source]; ok {
			wantSources[citation.Source] = true
		}
	}
	for source, found := range wantSources {
		if !found {
			t.Fatalf("missing current %s Evidence: %#v", source, completed.Evidence)
		}
	}
	listed, err := repository.WorkspaceRuns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	var listedEvidenceCount int
	for _, item := range listed {
		if item.ID == completed.ID {
			listedEvidenceCount = item.EvidenceCount
			break
		}
	}
	if listedEvidenceCount != len(completed.Evidence) {
		t.Fatalf("listed Evidence count=%d want=%d", listedEvidenceCount, len(completed.Evidence))
	}
	if len(completed.Steps) != 4 {
		t.Fatalf("tool steps=%d want=4: %#v", len(completed.Steps), completed.Steps)
	}
	for _, step := range completed.Steps {
		if step.Status != StepCompleted || step.EvidenceID == "" {
			t.Fatalf("tool step did not retain Evidence: %#v", step)
		}
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_stream_events
	WHERE agent_run_id=(SELECT id FROM agent_runs WHERE public_id=?) AND event_type='tool.completed'`, 4, run.ID)
}

func TestMySQLAgentWorkspaceKnowledgeAndAuthority(t *testing.T) {
	db := openAgentWorkspaceIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	settingsService, err := settings.NewService(db, t.TempDir(), settings.BootstrapDiagnostics{MySQLDatabase: "agent-workspace-integration"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := settingsService.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Scopes) == 0 || len(revision.Scopes[0].Namespaces) == 0 {
		t.Fatalf("active revision has no bounded scope: %#v", revision)
	}
	scope := revision.Scopes[0]
	namespace := scope.Namespaces[0]
	resource := telemetry.ResourceReference{
		ID:   workspaceKubernetesResourceID(scope.ClusterID, "Deployment", namespace, "cloudops-api"),
		Kind: "Deployment", Namespace: namespace, Name: "cloudops-api",
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	alertID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO alerts
(public_id,source,alert_key,current_alert_instance_key,correlation_key,correlation_key_version,
 fingerprint,status,severity,cluster,environment,namespace,service_name,target_kind,target_name,
 category,summary,labels_json,annotations_json,first_seen_at,last_seen_at,starts_at)
VALUES (?,'prometheus',?,?,?,2,?,'firing','critical',?,?,?,?,?,'cloudops-api','availability',?,
 JSON_OBJECT('alertname','CloudOpsAPIUnavailable'),JSON_OBJECT(),?,?,?)`, alertID,
		workspaceSHA256([]byte("alert-key")), workspaceSHA256([]byte("instance-key")), workspaceSHA256([]byte("correlation-key")),
		"workspace-integration-"+alertID, scope.ClusterID, scope.Environment, namespace, "cloudops-api", "Deployment",
		"CloudOps API is unavailable", now.Add(-10*time.Minute), now, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	repository, err := NewWorkspaceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	investigation, err := repository.StartAlertInvestigation(ctx, alertID, "alert-investigation-integration", "investigate live Alert")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.StartAlertInvestigation(ctx, alertID, "alert-investigation-integration", "ignored replay")
	if err != nil || replayed.ID != investigation.ID {
		t.Fatalf("Alert Investigation replay=%#v error=%v", replayed, err)
	}
	if investigation.SubjectType != WorkspaceSubjectAlert || investigation.ContextSnapshotID == "" || investigation.Status != RunPending {
		t.Fatalf("Alert Investigation=%#v", investigation)
	}
	lease, claimed, err := repository.ClaimWorkspaceTask(ctx, "workspace-integration-worker", 30*time.Second)
	if err != nil || !claimed || lease.RunPublicID != investigation.ID || lease.ConfigurationRevisionID != revision.ID {
		t.Fatalf("Workspace claim=%#v claimed=%v error=%v", lease, claimed, err)
	}
	execution, err := repository.WorkspaceExecution(ctx, lease)
	if err != nil || execution.Snapshot.ID != investigation.ContextSnapshotID || execution.Run.AlertID != alertID {
		t.Fatalf("Workspace execution=%#v error=%v", execution, err)
	}
	step, err := repository.StartWorkspaceTool(ctx, lease, "kubernetes.read_topology", json.RawMessage(`{"cluster_id":"`+scope.ClusterID+`","namespaces":["`+namespace+`"]}`))
	if err != nil {
		t.Fatal(err)
	}
	evidenceID, err := repository.CompleteWorkspaceTool(ctx, lease, step, WorkspaceToolObservation{
		Tool: "kubernetes.read_topology", Source: "kubernetes", ResourceRef: resource.ID,
		Summary:    "Deployment topology was collected through the bounded Provider Gateway.",
		Facts:      json.RawMessage(`[{"kind":"Deployment","name":"cloudops-api","ready":true}]`),
		Provenance: json.RawMessage(`{"provider":"kubernetes","server_version":"v1.test"}`),
		TrustAxes:  json.RawMessage(`{"authority":"kubernetes_api","integrity":"provider_response","freshness":"captured","completeness":"bounded"}`),
		ObservedAt: now, CollectedAt: now,
	})
	if err != nil || evidenceID == "" {
		t.Fatalf("complete Workspace tool evidence=%q error=%v", evidenceID, err)
	}
	if err := repository.CompleteWorkspaceTask(ctx, lease, WorkspaceCompletion{
		Outcome: WorkspaceOutcomeInsufficient, Uncertainty: "high",
		Answer:      "当前 Kubernetes Evidence 已保存；模型 Provider 未启用，因此不能生成完整诊断。",
		FailureCode: "MODEL_PROVIDER_DISABLED", FailureSummary: "LLM Provider is disabled in the bound Configuration Revision",
	}); err != nil {
		t.Fatal(err)
	}
	completedInvestigation, err := repository.WorkspaceRun(ctx, investigation.ID)
	if err != nil || completedInvestigation.Outcome != WorkspaceOutcomeInsufficient || len(completedInvestigation.Evidence) != 1 ||
		completedInvestigation.Evidence[0].EvidenceID != evidenceID || completedInvestigation.FailureCode != "MODEL_PROVIDER_DISABLED" {
		t.Fatalf("completed Alert Investigation=%#v error=%v", completedInvestigation, err)
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_workspace_task_attempts
WHERE task_id=? AND status='succeeded'`, 1, lease.TaskID)

	telemetryRepository, err := telemetry.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	consultationSeed := telemetry.CreateConsultationRequest{
		Title: "Investigate logs", ClusterID: scope.ClusterID, Environment: scope.Environment,
		Namespaces: []string{namespace}, Resources: []telemetry.ResourceReference{resource}, Filters: json.RawMessage(`{"level":"error"}`),
		From: now.Add(-15 * time.Minute), To: now, EvidenceIDs: []string{uuid.NewString()},
	}
	consultation, err := telemetryRepository.CreateConsultation(ctx, revision.ID, consultationSeed, workspaceSHA256([]byte("consultation-context")))
	if err != nil {
		t.Fatal(err)
	}
	ownerMessage, turn, err := repository.CreateConsultationTurn(ctx, consultation.ID, SendMessageRequest{
		Content: "Explain the selected errors", IdempotencyKey: "consultation-turn-integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	replayedMessage, replayedTurn, err := repository.CreateConsultationTurn(ctx, consultation.ID, SendMessageRequest{
		Content: "different replay body", IdempotencyKey: "consultation-turn-integration",
	})
	if err != nil || replayedMessage.ID != ownerMessage.ID || replayedTurn.ID != turn.ID {
		t.Fatalf("Consultation replay message=%#v run=%#v error=%v", replayedMessage, replayedTurn, err)
	}

	var consultationInternalID, runInternalID, snapshotInternalID uint64
	if err := db.QueryRowContext(ctx, `SELECT consultation.id,run.id,snapshot.id
FROM agent_consultations consultation
JOIN agent_runs run ON run.consultation_id=consultation.id AND run.public_id=?
JOIN context_snapshots snapshot ON snapshot.public_id=?
WHERE consultation.public_id=?`, turn.ID, consultation.Snapshot.ID, consultation.ID).Scan(
		&consultationInternalID, &runInternalID, &snapshotInternalID); err != nil {
		t.Fatal(err)
	}
	assistantMessageID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_consultation_messages
(public_id,consultation_id,agent_run_id,context_snapshot_id,sequence,role,content,status,created_at,completed_at)
VALUES (?,?,?,?,2,'assistant','Current Evidence is insufficient without a model response.','completed',?,?)`,
		assistantMessageID, consultationInternalID, runInternalID, snapshotInternalID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_runs SET status='completed',outcome='insufficient',uncertainty='high',
failure_code='model_unavailable',failure_summary='LLM Provider is disabled',started_at=?,completed_at=?,updated_at=? WHERE id=?`,
		now, now, now, runInternalID); err != nil {
		t.Fatal(err)
	}

	reviewAt, expiresAt := now.Add(7*24*time.Hour), now.Add(30*24*time.Hour)
	if _, err := repository.CreateKnowledge(ctx, SaveKnowledgeRequest{
		Title: "Invalid owner source", Content: "must not be accepted", SourceConsultationID: consultation.ID,
		SourceMessageID: ownerMessage.ID, ClusterID: scope.ClusterID, Environment: scope.Environment,
		Namespaces: []string{namespace}, Resources: []telemetry.ResourceReference{}, ReviewAt: &reviewAt, ExpiresAt: &expiresAt,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Owner message promoted to Knowledge error=%v", err)
	}
	knowledge, err := repository.CreateKnowledge(ctx, SaveKnowledgeRequest{
		Title: "CloudOps API log pattern", Content: "Check repeated readiness failures before restart guidance.",
		SourceConsultationID: consultation.ID, SourceMessageID: assistantMessageID,
		ClusterID: scope.ClusterID, Environment: scope.Environment, Namespaces: []string{namespace},
		Resources: []telemetry.ResourceReference{resource}, ReviewAt: &reviewAt, ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if knowledge.Revision.Revision != 1 || knowledge.Revision.SourceMessageID != assistantMessageID || knowledge.Revision.ConfirmedBy != "local-owner" {
		t.Fatalf("Owner-confirmed Knowledge=%#v", knowledge)
	}
	updated, err := repository.UpdateKnowledge(ctx, knowledge.ID, UpdateKnowledgeRequest{Content: "Check exact readiness and dependency failures before restart guidance."})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision.Revision != 2 || updated.Revision.ID == knowledge.Revision.ID || len(updated.Revisions) != 2 {
		t.Fatalf("Knowledge immutable revision=%#v", updated)
	}
	applicable, err := repository.ApplicableKnowledge(ctx, consultation.Snapshot.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(applicable) != 1 || applicable[0].ID != updated.Revision.ID || applicable[0].Revision != 2 {
		t.Fatalf("applicable Knowledge=%#v", applicable)
	}

	actionRequest := ActionProposalRequest{
		RunID: turn.ID, ActionType: "alert.acknowledge", Target: json.RawMessage(`{"alert_id":"` + alertID + `"}`),
		Parameters: json.RawMessage(`{"reason":"owner reviewed"}`), Preconditions: json.RawMessage(`["alert is firing"]`),
		Risk: "Marks the local Alert recurrence as acknowledged.", ExpiresAt: now.Add(time.Hour),
	}
	card, err := repository.ProposeActionCard(ctx, actionRequest)
	if err != nil {
		t.Fatal(err)
	}
	cardReplay, err := repository.ProposeActionCard(ctx, actionRequest)
	if err != nil || cardReplay.ID != card.ID {
		t.Fatalf("action card replay=%#v error=%v", cardReplay, err)
	}
	if _, err := repository.AuthorizeActionCard(ctx, card.ID, AuthorizeActionRequest{ExpectedHash: workspaceSHA256([]byte("wrong")), Reason: "wrong hash"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong action hash authorization error=%v", err)
	}
	card, err = repository.AuthorizeActionCard(ctx, card.ID, AuthorizeActionRequest{ExpectedHash: card.ContentHash, Reason: "reviewed exact action card"})
	if err != nil {
		t.Fatal(err)
	}
	if card.Status != "authorized" || card.Authorization == nil || card.Authorization.AuthorizedHash != card.ContentHash {
		t.Fatalf("authorized action card=%#v", card)
	}

	planRequest := ActionProposalRequest{
		RunID: turn.ID, ActionType: "kubernetes.deployment.patch", Target: json.RawMessage(`{"resource_id":"` + resource.ID + `"}`),
		Parameters: json.RawMessage(`{"replicas":2}`), IntendedState: json.RawMessage(`{"ready_replicas":2}`),
		Preconditions: json.RawMessage(`["resourceVersion remains unchanged"]`), Risk: "Changes a Kubernetes workload.",
		VerificationIntent: json.RawMessage(`{"check":"ready replicas"}`), ExpiresAt: now.Add(time.Hour),
	}
	plan, err := repository.ProposeOperationPlan(ctx, planRequest)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = repository.AuthorizeOperationPlan(ctx, plan.ID, AuthorizeActionRequest{ExpectedHash: plan.ContentHash, Reason: "reviewed immutable Operation Plan"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "authorized" || plan.Authorization == nil || plan.ConfigurationRevisionID != revision.ID {
		t.Fatalf("authorized Operation Plan=%#v", plan)
	}
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_action_authorizations WHERE action_card_id IS NOT NULL`, 1)
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM agent_action_authorizations WHERE operation_plan_id IS NOT NULL`, 1)
	assertAgentWorkspaceCount(t, ctx, db, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE()
AND table_name IN ('agent_runs','agent_consultation_messages') AND column_name IN ('chain_of_thought','reasoning','thoughts')`, 0)
}

func insertWorkspaceAlert(t *testing.T, ctx context.Context, db *sql.DB, scope settings.OperationalScope, suffix string) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	alertID := uuid.NewString()
	namespace := scope.Namespaces[0]
	if _, err := db.ExecContext(ctx, `INSERT INTO alerts
(public_id,source,alert_key,current_alert_instance_key,correlation_key,correlation_key_version,
 fingerprint,status,severity,cluster,environment,namespace,service_name,target_kind,target_name,
 category,summary,labels_json,annotations_json,first_seen_at,last_seen_at,starts_at)
VALUES (?,'prometheus',?,?,?,2,?,'firing','critical',?,?,?,?,?,'cloudops-api','availability',?,
 JSON_OBJECT('alertname','CloudOpsAPIUnavailable'),JSON_OBJECT(),?,?,?)`, alertID,
		workspaceSHA256([]byte("alert-key-"+suffix)), workspaceSHA256([]byte("instance-key-"+suffix)), workspaceSHA256([]byte("correlation-key-"+suffix)),
		"workspace-integration-"+suffix+"-"+alertID, scope.ClusterID, scope.Environment, namespace, "cloudops-api", "Deployment",
		"CloudOps API is unavailable", now.Add(-10*time.Minute), now, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return alertID
}

func openAgentWorkspaceIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	name := fmt.Sprintf("cloudops_agent_workspace_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		_ = admin.Close()
	})
	config, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName, config.ParseTime, config.MultiStatements = name, true, true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	runner, err := migrationrunner.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertAgentWorkspaceCount(t *testing.T, ctx context.Context, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d query=%s", got, want, query)
	}
}

type fixedWorkspaceRevision struct {
	revision settings.Revision
}

func (source fixedWorkspaceRevision) Revision(context.Context, string) (settings.Revision, error) {
	return source.revision, nil
}

type disabledWorkspaceModelFactory struct{}

func (disabledWorkspaceModelFactory) Model(context.Context, string) (WorkspaceModel, string, string, error) {
	return nil, "", "", ErrWorkspaceModelDisabled
}

type integrationInfrastructureProvider struct {
	now time.Time
}

func (provider integrationInfrastructureProvider) Probe(context.Context, string) (infrastructure.ProviderSource, error) {
	return infrastructure.ProviderSource{Provider: "kubernetes", Identity: "kind-cloudops-local", ServerVersion: "v1.integration", CollectedAt: provider.now}, nil
}

func (provider integrationInfrastructureProvider) Read(_ context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	namespace := request.Namespaces[0]
	return infrastructure.Projection{
		Source: infrastructure.ProviderSource{Provider: "kubernetes", ClusterID: request.ClusterID, Identity: "kind-cloudops-local", ServerVersion: "v1.integration", CollectedAt: provider.now},
		Nodes: []infrastructure.Resource{{
			ID:         workspaceKubernetesResourceID(request.ClusterID, "Deployment", namespace, "cloudops-api"),
			APIVersion: "apps/v1", Kind: "Deployment", Layer: infrastructure.LayerWorkload,
			Namespace: namespace, Name: "cloudops-api", Status: "Running",
			Health: infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: "available replicas match desired replicas"},
		}},
		Edges: []infrastructure.TopologyEdge{}, Issues: []infrastructure.ProviderIssue{},
	}, nil
}

func (integrationInfrastructureProvider) Events(context.Context, string, infrastructure.Resource, int) ([]infrastructure.Event, bool, error) {
	return []infrastructure.Event{}, false, nil
}

type integrationMetricsProvider struct {
	now time.Time
}

func (provider integrationMetricsProvider) Catalog(context.Context, observability.ProviderCatalogRequest) (observability.ProviderCatalog, error) {
	return observability.ProviderCatalog{Source: observability.ProviderSource{Provider: "prometheus", Identity: "prometheus.integration.test", CollectedAt: provider.now}}, nil
}

func (provider integrationMetricsProvider) Query(_ context.Context, request observability.ProviderQueryRequest) (observability.ProviderQueryResult, error) {
	return observability.ProviderQueryResult{
		Source: observability.ProviderSource{Provider: "prometheus", Identity: "prometheus.integration.test", ServerVersion: "v3.integration", CollectedAt: provider.now},
		Result: observability.QueryResult{ResultType: "matrix", Series: []observability.QuerySeries{{
			Labels: map[string]string{"deployment": request.Resource.Name},
			Points: []observability.QueryPoint{{Timestamp: provider.now, Value: 1}},
		}}},
		SeriesCount: 1, SampleCount: 1, ResponseBytes: 128,
	}, nil
}

type integrationTelemetryProvider struct {
	now time.Time
}

func (provider integrationTelemetryProvider) Catalog(_ context.Context, request telemetry.ProviderCatalogRequest) (telemetry.ProviderCatalog, error) {
	return telemetry.ProviderCatalog{Source: telemetry.ProviderSource{Provider: request.Provider, Identity: request.Provider + ".integration.test", CollectedAt: provider.now}}, nil
}

func (provider integrationTelemetryProvider) QueryLogs(_ context.Context, request telemetry.ProviderLogRequest) (telemetry.ProviderLogResult, error) {
	return telemetry.ProviderLogResult{
		Source:        telemetry.ProviderSource{Provider: "elasticsearch", Identity: "elasticsearch.integration.test", ServerVersion: "v9.integration", CollectedAt: provider.now},
		Entries:       []telemetry.LogEntry{{ID: "log-integration", Timestamp: provider.now, Level: "error", Message: "readiness dependency recovered", Service: request.Resource.Name, Resource: request.Resource}},
		ResponseBytes: 256,
	}, nil
}

func (provider integrationTelemetryProvider) SearchTraces(_ context.Context, request telemetry.ProviderTraceSearchRequest) (telemetry.ProviderTraceSearchResult, error) {
	return telemetry.ProviderTraceSearchResult{
		Source:        telemetry.ProviderSource{Provider: "tempo", Identity: "tempo.integration.test", ServerVersion: "v2.integration", CollectedAt: provider.now},
		Traces:        []telemetry.TraceSummary{{TraceID: "0123456789abcdef0123456789abcdef", RootService: request.Resource.Name, RootOperation: "GET /readyz", StartTime: provider.now, DurationMS: 12, SpanCount: 2, Resource: request.Resource}},
		ResponseBytes: 192,
	}, nil
}

func (integrationTelemetryProvider) Trace(context.Context, telemetry.ProviderTraceDetailRequest) (telemetry.ProviderTraceDetailResult, error) {
	return telemetry.ProviderTraceDetailResult{}, nil
}

func waitForWorkspaceRunTerminal(t *testing.T, ctx context.Context, repository *WorkspaceRepository, runID string) WorkspaceRun {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := repository.WorkspaceRun(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == RunCompleted || run.Status == RunFailed || run.Status == RunCancelled {
			return run
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Workspace run: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}
