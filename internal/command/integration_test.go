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

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
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
			IdempotencyKey:    "phase2-same-payload",
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
			IdempotencyKey:    "phase2-conflicting-payload",
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

	t.Run("close atomically fences running task and attempt", func(t *testing.T) {
		publicID := uuid.NewString()
		result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no
) VALUES (?, ?, ?, 2, 'kind', 'demo', 'checkout', 'demo', 'Deployment', 'checkout',
          'warning', 'DIAGNOSING', 'command close fixture', NOW(6), NOW(6), 1, 3,
          'investigating', 1)`, publicID, "command-close-"+publicID, "v2:"+strings.Repeat("9", 64))
		if err != nil {
			t.Fatal(err)
		}
		incidentID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		queue, err := asyncjob.NewRepository(db)
		if err != nil {
			t.Fatal(err)
		}
		task, err := queue.Enqueue(ctx, asyncjob.NewTask{
			IncidentID: uint64(incidentID), CycleNo: 1, Type: asyncjob.TaskInvestigationAdvance,
			SubjectType: "incident", SubjectID: uint64(incidentID), Transition: "investigation.start",
			ExpectedSubjectVersion: 1, PayloadSchemaVersion: 1, Payload: []byte(`{"mode":"start"}`),
			DedupeKey: strings.Repeat("f", 64), MaxAttempts: 3,
		})
		if err != nil {
			t.Fatal(err)
		}
		execution, err := queue.ClaimReady(ctx, asyncjob.ClaimRequest{Queue: asyncjob.QueueInvestigate, Owner: "command-close-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		port, err := NewPort(db)
		if err != nil {
			t.Fatal(err)
		}
		request := apiv3.CommandRequest{
			Kind: apiv3.CommandCloseIncident, ResourceID: publicID,
			Actor:          apiv3.Identity{Subject: "github:operator", Provider: "github", Login: "operator", Role: "operator"},
			IdempotencyKey: "close-running-task", ExpectedVersion: 1,
			CanonicalBody: []byte(`{"expected_version":1}`), RequestID: uuid.NewString(),
		}
		closed, err := port.Execute(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if closed.Status != "closed" || closed.Version != 2 {
			t.Fatalf("close result=%+v", closed)
		}
		var taskStatus, attemptStatus string
		var generation uint64
		if err := db.QueryRowContext(ctx, `SELECT status, lease_generation FROM async_tasks WHERE id = ?`, task.ID).Scan(&taskStatus, &generation); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_task_attempts WHERE task_id = ? AND attempt = ?`, task.ID, execution.Lease.Attempt).Scan(&attemptStatus); err != nil {
			t.Fatal(err)
		}
		if taskStatus != "cancelled" || attemptStatus != "cancelled" || generation != execution.Lease.Generation+1 {
			t.Fatalf("task/attempt/generation=%q/%q/%d", taskStatus, attemptStatus, generation)
		}
		request.RequestID = uuid.NewString()
		replayed, err := port.Execute(ctx, request)
		if err != nil || replayed.Status != "closed" || replayed.Version != closed.Version || !replayed.Replayed {
			t.Fatalf("idempotent close replay=%+v err=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'incident_closed'`, 1, incidentID)
	})

	t.Run("close rejects any existing change request including terminal", func(t *testing.T) {
		publicID := uuid.NewString()
		result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
service_name, environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no
) VALUES (?, ?, ?, 2, 'kind', 'demo', 'checkout', 'demo', 'Deployment', 'checkout',
          'warning', 'DIAGNOSING', 'close guard fixture', NOW(6), NOW(6), 1, 3,
          'investigating', 1)`, publicID, "command-close-change-"+publicID, "v2:"+strings.Repeat("8", 64))
		if err != nil {
			t.Fatal(err)
		}
		incidentID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		planPublicID := uuid.NewString()
		planResult, err := db.ExecContext(ctx, `INSERT INTO remediation_plans (
public_id, incident_id, plan_version, plan_hash, status, operation_type,
target_repository, target_base_revision, target_path, parameters_json,
evidence_references_json, risk_level, policy_snapshot_hash, expected_before_hash,
proposed_patch_hash, patch_summary, rollback_plan, validation_plan, row_version,
domain_schema_version, cycle_no, v3_status, hash_schema_version,
canonical_plan_hash, verification_plan_json
) VALUES (?, ?, 1, ?, 'delivery_pending', 'rollback_image', 'owner/repo', ?,
          'apps/demo/deployment.yaml', '{}', '[]', 'low', ?, ?, ?, 'fixture',
          'fixture', 'fixture', 1, 3, 1, 'consumed', 1, ?, '{}')`,
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
public_id, plan_id, repository, base_revision, head_branch, status, ci_status,
idempotency_key, row_version, domain_schema_version, incident_id, cycle_no,
v3_status, write_phase, expected_subject_version, logical_operation_key
) VALUES (?, ?, 'owner/repo', ?, 'cloudops/fixture', 'failed', 'failing', ?, 1,
          3, ?, 1, 'failed', 'ensure_draft_pr', 1, ?)`,
			uuid.NewString(), planID, strings.Repeat("7", 64), strings.Repeat("8", 64),
			incidentID, strings.Repeat("9", 64)); err != nil {
			t.Fatal(err)
		}
		port, err := NewPort(db)
		if err != nil {
			t.Fatal(err)
		}
		request := apiv3.CommandRequest{
			Kind: apiv3.CommandCloseIncident, ResourceID: publicID,
			Actor:          apiv3.Identity{Subject: "github:operator", Provider: "github", Login: "operator", Role: "operator"},
			IdempotencyKey: "close-terminal-change", ExpectedVersion: 1,
			CanonicalBody: []byte(`{"expected_version":1}`), RequestID: uuid.NewString(),
		}
		first, err := port.Execute(ctx, request)
		if !errors.Is(err, apiv3.ErrInvalidTransition) {
			t.Fatalf("close with terminal ChangeRequest error=%v", err)
		}
		if first.Replayed {
			t.Fatal("first invalid close was marked as a replay")
		}
		request.RequestID = uuid.NewString()
		replayed, err := port.Execute(ctx, request)
		if !errors.Is(err, apiv3.ErrInvalidTransition) || !replayed.Replayed {
			t.Fatalf("durable invalid close replay=%+v error=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents
WHERE id = ? AND v3_status = 'investigating' AND version = 1`, 1, incidentID)
	})
}

func TestMySQLInvestigationRetryAuthorizationIsDurableConcurrentAndHardBounded(t *testing.T) {
	db := openCommandIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	port, err := NewPort(db)
	if err != nil {
		t.Fatal(err)
	}
	actor := apiv3.Identity{Subject: "github:operator", Provider: "github", Login: "operator", Role: "operator"}

	t.Run("ordinary slots keep optional reason", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 0)
		request := newInvestigationStartRequest(publicID, 1, "ordinary-start", actor, "")
		if _, err := port.Execute(ctx, request); err != nil {
			t.Fatal(err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'`, 1, incidentID)
	})

	t.Run("concurrent slot four creates one Decision and one task", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 3)
		requests := []apiv3.CommandRequest{
			newInvestigationStartRequest(publicID, 1, "retry-slot-four-a", actor, "operator reviewed failed cycle evidence"),
			newInvestigationStartRequest(publicID, 1, "retry-slot-four-b", actor, "operator approved one bounded retry"),
		}
		start := make(chan struct{})
		errs := make([]error, len(requests))
		results := make([]apiv3.CommandResult, len(requests))
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
		successes, conflicts := 0, 0
		successfulRequest := -1
		for index, commandErr := range errs {
			switch {
			case commandErr == nil:
				successes++
				successfulRequest = index
			case errors.Is(commandErr, apiv3.ErrConflict):
				conflicts++
			default:
				t.Fatalf("unexpected concurrent retry error: %v", commandErr)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("successes=%d conflicts=%d results=%+v", successes, conflicts, results)
		}
		replayed, err := port.Execute(ctx, requests[successfulRequest])
		if err != nil || !replayed.Replayed {
			t.Fatalf("authorization command replay=%+v err=%v", replayed, err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ? AND cycle_no = 1 AND slot_no = 4`, 1, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'ready'`, 1, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'agent_run_retry_authorized'`, 1, incidentID)
		var reason, authorizationPublicID string
		if err := db.QueryRowContext(ctx, `SELECT reason, public_id FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, incidentID).Scan(&reason, &authorizationPublicID); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(reason) == "" {
			t.Fatal("retry authorization reason is empty")
		}
		var payload []byte
		if err := db.QueryRowContext(ctx, `SELECT payload_json FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'`, incidentID).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(payload), authorizationPublicID) {
			t.Fatalf("task payload does not reference durable authorization: %s", payload)
		}

		missing := newInvestigationStartRequest(publicID, 1, "retry-slot-four-missing-reason", actor, "")
		if _, err := port.Execute(ctx, missing); !errors.Is(err, apiv3.ErrInvalidArgument) {
			t.Fatalf("missing retry reason error=%v", err)
		}
	})

	t.Run("slot six rejects and writes no task", func(t *testing.T) {
		incidentID, publicID := insertCommandBudgetIncident(t, ctx, db, 5)
		request := newInvestigationStartRequest(publicID, 1, "retry-slot-six", actor, "operator requested another bounded retry")
		if _, err := port.Execute(ctx, request); !errors.Is(err, apiv3.ErrInvalidTransition) {
			t.Fatalf("slot six error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 0, incidentID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'`, 0, incidentID)
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
	port, err := NewPort(db)
	if err != nil {
		t.Fatal(err)
	}
	operator := apiv3.Identity{Subject: "github:operator", Provider: "github", Login: "operator", Role: "operator"}

	t.Run("approval and delivery task commit atomically", func(t *testing.T) {
		fixture := insertCommandRemediationFixture(t, ctx, db)
		request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "approve-atomic", operator, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER command_fail_change_enqueue
BEFORE INSERT ON async_tasks FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'forced change enqueue failure'`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = db.Exec("DROP TRIGGER IF EXISTS command_fail_change_enqueue") })

		if _, err := port.Execute(ctx, request); !errors.Is(err, apiv3.ErrUnavailable) {
			t.Fatalf("approval with failed enqueue error=%v", err)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_plans WHERE id = ? AND v3_status = 'awaiting_approval' AND row_version = ?`, 1, fixture.plan.ID, fixture.plan.RowVersion)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM command_idempotency_records WHERE command_scope = ? AND idempotency_key = ?`, 0, string(apiv3.CommandDecideRemediation)+":"+fixture.plan.PublicID, request.IdempotencyKey)

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
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_plans WHERE id = ? AND v3_status = 'approved' AND row_version = ?`, 1, fixture.plan.ID, fixture.plan.RowVersion+1)
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
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents WHERE id = ? AND v3_status = 'awaiting_approval' AND version = ?`, 1, fixture.incidentID, fixture.plan.IncidentVersion+1)

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
		request := newRemediationDecisionRequest(fixture, remediation.DecisionRejected, "reject-no-delivery", operator, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
		rejected, err := port.Execute(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if rejected.Status != string(remediation.DecisionRejected) || rejected.Version != fixture.plan.RowVersion+1 || rejected.Cycle != fixture.plan.CycleNo {
			t.Fatalf("rejection result=%+v", rejected)
		}
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ? AND decision = 'rejected'`, 1, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_plans WHERE id = ? AND v3_status = 'rejected' AND row_version = ?`, 1, fixture.plan.ID, fixture.plan.RowVersion+1)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents WHERE id = ? AND status = 'DIAGNOSING' AND v3_status = 'investigating' AND version = ?`, 1, fixture.incidentID, fixture.plan.IncidentVersion+2)
		assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND cycle_no = ? AND event_type = 'remediation_plan_rejected'`, 1, fixture.incidentID, fixture.plan.CycleNo)
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
				request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "stale-"+test.name, operator, expectedVersion, expectedHash)
				if _, err := port.Execute(ctx, request); !errors.Is(err, apiv3.ErrStaleVersion) {
					t.Fatalf("stale %s error=%v", test.name, err)
				}
				assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
				assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
			})
		}
	})

	t.Run("expired plan and non github identity fail closed", func(t *testing.T) {
		t.Run("expired", func(t *testing.T) {
			fixture := insertCommandRemediationFixture(t, ctx, db)
			expireCommandRemediationPlan(t, ctx, db, &fixture)
			request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "expired-plan", operator, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
			if _, err := port.Execute(ctx, request); !errors.Is(err, apiv3.ErrConflict) {
				t.Fatalf("expired plan error=%v", err)
			}
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		})

		t.Run("identity", func(t *testing.T) {
			fixture := insertCommandRemediationFixture(t, ctx, db)
			untrusted := apiv3.Identity{Subject: "local:operator", Provider: "local", Login: "operator", Role: "operator"}
			request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, "non-github", untrusted, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
			if _, err := port.Execute(ctx, request); !errors.Is(err, apiv3.ErrForbidden) {
				t.Fatalf("non-GitHub identity error=%v", err)
			}
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM remediation_decisions WHERE plan_id = ?`, 0, fixture.plan.ID)
			assertCommandIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE subject_type = 'remediation_plan' AND subject_id = ?`, 0, fixture.plan.ID)
		})
	})

	t.Run("concurrent approval creates one decision and one task", func(t *testing.T) {
		fixture := insertCommandRemediationFixture(t, ctx, db)
		const workers = 8
		start := make(chan struct{})
		results := make([]apiv3.CommandResult, workers)
		errorsByWorker := make([]error, workers)
		var wait sync.WaitGroup
		for index := 0; index < workers; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				request := newRemediationDecisionRequest(fixture, remediation.DecisionApproved, fmt.Sprintf("concurrent-approve-%d", index), operator, fixture.plan.RowVersion, fixture.plan.CanonicalPlanHash)
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
			case errors.Is(workerErr, apiv3.ErrStaleVersion):
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
service_name, environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no
) VALUES (?, ?, ?, 2, 'kind', 'demo', 'demo', 'development', 'Deployment',
          'demo', 'warning', 'DIAGNOSING', 'command remediation fixture', NOW(6),
          NOW(6), 2, 3, 'investigating', 1)`, incidentPublicID,
		"command-remediation-"+incidentPublicID, "v2:"+canonicalHash("command-remediation", incidentPublicID))
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
public_id, incident_id, status, model, prompt_version, max_steps, final_diagnosis,
failure_code, completed_at, domain_schema_version, v3_status, cycle_no,
expected_incident_version
) VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, ?, '',
          NOW(6), 3, 'completed', 1, 2)`, agentRunPublicID, incidentID, diagnosisJSON)
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
content_hash, raw_ref, truncated, valid, collected_at, domain_schema_version, cycle_no
) VALUES (?, ?, ?, 'configuration', 'github', 'agent_step', ?,
          'github://acme/gitops/apps/demo.yaml', 'exact blob', 'verified baseline node',
          JSON_OBJECT('required_env','healthy'), ?, ?, '', FALSE, TRUE, NOW(6), 3, 1)`,
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
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
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
	repository, err := remediationmysql.NewV3RemediationRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreatePlan(ctx, &plan); err != nil {
		t.Fatal(err)
	}
	updated, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'AWAITING_APPROVAL', v3_status = 'awaiting_approval', version = version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = 1 AND version = 2 AND v3_status = 'investigating'`, incidentID)
	if err != nil {
		t.Fatal(err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		t.Fatalf("approval fixture Incident transition affected=%d", affected)
	}
	return commandRemediationFixture{incidentID: uint64(incidentID), plan: plan}
}

func newRemediationDecisionRequest(fixture commandRemediationFixture, decision remediation.Decision, key string, actor apiv3.Identity, expectedVersion uint64, expectedHash string) apiv3.CommandRequest {
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
	return apiv3.CommandRequest{
		Kind: apiv3.CommandDecideRemediation, ResourceID: fixture.plan.PublicID,
		Actor: actor, IdempotencyKey: key, ExpectedVersion: expectedVersion,
		ExpectedHash: expectedHash, CanonicalBody: body, RequestID: uuid.NewString(),
	}
}

func expireCommandRemediationPlan(t *testing.T, ctx context.Context, db *sql.DB, fixture *commandRemediationFixture) {
	t.Helper()
	fixture.plan.ExpiresAt = fixture.plan.CreatedAt.Add(30 * time.Second)
	hash, err := remediation.CanonicalV3PlanHash(fixture.plan)
	if err != nil {
		t.Fatal(err)
	}
	fixture.plan.PlanHash = hash
	fixture.plan.CanonicalPlanHash = hash
	if _, err := db.ExecContext(ctx, `UPDATE remediation_plans
SET expires_at = ?, plan_hash = ?, canonical_plan_hash = ?, updated_at = NOW(6)
WHERE id = ? AND v3_status = 'awaiting_approval'`, fixture.plan.ExpiresAt, hash, hash, fixture.plan.ID); err != nil {
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
service_name, environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no
) VALUES (?, ?, ?, 2, 'kind', 'demo', 'checkout', 'demo', 'Deployment', 'checkout',
          'warning', 'DIAGNOSING', 'business budget command fixture', NOW(6), NOW(6), 1, 3,
          'investigating', 1)`, publicID, "budget-command-"+publicID, "v2:"+canonicalHash("budget-command", publicID))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < agentRuns; index++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, failure_code,
  completed_at, row_version, domain_schema_version, v3_status, cycle_no, expected_incident_version)
VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, '', NOW(6), 1, 3, 'completed', 1, 1)`,
			uuid.NewString(), incidentID); err != nil {
			t.Fatal(err)
		}
	}
	return uint64(incidentID), publicID
}

func newInvestigationStartRequest(publicID string, expectedVersion uint64, key string, actor apiv3.Identity, reason string) apiv3.CommandRequest {
	body, _ := json.Marshal(struct {
		ExpectedVersion uint64 `json:"expected_version"`
		Reason          string `json:"reason,omitempty"`
	}{ExpectedVersion: expectedVersion, Reason: reason})
	return apiv3.CommandRequest{
		Kind: apiv3.CommandStartInvestigation, ResourceID: publicID, Actor: actor,
		IdempotencyKey: key, ExpectedVersion: expectedVersion, CanonicalBody: body,
		RequestID: uuid.NewString(),
	}
}
