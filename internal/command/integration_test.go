package command

import (
	"context"
	"database/sql"
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
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
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
