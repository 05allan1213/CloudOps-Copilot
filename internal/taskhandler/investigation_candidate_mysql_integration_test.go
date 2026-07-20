package taskhandler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/google/uuid"
)

func TestMySQLInvestigationPersistsAndAssessesImmutableChangeCandidate(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin := openVerificationIntegrationDB(t, adminDSN)
	defer admin.Close()
	databaseName := fmt.Sprintf("cloudops_investigation_candidate_%d", time.Now().UnixNano())
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
	defer db.Close()
	migrations, err := migration.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	incidentPublicID := uuid.NewString()
	result, err := db.ExecContext(ctx, `INSERT INTO incidents
 (public_id, fingerprint, correlation_key, correlation_key_version, cluster, namespace,
  service_name, environment, target_kind, target_name, severity, status, summary,
  first_seen_at, last_seen_at, version, domain_schema_version, v3_status, cycle_no)
VALUES (?, ?, ?, 2, 'kind-v3', 'cloudops-demo', 'demo', 'development', 'Deployment',
        'demo', 'warning', 'DIAGNOSING', 'candidate integration fixture', ?, ?, 2, 3, 'investigating', 1)`,
		incidentPublicID, "candidate-"+uuid.NewString(), "v2:"+strings.Repeat("a", 64), now.Add(-2*time.Minute), now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	incidentID, _ := result.LastInsertId()
	agentRunPublicID := uuid.NewString()
	result, err = db.ExecContext(ctx, `INSERT INTO agent_runs
 (public_id, incident_id, status, model, prompt_version, max_steps, final_diagnosis,
  failure_code, row_version, domain_schema_version, v3_status, cycle_no, expected_incident_version)
VALUES (?, ?, 'RUNNING', 'fixture-model', 'incident-agent-v3', 10, NULL, '', 1, 3, 'running', 1, 2)`,
		agentRunPublicID, incidentID)
	if err != nil {
		t.Fatal(err)
	}
	agentRunID, _ := result.LastInsertId()
	policyHash := strings.Repeat("c", 64)
	snapshot := investigationSnapshot{
		Task:        asyncjob.Task{IncidentID: uint64(incidentID), CycleNo: 1, SubjectID: uint64(agentRunID)},
		RunPublicID: agentRunPublicID, IncidentPublicID: incidentPublicID,
		Limits: agent.Limits{MaxEvidenceBytes: 64 * 1024},
		State:  agent.InvestigationState{Coverage: agent.CoverageRequirements{ClaimPolicyHash: policyHash}},
	}
	deploymentEvidenceID := uuid.NewString()
	detailEvidenceID := uuid.NewString()
	changeRef := uuid.NewString()
	revision := strings.Repeat("d", 40)
	imageDigest := "sha256:" + strings.Repeat("e", 64)
	deploymentFacts := []agent.EvidenceFact{
		investigationCandidateTestFact("deployment.change_ref", deploymentEvidenceID, map[string]string{
			"change_ref": changeRef, "repository": "acme/gitops", "revision": revision,
			"image_digest": imageDigest, "path": "apps/demo.yaml",
			"deployed_at": now.Add(-time.Minute).Format(time.RFC3339), "is_current": "true",
		}, snapshot),
		investigationCandidateTestFact("argocd.bad_revision_deployed", deploymentEvidenceID, map[string]string{"deployed_revision": revision}, snapshot),
		investigationCandidateTestFact("source_revision.unchanged", deploymentEvidenceID, map[string]string{"source_revision": strings.Repeat("f", 40)}, snapshot),
		investigationCandidateTestFact("image_digest.unchanged", deploymentEvidenceID, map[string]string{"image_digest": imageDigest}, snapshot),
	}
	deploymentCheckpoint := investigationStepCheckpoint{
		CapturedAt: now.Add(-50 * time.Second), Action: &agent.ProposedAction{Tool: "get_deployment_context"},
		Observation: &agent.ToolObservation{
			Status: agent.CollectionAvailable, SourceSystem: "argocd", CollectionPath: "argocd/deployment-context",
			TemplateVersion: "deployment-context/v1", Summary: "exact deployment context",
			Facts: deploymentFacts, ContentHash: hashBytesInvestigation([]byte("candidate-deployment-observation")),
		},
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertInvestigationEvidence(ctx, tx, snapshot, deploymentCheckpoint, deploymentEvidenceID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := insertInvestigationChangeCandidates(ctx, tx, snapshot, deploymentCheckpoint, deploymentEvidenceID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	detailFacts := []agent.EvidenceFact{
		investigationCandidateTestFact("gitops.required_env_removed", detailEvidenceID, map[string]string{
			"change_ref": changeRef, "repository": "acme/gitops", "revision": revision, "path": "apps/demo.yaml",
		}, snapshot),
		investigationCandidateTestFact("change.ci_succeeded", detailEvidenceID, map[string]string{
			"change_ref": changeRef, "repository": "acme/gitops", "revision": revision,
		}, snapshot),
	}
	detailCheckpoint := investigationStepCheckpoint{
		CapturedAt: now.Add(-20 * time.Second), Action: &agent.ProposedAction{Tool: "get_change_detail"},
		Observation: &agent.ToolObservation{
			Status: agent.CollectionAvailable, SourceSystem: "github", CollectionPath: "github/change-detail",
			TemplateVersion: "change-detail/v1", Summary: "exact change detail",
			Facts: detailFacts, ContentHash: hashBytesInvestigation([]byte("candidate-detail-observation")),
		},
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertInvestigationEvidence(ctx, tx, snapshot, detailCheckpoint, detailEvidenceID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	snapshot.Facts = append(append([]agent.EvidenceFact(nil), deploymentFacts...), detailFacts...)
	diagnosisHash := strings.Repeat("a", 64)
	terminal := investigationStepCheckpoint{
		TerminalOutcome: "diagnosed", CapturedAt: now,
		State: agent.InvestigationState{Coverage: agent.CoverageRequirements{ClaimPolicyHash: policyHash}},
		Diagnosis: &agent.DiagnosisRecord{
			Candidate: agent.DiagnosisCandidate{
				ClaimType: agent.GoldenRequiredEnvClaimPolicy().ClaimType, Confidence: agent.DiagnosisConfirmed,
				RemediationHint: agent.RemediationRestoreRequiredEnv,
			},
			ClaimPolicyHash: policyHash, DiagnosisHash: diagnosisHash,
			EvidenceIDs: []string{deploymentEvidenceID, detailEvidenceID},
		},
	}
	assess := func(checkpoint investigationStepCheckpoint) {
		t.Helper()
		tx, beginErr := db.BeginTx(ctx, nil)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if err := assessInvestigationChangeCandidates(ctx, tx, snapshot, checkpoint); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	assess(terminal)
	assess(terminal)

	var candidateCount, assessmentCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_candidates WHERE agent_run_id = ?`, agentRunID).Scan(&candidateCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_candidate_assessments`).Scan(&assessmentCount); err != nil {
		t.Fatal(err)
	}
	if candidateCount != 1 || assessmentCount != 1 {
		t.Fatalf("candidate/assessment counts=%d/%d, want 1/1", candidateCount, assessmentCount)
	}
	var status string
	var supportingJSON, contradictingJSON []byte
	if err := db.QueryRowContext(ctx, `SELECT status, supporting_evidence_json, contradicting_evidence_json
FROM change_candidate_assessments ORDER BY id DESC LIMIT 1`).Scan(&status, &supportingJSON, &contradictingJSON); err != nil {
		t.Fatal(err)
	}
	var supporting, contradicting []string
	if json.Unmarshal(supportingJSON, &supporting) != nil || json.Unmarshal(contradictingJSON, &contradicting) != nil ||
		status != "matched" || !slices.Equal(supporting, stableUniqueInvestigation([]string{deploymentEvidenceID, detailEvidenceID})) || len(contradicting) != 0 {
		t.Fatalf("assessment status=%s supporting=%v contradicting=%v", status, supporting, contradicting)
	}

	terminal.Diagnosis.DiagnosisHash = strings.Repeat("b", 64)
	assess(terminal)
	var supersedes sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_candidate_assessments`).Scan(&assessmentCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT supersedes_assessment_id FROM change_candidate_assessments ORDER BY id DESC LIMIT 1`).Scan(&supersedes); err != nil {
		t.Fatal(err)
	}
	if assessmentCount != 2 || !supersedes.Valid || supersedes.Int64 <= 0 {
		t.Fatalf("superseding assessment count=%d supersedes=%+v", assessmentCount, supersedes)
	}
}
