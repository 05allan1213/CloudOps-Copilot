package incidentmysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"server-web/internal/agent"
	"server-web/internal/change"
	domain "server-web/internal/incident"
	"server-web/internal/infra/webhook"
	appincident "server-web/internal/service/incident"
	"server-web/migrations"
)

func TestMySQLMigrationRepositoryAndConcurrentIngestion(t *testing.T) {
	dsn := os.Getenv("CLOUDOPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_DSN is not set; requires a disposable MySQL 8 test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	var databaseName string
	if err := sqlDB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(databaseName), "test") {
		t.Fatalf("refusing destructive migration test against non-test database %q", databaseName)
	}
	var existing int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'incidents'").Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 0 {
		t.Fatal("test requires an empty disposable database without incidents table")
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("mysql"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 2); err != nil {
		t.Fatalf("empty database 0 to 2 migration: %v", err)
	}
	if tableExists(t, ctx, sqlDB, "changes") || !tableExists(t, ctx, sqlDB, "agent_runs") {
		t.Fatal("version 2 schema boundary is invalid")
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 3); err != nil {
		t.Fatalf("migration 2 to 3: %v", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("repeat up migration: %v", err)
	}
	if !tableExists(t, ctx, sqlDB, "changes") {
		t.Fatal("version 3 changes table missing")
	}
	if err := goose.DownToContext(ctx, sqlDB, ".", 2); err != nil {
		t.Fatalf("migration 3 down to 2: %v", err)
	}
	if tableExists(t, ctx, sqlDB, "changes") || !tableExists(t, ctx, sqlDB, "incidents") || !tableExists(t, ctx, sqlDB, "agent_runs") {
		t.Fatal("Phase 3 down migration damaged Phase 1 or Phase 2 schema")
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 3); err != nil {
		t.Fatalf("repeat migration 2 to 3: %v", err)
	}
	defer func() {
		if err := goose.DownToContext(context.Background(), sqlDB, ".", 0); err != nil {
			t.Errorf("cleanup down migration: %v", err)
		}
	}()

	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(gormDB)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("concurrent duplicate ingestion", func(t *testing.T) {
		now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
		service, err := appincident.NewService(appincident.Config{UnitOfWork: store, AggregationWindow: 4 * time.Hour, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		payload := webhook.AlertmanagerWebhookRequest{Alerts: []webhook.AlertRecord{{Status: "firing", Fingerprint: "mysql-concurrent", StartsAt: now, Labels: map[string]string{"alertname": "Down", "cluster": "test", "namespace": "default", "service": "checkout", "severity": "critical"}, Annotations: map[string]string{"summary": "checkout down"}}}}
		const workers = 16
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := service.IngestAlertmanager(context.Background(), payload)
				if err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		var incidentCount, signalCount int64
		if err := gormDB.Model(&incidentRow{}).Count(&incidentCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := gormDB.Model(&signalRow{}).Count(&signalCount).Error; err != nil {
			t.Fatal(err)
		}
		if incidentCount != 1 || signalCount != 1 {
			t.Fatalf("incidents=%d signals=%d", incidentCount, signalCount)
		}
	})

	item, err := store.GetByPublicID(ctx, findOnlyPublicID(t, gormDB))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("change idempotency filtering foreign key and atomic evidence", func(t *testing.T) {
		repository, err := NewChangeRepository(gormDB)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := change.New(item.ID, change.SourceGitHubCommit, item.PublicID, "acme/api", "abcdef1")
		if err != nil {
			t.Fatal(err)
		}
		candidate.Repository, candidate.RepositoryOwner, candidate.CommitSHA = "acme/api", "acme", "abcdef1"
		candidate.ServiceName, candidate.Namespace = item.ServiceName, item.Namespace
		candidate.Status, candidate.Category, candidate.CorrelationScore = change.StatusMatched, change.CategoryConfirmed, 100
		candidate.CorrelationReasons = []string{"revision_exact"}
		evidence := &domain.EvidenceItem{PublicID: uuid.NewString(), IncidentID: item.ID, Type: "change", Source: "github_commit", ResourceRef: candidate.PublicID, TimeRange: json.RawMessage(`{}`), Query: "deterministic change correlation", Summary: "bounded", Facts: json.RawMessage(`{"category":"confirmed_match","status":"matched"}`), CollectedAt: time.Now().UTC()}
		created, err := repository.PersistWithEvidence(ctx, candidate, evidence)
		if err != nil || !created {
			t.Fatalf("created=%v err=%v", created, err)
		}
		duplicate, _ := change.New(item.ID, change.SourceGitHubCommit, item.PublicID, "acme/api", "abcdef1")
		duplicate.Repository, duplicate.CommitSHA = "acme/api", "abcdef1"
		created, err = repository.PersistWithEvidence(ctx, duplicate, &domain.EvidenceItem{PublicID: uuid.NewString(), IncidentID: item.ID, Facts: json.RawMessage(`{}`)})
		if err != nil || created || duplicate.PublicID != candidate.PublicID {
			t.Fatalf("idempotent replay created=%v duplicate=%+v err=%v", created, duplicate, err)
		}
		page, err := repository.ListByIncident(ctx, item.PublicID, change.ListFilter{SourceType: change.SourceGitHubCommit, Status: change.StatusMatched, Category: change.CategoryConfirmed, Page: 1, PageSize: 10})
		if err != nil || page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("filtered changes=%+v err=%v", page, err)
		}
		var evidenceCount int64
		if err := gormDB.Model(&evidenceRow{}).Where("change_id = ?", candidate.ID).Count(&evidenceCount).Error; err != nil || evidenceCount != 1 {
			t.Fatalf("atomic evidence count=%d err=%v", evidenceCount, err)
		}
		var storedEvidence evidenceRow
		if err := gormDB.Where("change_id = ?", candidate.ID).First(&storedEvidence).Error; err != nil || !storedEvidence.Valid || len(storedEvidence.ResultHash) != 64 || !json.Valid(storedEvidence.RedactionJSON) {
			t.Fatalf("evidence validity metadata=%+v err=%v", storedEvidence, err)
		}
		invalid, _ := change.New(item.ID+999999, change.SourceGitHubCommit, "foreign")
		invalid.CommitSHA = "abcdef2"
		if _, err := repository.CreateIfAbsent(ctx, invalid); err == nil {
			t.Fatal("expected incident foreign key rejection")
		}
		rolledBack, _ := change.New(item.ID, change.SourceGitHubCommit, "rollback")
		rolledBack.CommitSHA = "abcdef3"
		badEvidence := &domain.EvidenceItem{PublicID: evidence.PublicID, IncidentID: item.ID, Facts: json.RawMessage(`{}`)}
		if _, err := repository.PersistWithEvidence(ctx, rolledBack, badEvidence); err == nil {
			t.Fatal("expected evidence conflict rollback")
		}
		if _, err := repository.GetByPublicID(ctx, rolledBack.PublicID); !errors.Is(err, change.ErrNotFound) {
			t.Fatalf("rolled back Change visible: %v", err)
		}
	})
	t.Run("optimistic lock", func(t *testing.T) {
		copy := *item
		expected := copy.Version
		copy.Summary = "updated"
		copy.Version++
		copy.UpdatedAt = time.Now().UTC()
		if err := store.Update(ctx, &copy, expected); err != nil {
			t.Fatal(err)
		}
		stale := *item
		stale.Version++
		if err := store.Update(ctx, &stale, expected); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("expected optimistic conflict, got %v", err)
		}
		item = &copy
	})

	t.Run("agent and outbox unique constraints", func(t *testing.T) {
		repos := store.ReadRepositories()
		run := &domain.AgentRun{PublicID: uuid.NewString(), IncidentID: item.ID, Status: domain.AgentRunPending, Model: "contract-only", PromptVersion: "v1", MaxSteps: 8, CurrentCheckpoint: json.RawMessage(`{}`), FinalDiagnosis: json.RawMessage(`{}`)}
		if err := repos.AgentRuns.Create(ctx, run); err != nil {
			t.Fatal(err)
		}
		if err := repos.AgentRuns.Transition(ctx, run.ID, domain.AgentRunPending, domain.AgentRunRunning, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		loadedRun, err := repos.AgentRuns.GetByPublicID(ctx, run.PublicID)
		if err != nil || loadedRun.Status != domain.AgentRunRunning || loadedRun.StartedAt == nil {
			t.Fatalf("unexpected loaded AgentRun: run=%+v err=%v", loadedRun, err)
		}
		step := &domain.AgentStep{PublicID: uuid.NewString(), AgentRunID: run.ID, Sequence: 1, StepType: "contract", ShortReason: "bounded summary", Arguments: json.RawMessage(`{}`), ResultSummary: "not executed", Status: domain.AgentStepCompleted}
		if err := repos.AgentSteps.Create(ctx, step); err != nil {
			t.Fatal(err)
		}
		duplicate := *step
		duplicate.ID, duplicate.PublicID = 0, uuid.NewString()
		if err := repos.AgentSteps.Create(ctx, &duplicate); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("expected AgentStep sequence conflict, got %v", err)
		}
		steps, err := repos.AgentSteps.ListByRun(ctx, run.ID, 10)
		if err != nil || len(steps) != 1 || steps[0].Sequence != 1 {
			t.Fatalf("unexpected AgentStep list: steps=%+v err=%v", steps, err)
		}
		evidence := &domain.EvidenceItem{PublicID: uuid.NewString(), IncidentID: item.ID, AgentRunID: &run.ID, Type: "contract", Source: "test", TimeRange: json.RawMessage(`{}`), Query: "bounded", Summary: "bounded evidence", Facts: json.RawMessage(`{"fact":"value"}`), CollectedAt: time.Now().UTC()}
		if err := repos.Evidence.Create(ctx, evidence); err != nil {
			t.Fatal(err)
		}
		evidenceItems, err := repos.Evidence.ListByIncident(ctx, item.ID, 10)
		foundEvidence := false
		for _, candidate := range evidenceItems {
			foundEvidence = foundEvidence || candidate.PublicID == evidence.PublicID
		}
		if err != nil || !foundEvidence {
			t.Fatalf("unexpected Evidence list: items=%+v err=%v", evidenceItems, err)
		}
		event := &domain.OutboxEvent{EventID: uuid.NewString(), AggregateType: "incident", AggregateID: item.PublicID, EventType: "test", SchemaVersion: 1, Payload: json.RawMessage(`{}`), OccurredAt: time.Now().UTC()}
		if err := repos.Outbox.Add(ctx, event); err != nil {
			t.Fatal(err)
		}
		duplicateEvent := *event
		duplicateEvent.ID = 0
		if err := repos.Outbox.Add(ctx, &duplicateEvent); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("expected outbox event ID conflict, got %v", err)
		}
		if err := repos.AgentRuns.Transition(ctx, run.ID, domain.AgentRunRunning, domain.AgentRunCompleted, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("concurrent active run creation and pending cancellation", func(t *testing.T) {
		limits := agent.Limits{MaxSteps: 12, MaxToolCalls: 6, MaxModelCalls: 8, TokenBudget: 12000, MaxEvidenceItems: 12, MaxRuntime: 2 * time.Minute, ToolTimeout: 15 * time.Second, MaxEvidenceBytes: 16384, MaxCheckpointSize: 32768, MaxStepRetries: 1}
		now := time.Now().UTC()
		const creators = 8
		var wg sync.WaitGroup
		created := make(chan *agent.Run, creators)
		failures := make(chan error, creators)
		for index := range creators {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				run, createErr := store.CreateRun(context.Background(), agent.CreateRunRequest{IncidentPublicID: item.PublicID, IdempotencyKey: "concurrent-create-" + strconv.Itoa(index), Model: "fake", PromptVersion: "test-v1", Limits: limits, Checkpoint: json.RawMessage(`{"schema_version":1,"next_node":"load_incident"}`), At: now})
				if createErr == nil {
					created <- run
				} else if !errors.Is(createErr, agent.ErrConflict) {
					failures <- createErr
				}
			}(index)
		}
		wg.Wait()
		close(created)
		close(failures)
		for createErr := range failures {
			t.Error(createErr)
		}
		var winner *agent.Run
		count := 0
		for run := range created {
			winner, count = run, count+1
		}
		if count != 1 {
			t.Fatalf("active run winners=%d want=1", count)
		}
		if err := store.RequestCancel(ctx, winner.PublicID, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		cancelled, err := store.GetRun(ctx, winner.PublicID)
		if err != nil || cancelled.Status != agent.RunCancelled || cancelled.FinishedAt == nil {
			t.Fatalf("cancelled=%+v err=%v", cancelled, err)
		}
	})

	t.Run("bounded runtime lease optimistic concurrency and crash recovery", func(t *testing.T) {
		limits := agent.Limits{MaxSteps: 12, MaxToolCalls: 6, MaxModelCalls: 8, TokenBudget: 12000, MaxEvidenceItems: 12, MaxRuntime: 2 * time.Minute, ToolTimeout: 15 * time.Second, MaxEvidenceBytes: 16384, MaxCheckpointSize: 32768, MaxStepRetries: 1}
		now := time.Now().UTC()
		request := agent.CreateRunRequest{IncidentPublicID: item.PublicID, IdempotencyKey: "mysql-agent-idempotency", Model: "fake", PromptVersion: "test-v1", Limits: limits, Checkpoint: json.RawMessage(`{"schema_version":1,"next_node":"load_incident"}`), At: now}
		run, err := store.CreateRun(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		idempotent, err := store.CreateRun(ctx, request)
		if err != nil || idempotent.PublicID != run.PublicID {
			t.Fatalf("idempotent create run=%+v err=%v", idempotent, err)
		}
		conflicting := request
		conflicting.IdempotencyKey = "different-active-key"
		if _, err := store.CreateRun(ctx, conflicting); !errors.Is(err, agent.ErrConflict) {
			t.Fatalf("expected one-active-run conflict, got %v", err)
		}

		const claimers = 8
		var wg sync.WaitGroup
		claimed := make(chan *agent.Run, claimers)
		claimErrors := make(chan error, claimers)
		for index := range claimers {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				candidate, claimErr := store.ClaimNext(context.Background(), "worker-"+strconv.Itoa(index), now.Add(time.Second), 5*time.Second)
				if claimErr == nil {
					claimed <- candidate
				} else if !errors.Is(claimErr, agent.ErrNotFound) {
					claimErrors <- claimErr
				}
			}(index)
		}
		wg.Wait()
		close(claimed)
		close(claimErrors)
		for claimErr := range claimErrors {
			t.Error(claimErr)
		}
		var first *agent.Run
		count := 0
		for candidate := range claimed {
			first, count = candidate, count+1
		}
		if count != 1 {
			t.Fatalf("concurrent claims=%d want=1", count)
		}
		if err := store.Heartbeat(ctx, first.ID, "wrong-worker", now.Add(2*time.Second), 5*time.Second); !errors.Is(err, agent.ErrLeaseLost) {
			t.Fatalf("wrong owner heartbeat=%v", err)
		}
		if err := store.Heartbeat(ctx, first.ID, first.LeaseOwner, now.Add(2*time.Second), 5*time.Second); err != nil {
			t.Fatalf("lease owner heartbeat=%v", err)
		}
		step, err := store.BeginStep(ctx, first, agent.StepStart{Node: agent.NodeExecuteTool, Reason: "simulate crash after step begin", SelectedTool: "alert.list_active", Arguments: json.RawMessage(`{}`), ArgumentsHash: strings.Repeat("a", 64), Budgeted: true, At: now.Add(2 * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		stale := *first
		stale.RowVersion--
		if err := store.FinishStep(ctx, &stale, step, agent.StepFinish{Status: agent.StepCompleted, Checkpoint: json.RawMessage(`{}`), CheckpointSchema: 1, At: now.Add(3 * time.Second)}); !errors.Is(err, agent.ErrLeaseLost) {
			t.Fatalf("expected optimistic conflict, got %v", err)
		}

		recovered, err := store.ClaimNext(ctx, "recovery-worker", now.Add(10*time.Second), 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		steps, err := store.ListSteps(ctx, recovered.PublicID, 100)
		if err != nil || len(steps) != 1 || steps[0].Status != agent.StepFailed || steps[0].ErrorCode != agent.ErrorLeaseLost {
			t.Fatalf("orphan step not closed safely: steps=%+v err=%v", steps, err)
		}
		persistStep, err := store.BeginStep(ctx, recovered, agent.StepStart{Node: agent.NodePersistObservation, Reason: "commit evidence", At: now.Add(11 * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		evidence := agent.EvidenceRecord{SourceType: "tool_observation", ToolName: "alert.list_active", Summary: "bounded", Facts: json.RawMessage(`{"active":1}`), ResultHash: strings.Repeat("b", 64), Redaction: json.RawMessage(`{}`), Valid: true, IdempotencyKey: strings.Repeat("c", 64), CollectedAt: now.Add(11 * time.Second)}
		checkpoint := json.RawMessage(`{"schema_version":1,"next_node":"evaluate_coverage"}`)
		persisted, err := store.PersistEvidence(ctx, recovered, persistStep, evidence, agent.StepFinish{Status: agent.StepCompleted, Usage: agent.Usage{Evidence: 1}, Checkpoint: checkpoint, CheckpointHash: strings.Repeat("d", 64), CheckpointSchema: 1, At: now.Add(12 * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		duplicateStep, err := store.BeginStep(ctx, recovered, agent.StepStart{Node: agent.NodePersistObservation, Reason: "crash replay", At: now.Add(13 * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		duplicate, err := store.PersistEvidence(ctx, recovered, duplicateStep, evidence, agent.StepFinish{Status: agent.StepCompleted, Usage: agent.Usage{Evidence: 1}, Checkpoint: checkpoint, CheckpointHash: strings.Repeat("e", 64), CheckpointSchema: 1, At: now.Add(14 * time.Second)})
		if err != nil || duplicate.PublicID != persisted.PublicID || recovered.Usage.Evidence != 1 {
			t.Fatalf("evidence replay duplicated: first=%+v duplicate=%+v usage=%+v err=%v", persisted, duplicate, recovered.Usage, err)
		}
		if err := store.FinishRun(ctx, recovered, agent.RunCompleted, agent.Diagnosis{Summary: "validated", Unknowns: []string{"none"}}, "", "", now.Add(15*time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ClaimNext(ctx, "late-worker", now.Add(time.Minute), 10*time.Second); !errors.Is(err, agent.ErrNotFound) {
			t.Fatalf("completed run was reclaimable: %v", err)
		}
		item.Status = domain.StatusDiagnosisCompleted
	})

	t.Run("transaction rollback", func(t *testing.T) {
		publicID := uuid.NewString()
		sentinel := errors.New("rollback")
		err := store.WithinTransaction(ctx, func(repos domain.Repositories) error {
			created := &domain.Incident{PublicID: publicID, Fingerprint: "rollback", CorrelationKey: "v1:" + strings.Repeat("a", 64), Cluster: "test", Namespace: "default", ServiceName: "rollback", Environment: "test", TargetKind: "deployment", TargetName: "rollback", Severity: domain.SeverityInfo, Status: domain.StatusDetected, FirstSeenAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), Version: 1}
			if err := repos.Incidents.Create(ctx, created); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected rollback sentinel, got %v", err)
		}
		if _, err := store.GetByPublicID(ctx, publicID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("rolled back Incident is visible: %v", err)
		}
	})

	t.Run("pagination and filtering", func(t *testing.T) {
		page, err := store.List(ctx, domain.ListFilter{Status: item.Status, Severity: item.Severity, Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, Page: 1, PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("unexpected filtered page: %+v", page)
		}
	})

	if err := goose.DownToContext(ctx, sqlDB, ".", 0); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'incidents'").Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 0 {
		t.Fatal("down migration left incidents table")
	}
}

func findOnlyPublicID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var rows []incidentRow
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one Incident, got %d", len(rows))
	}
	return rows[0].PublicID
}

func tableExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}
