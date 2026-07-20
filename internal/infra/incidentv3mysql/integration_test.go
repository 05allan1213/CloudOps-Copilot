package incidentv3mysql

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
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
FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'succeeded'`, 4, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*)
FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'dead'`, 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*)
FROM incident_events
WHERE incident_id = ? AND event_type = 'agent_run_budget_exhausted'`, 1, incidentID)
	var attention bool
	if err := db.QueryRowContext(ctx, `SELECT needs_attention FROM incidents WHERE id = ?`, incidentID).Scan(&attention); err != nil {
		t.Fatal(err)
	}
	if !attention {
		t.Fatal("AgentRun budget exhaustion did not mark Incident needs_attention")
	}
}

func TestMySQLIncidentV3AuthorizedAgentRunSlotsFourAndFiveAreConcurrentAndIdempotent(t *testing.T) {
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
	correlationKey := "v2:" + strings.Repeat("b", 64)
	created, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(45, correlationKey)})
	if err != nil || len(created) != 1 {
		t.Fatalf("authorized budget ingest=%+v err=%v", created, err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	for run := 1; run <= businessbudget.DefaultLimit; run++ {
		processOneIncidentStart(t, ctx, db, incidentID, 1)
		advanceFailedInvestigationRun(t, ctx, db, incidentID)
		if run < businessbudget.DefaultLimit {
			version := incidentVersion(t, ctx, db, incidentID)
			if _, err := repository.Enqueue(ctx, budgetStartTask(incidentID, version, fmt.Sprintf("authorized-primer-%d", run))); err != nil {
				t.Fatal(err)
			}
		}
	}
	for slot := businessbudget.DefaultLimit + 1; slot <= businessbudget.HardLimit; slot++ {
		authorization, version := authorizeBudgetAgentRun(t, ctx, db, incidentID, fmt.Sprintf("operator authorized slot %d after reviewing durable evidence", slot))
		if authorization.Slot != slot {
			t.Fatalf("authorization slot=%d want=%d", authorization.Slot, slot)
		}
		firstTask := authorizedBudgetStartTask(incidentID, version, authorization.PublicID, fmt.Sprintf("slot-%d-replay", slot))
		first, err := repository.Enqueue(ctx, firstTask)
		if err != nil {
			t.Fatal(err)
		}
		duplicate, err := repository.Enqueue(ctx, firstTask)
		if err != nil || duplicate.ID != first.ID {
			t.Fatalf("duplicate authorized task first=%+v duplicate=%+v err=%v", first, duplicate, err)
		}
		if _, err := repository.Enqueue(ctx, authorizedBudgetStartTask(incidentID, version, authorization.PublicID, fmt.Sprintf("slot-%d-concurrent", slot))); err != nil {
			t.Fatal(err)
		}
		resolveConcurrentBudgetStarts(t, ctx, repository, 2, fmt.Sprintf("slot-%d", slot))
		assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
WHERE incident_id = ? AND cycle_no = 1 AND domain_schema_version = 3`, slot, incidentID)
		assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
WHERE incident_id = ? AND cycle_no = 1 AND business_budget_authorization_id = ?`, 1, incidentID, authorization.ID)
		assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
WHERE incident_id = ? AND event_type = 'agent_run_created'
  AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.business_budget_authorization_id')) = ?`, 1, incidentID, authorization.PublicID)

		var originatingRunID uint64
		if err := db.QueryRowContext(ctx, `SELECT id FROM agent_runs WHERE business_budget_authorization_id = ?`, authorization.ID).Scan(&originatingRunID); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []businessbudget.Kind{businessbudget.KindRemediationPlan, businessbudget.KindVerificationRun} {
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			lineage, guardErr := businessbudget.GuardChild(ctx, tx, kind, incidentID, 1, originatingRunID)
			_ = tx.Rollback()
			if guardErr != nil || !lineage.Allowed() || lineage.AuthorizationID != authorization.ID ||
				lineage.OriginatingAgentRunPublicID == "" {
				t.Fatalf("kind=%s lineage=%+v err=%v", kind, lineage, guardErr)
			}
		}
		if slot < businessbudget.HardLimit {
			advanceFailedInvestigationRun(t, ctx, db, incidentID)
		}
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	_, hard, err := businessbudget.AuthorizeAgentRun(ctx, tx, incidentID, 1, businessbudget.Actor{
		Provider: "github", Login: "operator", Role: "operator", Reason: "slot six must reject", RequestID: uuid.NewString(),
	})
	if err != nil || hard.Outcome != businessbudget.OutcomeHardExhausted {
		_ = tx.Rollback()
		t.Fatalf("hard authorization result=%+v err=%v", hard, err)
	}
	if err := businessbudget.MarkExhausted(ctx, tx, hard, incidentID, 1, "integration.operator"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs WHERE incident_id = ? AND cycle_no = 1`, businessbudget.HardLimit, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_cycle_budget_authorizations WHERE incident_id = ?`, 2, incidentID)
}

func TestMySQLIncidentV3AutomaticStartProducersCreateNoOrphanAtDefaultBudget(t *testing.T) {
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
	created, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(46, "v2:"+strings.Repeat("d", 64))})
	if err != nil || len(created) != 1 {
		t.Fatalf("automatic producer fixture=%+v err=%v", created, err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	for run := 1; run <= businessbudget.DefaultLimit; run++ {
		processOneIncidentStart(t, ctx, db, incidentID, 1)
		advanceFailedInvestigationRun(t, ctx, db, incidentID)
		if run < businessbudget.DefaultLimit {
			if _, err := repository.Enqueue(ctx, budgetStartTask(incidentID, incidentVersion(t, ctx, db, incidentID), fmt.Sprintf("producer-primer-%d", run))); err != nil {
				t.Fatal(err)
			}
		}
	}
	versionBeforeBlock := incidentVersion(t, ctx, db, incidentID)

	start := make(chan struct{})
	errs := make([]error, 2)
	blocked := make([]bool, 2)
	var wait sync.WaitGroup
	for index := range errs {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				errs[index] = err
				return
			}
			defer func() { _ = tx.Rollback() }()
			_, blocked[index], err = enqueueInvestigationStart(ctx, tx, incidentRow{
				id: incidentID, publicID: created[0].IncidentPublicID, cycleNo: 1,
			})
			if err == nil {
				err = tx.Commit()
			}
			errs[index] = err
		}(index)
	}
	close(start)
	wait.Wait()
	for index := range errs {
		if errs[index] != nil || !blocked[index] {
			t.Fatalf("producer %d blocked=%v err=%v", index, blocked[index], errs[index])
		}
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs WHERE incident_id = ? AND cycle_no = 1`, businessbudget.DefaultLimit, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start'`, businessbudget.DefaultLimit, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ? AND transition = 'investigation.start' AND status IN ('ready','running')`, 0, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'agent_run_budget_exhausted'`, 1, incidentID)
	var versionAfterBlock uint64
	if err := db.QueryRowContext(ctx, `SELECT version FROM incidents WHERE id = ?`, incidentID).Scan(&versionAfterBlock); err != nil {
		t.Fatal(err)
	}
	if versionAfterBlock != versionBeforeBlock+1 {
		t.Fatalf("repeated budget block version=%d want=%d", versionAfterBlock, versionBeforeBlock+1)
	}
}

func TestMySQLIncidentV3BudgetAuthorizationRejectsMissingWrongCycleAndWrongLineage(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	incidentID, _ := insertSimpleBudgetIncident(t, ctx, db, 3)
	foreignIncidentID, foreignRunID := insertSimpleBudgetIncident(t, ctx, db, 1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = businessbudget.AuthorizeAgentRun(ctx, tx, incidentID, 1, businessbudget.Actor{
		Provider: "github", Login: "operator", Role: "operator", RequestID: uuid.NewString(),
	})
	_ = tx.Rollback()
	if !errors.Is(err, businessbudget.ErrInvalidAuthorization) {
		t.Fatalf("empty authorization reason error=%v", err)
	}

	authorization, _ := authorizeBudgetAgentRun(t, ctx, db, incidentID, "operator approved slot four with durable evidence")
	for name, guard := range map[string]func(*sql.Tx) error{
		"missing authorization": func(tx *sql.Tx) error {
			result, err := businessbudget.GuardAgentRun(ctx, tx, incidentID, 1, "")
			if err != nil {
				return err
			}
			if result.Outcome != businessbudget.OutcomeDefaultExhausted {
				return fmt.Errorf("outcome=%s", result.Outcome)
			}
			return nil
		},
		"unknown authorization": func(tx *sql.Tx) error {
			_, err := businessbudget.GuardAgentRun(ctx, tx, incidentID, 1, uuid.NewString())
			if !errors.Is(err, businessbudget.ErrInvalidAuthorization) {
				return fmt.Errorf("error=%v", err)
			}
			return nil
		},
		"wrong cycle": func(tx *sql.Tx) error {
			_, err := businessbudget.GuardAgentRun(ctx, tx, incidentID, 2, authorization.PublicID)
			if err == nil {
				return errors.New("wrong-cycle authorization was accepted")
			}
			return nil
		},
		"wrong lineage": func(tx *sql.Tx) error {
			_, err := businessbudget.GuardChild(ctx, tx, businessbudget.KindRemediationPlan, incidentID, 1, foreignRunID)
			if !errors.Is(err, businessbudget.ErrInvalidAuthorization) {
				return fmt.Errorf("error=%v foreign_incident=%d", err, foreignIncidentID)
			}
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if err := guard(tx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLIncidentV3PlanAndBothVerificationPathsShareDurableAuthorizedLineage(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	incidentID, automaticRunID := insertSimpleBudgetIncident(t, ctx, db, businessbudget.DefaultLimit)
	authorization, _ := authorizeBudgetAgentRun(t, ctx, db, incidentID, "operator authorized a bounded child-producing investigation retry")
	foreignIncidentID, _ := insertSimpleBudgetIncident(t, ctx, db, 0)

	if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, failure_code,
  completed_at, row_version, domain_schema_version, v3_status, cycle_no,
  expected_incident_version, business_budget_authorization_id)
VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, '', NOW(6), 1, 3,
        'completed', 1, 1, ?)`, uuid.NewString(), foreignIncidentID, authorization.ID); err == nil {
		t.Fatal("cross-Incident authorization lineage insert unexpectedly succeeded")
	}

	result, err := db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, failure_code,
  completed_at, row_version, domain_schema_version, v3_status, cycle_no,
  expected_incident_version, business_budget_authorization_id)
VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, '', NOW(6), 1, 3,
        'completed', 1, 1, ?)`, uuid.NewString(), incidentID, authorization.ID)
	if err != nil {
		t.Fatal(err)
	}
	authorizedRunID, _ := result.LastInsertId()
	insertPartialBudgetPlans(t, ctx, db, incidentID, businessbudget.DefaultLimit)
	insertPartialBudgetVerifications(t, ctx, db, incidentID, businessbudget.DefaultLimit)

	for _, test := range []struct {
		name, source string
		kind         businessbudget.Kind
	}{
		{name: "remediation prepare", source: "remediation.prepare", kind: businessbudget.KindRemediationPlan},
		{name: "post delivery verification", source: "delivery.observe", kind: businessbudget.KindVerificationRun},
		{name: "no change verification", source: "v3-ingress.no-change", kind: businessbudget.KindVerificationRun},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			blocked, err := businessbudget.GuardChild(ctx, tx, test.kind, incidentID, 1, automaticRunID)
			if err != nil || blocked.Outcome != businessbudget.OutcomeDefaultExhausted {
				_ = tx.Rollback()
				t.Fatalf("automatic guard=%+v err=%v", blocked, err)
			}
			if err := businessbudget.MarkExhausted(ctx, tx, blocked, incidentID, 1, test.source); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}

			tx, err = db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			authorized, err := businessbudget.GuardChild(ctx, tx, test.kind, incidentID, 1, uint64(authorizedRunID))
			_ = tx.Rollback()
			if err != nil || !authorized.Allowed() || authorized.AuthorizationID != authorization.ID ||
				authorized.OriginatingAgentRunPublicID == "" {
				t.Fatalf("authorized guard=%+v err=%v", authorized, err)
			}
		})
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks WHERE incident_id = ?`, 0, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'remediation_plan_budget_exhausted'`, 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events WHERE incident_id = ? AND event_type = 'verification_run_budget_exhausted'`, 2, incidentID)

	insertPartialBudgetPlans(t, ctx, db, incidentID, businessbudget.HardLimit-businessbudget.DefaultLimit)
	insertPartialBudgetVerifications(t, ctx, db, incidentID, businessbudget.HardLimit-businessbudget.DefaultLimit)
	for _, kind := range []businessbudget.Kind{businessbudget.KindRemediationPlan, businessbudget.KindVerificationRun} {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		hard, err := businessbudget.GuardChild(ctx, tx, kind, incidentID, 1, uint64(authorizedRunID))
		_ = tx.Rollback()
		if err != nil || hard.Outcome != businessbudget.OutcomeHardExhausted {
			t.Fatalf("kind=%s hard guard=%+v err=%v", kind, hard, err)
		}
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

func TestMySQLIncidentV3SequentialSeverityPreservesRunningAuthorizedStart(t *testing.T) {
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
	correlationKey := "v2:" + strings.Repeat("e", 64)
	created, err := store.IngestBatch(ctx, []SignalInput{incidentIntegrationSignal(83, correlationKey)})
	if err != nil || len(created) != 1 {
		t.Fatalf("authorized replacement fixture=%+v err=%v", created, err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	for run := 1; run <= businessbudget.DefaultLimit; run++ {
		processOneIncidentStart(t, ctx, db, incidentID, 1)
		advanceFailedInvestigationRun(t, ctx, db, incidentID)
		if run < businessbudget.DefaultLimit {
			if _, err := repository.Enqueue(ctx, budgetStartTask(incidentID, incidentVersion(t, ctx, db, incidentID), fmt.Sprintf("authorized-refresh-primer-%d", run))); err != nil {
				t.Fatal(err)
			}
		}
	}

	authorization, authorizedVersion := authorizeBudgetAgentRun(t, ctx, db, incidentID, "operator approved retry despite a concurrent severity refresh")
	if _, err := repository.Enqueue(ctx, authorizedBudgetStartTask(incidentID, authorizedVersion, authorization.PublicID, "authorized-refresh")); err != nil {
		t.Fatal(err)
	}
	stale, err := claimIncidentStartEventually(ctx, repository, asyncjob.ClaimRequest{
		Queue: asyncjob.QueueInvestigate, Owner: "stale-authorized-start", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Task.Transition != "investigation.start" || !strings.Contains(string(stale.Task.Payload), authorization.PublicID) {
		t.Fatalf("claimed authorized start=%+v", stale.Task)
	}

	critical := incidentIntegrationSignal(84, correlationKey)
	critical.Severity = domain.SeverityCritical
	if _, err := store.IngestBatch(ctx, []SignalInput{critical}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Heartbeat(ctx, stale.Lease, time.Minute); !errors.Is(err, asyncjob.ErrLeaseLost) {
		t.Fatalf("stale authorized start heartbeat error=%v", err)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'cancelled'`, 1, incidentID)
	var replacementVersion uint64
	var replacementAuthorization string
	if err := db.QueryRowContext(ctx, `SELECT expected_subject_version,
       JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.business_budget_authorization_id'))
FROM async_tasks
WHERE incident_id = ? AND transition = 'investigation.start' AND status = 'ready'`, incidentID).
		Scan(&replacementVersion, &replacementAuthorization); err != nil {
		t.Fatal(err)
	}
	if replacementVersion != authorizedVersion+1 || replacementAuthorization != authorization.PublicID {
		t.Fatalf("replacement version=%d authorization=%q", replacementVersion, replacementAuthorization)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
WHERE incident_id = ? AND event_type IN ('agent_run_budget_exhausted','agent_run_hard_limit_exhausted')`, 0, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incidents
WHERE id = ? AND needs_attention = FALSE`, 1, incidentID)

	processOneIncidentStart(t, ctx, db, incidentID, 1)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
WHERE incident_id = ? AND cycle_no = 1`, businessbudget.DefaultLimit+1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
WHERE incident_id = ? AND cycle_no = 1 AND business_budget_authorization_id = ?`, 1, incidentID, authorization.ID)
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

func authorizedBudgetStartTask(incidentID, version uint64, authorizationPublicID, identity string) asyncjob.NewTask {
	payload, _ := json.Marshal(map[string]any{
		"mode": "start", "cycle_no": 1,
		"business_budget_authorization_id": authorizationPublicID,
	})
	task := budgetStartTask(incidentID, version, identity)
	task.Payload = payload
	return task
}

func insertSimpleBudgetIncident(t *testing.T, ctx context.Context, db *sql.DB, agentRuns int) (uint64, uint64) {
	t.Helper()
	publicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents
 (public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
  service_name, environment, target_kind, target_name, severity, status, summary,
  first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no)
VALUES (?, ?, ?, 2, 'kind', 'demo', 'checkout', 'demo', 'Deployment', 'checkout',
        'warning', 'DIAGNOSING', 'simple business budget fixture', NOW(6), NOW(6), 1, 3, 'investigating', 1)`,
		publicID, "simple-budget-"+publicID, "v2:"+hashCanonical("simple-budget", publicID))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	var lastRunID uint64
	for index := 0; index < agentRuns; index++ {
		result, err := db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, failure_code,
  completed_at, row_version, domain_schema_version, v3_status, cycle_no, expected_incident_version)
VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 1, '', NOW(6), 1, 3, 'completed', 1, 1)`,
			uuid.NewString(), incidentID)
		if err != nil {
			t.Fatal(err)
		}
		runID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		lastRunID = uint64(runID)
	}
	return uint64(incidentID), lastRunID
}

func insertPartialBudgetPlans(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, count int) {
	t.Helper()
	var start int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(plan_version), 0) FROM remediation_plans WHERE incident_id = ?`, incidentID).Scan(&start); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= count; index++ {
		version := start + index
		if _, err := db.ExecContext(ctx, `INSERT INTO remediation_plans
 (public_id, incident_id, plan_version, plan_hash, status, operation_type,
  target_repository, target_base_revision, target_path, parameters_json,
  evidence_references_json, risk_level, policy_snapshot_hash, expected_before_hash,
  proposed_patch_hash, patch_summary, rollback_plan, validation_plan, row_version,
  domain_schema_version, cycle_no, v3_status, hash_schema_version, canonical_plan_hash)
VALUES (?, ?, ?, ?, 'cancelled', 'restore_required_env', 'acme/gitops', ?, 'apps/demo.yaml',
        JSON_OBJECT(), JSON_ARRAY(), 'low', ?, ?, ?, 'budget fixture', 'manual rollback',
        'manual validation', 1, 3, 1, 'cancelled', 1, ?)`,
			uuid.NewString(), incidentID, version, strings.Repeat("a", 64), strings.Repeat("b", 40),
			strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)); err != nil {
			t.Fatal(err)
		}
	}
}

func insertPartialBudgetVerifications(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, count int) {
	t.Helper()
	var start int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM verification_runs WHERE incident_id = ? AND cycle_no = 1`, incidentID).Scan(&start); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= count; index++ {
		sequence := start + index
		signalPublicID := uuid.NewString()
		result, err := db.ExecContext(ctx, `INSERT INTO incident_signals
 (public_id, incident_id, source, source_event_id, fingerprint, status, severity,
  cluster, namespace, service_name, environment, target_kind, target_name, category,
  occurred_at, received_at, summary, labels_json, annotations_json, domain_schema_version,
  cycle_no, canonical_schema_version, correlation_key_version, alert_instance_key, starts_at, ends_at)
VALUES (?, ?, 'alertmanager', ?, ?, 'resolved', 'warning', 'kind', 'demo', 'checkout',
        'demo', 'Deployment', 'checkout', 'readiness', NOW(6), NOW(6), 'budget signal',
        JSON_OBJECT(), JSON_OBJECT(), 3, 1, 2, 2, ?, NOW(6), NOW(6))`,
			signalPublicID, incidentID, fmt.Sprintf("v2:%064x", sequence), fmt.Sprintf("budget-fingerprint-%d", sequence), fmt.Sprintf("%064x", sequence))
		if err != nil {
			t.Fatal(err)
		}
		signalID, _ := result.LastInsertId()
		if _, err := db.ExecContext(ctx, `INSERT INTO verification_runs
 (public_id, incident_id, remediation_plan_id, change_request_id, status, target_revision,
  plan_json, deadline_at, attempt, row_version, domain_schema_version, cycle_no, v3_status,
  trigger_type, trigger_signal_id, source_revision, image_digest, gitops_revision,
  verification_profile_version, verification_profile_hash, expected_subject_version)
VALUES (?, ?, NULL, NULL, 'cancelled', ?, JSON_OBJECT(), NOW(6), 1, 1, 3, 1, 'cancelled',
        'no_change_signal', ?, ?, ?, ?, 1, ?, 1)`,
			uuid.NewString(), incidentID, strings.Repeat("1", 40), signalID, strings.Repeat("2", 40),
			"sha256:"+strings.Repeat("3", 64), strings.Repeat("4", 40), strings.Repeat("5", 64)); err != nil {
			t.Fatal(err)
		}
	}
}

func authorizeBudgetAgentRun(t *testing.T, ctx context.Context, db *sql.DB, incidentID uint64, reason string) (businessbudget.Authorization, uint64) {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	authorization, result, err := businessbudget.AuthorizeAgentRun(ctx, tx, incidentID, 1, businessbudget.Actor{
		Provider: "github", Login: "operator", Role: "operator", Reason: reason, RequestID: uuid.NewString(),
	})
	if err != nil || authorization.ID == 0 || !result.Allowed() {
		t.Fatalf("authorize AgentRun result=%+v authorization=%+v err=%v", result, authorization, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return authorization, result.IncidentVersion
}

func resolveConcurrentBudgetStarts(t *testing.T, ctx context.Context, repository *asyncjob.Repository, count int, ownerPrefix string) {
	t.Helper()
	executions := make([]*asyncjob.Execution, count)
	for index := range count {
		execution, err := claimIncidentStartEventually(ctx, repository, asyncjob.ClaimRequest{
			Queue: asyncjob.QueueInvestigate, Owner: fmt.Sprintf("%s-worker-%d", ownerPrefix, index), LeaseDuration: 30 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		executions[index] = execution
	}
	start := make(chan struct{})
	errs := make([]error, count)
	var wait sync.WaitGroup
	for index := range executions {
		result := taskhandler.New(taskhandler.Config{})[asyncjob.TaskInvestigationAdvance].Handle(ctx, *executions[index])
		if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
			t.Fatalf("authorized start result=%+v", result)
		}
		wait.Add(1)
		go func(index int, result asyncjob.Result) {
			defer wait.Done()
			<-start
			errs[index] = repository.Resolve(ctx, executions[index].Lease, result)
		}(index, result)
	}
	close(start)
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
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
