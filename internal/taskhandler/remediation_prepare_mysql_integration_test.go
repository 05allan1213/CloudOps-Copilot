package taskhandler

import (
	"context"
	"crypto/sha1" // #nosec G505 -- fixture computes Git SHA-1 object identities.
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
	"github.com/google/uuid"
)

func TestMySQLRemediationPrepareLoaderBindsTaskDiagnosisEvidenceBaselineAndExactGit(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openVerificationIntegrationDB(t, adminDSN)
	defer closeVerificationIntegrationDB(t, "remediation loader admin", admin)
	databaseName := fmt.Sprintf("cloudops_remediation_loader_%d", time.Now().UnixNano())
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
	defer closeVerificationIntegrationDB(t, "remediation loader", db)
	migrations, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	fixture := insertRemediationLoaderFixture(t, ctx, db, now)
	git := newRemediationLoaderGitFixture(t, fixture.baselineContent, fixture.currentContent, fixture.baselineRevision)
	policy := remediation.RestoreEnvPolicy{
		Version: "restore-required-env-policy/v1", Repository: "acme/gitops", BaseBranch: "main",
		AllowedPath: "apps/demo.yaml", APIVersion: "apps/v1", Namespace: "demo",
		Workload: "demo", Container: "demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
		VerificationVersion: verification.GoldenRequiredEnvProfileID,
	}
	loader, err := NewMySQLRemediationPrepareLoader(db, git, MySQLRemediationPrepareLoaderConfig{
		Policy: policy, PlanTTL: 30 * time.Minute, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	capture := &remediationLoaderCaptureStore{captured: make(chan remediationLoaderCapture, 1)}
	operation, err := NewRemediationPrepare(RemediationPrepareConfig{Loader: loader, Store: capture})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(remediationPreparePayload{AgentRunID: fixture.agentRunPublicID, CycleNo: 1})
	if _, err := tasks.Enqueue(ctx, asyncjob.NewTask{
		IncidentID: fixture.incidentID, CycleNo: 1, Type: asyncjob.TaskRemediationPrepare,
		SubjectType: "agent_run", SubjectID: fixture.agentRunID, Transition: "remediation.prepare",
		ExpectedSubjectVersion: fixture.agentRunVersion, PayloadSchemaVersion: remediationPreparePayloadSchema,
		Payload: payload, DedupeKey: hashCanonical("remediation-loader", fixture.agentRunPublicID), Priority: 50, MaxAttempts: 3,
	}); err != nil {
		t.Fatal(err)
	}
	runner, err := asyncjob.NewRunner(asyncjob.RunnerConfig{
		Owner: "remediation-loader-integration", Store: tasks,
		Handlers: New(Config{RemediationPrepare: operation}), PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var captured remediationLoaderCapture
	select {
	case captured = <-capture.captured:
	case <-time.After(15 * time.Second):
		var status, code, summary string
		var attempt uint32
		if err := db.QueryRowContext(context.Background(), `SELECT status, COALESCE(last_error_code, ''), COALESCE(last_error_summary, ''), attempt
FROM async_tasks WHERE incident_id = ? AND task_type = 'remediation.prepare' ORDER BY id DESC LIMIT 1`, fixture.incidentID).
			Scan(&status, &code, &summary, &attempt); err != nil {
			t.Fatalf("timed out waiting for remediation.prepare mutation; inspect task: %v", err)
		}
		t.Fatalf("timed out waiting for remediation.prepare mutation: status=%s attempt=%d code=%s summary=%s", status, attempt, code, summary)
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}

	request, plan := captured.input.Request, captured.plan
	if git.calls.Load() != 1 || request.IncidentID != fixture.incidentID || request.IncidentVersion != 2 ||
		request.CreatedByAgentRunID != fixture.agentRunPublicID || request.DiagnosisHash != fixture.diagnosis.DiagnosisHash ||
		request.LastKnownGoodRevision != fixture.baselineRevision || request.BaseRevision != git.facts.BaseRevision ||
		request.BaseBlobSHA != git.facts.BaseBlobSHA || len(request.Evidence) != len(fixture.evidenceIDs) ||
		captured.input.Baseline.ID != fixture.baselineID || captured.input.Baseline.ObservationID != fixture.observationID {
		t.Fatalf("input=%+v git_calls=%d", captured.input, git.calls.Load())
	}
	if plan == nil || plan.PublicID != captured.input.PlanPublicID || plan.CanonicalPlanHash == "" ||
		plan.ExpectedTreeHash == "" || plan.ExpectedPostImageHash == "" || plan.LastKnownGoodRevision != fixture.baselineRevision {
		t.Fatalf("compiled Plan=%+v", plan)
	}
}

type remediationLoaderFixture struct {
	incidentID       uint64
	agentRunID       uint64
	agentRunVersion  uint64
	agentRunPublicID string
	diagnosis        agent.DiagnosisRecord
	evidenceIDs      []string
	baselineID       uint64
	observationID    uint64
	baselineRevision string
	currentContent   []byte
	baselineContent  []byte
}

func insertRemediationLoaderFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) remediationLoaderFixture {
	t.Helper()
	fixture := remediationLoaderFixture{
		agentRunVersion: 7, agentRunPublicID: uuid.NewString(), baselineRevision: strings.Repeat("9", 40),
		currentContent: []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\n  namespace: demo\nspec:\n  template:\n    spec:\n      containers:\n        - name: demo\n          image: example/demo@sha256:" + strings.Repeat("a", 64) + "\n"),
	}
	fixture.baselineContent = append(append([]byte(nil), fixture.currentContent...), []byte("          env:\n            - name: REQUIRED_ENV\n              value: healthy\n")...)
	incidentPublicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents
 (public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
  service_name, environment, target_kind, target_name, severity, status, summary,
  first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no)
VALUES (?, ?, ?, 2, 'kind-v3', 'demo', 'demo', 'development', 'Deployment',
        'demo', 'warning', 'DIAGNOSING', 'remediation loader fixture', ?, ?, 2, 3, 'investigating', 1)`,
		incidentPublicID, "remediation-loader-"+uuid.NewString(), "v2:"+strings.Repeat("a", 64), now.Add(-2*time.Minute), now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, _ := result.LastInsertId()
	fixture.incidentID = uint64(incidentID)

	fixture.evidenceIDs = []string{uuid.NewString(), uuid.NewString()}
	facts := remediationLoaderFacts(incidentPublicID, fixture.evidenceIDs)
	policy := agent.GoldenRequiredEnvClaimPolicy()
	sufficiency, err := agent.EvaluateSufficiency(agent.SufficiencyInput{IncidentID: incidentPublicID, CycleNo: 1, Facts: facts, Policy: policy})
	if err != nil || sufficiency.Outcome != agent.SufficiencyReady {
		t.Fatalf("fixture sufficiency=%+v err=%v", sufficiency, err)
	}
	factIDs := make([]string, 0, len(facts))
	for _, fact := range facts {
		factIDs = append(factIDs, fact.ID)
	}
	diagnosis, err := validateV3Diagnosis(agent.DiagnosisCandidate{
		ClaimType: policy.ClaimType, Summary: "The required environment node was removed from the deployed GitOps revision.",
		Confidence: agent.DiagnosisConfirmed, EvidenceFactIDs: factIDs, RemediationHint: agent.RemediationRestoreRequiredEnv,
	}, investigationSnapshot{IncidentPublicID: incidentPublicID, Task: asyncjob.Task{CycleNo: 1}, Facts: facts}, policy, sufficiency)
	if err != nil {
		t.Fatal(err)
	}
	fixture.diagnosis = diagnosis
	diagnosisJSON, _ := json.Marshal(diagnosis)
	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, final_diagnosis,
  failure_code, completed_at, row_version, domain_schema_version, v3_status, cycle_no, expected_incident_version)
VALUES (?, ?, 'COMPLETED', 'fixture-model', 'incident-agent-v3', 10, ?, '', ?, ?, 3, 'completed', 1, 2)`,
		fixture.agentRunPublicID, fixture.incidentID, diagnosisJSON, now.Add(-time.Minute), fixture.agentRunVersion)
	if err != nil {
		t.Fatal(err)
	}
	agentRunID, _ := result.LastInsertId()
	fixture.agentRunID = uint64(agentRunID)

	for index, evidenceID := range fixture.evidenceIDs {
		contentHash := strings.Repeat(fmt.Sprint(index+1), 64)
		var selected []agent.EvidenceFact
		for _, fact := range facts {
			if fact.EvidenceID == evidenceID {
				selected = append(selected, fact)
			}
		}
		envelope, _ := json.Marshal(storedEvidenceEnvelope{
			SchemaVersion: 1, Status: agent.CollectionAvailable, SourceSystem: selected[0].SourceSystem,
			CollectionPath: selected[0].CollectionPath, TemplateVersion: "fixture/v1", Summary: "verified fixture facts",
			Facts: selected, ContentHash: contentHash,
		})
		if _, err := db.ExecContext(ctx, `INSERT INTO evidence_items
 (public_id, incident_id, domain_schema_version, cycle_no, agent_run_id, type, source,
  producer_type, producer_dedupe_key, tool_name, resource_ref, query_text, summary,
  facts_json, result_hash, content_hash, raw_ref, redaction_json, truncated, valid,
  idempotency_key, collected_at, created_at)
VALUES (?, ?, 3, 1, ?, 'agent_observation', ?, 'agent_step', ?, 'fixture.read',
        'github://acme/gitops/apps/demo.yaml', '', 'verified fixture facts', ?, ?, ?, '',
        JSON_OBJECT('policy','fixture'), FALSE, TRUE, ?, ?, ?)`,
			evidenceID, fixture.incidentID, fixture.agentRunID, selected[0].SourceSystem,
			hashCanonical("producer", evidenceID), envelope, contentHash, contentHash,
			hashCanonical("evidence", evidenceID), now.Add(-90*time.Second), now.Add(-90*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	configHash := remediation.HashBytes(fixture.baselineContent)
	result, err = db.ExecContext(ctx, `INSERT INTO deployment_baselines
 (public_id, domain_schema_version, baseline_schema_version, target_identity_hash,
  cluster, environment, namespace, workload_kind, workload_name, container_name,
  repository, base_branch, target_path, source_revision, image_digest, gitops_revision,
  config_hash, verification_policy_version, verification_hash, status, row_version,
  verified_at, created_at, updated_at)
VALUES (?, 3, 1, ?, 'kind-v3', 'development', 'demo', 'Deployment', 'demo', 'demo',
        'acme/gitops', 'main', 'apps/demo.yaml', ?, ?, ?, ?, 'baseline-health/v1', ?,
        'active', 1, ?, ?, ?)`, uuid.NewString(), strings.Repeat("b", 64), strings.Repeat("c", 40),
		"sha256:"+strings.Repeat("d", 64), fixture.baselineRevision, configHash, strings.Repeat("e", 64),
		now.Add(-3*time.Minute), now.Add(-3*time.Minute), now.Add(-3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	baselineID, _ := result.LastInsertId()
	fixture.baselineID = uint64(baselineID)
	result, err = db.ExecContext(ctx, `INSERT INTO baseline_observations
 (public_id, domain_schema_version, observation_schema_version, baseline_id, sequence_no,
  observation_type, source_identity, observed_json, content_hash, dedupe_key, observed_at, created_at)
VALUES (?, 3, 1, ?, 1, 'config_blob', ?, JSON_OBJECT('repository','acme/gitops','path','apps/demo.yaml','revision',?),
        ?, ?, ?, ?)`, uuid.NewString(), fixture.baselineID, "github:acme/gitops@"+fixture.baselineRevision,
		fixture.baselineRevision, configHash, hashCanonical("baseline-observation", configHash), now.Add(-3*time.Minute), now.Add(-3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	observationID, _ := result.LastInsertId()
	fixture.observationID = uint64(observationID)
	return fixture
}

func remediationLoaderFacts(incidentPublicID string, evidenceIDs []string) []agent.EvidenceFact {
	types := []string{
		"workload.subject_confirmed", "gitops.required_env_removed", "argocd.bad_revision_deployed",
		"kubernetes.required_env_absent", "source_revision.unchanged", "image_digest.unchanged",
		"metric.readiness_or_5xx_failure", "log.required_env_missing", "trace.request_failure",
	}
	facts := make([]agent.EvidenceFact, 0, len(types))
	for index, factType := range types {
		evidenceIndex := 0
		source, collection := "github", "github/get_deployment_context"
		if index >= 4 {
			evidenceIndex, source, collection = 1, "kubernetes", "kubernetes/get_deployment_context"
		}
		facts = append(facts, agent.EvidenceFact{
			ID: fmt.Sprintf("fact-%02d", index+1), EvidenceID: evidenceIDs[evidenceIndex],
			IncidentID: incidentPublicID, CycleNo: 1, Type: factType, SourceSystem: source,
			CollectionPath: collection, CorroborationGroup: fmt.Sprintf("group-%02d", index+1),
			Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
			ClaimUse: "allowed", CollectionStatus: agent.CollectionAvailable, Direct: index == 0,
		})
	}
	return facts
}

type remediationLoaderGitFixture struct {
	facts remediation.ExactGitRestoreFacts
	calls atomic.Int32
}

func newRemediationLoaderGitFixture(t *testing.T, baseline, current []byte, baselineRevision string) *remediationLoaderGitFixture {
	t.Helper()
	currentBlob := remediationLoaderGitHash("blob", current)
	baselineBlob := remediationLoaderGitHash("blob", baseline)
	appsTree := remediationLoaderGitHash("tree", remediationLoaderRawTree("100644", "demo.yaml", currentBlob))
	rootTree := remediationLoaderGitHash("tree", remediationLoaderRawTree("40000", "apps", appsTree))
	return &remediationLoaderGitFixture{facts: remediation.ExactGitRestoreFacts{
		Repository: "acme/gitops", BaseBranch: "main", TargetPath: "apps/demo.yaml",
		BaseRevision: strings.Repeat("a", 40), BaseTreeSHA: rootTree, BaseBlobSHA: currentBlob,
		FileMode: "100644", CurrentContent: append([]byte(nil), current...), BaselineRevision: baselineRevision,
		BaselineBlobSHA: baselineBlob, BaselineContent: append([]byte(nil), baseline...), BaselineIsAncestor: true,
		TreeEntries: []remediation.GitTreeEntry{
			{Path: "apps", Mode: "040000", Type: "tree", ObjectID: appsTree},
			{Path: "apps/demo.yaml", Mode: "100644", Type: "blob", ObjectID: currentBlob},
		},
	}}
}

func (f *remediationLoaderGitFixture) ReadRestoreFacts(_ context.Context, query remediation.ExactGitRestoreQuery) (remediation.ExactGitRestoreFacts, error) {
	f.calls.Add(1)
	if query.Repository != f.facts.Repository || query.BaseBranch != f.facts.BaseBranch || query.TargetPath != f.facts.TargetPath || query.BaselineRevision != f.facts.BaselineRevision {
		return remediation.ExactGitRestoreFacts{}, remediation.ErrDrift
	}
	return f.facts, nil
}

type remediationLoaderCapture struct {
	input RemediationPrepareInput
	plan  *remediation.RemediationPlan
}

type remediationLoaderCaptureStore struct {
	captured chan remediationLoaderCapture
}

func (s *remediationLoaderCaptureStore) PersistIn(_ context.Context, _ asyncjob.DBTX, _ asyncjob.Task, input RemediationPrepareInput, plan *remediation.RemediationPlan) error {
	copyPlan := *plan
	s.captured <- remediationLoaderCapture{input: input, plan: &copyPlan}
	return nil
}

func remediationLoaderGitHash(kind string, content []byte) string {
	payload := append([]byte(fmt.Sprintf("%s %d\x00", kind, len(content))), content...)
	sum := sha1.Sum(payload) // #nosec G401 -- fixture computes Git SHA-1 object identities.
	return hex.EncodeToString(sum[:])
}

func remediationLoaderRawTree(mode, name, objectID string) []byte {
	raw, _ := hex.DecodeString(objectID)
	return append([]byte(mode+" "+name+"\x00"), raw...)
}
