package taskhandler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

func TestMySQLInvestigationStepPersistsStableExecutionErrorCode(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openVerificationIntegrationDB(t, adminDSN)
	defer closeVerificationIntegrationDB(t, "investigation step admin", admin)
	databaseName := fmt.Sprintf("cloudops_investigation_step_%d", time.Now().UnixNano())
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
	defer closeVerificationIntegrationDB(t, "investigation step", db)
	migrations, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	incidentPublicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents
	 (public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
	  service_name, environment, target_kind, target_name, severity, summary,
	  first_seen_at, last_seen_at, version, status, cycle_no)
	VALUES (?, ?, ?, 2, 'kind', 'demo', 'demo', 'development', 'Deployment',
	        'demo', 'warning', 'investigation error fixture', ?, ?, 2, 'investigating', 1)`,
		incidentPublicID, "investigation-step-"+uuid.NewString(), strings.Repeat("a", 64), now.Add(-2*time.Minute), now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	agentRunPublicID := uuid.NewString()
	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs
	 (public_id, incident_id, model, prompt_version, max_steps, final_diagnosis,
	  failure_code, row_version, status, cycle_no, expected_incident_version)
	VALUES (?, ?, 'fixture-model', 'incident-investigation-fixture', 10, NULL, '', 1, 'running', 1, 2)`,
		agentRunPublicID, incidentID)
	if err != nil {
		t.Fatal(err)
	}
	agentRunID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	snapshot.Task.PublicID = uuid.NewString()
	snapshot.Task.IncidentID = uint64(incidentID)
	snapshot.Task.SubjectID = uint64(agentRunID)
	snapshot.Task.DedupeKey = hashCanonical("task", agentRunPublicID, "investigation.step", "1")
	snapshot.RunPublicID = agentRunPublicID
	snapshot.IncidentPublicID = incidentPublicID
	snapshot.State.RunID = agentRunPublicID
	snapshot.State.IncidentID = incidentPublicID
	payload, err := json.Marshal(investigationStepPayload{
		Mode: stepModeDecide, AgentRunID: agentRunPublicID, CycleNo: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Task.Payload = payload
	providerDetail := "credential-shaped provider detail must not persist"
	model := &twoCallStepModel{stepTestModel: stepTestModel{proposeErr: agent.NewRuntimeError(
		agent.ErrorMalformedModel, providerDetail, agent.ErrInvalidArgument,
	)}}
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, model, &stepTestTool{}, store)
	operationResult := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if operationResult.Disposition != asyncjob.DispositionSucceeded || operationResult.Mutate == nil {
		t.Fatalf("operation result=%+v", operationResult)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := operationResult.Mutate(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var errorCode, summary, outcome string
	if err := db.QueryRowContext(ctx, `SELECT step.error_code, step.result_summary, run.outcome
	FROM agent_steps step JOIN agent_runs run ON run.id = step.agent_run_id
	WHERE step.agent_run_id = ?`, agentRunID).
		Scan(&errorCode, &summary, &outcome); err != nil {
		t.Fatal(err)
	}
	if errorCode != "step_execution_malformed_model_output" || outcome != "insufficient" || strings.Contains(summary, providerDetail) {
		t.Fatalf("persisted error_code=%q outcome=%q summary=%q", errorCode, outcome, summary)
	}
}
