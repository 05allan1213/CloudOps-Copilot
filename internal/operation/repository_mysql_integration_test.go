package operation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

func TestMySQLExactAuthorityLocalExecutionAndInvalidation(t *testing.T) {
	db := openOperationIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	workspace, err := agent.NewWorkspaceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	runID, target := createOperationWorkspaceRun(t, ctx, db, workspace)
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewLocalChangeFreezeAdapter(repository)
	if err != nil {
		t.Fatal(err)
	}

	card := proposeChangeFreeze(t, ctx, workspace, runID, target, true, false, 0, "freeze before maintenance")
	if _, err = repository.EnqueueActionCard(ctx, card.ID, ExecuteRequest{ExpectedHash: card.ContentHash}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("zero-authorization enqueue error=%v", err)
	}
	assertOperationCount(t, ctx, db, "SELECT COUNT(*) FROM operation_executions", 0)
	if _, found, claimErr := repository.Claim(ctx, "operation-test", time.Minute); claimErr != nil || found {
		t.Fatalf("zero-authorization claim found=%v error=%v", found, claimErr)
	}

	card, err = workspace.AuthorizeActionCard(ctx, card.ID, agent.AuthorizeActionRequest{
		ExpectedHash: card.ContentHash, Reason: "reviewed exact local change-freeze action",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.EnqueueActionCard(ctx, card.ID, ExecuteRequest{ExpectedHash: strings.Repeat("f", 64)}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong execution hash error=%v", err)
	}
	execution, err := repository.EnqueueActionCard(ctx, card.ID, ExecuteRequest{ExpectedHash: card.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.EnqueueActionCard(ctx, card.ID, ExecuteRequest{ExpectedHash: card.ContentHash})
	if err != nil || replayed.ID != execution.ID {
		t.Fatalf("execution replay=%#v error=%v", replayed, err)
	}
	lease, found, err := repository.Claim(ctx, "operation-test", time.Minute)
	if err != nil || !found || lease.ExecutionPublicID != execution.ID {
		t.Fatalf("claim=%#v found=%v error=%v", lease, found, err)
	}
	subject, err := repository.SubjectForExecution(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := adapter.Prepare(ctx, subject)
	if err != nil || prepared.External {
		t.Fatalf("prepared=%#v error=%v", prepared, err)
	}
	if err = repository.RecordPrepared(ctx, lease, prepared); err != nil {
		t.Fatal(err)
	}
	if err = repository.BeginEffect(ctx, lease, subject, prepared); err != nil {
		t.Fatal(err)
	}
	observation, err := adapter.Apply(ctx, subject, prepared)
	if err != nil || !observation.Verified || observation.Source != "local" {
		t.Fatalf("observation=%#v error=%v", observation, err)
	}
	if err = repository.Complete(ctx, lease, observation); err != nil {
		t.Fatal(err)
	}
	completed, err := repository.Execution(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" || completed.Verification == nil || completed.Verification.Status != "passed" || len(completed.Events) != 6 {
		t.Fatalf("completed execution=%#v", completed)
	}
	for index, event := range completed.Events {
		if event.Sequence != uint32(index+1) || len(event.ContentHash) != 64 {
			t.Fatalf("audit event[%d]=%#v", index, event)
		}
	}
	freeze, err := repository.ChangeFreeze(ctx, target)
	if err != nil || !freeze.Enabled || freeze.RowVersion != 1 || freeze.Reason != "freeze before maintenance" {
		t.Fatalf("change freeze=%#v error=%v", freeze, err)
	}
	projection, err := NewWorkspaceService(repository, workspace)
	if err != nil {
		t.Fatal(err)
	}
	devops, err := projection.Workspace(ctx, 50)
	if err != nil || len(devops.ChangeFreezes) != 1 || len(devops.Executions) != 1 || devops.ChangeFreezes[0].Target != target {
		t.Fatalf("DevOps projection=%#v error=%v", devops, err)
	}

	preconditionCard := proposeChangeFreeze(t, ctx, workspace, runID, target, false, false, 1, "unfreeze after maintenance")
	preconditionCard, err = workspace.AuthorizeActionCard(ctx, preconditionCard.ID, agent.AuthorizeActionRequest{
		ExpectedHash: preconditionCard.ContentHash, Reason: "reviewed exact unfreeze action",
	})
	if err != nil {
		t.Fatal(err)
	}
	preconditionExecution, err := repository.EnqueueActionCard(ctx, preconditionCard.ID, ExecuteRequest{ExpectedHash: preconditionCard.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	lease, found, err = repository.Claim(ctx, "operation-test", time.Minute)
	if err != nil || !found {
		t.Fatalf("precondition claim found=%v error=%v", found, err)
	}
	subject, err = repository.SubjectForExecution(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Prepare(ctx, subject)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale precondition error=%v", err)
	}
	if err = repository.Fail(ctx, lease, err); err != nil {
		t.Fatal(err)
	}
	failed, err := repository.Execution(ctx, preconditionExecution.ID)
	if err != nil || failed.Status != "precondition_failed" || failed.FailureCode != "PRECONDITION_FAILED" {
		t.Fatalf("precondition execution=%#v error=%v", failed, err)
	}
	unchanged, err := repository.ChangeFreeze(ctx, target)
	if err != nil || !unchanged.Enabled || unchanged.RowVersion != 1 {
		t.Fatalf("precondition failure changed state=%#v error=%v", unchanged, err)
	}

	expiredCard := proposeChangeFreeze(t, ctx, workspace, runID, target, false, true, 1, "bounded expired action")
	expiredCard, err = workspace.AuthorizeActionCard(ctx, expiredCard.ID, agent.AuthorizeActionRequest{
		ExpectedHash: expiredCard.ContentHash, Reason: "reviewed before expiry",
	})
	if err != nil {
		t.Fatal(err)
	}
	repository.now = func() time.Time { return expiredCard.ExpiresAt.Add(time.Microsecond) }
	if _, err = repository.EnqueueActionCard(ctx, expiredCard.ID, ExecuteRequest{ExpectedHash: expiredCard.ContentHash}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired authorization error=%v", err)
	}
	repository.now = time.Now

	tamperedPlan := proposeScalePlan(t, ctx, workspace, runID, target, 2)
	tamperedPlan, err = workspace.AuthorizeOperationPlan(ctx, tamperedPlan.ID, agent.AuthorizeActionRequest{
		ExpectedHash: tamperedPlan.ContentHash, Reason: "reviewed exact scale plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_operation_plans SET parameters_json=JSON_OBJECT('replicas',3) WHERE public_id=?`, tamperedPlan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.EnqueueOperationPlan(ctx, tamperedPlan.ID, ExecuteRequest{ExpectedHash: tamperedPlan.ContentHash}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("materially changed Plan error=%v", err)
	}

	revisionPlan := proposeScalePlan(t, ctx, workspace, runID, target, 3)
	revisionPlan, err = workspace.AuthorizeOperationPlan(ctx, revisionPlan.ID, agent.AuthorizeActionRequest{
		ExpectedHash: revisionPlan.ContentHash, Reason: "reviewed plan under current revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	activateNewOperationRevision(t, ctx, db)
	if _, err = repository.EnqueueOperationPlan(ctx, revisionPlan.ID, ExecuteRequest{ExpectedHash: revisionPlan.ContentHash}); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("changed Configuration Revision error=%v", err)
	}
	assertOperationCount(t, ctx, db, "SELECT COUNT(*) FROM operation_executions", 2)
}

func proposeChangeFreeze(
	t *testing.T,
	ctx context.Context,
	workspace *agent.WorkspaceRepository,
	runID string,
	target OperationTarget,
	enabled, expectedEnabled bool,
	expectedVersion uint64,
	reason string,
) agent.ActionCard {
	t.Helper()
	targetJSON, _ := json.Marshal(target)
	parameters, _ := json.Marshal(map[string]any{"enabled": enabled, "reason": reason})
	preconditions, _ := json.Marshal([]map[string]any{{
		"type": "local.change_freeze", "expected_enabled": expectedEnabled, "expected_version": expectedVersion,
	}})
	card, err := workspace.ProposeActionCard(ctx, agent.ActionProposalRequest{
		RunID: runID, ActionType: ActionSetChangeFreeze, Target: targetJSON,
		Parameters: parameters, Preconditions: preconditions,
		Risk: "Changes only the bounded local change-freeze record.", ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func proposeScalePlan(
	t *testing.T,
	ctx context.Context,
	workspace *agent.WorkspaceRepository,
	runID string,
	target OperationTarget,
	replicas int32,
) agent.OperationPlan {
	t.Helper()
	targetJSON, _ := json.Marshal(target)
	parameters, _ := json.Marshal(map[string]any{"replicas": replicas})
	preconditions, _ := json.Marshal([]map[string]any{
		{"type": "deployment.replicas", "expected_replicas": 1},
		{"type": "deployment.resource_version", "expected_resource_version": "resource-version-1"},
		{"type": "local.change_freeze", "expected_enabled": false, "expected_version": 1},
	})
	verification, _ := json.Marshal(map[string]any{"type": ActionScaleDeployment, "expected_replicas": replicas})
	plan, err := workspace.ProposeOperationPlan(ctx, agent.ActionProposalRequest{
		RunID: runID, ActionType: ActionScaleDeployment, Target: targetJSON,
		Parameters: parameters, IntendedState: parameters, Preconditions: preconditions,
		Risk: "Changes one exact Deployment replica target.", VerificationIntent: verification,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func createOperationWorkspaceRun(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workspace *agent.WorkspaceRepository,
) (string, OperationTarget) {
	t.Helper()
	var target OperationTarget
	var namespacesJSON []byte
	if err := db.QueryRowContext(ctx, `SELECT scope.cluster_id,scope.environment,scope.namespaces_json
FROM active_configuration active JOIN operational_scopes scope ON scope.configuration_revision_id=active.configuration_revision_id
WHERE active.singleton_id=1`).Scan(&target.ClusterID, &target.Environment, &namespacesJSON); err != nil {
		t.Fatal(err)
	}
	var namespaces []string
	if err := json.Unmarshal(namespacesJSON, &namespaces); err != nil || len(namespaces) == 0 {
		t.Fatalf("namespaces=%v error=%v", namespaces, err)
	}
	target.Namespace, target.WorkloadKind, target.WorkloadName = namespaces[0], "Deployment", "cloudops-api"
	target.ScenarioID = "scenario-operation-integration"
	now := time.Now().UTC().Truncate(time.Microsecond)
	alertID := uuid.NewString()
	suffix := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO alerts
(public_id,source,alert_key,current_alert_instance_key,correlation_key,correlation_key_version,
 fingerprint,status,severity,cluster,environment,namespace,service_name,target_kind,target_name,
 category,summary,labels_json,annotations_json,first_seen_at,last_seen_at,starts_at)
VALUES (?,'prometheus',?,?,?,2,?,'firing','critical',?,?,?,?,?,'cloudops-api','availability',?,
 JSON_OBJECT('alertname','CloudOpsOperationIntegration'),JSON_OBJECT(),?,?,?)`, alertID,
		operationSHA256("alert-key-"+suffix), operationSHA256("instance-key-"+suffix),
		operationSHA256("correlation-key-"+suffix), "operation-integration-"+suffix,
		target.ClusterID, target.Environment, target.Namespace, target.WorkloadName, target.WorkloadKind,
		"CloudOps operation integration", now.Add(-10*time.Minute), now, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	run, err := workspace.StartAlertInvestigation(ctx, alertID, "operation-integration-"+suffix, "test controlled operation")
	if err != nil {
		t.Fatal(err)
	}
	return run.ID, target
}

func activateNewOperationRevision(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	result, err := db.ExecContext(ctx, `INSERT INTO configuration_revisions
(public_id,revision_number,configuration_hash,summary,query_max_lookback_seconds,query_max_results,
 telemetry_retention_days,browser_notifications_enabled,automatic_escalation_enabled,created_by)
SELECT ?,MAX(revision_number)+1,?,'Operation integration revision',86400,1000,7,0,0,'test'
FROM configuration_revisions`, uuid.NewString(), strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE active_configuration SET configuration_revision_id=? WHERE singleton_id=1`, id); err != nil {
		t.Fatal(err)
	}
}

func openOperationIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	name := fmt.Sprintf("cloudops_operation_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
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
	migrationCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	runner, err := migrationrunner.NewRunner(migrationCtx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.Up(migrationCtx); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertOperationCount(t *testing.T, ctx context.Context, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d query=%s", got, want, query)
	}
}

func operationSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
