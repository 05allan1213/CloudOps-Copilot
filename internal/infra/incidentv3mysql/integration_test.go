package incidentv3mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
)

func TestMySQLIncidentV3DuplicateCreateReopenAndStartUniqueness(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ready(ctx); err != nil {
		t.Fatal(err)
	}

	correlationKey := "v2:" + strings.Repeat("a", 64)
	initial := incidentIntegrationSignal(1, correlationKey)
	first, err := store.IngestBatch(ctx, []SignalInput{initial})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Duplicate || !first[0].StartTaskCreated || first[0].CycleNo != 1 {
		t.Fatalf("initial ingest=%+v", first)
	}
	incidentPublicID := first[0].IncidentPublicID

	duplicate, err := store.IngestBatch(ctx, []SignalInput{initial})
	if err != nil {
		t.Fatal(err)
	}
	if len(duplicate) != 1 || !duplicate[0].Duplicate || duplicate[0].IncidentPublicID != incidentPublicID || duplicate[0].StartTaskCreated {
		t.Fatalf("duplicate ingest=%+v", duplicate)
	}
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE public_id = ?", 1, incidentPublicID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE source = ? AND source_event_id = ?", 1, initial.Source, initial.SourceEventID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = (SELECT id FROM incidents WHERE public_id = ?) AND cycle_no = 1 AND transition = 'investigation.start'", 1, incidentPublicID)

	incidentID := incidentIntegrationIncidentID(t, ctx, db, incidentPublicID)
	processOneIncidentStart(t, ctx, db, incidentID, 1)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM agent_runs WHERE incident_id = ? AND cycle_no = 1 AND domain_schema_version = 3", 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND cycle_no = 1 AND transition = 'investigation.step'", 1, incidentID)

	if _, err := db.ExecContext(ctx, `UPDATE agent_runs
SET status = 'COMPLETED', v3_status = 'completed', completed_at = NOW(6), row_version = row_version + 1
WHERE incident_id = ? AND cycle_no = 1 AND domain_schema_version = 3`, incidentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE incidents
SET status = 'RESOLVED', v3_status = 'resolved', resolved_at = NOW(6), terminal_at = NOW(6), version = version + 1
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = 1`, incidentID); err != nil {
		t.Fatal(err)
	}

	reopenInputs := []SignalInput{
		incidentIntegrationSignal(2, correlationKey),
		incidentIntegrationSignal(3, correlationKey),
	}
	start := make(chan struct{})
	resultSets := make([][]IngestResult, len(reopenInputs))
	errorsByWorker := make([]error, len(reopenInputs))
	var wait sync.WaitGroup
	for index := range reopenInputs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			resultSets[index], errorsByWorker[index] = store.IngestBatch(ctx, []SignalInput{reopenInputs[index]})
		}()
	}
	close(start)
	wait.Wait()
	createdTasks := 0
	for index, workerErr := range errorsByWorker {
		if workerErr != nil {
			t.Fatalf("concurrent reopen worker %d: %v", index, workerErr)
		}
		if len(resultSets[index]) != 1 || resultSets[index][0].IncidentPublicID != incidentPublicID || resultSets[index][0].CycleNo != 2 {
			t.Fatalf("concurrent reopen result %d=%+v", index, resultSets[index])
		}
		if resultSets[index][0].StartTaskCreated {
			createdTasks++
		}
	}
	if createdTasks != 1 {
		t.Fatalf("concurrent reopen start-task creators=%d, want 1", createdTasks)
	}
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE correlation_key = ?", 1, correlationKey)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE incident_id = ? AND cycle_no = 2", 2, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND cycle_no = 2 AND transition = 'investigation.start'", 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE id = ? AND current_agent_run_id IS NULL", 1, incidentID)

	processOneIncidentStart(t, ctx, db, incidentID, 2)
	for cycle := 1; cycle <= 2; cycle++ {
		assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM agent_runs WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3", 1, incidentID, cycle)
		assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND cycle_no = ? AND transition = 'investigation.start'", 1, incidentID, cycle)
	}
}

func TestMySQLIncidentV3AgentRunBudgetStopsConcurrentStarts(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("c", 64)
	result, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(40, correlationKey)})
	if err != nil || len(result) != 1 {
		t.Fatalf("initial budget ingest=%+v err=%v", result, err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, result[0].IncidentPublicID)

	// Consume the three automatic AgentRun slots in one cycle. Each terminal
	// run advances the Incident version before the next start Task is enqueued.
	for run := 1; run <= taskhandler.DefaultAgentRunBudget; run++ {
		processOneIncidentStart(t, ctx, db, incidentID, 1)
		if run == taskhandler.DefaultAgentRunBudget {
			break
		}
		advanceFailedInvestigationRun(t, ctx, db, incidentID)
		version := incidentVersion(t, ctx, db, incidentID)
		task := budgetStartTask(incidentID, version, fmt.Sprintf("budget-%d", run))
		if _, err := repository.Enqueue(ctx, task); err != nil {
			t.Fatalf("enqueue budget start %d: %v", run+1, err)
		}
	}

	version := incidentVersion(t, ctx, db, incidentID)
	var tasks []*asyncjob.Task
	for index := range 2 {
		created, err := repository.Enqueue(ctx, budgetStartTask(incidentID, version, fmt.Sprintf("exhausted-%d", index)))
		if err != nil {
			t.Fatalf("enqueue concurrent exhausted start %d: %v", index, err)
		}
		tasks = append(tasks, created)
	}

	start := make(chan struct{})
	executions := make([]*asyncjob.Execution, len(tasks))
	errorsByWorker := make([]error, len(tasks))
	var wait sync.WaitGroup
	for index := range tasks {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			executions[index], errorsByWorker[index] = claimIncidentStartEventually(ctx, repository, asyncjob.ClaimRequest{
				Queue: asyncjob.QueueInvestigate, Owner: fmt.Sprintf("budget-worker-%d", index), LeaseDuration: 30 * time.Second,
			})
		}(index)
	}
	close(start)
	wait.Wait()
	for index, claimErr := range errorsByWorker {
		if claimErr != nil {
			t.Fatalf("concurrent exhausted claim %d: %v", index, claimErr)
		}
	}
	resolveStart := make(chan struct{})
	resolveErrors := make([]error, len(executions))
	wait = sync.WaitGroup{}
	for index, execution := range executions {
		result := taskhandler.New(taskhandler.Config{})[asyncjob.TaskInvestigationAdvance].Handle(ctx, *execution)
		if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
			t.Fatalf("budget start handler %d result=%+v", index, result)
		}
		wait.Add(1)
		go func(index int, execution *asyncjob.Execution, result asyncjob.Result) {
			defer wait.Done()
			<-resolveStart
			resolveErrors[index] = repository.Resolve(ctx, execution.Lease, result)
		}(index, execution, result)
	}
	close(resolveStart)
	wait.Wait()
	for index, resolveErr := range resolveErrors {
		if resolveErr != nil {
			t.Fatalf("resolve exhausted start %d: %v", index, resolveErr)
		}
	}

	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*)
FROM agent_runs WHERE incident_id = ? AND cycle_no = 1 AND domain_schema_version = 3`, taskhandler.DefaultAgentRunBudget, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*)
FROM async_tasks WHERE incident_id = ? AND cycle_no = 1 AND transition = 'investigation.step'`, taskhandler.DefaultAgentRunBudget, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*)
FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'dead'`, 2, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*)
FROM incident_events
WHERE incident_id = ? AND event_type = 'async_task_dead'
  AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.error_code')) = 'business_budget_exceeded'`, 1, incidentID)
	var attention bool
	if err := db.QueryRowContext(ctx, `SELECT needs_attention FROM incidents WHERE id = ?`, incidentID).Scan(&attention); err != nil {
		t.Fatal(err)
	}
	if !attention {
		t.Fatal("AgentRun budget exhaustion did not mark Incident needs_attention")
	}
}

func TestMySQLIncidentV3ConcurrentCreate(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("b", 64)
	inputs := []SignalInput{
		incidentIntegrationSignal(11, correlationKey),
		incidentIntegrationSignal(12, correlationKey),
	}
	start := make(chan struct{})
	results := make([][]IngestResult, len(inputs))
	errorsByWorker := make([]error, len(inputs))
	var wait sync.WaitGroup
	for index := range inputs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index], errorsByWorker[index] = store.IngestBatch(ctx, []SignalInput{inputs[index]})
		}()
	}
	close(start)
	wait.Wait()

	incidentPublicID := ""
	startTaskCreators := 0
	for index, workerErr := range errorsByWorker {
		if workerErr != nil {
			t.Fatalf("concurrent create worker %d: %v", index, workerErr)
		}
		if len(results[index]) != 1 || results[index][0].CycleNo != 1 || results[index][0].IncidentPublicID == "" {
			t.Fatalf("concurrent create result %d=%+v", index, results[index])
		}
		if incidentPublicID == "" {
			incidentPublicID = results[index][0].IncidentPublicID
		} else if results[index][0].IncidentPublicID != incidentPublicID {
			t.Fatalf("concurrent create produced incidents %s and %s", incidentPublicID, results[index][0].IncidentPublicID)
		}
		if results[index][0].StartTaskCreated {
			startTaskCreators++
		}
	}
	if startTaskCreators != 1 {
		t.Fatalf("concurrent create start-task creators=%d, want 1", startTaskCreators)
	}
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE correlation_key = ?", 1, correlationKey)
	incidentID := incidentIntegrationIncidentID(t, ctx, db, incidentPublicID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE incident_id = ? AND cycle_no = 1", 2, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND cycle_no = 1 AND transition = 'investigation.start'", 1, incidentID)
}

func TestMySQLIncidentV3MultiAlertSingleDecisionUsesStrongestSeverity(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("c", 64)
	warning := incidentIntegrationSignal(21, correlationKey)
	critical := incidentIntegrationSignal(22, correlationKey)
	critical.Severity = domain.SeverityCritical
	results, err := store.IngestBatch(ctx, []SignalInput{warning, critical})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].IncidentPublicID == "" || results[0].IncidentPublicID != results[1].IncidentPublicID {
		t.Fatalf("multi-alert results=%+v", results)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, results[0].IncidentPublicID)
	var severity string
	if err := db.QueryRowContext(ctx, "SELECT severity FROM incidents WHERE id = ?", incidentID).Scan(&severity); err != nil {
		t.Fatal(err)
	}
	if severity != string(domain.SeverityCritical) {
		t.Fatalf("multi-alert incident severity=%s, want critical", severity)
	}
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE incident_id = ? AND cycle_no = 1", 2, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE correlation_key = ?", 1, correlationKey)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'", 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'incident_created'", 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'investigation_start_enqueued'", 1, incidentID)
}

func TestMySQLIncidentV3SequentialSeverityRefreshesReadyStartTask(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("7", 64)
	warning := incidentIntegrationSignal(71, correlationKey)
	created, err := store.IngestBatch(ctx, []SignalInput{warning})
	if err != nil {
		t.Fatal(err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	critical := incidentIntegrationSignal(72, correlationKey)
	critical.Severity = domain.SeverityCritical
	if _, err := store.IngestBatch(ctx, []SignalInput{critical}); err != nil {
		t.Fatal(err)
	}
	var severity string
	var version uint64
	if err := db.QueryRowContext(ctx, "SELECT severity, version FROM incidents WHERE id = ?", incidentID).Scan(&severity, &version); err != nil {
		t.Fatal(err)
	}
	if severity != string(domain.SeverityCritical) || version != 2 {
		t.Fatalf("severity/version=%s/%d", severity, version)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'ready'
  AND expected_subject_version = 2`, 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'", 1, incidentID)
	processOneIncidentStart(t, ctx, db, incidentID, 1)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM agent_runs WHERE incident_id = ? AND cycle_no = 1", 1, incidentID)
}

func TestMySQLIncidentV3SequentialSeverityFencesRunningStartTask(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("8", 64)
	created, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(81, correlationKey)})
	if err != nil {
		t.Fatal(err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	repository, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := repository.ClaimReady(ctx, asyncjob.ClaimRequest{Queue: asyncjob.QueueInvestigate, Owner: "stale-start", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	critical := incidentIntegrationSignal(82, correlationKey)
	critical.Severity = domain.SeverityCritical
	if _, err := store.IngestBatch(ctx, []SignalInput{critical}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Heartbeat(ctx, stale.Lease, time.Minute); !errors.Is(err, asyncjob.ErrLeaseLost) {
		t.Fatalf("stale start heartbeat error=%v", err)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'cancelled'`, 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'ready'
  AND expected_subject_version = 2`, 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_task_attempts
WHERE task_id = ? AND status = 'cancelled'`, 1, stale.Task.ID)
	processOneIncidentStart(t, ctx, db, incidentID, 1)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM agent_runs WHERE incident_id = ? AND cycle_no = 1", 1, incidentID)
}

func TestMySQLIncidentV3ResolvedWithoutActiveIncidentAttachesToOriginalFiring(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("9", 64)
	firing := incidentIntegrationSignal(91, correlationKey)
	created, err := store.IngestBatch(ctx, []SignalInput{firing})
	if err != nil {
		t.Fatal(err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	if _, err := db.ExecContext(ctx, `UPDATE incidents
SET v3_status = 'resolved', status = 'RESOLVED', resolved_at = NOW(6), terminal_at = NOW(6)
WHERE id = ?`, incidentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE async_tasks SET status = 'cancelled', cancelled_at = NOW(6)
WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'ready'`, incidentID); err != nil {
		t.Fatal(err)
	}
	resolved := firing
	resolved.SourceEventID = "v2:" + strings.Repeat("a", 64)
	resolved.Status = domain.SignalStatusResolved
	endsAt := firing.StartsAt.Add(time.Minute)
	resolved.EndsAt = &endsAt
	resolved.OccurredAt = endsAt
	results, err := store.IngestBatch(ctx, []SignalInput{resolved})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Rejected || results[0].IncidentPublicID != created[0].IncidentPublicID || results[0].CycleNo != 1 {
		t.Fatalf("resolved attachment=%+v", results)
	}
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE correlation_key = ?", 1, correlationKey)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE incident_id = ? AND cycle_no = 1", 2, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM signal_rejections WHERE source_event_id = ?", 0, resolved.SourceEventID)
}

func TestMySQLIncidentV3SameBatchResolvedBeforeFiringStillPairsInstance(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := "v2:" + strings.Repeat("b", 64)
	firing := incidentIntegrationSignal(101, correlationKey)
	firing.SourceEventID = "v2:" + strings.Repeat("f", 64)
	resolved := firing
	resolved.SourceEventID = "v2:" + strings.Repeat("0", 64)
	resolved.Status = domain.SignalStatusResolved
	endsAt := firing.StartsAt.Add(time.Minute)
	resolved.EndsAt = &endsAt
	resolved.OccurredAt = endsAt
	results, err := store.IngestBatch(ctx, []SignalInput{resolved, firing})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("batch results=%+v", results)
	}
	for _, result := range results {
		if result.Rejected || result.IncidentPublicID == "" || result.CycleNo != 1 {
			t.Fatalf("batch result=%+v", result)
		}
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, results[0].IncidentPublicID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incident_signals WHERE incident_id = ? AND cycle_no = 1", 2, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'", 0, incidentID)
}

func TestMySQLIncidentV3ReopenWindowAndLatestTerminalRules(t *testing.T) {
	t.Run("exact thirty minute boundary reopens", func(t *testing.T) {
		db := openIncidentIntegrationDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(ctx, "SET timestamp = 1784376000"); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), "SET timestamp = DEFAULT") }()
		correlationKey := "v2:" + strings.Repeat("d", 64)
		if _, err := insertIncidentIntegrationTerminal(ctx, conn, correlationKey, "resolved", "TIMESTAMPADD(MINUTE, -30, NOW(6))"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, "UPDATE incidents SET severity = 'critical' WHERE correlation_key = ?", correlationKey); err != nil {
			t.Fatal(err)
		}
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := lockCorrelation(ctx, tx, correlationKey); err != nil {
			t.Fatal(err)
		}
		input := incidentIntegrationSignal(31, correlationKey)
		row, reopened, err := createOrReopenIncident(ctx, tx, input)
		if err != nil {
			t.Fatal(err)
		}
		if !reopened || row.cycleNo != 2 || row.status != domain.V3StatusInvestigating || row.severity != domain.SeverityWarning {
			t.Fatalf("exact-boundary reopen row=%+v reopened=%v", row, reopened)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("outside window creates a new incident", func(t *testing.T) {
		db := openIncidentIntegrationDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		store, err := NewStore(db)
		if err != nil {
			t.Fatal(err)
		}
		correlationKey := "v2:" + strings.Repeat("e", 64)
		oldID, err := insertIncidentIntegrationTerminal(ctx, db, correlationKey, "resolved", "TIMESTAMPADD(SECOND, -1801, NOW(6))")
		if err != nil {
			t.Fatal(err)
		}
		result, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(32, correlationKey)})
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 || result[0].CycleNo != 1 || !result[0].StartTaskCreated {
			t.Fatalf("outside-window result=%+v", result)
		}
		newID := incidentIntegrationIncidentID(t, ctx, db, result[0].IncidentPublicID)
		if newID == oldID {
			t.Fatal("outside-window firing reopened the old incident")
		}
		assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE correlation_key = ?", 2, correlationKey)
	})

	t.Run("newer closed terminal prevents older resolved reopen", func(t *testing.T) {
		db := openIncidentIntegrationDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		store, err := NewStore(db)
		if err != nil {
			t.Fatal(err)
		}
		correlationKey := "v2:" + strings.Repeat("f", 64)
		resolvedID, err := insertIncidentIntegrationTerminal(ctx, db, correlationKey, "resolved", "TIMESTAMPADD(MINUTE, -10, NOW(6))")
		if err != nil {
			t.Fatal(err)
		}
		closedID, err := insertIncidentIntegrationTerminal(ctx, db, correlationKey, "closed", "TIMESTAMPADD(MINUTE, -1, NOW(6))")
		if err != nil {
			t.Fatal(err)
		}
		result, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(33, correlationKey)})
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 || result[0].CycleNo != 1 || !result[0].StartTaskCreated {
			t.Fatalf("closed-after-resolved result=%+v", result)
		}
		newID := incidentIntegrationIncidentID(t, ctx, db, result[0].IncidentPublicID)
		if newID == resolvedID || newID == closedID {
			t.Fatalf("closed-after-resolved reused terminal incident id=%d", newID)
		}
		assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE correlation_key = ?", 3, correlationKey)
		assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE id = ? AND v3_status = 'resolved' AND cycle_no = 1", 1, resolvedID)
		assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE id = ? AND v3_status = 'closed' AND cycle_no = 1", 1, closedID)
	})
}

func processOneIncidentStart(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, cycle uint32) {
	t.Helper()
	repository, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	requestA := asyncjob.ClaimRequest{Queue: asyncjob.QueueInvestigate, Owner: fmt.Sprintf("incident-start-%d-a", cycle), LeaseDuration: 30 * time.Second}
	requestB := asyncjob.ClaimRequest{Queue: asyncjob.QueueInvestigate, Owner: fmt.Sprintf("incident-start-%d-b", cycle), LeaseDuration: 30 * time.Second}
	start := make(chan struct{})
	executions := make([]*asyncjob.Execution, 2)
	errorsByWorker := make([]error, 2)
	var wait sync.WaitGroup
	for index, request := range []asyncjob.ClaimRequest{requestA, requestB} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			executions[index], errorsByWorker[index] = repository.Claim(ctx, request)
		}()
	}
	close(start)
	wait.Wait()

	var claimed *asyncjob.Execution
	noTask := 0
	for index, workerErr := range errorsByWorker {
		switch {
		case workerErr == nil:
			if executions[index] == nil || executions[index].Task.IncidentID != incidentID || executions[index].Task.CycleNo != cycle || executions[index].Task.Transition != "investigation.start" {
				t.Fatalf("unexpected claimed start task: %+v", executions[index])
			}
			claimed = executions[index]
		case errors.Is(workerErr, asyncjob.ErrNoTask):
			noTask++
		default:
			t.Fatalf("claim start task worker %d: %v", index, workerErr)
		}
	}
	if claimed == nil || noTask != 1 {
		t.Fatalf("concurrent start claim claimed=%+v no_task=%d", claimed, noTask)
	}
	handler := taskhandler.New(taskhandler.Config{})[asyncjob.TaskInvestigationAdvance]
	result := handler.Handle(ctx, *claimed)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("investigation.start handler result=%+v", result)
	}
	if err := repository.Resolve(ctx, claimed.Lease, result); err != nil {
		t.Fatalf("resolve investigation.start: %v", err)
	}
	var (
		currentRunID, runID, runCycle, expectedIncidentVersion  uint64
		maxSteps, maxToolCalls, maxModelCalls, maxEvidenceItems int
		tokenBudget, maxRuntimeMS, maxCheckpointBytes           int64
		deadlineMicros                                          int64
	)
	if err := db.QueryRowContext(ctx, `
SELECT i.current_agent_run_id, r.id, r.cycle_no, r.expected_incident_version,
       r.max_steps, r.max_tool_calls, r.max_model_calls, r.token_budget,
       r.max_evidence_items, r.max_runtime_ms, r.max_checkpoint_bytes,
       TIMESTAMPDIFF(MICROSECOND, r.created_at, r.deadline_at)
FROM incidents i
JOIN agent_runs r ON r.id = i.current_agent_run_id
WHERE i.id = ? AND r.domain_schema_version = 3`, incidentID).Scan(
		&currentRunID, &runID, &runCycle, &expectedIncidentVersion,
		&maxSteps, &maxToolCalls, &maxModelCalls, &tokenBudget,
		&maxEvidenceItems, &maxRuntimeMS, &maxCheckpointBytes, &deadlineMicros,
	); err != nil {
		t.Fatalf("load investigation.start runtime snapshot: %v", err)
	}
	if currentRunID != runID || runCycle != uint64(cycle) || expectedIncidentVersion != claimed.Task.ExpectedSubjectVersion+1 ||
		maxSteps != 8 || maxToolCalls != 8 || maxModelCalls != 10 || tokenBudget != 16_000 ||
		maxEvidenceItems != 20 || maxRuntimeMS != 180_000 || maxCheckpointBytes != 64*1024 ||
		deadlineMicros != 180_000_000 {
		t.Fatalf("unexpected investigation.start runtime snapshot run=%d/%d cycle=%d expected=%d budgets=%d/%d/%d/%d/%d/%d/%d deadline_us=%d",
			currentRunID, runID, runCycle, expectedIncidentVersion, maxSteps, maxToolCalls,
			maxModelCalls, tokenBudget, maxEvidenceItems, maxRuntimeMS, maxCheckpointBytes, deadlineMicros)
	}
}

func advanceFailedInvestigationRun(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64) {
	t.Helper()
	var runID, version uint64
	if err := db.QueryRowContext(ctx, `
SELECT current_agent_run_id, version
FROM incidents WHERE id = ? AND v3_status = 'investigating' FOR UPDATE`, incidentID).Scan(&runID, &version); err != nil {
		t.Fatal(err)
	}
	if runID == 0 {
		t.Fatalf("Incident %d has no current AgentRun", incidentID)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE agent_runs
SET status = 'FAILED', v3_status = 'failed', completed_at = NOW(6),
    row_version = row_version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND v3_status = 'pending'`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE incidents
SET version = version + 1, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND v3_status = 'investigating' AND version = ?`, incidentID, version); err != nil {
		t.Fatal(err)
	}
}

func incidentVersion(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64) uint64 {
	t.Helper()
	var version uint64
	if err := db.QueryRowContext(ctx, `SELECT version FROM incidents WHERE id = ?`, incidentID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func budgetStartTask(incidentID, version uint64, identity string) asyncjob.NewTask {
	return asyncjob.NewTask{
		IncidentID: incidentID, CycleNo: 1, Type: asyncjob.TaskInvestigationAdvance,
		SubjectType: "incident", SubjectID: incidentID, Transition: "investigation.start",
		ExpectedSubjectVersion: version, PayloadSchemaVersion: 1,
		Payload: []byte(`{"mode":"start"}`), DedupeKey: hashCanonical("budget-start", fmt.Sprint(incidentID), fmt.Sprint(version), identity),
		Priority: 100, MaxAttempts: 3,
	}
}

func claimIncidentStartEventually(ctx context.Context, repository *asyncjob.Repository, request asyncjob.ClaimRequest) (*asyncjob.Execution, error) {
	deadline := time.Now().Add(time.Second)
	for {
		execution, err := repository.Claim(ctx, request)
		if err == nil {
			return execution, nil
		}
		if !errors.Is(err, asyncjob.ErrNoTask) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(time.Millisecond)
	}
}

func incidentIntegrationSignal(sequence int, correlationKey string) SignalInput {
	startsAt := time.Date(2026, 7, 18, 10, 0, sequence, 123456000, time.UTC)
	return SignalInput{
		Source: "alertmanager", SourceEventID: fmt.Sprintf("v2:%064x", sequence),
		AlertInstanceKey: fmt.Sprintf("%064x", sequence), CorrelationKey: correlationKey,
		Fingerprint: fmt.Sprintf("fingerprint-%d", sequence), Status: domain.SignalStatusFiring,
		Severity: domain.SeverityWarning, Cluster: "kind", Environment: "demo", Namespace: "demo",
		ServiceName: "checkout", TargetKind: "Deployment", TargetName: "checkout", Category: "readiness",
		StartsAt: startsAt, OccurredAt: startsAt, Summary: "workload is not ready",
		Labels: []byte(`{"alertname":"WorkloadNotReady"}`), Annotations: []byte(`{"summary":"not ready"}`),
	}
}

type incidentIntegrationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertIncidentIntegrationTerminal(ctx context.Context, executor incidentIntegrationExecutor, correlationKey, status, terminalExpression string) (uint64, error) {
	legacyStatus := "RESOLVED"
	resolvedExpression := terminalExpression
	if status == "closed" {
		legacyStatus = "CLOSED_NO_ACTION"
		resolvedExpression = "NULL"
	}
	query := fmt.Sprintf(`INSERT INTO incidents (
public_id, fingerprint, correlation_key, correlation_key_version, cluster,
namespace, service_name, environment, target_kind, target_name, severity,
status, summary, first_seen_at, last_seen_at, resolved_at, version,
domain_schema_version, v3_status, cycle_no, terminal_at
) VALUES (?, 'terminal-fixture', ?, 2, 'kind', 'demo', 'checkout', 'demo',
          'Deployment', 'checkout', 'warning', ?, 'terminal fixture',
          TIMESTAMPADD(HOUR, -1, NOW(6)), NOW(6), %s, 1, 3, ?, 1, %s)`,
		resolvedExpression, terminalExpression)
	result, err := executor.ExecContext(ctx, query, uuid.NewString(), correlationKey, legacyStatus, status)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func openIncidentIntegrationDB(t *testing.T) *sql.DB {
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
	name := fmt.Sprintf("cloudops_incidentv3_%d", time.Now().UnixNano())
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

func incidentIntegrationIncidentID(t *testing.T, ctx context.Context, db *sql.DB, publicID string) uint64 {
	t.Helper()
	var id uint64
	if err := db.QueryRowContext(ctx, "SELECT id FROM incidents WHERE public_id = ?", publicID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertIncidentIntegrationCount(t *testing.T, ctx context.Context, db *sql.DB, query string, expected int, args ...any) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("count=%d want=%d query=%s", count, expected, query)
	}
}
