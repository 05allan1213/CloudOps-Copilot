package taskhandler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
	"github.com/google/uuid"
)

const defaultRemediationPlanTTL = 30 * time.Minute

type MySQLRemediationPrepareLoaderConfig struct {
	Policy  remediation.RestoreEnvPolicy
	PlanTTL time.Duration
	Now     func() time.Time
}

type mysqlRemediationPrepareLoader struct {
	db      *sql.DB
	git     remediation.ExactGitReader
	policy  remediation.RestoreEnvPolicy
	planTTL time.Duration
	now     func() time.Time
}

func NewMySQLRemediationPrepareLoader(db *sql.DB, git remediation.ExactGitReader, cfg MySQLRemediationPrepareLoaderConfig) (RemediationPrepareLoader, error) {
	if db == nil || git == nil {
		return nil, errors.New("remediation.prepare MySQL and exact Git readers are required")
	}
	if err := validateRemediationPreparePolicy(cfg.Policy); err != nil {
		return nil, err
	}
	if cfg.PlanTTL == 0 {
		cfg.PlanTTL = defaultRemediationPlanTTL
	}
	if cfg.PlanTTL <= 0 || cfg.PlanTTL > 24*time.Hour {
		return nil, fmt.Errorf("%w: remediation Plan TTL is outside bounds", remediation.ErrInvalidArgument)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &mysqlRemediationPrepareLoader{db: db, git: git, policy: cfg.Policy, planTTL: cfg.PlanTTL, now: cfg.Now}, nil
}

type remediationPrepareDurableFacts struct {
	IncidentPublicID      string
	IncidentVersion       uint64
	Cluster               string
	Environment           string
	Namespace             string
	ServiceName           string
	TargetKind            string
	TargetName            string
	AgentRunPublicID      string
	AgentRunVersion       uint64
	MigratedLegacy        bool
	MigratedLegacyContext bool
	RunCompletedAt        time.Time
	Diagnosis             agent.DiagnosisRecord
	Evidence              []remediation.EvidenceBinding
	Baseline              remediationPrepareBaseline
	PlanVersion           int
}

type remediationPrepareBaseline struct {
	RemediationPrepareBaselineFence
	TargetIdentityHash        string
	SourceRevision            string
	ImageDigest               string
	VerificationPolicyVersion string
	VerificationHash          string
	ObservationPublicID       string
	VerifiedAt                time.Time
}

func (l *mysqlRemediationPrepareLoader) Load(ctx context.Context, task asyncjob.Task) (RemediationPrepareInput, error) {
	if l == nil || l.db == nil || l.git == nil || task.SubjectID == 0 || task.IncidentID == 0 || task.CycleNo == 0 || task.ExpectedSubjectVersion == 0 || task.CreatedAt.IsZero() || !validSHA256Text(task.DedupeKey) {
		return RemediationPrepareInput{}, fmt.Errorf("%w: remediation.prepare task fence is incomplete", remediation.ErrInvalidArgument)
	}
	durable, err := l.loadDurableFacts(ctx, task)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	createdAt := task.CreatedAt.UTC().Truncate(time.Microsecond)
	expiresAt := createdAt.Add(l.planTTL)
	now := l.now().UTC()
	if createdAt.Before(durable.RunCompletedAt) || !now.Before(expiresAt) {
		return RemediationPrepareInput{}, fmt.Errorf("%w: remediation task is stale or predates its completed AgentRun", asyncjob.ErrPolicyViolation)
	}

	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	gitFacts, gitErr := l.git.ReadRestoreFacts(externalCtx, remediation.ExactGitRestoreQuery{
		Repository: l.policy.Repository, BaseBranch: l.policy.BaseBranch,
		TargetPath: l.policy.AllowedPath, BaselineRevision: durable.Baseline.GitOpsRevision,
	})
	cancel()
	if gitErr != nil {
		return RemediationPrepareInput{}, classifyRemediationGitReadError(gitErr)
	}
	if gitFacts.Repository != l.policy.Repository || gitFacts.BaseBranch != l.policy.BaseBranch || gitFacts.TargetPath != l.policy.AllowedPath ||
		gitFacts.BaselineRevision != durable.Baseline.GitOpsRevision || !gitFacts.BaselineIsAncestor {
		return RemediationPrepareInput{}, fmt.Errorf("%w: exact Git facts do not match the active DeploymentBaseline", remediation.ErrDrift)
	}
	if remediation.HashBytes(gitFacts.BaselineContent) != durable.Baseline.ConfigHash || durable.Baseline.ObservationHash != durable.Baseline.ConfigHash {
		return RemediationPrepareInput{}, fmt.Errorf("%w: baseline config blob does not match its verified hash", remediation.ErrDrift)
	}
	target := remediation.TargetResource{
		APIVersion: l.policy.APIVersion, Kind: "Deployment", Namespace: l.policy.Namespace,
		Name: l.policy.Workload, Container: l.policy.Container,
	}
	patch, err := remediation.RenderRestoreRequiredEnv(gitFacts.CurrentContent, gitFacts.BaselineContent, target, l.policy.EnvKey)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	expectedTree, err := remediation.ExpectedGitTreeHash(gitFacts, patch.Content)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	verificationPlan, err := buildRemediationVerificationSnapshot(durable.Baseline)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	request := remediation.RestoreEnvCompileRequest{
		IncidentPublicID: durable.IncidentPublicID, IncidentID: task.IncidentID,
		CycleNo: uint64(task.CycleNo), IncidentVersion: durable.IncidentVersion,
		CreatedByAgentRunID: durable.AgentRunPublicID, DiagnosisHash: durable.Diagnosis.DiagnosisHash,
		Repository: l.policy.Repository, BaseBranch: l.policy.BaseBranch, BaseRevision: gitFacts.BaseRevision,
		LastKnownGoodRevision: durable.Baseline.GitOpsRevision, TargetPath: l.policy.AllowedPath,
		BaseBlobSHA: gitFacts.BaseBlobSHA, ExpectedTreeHash: expectedTree, FileMode: gitFacts.FileMode,
		Target: target, EnvKey: l.policy.EnvKey, CurrentContent: append([]byte(nil), gitFacts.CurrentContent...),
		BaselineContent: append([]byte(nil), gitFacts.BaselineContent...), Policy: l.policy,
		VerificationPlan: verificationPlan, Evidence: append([]remediation.EvidenceBinding(nil), durable.Evidence...),
		BaselineIsAncestor: true, CreatedAt: createdAt, ExpiresAt: expiresAt, PlanVersion: durable.PlanVersion,
	}
	return RemediationPrepareInput{
		AgentRunID:     task.SubjectID,
		PlanPublicID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("remediation-plan\x00"+task.DedupeKey)).String(),
		Baseline:       durable.Baseline.RemediationPrepareBaselineFence,
		Request:        request,
		MigratedLegacy: durable.MigratedLegacy, MigratedLegacyContext: durable.MigratedLegacyContext,
	}, nil
}

func (l *mysqlRemediationPrepareLoader) loadDurableFacts(ctx context.Context, task asyncjob.Task) (_ remediationPrepareDurableFacts, err error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return remediationPrepareDurableFacts{}, fmt.Errorf("begin remediation.prepare read snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var result remediationPrepareDurableFacts
	var runLegacyStatus, runStatus, incidentStatus string
	var expectedIncidentVersion uint64
	var finalDiagnosis []byte
	var completedAt sql.NullTime
	const subjectQuery = `SELECT
 r.public_id, r.row_version, r.status, r.v3_status, r.expected_incident_version,
	r.migrated_legacy, r.migrated_legacy_context,
 r.final_diagnosis, r.completed_at,
 i.public_id, i.version, i.v3_status, i.cluster, i.environment, i.namespace,
 i.service_name, i.target_kind, i.target_name
FROM agent_runs r
JOIN incidents i ON i.id = r.incident_id
WHERE r.id = ? AND r.incident_id = ? AND r.domain_schema_version = 3 AND r.cycle_no = ?
  AND i.domain_schema_version = 3 AND i.cycle_no = ?`
	if err := tx.QueryRowContext(ctx, subjectQuery, task.SubjectID, task.IncidentID, task.CycleNo, task.CycleNo).Scan(
		&result.AgentRunPublicID, &result.AgentRunVersion, &runLegacyStatus, &runStatus, &expectedIncidentVersion,
		&result.MigratedLegacy, &result.MigratedLegacyContext,
		&finalDiagnosis, &completedAt, &result.IncidentPublicID, &result.IncidentVersion, &incidentStatus,
		&result.Cluster, &result.Environment, &result.Namespace, &result.ServiceName, &result.TargetKind, &result.TargetName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return remediationPrepareDurableFacts{}, asyncjob.ErrSubjectVersionMismatch
		}
		return remediationPrepareDurableFacts{}, fmt.Errorf("load remediation.prepare subject: %w", err)
	}
	if result.AgentRunVersion != task.ExpectedSubjectVersion || runLegacyStatus != "COMPLETED" || runStatus != "completed" || !completedAt.Valid ||
		result.MigratedLegacy != task.MigratedLegacy || result.MigratedLegacyContext != task.MigratedLegacyContext ||
		incidentStatus != "investigating" || expectedIncidentVersion != result.IncidentVersion ||
		result.Namespace != l.policy.Namespace || result.TargetKind != "Deployment" || result.TargetName != l.policy.Workload {
		return remediationPrepareDurableFacts{}, asyncjob.ErrSubjectVersionMismatch
	}
	result.RunCompletedAt = completedAt.Time.UTC().Truncate(time.Microsecond)
	diagnosis, err := decodeRemediationDiagnosis(finalDiagnosis)
	if err != nil {
		return remediationPrepareDurableFacts{}, err
	}
	evidence, facts, err := loadRemediationDiagnosisEvidence(ctx, tx, task, result.IncidentPublicID, diagnosis.EvidenceIDs)
	if err != nil {
		return remediationPrepareDurableFacts{}, err
	}
	if err := validateCurrentRemediationDiagnosis(diagnosis, result.IncidentPublicID, task.CycleNo, facts); err != nil {
		return remediationPrepareDurableFacts{}, err
	}
	result.Diagnosis, result.Evidence = diagnosis, evidence

	baseline, err := l.loadActiveBaseline(ctx, tx, result, task.CreatedAt.UTC())
	if err != nil {
		return remediationPrepareDurableFacts{}, err
	}
	result.Baseline = baseline
	var maxPlanVersion uint64
	var actionable int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(plan_version), 0),
 COALESCE(SUM(CASE WHEN cycle_no = ? AND v3_status IN ('awaiting_approval','approved') THEN 1 ELSE 0 END), 0)
FROM remediation_plans
WHERE incident_id = ?`, task.CycleNo, task.IncidentID).Scan(&maxPlanVersion, &actionable); err != nil {
		return remediationPrepareDurableFacts{}, fmt.Errorf("load remediation Plan version: %w", err)
	}
	if actionable != 0 || maxPlanVersion >= math.MaxInt {
		return remediationPrepareDurableFacts{}, fmt.Errorf("%w: Incident cycle already has an actionable remediation Plan", asyncjob.ErrPolicyViolation)
	}
	result.PlanVersion = int(maxPlanVersion + 1)
	if err := tx.Commit(); err != nil {
		return remediationPrepareDurableFacts{}, fmt.Errorf("commit remediation.prepare read snapshot: %w", err)
	}
	return result, nil
}

func decodeRemediationDiagnosis(payload []byte) (agent.DiagnosisRecord, error) {
	if len(payload) == 0 || len(payload) > 32*1024 {
		return agent.DiagnosisRecord{}, fmt.Errorf("%w: completed AgentRun has no bounded final Diagnosis", asyncjob.ErrPolicyViolation)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var diagnosis agent.DiagnosisRecord
	if err := decoder.Decode(&diagnosis); err != nil {
		return agent.DiagnosisRecord{}, fmt.Errorf("%w: completed AgentRun final Diagnosis is malformed", asyncjob.ErrPolicyViolation)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return agent.DiagnosisRecord{}, fmt.Errorf("%w: completed AgentRun final Diagnosis has trailing data", asyncjob.ErrPolicyViolation)
	}
	if diagnosis.Candidate.Confidence != agent.DiagnosisConfirmed || diagnosis.Candidate.RemediationHint != agent.RemediationRestoreRequiredEnv ||
		diagnosis.DiagnosisHash == "" || len(diagnosis.EvidenceIDs) == 0 || len(diagnosis.EvidenceIDs) > remediation.MaxV3Evidence {
		return agent.DiagnosisRecord{}, fmt.Errorf("%w: final Diagnosis does not authorize restore_required_env", asyncjob.ErrPolicyViolation)
	}
	return diagnosis, nil
}

func loadRemediationDiagnosisEvidence(ctx context.Context, tx *sql.Tx, task asyncjob.Task, incidentPublicID string, evidenceIDs []string) ([]remediation.EvidenceBinding, []agent.EvidenceFact, error) {
	bindings := make([]remediation.EvidenceBinding, 0, len(evidenceIDs))
	facts := make([]agent.EvidenceFact, 0, len(evidenceIDs)*2)
	for _, publicID := range evidenceIDs {
		var contentHash, resultHash, producerType string
		var factsJSON []byte
		var agentRunID sql.NullInt64
		var valid, truncated, migratedLegacy, migratedLegacyContext bool
		if err := tx.QueryRowContext(ctx, `SELECT content_hash, result_hash, producer_type, agent_run_id, facts_json, valid, truncated,
migrated_legacy, migrated_legacy_context
FROM evidence_items
WHERE public_id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?`,
			publicID, task.IncidentID, task.CycleNo).Scan(&contentHash, &resultHash, &producerType, &agentRunID,
			&factsJSON, &valid, &truncated, &migratedLegacy, &migratedLegacyContext); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil, fmt.Errorf("%w: Diagnosis Evidence is absent from the current cycle", asyncjob.ErrPolicyViolation)
			}
			return nil, nil, fmt.Errorf("load Diagnosis Evidence: %w", err)
		}
		if !agentRunID.Valid || uint64(agentRunID.Int64) != task.SubjectID ||
			(producerType != "agent_step" && producerType != "system_enrichment") || !valid || truncated ||
			migratedLegacy != task.MigratedLegacy || migratedLegacyContext != task.MigratedLegacyContext ||
			!validSHA256Text(contentHash) || resultHash != contentHash {
			return nil, nil, fmt.Errorf("%w: Diagnosis Evidence is not a current verified observation", asyncjob.ErrPolicyViolation)
		}
		var envelope storedEvidenceEnvelope
		if len(factsJSON) == 0 || len(factsJSON) > remediation.MaxV3PostImageBytes || json.Unmarshal(factsJSON, &envelope) != nil ||
			envelope.SchemaVersion != 1 || envelope.ContentHash != contentHash || envelope.Truncated || envelope.Status != agent.CollectionAvailable {
			return nil, nil, fmt.Errorf("%w: Diagnosis Evidence envelope is malformed", asyncjob.ErrPolicyViolation)
		}
		for _, fact := range envelope.Facts {
			if fact.EvidenceID != publicID || fact.IncidentID != incidentPublicID || fact.CycleNo != uint64(task.CycleNo) {
				return nil, nil, fmt.Errorf("%w: Diagnosis Evidence fact ownership is invalid", asyncjob.ErrPolicyViolation)
			}
			if fact.MigratedLegacy != migratedLegacy {
				return nil, nil, fmt.Errorf("%w: Diagnosis Evidence migrated-legacy provenance is invalid", asyncjob.ErrPolicyViolation)
			}
			facts = append(facts, fact)
		}
		bindings = append(bindings, remediation.EvidenceBinding{ID: publicID, ContentHash: contentHash})
	}
	if err := validateApprovedEvidenceCurrent(ctx, tx, task.IncidentID, uint64(task.CycleNo), bindings); err != nil {
		return nil, nil, err
	}
	return bindings, facts, nil
}

func validateCurrentRemediationDiagnosis(stored agent.DiagnosisRecord, incidentPublicID string, cycleNo uint32, facts []agent.EvidenceFact) error {
	policy := agent.GoldenRequiredEnvClaimPolicy()
	sufficiency, err := agent.EvaluateSufficiency(agent.SufficiencyInput{
		IncidentID: incidentPublicID, CycleNo: uint64(cycleNo), Facts: facts, Policy: policy,
	})
	if err != nil || sufficiency.Outcome != agent.SufficiencyReady {
		return fmt.Errorf("%w: current Evidence no longer satisfies the Diagnosis policy", asyncjob.ErrPolicyViolation)
	}
	validated, err := validateV3Diagnosis(stored.Candidate, investigationSnapshot{
		IncidentPublicID: incidentPublicID, Task: asyncjob.Task{CycleNo: cycleNo}, Facts: facts,
	}, policy, sufficiency)
	if err != nil {
		return fmt.Errorf("%w: final Diagnosis no longer validates: %v", asyncjob.ErrPolicyViolation, err)
	}
	storedJSON, storedErr := json.Marshal(stored)
	validatedJSON, validatedErr := json.Marshal(validated)
	if storedErr != nil || validatedErr != nil || !bytes.Equal(storedJSON, validatedJSON) {
		return fmt.Errorf("%w: final Diagnosis content/hash diverges from current Evidence", asyncjob.ErrPolicyViolation)
	}
	return nil
}

func (l *mysqlRemediationPrepareLoader) loadActiveBaseline(ctx context.Context, tx *sql.Tx, incident remediationPrepareDurableFacts, taskCreatedAt time.Time) (result remediationPrepareBaseline, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, public_id, baseline_schema_version, target_identity_hash,
 source_revision, image_digest, gitops_revision, config_hash,
 verification_policy_version, verification_hash, row_version, verified_at
FROM deployment_baselines
WHERE domain_schema_version = 3 AND status = 'active'
  AND cluster = ? AND environment = ? AND namespace = ? AND workload_kind = 'Deployment'
  AND workload_name = ? AND container_name = ? AND repository = ? AND base_branch = ? AND target_path = ?
ORDER BY id
LIMIT 2`, incident.Cluster, incident.Environment, incident.Namespace, incident.TargetName,
		l.policy.Container, l.policy.Repository, l.policy.BaseBranch, l.policy.AllowedPath)
	if err != nil {
		return remediationPrepareBaseline{}, fmt.Errorf("load active DeploymentBaseline: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close active deployment baseline rows: %w", closeErr))
		}
	}()
	var matches []remediationPrepareBaseline
	for rows.Next() {
		var baseline remediationPrepareBaseline
		var schemaVersion uint64
		if err := rows.Scan(&baseline.ID, &baseline.PublicID, &schemaVersion, &baseline.TargetIdentityHash,
			&baseline.SourceRevision, &baseline.ImageDigest, &baseline.GitOpsRevision, &baseline.ConfigHash,
			&baseline.VerificationPolicyVersion, &baseline.VerificationHash, &baseline.RowVersion, &baseline.VerifiedAt); err != nil {
			return remediationPrepareBaseline{}, fmt.Errorf("scan active DeploymentBaseline: %w", err)
		}
		if schemaVersion == 0 || baseline.RowVersion == 0 || !validSHA256Text(baseline.TargetIdentityHash) ||
			!change.ValidCommitSHA(strings.ToLower(baseline.SourceRevision)) || !change.ValidCommitSHA(strings.ToLower(baseline.GitOpsRevision)) ||
			!validImageDigest(baseline.ImageDigest) || !validSHA256Text(baseline.ConfigHash) || !validSHA256Text(baseline.VerificationHash) ||
			strings.TrimSpace(baseline.VerificationPolicyVersion) == "" || baseline.VerifiedAt.IsZero() || baseline.VerifiedAt.After(taskCreatedAt) {
			return remediationPrepareBaseline{}, fmt.Errorf("%w: active DeploymentBaseline identity is invalid", asyncjob.ErrPolicyViolation)
		}
		if _, err := uuid.Parse(baseline.PublicID); err != nil {
			return remediationPrepareBaseline{}, fmt.Errorf("%w: active DeploymentBaseline public identity is invalid", asyncjob.ErrPolicyViolation)
		}
		baseline.SourceRevision = strings.ToLower(baseline.SourceRevision)
		baseline.GitOpsRevision = strings.ToLower(baseline.GitOpsRevision)
		baseline.ImageDigest = strings.ToLower(baseline.ImageDigest)
		baseline.VerifiedAt = baseline.VerifiedAt.UTC().Truncate(time.Microsecond)
		matches = append(matches, baseline)
	}
	if err := rows.Err(); err != nil {
		return remediationPrepareBaseline{}, fmt.Errorf("iterate active DeploymentBaseline: %w", err)
	}
	if len(matches) != 1 {
		return remediationPrepareBaseline{}, fmt.Errorf("%w: remediation requires exactly one active DeploymentBaseline", asyncjob.ErrPolicyViolation)
	}
	baseline := matches[0]
	observationRows, err := tx.QueryContext(ctx, `SELECT id, public_id, observation_schema_version, source_identity,
 observed_json, content_hash
FROM baseline_observations
WHERE baseline_id = ? AND domain_schema_version = 3 AND observation_type = 'config_blob'
ORDER BY sequence_no, id
LIMIT 2`, baseline.ID)
	if err != nil {
		return remediationPrepareBaseline{}, fmt.Errorf("load baseline config observation: %w", err)
	}
	defer func() {
		if closeErr := observationRows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close baseline config observation rows: %w", closeErr))
		}
	}()
	count := 0
	for observationRows.Next() {
		var schemaVersion uint64
		var sourceIdentity string
		var observedJSON []byte
		count++
		if err := observationRows.Scan(&baseline.ObservationID, &baseline.ObservationPublicID, &schemaVersion,
			&sourceIdentity, &observedJSON, &baseline.ObservationHash); err != nil {
			return remediationPrepareBaseline{}, fmt.Errorf("scan baseline config observation: %w", err)
		}
		if schemaVersion == 0 || sourceIdentity == "" || len(observedJSON) == 0 || len(observedJSON) > 16*1024 || !json.Valid(observedJSON) ||
			baseline.ObservationHash != baseline.ConfigHash {
			return remediationPrepareBaseline{}, fmt.Errorf("%w: baseline config observation is not bound to config_hash", asyncjob.ErrPolicyViolation)
		}
		if _, err := uuid.Parse(baseline.ObservationPublicID); err != nil {
			return remediationPrepareBaseline{}, fmt.Errorf("%w: baseline observation public identity is invalid", asyncjob.ErrPolicyViolation)
		}
	}
	if err := observationRows.Err(); err != nil {
		return remediationPrepareBaseline{}, fmt.Errorf("iterate baseline config observation: %w", err)
	}
	if count != 1 || baseline.ObservationID == 0 {
		return remediationPrepareBaseline{}, fmt.Errorf("%w: active DeploymentBaseline requires one exact config_blob observation", asyncjob.ErrPolicyViolation)
	}
	return baseline, nil
}

func buildRemediationVerificationSnapshot(baseline remediationPrepareBaseline) (json.RawMessage, error) {
	profile := verification.GoldenRequiredEnvProfileV1()
	profileHash, err := verification.V3ProfileHash(profile)
	if err != nil {
		return nil, err
	}
	snapshot := struct {
		SchemaVersion int                              `json:"schema_version"`
		Profile       verification.V3ProfileDefinition `json:"profile"`
		ProfileHash   string                           `json:"profile_hash"`
		Baseline      struct {
			PublicID                  string `json:"public_id"`
			ObservationPublicID       string `json:"config_observation_public_id"`
			ObservationHash           string `json:"config_observation_hash"`
			SourceRevision            string `json:"source_revision"`
			ImageDigest               string `json:"image_digest"`
			GitOpsRevision            string `json:"gitops_revision"`
			VerificationPolicyVersion string `json:"verification_policy_version"`
			VerificationHash          string `json:"verification_hash"`
		} `json:"baseline"`
	}{SchemaVersion: 1, Profile: profile, ProfileHash: profileHash}
	snapshot.Baseline.PublicID = baseline.PublicID
	snapshot.Baseline.ObservationPublicID = baseline.ObservationPublicID
	snapshot.Baseline.ObservationHash = baseline.ObservationHash
	snapshot.Baseline.SourceRevision = baseline.SourceRevision
	snapshot.Baseline.ImageDigest = baseline.ImageDigest
	snapshot.Baseline.GitOpsRevision = baseline.GitOpsRevision
	snapshot.Baseline.VerificationPolicyVersion = baseline.VerificationPolicyVersion
	snapshot.Baseline.VerificationHash = baseline.VerificationHash
	payload, err := json.Marshal(snapshot)
	if err != nil || len(payload) > 16*1024 {
		return nil, fmt.Errorf("%w: remediation VerificationPlan snapshot exceeds bounds", remediation.ErrInvalidArgument)
	}
	return payload, nil
}

func validateRemediationPreparePolicy(policy remediation.RestoreEnvPolicy) error {
	if policy.Version != "restore-required-env-policy/v1" || policy.VerificationVersion != verification.GoldenRequiredEnvProfileID ||
		policy.APIVersion != "apps/v1" || policy.Namespace == "" || policy.Workload == "" || policy.Container == "" || policy.EnvKey == "" ||
		policy.Repository == "" || strings.Count(policy.Repository, "/") != 1 || policy.BaseBranch == "" ||
		policy.AllowedPath == "" || path.Clean(policy.AllowedPath) != policy.AllowedPath || strings.HasPrefix(policy.AllowedPath, "../") ||
		change.SensitivePath(policy.AllowedPath, nil) ||
		policy.MaxDiffBytes <= 0 || policy.MaxDiffBytes > remediation.MaxV3PlanDiffBytes ||
		policy.MaxPostImageBytes <= 0 || policy.MaxPostImageBytes > remediation.MaxV3PostImageBytes {
		return fmt.Errorf("%w: remediation.prepare policy is not the frozen restore_required_env contract", remediation.ErrInvalidArgument)
	}
	return nil
}

func classifyRemediationGitReadError(err error) error {
	switch {
	case errors.Is(err, change.ErrNotAllowed), errors.Is(err, change.ErrInvalidArgument), errors.Is(err, change.ErrPermission):
		return fmt.Errorf("%w: exact Git read policy rejected the request: %v", asyncjob.ErrPolicyViolation, err)
	case errors.Is(err, change.ErrNotFound), errors.Is(err, change.ErrConflict):
		return fmt.Errorf("%w: exact Git facts drifted: %v", remediation.ErrDrift, err)
	default:
		return err
	}
}

func validSHA256Text(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validImageDigest(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "sha256:") && len(value) == 71 && validSHA256Text(strings.TrimPrefix(value, "sha256:"))
}
