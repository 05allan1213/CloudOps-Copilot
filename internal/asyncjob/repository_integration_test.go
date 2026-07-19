package asyncjob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

func TestMySQLRepositoryQueueFencing(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := newAsyncJobDatabase(t, ctx, adminDSN)
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ready(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("concurrent workers claim distinct rows with SKIP LOCKED", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		const count = 12
		for index := range count {
			if _, err := repository.Enqueue(ctx, integrationTask(incidentID, fmt.Sprintf("claim-%d", index), 3)); err != nil {
				t.Fatal(err)
			}
		}
		start := make(chan struct{})
		executions := make(chan *Execution, count)
		errorsByWorker := make(chan error, count)
		var wait sync.WaitGroup
		for index := range count {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				execution, err := claimReadyEventually(ctx, repository, ClaimRequest{Queue: QueueInvestigate, Owner: fmt.Sprintf("worker-%d", index), LeaseDuration: 5 * time.Second})
				if err != nil {
					errorsByWorker <- err
					return
				}
				executions <- execution
			}()
		}
		close(start)
		wait.Wait()
		close(executions)
		close(errorsByWorker)
		for err := range errorsByWorker {
			t.Fatal(err)
		}
		seen := make(map[uint64]struct{}, count)
		for execution := range executions {
			if _, duplicate := seen[execution.Task.ID]; duplicate {
				t.Fatalf("task %d was claimed twice", execution.Task.ID)
			}
			seen[execution.Task.ID] = struct{}{}
			if err := repository.Resolve(ctx, execution.Lease, Succeeded(nil)); err != nil {
				t.Fatal(err)
			}
		}
		if len(seen) != count {
			t.Fatalf("claimed=%d, want %d", len(seen), count)
		}
		var succeeded, attempts int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND status = 'succeeded'`, incidentID).Scan(&succeeded); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_task_attempts a JOIN async_tasks t ON t.id = a.task_id WHERE t.incident_id = ?`, incidentID).Scan(&attempts); err != nil {
			t.Fatal(err)
		}
		if succeeded != count || attempts != count {
			t.Fatalf("succeeded=%d attempts=%d, want %d/%d", succeeded, attempts, count, count)
		}
	})

	t.Run("Incident first refresh does not deadlock a concurrent claim", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "incident-first-refresh", 3))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		var version uint64
		if err := tx.QueryRowContext(ctx, `SELECT version FROM incidents WHERE id = ? FOR UPDATE`, incidentID).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE incidents SET version = version + 1 WHERE id = ? AND version = ?`, incidentID, version); err != nil {
			t.Fatal(err)
		}

		claimResult := make(chan *Execution, 1)
		claimErrors := make(chan error, 1)
		go func() {
			execution, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "refresh-race", LeaseDuration: time.Second})
			claimResult <- execution
			claimErrors <- err
		}()
		time.Sleep(25 * time.Millisecond)
		refreshedDedupe := integrationHash(fmt.Sprintf("%d:incident-first-refresh:v2", incidentID))
		if _, err := tx.ExecContext(ctx, `UPDATE async_tasks
SET expected_subject_version = 2, dedupe_key = ?, updated_at = NOW(6)
WHERE id = ? AND status = 'ready' AND expected_subject_version = 1`, refreshedDedupe, created.ID); err != nil {
			t.Fatalf("refresh task while Incident is locked: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		execution := <-claimResult
		claimErr := <-claimErrors
		if claimErr != nil && !errors.Is(claimErr, ErrNoTask) {
			t.Fatalf("concurrent claim returned a lock-order error: %v", claimErr)
		}
		if execution == nil {
			execution, err = claimReadyEventually(ctx, repository, ClaimRequest{Queue: QueueInvestigate, Owner: "refresh-after", LeaseDuration: time.Second})
			if err != nil {
				t.Fatal(err)
			}
		}
		if execution.Task.ID != created.ID || execution.Lease.ExpectedSubjectVersion != 2 {
			t.Fatalf("refreshed claim task/version=%d/%d, want %d/2", execution.Task.ID, execution.Lease.ExpectedSubjectVersion, created.ID)
		}
		if err := repository.Resolve(ctx, execution.Lease, Succeeded(nil)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("concurrent child dead resolutions do not upgrade shared Incident locks", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		executions := make([]*Execution, 0, 2)
		for index := range 2 {
			runID := insertIntegrationAgentRun(t, ctx, db, incidentID, 1)
			created, err := repository.Enqueue(ctx, integrationAgentRunTask(incidentID, runID, fmt.Sprintf("concurrent-dead-%d", index), 3))
			if err != nil {
				t.Fatal(err)
			}
			execution, err := claimReadyEventually(ctx, repository, ClaimRequest{Queue: QueueInvestigate, Owner: fmt.Sprintf("dead-worker-%d", index), LeaseDuration: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if execution.Task.ID != created.ID {
				t.Fatalf("claimed task=%d, want %d", execution.Task.ID, created.ID)
			}
			executions = append(executions, execution)
		}

		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		var calls [2]atomic.Int32
		errorsByWorker := make(chan error, 2)
		for index, execution := range executions {
			go func() {
				mutation := func(ctx context.Context, _ DBTX) error {
					calls[index].Add(1)
					entered <- struct{}{}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				errorsByWorker <- repository.Resolve(ctx, execution.Lease, Dead("concurrent_dead", "concurrent dead resolution", mutation))
			}()
		}
		timer := time.NewTimer(100 * time.Millisecond)
		enteredCount := 0
	waitForDeadMutations:
		for enteredCount < 2 {
			select {
			case <-entered:
				enteredCount++
			case <-timer.C:
				break waitForDeadMutations
			}
		}
		if !timer.Stop() && enteredCount == 2 {
			<-timer.C
		}
		close(release)
		for range 2 {
			if err := <-errorsByWorker; err != nil {
				t.Fatal(err)
			}
		}
		for index, execution := range executions {
			if got := calls[index].Load(); got != 1 {
				t.Fatalf("dead mutation %d calls=%d, want one transaction attempt", index, got)
			}
			assertIntegrationTaskDead(t, ctx, db, execution.Task.ID, "concurrent_dead")
			assertIntegrationAttemptStatus(t, ctx, db, execution.Task.ID, "dead")
		}
		var incidentVersion uint64
		if err := db.QueryRowContext(ctx, `SELECT version FROM incidents WHERE id = ?`, incidentID).Scan(&incidentVersion); err != nil {
			t.Fatal(err)
		}
		if incidentVersion != 3 {
			t.Fatalf("Incident version=%d, want 3 after two atomic attention transitions", incidentVersion)
		}
	})

	t.Run("deadlock victim retries the complete resolution transaction", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `CREATE TABLE async_tx_retry_probe (
id INT NOT NULL PRIMARY KEY,
touched INT NOT NULL DEFAULT 0
) ENGINE=InnoDB`); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS async_tx_retry_probe`) }()
		if _, err := db.ExecContext(ctx, `INSERT INTO async_tx_retry_probe (id, touched) VALUES (1, 0), (2, 0)`); err != nil {
			t.Fatal(err)
		}

		executions := make([]*Execution, 0, 2)
		for index := range 2 {
			incidentID := insertIntegrationIncident(t, ctx, db)
			created, err := repository.Enqueue(ctx, integrationTask(incidentID, fmt.Sprintf("deadlock-retry-%d", index), 3))
			if err != nil {
				t.Fatal(err)
			}
			execution, err := claimReadyEventually(ctx, repository, ClaimRequest{Queue: QueueInvestigate, Owner: fmt.Sprintf("retry-worker-%d", index), LeaseDuration: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if execution.Task.ID != created.ID {
				t.Fatalf("claimed task=%d, want %d", execution.Task.ID, created.ID)
			}
			executions = append(executions, execution)
		}

		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		var calls [2]atomic.Int32
		errorsByWorker := make(chan error, 2)
		for index, execution := range executions {
			go func() {
				first, second := index+1, 2-index
				mutation := func(ctx context.Context, tx DBTX) error {
					attempt := calls[index].Add(1)
					if _, err := tx.ExecContext(ctx, `UPDATE async_tx_retry_probe SET touched = touched + 1 WHERE id = ?`, first); err != nil {
						return err
					}
					if attempt == 1 {
						entered <- struct{}{}
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					_, err := tx.ExecContext(ctx, `UPDATE async_tx_retry_probe SET touched = touched + 1 WHERE id = ?`, second)
					return err
				}
				errorsByWorker <- repository.Resolve(ctx, execution.Lease, Succeeded(mutation))
			}()
		}
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("concurrent transactions did not acquire opposite probe rows")
			}
		}
		close(release)
		for range 2 {
			if err := <-errorsByWorker; err != nil {
				t.Fatal(err)
			}
		}
		if total := calls[0].Load() + calls[1].Load(); total != 3 {
			t.Fatalf("resolution transaction attempts=%d, want one deadlock retry", total)
		}
		var firstTouched, secondTouched int
		if err := db.QueryRowContext(ctx, `SELECT
MAX(CASE WHEN id = 1 THEN touched ELSE 0 END),
MAX(CASE WHEN id = 2 THEN touched ELSE 0 END)
FROM async_tx_retry_probe`).Scan(&firstTouched, &secondTouched); err != nil {
			t.Fatal(err)
		}
		if firstTouched != 2 || secondTouched != 2 {
			t.Fatalf("probe touches=%d/%d, want 2/2", firstTouched, secondTouched)
		}
	})

	t.Run("expired takeover fences heartbeat checkpoint and completion", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "takeover", 3))
		if err != nil {
			t.Fatal(err)
		}
		first, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "worker-old", LeaseDuration: 30 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		waitUntilLeaseExpired(t, ctx, db, created.ID)
		second, err := repository.TakeoverExpired(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "worker-new", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if second.Task.ID != first.Task.ID || second.Lease.Generation != first.Lease.Generation+1 || second.Lease.Attempt != first.Lease.Attempt+1 {
			t.Fatalf("takeover=%+v first=%+v", second.Lease, first.Lease)
		}
		if err := repository.Heartbeat(ctx, first.Lease, time.Second); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("stale heartbeat error=%v", err)
		}
		checkpoint := integrationCheckpoint(1)
		if err := repository.Checkpoint(ctx, first.Lease, checkpoint, nil); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("stale checkpoint error=%v", err)
		}
		if err := repository.Resolve(ctx, first.Lease, Succeeded(nil)); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("stale completion error=%v", err)
		}
		if err := repository.Heartbeat(ctx, second.Lease, time.Second); err != nil {
			t.Fatal(err)
		}
		if err := repository.Resolve(ctx, second.Lease, Succeeded(nil)); err != nil {
			t.Fatal(err)
		}
		var firstStatus, secondStatus string
		if err := db.QueryRowContext(ctx, `SELECT
MAX(CASE WHEN attempt = 1 THEN status ELSE '' END),
MAX(CASE WHEN attempt = 2 THEN status ELSE '' END)
FROM async_task_attempts WHERE task_id = ?`, created.ID).Scan(&firstStatus, &secondStatus); err != nil {
			t.Fatal(err)
		}
		if firstStatus != "lease_expired" || secondStatus != "succeeded" {
			t.Fatalf("attempt statuses=%q/%q", firstStatus, secondStatus)
		}
	})

	t.Run("retry exhaustion becomes dead and stale replay is rejected", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "replay", 2))
		if err != nil {
			t.Fatal(err)
		}
		first, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "retry-one", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.Resolve(ctx, first.Lease, RetryAfter(2*time.Millisecond, "transient", "first transient failure", nil)); err != nil {
			t.Fatal(err)
		}
		second, err := claimReadyEventually(ctx, repository, ClaimRequest{Queue: QueueInvestigate, Owner: "retry-two", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.Resolve(ctx, second.Lease, RetryAfter(time.Second, "transient", "attempt budget exhausted", nil)); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_tasks WHERE id = ?`, created.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "dead" {
			t.Fatalf("status=%q, want dead", status)
		}
		assertIntegrationAttentionAndAttempt(t, ctx, db, incidentID, created.ID, "task_attempts_exhausted", "dead")

		request := ReplayRequest{DeadTaskID: created.ID, ExpectedSubjectVersion: 1, ValidateSubject: incidentReplayValidator(incidentID)}
		if _, err := repository.Replay(ctx, request); !errors.Is(err, ErrSubjectVersionMismatch) {
			t.Fatalf("stale Incident replay error=%v", err)
		}
		var generations int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_tasks WHERE dedupe_key = ?`, created.DedupeKey).Scan(&generations); err != nil {
			t.Fatal(err)
		}
		if generations != 1 {
			t.Fatalf("stale task generations=%d, want original only", generations)
		}
	})

	t.Run("current child subject dead replay is one generation", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		runID := insertIntegrationAgentRun(t, ctx, db, incidentID, 1)
		created, err := repository.Enqueue(ctx, integrationAgentRunTask(incidentID, runID, "child-replay", 2))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "child-dead", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.Resolve(ctx, execution.Lease, Dead("child_failed", "child task requires replay", nil)); err != nil {
			t.Fatal(err)
		}
		assertIntegrationAttentionAndAttempt(t, ctx, db, incidentID, created.ID, "task_dead", "dead")

		request := ReplayRequest{DeadTaskID: created.ID, ExpectedSubjectVersion: 1, ValidateSubject: agentRunReplayValidator(runID)}
		const callers = 2
		results := make(chan *Task, callers)
		replayErrors := make(chan error, callers)
		var wait sync.WaitGroup
		for range callers {
			wait.Add(1)
			go func() {
				defer wait.Done()
				task, err := repository.Replay(ctx, request)
				if err != nil {
					replayErrors <- err
					return
				}
				results <- task
			}()
		}
		wait.Wait()
		close(results)
		close(replayErrors)
		for err := range replayErrors {
			t.Fatal(err)
		}
		var replayID uint64
		for replayed := range results {
			if replayID == 0 {
				replayID = replayed.ID
			}
			if replayed.ID != replayID || replayed.ExpectedSubjectVersion != 1 || replayed.ReplayGeneration != 1 || replayed.ReplayedFromTaskID == nil || *replayed.ReplayedFromTaskID != created.ID {
				t.Fatalf("unexpected replay row: %+v", replayed)
			}
		}
		var generations int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_tasks WHERE dedupe_key = ?`, created.DedupeKey).Scan(&generations); err != nil {
			t.Fatal(err)
		}
		if generations != 2 {
			t.Fatalf("task generations=%d, want 2", generations)
		}
		if err := repository.Cancel(ctx, replayID, 1, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE agent_runs SET row_version = row_version + 1 WHERE id = ?`, runID); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.Replay(ctx, request); !errors.Is(err, ErrSubjectVersionMismatch) {
			t.Fatalf("stale child replay error=%v", err)
		}
	})

	t.Run("explicit dead result atomically blocks Incident and appends event", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "explicit-dead", 3))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "explicit-dead-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.Resolve(ctx, execution.Lease, Dead("invalid_task_subject", "poison task cannot progress", nil)); err != nil {
			t.Fatal(err)
		}
		assertIntegrationTaskDead(t, ctx, db, created.ID, "invalid_task_subject")
		assertIntegrationAttentionAndAttempt(t, ctx, db, incidentID, created.ID, "task_dead", "dead")
	})

	t.Run("dead transition rolls back when Incident event append fails", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "dead-event-rollback", 3))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "dead-rollback-worker", LeaseDuration: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_async_task_dead
BEFORE INSERT ON incident_events FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject async task dead event'`); err != nil {
			t.Fatal(err)
		}
		triggerActive := true
		defer func() {
			if triggerActive {
				_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS reject_async_task_dead`)
			}
		}()
		if err := repository.Resolve(ctx, execution.Lease, Dead("poison", "must roll back", nil)); err == nil {
			t.Fatal("dead resolution succeeded while Incident event trigger rejected the insert")
		}
		if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_async_task_dead`); err != nil {
			t.Fatal(err)
		}
		triggerActive = false
		var taskStatus, attemptStatus string
		var attention bool
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_tasks WHERE id = ?`, created.ID).Scan(&taskStatus); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_task_attempts WHERE task_id = ? AND attempt = ?`, created.ID, execution.Lease.Attempt).Scan(&attemptStatus); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT needs_attention FROM incidents WHERE id = ?`, incidentID).Scan(&attention); err != nil {
			t.Fatal(err)
		}
		if taskStatus != "running" || attemptStatus != "running" || attention {
			t.Fatalf("rollback task/attempt/attention=%q/%q/%v, want running/running/false", taskStatus, attemptStatus, attention)
		}
		if err := repository.Resolve(ctx, execution.Lease, Dead("poison", "retry after rollback", nil)); err != nil {
			t.Fatal(err)
		}
		assertIntegrationAttentionAndAttempt(t, ctx, db, incidentID, created.ID, "task_dead", "dead")
	})

	t.Run("stale running subject is fenced to dead before handler mutation", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "checkpoint", 3))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "checkpoint-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := integrationCheckpoint(1)
		if err := repository.Checkpoint(ctx, execution.Lease, checkpoint, nil); err != nil {
			t.Fatal(err)
		}
		var version uint64
		var hash string
		if err := db.QueryRowContext(ctx, `SELECT checkpoint_version, checkpoint_hash FROM async_tasks WHERE id = ?`, created.ID).Scan(&version, &hash); err != nil {
			t.Fatal(err)
		}
		if version != checkpoint.Version || hash != checkpoint.Hash {
			t.Fatalf("checkpoint version/hash=%d/%q", version, hash)
		}

		if _, err := db.ExecContext(ctx, `UPDATE incidents SET version = version + 1 WHERE id = ?`, incidentID); err != nil {
			t.Fatal(err)
		}
		if err := repository.Heartbeat(ctx, execution.Lease, time.Second); !errors.Is(err, ErrSubjectVersionMismatch) {
			t.Fatalf("stale-subject heartbeat error=%v", err)
		}
		if err := repository.Checkpoint(ctx, execution.Lease, integrationCheckpoint(2), nil); !errors.Is(err, ErrSubjectVersionMismatch) {
			t.Fatalf("stale-subject checkpoint error=%v", err)
		}
		called := false
		mutation := func(ctx context.Context, tx DBTX) error {
			called = true
			return nil
		}
		if err := repository.Resolve(ctx, execution.Lease, Succeeded(mutation)); err != nil {
			t.Fatalf("stale-subject resolution error=%v", err)
		}
		if called {
			t.Fatal("stale subject invoked the domain mutation")
		}
		assertIntegrationTaskDead(t, ctx, db, created.ID, "subject_version_mismatch")
		assertIntegrationAttentionAndAttempt(t, ctx, db, incidentID, created.ID, "task_subject_version_mismatch", "dead")
	})

	t.Run("deterministic mutation rejection rolls back domain writes", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "mutation-rejection", 3))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "mutation-rejection-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		mutation := func(ctx context.Context, tx DBTX) error {
			if _, err := tx.ExecContext(ctx, `UPDATE incidents SET summary = 'must roll back' WHERE id = ?`, incidentID); err != nil {
				return err
			}
			return fmt.Errorf("%w: rejected operation", ErrInvalidMutation)
		}
		if err := repository.Resolve(ctx, execution.Lease, Succeeded(mutation)); err != nil {
			t.Fatalf("deterministic mutation resolution error=%v", err)
		}
		var taskStatus, summary string
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_tasks WHERE id = ?`, created.ID).Scan(&taskStatus); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT summary FROM incidents WHERE id = ?`, incidentID).Scan(&summary); err != nil {
			t.Fatal(err)
		}
		if taskStatus != "dead" || summary == "must roll back" {
			t.Fatalf("task status=%q summary=%q, want dead and original summary", taskStatus, summary)
		}
		assertIntegrationTaskDead(t, ctx, db, created.ID, "invalid_mutation")
		assertIntegrationAttentionAndAttempt(t, ctx, db, incidentID, created.ID, "task_dead", "dead")
	})

	t.Run("stale ready and expired tasks become dead without execution", func(t *testing.T) {
		readyIncidentID := insertIntegrationIncident(t, ctx, db)
		ready, err := repository.Enqueue(ctx, integrationTask(readyIncidentID, "stale-ready", 3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE incidents SET version = version + 1 WHERE id = ?`, readyIncidentID); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "stale-ready-worker", LeaseDuration: time.Second}); !errors.Is(err, ErrNoTask) {
			t.Fatalf("stale ready claim error=%v", err)
		}
		assertIntegrationTaskDead(t, ctx, db, ready.ID, "subject_version_mismatch")
		assertIntegrationAttentionAndEvent(t, ctx, db, readyIncidentID, ready.ID, "task_subject_version_mismatch")

		expiredIncidentID := insertIntegrationIncident(t, ctx, db)
		expired, err := repository.Enqueue(ctx, integrationTask(expiredIncidentID, "stale-expired", 3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "stale-expired-old", LeaseDuration: 30 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE incidents SET version = version + 1 WHERE id = ?`, expiredIncidentID); err != nil {
			t.Fatal(err)
		}
		waitUntilLeaseExpired(t, ctx, db, expired.ID)
		if _, err := repository.TakeoverExpired(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "stale-expired-new", LeaseDuration: time.Second}); !errors.Is(err, ErrNoTask) {
			t.Fatalf("stale expired takeover error=%v", err)
		}
		assertIntegrationTaskDead(t, ctx, db, expired.ID, "subject_version_mismatch")
		assertIntegrationAttentionAndEvent(t, ctx, db, expiredIncidentID, expired.ID, "task_subject_version_mismatch")
	})

	t.Run("exhausted ready and expired tasks record attempts and Incident attention", func(t *testing.T) {
		readyIncidentID := insertIntegrationIncident(t, ctx, db)
		ready, err := repository.Enqueue(ctx, integrationTask(readyIncidentID, "exhausted-ready", 1))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE async_tasks SET attempt = max_attempts WHERE id = ?`, ready.ID); err != nil {
			t.Fatal(err)
		}
		reaped, err := repository.ReapExhaustedReady(ctx, QueueInvestigate)
		if err != nil || !reaped {
			t.Fatalf("reap exhausted ready=%v err=%v", reaped, err)
		}
		assertIntegrationTaskDead(t, ctx, db, ready.ID, "attempts_exhausted")
		assertIntegrationAttentionAndAttempt(t, ctx, db, readyIncidentID, ready.ID, "task_attempts_exhausted", "dead")

		expiredIncidentID := insertIntegrationIncident(t, ctx, db)
		expired, err := repository.Enqueue(ctx, integrationTask(expiredIncidentID, "exhausted-expired", 1))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "exhausted-expired-old", LeaseDuration: 30 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		waitUntilLeaseExpired(t, ctx, db, expired.ID)
		if _, err := repository.TakeoverExpired(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "exhausted-expired-new", LeaseDuration: time.Second}); !errors.Is(err, ErrNoTask) {
			t.Fatalf("exhausted expired takeover error=%v", err)
		}
		assertIntegrationTaskDead(t, ctx, db, expired.ID, "lease_expired")
		assertIntegrationAttentionAndAttempt(t, ctx, db, expiredIncidentID, expired.ID, "task_lease_expired", "dead")
	})

	t.Run("terminal poison task does not block a healthy ready task", func(t *testing.T) {
		poisonIncidentID := insertIntegrationIncident(t, ctx, db)
		poisonTask := integrationTask(poisonIncidentID, "terminal-poison", 1)
		poisonTask.Priority = 100
		poison, err := repository.Enqueue(ctx, poisonTask)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE async_tasks SET attempt = max_attempts WHERE id = ?`, poison.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'CLOSED', v3_status = 'closed', terminal_at = NOW(6), version = version + 1
WHERE id = ?`, poisonIncidentID); err != nil {
			t.Fatal(err)
		}

		healthyIncidentID := insertIntegrationIncident(t, ctx, db)
		healthy, err := repository.Enqueue(ctx, integrationTask(healthyIncidentID, "terminal-poison-healthy", 3))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.Claim(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "terminal-poison-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if execution.Task.ID != healthy.ID {
			t.Fatalf("claimed task=%d, want healthy task %d", execution.Task.ID, healthy.ID)
		}
		assertIntegrationTaskDead(t, ctx, db, poison.ID, "subject_version_mismatch")
		assertIntegrationNoAttentionAndEvent(t, ctx, db, poisonIncidentID, poison.ID, 2, 1)
		assertIntegrationAttemptStatus(t, ctx, db, poison.ID, "dead")
		if err := repository.Resolve(ctx, execution.Lease, Succeeded(nil)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("legacy Incident poison task records compatible history and does not block", func(t *testing.T) {
		legacyPublicID := uuid.NewString()
		legacyResult, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, cluster, namespace, service_name,
environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version
) VALUES (?, ?, ?, 'cluster', 'namespace', 'legacy-service', 'test', 'Deployment',
          'legacy-workload', 'warning', 'DETECTED', 'legacy integration incident',
          NOW(6), NOW(6), 1)`, legacyPublicID, "legacy-fingerprint-"+legacyPublicID, "legacy:"+legacyPublicID)
		if err != nil {
			t.Fatal(err)
		}
		legacyID, err := legacyResult.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		// Public enqueue correctly rejects a legacy parent. Insert a historical
		// poison row directly so the reaper compatibility path remains covered.
		poisonPublicID := uuid.NewString()
		poisonResult, err := db.ExecContext(ctx, `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
dedupe_key, replay_generation, status, priority, available_at, attempt,
max_attempts, lease_generation, created_at, updated_at
) VALUES (?, ?, 1, 'investigate', 'investigation.advance', 'incident', ?,
          'investigation.start', 1, 1, JSON_OBJECT(), ?, 0, 'ready', 100,
          NOW(6), 1, 1, 0, NOW(6), NOW(6))`,
			poisonPublicID, legacyID, legacyID, integrationHash("legacy-poison:"+poisonPublicID))
		if err != nil {
			t.Fatal(err)
		}
		poisonID, err := poisonResult.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		poison := Task{ID: uint64(poisonID), Priority: 100}

		healthyIncidentID := insertIntegrationIncident(t, ctx, db)
		healthy, err := repository.Enqueue(ctx, integrationTask(healthyIncidentID, "legacy-poison-healthy", 3))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := repository.Claim(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "legacy-poison-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if execution.Task.ID != healthy.ID {
			t.Fatalf("claimed task=%d, want healthy task %d", execution.Task.ID, healthy.ID)
		}
		assertIntegrationTaskDead(t, ctx, db, poison.ID, "subject_version_mismatch")
		assertIntegrationNoAttentionAndEvent(t, ctx, db, uint64(legacyID), poison.ID, 1, 0)
		assertIntegrationAttemptStatus(t, ctx, db, poison.ID, "dead")
		var legacyEvents int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM incident_events
WHERE incident_id = ? AND event_type = 'async_task_dead'
  AND domain_schema_version IS NULL AND cycle_no IS NULL AND event_schema_version IS NULL`, legacyID).Scan(&legacyEvents); err != nil {
			t.Fatal(err)
		}
		if legacyEvents != 1 {
			t.Fatalf("legacy-compatible dead events=%d, want 1", legacyEvents)
		}
		if err := repository.Resolve(ctx, execution.Lease, Succeeded(nil)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("stale old cycle task records history without mutating current Incident", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "old-cycle-poison", 1))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE async_tasks SET attempt = max_attempts WHERE id = ?`, created.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE incidents SET cycle_no = 2, version = version + 1 WHERE id = ?`, incidentID); err != nil {
			t.Fatal(err)
		}
		reaped, err := repository.ReapExhaustedReady(ctx, QueueInvestigate)
		if err != nil || !reaped {
			t.Fatalf("reap stale old-cycle task=%v err=%v", reaped, err)
		}
		assertIntegrationTaskDead(t, ctx, db, created.ID, "subject_version_mismatch")
		assertIntegrationNoAttentionAndEvent(t, ctx, db, incidentID, created.ID, 2, 1)
		assertIntegrationAttemptStatus(t, ctx, db, created.ID, "dead")
	})

	t.Run("reopen fences stale child task before claim", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		runID := insertIntegrationAgentRun(t, ctx, db, incidentID, 1)
		created, err := repository.Enqueue(ctx, integrationAgentRunTask(incidentID, runID, "stale-child-cycle", 3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE incidents SET cycle_no = 2, version = version + 1 WHERE id = ?`, incidentID); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "stale-child-worker", LeaseDuration: time.Second}); !errors.Is(err, ErrNoTask) {
			t.Fatalf("stale child claim error=%v", err)
		}
		assertIntegrationTaskDead(t, ctx, db, created.ID, "subject_version_mismatch")
		assertIntegrationNoAttentionAndEvent(t, ctx, db, incidentID, created.ID, 2, 1)
	})

	t.Run("expired running task wins over sustained ready backlog", func(t *testing.T) {
		expiredIncidentID := insertIntegrationIncident(t, ctx, db)
		expired, err := repository.Enqueue(ctx, integrationTask(expiredIncidentID, "fair-expired", 3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ClaimReady(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "fair-old", LeaseDuration: 30 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		readyIDs := make([]uint64, 0, 24)
		for index := range 24 {
			incidentID := insertIntegrationIncident(t, ctx, db)
			task := integrationTask(incidentID, fmt.Sprintf("fair-ready-%d", index), 3)
			task.Priority = 100
			created, err := repository.Enqueue(ctx, task)
			if err != nil {
				t.Fatal(err)
			}
			readyIDs = append(readyIDs, created.ID)
		}
		waitUntilLeaseExpired(t, ctx, db, expired.ID)
		execution, err := repository.Claim(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "fair-new", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if execution.Task.ID != expired.ID || execution.Lease.Generation < 2 {
			t.Fatalf("fair claim task/generation=%d/%d, want expired %d with takeover generation", execution.Task.ID, execution.Lease.Generation, expired.ID)
		}
		if err := repository.Resolve(ctx, execution.Lease, Succeeded(nil)); err != nil {
			t.Fatal(err)
		}
		for _, taskID := range readyIDs {
			if err := repository.Cancel(ctx, taskID, 1, nil); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("database rejects invalid queue type and subject transition contracts", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		assertAsyncTaskContractRejected(t, ctx, db, incidentID, QueueDeliver, TaskInvestigationAdvance, "incident", "investigation.start", "queue-type")
		assertAsyncTaskContractRejected(t, ctx, db, incidentID, QueueInvestigate, TaskInvestigationAdvance, "incident", "investigation.step", "subject-transition")
	})

	t.Run("shutdown leaves unfinished task for lease takeover", func(t *testing.T) {
		incidentID := insertIntegrationIncident(t, ctx, db)
		created, err := repository.Enqueue(ctx, integrationTask(incidentID, "shutdown-takeover", 3))
		if err != nil {
			t.Fatal(err)
		}
		handlers := successfulHandlers()
		handlers[TaskInvestigationAdvance] = HandlerFunc(func(ctx context.Context, _ Execution) Result {
			<-ctx.Done()
			return Succeeded(nil)
		})
		pools := tinyPools(map[Queue]int{QueueInvestigate: 1, QueueDeliver: 1, QueueObserve: 1, QueueVerify: 1})
		investigate := pools[QueueInvestigate]
		investigate.LeaseDuration = 200 * time.Millisecond
		investigate.HeartbeatPeriod = 50 * time.Millisecond
		investigate.HandlerDeadline = 150 * time.Millisecond
		investigate.ExternalDeadline = 100 * time.Millisecond
		pools[QueueInvestigate] = investigate
		runner, err := NewRunner(RunnerConfig{
			Owner: "shutdown-worker", Store: repository, Handlers: handlers, Pools: pools,
			PollInterval: time.Millisecond, DrainTimeout: 20 * time.Millisecond, CancelWait: 30 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := runner.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitFor(t, time.Second, func() bool { return runner.InFlight(QueueInvestigate) == 1 })
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		if err := runner.Shutdown(shutdownCtx); !errors.Is(err, ErrDrainTimeout) {
			t.Fatalf("shutdown error=%v, want bounded drain timeout", err)
		}
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM async_tasks WHERE id = ?`, created.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "running" {
			t.Fatalf("unfinished task status=%q, want running", status)
		}
		waitUntilLeaseExpired(t, ctx, db, created.ID)
		taken, err := repository.TakeoverExpired(ctx, ClaimRequest{Queue: QueueInvestigate, Owner: "takeover-worker", LeaseDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if taken.Task.ID != created.ID || taken.Lease.Generation < 2 {
			t.Fatalf("takeover execution=%+v", taken)
		}
		if err := repository.Resolve(ctx, taken.Lease, Succeeded(nil)); err != nil {
			t.Fatal(err)
		}
	})
}

func newAsyncJobDatabase(t *testing.T, ctx context.Context, adminDSN string) *sql.DB {
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
	databaseName := fmt.Sprintf("cloudops_asyncjob_%d", time.Now().UnixNano())
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
	db.SetMaxOpenConns(24)
	db.SetMaxIdleConns(24)
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
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS `"+databaseName+"`")
		_ = admin.Close()
	})
	return db
}

func insertIntegrationIncident(t *testing.T, ctx context.Context, db *sql.DB) uint64 {
	t.Helper()
	publicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents (
public_id, fingerprint, correlation_key, cluster, namespace, service_name,
environment, target_kind, target_name, severity, status, summary,
first_seen_at, last_seen_at, version, domain_schema_version, v3_status,
cycle_no, correlation_key_version
) VALUES (?, ?, ?, 'cluster', 'namespace', 'service', 'test', 'Deployment',
          'workload', 'warning', 'DETECTED', 'integration incident', NOW(6), NOW(6),
          1, 3, 'detected', 1, 2)`, publicID, "fingerprint-"+publicID, "v2:"+publicID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id)
}

func integrationTask(incidentID uint64, identity string, maxAttempts uint32) NewTask {
	return NewTask{
		IncidentID:             incidentID,
		CycleNo:                1,
		Type:                   TaskInvestigationAdvance,
		SubjectType:            "incident",
		SubjectID:              incidentID,
		Transition:             "investigation.start",
		ExpectedSubjectVersion: 1,
		PayloadSchemaVersion:   1,
		Payload:                []byte(`{"mode":"start"}`),
		DedupeKey:              integrationHash(fmt.Sprintf("%d:%s", incidentID, identity)),
		MaxAttempts:            maxAttempts,
	}
}

func integrationAgentRunTask(incidentID, runID uint64, identity string, maxAttempts uint32) NewTask {
	return NewTask{
		IncidentID:             incidentID,
		CycleNo:                1,
		Type:                   TaskInvestigationAdvance,
		SubjectType:            "agent_run",
		SubjectID:              runID,
		Transition:             "investigation.step",
		ExpectedSubjectVersion: 1,
		PayloadSchemaVersion:   1,
		Payload:                []byte(`{"mode":"step"}`),
		DedupeKey:              integrationHash(fmt.Sprintf("%d:%d:%s", incidentID, runID, identity)),
		MaxAttempts:            maxAttempts,
	}
}

func insertIntegrationAgentRun(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, cycle uint32) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, `INSERT INTO agent_runs (
public_id, incident_id, idempotency_key, status, model, prompt_version, max_steps,
failure_code, row_version, domain_schema_version, v3_status, cycle_no,
expected_incident_version, completed_at, created_at, updated_at
) VALUES (?, ?, ?, 'COMPLETED', 'fixture', 'fixture-v1', 1, '', 1, 3,
          'completed', ?, 1, NOW(6), NOW(6), NOW(6))`, uuid.NewString(), incidentID, uuid.NewString(), cycle)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id)
}

func integrationCheckpoint(version uint64) Checkpoint {
	payload := []byte(fmt.Sprintf(`{"version":%d}`, version))
	digest := sha256.Sum256(payload)
	return Checkpoint{SchemaVersion: 1, Version: version, Hash: hex.EncodeToString(digest[:]), Payload: payload}
}

func integrationHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func claimReadyEventually(ctx context.Context, repository *Repository, request ClaimRequest) (*Execution, error) {
	deadline := time.Now().Add(time.Second)
	for {
		execution, err := repository.ClaimReady(ctx, request)
		if err == nil {
			return execution, nil
		}
		if !errors.Is(err, ErrNoTask) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(time.Millisecond)
	}
}

func waitUntilLeaseExpired(t *testing.T, ctx context.Context, db *sql.DB, taskID uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var expired bool
		if err := db.QueryRowContext(ctx, `SELECT lease_expires_at <= NOW(6) FROM async_tasks WHERE id = ?`, taskID).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("lease did not expire within one second")
}

func assertIntegrationTaskDead(t *testing.T, ctx context.Context, db *sql.DB, taskID uint64, code string) {
	t.Helper()
	var status, actualCode string
	if err := db.QueryRowContext(ctx, `SELECT status, last_error_code FROM async_tasks WHERE id = ?`, taskID).Scan(&status, &actualCode); err != nil {
		t.Fatal(err)
	}
	if status != "dead" || actualCode != code {
		t.Fatalf("task %d status/code=%q/%q, want dead/%q", taskID, status, actualCode, code)
	}
}

func assertIntegrationAttentionAndAttempt(t *testing.T, ctx context.Context, db *sql.DB, incidentID, taskID uint64, reason, attemptStatus string) {
	t.Helper()
	assertIntegrationAttentionAndEvent(t, ctx, db, incidentID, taskID, reason)
	assertIntegrationAttemptStatus(t, ctx, db, taskID, attemptStatus)
}

func assertIntegrationAttemptStatus(t *testing.T, ctx context.Context, db *sql.DB, taskID uint64, attemptStatus string) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_task_attempts WHERE task_id = ? AND status = ?`, taskID, attemptStatus).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("task %d %s attempt count=%d, want 1", taskID, attemptStatus, count)
	}
}

func assertIntegrationAttentionAndEvent(t *testing.T, ctx context.Context, db *sql.DB, incidentID, taskID uint64, reason string) {
	t.Helper()
	var attention bool
	var actualReason string
	var blocked bool
	if err := db.QueryRowContext(ctx, `SELECT needs_attention, blocking_reason_code, blocked_at IS NOT NULL FROM incidents WHERE id = ?`, incidentID).Scan(&attention, &actualReason, &blocked); err != nil {
		t.Fatal(err)
	}
	if !attention || actualReason != reason || !blocked {
		t.Fatalf("incident %d attention/reason/blocked=%v/%q/%v, want true/%q/true", incidentID, attention, actualReason, blocked, reason)
	}
	assertIntegrationDeadEvent(t, ctx, db, incidentID, taskID, 0)
}

func assertIntegrationNoAttentionAndEvent(t *testing.T, ctx context.Context, db *sql.DB, incidentID, taskID, expectedVersion uint64, eventCycle uint32) {
	t.Helper()
	var attention bool
	var reason sql.NullString
	var blockedAt sql.NullTime
	var version uint64
	if err := db.QueryRowContext(ctx, `SELECT needs_attention, blocking_reason_code, blocked_at, version FROM incidents WHERE id = ?`, incidentID).Scan(&attention, &reason, &blockedAt, &version); err != nil {
		t.Fatal(err)
	}
	if attention || reason.Valid || blockedAt.Valid || version != expectedVersion {
		t.Fatalf("incident %d attention/reason/blocked/version=%v/%v/%v/%d, want false/NULL/NULL/%d", incidentID, attention, reason, blockedAt, version, expectedVersion)
	}
	assertIntegrationDeadEvent(t, ctx, db, incidentID, taskID, eventCycle)
}

func assertIntegrationDeadEvent(t *testing.T, ctx context.Context, db *sql.DB, incidentID, taskID uint64, eventCycle uint32) {
	t.Helper()
	var count int
	query := `SELECT COUNT(*)
FROM incident_events ie
JOIN async_tasks task ON task.id = ?
WHERE ie.incident_id = ?
  AND ie.event_type = 'async_task_dead'
	  AND JSON_UNQUOTE(JSON_EXTRACT(ie.metadata_json, '$.task_public_id')) = task.public_id
  AND JSON_EXTRACT(ie.metadata_json, '$.task_id') IS NULL
  AND JSON_EXTRACT(ie.metadata_json, '$.subject_id') IS NULL`
	args := []any{taskID, incidentID}
	if eventCycle > 0 {
		query += ` AND ie.cycle_no = ?`
		args = append(args, eventCycle)
	}
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("task %d async_task_dead event count=%d, want 1", taskID, count)
	}
}

func assertAsyncTaskContractRejected(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, queue Queue, taskType TaskType, subjectType, transition, identity string) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO async_tasks (
public_id, incident_id, cycle_no, queue, task_type, subject_type, subject_id,
transition, expected_subject_version, payload_schema_version, payload_json,
dedupe_key, replay_generation, status, priority, available_at, attempt,
max_attempts, lease_generation, created_at, updated_at
) VALUES (?, ?, 1, ?, ?, ?, ?, ?, 1, 1, JSON_OBJECT(), ?, 0, 'ready', 0,
          NOW(6), 0, 3, 0, NOW(6), NOW(6))`, uuid.NewString(), incidentID, queue, taskType, subjectType, incidentID, transition, integrationHash(identity))
	if err == nil {
		t.Fatalf("database accepted invalid async task contract %s/%s/%s/%s", queue, taskType, subjectType, transition)
	}
}

func incidentReplayValidator(incidentID uint64) ReplayValidator {
	return func(ctx context.Context, tx DBTX, task Task) error {
		var version uint64
		if err := tx.QueryRowContext(ctx, `SELECT version FROM incidents WHERE id = ? FOR UPDATE`, incidentID).Scan(&version); err != nil {
			return err
		}
		if version != task.ExpectedSubjectVersion {
			return ErrSubjectVersionMismatch
		}
		return nil
	}
}

func agentRunReplayValidator(runID uint64) ReplayValidator {
	return func(ctx context.Context, tx DBTX, task Task) error {
		var version uint64
		if err := tx.QueryRowContext(ctx, `SELECT row_version FROM agent_runs WHERE id = ? FOR UPDATE`, runID).Scan(&version); err != nil {
			return err
		}
		if version != task.ExpectedSubjectVersion {
			return ErrSubjectVersionMismatch
		}
		return nil
	}
}
