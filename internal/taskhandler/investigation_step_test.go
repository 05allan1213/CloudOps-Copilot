package taskhandler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

func TestInvestigationStepRunsOneModelDecisionThroughUnifiedRegistryAndCheckpoints(t *testing.T) {
	action := testInvestigationAction()
	model := &stepTestModel{delta: agent.StateDelta{
		SchemaVersion: agent.InvestigationStateSchemaVersion, BasisCheckpointVersion: 0,
		ProposedStop: agent.StopContinue, ProposedAction: &action,
	}, usage: agent.ModelUsage{InputTokens: 10, OutputTokens: 5}}
	taskStore := &stepTestTaskStore{}
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	operation := testInvestigationOperation(snapshot, model, &stepTestTool{}, taskStore)

	result := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if model.proposeCalls != 1 || model.synthesisCalls != 0 {
		t.Fatalf("model calls propose=%d synthesize=%d", model.proposeCalls, model.synthesisCalls)
	}
	if len(model.lastView.AllowedActions) != 1 || model.lastView.AllowedActions[0].Tool != "inspect_workload" {
		t.Fatalf("model did not receive the bounded action contract: %+v", model.lastView.AllowedActions)
	}
	if len(model.lastView.CandidateClaims) != 1 || model.lastView.CandidateClaims[0].ClaimType != "test-claim/v1" {
		t.Fatalf("model did not receive the candidate claim policy: %+v", model.lastView.CandidateClaims)
	}
	sufficiency := model.lastView.ClaimSufficiency["test-claim/v1"]
	if !slices.Contains(sufficiency.MissingFacets, "subject") || len(model.lastView.ActionCandidates) != 0 {
		t.Fatalf("model view did not carry deterministic sufficiency/production candidate state: %+v", model.lastView)
	}
	checkpoint := taskStore.singleCheckpoint(t)
	var durable investigationStepCheckpoint
	if err := json.Unmarshal(checkpoint.Payload, &durable); err != nil {
		t.Fatal(err)
	}
	if durable.Mode != stepModeDecide || durable.NextMode != stepModeTool || durable.NextAction == nil || durable.State.NextNode != agent.NodeExecuteTool {
		t.Fatalf("unexpected durable decision checkpoint: %+v", durable)
	}
	if durable.State.CheckpointVersion != 1 || durable.State.Usage.ModelCalls != 1 || durable.State.Usage.Steps != 1 {
		t.Fatalf("unexpected durable state: %+v", durable.State)
	}
}

func TestInvestigationStepReservesSingleRepairBeforeCallingTypedModel(t *testing.T) {
	action := testInvestigationAction()
	model := &twoCallStepModel{stepTestModel: stepTestModel{delta: agent.StateDelta{
		SchemaVersion: agent.InvestigationStateSchemaVersion, BasisCheckpointVersion: 0,
		ProposedStop: agent.StopContinue, ProposedAction: &action,
	}}}
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	snapshot.State.Limits.MaxModelCalls = 1
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, model, &stepTestTool{}, store)

	result := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if model.proposeCalls != 0 {
		t.Fatalf("provider was called without repair capacity: %d", model.proposeCalls)
	}
	var checkpoint investigationStepCheckpoint
	if err := json.Unmarshal(store.singleCheckpoint(t).Payload, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.TerminalOutcome != "insufficient_evidence" || !slices.Contains(checkpoint.Sufficiency.ReasonCodes, "decision_budget_exhausted") {
		t.Fatalf("checkpoint=%+v", checkpoint)
	}
}

func TestInvestigationStepDoesNotRetryAfterTypedModelRepairFails(t *testing.T) {
	model := &twoCallStepModel{stepTestModel: stepTestModel{proposeErr: agent.NewRuntimeError(
		agent.ErrorMalformedModel, "initial output and one repair were invalid", agent.ErrInvalidArgument,
	)}}
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, model, &stepTestTool{}, store)

	result := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if model.proposeCalls != 1 {
		t.Fatalf("typed model invocations=%d, want exactly 1", model.proposeCalls)
	}
	var checkpoint investigationStepCheckpoint
	if err := json.Unmarshal(store.singleCheckpoint(t).Payload, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.TerminalOutcome != "insufficient_evidence" || checkpoint.ErrorCode != "step_execution_malformed_model_output" ||
		!slices.Contains(checkpoint.Sufficiency.ReasonCodes, checkpoint.ErrorCode) {
		t.Fatalf("checkpoint=%+v", checkpoint)
	}
}

func TestInvestigationExecutionErrorCodeIsBoundedAndStable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "invalid argument", err: agent.ErrInvalidArgument, want: "step_execution_invalid_argument"},
		{name: "permission", err: agent.ErrPermission, want: "step_execution_permission"},
		{name: "timeout", err: context.DeadlineExceeded, want: "step_execution_timeout"},
		{name: "typed malformed model", err: agent.NewRuntimeError(agent.ErrorMalformedModel, "provider detail must not persist", agent.ErrInvalidArgument), want: "step_execution_malformed_model_output"},
		{name: "unknown", err: errors.New("credential-shaped provider detail must not persist"), want: "step_execution_internal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := investigationExecutionErrorCode(test.err)
			if got != test.want || !validInvestigationExecutionErrorCode(got) || strings.Contains(got, "provider") {
				t.Fatalf("code=%q want=%q", got, test.want)
			}
		})
	}
}

func TestInvestigationStepPersistsTypedToolObservationAndAdvancesToSynthesis(t *testing.T) {
	action := testInvestigationAction()
	signature, err := agent.ActionSignature(action)
	if err != nil {
		t.Fatal(err)
	}
	state := testInvestigationState()
	state.CheckpointVersion = 1
	state.NextNode = agent.NodeExecuteTool
	state.ToolAttempts = []agent.ToolAttempt{{Signature: signature, Tool: action.Tool, Status: "proposed"}}
	state.Usage = agent.Usage{Steps: 1, ModelCalls: 1, InputTokens: 10, OutputTokens: 5}
	tool := &stepTestTool{observation: agent.ToolObservation{
		Status: agent.CollectionAvailable, SourceSystem: "kubernetes", CollectionPath: "kubernetes-api/v1",
		TemplateVersion: action.TemplateID, Summary: "workload identity confirmed",
		Facts: []agent.EvidenceFact{{
			ID: "fact-subject", Type: "workload.subject_confirmed", CorroborationGroup: "workload-control-plane",
			Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
			ClaimUse: "supporting", CollectionStatus: agent.CollectionAvailable, Direct: true,
		}},
	}}
	taskStore := &stepTestTaskStore{}
	snapshot := testInvestigationSnapshot(t, stepModeTool, &action)
	snapshot.State = state
	snapshot.StateHash = stateHashForTest(t, state)
	payload, err := json.Marshal(investigationStepPayload{Mode: stepModeTool, AgentRunID: snapshot.RunPublicID, CycleNo: 1, BasisCheckpointVersion: 1, Action: &action})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Task.Payload = payload
	operation := testInvestigationOperation(snapshot, &stepTestModel{}, tool, taskStore)

	result := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if tool.calls != 1 {
		t.Fatalf("tool calls=%d, want 1", tool.calls)
	}
	checkpoint := taskStore.singleCheckpoint(t)
	var durable investigationStepCheckpoint
	if err := json.Unmarshal(checkpoint.Payload, &durable); err != nil {
		t.Fatal(err)
	}
	if durable.NextMode != stepModeSynthesize || durable.State.NextNode != agent.NodeProduceDiagnosis || durable.Observation == nil {
		t.Fatalf("unexpected tool checkpoint: %+v", durable)
	}
	if durable.State.Usage.ToolCalls != 1 || durable.State.Usage.Evidence != 1 || len(durable.State.Evidence) != 1 {
		t.Fatalf("tool state was not charged/persisted: %+v", durable.State)
	}
	fact := durable.Observation.Facts[0]
	if fact.IncidentID != snapshot.IncidentPublicID || fact.CycleNo != uint64(snapshot.Task.CycleNo) || fact.EvidenceID == "" {
		t.Fatalf("fact was not subject-bound: %+v", fact)
	}
}

func TestInvestigationStepTakeoverReusesTaskCheckpointWithoutRepeatingModelCall(t *testing.T) {
	action := testInvestigationAction()
	firstModel := &stepTestModel{delta: agent.StateDelta{
		SchemaVersion: agent.InvestigationStateSchemaVersion, BasisCheckpointVersion: 0,
		ProposedStop: agent.StopContinue, ProposedAction: &action,
	}}
	firstStore := &stepTestTaskStore{}
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	first := testInvestigationOperation(snapshot, firstModel, &stepTestTool{}, firstStore)
	if result := runInvestigationOperation(t, snapshot.Task, first.handle); result.Disposition != asyncjob.DispositionSucceeded {
		t.Fatalf("first result=%+v", result)
	}
	persisted := firstStore.singleCheckpoint(t)

	replayTask := snapshot.Task
	replayTask.CheckpointSchema = persisted.SchemaVersion
	replayTask.CheckpointVersion = persisted.Version
	replayTask.CheckpointHash = persisted.Hash
	replayTask.Checkpoint = reverseTopLevelJSONForTest(t, persisted.Payload)
	if bytes.Equal(replayTask.Checkpoint, persisted.Payload) {
		t.Fatal("test fixture did not simulate MySQL JSON key reordering")
	}
	snapshot.Task = replayTask
	replayModel := &stepTestModel{proposeErr: errors.New("model must not be called during takeover")}
	replayStore := &stepTestTaskStore{}
	replay := testInvestigationOperation(snapshot, replayModel, &stepTestTool{}, replayStore)
	result := runInvestigationOperation(t, replayTask, replay.handle)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("replay result=%+v", result)
	}
	if replayModel.proposeCalls != 0 || replayStore.checkpointCount() != 0 {
		t.Fatalf("takeover repeated work: model=%d checkpoints=%d", replayModel.proposeCalls, replayStore.checkpointCount())
	}

	var tampered map[string]any
	if err := json.Unmarshal(replayTask.Checkpoint, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["step_summary"] = "tampered after persistence"
	tamperedTask := replayTask
	tamperedTask.Checkpoint, _ = json.Marshal(tampered)
	var payload investigationStepPayload
	if err := json.Unmarshal(tamperedTask.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeInvestigationTaskCheckpoint(tamperedTask, snapshot, payload); err == nil || !strings.Contains(err.Error(), "hash does not match") {
		t.Fatalf("tampered task checkpoint error=%v", err)
	}
}

func TestDecodeRunStateAcceptsMySQLJSONKeyReorderingAndRejectsTampering(t *testing.T) {
	state := testInvestigationState()
	state.CheckpointVersion = 1
	state.Usage = agent.Usage{Steps: 1, ModelCalls: 1, InputTokens: 9007199254740993}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashInvestigationJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reordered := reverseTopLevelJSONForTest(t, encoded)
	if bytes.Equal(reordered, encoded) {
		t.Fatal("test fixture did not simulate MySQL JSON key reordering")
	}
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	snapshot.RunCheckpoint = reordered
	snapshot.RunCheckpointVersion = state.CheckpointVersion
	snapshot.RunCheckpointSchema = investigationRunCheckpointSchema
	snapshot.RunCheckpointHash = hash
	snapshot.Usage = state.Usage
	snapshot.Limits = state.Limits

	decoded, decodedHash, err := decodeRunState(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if decodedHash != hash || decoded.Usage.InputTokens != 9007199254740993 {
		t.Fatalf("decoded hash=%s usage=%+v", decodedHash, decoded.Usage)
	}

	var tampered map[string]any
	decoder := json.NewDecoder(bytes.NewReader(reordered))
	decoder.UseNumber()
	if err := decoder.Decode(&tampered); err != nil {
		t.Fatal(err)
	}
	tampered["objective"] = "tampered after persistence"
	snapshot.RunCheckpoint, _ = json.Marshal(tampered)
	if _, _, err := decodeRunState(snapshot); err == nil || !strings.Contains(err.Error(), "hash does not match") {
		t.Fatalf("tampered run checkpoint error=%v", err)
	}
}

func TestInvestigationStepBindsDiagnosisToCurrentFactsAndClaimPolicy(t *testing.T) {
	fact := agent.EvidenceFact{
		ID: "fact-subject", EvidenceID: "evidence-1", IncidentID: "incident-1", CycleNo: 1,
		Type: "workload.subject_confirmed", SourceSystem: "kubernetes", CollectionPath: "kubernetes-api/v1",
		CorroborationGroup: "workload-control-plane", Authority: "authoritative", Integrity: "verified",
		Freshness: "fresh", Completeness: "complete", ClaimUse: "supporting",
		CollectionStatus: agent.CollectionAvailable, Direct: true,
	}
	state := testInvestigationState()
	state.CheckpointVersion = 2
	state.NextNode = agent.NodeProduceDiagnosis
	state.Evidence = []agent.EvidenceReference{{ID: fact.ID, FactType: fact.Type}}
	state.Usage = agent.Usage{Steps: 2, ToolCalls: 1, ModelCalls: 1, InputTokens: 10, OutputTokens: 5, Evidence: 1}
	snapshot := testInvestigationSnapshot(t, stepModeSynthesize, nil)
	snapshot.State, snapshot.Facts = state, []agent.EvidenceFact{fact}
	snapshot.StateHash = stateHashForTest(t, state)
	payload, err := json.Marshal(investigationStepPayload{Mode: stepModeSynthesize, AgentRunID: snapshot.RunPublicID, CycleNo: 1, BasisCheckpointVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Task.Payload = payload
	model := &stepTestModel{diagnosis: agent.DiagnosisCandidate{
		ClaimType: "test-claim/v1", Summary: "The workload identity is confirmed by the control plane.",
		Confidence: agent.DiagnosisConfirmed, EvidenceFactIDs: []string{fact.ID}, RemediationHint: agent.RemediationNone,
	}, usage: agent.ModelUsage{InputTokens: 12, OutputTokens: 6}}
	taskStore := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, model, &stepTestTool{}, taskStore)

	result := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	checkpoint := taskStore.singleCheckpoint(t)
	var durable investigationStepCheckpoint
	if err := json.Unmarshal(checkpoint.Payload, &durable); err != nil {
		t.Fatal(err)
	}
	if durable.TerminalOutcome != "diagnosed" || durable.State.TerminalOutcome != "diagnosed" || durable.Diagnosis == nil {
		t.Fatalf("unexpected diagnosis checkpoint: %+v", durable)
	}
	if durable.Diagnosis.ClaimPolicyHash == "" || durable.Diagnosis.DiagnosisHash == "" || len(durable.Diagnosis.EvidenceIDs) != 1 {
		t.Fatalf("diagnosis was not policy/evidence bound: %+v", durable.Diagnosis)
	}
	if model.synthesisCalls != 1 || model.proposeCalls != 0 {
		t.Fatalf("model calls propose=%d synthesize=%d", model.proposeCalls, model.synthesisCalls)
	}
}

func TestInvestigationStepEnqueuesNextTaskWithNextSubjectVersionDedupe(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	state := snapshot.State
	state.CheckpointVersion = 1
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, &stepTestModel{}, &stepTestTool{}, store)
	checkpoint := investigationStepCheckpoint{NextMode: stepModeDecide}
	if err := operation.enqueueNextInvestigationStep(context.Background(), nil, snapshot, state, checkpoint); err != nil {
		t.Fatal(err)
	}
	enqueued := store.singleEnqueued(t)
	if enqueued.ExpectedSubjectVersion != 2 {
		t.Fatalf("expected subject version=%d, want 2", enqueued.ExpectedSubjectVersion)
	}
	currentDedupe := hashCanonical("task", snapshot.RunPublicID, "investigation.step", "1")
	if enqueued.DedupeKey == currentDedupe || enqueued.DedupeKey != hashCanonical("task", snapshot.RunPublicID, "investigation.step", "2") {
		t.Fatalf("next dedupe=%s current=%s", enqueued.DedupeKey, currentDedupe)
	}
}

func TestInvestigationStepEnqueuesRemediationOnlyForConfirmedRestoreDiagnosis(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeSynthesize, nil)
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, &stepTestModel{}, &stepTestTool{}, store)
	at := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	checkpoint := investigationStepCheckpoint{
		TerminalOutcome: "diagnosed", CapturedAt: at,
		Diagnosis: &agent.DiagnosisRecord{Candidate: agent.DiagnosisCandidate{
			Confidence: agent.DiagnosisConfirmed, RemediationHint: agent.RemediationRestoreRequiredEnv,
		}},
	}
	if err := operation.enqueueRemediationPrepare(context.Background(), nil, snapshot, checkpoint); err != nil {
		t.Fatal(err)
	}
	enqueued := store.singleEnqueued(t)
	if enqueued.Type != asyncjob.TaskRemediationPrepare || enqueued.SubjectType != "agent_run" ||
		enqueued.SubjectID != snapshot.Task.SubjectID || enqueued.Transition != "remediation.prepare" ||
		enqueued.ExpectedSubjectVersion != snapshot.Task.ExpectedSubjectVersion+1 ||
		enqueued.DedupeKey != hashCanonical("task", snapshot.RunPublicID, "remediation.prepare", "2") ||
		enqueued.AvailableAt == nil || !enqueued.AvailableAt.Equal(at) {
		t.Fatalf("unexpected remediation task: %+v", enqueued)
	}
	var payload remediationPreparePayload
	if err := json.Unmarshal(enqueued.Payload, &payload); err != nil || payload.AgentRunID != snapshot.RunPublicID || payload.CycleNo != 1 {
		t.Fatalf("unexpected remediation payload=%+v err=%v", payload, err)
	}

	store = &stepTestTaskStore{}
	operation = testInvestigationOperation(snapshot, &stepTestModel{}, &stepTestTool{}, store)
	checkpoint.Diagnosis.Candidate.RemediationHint = agent.RemediationNone
	if err := operation.enqueueRemediationPrepare(context.Background(), nil, snapshot, checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(store.enqueued) != 0 {
		t.Fatalf("non-remediable diagnosis enqueued %d tasks", len(store.enqueued))
	}
}

func TestBuildInvestigationChangeCandidatesBindsImmutableDeploymentIdentity(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeTool, nil)
	evidenceID := "22222222-2222-4222-8222-222222222222"
	changeRef := "33333333-3333-4333-8333-333333333333"
	revision := strings.Repeat("a", 40)
	imageDigest := "sha256:" + strings.Repeat("b", 64)
	fact := investigationCandidateTestFact("deployment.change_ref", evidenceID, map[string]string{
		"change_ref": changeRef, "repository": "acme/gitops", "revision": revision,
		"image_digest": imageDigest, "path": "apps/demo.yaml",
		"deployed_at": "2026-07-19T09:58:00Z", "is_current": "true",
	}, snapshot)
	checkpoint := investigationStepCheckpoint{Observation: &agent.ToolObservation{
		Status: agent.CollectionAvailable, CollectionPath: "argocd/deployment-context", Facts: []agent.EvidenceFact{fact},
	}}

	first, err := buildInvestigationChangeCandidates(snapshot, checkpoint, evidenceID)
	if err != nil || len(first) != 1 {
		t.Fatalf("candidates=%+v err=%v", first, err)
	}
	second, err := buildInvestigationChangeCandidates(snapshot, checkpoint, evidenceID)
	if err != nil || len(second) != 1 {
		t.Fatalf("replayed candidates=%+v err=%v", second, err)
	}
	candidate := first[0]
	if candidate.PublicID == "" || candidate.ContentHash == "" || candidate.ChangeRef != changeRef ||
		candidate.Repository != "acme/gitops" || candidate.Revision != revision || candidate.ImageDigest != imageDigest ||
		candidate.TargetPath != "apps/demo.yaml" || candidate.Category != "high_confidence" ||
		!slices.Equal(candidate.SupportingEvidence, []string{evidenceID}) ||
		candidate.PublicID != second[0].PublicID || candidate.ContentHash != second[0].ContentHash {
		t.Fatalf("candidate identity is not deterministic: first=%+v second=%+v", candidate, second[0])
	}
}

func TestBuildInvestigationChangeAssessmentRequiresCandidateSpecificCorrelation(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeSynthesize, nil)
	deploymentEvidence := "22222222-2222-4222-8222-222222222222"
	detailEvidence := "33333333-3333-4333-8333-333333333333"
	changeRef := "44444444-4444-4444-8444-444444444444"
	revision := strings.Repeat("a", 40)
	imageDigest := "sha256:" + strings.Repeat("b", 64)
	candidate := persistedInvestigationChangeCandidate{ID: 41, investigationChangeCandidate: investigationChangeCandidate{
		PublicID: "55555555-5555-4555-8555-555555555555", ChangeRef: changeRef,
		Repository: "acme/gitops", Revision: revision, ImageDigest: imageDigest,
		TargetPath: "apps/demo.yaml", Category: "high_confidence",
		ChangeTime:         time.Date(2026, 7, 19, 9, 58, 0, 0, time.UTC),
		SupportingEvidence: []string{deploymentEvidence}, ContentHash: strings.Repeat("c", 64),
	}}
	snapshot.Facts = []agent.EvidenceFact{
		investigationCandidateTestFact("deployment.change_ref", deploymentEvidence, map[string]string{
			"change_ref": changeRef, "repository": candidate.Repository, "revision": revision, "is_current": "true",
		}, snapshot),
		investigationCandidateTestFact("argocd.bad_revision_deployed", deploymentEvidence, map[string]string{"deployed_revision": revision}, snapshot),
		investigationCandidateTestFact("source_revision.unchanged", deploymentEvidence, nil, snapshot),
		investigationCandidateTestFact("image_digest.unchanged", deploymentEvidence, map[string]string{"image_digest": imageDigest}, snapshot),
		investigationCandidateTestFact("gitops.required_env_removed", detailEvidence, map[string]string{
			"change_ref": changeRef, "repository": candidate.Repository, "revision": revision, "path": candidate.TargetPath,
		}, snapshot),
		investigationCandidateTestFact("change.ci_succeeded", detailEvidence, map[string]string{
			"change_ref": changeRef, "repository": candidate.Repository, "revision": revision,
		}, snapshot),
	}
	policyHash, diagnosisHash := strings.Repeat("d", 64), strings.Repeat("e", 64)
	checkpoint := investigationStepCheckpoint{TerminalOutcome: "diagnosed", Diagnosis: &agent.DiagnosisRecord{
		Candidate: agent.DiagnosisCandidate{
			ClaimType: agent.GoldenRequiredEnvClaimPolicy().ClaimType, Confidence: agent.DiagnosisConfirmed,
			RemediationHint: agent.RemediationRestoreRequiredEnv,
		},
		ClaimPolicyHash: policyHash, DiagnosisHash: diagnosisHash,
		EvidenceIDs: []string{deploymentEvidence, detailEvidence},
	}}

	assessment, err := buildInvestigationChangeAssessment(snapshot, checkpoint, candidate, policyHash, diagnosisHash)
	if err != nil || assessment.Status != "matched" ||
		!slices.Equal(assessment.SupportingEvidence, []string{deploymentEvidence, detailEvidence}) || len(assessment.ContradictingEvidence) != 0 {
		t.Fatalf("matched assessment=%+v err=%v", assessment, err)
	}

	other := candidate
	other.ID = 42
	other.ChangeRef = "66666666-6666-4666-8666-666666666666"
	other.Revision = strings.Repeat("f", 40)
	unknown, err := buildInvestigationChangeAssessment(snapshot, checkpoint, other, policyHash, diagnosisHash)
	if err != nil || unknown.Status != "unknown" {
		t.Fatalf("cross-candidate assessment=%+v err=%v", unknown, err)
	}
}

func TestBuildInvestigationChangeAssessmentPlacesDisqualifierInContradictingEvidence(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeSynthesize, nil)
	deploymentEvidence := "22222222-2222-4222-8222-222222222222"
	detailEvidence := "33333333-3333-4333-8333-333333333333"
	candidate := persistedInvestigationChangeCandidate{ID: 41, investigationChangeCandidate: investigationChangeCandidate{
		PublicID: "55555555-5555-4555-8555-555555555555", ChangeRef: "44444444-4444-4444-8444-444444444444",
		Repository: "acme/gitops", Revision: strings.Repeat("a", 40), ImageDigest: "sha256:" + strings.Repeat("b", 64),
		TargetPath: "apps/demo.yaml", Category: "high_confidence", ChangeTime: time.Now().UTC(),
		SupportingEvidence: []string{deploymentEvidence}, ContentHash: strings.Repeat("c", 64),
	}}
	snapshot.Facts = []agent.EvidenceFact{
		investigationCandidateTestFact("deployment.change_ref", deploymentEvidence, map[string]string{
			"change_ref": candidate.ChangeRef, "repository": candidate.Repository, "revision": candidate.Revision, "is_current": "true",
		}, snapshot),
		investigationCandidateTestFact("gitops.required_env_not_removed", detailEvidence, map[string]string{
			"change_ref": candidate.ChangeRef, "repository": candidate.Repository, "revision": candidate.Revision, "path": candidate.TargetPath,
		}, snapshot),
	}
	assessment, err := buildInvestigationChangeAssessment(snapshot, investigationStepCheckpoint{TerminalOutcome: "insufficient_evidence"}, candidate, strings.Repeat("d", 64), "")
	if err != nil || assessment.Status != "excluded" ||
		!slices.Equal(assessment.SupportingEvidence, []string{deploymentEvidence}) ||
		!slices.Equal(assessment.ContradictingEvidence, []string{detailEvidence}) {
		t.Fatalf("excluded assessment=%+v err=%v", assessment, err)
	}
}

func investigationCandidateTestFact(factType, evidenceID string, attributes map[string]string, snapshot investigationSnapshot) agent.EvidenceFact {
	return agent.EvidenceFact{
		ID: hashCanonical("candidate-test-fact", factType, evidenceID), EvidenceID: evidenceID,
		IncidentID: snapshot.IncidentPublicID, CycleNo: uint64(snapshot.Task.CycleNo), Type: factType,
		SourceSystem: "fixture", CollectionPath: "fixture/v1", CorroborationGroup: "fixture/candidate",
		Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
		ClaimUse: "support", CollectionStatus: agent.CollectionAvailable, Direct: true, Attributes: attributes,
	}
}

func TestInvestigationStepRejectsInvalidLeaseBeforeCallingDependencies(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeDecide, nil)
	model := &stepTestModel{}
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, model, &stepTestTool{}, store)

	result := operation.handle(context.Background(), asyncjob.Execution{Task: snapshot.Task})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "invalid_task_lease" {
		t.Fatalf("result=%+v", result)
	}
	if model.proposeCalls != 0 || store.checkpointCount() != 0 {
		t.Fatalf("invalid lease reached dependencies: model=%d checkpoints=%d", model.proposeCalls, store.checkpointCount())
	}
}

func TestInvestigationStepRejectsToolPayloadOutsideFrozenPolicy(t *testing.T) {
	action := testInvestigationAction()
	action.ExpectedFactTypes = []string{"secret.value"}
	signature, err := agent.ActionSignature(action)
	if err != nil {
		t.Fatal(err)
	}
	state := testInvestigationState()
	state.CheckpointVersion = 1
	state.NextNode = agent.NodeExecuteTool
	state.ToolAttempts = []agent.ToolAttempt{{Signature: signature, Tool: action.Tool, Status: "proposed"}}
	snapshot := testInvestigationSnapshot(t, stepModeTool, &action)
	snapshot.State = state
	snapshot.StateHash = stateHashForTest(t, state)
	payload, err := json.Marshal(investigationStepPayload{
		Mode: stepModeTool, AgentRunID: snapshot.RunPublicID, CycleNo: 1,
		BasisCheckpointVersion: 1, Action: &action,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Task.Payload = payload
	tool := &stepTestTool{}
	store := &stepTestTaskStore{}
	operation := testInvestigationOperation(snapshot, &stepTestModel{}, tool, store)

	result := runInvestigationOperation(t, snapshot.Task, operation.handle)
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "invalid_step_payload" {
		t.Fatalf("result=%+v", result)
	}
	if tool.calls != 0 || store.checkpointCount() != 0 {
		t.Fatalf("invalid action reached dependencies: tools=%d checkpoints=%d", tool.calls, store.checkpointCount())
	}
}

type stepTestModel struct {
	delta          agent.StateDelta
	diagnosis      agent.DiagnosisCandidate
	usage          agent.ModelUsage
	proposeErr     error
	proposeCalls   int
	synthesisCalls int
	lastView       agent.ModelView
}

type twoCallStepModel struct{ stepTestModel }

func (*twoCallStepModel) MaxProviderCallsPerInvocation() int { return 2 }

func (m *stepTestModel) ProposeDelta(_ context.Context, view agent.ModelView) (agent.StateDelta, agent.ModelUsage, error) {
	m.proposeCalls++
	m.lastView = view
	return m.delta, m.usage, m.proposeErr
}

func (m *stepTestModel) SynthesizeDiagnosis(context.Context, agent.DiagnosisView) (agent.DiagnosisCandidate, agent.ModelUsage, error) {
	m.synthesisCalls++
	return m.diagnosis, m.usage, nil
}

type stepTestTool struct {
	observation agent.ToolObservation
	err         error
	calls       int
}

func (t *stepTestTool) Execute(context.Context, agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	t.calls++
	return t.observation, t.err
}

type stepTestLoader struct{ snapshot investigationSnapshot }

func (l stepTestLoader) Load(_ context.Context, task asyncjob.Task) (investigationSnapshot, error) {
	result := l.snapshot
	result.Task = task
	return result, nil
}

type stepTestTaskStore struct {
	mu          sync.Mutex
	checkpoints []asyncjob.Checkpoint
	enqueued    []asyncjob.NewTask
}

func (s *stepTestTaskStore) Checkpoint(_ context.Context, _ asyncjob.Lease, checkpoint asyncjob.Checkpoint, _ asyncjob.Mutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints = append(s.checkpoints, checkpoint)
	return nil
}

func (s *stepTestTaskStore) EnqueueIn(_ context.Context, _ asyncjob.DBTX, task asyncjob.NewTask) (*asyncjob.Task, error) {
	s.mu.Lock()
	s.enqueued = append(s.enqueued, task)
	s.mu.Unlock()
	return &asyncjob.Task{ID: 99}, nil
}

func (s *stepTestTaskStore) singleCheckpoint(t *testing.T) asyncjob.Checkpoint {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.checkpoints) != 1 {
		t.Fatalf("checkpoints=%d, want 1", len(s.checkpoints))
	}
	return s.checkpoints[0]
}

func (s *stepTestTaskStore) checkpointCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.checkpoints)
}

func (s *stepTestTaskStore) singleEnqueued(t *testing.T) asyncjob.NewTask {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.enqueued) != 1 {
		t.Fatalf("enqueued=%d, want 1", len(s.enqueued))
	}
	return s.enqueued[0]
}

type stepRunnerStore struct {
	mu        sync.Mutex
	execution asyncjob.Execution
	claimed   bool
	resolved  chan asyncjob.Result
}

func (*stepRunnerStore) Ready(context.Context) error { return nil }

func (s *stepRunnerStore) Claim(ctx context.Context, request asyncjob.ClaimRequest) (*asyncjob.Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.Queue != asyncjob.QueueInvestigate || s.claimed {
		return nil, asyncjob.ErrNoTask
	}
	s.claimed = true
	result := s.execution
	result.Task.Status = asyncjob.StatusRunning
	result.Task.LeaseOwner = request.Owner
	result.Task.LeaseGeneration = result.Lease.Generation
	result.Lease.Owner = request.Owner
	return &result, nil
}

func (*stepRunnerStore) Heartbeat(context.Context, asyncjob.Lease, time.Duration) error { return nil }

func (s *stepRunnerStore) Resolve(_ context.Context, _ asyncjob.Lease, result asyncjob.Result) error {
	s.resolved <- result
	return nil
}

func runInvestigationOperation(t *testing.T, task asyncjob.Task, operation Operation) asyncjob.Result {
	t.Helper()
	lease := asyncjob.Lease{TaskID: task.ID, Owner: "runner", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: task.MaxAttempts}
	store := &stepRunnerStore{execution: asyncjob.Execution{Task: task, Lease: lease}, resolved: make(chan asyncjob.Result, 1)}
	handlers := New(Config{InvestigationStep: operation})
	runner, err := asyncjob.NewRunner(asyncjob.RunnerConfig{
		Owner: "runner", Store: store, Handlers: handlers, PollInterval: time.Millisecond,
		DrainTimeout: time.Second, CancelWait: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := runner.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	var result asyncjob.Result
	select {
	case result = <-store.resolved:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for investigation operation")
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := runner.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	return result
}

func testInvestigationOperation(snapshot investigationSnapshot, model agent.InvestigationModel, tool agent.InvestigationReadTool, tasks InvestigationTaskStore) *investigationStepOperation {
	claimPolicy := testClaimPolicy()
	actionPolicies := map[string]agent.ToolActionPolicy{
		"inspect_workload": {TemplateIDs: []string{"workload-snapshot/v1"}, ParameterKeys: []string{"include_events"}, ExpectedFactTypes: []string{"workload.subject_confirmed"}},
	}
	claimJSON, _ := json.Marshal(claimPolicy)
	actionJSON, _ := json.Marshal(actionPolicies)
	snapshot.State.Coverage.ClaimPolicyVersion = claimPolicy.Version
	snapshot.State.Coverage.ClaimPolicyHash = hashBytesInvestigation(claimJSON)
	snapshot.State.Coverage.ActionPolicyHash = hashBytesInvestigation(actionJSON)
	stateJSON, _ := canonicalInvestigationJSON(snapshot.State)
	snapshot.StateHash = hashBytesInvestigation(stateJSON)
	return &investigationStepOperation{
		cfg: InvestigationStepConfig{
			Tasks: tasks, Model: model, Tools: tool, ClaimPolicy: claimPolicy, ActionPolicies: actionPolicies,
			RequiredSources: []string{"kubernetes"}, MaxCheckpointBytes: defaultTaskCheckpointBytes,
			Now: func() time.Time { return time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC) },
		},
		loader: stepTestLoader{snapshot: snapshot},
	}
}

func testInvestigationSnapshot(t *testing.T, mode string, action *agent.ProposedAction) investigationSnapshot {
	t.Helper()
	payload, err := json.Marshal(investigationStepPayload{Mode: mode, AgentRunID: "run-1", CycleNo: 1, Action: action})
	if err != nil {
		t.Fatal(err)
	}
	task := asyncjob.Task{
		ID: 10, PublicID: "11111111-1111-4111-8111-111111111111", IncidentID: 20, CycleNo: 1,
		Queue: asyncjob.QueueInvestigate, Type: asyncjob.TaskInvestigationAdvance, SubjectType: "agent_run", SubjectID: 30,
		Transition: "investigation.step", ExpectedSubjectVersion: 1, PayloadSchemaVersion: 1, Payload: payload,
		DedupeKey: hashCanonical("task", "run-1", "investigation.step", "1"), Priority: 50, MaxAttempts: 5,
	}
	state := testInvestigationState()
	return investigationSnapshot{
		Task: task, RunPublicID: "run-1", Status: "pending",
		Objective: "investigate", Model: "fixture", PromptVersion: "v1", Limits: state.Limits, Usage: state.Usage,
		RunVersion: 1, ExpectedIncidentVersion: 2, DeadlineAt: time.Date(2026, 7, 19, 10, 3, 0, 0, time.UTC),
		RunCreatedAt: time.Date(2026, 7, 19, 9, 59, 0, 0, time.UTC), IncidentPublicID: "incident-1",
		IncidentVersion: 2, Cluster: "kind", Environment: "demo", Namespace: "demo",
		ServiceName: "demo", TargetKind: "Deployment", TargetName: "demo", FirstSeenAt: time.Date(2026, 7, 19, 9, 58, 0, 0, time.UTC),
		Facts: nil, Evidence: map[string]agent.EvidenceRecord{}, State: state, StateHash: stateHashForTest(t, state), ScopeRef: "scope-1",
	}
}

func testInvestigationState() agent.InvestigationState {
	return agent.InvestigationState{
		SchemaVersion: agent.InvestigationStateSchemaVersion, RunID: "run-1", IncidentID: "incident-1", CycleNo: 1,
		IncidentVersion: 2, Objective: "investigate", NextNode: agent.NodeSelectAction,
		Coverage:  agent.CoverageRequirements{ClaimType: "test-claim/v1", RequiredFacets: []string{"subject"}, RequiredSources: []string{"kubernetes"}},
		Limits:    agent.Limits{MaxSteps: 8, MaxToolCalls: 8, MaxModelCalls: 10, TokenBudget: 16000, MaxEvidenceItems: 20, MaxRuntime: 3 * time.Minute, ToolTimeout: 30 * time.Second, MaxEvidenceBytes: 16 * 1024, MaxCheckpointSize: 64 * 1024, MaxStepRetries: 1},
		UpdatedAt: time.Date(2026, 7, 19, 9, 59, 0, 0, time.UTC),
	}
}

func testClaimPolicy() agent.ClaimPolicy {
	return agent.ClaimPolicy{Version: "test-policy/v1", ClaimType: "test-claim/v1", Requirements: []agent.FactRequirement{{Facet: "subject", AnyOf: []string{"workload.subject_confirmed"}}}, MinIndependentCollectors: 1, RequireDirectFact: true}
}

func testInvestigationAction() agent.ProposedAction {
	return agent.ProposedAction{Tool: "inspect_workload", ScopeRef: "scope-1", TemplateID: "workload-snapshot/v1", BoundedParameters: json.RawMessage(`{"include_events":true}`), ExpectedFactTypes: []string{"workload.subject_confirmed"}, PurposeSummary: "confirm the workload subject"}
}

func stateHashForTest(t *testing.T, state agent.InvestigationState) string {
	t.Helper()
	encoded, err := canonicalInvestigationJSON(state)
	if err != nil {
		t.Fatal(err)
	}
	return hashBytesInvestigation(encoded)
}

func reverseTopLevelJSONForTest(t *testing.T, value []byte) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	slices.Reverse(keys)
	var output bytes.Buffer
	output.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			output.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(encodedKey)
		output.WriteByte(':')
		output.Write(fields[key])
	}
	output.WriteByte('}')
	return output.Bytes()
}
