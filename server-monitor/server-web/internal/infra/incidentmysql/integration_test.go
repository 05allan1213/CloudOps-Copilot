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
	"server-web/internal/remediation"
	appincident "server-web/internal/service/incident"
	"server-web/internal/verification"
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
	var mysqlVersion string
	if err := sqlDB.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		t.Fatal(err)
	}
	if strings.SplitN(mysqlVersion, ".", 2)[0] != "8" {
		t.Fatalf("requires MySQL 8, got %q", mysqlVersion)
	}
	t.Logf("mysql_version=%s database=%s", mysqlVersion, databaseName)
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
	if err := goose.UpToContext(ctx, sqlDB, ".", 4); err != nil {
		t.Fatalf("migration 3 to 4: %v", err)
	}
	if !tableExists(t, ctx, sqlDB, "changes") || !tableExists(t, ctx, sqlDB, "remediation_plans") || !tableExists(t, ctx, sqlDB, "remediation_approvals") || !tableExists(t, ctx, sqlDB, "change_requests") {
		t.Fatal("version 4 schema boundary is invalid")
	}
	if err := goose.DownToContext(ctx, sqlDB, ".", 3); err != nil {
		t.Fatalf("migration 4 down to 3: %v", err)
	}
	if tableExists(t, ctx, sqlDB, "remediation_plans") || !tableExists(t, ctx, sqlDB, "changes") || !tableExists(t, ctx, sqlDB, "incidents") || !tableExists(t, ctx, sqlDB, "agent_runs") {
		t.Fatal("Phase 4 down migration damaged Phase 1-3 schema")
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 4); err != nil {
		t.Fatalf("repeat migration 3 to 4: %v", err)
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 5); err != nil {
		t.Fatalf("migration 4 to 5: %v", err)
	}
	if !tableExists(t, ctx, sqlDB, "verification_runs") || !tableExists(t, ctx, sqlDB, "verification_checks") {
		t.Fatal("version 5 schema boundary is invalid")
	}
	if err := goose.DownToContext(ctx, sqlDB, ".", 4); err != nil {
		t.Fatalf("migration 5 down to 4: %v", err)
	}
	if tableExists(t, ctx, sqlDB, "verification_runs") || tableExists(t, ctx, sqlDB, "verification_checks") || !tableExists(t, ctx, sqlDB, "change_requests") || !tableExists(t, ctx, sqlDB, "remediation_plans") || !tableExists(t, ctx, sqlDB, "incidents") || !tableExists(t, ctx, sqlDB, "agent_runs") || !tableExists(t, ctx, sqlDB, "changes") {
		t.Fatal("Phase 5 down migration damaged Phase 1-4 schema")
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 5); err != nil {
		t.Fatalf("repeat migration 4 to 5: %v", err)
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 6); err != nil {
		t.Fatalf("migration 5 to 6: %v", err)
	}
	if !tableExists(t, ctx, sqlDB, "postmortems") {
		t.Fatal("version 6 schema boundary is invalid")
	}
	if err := goose.DownToContext(ctx, sqlDB, ".", 5); err != nil {
		t.Fatalf("migration 6 down to 5: %v", err)
	}
	if tableExists(t, ctx, sqlDB, "postmortems") || !tableExists(t, ctx, sqlDB, "verification_runs") || !tableExists(t, ctx, sqlDB, "verification_checks") || !tableExists(t, ctx, sqlDB, "incidents") || !tableExists(t, ctx, sqlDB, "changes") || !tableExists(t, ctx, sqlDB, "change_requests") {
		t.Fatal("Phase 6 down migration damaged Phase 1-5 schema")
	}
	if err := goose.UpToContext(ctx, sqlDB, ".", 6); err != nil {
		t.Fatalf("repeat migration 5 to 6: %v", err)
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
	remediationItem := &domain.Incident{
		PublicID: uuid.NewString(), Fingerprint: "phase4-remediation", CorrelationKey: "v1:" + strings.Repeat("4", 64),
		Cluster: "test", Namespace: "phase4", ServiceName: "remediation", Environment: "test",
		TargetKind: "deployment", TargetName: "remediation", Severity: domain.SeverityWarning,
		Status: domain.StatusDiagnosisCompleted, Summary: "isolated Phase 4 remediation fixture",
		FirstSeenAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), Version: 1,
	}
	if err := store.Create(ctx, remediationItem); err != nil {
		t.Fatal(err)
	}
	var phase5Delivery *remediation.ChangeRequest
	t.Run("remediation approval idempotency hash binding and lease recovery", func(t *testing.T) {
		repository, err := NewRemediationRepository(gormDB)
		if err != nil {
			t.Fatal(err)
		}
		plan := &remediation.RemediationPlan{PublicID: uuid.NewString(), IncidentID: remediationItem.ID, IncidentPublicID: remediationItem.PublicID, PlanVersion: 1, PlanHash: strings.Repeat("a", 64), Status: remediation.PlanAwaitingApproval, OperationType: remediation.OperationSetReplicas, TargetRepository: "acme/gitops", TargetBaseRevision: strings.Repeat("b", 40), TargetPath: "apps/api.yaml", Parameters: remediation.Parameters{Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "api"}}, EvidenceReferences: []string{uuid.NewString()}, RiskLevel: remediation.RiskLow, PolicySnapshotHash: strings.Repeat("c", 64), ExpectedBeforeHash: strings.Repeat("d", 64), ProposedPatchHash: strings.Repeat("e", 64), PatchSummary: "set replicas", RollbackPlan: "revert", ValidationPlan: "checks", RowVersion: 1}
		if err := repository.CreatePlan(ctx, plan); err != nil {
			t.Fatal(err)
		}
		badApproval := remediation.Approval{PublicID: uuid.NewString(), Decision: remediation.DecisionApproved, Actor: "admin", ApprovedPlanHash: strings.Repeat("f", 64), ApprovedPatchHash: plan.ProposedPatchHash}
		delivery := &remediation.ChangeRequest{PublicID: uuid.NewString(), Repository: plan.TargetRepository, BaseRevision: plan.TargetBaseRevision, HeadBranch: "cloudops/incident-" + remediationItem.PublicID + "/remediation-" + plan.PublicID, Status: remediation.DeliveryPending, CIStatus: remediation.CIPending, IdempotencyKey: strings.Repeat("1", 64), RowVersion: 1}
		if _, _, err := repository.ApprovePlan(ctx, plan.PublicID, plan.RowVersion, badApproval, delivery); !errors.Is(err, remediation.ErrApprovalMismatch) {
			t.Fatalf("approval hash mismatch err=%v", err)
		}
		approval := badApproval
		approval.PublicID, approval.ApprovedPlanHash = uuid.NewString(), plan.PlanHash
		approved, createdDelivery, err := repository.ApprovePlan(ctx, plan.PublicID, plan.RowVersion, approval, delivery)
		if err != nil || approved.Status != remediation.PlanDeliveryPending || createdDelivery.ID == 0 {
			t.Fatalf("approved=%+v delivery=%+v err=%v", approved, createdDelivery, err)
		}
		_, replayDelivery, err := repository.ApprovePlan(ctx, plan.PublicID, plan.RowVersion, approval, delivery)
		if err != nil || replayDelivery.ID != createdDelivery.ID {
			t.Fatalf("approval replay was not idempotent: delivery=%+v err=%v", replayDelivery, err)
		}
		var approvalCount, deliveryCount int64
		if err := gormDB.Model(&remediationApprovalRow{}).Where("plan_id = ?", plan.ID).Count(&approvalCount).Error; err != nil || approvalCount != 1 {
			t.Fatalf("approval uniqueness count=%d err=%v", approvalCount, err)
		}
		if err := gormDB.Model(&changeRequestRow{}).Where("plan_id = ?", plan.ID).Count(&deliveryCount).Error; err != nil || deliveryCount != 1 {
			t.Fatalf("delivery uniqueness count=%d err=%v", deliveryCount, err)
		}
		rejection := remediation.Approval{PublicID: uuid.NewString(), Decision: remediation.DecisionRejected, Actor: "other-admin", ApprovedPlanHash: plan.PlanHash, ApprovedPatchHash: plan.ProposedPatchHash}
		if _, err := repository.RejectPlan(ctx, plan.PublicID, approved.RowVersion, rejection); !errors.Is(err, remediation.ErrConflict) {
			t.Fatalf("approved plan accepted a second decision: %v", err)
		}
		claimed, claimedPlan, err := repository.ClaimDelivery(ctx, "worker-a", time.Now().UTC(), time.Second)
		if err != nil || claimed.Status != remediation.DeliveryDelivering || claimedPlan.IncidentPublicID != remediationItem.PublicID {
			t.Fatalf("claim=%+v plan=%+v err=%v", claimed, claimedPlan, err)
		}
		if err := gormDB.Model(&changeRequestRow{}).Where("id = ?", claimed.ID).Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		reclaimed, _, err := repository.ClaimDelivery(ctx, "worker-b", time.Now().UTC(), time.Second)
		if err != nil || reclaimed.LeaseOwner != "worker-b" || reclaimed.Attempts != 2 {
			t.Fatalf("expired lease not recovered: %+v err=%v", reclaimed, err)
		}
		phase5Delivery = reclaimed
	})

	t.Run("phase5 verification claim recovery stability and atomic resolve", func(t *testing.T) {
		if phase5Delivery == nil {
			t.Fatal("missing Phase 4 ChangeRequest fixture")
		}
		now := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
		revision := strings.Repeat("9", 40)
		updates := map[string]any{
			"status": "delivered", "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil,
			"pr_number": int64(42), "commit_sha": strings.Repeat("8", 40), "pr_state": "closed", "merged_commit_sha": revision, "target_revision": revision,
			"argocd_application": "remediation", "argocd_project": "test", "detected_revision": revision,
			"argocd_sync_status": "Synced", "argocd_operation_phase": "Succeeded", "argocd_health_status": "Healthy",
			"resource_health_json": json.RawMessage(`[{"kind":"Deployment","health":"Healthy"}]`),
			"cluster":              "test", "environment": "test", "namespace": "phase4", "workload_kind": "Deployment", "workload_name": "remediation",
			"delivery_started_at": now.Add(-time.Minute), "delivery_deadline_at": now.Add(time.Hour), "delivery_completed_at": now,
			"row_version": gorm.Expr("row_version + 1"),
		}
		if err := gormDB.Table("change_requests").Where("id = ?", phase5Delivery.ID).Updates(updates).Error; err != nil {
			t.Fatal(err)
		}
		repository, err := NewVerificationRepository(gormDB)
		if err != nil {
			t.Fatal(err)
		}
		delivery, err := repository.FindDeliveredWithoutRun(ctx)
		if err != nil || delivery.TargetRevision != revision || delivery.DetectedRevision != revision {
			t.Fatalf("exact delivered projection=%+v err=%v", delivery, err)
		}
		plan, err := verification.CompileTrustedPlan(verification.Subject{Repository: delivery.Repository, PullRequest: delivery.PRNumber, Revision: revision, ArgoApplication: delivery.ArgoApplication, ArgoProject: delivery.ArgoProject, Cluster: delivery.Cluster, Environment: delivery.Environment, Namespace: delivery.Namespace, WorkloadKind: delivery.WorkloadKind, WorkloadName: delivery.WorkloadName, AlertFingerprint: delivery.IncidentFingerprint}, verification.CompilerConfig{PollInterval: time.Second, Timeout: time.Minute, StabilityWindow: 2 * time.Second, AlertLookback: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		created, err := repository.CreateRun(ctx, delivery, plan, now)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := repository.CreateRun(ctx, delivery, plan, now)
		if err != nil || replayed.PublicID != created.PublicID {
			t.Fatalf("run identity replay=%+v err=%v", replayed, err)
		}

		const claimers = 8
		var wg sync.WaitGroup
		claims := make(chan *verification.Run, claimers)
		errs := make(chan error, claimers)
		for index := range claimers {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				run, claimErr := repository.ClaimRun(context.Background(), "phase5-worker-"+strconv.Itoa(index), now.Add(time.Second), 10*time.Second)
				if claimErr == nil {
					claims <- run
				} else if !errors.Is(claimErr, verification.ErrNotFound) {
					errs <- claimErr
				}
			}(index)
		}
		wg.Wait()
		close(claims)
		close(errs)
		for claimErr := range errs {
			t.Error(claimErr)
		}
		var winner *verification.Run
		winnerCount := 0
		for claim := range claims {
			winner, winnerCount = claim, winnerCount+1
		}
		if winnerCount != 1 {
			t.Fatalf("verification claim winners=%d", winnerCount)
		}
		checks, err := repository.ListChecks(ctx, winner.ID)
		if err != nil || len(checks) != len(plan.Checks) {
			t.Fatalf("checks=%d err=%v", len(checks), err)
		}
		stale := *winner
		stale.RowVersion--
		if err := repository.PersistCheckSample(ctx, &stale, &checks[0], verification.Sample{Status: verification.SamplePassed, Observed: json.RawMessage(`{"ok":true}`)}, now.Add(2*time.Second), now.Add(3*time.Second)); !errors.Is(err, verification.ErrLeaseLost) {
			t.Fatalf("stale writer accepted: %v", err)
		}
		if err := gormDB.Table("verification_runs").Where("id = ?", winner.ID).Update("lease_expires_at", now).Error; err != nil {
			t.Fatal(err)
		}
		takeover, err := repository.ClaimRun(ctx, "recovery-worker", now.Add(2*time.Second), 10*time.Second)
		if err != nil || !takeover.LeaseTakeover {
			t.Fatalf("expired lease takeover=%+v err=%v", takeover, err)
		}
		if err := repository.ReleaseRun(ctx, takeover, now.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}

		for round, at := range []time.Time{now.Add(3 * time.Second), now.Add(6 * time.Second)} {
			for index := range checks {
				run, claimErr := repository.ClaimRun(ctx, "check-worker", at.Add(time.Duration(index)*time.Millisecond), 10*time.Second)
				if claimErr != nil {
					t.Fatal(claimErr)
				}
				currentChecks, listErr := repository.ListChecks(ctx, run.ID)
				if listErr != nil {
					t.Fatal(listErr)
				}
				current := &currentChecks[index]
				if persistErr := repository.PersistCheckSample(ctx, run, current, verification.Sample{Status: verification.SamplePassed, Observed: json.RawMessage(`{"ok":true}`), SourceReference: "deterministic:test"}, at.Add(time.Duration(index)*time.Millisecond), at.Add(time.Second)); persistErr != nil {
					t.Fatalf("round=%d check=%d: %v", round, index, persistErr)
				}
			}
		}
		aggregateOwner, err := repository.ClaimRun(ctx, "aggregate-worker", now.Add(7*time.Second), 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		completed, err := repository.AggregateRun(ctx, aggregateOwner, now.Add(8*time.Second))
		if err != nil || completed.Status != verification.RunPassed {
			t.Fatalf("aggregate=%+v err=%v", completed, err)
		}
		var incidentAfter incidentRow
		if err := gormDB.First(&incidentAfter, remediationItem.ID).Error; err != nil || incidentAfter.Status != string(domain.StatusResolved) || incidentAfter.ResolvedAt == nil {
			t.Fatalf("incident not resolved exactly after verification: %+v err=%v", incidentAfter, err)
		}
		if _, err := repository.ClaimRun(ctx, "replay-worker", now.Add(time.Minute), time.Second); !errors.Is(err, verification.ErrNotFound) {
			t.Fatalf("terminal run replayable: %v", err)
		}
		var resolvedFacts, resolvedOutbox int64
		if err := gormDB.Table("incident_events").Where("incident_id = ? AND event_type = ?", remediationItem.ID, "incident_resolved_after_verification").Count(&resolvedFacts).Error; err != nil {
			t.Fatal(err)
		}
		if err := gormDB.Table("outbox_events").Where("aggregate_id = ? AND event_type = ?", remediationItem.PublicID, "incident_resolved_after_verification").Count(&resolvedOutbox).Error; err != nil {
			t.Fatal(err)
		}
		if resolvedFacts != 1 || resolvedOutbox != 1 {
			t.Fatalf("resolved facts timeline=%d outbox=%d", resolvedFacts, resolvedOutbox)
		}
		postmortem, err := repository.GetPostmortem(ctx, remediationItem.PublicID)
		if err != nil || postmortem.VerificationRunPublicID != completed.PublicID || postmortem.GenerationVersion != 1 || postmortem.RootCause.Classification == "fact" {
			t.Fatalf("postmortem integrity=%+v err=%v", postmortem, err)
		}
		var postmortemCount int64
		if err := gormDB.Table("postmortems").Where("incident_id = ?", remediationItem.ID).Count(&postmortemCount).Error; err != nil || postmortemCount != 1 {
			t.Fatalf("postmortem count=%d err=%v", postmortemCount, err)
		}

		// An explicit operator retry appends attempt 2; it never changes attempt 1.
		if err := gormDB.Model(&incidentRow{}).Where("id = ? AND version = ?", incidentAfter.ID, incidentAfter.Version).Updates(map[string]any{"status": domain.StatusApplyingChange, "resolved_at": nil, "version": incidentAfter.Version + 1}).Error; err != nil {
			t.Fatal(err)
		}
		retryAt := now.Add(10 * time.Minute)
		retry, err := repository.CreateRetryRun(ctx, delivery, plan, retryAt)
		if err != nil || retry.Attempt != 2 || retry.PublicID == completed.PublicID {
			t.Fatalf("retry=%+v err=%v", retry, err)
		}
		timeoutOwner, err := repository.ClaimRun(ctx, "timeout-worker", retryAt.Add(2*time.Minute), 10*time.Second)
		if err != nil || timeoutOwner.Attempt != 2 {
			t.Fatalf("timeout claim=%+v err=%v", timeoutOwner, err)
		}
		if err := repository.TimeoutRun(ctx, timeoutOwner, retryAt.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		var attempts []verificationRunRow
		if err := gormDB.Where("change_request_id = ?", delivery.ID).Order("attempt ASC").Find(&attempts).Error; err != nil || len(attempts) != 2 || attempts[0].Status != string(verification.RunPassed) || attempts[1].Status != string(verification.RunTimedOut) {
			t.Fatalf("preserved attempts=%+v err=%v", attempts, err)
		}
		incidentAfter = incidentRow{}
		if err := gormDB.First(&incidentAfter, remediationItem.ID).Error; err != nil || incidentAfter.Status != string(domain.StatusDiagnosing) || incidentAfter.ResolvedAt != nil {
			t.Fatalf("timeout did not return to investigation: %+v err=%v", incidentAfter, err)
		}

		// A later successful Phase 6 attempt persists an observability check and
		// rebinds the one Postmortem to the final passing attempt.
		if err := gormDB.Model(&incidentRow{}).Where("id = ? AND version = ?", incidentAfter.ID, incidentAfter.Version).Updates(map[string]any{"status": domain.StatusApplyingChange, "version": incidentAfter.Version + 1}).Error; err != nil {
			t.Fatal(err)
		}
		phase6Subject := verification.Subject{Repository: delivery.Repository, PullRequest: delivery.PRNumber, Revision: revision, ArgoApplication: delivery.ArgoApplication, ArgoProject: delivery.ArgoProject, Cluster: delivery.Cluster, Environment: delivery.Environment, Namespace: delivery.Namespace, Service: delivery.ServiceName, WorkloadKind: delivery.WorkloadKind, WorkloadName: delivery.WorkloadName, AlertFingerprint: delivery.IncidentFingerprint}
		profiles := verification.Profiles{Items: []verification.Profile{{ID: "mysql-phase6", Service: delivery.ServiceName, Environment: delivery.Environment, Namespace: delivery.Namespace, Workload: delivery.WorkloadName, Templates: []verification.Template{{ID: "metric-error-v1", Type: verification.CheckMetricErrorRateBelow, Required: true, Comparison: verification.CompareLTE, Threshold: .01, LookbackSeconds: 60, TimeoutSeconds: 60, StabilitySeconds: 2}}}}}
		if err := profiles.Validate(); err != nil {
			t.Fatal(err)
		}
		phase6Plan, err := verification.CompileTrustedPlanWithProfile(phase6Subject, verification.CompilerConfig{PollInterval: time.Second, Timeout: time.Minute, StabilityWindow: 2 * time.Second, AlertLookback: time.Minute}, &profiles.Items[0])
		if err != nil {
			t.Fatal(err)
		}
		finalAt := retryAt.Add(5 * time.Minute)
		finalRun, err := repository.CreateRetryRun(ctx, delivery, phase6Plan, finalAt)
		if err != nil || finalRun.Attempt != 3 {
			t.Fatalf("final retry=%+v err=%v", finalRun, err)
		}
		for _, sampledAt := range []time.Time{finalAt.Add(time.Second), finalAt.Add(4 * time.Second)} {
			for index := range phase6Plan.Checks {
				owner, claimErr := repository.ClaimRun(ctx, "phase6-worker", sampledAt.Add(time.Duration(index)*time.Millisecond), 10*time.Second)
				if claimErr != nil {
					t.Fatal(claimErr)
				}
				items, listErr := repository.ListChecks(ctx, owner.ID)
				if listErr != nil {
					t.Fatal(listErr)
				}
				if persistErr := repository.PersistCheckSample(ctx, owner, &items[index], verification.Sample{Status: verification.SamplePassed, Observed: json.RawMessage(`{"status":"available","value":0.01,"sample_count":1}`), SourceReference: "prometheus://trusted"}, sampledAt.Add(time.Duration(index)*time.Millisecond), sampledAt.Add(time.Second)); persistErr != nil {
					t.Fatal(persistErr)
				}
			}
		}
		finalOwner, err := repository.ClaimRun(ctx, "phase6-aggregate", finalAt.Add(6*time.Second), 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		finalCompleted, err := repository.AggregateRun(ctx, finalOwner, finalAt.Add(7*time.Second))
		if err != nil || finalCompleted.Status != verification.RunPassed {
			t.Fatalf("final aggregate=%+v err=%v", finalCompleted, err)
		}
		finalPostmortem, err := repository.GetPostmortem(ctx, remediationItem.PublicID)
		if err != nil || finalPostmortem.PublicID != postmortem.PublicID || finalPostmortem.VerificationRunPublicID != finalCompleted.PublicID {
			t.Fatalf("final postmortem=%+v err=%v", finalPostmortem, err)
		}
		var observabilityChecks int64
		if err := gormDB.Table("verification_checks").Where("verification_run_id = ? AND check_type = ?", finalCompleted.ID, verification.CheckMetricErrorRateBelow).Count(&observabilityChecks).Error; err != nil || observabilityChecks != 1 {
			t.Fatalf("phase6 checks=%d err=%v", observabilityChecks, err)
		}
	})
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
