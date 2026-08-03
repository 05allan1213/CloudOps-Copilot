package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestMySQLCommandIdempotencyConcurrentSameAndDifferentPayload(t *testing.T) {
	db := openCommandIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE TABLE command_test_effects (
id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
token VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
PRIMARY KEY (id), UNIQUE KEY uk_command_test_effects_token (token)
) ENGINE=InnoDB`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("same key and payload returns one durable result", func(t *testing.T) {
		request := Request{
			ActorIdentityHash: strings.Repeat("a", 64),
			CommandScope:      "incident.close:123e4567-e89b-12d3-a456-426614174000",
			IdempotencyKey:    "command-same-payload",
			RequestHash:       strings.Repeat("b", 64),
		}
		const workers = 8
		start := make(chan struct{})
		responses := make([]Response, workers)
		replayed := make([]bool, workers)
		errorsByWorker := make([]error, workers)
		var callbacks atomic.Int32
		var wait sync.WaitGroup
		for index := 0; index < workers; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				responses[index], replayed[index], errorsByWorker[index] = store.Execute(ctx, request,
					func(ctx context.Context, tx *sql.Tx) (Response, error) {
						callbacks.Add(1)
						if _, err := tx.ExecContext(ctx, "INSERT INTO command_test_effects (token) VALUES ('same-payload')"); err != nil {
							return Response{}, err
						}
						time.Sleep(50 * time.Millisecond)
						return commandIntegrationResponse(`{"result":"same"}`), nil
					})
			}()
		}
		close(start)
		wait.Wait()
		replayCount := 0
		for index, workerErr := range errorsByWorker {
			if workerErr != nil {
				t.Fatalf("same-payload worker %d: %v", index, workerErr)
			}
			if responses[index].HTTPStatus != 202 || string(responses[index].Body) != `{"result":"same"}` {
				t.Fatalf("same-payload response %d=%+v", index, responses[index])
			}
			if replayed[index] {
				replayCount++
			}
		}
		if callbacks.Load() != 1 || replayCount != workers-1 {
			t.Fatalf("callbacks=%d replayed=%d", callbacks.Load(), replayCount)
		}
		assertCommandIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM command_test_effects WHERE token = 'same-payload'", 1)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM command_idempotency_records
WHERE actor_identity_hash = ? AND command_scope = ? AND idempotency_key = ?
  AND request_hash = ? AND status = 'completed' AND http_status = 202
  AND completed_at IS NOT NULL
  AND TIMESTAMPDIFF(SECOND, completed_at, expires_at) = 86400`, 1,
			request.ActorIdentityHash, request.CommandScope, request.IdempotencyKey, request.RequestHash)
	})

	t.Run("same key with different payload conflicts", func(t *testing.T) {
		base := Request{
			ActorIdentityHash: strings.Repeat("c", 64),
			CommandScope:      "incident.start:123e4567-e89b-12d3-a456-426614174000",
			IdempotencyKey:    "command-conflicting-payload",
		}
		requests := []Request{base, base}
		requests[0].RequestHash = strings.Repeat("d", 64)
		requests[1].RequestHash = strings.Repeat("e", 64)
		start := make(chan struct{})
		errorsByWorker := make([]error, len(requests))
		var callbacks atomic.Int32
		var wait sync.WaitGroup
		for index := range requests {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				_, _, errorsByWorker[index] = store.Execute(ctx, requests[index],
					func(ctx context.Context, tx *sql.Tx) (Response, error) {
						callbacks.Add(1)
						if _, err := tx.ExecContext(ctx, "INSERT INTO command_test_effects (token) VALUES ('different-payload')"); err != nil {
							return Response{}, err
						}
						time.Sleep(50 * time.Millisecond)
						return commandIntegrationResponse(`{"result":"winner"}`), nil
					})
			}()
		}
		close(start)
		wait.Wait()
		successes, conflicts := 0, 0
		for _, workerErr := range errorsByWorker {
			switch {
			case workerErr == nil:
				successes++
			case errors.Is(workerErr, ErrPayloadConflict):
				conflicts++
			default:
				t.Fatalf("different-payload result error=%v", workerErr)
			}
		}
		if successes != 1 || conflicts != 1 || callbacks.Load() != 1 {
			t.Fatalf("successes=%d conflicts=%d callbacks=%d", successes, conflicts, callbacks.Load())
		}
		assertCommandIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM command_test_effects WHERE token = 'different-payload'", 1)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM command_idempotency_records
WHERE actor_identity_hash = ? AND command_scope = ? AND idempotency_key = ? AND status = 'completed'`,
			1, base.ActorIdentityHash, base.CommandScope, base.IdempotencyKey)
	})

	t.Run("close requires completed recovery proof and preserves it", func(t *testing.T) {
		fixture := insertResolvedCloseFixture(t, ctx, db)
		result, err := db.ExecContext(ctx, `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
configuration_revision_id, dedupe_key, max_attempts, status
) VALUES (?, ?, 2, 'verify', 'verification.advance', 'verification_run', ?,
          'verification.advance', 1, 1, JSON_OBJECT(),
          (SELECT configuration_revision_id FROM active_configuration WHERE singleton_id = 1),
          ?, 3, 'ready')`, uuid.NewString(), fixture.incidentID,
			fixture.verificationID, strings.Repeat("f", 64))
		if err != nil {
			t.Fatal(err)
		}
		taskID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		port, err := NewPort(db, PortOptions{DeliveryEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		request := api.CommandRequest{
			Kind: api.CommandCloseIncident, ResourceID: fixture.publicID,
			Actor:          api.OwnerIdentity{Subject: "local-owner", Provider: "local", Login: "owner", Role: "owner"},
			IdempotencyKey: "close-running-task", ExpectedVersion: 5,
			CanonicalBody: []byte(`{"expected_version":5}`), RequestID: uuid.NewString(),
		}
		blocked, err := port.Execute(ctx, request)
		if !errors.Is(err, api.ErrInvalidTransition) || blocked.Replayed {
			t.Fatalf("close with active task result=%+v err=%v", blocked, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents
WHERE id = ? AND status = 'resolved' AND version = 5 AND resolved_at = ?`, 1,
			fixture.incidentID, fixture.resolvedAt)
		if _, err := db.ExecContext(ctx, `UPDATE async_tasks
SET status = 'cancelled', cancelled_at = NOW(6), updated_at = NOW(6)
WHERE id = ? AND status = 'ready'`, taskID); err != nil {
			t.Fatal(err)
		}
		var taskStatus string
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_tasks WHERE id = ?`, taskID).Scan(&taskStatus); err != nil {
			t.Fatal(err)
		}
		if taskStatus != "cancelled" {
			t.Fatalf("task status=%q", taskStatus)
		}

		request.IdempotencyKey = "close-after-recovery-proof"
		closed, err := port.Execute(ctx, request)
		if err != nil || closed.Status != "closed" || closed.Version != 6 || closed.Replayed {
			t.Fatalf("close result=%+v err=%v", closed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents
WHERE id = ? AND status = 'closed' AND version = 6 AND resolved_at = ?`, 1,
			fixture.incidentID, fixture.resolvedAt)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM verification_runs
WHERE public_id = ? AND status = 'passed' AND common_window_completed_at IS NOT NULL`, 1,
			fixture.verificationPublicID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM resolution_reports
WHERE public_id = ? AND incident_id = ? AND cycle_no = 2`, 1,
			fixture.reportPublicID, fixture.incidentID)
		request.RequestID = uuid.NewString()
		replayed, err := port.Execute(ctx, request)
		if err != nil || replayed.Status != "closed" || replayed.Version != closed.Version || !replayed.Replayed {
			t.Fatalf("idempotent close replay=%+v err=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
WHERE incident_id = ? AND cycle_no = 2 AND event_type = 'incident_closed'
  AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.verification_run_id')) = ?
  AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.resolution_report_id')) = ?
  AND JSON_EXTRACT(metadata_json, '$.resolved_history_preserved') = TRUE`, 1,
			fixture.incidentID, fixture.verificationPublicID, fixture.reportPublicID)
	})

	t.Run("close rejects an unrecovered Incident even when change work is terminal", func(t *testing.T) {
		publicID := uuid.NewString()
		result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, summary,
first_seen_at, last_seen_at, version, status, cycle_no
) VALUES (?, ?, ?, 1, 'kind', 'demo', 'checkout', 'demo', 'Deployment', 'checkout',
          'warning', 'close guard fixture', NOW(6), NOW(6), 1,
          'investigating', 1)`, publicID, "command-close-change-"+publicID, strings.Repeat("8", 64))
		if err != nil {
			t.Fatal(err)
		}
		incidentID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		planPublicID := uuid.NewString()
		planResult, err := db.ExecContext(ctx, `INSERT INTO remediation_plans (
public_id, incident_id, plan_version, plan_hash, operation_type,
target_repository, target_base_revision, target_path, parameters_json,
evidence_references_json, risk_level, policy_snapshot_hash, expected_before_hash,
proposed_patch_hash, patch_summary, rollback_plan, validation_plan, row_version,
cycle_no, status, hash_schema_version,
canonical_plan_hash, verification_plan_json
) VALUES (?, ?, 1, ?, 'rollback_image', 'owner/repo', ?,
          'apps/demo/deployment.yaml', '{}', '[]', 'low', ?, ?, ?, 'fixture',
          'fixture', 'fixture', 1, 1, 'consumed', 1, ?, '{}')`,
			planPublicID, incidentID, strings.Repeat("1", 64), strings.Repeat("2", 64),
			strings.Repeat("3", 64), strings.Repeat("4", 64), strings.Repeat("5", 64),
			strings.Repeat("6", 64))
		if err != nil {
			t.Fatal(err)
		}
		planID, err := planResult.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO change_requests (
public_id, plan_id, repository, base_revision, head_branch, ci_status,
idempotency_key, row_version, incident_id, cycle_no,
status, operation_step, expected_subject_version, logical_operation_key
) VALUES (?, ?, 'owner/repo', ?, 'cloudops/fixture', 'failing', ?, 1,
          ?, 1, 'failed', 'ensure_draft_pr', 1, ?)`,
			uuid.NewString(), planID, strings.Repeat("7", 64), strings.Repeat("8", 64),
			incidentID, strings.Repeat("9", 64)); err != nil {
			t.Fatal(err)
		}
		port, err := NewPort(db, PortOptions{DeliveryEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		request := api.CommandRequest{
			Kind: api.CommandCloseIncident, ResourceID: publicID,
			Actor:          api.OwnerIdentity{Subject: "local-owner", Provider: "local", Login: "owner", Role: "owner"},
			IdempotencyKey: "close-terminal-change", ExpectedVersion: 1,
			CanonicalBody: []byte(`{"expected_version":1}`), RequestID: uuid.NewString(),
		}
		first, err := port.Execute(ctx, request)
		if !errors.Is(err, api.ErrInvalidTransition) {
			t.Fatalf("close with terminal ChangeRequest error=%v", err)
		}
		if first.Replayed {
			t.Fatal("first invalid close was marked as a replay")
		}
		request.RequestID = uuid.NewString()
		replayed, err := port.Execute(ctx, request)
		if !errors.Is(err, api.ErrInvalidTransition) || !replayed.Replayed {
			t.Fatalf("durable invalid close replay=%+v error=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents
WHERE id = ? AND status = 'investigating' AND version = 1`, 1, incidentID)
	})
}

func TestMySQLInvestigationRetryAuthorizationIsDurableConcurrentAndHardBounded(t *testing.T) {
	db := openCommandIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	port, err := NewPort(db, PortOptions{DeliveryEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	actor := api.OwnerIdentity{Subject: "local-owner", Provider: "local", Login: "owner", Role: "owner"}

	t.Run("ordinary slots keep optional reason", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 0)
		request := newInvestigationStartRequest(publicID, 1, "ordinary-start", actor, "")
		if _, err := port.Execute(ctx, request); err != nil {
			t.Fatal(err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
		WHERE incident_id = ? AND transition = 'investigation.start'`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*)
		FROM agent_workspace_tasks task
		JOIN agent_runs run ON run.id = task.agent_run_id
		WHERE run.incident_id = ? AND run.run_kind='workspace' AND run.subject_type='incident'
		  AND run.status='pending' AND task.status='ready'`, 1, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents
		WHERE id=? AND status='investigating' AND version=2 AND current_agent_run_id IS NOT NULL`, 1, incidentID)
	})

	t.Run("ready legacy start is atomically superseded by Workspace", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 0)
		legacyPublicID := uuid.NewString()
		if _, err := db.ExecContext(ctx, `INSERT INTO async_tasks (
		 public_id,incident_id,cycle_no,queue,task_type,subject_type,subject_id,transition,
		 expected_subject_version,payload_schema_version,payload_json,configuration_revision_id,
		 dedupe_key,status,priority,available_at,max_attempts,created_at,updated_at
		) VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'incident', ?, 'investigation.start',
		          1, 1, JSON_OBJECT('mode','start','incident_public_id',?,'cycle_no',1),
		          (SELECT configuration_revision_id FROM active_configuration WHERE singleton_id=1),
		          ?, 'ready', 100, NOW(6), 5, NOW(6), NOW(6))`,
			legacyPublicID, incidentID, incidentID, publicID, canonicalHash("legacy-ready", publicID)); err != nil {
			t.Fatal(err)
		}

		request := newInvestigationStartRequest(publicID, 1, "workspace-supersedes-ready", actor, "use bounded local Workspace")
		accepted, err := port.Execute(ctx, request)
		if err != nil || accepted.Status != "investigation_started" || accepted.Version != 2 {
			t.Fatalf("superseded legacy start=%+v error=%v", accepted, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
		WHERE public_id=? AND status='cancelled' AND cancelled_at IS NOT NULL
		  AND last_error_code='superseded_by_workspace'`, 1, legacyPublicID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*)
		FROM agent_workspace_tasks task JOIN agent_runs run ON run.id=task.agent_run_id
		WHERE run.incident_id=? AND run.run_kind='workspace' AND task.status='ready'`, 1, incidentID)
	})

	t.Run("active run rejects a second start without orphan work", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 0)
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
	 (public_id, incident_id, model, prompt_version, max_steps, failure_code,
	  row_version, status, cycle_no, expected_incident_version)
	VALUES (?, ?, 'fixture-model', 'incident-investigation-fixture', 1, '', 1, 'pending', 1, 1)`,
			uuid.NewString(), incidentID); err != nil {
			t.Fatal(err)
		}

		request := newInvestigationStartRequest(publicID, 1, "active-run-conflict", actor, "wait for the active run")
		blocked, err := port.Execute(ctx, request)
		if !errors.Is(err, api.ErrConflict) || blocked.Replayed {
			t.Fatalf("active run command=%+v error=%v", blocked, err)
		}
		request.RequestID = uuid.NewString()
		replayed, err := port.Execute(ctx, request)
		if !errors.Is(err, api.ErrConflict) || !replayed.Replayed {
			t.Fatalf("active run replay=%+v error=%v", replayed, err)
		}

		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*)
		FROM agent_workspace_tasks task
		JOIN agent_runs run ON run.id = task.agent_run_id
		WHERE run.incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'investigation_requested'`, 0, incidentID)
	})

	t.Run("dead current investigation task is reconciled before a new business run", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 0)
		runPublicID := uuid.NewString()
		runResult, err := db.ExecContext(ctx, `INSERT INTO agent_runs
		 (public_id, incident_id, model, prompt_version, max_steps, failure_code,
		  row_version, status, cycle_no, expected_incident_version)
		VALUES (?, ?, 'fixture-model', 'incident-investigation-fixture', 1, '', 3, 'running', 1, 1)`,
			runPublicID, incidentID)
		if err != nil {
			t.Fatal(err)
		}
		runID, _ := runResult.LastInsertId()
		taskPublicID := uuid.NewString()
		if _, err := db.ExecContext(ctx, `INSERT INTO async_tasks
	 (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
	  expected_subject_version, payload_schema_version, payload_json, configuration_revision_id, dedupe_key, replay_generation,
	  status, priority, available_at, attempt, max_attempts, lease_generation,
	  last_error_code, last_error_summary, dead_at, created_at, updated_at)
	VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'agent_run', ?, 'investigation.step',
	        3, 1, JSON_OBJECT('mode','synthesize','agent_run_id',?,'cycle_no',1),
	        (SELECT configuration_revision_id FROM active_configuration WHERE singleton_id = 1), ?, 0,
	        'dead', 50, NOW(6), 1, 5, 1,
	        'invalid_agent_run_state', 'durable Evidence provenance is invalid', NOW(6), NOW(6), NOW(6))`,
			taskPublicID, incidentID, runID, runPublicID, canonicalHash("dead-investigation", taskPublicID)); err != nil {
			t.Fatal(err)
		}

		request := newInvestigationStartRequest(publicID, 1, "reconcile-dead-run", actor, "retry after terminal task failure")
		accepted, err := port.Execute(ctx, request)
		if err != nil || accepted.Status != "investigation_started" || accepted.Version != 2 {
			t.Fatalf("reconciled investigation command=%+v error=%v", accepted, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
		WHERE id = ? AND status = 'failed' AND outcome = 'failed' AND row_version = 4
		  AND failure_code = 'invalid_agent_run_state'`, 1, runID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
		WHERE incident_id = ? AND transition = 'investigation.start'`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*)
		FROM agent_workspace_tasks task
		JOIN agent_runs run ON run.id = task.agent_run_id
		WHERE run.incident_id = ? AND run.run_kind='workspace' AND task.status='ready'`, 1, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
		WHERE incident_id = ? AND event_type = 'agent_run_failed'`, 1, incidentID)
	})

	t.Run("live technical replay keeps the active run conflict", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 0)
		runPublicID := uuid.NewString()
		runResult, err := db.ExecContext(ctx, `INSERT INTO agent_runs
		 (public_id, incident_id, model, prompt_version, max_steps, failure_code,
		  row_version, status, cycle_no, expected_incident_version)
		VALUES (?, ?, 'fixture-model', 'incident-investigation-fixture', 1, '', 3, 'running', 1, 1)`,
			runPublicID, incidentID)
		if err != nil {
			t.Fatal(err)
		}
		runID, _ := runResult.LastInsertId()
		dedupe := canonicalHash("replayed-investigation", runPublicID)
		deadResult, err := db.ExecContext(ctx, `INSERT INTO async_tasks
	 (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
	  expected_subject_version, payload_schema_version, payload_json, configuration_revision_id, dedupe_key, replay_generation,
	  status, priority, available_at, attempt, max_attempts, lease_generation,
	  last_error_code, last_error_summary, dead_at, created_at, updated_at)
	VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'agent_run', ?, 'investigation.step',
	        3, 1, JSON_OBJECT('mode','synthesize','agent_run_id',?,'cycle_no',1),
	        (SELECT configuration_revision_id FROM active_configuration WHERE singleton_id = 1), ?, 0,
	        'dead', 50, NOW(6), 1, 5, 1, 'dependency_failed', 'retryable failure', NOW(6), NOW(6), NOW(6))`,
			uuid.NewString(), incidentID, runID, runPublicID, dedupe)
		if err != nil {
			t.Fatal(err)
		}
		deadTaskID, _ := deadResult.LastInsertId()
		if _, err := db.ExecContext(ctx, `INSERT INTO async_tasks
	 (public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id, transition,
	  expected_subject_version, payload_schema_version, payload_json, configuration_revision_id, dedupe_key, replay_generation,
	  status, priority, available_at, attempt, max_attempts, lease_generation, replayed_from_task_id,
	  created_at, updated_at)
	VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'agent_run', ?, 'investigation.step',
	        3, 1, JSON_OBJECT('mode','synthesize','agent_run_id',?,'cycle_no',1),
	        (SELECT configuration_revision_id FROM active_configuration WHERE singleton_id = 1), ?, 1,
	        'ready', 50, NOW(6), 0, 5, 0, ?, NOW(6), NOW(6))`,
			uuid.NewString(), incidentID, runID, runPublicID, dedupe, deadTaskID); err != nil {
			t.Fatal(err)
		}

		request := newInvestigationStartRequest(publicID, 1, "live-replay-conflict", actor, "wait for technical replay")
		if _, err := port.Execute(ctx, request); !errors.Is(err, api.ErrConflict) {
			t.Fatalf("live replay start error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
		WHERE id = ? AND status = 'running' AND row_version = 3`, 1, runID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
		WHERE incident_id = ? AND transition = 'investigation.start'`, 0, incidentID)
	})

	t.Run("concurrent slot four creates one Decision and one task", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 3)
		requests := []api.CommandRequest{
			newInvestigationStartRequest(publicID, 1, "retry-slot-four-a", actor, "Owner reviewed failed cycle evidence"),
			newInvestigationStartRequest(publicID, 1, "retry-slot-four-b", actor, "Owner approved one bounded retry"),
		}
		start := make(chan struct{})
		errs := make([]error, len(requests))
		results := make([]api.CommandResult, len(requests))
		var wait sync.WaitGroup
		for index := range requests {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				<-start
				results[index], errs[index] = port.Execute(ctx, requests[index])
			}(index)
		}
		close(start)
		wait.Wait()
		successes, rejected := 0, 0
		successfulRequest := -1
		for index, commandErr := range errs {
			switch {
			case commandErr == nil:
				successes++
				successfulRequest = index
			case errors.Is(commandErr, api.ErrConflict), errors.Is(commandErr, api.ErrStaleVersion):
				rejected++
			default:
				t.Fatalf("unexpected concurrent retry error: %v", commandErr)
			}
		}
		if successes != 1 || rejected != 1 {
			t.Fatalf("successes=%d rejected=%d results=%+v", successes, rejected, results)
		}
		replayed, err := port.Execute(ctx, requests[successfulRequest])
		if err != nil || !replayed.Replayed {
			t.Fatalf("authorization command replay=%+v err=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ? AND cycle_no = 1 AND slot_no = 4`, 1, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
		WHERE incident_id = ? AND transition = 'investigation.start'`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*)
		FROM agent_workspace_tasks task
		JOIN agent_runs run ON run.id = task.agent_run_id
		WHERE run.incident_id = ? AND run.run_kind='workspace' AND task.status='ready'`, 1, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'agent_run_retry_authorized'`, 1, incidentID)
		var reason, authorizationPublicID string
		if err := db.QueryRowContext(ctx, `SELECT reason, public_id FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, incidentID).Scan(&reason, &authorizationPublicID); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(reason) == "" {
			t.Fatal("retry authorization reason is empty")
		}
		var runAuthorizationPublicID string
		if err := db.QueryRowContext(ctx, `SELECT authorization.public_id
		FROM agent_runs run
		JOIN incident_cycle_budget_authorizations authorization
		  ON authorization.id=run.business_budget_authorization_id
		WHERE run.incident_id=? AND run.cycle_no=1 AND run.run_kind='workspace'`, incidentID).Scan(&runAuthorizationPublicID); err != nil {
			t.Fatal(err)
		}
		if runAuthorizationPublicID != authorizationPublicID {
			t.Fatalf("Investigation Workspace authorization=%q, want %q", runAuthorizationPublicID, authorizationPublicID)
		}

		missingIncidentID, missingPublicID := insertCommandBudgetIncident(t, ctx, db, 3)
		missing := newInvestigationStartRequest(missingPublicID, 1, "retry-slot-four-missing-reason", actor, "")
		if _, err := port.Execute(ctx, missing); !errors.Is(err, api.ErrInvalidArgument) {
			t.Fatalf("missing retry reason error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 0, missingIncidentID)
	})

	t.Run("imported authorization cannot unlock a current retry", func(t *testing.T) {
		incidentID, _ := insertCommandBudgetIncident(t, ctx, db, 3)
		authorizationPublicID := uuid.NewString()
		if _, err := db.ExecContext(ctx, `INSERT INTO incident_cycle_budget_authorizations (
public_id, authorization_schema_version, incident_id, cycle_no, budget_kind, slot_no,
actor_provider, actor_login, actor_role, imported_history, reason, request_id,
request_authenticated_at, created_at
) VALUES (?, 1, ?, 1, 'agent_run', 4, 'local', 'owner', 'owner', TRUE,
          'historical approval retained during semantic import', ?, NOW(6), NOW(6))`,
			authorizationPublicID, incidentID, uuid.NewString()); err != nil {
			t.Fatal(err)
		}

		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := businessbudget.GuardAgentRun(ctx, tx, incidentID, 1, authorizationPublicID); !errors.Is(err, businessbudget.ErrInvalidAuthorization) {
			t.Fatalf("imported authorization guard error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations
WHERE public_id = ? AND imported_history = TRUE`, 1, authorizationPublicID)
	})

	t.Run("slot six rejects and writes no task", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 5)
		request := newInvestigationStartRequest(publicID, 1, "retry-slot-six", actor, "Owner requested another bounded retry")
		if _, err := port.Execute(ctx, request); !errors.Is(err, api.ErrInvalidTransition) {
			t.Fatalf("slot six error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*)
		FROM agent_workspace_tasks task
		JOIN agent_runs run ON run.id = task.agent_run_id
		WHERE run.incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'agent_run_hard_limit_exhausted'`, 1, incidentID)
		var attention bool
		var version uint64
		if err := db.QueryRowContext(ctx, `SELECT needs_attention, version FROM incidents WHERE id = ?`, incidentID).Scan(&attention, &version); err != nil {
			t.Fatal(err)
		}
		if !attention || version != 2 {
			t.Fatalf("hard exhaustion attention=%v version=%d", attention, version)
		}
	})
}

func TestMySQLRemediationDecisionCommandIsAtomicAndFenced(t *testing.T) {
	db := openCommandIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	port, err := NewPort(db, PortOptions{DeliveryEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	owner := api.OwnerIdentity{Subject: "local-owner", Provider: "local", Login: "owner", Role: "owner"}

	t.Run("approval and delivery task commit atomically", func(t *testing.T) {
		fixture := insertCommandRemediationFixture(t, ctx, db)
		request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "approve-atomic", owner, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER command_fail_change_enqueue
BEFORE INSERT ON async_tasks FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'forced change enqueue failure'`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = db.Exec("DROP TRIGGER IF EXISTS command_fail_change_enqueue") })

		if _, err := port.Execute(ctx, request); !errors.Is(err, api.ErrUnavailable) {
			t.Fatalf("approval with failed enqueue error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_plans WHERE id = ? AND status = 'awaiting_approval' AND row_version = ?`, 1, fixture.plan.ID, fixture.plan.RowVersion)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM command_idempotency_records WHERE command_scope = ? AND idempotency_key = ?`, 0, string(api.CommandDecideRemediation)+":"+fixture.plan.PublicID, request.IdempotencyKey)

		if _, err := db.ExecContext(ctx, "DROP TRIGGER command_fail_change_enqueue"); err != nil {
			t.Fatal(err)
		}
		approved, err := port.Execute(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if approved.Status != string(remediation.DecisionApproved) || approved.Version != fixture.plan.RowVersion+1 || approved.Cycle != fixture.plan.CycleNo || approved.Replayed {
			t.Fatalf("approval result=%+v", approved)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ? AND decision = 'approved'`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_plans WHERE id = ? AND status = 'approved' AND row_version = ?`, 1, fixture.plan.ID, fixture.plan.RowVersion+1)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND cycle_no = ? AND event_type = 'remediation_plan_approved'`, 1, fixture.incidentID, fixture.plan.CycleNo)

		var taskType, subjectType, transition, status, payloadPlanID string
		var subjectID, expectedVersion uint64
		if err := db.QueryRowContext(ctx, `SELECT task_type, subject_type, subject_id, transition,
expected_subject_version, status, JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.plan_id'))
FROM async_tasks WHERE incident_id = ? AND cycle_no = ? AND subject_type = 'remediation_plan' AND subject_id = ?`,
			fixture.incidentID, fixture.plan.CycleNo, fixture.plan.ID).
			Scan(&taskType, &subjectType, &subjectID, &transition, &expectedVersion, &status, &payloadPlanID); err != nil {
			t.Fatal(err)
		}
		if taskType != string(asyncjob.TaskChangeEnsurePR) || subjectType != "remediation_plan" || subjectID != fixture.plan.ID || transition != "change.ensure_pr" || expectedVersion != fixture.plan.RowVersion+1 || status != string(asyncjob.StatusReady) || payloadPlanID != fixture.plan.PublicID {
			t.Fatalf("approval task=%q/%q/%d/%q version=%d status=%q plan=%q", taskType, subjectType, subjectID, transition, expectedVersion, status, payloadPlanID)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents WHERE id = ? AND status = 'awaiting_approval' AND version = ?`, 1, fixture.incidentID, fixture.plan.IncidentVersion+1)

		var authenticatedAt, createdAt, decisionExpiresAt, planExpiresAt time.Time
		if err := db.QueryRowContext(ctx, `SELECT d.request_authenticated_at, d.created_at, d.expires_at, p.expires_at
FROM remediation_decisions d JOIN remediation_plans p ON p.id = d.plan_id WHERE d.plan_id = ?`, fixture.plan.ID).
			Scan(&authenticatedAt, &createdAt, &decisionExpiresAt, &planExpiresAt); err != nil {
			t.Fatal(err)
		}
		if !authenticatedAt.Equal(createdAt) || !decisionExpiresAt.After(createdAt) || decisionExpiresAt.After(planExpiresAt) || decisionExpiresAt.Sub(createdAt) > remediationDecisionTTL {
			t.Fatalf("decision times authenticated=%s created=%s decision_expiry=%s plan_expiry=%s", authenticatedAt, createdAt, decisionExpiresAt, planExpiresAt)
		}

		replayed, err := port.Execute(ctx, request)
		if err != nil || !replayed.Replayed || replayed.Status != approved.Status || replayed.Version != approved.Version {
			t.Fatalf("approval replay=%+v err=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 1, fixture.plan.ID)
	})

	t.Run("rejection persists decision without delivery work", func(t *testing.T) {
		fixture := insertCommandRemediationFixture(t, ctx, db)
		request := newRemediationDecisionRequest(fixture, remediation.DecisionRejected, "reject-no-delivery", owner, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
		rejected, err := port.Execute(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if rejected.Status != string(remediation.DecisionRejected) || rejected.Version != fixture.plan.RowVersion+1 || rejected.Cycle != fixture.plan.CycleNo {
			t.Fatalf("rejection result=%+v", rejected)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ? AND decision = 'rejected'`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_plans WHERE id = ? AND status = 'rejected' AND row_version = ?`, 1, fixture.plan.ID, fixture.plan.RowVersion+1)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents WHERE id = ? AND status = 'investigating' AND version = ?`, 1, fixture.incidentID, fixture.plan.IncidentVersion+2)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND cycle_no = ? AND event_type = 'remediation_plan_rejected'`, 1, fixture.incidentID, fixture.plan.CycleNo)
	})

	t.Run("approval persists and defers delivery when external writes are disabled", func(t *testing.T) {
		disabledPort, err := NewPort(db)
		if err != nil {
			t.Fatal(err)
		}
		fixture := insertCommandRemediationFixture(t, ctx, db)
		request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "approve-deferred", owner, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
		approved, err := disabledPort.Execute(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if approved.Status != string(remediation.DecisionApproved) || approved.Version != fixture.plan.RowVersion+1 {
			t.Fatalf("deferred approval result=%+v", approved)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ? AND decision = 'approved'`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ? AND transition = 'change.ensure_pr'`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND cycle_no = ? AND event_type = 'remediation_delivery_deferred' AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.reason')) = 'github_write_disabled'`, 1, fixture.incidentID, fixture.plan.CycleNo)
	})

	t.Run("stale version and hash fail closed", func(t *testing.T) {
		for _, test := range []struct {
			name            string
			expectedVersion uint64
			expectedHash    string
		}{
			{name: "version", expectedVersion: 2},
			{name: "hash", expectedVersion: 1, expectedHash: strings.Repeat("f", 64)},
		} {
			t.Run(test.name, func(t *testing.T) {
				fixture := insertCommandRemediationFixture(t, ctx, db)
				expectedVersion := test.expectedVersion
				if expectedVersion == 0 {
					expectedVersion = fixture.plan.RowVersion
				}
				expectedHash := test.expectedHash
				if expectedHash == "" {
					expectedHash = fixture.plan.CanonicalPlanHash
				}
				request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "stale-"+test.name, owner, expectedVersion, expectedHash)
				if _, err := port.Execute(ctx, request); !errors.Is(err, api.ErrStaleVersion) {
					t.Fatalf("stale %s error=%v", test.name, err)
				}
				assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
				assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
			})
		}
	})

	t.Run("expired plan and non-Owner identity fail closed", func(t *testing.T) {
		t.Run("expired", func(t *testing.T) {
			fixture := insertCommandRemediationFixture(t, ctx, db)
			expireCommandRemediationPlan(t, ctx, db, &fixture)
			request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "expired-plan", owner, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
			if _, err := port.Execute(ctx, request); !errors.Is(err, api.ErrConflict) {
				t.Fatalf("expired plan error=%v", err)
			}
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		})

		t.Run("identity", func(t *testing.T) {
			fixture := insertCommandRemediationFixture(t, ctx, db)
			untrusted := api.OwnerIdentity{Subject: "github:operator", Provider: "github", Login: "operator", Role: "operator"}
			request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "imported-identity", untrusted, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
			if _, err := port.Execute(ctx, request); !errors.Is(err, api.ErrForbidden) {
				t.Fatalf("non-Owner identity error=%v", err)
			}
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		})
	})

	t.Run("imported decision is retained but unavailable to current runtime", func(t *testing.T) {
		fixture := insertCommandRemediationFixture(t, ctx, db)
		var databaseNow time.Time
		if err := db.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
			t.Fatal(err)
		}
		decision, err := remediation.NewApproval(
			fixture.plan, "local", "owner", "owner", "historical exact-plan approval",
			uuid.NewString(), databaseNow.UTC(), databaseNow.UTC().Add(5*time.Minute),
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO remediation_decisions (
public_id, decision_schema_version, incident_id, cycle_no, plan_id, plan_version,
decision, actor_provider, actor_login, actor_role, imported_history, reason,
request_id, request_authenticated_at, expires_at, approved_hash_schema_version,
approved_plan_hash, approved_base_sha, approved_post_image_hash, approved_tree_hash,
approved_patch_hash, approved_policy_hash, approved_verification_hash,
approved_evidence_set_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			decision.PublicID, decision.DecisionSchemaVersion, decision.IncidentID,
			decision.CycleNo, decision.PlanID, decision.PlanVersion, decision.Decision,
			decision.ActorProvider, decision.Actor, decision.Role, decision.Reason,
			decision.RequestID, decision.RequestAuthenticatedAt, decision.ExpiresAt,
			decision.ApprovedHashSchemaVersion, decision.ApprovedPlanHash,
			decision.ApprovedBaseSHA, decision.ApprovedPostImageHash,
			decision.ApprovedTreeHash, decision.ApprovedPatchHash,
			decision.ApprovedPolicyHash, decision.ApprovedVerificationHash,
			decision.ApprovedEvidenceSetHash, decision.CreatedAt); err != nil {
			t.Fatal(err)
		}

		repository, err := remediationmysql.NewRepository(db)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.GetDecision(ctx, fixture.plan.PublicID); !errors.Is(err, remediation.ErrNotFound) {
			t.Fatalf("imported remediation Decision read error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions
WHERE plan_id = ? AND imported_history = TRUE`, 1, fixture.plan.ID)
	})

	t.Run("concurrent approval creates one decision and one task", func(t *testing.T) {
		fixture := insertCommandRemediationFixture(t, ctx, db)
		const workers = 8
		start := make(chan struct{})
		results := make([]api.CommandResult, workers)
		errorsByWorker := make([]error, workers)
		var wait sync.WaitGroup
		for index := 0; index < workers; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, fmt.Sprintf("concurrent-approve-%d", index), owner, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
				results[index], errorsByWorker[index] = port.Execute(ctx, request)
			}()
		}
		close(start)
		wait.Wait()
		successes, stale := 0, 0
		for index, workerErr := range errorsByWorker {
			switch {
			case workerErr == nil:
				successes++
				if results[index].Status != string(remediation.DecisionApproved) {
					t.Fatalf("winner result=%+v", results[index])
				}
			case errors.Is(workerErr, api.ErrStaleVersion):
				stale++
			default:
				t.Fatalf("concurrent worker %d error=%v", index, workerErr)
			}
		}
		if successes != 1 || stale != workers-1 {
			t.Fatalf("concurrent approvals successes=%d stale=%d", successes, stale)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND cycle_no = ? AND event_type = 'remediation_plan_approved'`, 1, fixture.incidentID, fixture.plan.CycleNo)
	})
}

type resolvedCloseFixture struct {
	incidentID           uint64
	publicID             string
	verificationID       uint64
	verificationPublicID string
	reportPublicID       string
	resolvedAt           time.Time
}

func insertResolvedCloseFixture(t *testing.T, ctx context.Context, db *sql.DB) resolvedCloseFixture {
	t.Helper()
	var databaseNow time.Time
	if err := db.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
		t.Fatal(err)
	}
	resolvedAt := databaseNow.UTC().Truncate(time.Microsecond)
	cycleStartedAt := resolvedAt.Add(-5 * time.Minute)
	commonStartedAt := resolvedAt.Add(-time.Minute)
	publicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, summary,
first_seen_at, last_seen_at, resolved_at, terminal_at, version, status, cycle_no
) VALUES (?, ?, ?, 1, 'cloudops-local', 'demo', 'checkout', 'local', 'Deployment',
          'checkout', 'warning', 'verified recovery ready to close', ?, ?, ?, ?, 5,
          'resolved', 2)`, publicID, "command-close-"+publicID,
		canonicalHash("command-close", publicID), cycleStartedAt, resolvedAt, resolvedAt, resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	incidentID64, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	incidentID := uint64(incidentID64)

	signalPublicID := uuid.NewString()
	result, err = db.ExecContext(ctx, `INSERT INTO incident_signals (
public_id, incident_id, cycle_no, source, source_event_id, canonical_schema_version,
correlation_key_version, fingerprint, alert_instance_key, status, severity, cluster,
namespace, service_name, environment, target_kind, target_name, category, occurred_at,
starts_at, ends_at, received_at, summary, labels_json, annotations_json
) VALUES (?, ?, 2, 'alertmanager', ?, 1, 1, ?, ?, 'resolved', 'warning',
          'cloudops-local', 'demo', 'checkout', 'local', 'Deployment', 'checkout',
          'availability', ?, ?, ?, ?, 'recovery signal', JSON_OBJECT(), JSON_OBJECT())`,
		signalPublicID, incidentID, "close-signal-"+signalPublicID,
		"close-signal-"+signalPublicID, strings.Repeat("1", 64), cycleStartedAt,
		cycleStartedAt, resolvedAt, resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	signalID64, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	signalID := uint64(signalID64)

	verificationPublicID := uuid.NewString()
	revision := strings.Repeat("a", 40)
	profileHash := strings.Repeat("b", 64)
	result, err = db.ExecContext(ctx, `INSERT INTO verification_runs (
public_id, incident_id, cycle_no, trigger_signal_id, status, trigger_type,
target_revision, source_revision, image_digest, gitops_revision, plan_json,
verification_profile_id, verification_profile_version, verification_profile_hash,
verification_contract_version, common_stability_window_ms, common_success_since,
common_window_completed_at, started_at, deadline_at, completed_at, attempt,
expected_subject_version, result_summary, failure_reason
) VALUES (?, ?, 2, ?, 'passed', 'no_change_signal', ?, ?, ?, ?, JSON_OBJECT(),
          'no-change/v1', 1, ?, 1, 60000, ?, ?, ?, ?, ?, 1, 5,
          'all required checks passed for the common window', '')`,
		verificationPublicID, incidentID, signalID, revision, revision,
		"sha256:"+strings.Repeat("c", 64), revision, profileHash, commonStartedAt,
		resolvedAt, commonStartedAt, resolvedAt.Add(time.Minute), resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	verificationID64, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	verificationID := uint64(verificationID64)

	reportPublicID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO resolution_reports (
public_id, report_schema_version, incident_id, cycle_no, verification_run_id,
initial_signal_id, trigger_signal_id, trigger_type, resolution_reason, service,
workload, environment, impact_summary, cycle_started_at, resolved_at,
measured_duration_ms, source_revision, image_digest, gitops_revision,
verification_profile_id, verification_profile_hash, common_window_started_at,
common_window_completed_at, trigger_signal_json, evidence_json, verification_json,
timeline_json, agent_usage_json, summary, content_hash, generated_at
) VALUES (?, 1, ?, 2, ?, ?, ?, 'no_change_signal', 'recovered_without_change',
          'checkout', 'checkout', 'local', 'availability recovered without delivery',
          ?, ?, 300000, ?, ?, ?, 'no-change/v1', ?, ?, ?,
          JSON_OBJECT('public_id', ?), JSON_OBJECT('evidence_count', 0),
          JSON_OBJECT('public_id', ?), JSON_OBJECT('event_count', 1),
          JSON_OBJECT('run_count', 0), 'verified recovery proof', ?, ?)`,
		reportPublicID, incidentID, verificationID, signalID, signalID, cycleStartedAt,
		resolvedAt, revision, "sha256:"+strings.Repeat("c", 64), revision, profileHash,
		commonStartedAt, resolvedAt, signalPublicID, verificationPublicID,
		strings.Repeat("d", 64), resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	return resolvedCloseFixture{
		incidentID: incidentID, publicID: publicID, verificationID: verificationID,
		verificationPublicID: verificationPublicID, reportPublicID: reportPublicID,
		resolvedAt: resolvedAt,
	}
}

type commandRemediationFixture struct {
	incidentID uint64
	plan       remediation.RemediationPlan
}

func insertCommandRemediationFixture(t *testing.T, ctx context.Context, db *sql.DB) commandRemediationFixture {
	t.Helper()
	var databaseNow time.Time
	if err := db.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
		t.Fatal(err)
	}
	incidentPublicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, summary,
first_seen_at, last_seen_at, version, status, cycle_no
) VALUES (?, ?, ?, 1, 'kind', 'demo', 'demo', 'development', 'Deployment',
          'demo', 'warning', 'command remediation fixture', NOW(6),
          NOW(6), 2, 'investigating', 1)`, incidentPublicID,
		"command-remediation-"+incidentPublicID, canonicalHash("command-remediation", incidentPublicID))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	agentRunPublicID := uuid.NewString()
	diagnosisHash := strings.Repeat("d", 64)
	diagnosisJSON, _ := json.Marshal(map[string]any{"diagnosis_hash": diagnosisHash})
	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, model, prompt_version, max_steps, final_diagnosis,
failure_code, completed_at, status, cycle_no,
expected_incident_version
) VALUES (?, ?, 'fixture-model', 'incident-investigation-fixture', 1, ?, '',
	          NOW(6), 'completed', 1, 2)`, agentRunPublicID, incidentID, diagnosisJSON)
	if err != nil {
		t.Fatal(err)
	}
	agentRunID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	evidencePublicID := uuid.NewString()
	evidenceHash := strings.Repeat("1", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items (
public_id, incident_id, agent_run_id, type, source, producer_type,
producer_dedupe_key, resource_ref, query_text, summary, facts_json, result_hash,
content_hash, raw_ref, truncated, valid, collected_at, cycle_no
) VALUES (?, ?, ?, 'configuration', 'github', 'agent_step', ?,
          'github://acme/gitops/apps/demo.yaml', 'exact blob', 'verified baseline node',
          JSON_OBJECT('required_env','healthy'), ?, ?, '', FALSE, TRUE, NOW(6), 1)`,
		evidencePublicID, incidentID, agentRunID, "command-evidence-"+evidencePublicID, evidenceHash, evidenceHash); err != nil {
		t.Fatal(err)
	}
	current := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: demo
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
		AllowedPath: "apps/demo.yaml", APIVersion: "apps/v1", Namespace: "demo",
		Workload: "demo", Container: "demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: remediation.MaxPlanDiffBytes, MaxPostImageBytes: remediation.MaxPostImageBytes,
		VerificationVersion: "golden-required-env/v1",
	}
	createdAt := databaseNow.UTC().Add(-time.Minute)
	plan, err := remediation.CompileRestoreRequiredEnv(remediation.RestoreEnvCompileRequest{
		IncidentPublicID: incidentPublicID, IncidentID: uint64(incidentID), CycleNo: 1, IncidentVersion: 2,
		CreatedByAgentRunID: agentRunPublicID, DiagnosisHash: diagnosisHash,
		Repository: policy.Repository, BaseBranch: policy.BaseBranch, BaseRevision: strings.Repeat("a", 40),
		LastKnownGoodRevision: strings.Repeat("b", 40), TargetPath: policy.AllowedPath,
		BaseBlobSHA: strings.Repeat("c", 40), ExpectedTreeHash: strings.Repeat("e", 40), FileMode: "100644",
		Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "demo", Container: "demo"},
		EnvKey: "REQUIRED_ENV", CurrentContent: current, BaselineContent: baseline, Policy: policy,
		VerificationPlan:   json.RawMessage(`{"profile":"golden-required-env/v1","stability_window_seconds":60}`),
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
	updated, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'awaiting_approval', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = 1 AND version = 2 AND status = 'investigating'`, incidentID)
	if err != nil {
		t.Fatal(err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		t.Fatalf("approval fixture Incident transition affected=%d", affected)
	}
	return commandRemediationFixture{incidentID: uint64(incidentID), plan: plan}
}

func newRemediationDecisionRequest(fixture commandRemediationFixture, decision remediation.Decision, key string, actor api.OwnerIdentity, expectedVersion uint64, expectedHash string) api.CommandRequest {
	reason := "reviewed exact immutable plan"
	if decision == remediation.DecisionRejected {
		reason = "rejected after exact plan review"
	}
	body, _ := json.Marshal(struct {
		Decision        string `json:"decision"`
		ExpectedVersion uint64 `json:"expected_version"`
		ExpectedHash    string `json:"expected_hash"`
		Reason          string `json:"reason"`
	}{Decision: string(decision), ExpectedVersion: expectedVersion, ExpectedHash: expectedHash, Reason: reason})
	return api.CommandRequest{
		Kind: api.CommandDecideRemediation, ResourceID: fixture.plan.PublicID,
		Actor: actor, IdempotencyKey: key, ExpectedVersion: expectedVersion,
		ExpectedHash: expectedHash, CanonicalBody: body, RequestID: uuid.NewString(),
	}
}

func expireCommandRemediationPlan(t *testing.T, ctx context.Context, db *sql.DB, fixture *commandRemediationFixture) {
	t.Helper()
	fixture.plan.ExpiresAt = fixture.plan.CreatedAt.Add(30 * time.Second)
	hash, err := remediation.CanonicalPlanHash(fixture.plan)
	if err != nil {
		t.Fatal(err)
	}
	fixture.plan.PlanHash = hash
	fixture.plan.CanonicalPlanHash = hash
	if _, err := db.ExecContext(ctx, `UPDATE remediation_plans
SET expires_at = ?, plan_hash = ?, canonical_plan_hash = ?, updated_at = NOW(6)
WHERE id = ? AND status = 'awaiting_approval'`, fixture.plan.ExpiresAt, hash, hash, fixture.plan.ID); err != nil {
		t.Fatal(err)
	}
}

func commandIntegrationResponse(body string) Response {
	return Response{
		HTTPStatus:       202,
		Body:             []byte(body),
		ResourceType:     "incident",
		ResourcePublicID: "123e4567-e89b-12d3-a456-426614174000",
	}
}

func openCommandIntegrationDB(t *testing.T) *sql.DB {
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
	name := fmt.Sprintf("cloudops_command_%d", time.Now().UnixNano())
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
	config.DBName = name
	config.ParseTime = true
	config.MultiStatements = true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	runner, err := migrationrunner.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertCommandIntegrationCount(t *testing.T, ctx context.Context, db *sql.DB, query string, expected int, args ...any) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("count=%d want=%d query=%s", count, expected, query)
	}
}

func insertCommandBudgetIncident(t *testing.T, ctx context.Context, db *sql.DB, agentRuns int) (uint64, string) {
	t.Helper()
	publicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, summary,
first_seen_at, last_seen_at, version, status, cycle_no
) VALUES (?, ?, ?, 1, 'cloudops-local', 'demo', 'checkout', 'local', 'Deployment', 'checkout',
          'warning', 'business budget command fixture', NOW(6), NOW(6), 1,
	          'investigating', 1)`, publicID, "budget-command-"+publicID, canonicalHash("budget-command", publicID))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < agentRuns; index++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
	 (public_id, incident_id, model, prompt_version, max_steps, failure_code,
	  completed_at, row_version, status, cycle_no, expected_incident_version)
	VALUES (?, ?, 'fixture-model', 'incident-investigation-fixture', 1, '', NOW(6), 1, 'completed', 1, 1)`,
			uuid.NewString(), incidentID); err != nil {
			t.Fatal(err)
		}
	}
	return uint64(incidentID), publicID
}

func newInvestigationStartRequest(publicID string, expectedVersion uint64, key string, actor api.OwnerIdentity, reason string) api.CommandRequest {
	body, _ := json.Marshal(struct {
		ExpectedVersion uint64 `json:"expected_version"`
		Reason          string `json:"reason,omitempty"`
	}{ExpectedVersion: expectedVersion, Reason: reason})
	return api.CommandRequest{
		Kind: api.CommandStartInvestigation, ResourceID: publicID, Actor: actor,
		IdempotencyKey: key, ExpectedVersion: expectedVersion, CanonicalBody: body,
		RequestID: uuid.NewString(),
	}
}
