package incidentstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestMySQLIncidentResolvedSignalCreatesNoChangeVerificationAndFencesAgent(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	identity := insertNoChangeBaselineFixture(t, ctx, db)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := strings.Repeat("4", 64)
	firing := incidentIntegrationSignal(201, correlationKey)
	created, err := store.IngestBatch(ctx, []SignalInput{firing})
	if err != nil || len(created) != 1 || !created[0].StartTaskCreated {
		t.Fatalf("firing ingest=%+v err=%v", created, err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	processOneIncidentStart(t, ctx, db, incidentID, 1)
	repository, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := repository.ClaimReady(ctx, asyncjob.ClaimRequest{
		Queue: asyncjob.QueueInvestigate, Owner: "stale-investigation-step", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Task.SubjectType != "agent_run" || stale.Task.Transition != "investigation.step" {
		t.Fatalf("claimed task=%+v", stale.Task)
	}
	historicalResult, err := db.ExecContext(ctx, `INSERT INTO agent_runs
	 (public_id, incident_id, cycle_no, expected_incident_version, status, model,
	  prompt_version, max_steps, failure_code, completed_at)
	VALUES (?, ?, 1, 1, 'completed', 'historical-model', 'historical-fixture', 1, '', NOW(6))`, uuid.NewString(), incidentID)
	if err != nil {
		t.Fatal(err)
	}
	historicalPointerRunID, err := historicalResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE incidents SET current_agent_run_id = ? WHERE id = ?`, historicalPointerRunID, incidentID); err != nil {
		t.Fatal(err)
	}

	resolved := resolvedIncidentIntegrationSignal(firing, 202)
	results, err := store.IngestBatch(ctx, []SignalInput{resolved})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Rejected || results[0].IncidentPublicID != created[0].IncidentPublicID {
		t.Fatalf("resolved ingest=%+v", results)
	}
	if err := repository.Heartbeat(ctx, stale.Lease, time.Minute); !errors.Is(err, asyncjob.ErrLeaseLost) {
		t.Fatalf("stale Agent heartbeat error=%v", err)
	}

	var incidentStatus string
	var incidentVersion uint64
	var currentAgent sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT status, version, current_agent_run_id
	FROM incidents WHERE id = ?`, incidentID).Scan(&incidentStatus, &incidentVersion, &currentAgent); err != nil {
		t.Fatal(err)
	}
	if incidentStatus != "verifying" || incidentVersion != 3 ||
		!currentAgent.Valid || currentAgent.Int64 != historicalPointerRunID {
		t.Fatalf("incident status=%s version=%d current_agent=%+v", incidentStatus, incidentVersion, currentAgent)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
	WHERE incident_id = ? AND cycle_no = 1 AND status = 'cancelled'`, 1, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE id = ? AND status = 'cancelled' AND lease_generation > ?`, 1, stale.Task.ID, stale.Lease.Generation)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_task_attempts
WHERE task_id = ? AND status = 'cancelled' AND error_code = 'resolved_signal'`, 1, stale.Task.ID)

	var runID, triggerSignalID uint64
	var runPublicID, triggerType, profileID, profileHash string
	var sourceRevision, imageDigest, gitopsRevision string
	var planJSON []byte
	var remediationPlanID, changeRequestID sql.NullInt64
	var originatingAgentRunID, budgetAuthorizationID sql.NullInt64
	var attempt int
	if err := db.QueryRowContext(ctx, `SELECT id, public_id, trigger_signal_id, trigger_type,
 verification_profile_id, verification_profile_hash, source_revision, image_digest,
 gitops_revision, plan_json, remediation_plan_id, change_request_id, attempt,
 originating_agent_run_id, business_budget_authorization_id
FROM verification_runs WHERE incident_id = ? AND cycle_no = 1`, incidentID).Scan(
		&runID, &runPublicID, &triggerSignalID, &triggerType, &profileID, &profileHash,
		&sourceRevision, &imageDigest, &gitopsRevision, &planJSON, &remediationPlanID, &changeRequestID, &attempt,
		&originatingAgentRunID, &budgetAuthorizationID); err != nil {
		t.Fatal(err)
	}
	var plan verification.Plan
	if json.Unmarshal(planJSON, &plan) != nil || verification.ValidatePlan(plan) != nil ||
		triggerType != "no_change_signal" || profileID != verification.NoChangeProfileID || profileHash != plan.ProfileHash ||
		sourceRevision != identity.sourceRevision || imageDigest != identity.imageDigest || gitopsRevision != identity.gitopsRevision ||
		plan.TriggerType != "no_change" || plan.TargetRevision != identity.gitopsRevision || len(plan.Checks) != 8 ||
		remediationPlanID.Valid || changeRequestID.Valid || attempt != 1 ||
		!originatingAgentRunID.Valid || originatingAgentRunID.Int64 != int64(stale.Task.SubjectID) || budgetAuthorizationID.Valid {
		t.Fatalf("verification run trigger=%s profile=%s plan=%+v", triggerType, profileID, plan)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
WHERE id = ? AND incident_id = ? AND cycle_no = 1 AND status = 'cancelled'`, 1, originatingAgentRunID.Int64, incidentID)
	var resolvedSignalID uint64
	if err := db.QueryRowContext(ctx, `SELECT id FROM incident_signals
WHERE source = ? AND source_event_id = ?`, resolved.Source, resolved.SourceEventID).Scan(&resolvedSignalID); err != nil {
		t.Fatal(err)
	}
	if triggerSignalID != resolvedSignalID {
		t.Fatalf("trigger signal=%d want=%d", triggerSignalID, resolvedSignalID)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM verification_checks
WHERE verification_run_id = ? AND incident_id = ? AND cycle_no = 1 AND status = 'pending'`, 8, runID, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND cycle_no = 1 AND task_type = 'verification.advance'
  AND subject_id = ? AND expected_subject_version = 1 AND status = 'ready'`, 1, incidentID, runID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM incident_events
WHERE incident_id = ? AND cycle_no = 1 AND event_type = 'verification_started'
  AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.verification_run_id')) = ?`, 1, incidentID, runPublicID)

	duplicate, err := store.IngestBatch(ctx, []SignalInput{resolved})
	if err != nil || len(duplicate) != 1 || !duplicate[0].Duplicate {
		t.Fatalf("duplicate resolved=%+v err=%v", duplicate, err)
	}
	secondFiring := incidentIntegrationSignal(203, correlationKey)
	if _, err := store.IngestBatch(ctx, []SignalInput{secondFiring}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IngestBatch(ctx, []SignalInput{resolvedIncidentIntegrationSignal(secondFiring, 204)}); err != nil {
		t.Fatal(err)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM verification_runs
WHERE incident_id = ? AND cycle_no = 1`, 1, incidentID)
}

func TestMySQLIncidentNoChangeRequiresFinalEmptyFiringSet(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	insertNoChangeBaselineFixture(t, ctx, db)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := strings.Repeat("5", 64)
	first := incidentIntegrationSignal(211, correlationKey)
	second := incidentIntegrationSignal(212, correlationKey)
	second.Category = "ErrorRateHigh"
	created, err := store.IngestBatch(ctx, []SignalInput{first, second})
	if err != nil || len(created) != 2 {
		t.Fatalf("firing batch=%+v err=%v", created, err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	resolved := resolvedIncidentIntegrationSignal(first, 213)
	if _, err := store.IngestBatch(ctx, []SignalInput{resolved}); err != nil {
		t.Fatal(err)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM verification_runs
WHERE incident_id = ? AND cycle_no = 1`, 0, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM async_tasks
WHERE incident_id = ? AND cycle_no = 1 AND transition = 'investigation.start' AND status = 'ready'`, 1, incidentID)
}

func TestMySQLIncidentMissingBaselineCancelsAgentAndBlocksNoChange(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	correlationKey := strings.Repeat("6", 64)
	firing := incidentIntegrationSignal(221, correlationKey)
	created, err := store.IngestBatch(ctx, []SignalInput{firing})
	if err != nil {
		t.Fatal(err)
	}
	incidentID := incidentIntegrationIncidentID(t, ctx, db, created[0].IncidentPublicID)
	processOneIncidentStart(t, ctx, db, incidentID, 1)
	resolved := resolvedIncidentIntegrationSignal(firing, 222)
	if _, err := store.IngestBatch(ctx, []SignalInput{resolved}); err != nil {
		t.Fatal(err)
	}
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM verification_runs
WHERE incident_id = ? AND cycle_no = 1`, 0, incidentID)
	assertIncidentIntegrationCount(t, ctx, db, `SELECT COUNT(*) FROM agent_runs
WHERE incident_id = ? AND cycle_no = 1 AND status = 'cancelled'`, 1, incidentID)
	var status, reason string
	var attention bool
	if err := db.QueryRowContext(ctx, `SELECT status, needs_attention, blocking_reason_code
FROM incidents WHERE id = ?`, incidentID).Scan(&status, &attention, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "investigating" || !attention || reason != "no_change_identity_unavailable" {
		t.Fatalf("blocked Incident status=%s attention=%v reason=%s", status, attention, reason)
	}
}

type noChangeBaselineIdentity struct {
	sourceRevision, imageDigest, gitopsRevision string
}

func insertNoChangeBaselineFixture(t *testing.T, ctx context.Context, db *sql.DB) noChangeBaselineIdentity {
	t.Helper()
	identity := noChangeBaselineIdentity{
		sourceRevision: strings.Repeat("a", 40),
		imageDigest:    "sha256:" + strings.Repeat("b", 64),
		gitopsRevision: strings.Repeat("c", 40),
	}
	publicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO deployment_baselines
	 (public_id, baseline_schema_version, target_identity_hash,
	  cluster, environment, namespace, workload_kind, workload_name, container_name,
	  repository, base_branch, target_path, source_revision, image_digest, gitops_revision,
	  config_hash, verification_policy_version, verification_hash, status, row_version,
	  verified_at, created_at, updated_at)
	VALUES (?, 1, ?, 'kind', 'demo', 'demo', 'Deployment', 'checkout', 'checkout',
        'acme/gitops', 'main', 'apps/checkout.yaml', ?, ?, ?, ?, 'baseline-policy/v1', ?,
        'active', 1, NOW(6), NOW(6), NOW(6))`, publicID, strings.Repeat("d", 64),
		identity.sourceRevision, identity.imageDigest, identity.gitopsRevision,
		strings.Repeat("e", 64), strings.Repeat("f", 64))
	if err != nil {
		t.Fatal(err)
	}
	baselineID, _ := result.LastInsertId()
	observed, _ := json.Marshal(map[string]any{
		"application": "checkout", "project": "cloudops-demo",
		"deployed_revision": identity.gitopsRevision, "sync_status": "Synced", "operation_phase": "Succeeded",
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO baseline_observations
	 (public_id, observation_schema_version, baseline_id, sequence_no,
	  observation_type, source_identity, observed_json, content_hash, dedupe_key, observed_at, created_at)
	VALUES (?, 1, ?, 1, 'argocd_revision', 'argocd/application', ?, ?, ?, NOW(6), NOW(6))`,
		uuid.NewString(), baselineID, observed, strings.Repeat("1", 64), strings.Repeat("2", 64)); err != nil {
		t.Fatal(err)
	}
	return identity
}

func resolvedIncidentIntegrationSignal(firing SignalInput, sequence int) SignalInput {
	resolved := firing
	resolved.SourceEventID = fmt.Sprintf("%064x", sequence)
	resolved.Status = domain.SignalStatusResolved
	endsAt := firing.StartsAt.Add(time.Minute)
	resolved.EndsAt = &endsAt
	resolved.OccurredAt = endsAt
	return resolved
}
