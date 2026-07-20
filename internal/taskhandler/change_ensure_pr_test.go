package taskhandler

import (
	"context"
	"crypto/sha1" // #nosec G505 -- fixtures compute Git protocol object identities.
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestChangeEnsurePRPlanModeCreatesOnlyDurableChangeRequest(t *testing.T) {
	plan := changeTestPlan()
	store := &changeTestStore{plan: changePlanSnapshot{Plan: plan}}
	operation := &changeEnsurePROperation{
		cfg:   ChangeEnsurePRConfig{CurrentPolicyHash: plan.PolicySnapshotHash, Now: func() time.Time { return plan.CreatedAt.Add(time.Minute) }},
		store: store,
	}
	payload := []byte(`{"plan_id":"` + plan.PublicID + `"}`)
	task := asyncjob.Task{
		ID: 10, IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo), Queue: asyncjob.QueueDeliver,
		Type: asyncjob.TaskChangeEnsurePR, SubjectType: "remediation_plan", SubjectID: plan.ID,
		Transition: "change.ensure_pr", ExpectedSubjectVersion: plan.RowVersion,
		PayloadSchemaVersion: changeEnsurePayloadSchema, Payload: payload,
	}
	execution := asyncjob.Execution{Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5}}
	result := operation.handle(context.Background(), execution)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if store.created {
		t.Fatal("ChangeRequest was created before the task Resolve transaction")
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !store.created {
		t.Fatal("ChangeRequest mutation was not invoked")
	}
}

func TestBuildPhasedDeliveryRequestBindsApprovedPlan(t *testing.T) {
	plan := changeTestPlan()
	request := buildPhasedDeliveryRequest(plan, strings.Repeat("9", 64))
	if request.Repository != plan.TargetRepository || request.BaseRevision != plan.TargetBaseRevision ||
		request.BaseBlobSHA != plan.BaseBlobSHA || request.ExpectedBeforeHash != plan.ExpectedBeforeHash ||
		request.ExpectedPostImageHash != plan.ExpectedPostImageHash || request.ExpectedTreeHash != plan.ExpectedTreeHash ||
		string(request.Content) != string(plan.PostImage) || !strings.Contains(request.PRBody, plan.CanonicalPlanHash) ||
		!strings.Contains(request.PRBody, plan.EvidenceBindings[0].ID) {
		t.Fatalf("request is not fully bound: %+v", request)
	}
	if !validWriteAdvance(remediation.WritePhaseEnsureBranch, remediation.WritePhaseEnsureCommit) ||
		validWriteAdvance(remediation.WritePhaseEnsureDraftPR, remediation.WritePhaseEnsureCommit) {
		t.Fatal("write phase ordering is not monotonic")
	}
}

func TestChangeEnsurePRRejectsSupersededEvidenceBeforeWritePhase(t *testing.T) {
	plan := changeTestPlan()
	store := &changeTestStore{loadChangeErr: errApprovedEvidenceSuperseded}
	operation := &changeEnsurePROperation{
		cfg: ChangeEnsurePRConfig{
			CurrentPolicyHash: plan.PolicySnapshotHash,
			Now:               func() time.Time { return plan.CreatedAt.Add(time.Minute) },
		},
		store: store,
	}
	task := asyncjob.Task{
		ID: 12, IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo), Queue: asyncjob.QueueDeliver,
		Type: asyncjob.TaskChangeEnsurePR, SubjectType: "change_request", SubjectID: 31,
		Transition: "change.ensure_pr", ExpectedSubjectVersion: 4, PayloadSchemaVersion: changeEnsurePayloadSchema,
		Payload: []byte(`{"plan_id":"` + plan.PublicID + `","change_request_id":"44444444-4444-4444-8444-444444444444","write_phase":"ensure_commit"}`),
	}
	result := operation.handle(context.Background(), asyncjob.Execution{
		Task:  task,
		Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5},
	})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "change_preflight_rejected" || store.marked {
		t.Fatalf("result=%+v marked=%t", result, store.marked)
	}
}

func TestChangeEnsurePRRejectsSupersededEvidenceBeforeCreatingChangeRequest(t *testing.T) {
	plan := changeTestPlan()
	store := &changeTestStore{loadApprovedErr: errApprovedEvidenceSuperseded}
	operation := &changeEnsurePROperation{
		cfg: ChangeEnsurePRConfig{
			CurrentPolicyHash: plan.PolicySnapshotHash,
			Now:               func() time.Time { return plan.CreatedAt.Add(time.Minute) },
		},
		store: store,
	}
	task := asyncjob.Task{
		ID: 13, IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo), Queue: asyncjob.QueueDeliver,
		Type: asyncjob.TaskChangeEnsurePR, SubjectType: "remediation_plan", SubjectID: plan.ID,
		Transition: "change.ensure_pr", ExpectedSubjectVersion: plan.RowVersion,
		PayloadSchemaVersion: changeEnsurePayloadSchema, Payload: []byte(`{"plan_id":"` + plan.PublicID + `"}`),
	}
	result := operation.handle(context.Background(), asyncjob.Execution{
		Task:  task,
		Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5},
	})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "change_preflight_rejected" || result.Mutate == nil || store.created {
		t.Fatalf("result=%+v created=%t", result, store.created)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !store.superseded {
		t.Fatal("pre-ChangeRequest preflight rejection did not supersede the Plan")
	}
}

func TestChangeEnsurePRRevalidatesEvidenceBeforeEveryWriteMarker(t *testing.T) {
	plan := changeTestPlan()
	for _, phase := range []remediation.WritePhase{
		remediation.WritePhaseEnsureBranch,
		remediation.WritePhaseEnsureCommit,
		remediation.WritePhaseEnsureDraftPR,
	} {
		t.Run(string(phase), func(t *testing.T) {
			changeID := "44444444-4444-4444-8444-444444444444"
			store := &changeTestStore{
				change: changeSnapshot{
					PlanSnapshot:    changePlanSnapshot{Plan: plan},
					ChangeRequestID: 31, ChangePublicID: changeID, ChangeVersion: 4,
					WritePhase: phase, LogicalOperation: strings.Repeat("9", 64),
				},
				markErr: errApprovedEvidenceSuperseded,
			}
			operation := &changeEnsurePROperation{
				cfg: ChangeEnsurePRConfig{
					CurrentPolicyHash: plan.PolicySnapshotHash,
					Now:               func() time.Time { return plan.CreatedAt.Add(time.Minute) },
				},
				store: store,
			}
			task := asyncjob.Task{
				ID: 14, IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo), Queue: asyncjob.QueueDeliver,
				Type: asyncjob.TaskChangeEnsurePR, SubjectType: "change_request", SubjectID: 31,
				Transition: "change.ensure_pr", ExpectedSubjectVersion: 4, PayloadSchemaVersion: changeEnsurePayloadSchema,
				Payload: []byte(`{"plan_id":"` + plan.PublicID + `","change_request_id":"` + changeID + `","write_phase":"` + string(phase) + `"}`),
			}
			result := operation.handle(context.Background(), asyncjob.Execution{
				Task:  task,
				Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5},
			})
			if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "change_preflight_rejected" || !store.marked ||
				len(store.steps) != 2 || store.steps[0] != "validate" || store.steps[1] != "mark" {
				t.Fatalf("result=%+v marked=%t steps=%v", result, store.marked, store.steps)
			}
		})
	}
}

func TestChangeEnsurePRNoWritePreflightFailureInvalidatesWithoutCallingWriter(t *testing.T) {
	plan := changeTestPlan()
	changeID := "44444444-4444-4444-8444-444444444444"
	store := &changeTestStore{
		change: changeSnapshot{
			PlanSnapshot: changePlanSnapshot{Plan: plan}, ChangeRequestID: 31,
			ChangePublicID: changeID, ChangeVersion: 4, WritePhase: remediation.WritePhaseEnsureCommit,
			LogicalOperation: strings.Repeat("9", 64),
		},
		validateErr: fmt.Errorf("%w: policy drift", asyncjob.ErrPolicyViolation),
	}
	writer := &changeWriterSpy{}
	operation := &changeEnsurePROperation{
		cfg:   ChangeEnsurePRConfig{Writer: writer, CurrentPolicyHash: plan.PolicySnapshotHash, Now: func() time.Time { return plan.CreatedAt.Add(time.Minute) }},
		store: store,
	}
	task := asyncjob.Task{
		ID: 15, IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo), Queue: asyncjob.QueueDeliver,
		Type: asyncjob.TaskChangeEnsurePR, SubjectType: "change_request", SubjectID: 31,
		Transition: "change.ensure_pr", ExpectedSubjectVersion: 4, PayloadSchemaVersion: changeEnsurePayloadSchema,
		Payload: []byte(`{"plan_id":"` + plan.PublicID + `","change_request_id":"` + changeID + `","write_phase":"ensure_commit"}`),
	}
	result := operation.handle(context.Background(), asyncjob.Execution{
		Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: 4, Attempt: 1, MaxAttempts: 5},
	})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "change_preflight_rejected" || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !store.invalidated || store.safeTerminal || store.marked || writer.totalCalls() != 0 {
		t.Fatalf("invalidated=%t safe=%t marked=%t writer_calls=%d", store.invalidated, store.safeTerminal, store.marked, writer.totalCalls())
	}
}

func TestChangeEnsurePRWriterSelectionReconcilesMarkerAndEnsuresExactlyOnePhase(t *testing.T) {
	plan := changeTestPlan()
	request := buildPhasedDeliveryRequest(plan, strings.Repeat("9", 64))
	markerWriter := &changeWriterSpy{observation: remediation.WriteObservation{Phase: remediation.WritePhaseEnsureDraftPR, Reconciled: true}}
	operation := &changeEnsurePROperation{cfg: ChangeEnsurePRConfig{Writer: markerWriter}}
	if _, err := operation.callReconciler(context.Background(), changeSnapshot{
		PlanSnapshot: changePlanSnapshot{Request: request}, WritePhase: remediation.WritePhaseEnsureCommit,
		ExternalMarker: strings.Repeat("8", 64),
	}); err != nil {
		t.Fatal(err)
	}
	if markerWriter.reconcileCalls != 1 || markerWriter.ensureBranchCalls != 0 || markerWriter.ensureCommitCalls != 0 || markerWriter.ensurePRCalls != 0 {
		t.Fatalf("marker writer calls=%+v", markerWriter)
	}

	for _, phase := range []remediation.WritePhase{
		remediation.WritePhaseEnsureBranch,
		remediation.WritePhaseEnsureCommit,
		remediation.WritePhaseEnsureDraftPR,
	} {
		t.Run(string(phase), func(t *testing.T) {
			writer := &changeWriterSpy{observation: remediation.WriteObservation{Phase: remediation.WritePhaseComplete}}
			operation := &changeEnsurePROperation{cfg: ChangeEnsurePRConfig{Writer: writer}}
			if _, err := operation.callPhaseWriter(context.Background(), changeSnapshot{
				PlanSnapshot: changePlanSnapshot{Request: request}, WritePhase: phase,
			}); err != nil {
				t.Fatal(err)
			}
			if writer.reconcileCalls != 0 || writer.totalCalls() != 1 {
				t.Fatalf("phase=%s calls=%+v", phase, writer)
			}
		})
	}
}

func TestChangeEnsurePRMarkerPassRunsPhaseWriterAfterFreshPreflight(t *testing.T) {
	plan := changeTestPlan()
	changeID := "44444444-4444-4444-8444-444444444444"
	store := &changeTestStore{change: changeSnapshot{
		PlanSnapshot: changePlanSnapshot{Plan: plan}, ChangeRequestID: 31, ChangePublicID: changeID,
		ChangeVersion: 4, WritePhase: remediation.WritePhaseEnsureCommit,
		LogicalOperation: strings.Repeat("9", 64), ExternalMarker: strings.Repeat("8", 64),
	}}
	commit := strings.Repeat("d", 40)
	writer := &changeWriterSpy{observation: remediation.WriteObservation{
		Phase: remediation.WritePhaseEnsureDraftPR, BaseSHA: plan.TargetBaseRevision,
		BranchSHA: commit, CommitSHA: commit, TreeSHA: plan.ExpectedTreeHash,
	}}
	operation := &changeEnsurePROperation{
		cfg:   ChangeEnsurePRConfig{Writer: writer, CurrentPolicyHash: plan.PolicySnapshotHash, Now: func() time.Time { return plan.CreatedAt.Add(time.Minute) }},
		store: store, externalContext: passthroughExternalContext,
	}
	task := changeRequestTask(plan, changeID, remediation.WritePhaseEnsureCommit)
	result := operation.handle(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{
		TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5,
	}})
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil || writer.reconcileCalls != 0 || writer.ensureCommitCalls != 1 || !store.marked {
		t.Fatalf("result=%+v writer=%+v marked=%t", result, writer, store.marked)
	}
}

func TestChangeEnsurePRMarkerPreflightFailureReconcilesAndInvalidates(t *testing.T) {
	plan := changeTestPlan()
	changeID := "44444444-4444-4444-8444-444444444444"
	store := &changeTestStore{change: changeSnapshot{
		PlanSnapshot: changePlanSnapshot{Plan: plan}, ChangeRequestID: 31, ChangePublicID: changeID,
		ChangeVersion: 4, WritePhase: remediation.WritePhaseEnsureCommit,
		LogicalOperation: strings.Repeat("9", 64), ExternalMarker: strings.Repeat("8", 64),
	}, validateErr: fmt.Errorf("%w: Evidence superseded", asyncjob.ErrPolicyViolation)}
	writer := &changeWriterSpy{observation: remediation.WriteObservation{Phase: remediation.WritePhaseEnsureCommit, Reconciled: true}}
	operation := &changeEnsurePROperation{
		cfg:   ChangeEnsurePRConfig{Writer: writer, CurrentPolicyHash: plan.PolicySnapshotHash, Now: func() time.Time { return plan.CreatedAt.Add(time.Minute) }},
		store: store, externalContext: passthroughExternalContext,
	}
	task := changeRequestTask(plan, changeID, remediation.WritePhaseEnsureCommit)
	result := operation.handle(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{
		TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5,
	}})
	if result.Disposition != asyncjob.DispositionRetry || result.Mutate == nil || writer.reconcileCalls != 1 || writer.ensureCommitCalls != 0 {
		t.Fatalf("result=%+v writer=%+v", result, writer)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !store.invalidated || store.safeTerminal {
		t.Fatalf("invalidated=%t safe=%t", store.invalidated, store.safeTerminal)
	}
}

func TestChangeEnsurePRExpiredApprovalAcceptsOnlyExistingCompleteDraftPR(t *testing.T) {
	plan := changeTestPlan()
	changeID := "44444444-4444-4444-8444-444444444444"
	store := &changeTestStore{change: changeSnapshot{
		PlanSnapshot: changePlanSnapshot{Plan: plan}, ChangeRequestID: 31, ChangePublicID: changeID,
		ChangeVersion: 4, WritePhase: remediation.WritePhaseEnsureDraftPR,
		LogicalOperation: strings.Repeat("9", 64), ExternalMarker: strings.Repeat("8", 64),
	}, validateErr: fmt.Errorf("%w: %w", asyncjob.ErrPolicyViolation, errChangeApprovalExpired)}
	commit := strings.Repeat("d", 40)
	writer := &changeWriterSpy{observation: remediation.WriteObservation{
		Phase: remediation.WritePhaseComplete, BaseSHA: plan.TargetBaseRevision,
		BranchSHA: commit, CommitSHA: commit, TreeSHA: plan.ExpectedTreeHash,
		PRNumber: 17, PRURL: "https://github.example/acme/gitops/pull/17", Reconciled: true,
	}}
	operation := &changeEnsurePROperation{
		cfg:   ChangeEnsurePRConfig{Writer: writer, CurrentPolicyHash: plan.PolicySnapshotHash, Now: func() time.Time { return plan.ExpiresAt.Add(time.Minute) }},
		store: store, externalContext: passthroughExternalContext,
	}
	task := changeRequestTask(plan, changeID, remediation.WritePhaseEnsureDraftPR)
	result := operation.handle(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{
		TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5,
	}})
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil || writer.reconcileCalls != 1 || writer.ensurePRCalls != 0 {
		t.Fatalf("result=%+v writer=%+v", result, writer)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !store.applied || store.appliedSnapshot.PreflightRejected || store.invalidated {
		t.Fatalf("applied=%t rejected=%t invalidated=%t", store.applied, store.appliedSnapshot.PreflightRejected, store.invalidated)
	}
}

func TestChangeEnsurePRInvalidatedPlanNeverEnqueuesAnotherWrite(t *testing.T) {
	plan := changeTestPlan()
	plan.Status = remediation.PlanInvalidated
	changeID := "44444444-4444-4444-8444-444444444444"
	store := &changeTestStore{change: changeSnapshot{
		PlanSnapshot: changePlanSnapshot{Plan: plan}, ChangeRequestID: 31, ChangePublicID: changeID,
		ChangeVersion: 4, WritePhase: remediation.WritePhaseEnsureCommit,
		LogicalOperation: strings.Repeat("9", 64), ExternalMarker: strings.Repeat("8", 64),
	}}
	commit := strings.Repeat("d", 40)
	writer := &changeWriterSpy{observation: remediation.WriteObservation{
		Phase: remediation.WritePhaseEnsureDraftPR, BaseSHA: plan.TargetBaseRevision,
		BranchSHA: commit, CommitSHA: commit, TreeSHA: plan.ExpectedTreeHash, Reconciled: true,
	}}
	operation := &changeEnsurePROperation{
		cfg:   ChangeEnsurePRConfig{Writer: writer, Now: func() time.Time { return plan.CreatedAt.Add(time.Minute) }},
		store: store, externalContext: passthroughExternalContext,
	}
	task := changeRequestTask(plan, changeID, remediation.WritePhaseEnsureCommit)
	result := operation.handle(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{
		TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5,
	}})
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil || writer.reconcileCalls != 1 || writer.ensureCommitCalls != 0 {
		t.Fatalf("result=%+v writer=%+v", result, writer)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !store.applied {
		t.Fatal("reconciled fact was not persisted")
	}
	if !store.appliedSnapshot.PreflightRejected {
		t.Fatal("invalidated reconciliation did not carry the stop-writes fence")
	}
}

func TestChangeEnsurePRReconciliationClassification(t *testing.T) {
	tests := []struct {
		name        string
		current     remediation.WritePhase
		observation remediation.WriteObservation
		want        changeReconciliationAction
	}{
		{"advanced", remediation.WritePhaseEnsureCommit, remediation.WriteObservation{Phase: remediation.WritePhaseEnsureDraftPR, Reconciled: true}, changeReconciliationAdvance},
		{"safely absent", remediation.WritePhaseEnsureDraftPR, remediation.WriteObservation{Phase: remediation.WritePhaseEnsureBranch, Reconciled: true}, changeReconciliationAbsent},
		{"present", remediation.WritePhaseEnsureDraftPR, remediation.WriteObservation{Phase: remediation.WritePhaseEnsureCommit, Reconciled: true}, changeReconciliationPending},
		{"ambiguous", remediation.WritePhaseEnsureCommit, remediation.WriteObservation{Phase: remediation.WritePhaseEnsureCommit, Reconciled: true}, changeReconciliationPending},
		{"invalid", remediation.WritePhaseEnsureCommit, remediation.WriteObservation{Phase: remediation.WritePhaseEnsureDraftPR}, changeReconciliationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyChangeReconciliation(test.current, test.observation); got != test.want {
				t.Fatalf("action=%v want=%v", got, test.want)
			}
		})
	}
}

func TestChangeEnsurePRExternalWriteHistoryControlsInvalidationBoundary(t *testing.T) {
	tests := []struct {
		name            string
		externalStarted bool
		safeTerminal    bool
		terminal        bool
		v3Status        string
	}{
		{"no write", false, false, true, "superseded"},
		{"prior observed write", true, false, false, ""},
		{"reconciled absent", true, true, true, "failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyChangeInvalidation(test.externalStarted, test.safeTerminal)
			if got.Terminal != test.terminal || got.V3Status != test.v3Status {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
}

func TestChangeEnsurePRRejectsCompletePlanHashTamperingAndPolicyDrift(t *testing.T) {
	plan, _ := completeChangeTestPlanAndGit(t)
	if err := remediation.ValidateV3Plan(plan); err != nil {
		t.Fatal(err)
	}
	tampered := plan
	tampered.BoundedDiff += "# tampered\n"
	if err := remediation.ValidateV3Plan(tampered); err == nil || !isTerminalChangePreflight(err) {
		t.Fatalf("tampered plan error=%v", err)
	}
	if err := validateCurrentChangePolicy(plan, strings.Repeat("f", 64)); err == nil || !isTerminalChangePreflight(err) {
		t.Fatalf("policy drift error=%v", err)
	}
}

func TestChangeEnsurePRRejectsDiagnosisAndSufficiencyDrift(t *testing.T) {
	policy := agent.GoldenRequiredEnvClaimPolicy()
	incidentID := "11111111-1111-4111-8111-111111111111"
	evidenceIDs := []string{"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"}
	facts := changeDiagnosisFacts(incidentID, 2, evidenceIDs)
	sufficiency, err := agent.EvaluateSufficiency(agent.SufficiencyInput{IncidentID: incidentID, CycleNo: 2, Facts: facts, Policy: policy})
	if err != nil || sufficiency.Outcome != agent.SufficiencyReady {
		t.Fatalf("sufficiency=%+v err=%v", sufficiency, err)
	}
	factIDs := make([]string, 0, len(facts))
	for _, fact := range facts {
		factIDs = append(factIDs, fact.ID)
	}
	diagnosis, err := validateV3Diagnosis(agent.DiagnosisCandidate{
		ClaimType: policy.ClaimType, Summary: "The required environment node is absent from the deployed GitOps revision.",
		Confidence: agent.DiagnosisConfirmed, EvidenceFactIDs: factIDs, RemediationHint: agent.RemediationRestoreRequiredEnv,
	}, investigationSnapshot{IncidentPublicID: incidentID, Task: asyncjob.Task{CycleNo: 2}, Facts: facts}, policy, sufficiency)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentChangeDiagnosis(diagnosis, incidentID, 2, facts, policy); err != nil {
		t.Fatal(err)
	}
	driftedPolicy := policy
	driftedPolicy.Version = policy.Version + "/drifted"
	if err := validateCurrentChangeDiagnosis(diagnosis, incidentID, 2, facts, driftedPolicy); err == nil {
		t.Fatal("current ClaimPolicy drift was accepted")
	}
	if err := validateCurrentChangeDiagnosis(diagnosis, incidentID, 2, facts[:len(facts)-1], policy); err == nil {
		t.Fatal("insufficient current Evidence was accepted")
	}
}

func TestChangeEnsurePRRejectsBaseBlobAndTreeDrift(t *testing.T) {
	plan, facts := completeChangeTestPlanAndGit(t)
	if err := validateExactGitChangePreflight(plan, facts); err != nil {
		t.Fatal(err)
	}
	baseDrift := facts
	baseDrift.BaseRevision = strings.Repeat("f", 40)
	if err := validateExactGitChangePreflight(plan, baseDrift); err == nil {
		t.Fatal("base ref drift was accepted")
	}
	blobDrift := facts
	blobDrift.BaseBlobSHA = strings.Repeat("e", 40)
	if err := validateExactGitChangePreflight(plan, blobDrift); err == nil {
		t.Fatal("base blob drift was accepted")
	}
	treeDrift := plan
	treeDrift.ExpectedTreeHash = strings.Repeat("d", 40)
	if err := validateExactGitChangePreflight(treeDrift, facts); err == nil {
		t.Fatal("expected tree drift was accepted")
	}
}

func TestChangeEnsurePRExactGitPreflightRequiresShortExternalDeadline(t *testing.T) {
	plan, facts := completeChangeTestPlanAndGit(t)
	reader := &changeGitReaderSpy{facts: facts}
	store := &mysqlChangeEnsurePRStore{git: reader}
	err := store.validateGitPreflight(context.Background(), plan)
	if !errors.Is(err, asyncjob.ErrExternalDeadlineMissing) || reader.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, reader.calls)
	}
}

type changeTestStore struct {
	plan            changePlanSnapshot
	change          changeSnapshot
	loadApprovedErr error
	loadChangeErr   error
	validateErr     error
	markErr         error
	created         bool
	marked          bool
	superseded      bool
	invalidated     bool
	safeTerminal    bool
	blocked         bool
	applied         bool
	appliedSnapshot changeSnapshot
	steps           []string
}

func (s *changeTestStore) LoadApprovedPlan(context.Context, asyncjob.Task, time.Time, string) (changePlanSnapshot, error) {
	return s.plan, s.loadApprovedErr
}
func (s *changeTestStore) CreateChangeRequestIn(context.Context, asyncjob.DBTX, asyncjob.Task, changePlanSnapshot) error {
	s.created = true
	return nil
}
func (s *changeTestStore) SupersedeApprovedPlanIn(context.Context, asyncjob.DBTX, asyncjob.Task, string) error {
	s.superseded = true
	return nil
}
func (s *changeTestStore) LoadChange(context.Context, asyncjob.Task) (changeSnapshot, error) {
	return s.change, s.loadChangeErr
}
func (s *changeTestStore) ValidateChangePreflight(context.Context, changeSnapshot, time.Time, string) error {
	s.steps = append(s.steps, "validate")
	return s.validateErr
}
func (s *changeTestStore) MarkWriteIntent(context.Context, asyncjob.Task, changeSnapshot, string) error {
	s.steps = append(s.steps, "mark")
	s.marked = true
	return s.markErr
}
func (s *changeTestStore) ApplyObservationIn(_ context.Context, _ asyncjob.DBTX, _ asyncjob.Task, snapshot changeSnapshot, _ remediation.WriteObservation) error {
	s.applied = true
	s.appliedSnapshot = snapshot
	return nil
}
func (s *changeTestStore) BlockReconciliationIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, string) error {
	s.blocked = true
	return nil
}
func (s *changeTestStore) InvalidateIn(_ context.Context, _ asyncjob.DBTX, _ asyncjob.Task, _ changeSnapshot, _ string, safeTerminal bool) error {
	s.invalidated = true
	s.safeTerminal = safeTerminal
	return nil
}

type changeWriterSpy struct {
	observation       remediation.WriteObservation
	err               error
	reconcileCalls    int
	ensureBranchCalls int
	ensureCommitCalls int
	ensurePRCalls     int
}

type changeGitReaderSpy struct {
	facts remediation.ExactGitRestoreFacts
	err   error
	calls int
}

func (s *changeGitReaderSpy) ReadRestoreFacts(context.Context, remediation.ExactGitRestoreQuery) (remediation.ExactGitRestoreFacts, error) {
	s.calls++
	return s.facts, s.err
}

func passthroughExternalContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	return ctx, func() {}, nil
}

func changeRequestTask(plan remediation.RemediationPlan, changeID string, phase remediation.WritePhase) asyncjob.Task {
	return asyncjob.Task{
		ID: 16, IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo), Queue: asyncjob.QueueDeliver,
		Type: asyncjob.TaskChangeEnsurePR, SubjectType: "change_request", SubjectID: 31,
		Transition: "change.ensure_pr", ExpectedSubjectVersion: 4, PayloadSchemaVersion: changeEnsurePayloadSchema,
		Payload: []byte(`{"plan_id":"` + plan.PublicID + `","change_request_id":"` + changeID + `","write_phase":"` + string(phase) + `"}`),
	}
}

func (s *changeWriterSpy) ReconcileDraftPR(context.Context, remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	s.reconcileCalls++
	return s.observation, s.err
}

func (s *changeWriterSpy) EnsureBranch(context.Context, remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	s.ensureBranchCalls++
	return s.observation, s.err
}

func (s *changeWriterSpy) EnsureCommit(context.Context, remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	s.ensureCommitCalls++
	return s.observation, s.err
}

func (s *changeWriterSpy) EnsureDraftPR(context.Context, remediation.PhasedDeliveryRequest) (remediation.WriteObservation, error) {
	s.ensurePRCalls++
	return s.observation, s.err
}

func (s *changeWriterSpy) totalCalls() int {
	return s.reconcileCalls + s.ensureBranchCalls + s.ensureCommitCalls + s.ensurePRCalls
}

func completeChangeTestPlanAndGit(t *testing.T) (remediation.RemediationPlan, remediation.ExactGitRestoreFacts) {
	t.Helper()
	request := prepareTestRequest()
	currentBlob := changeGitObjectHash("blob", request.CurrentContent)
	baselineBlob := changeGitObjectHash("blob", request.BaselineContent)
	appsTree := changeGitObjectHash("tree", changeRawTree("100644", "demo.yaml", currentBlob))
	rootTree := changeGitObjectHash("tree", changeRawTree("40000", "apps", appsTree))
	facts := remediation.ExactGitRestoreFacts{
		Repository: request.Repository, BaseBranch: request.BaseBranch, TargetPath: request.TargetPath,
		BaseRevision: request.BaseRevision, BaseTreeSHA: rootTree, BaseBlobSHA: currentBlob,
		FileMode: request.FileMode, CurrentContent: append([]byte(nil), request.CurrentContent...),
		BaselineRevision: request.LastKnownGoodRevision, BaselineBlobSHA: baselineBlob,
		BaselineContent: append([]byte(nil), request.BaselineContent...), BaselineIsAncestor: true,
		TreeEntries: []remediation.GitTreeEntry{
			{Path: "apps", Mode: "040000", Type: "tree", ObjectID: appsTree},
			{Path: "apps/demo.yaml", Mode: "100644", Type: "blob", ObjectID: currentBlob},
		},
	}
	patch, err := remediation.RenderRestoreRequiredEnv(request.CurrentContent, request.BaselineContent, request.Target, request.EnvKey)
	if err != nil {
		t.Fatal(err)
	}
	expectedTree, err := remediation.ExpectedGitTreeHash(facts, patch.Content)
	if err != nil {
		t.Fatal(err)
	}
	request.BaseBlobSHA = currentBlob
	request.ExpectedTreeHash = expectedTree
	plan, err := remediation.CompileRestoreRequiredEnv(request)
	if err != nil {
		t.Fatal(err)
	}
	plan.ID = 21
	plan.Status = remediation.PlanApproved
	plan.RowVersion = 3
	plan.UpdatedAt = plan.CreatedAt.Add(time.Minute)
	return plan, facts
}

func changeGitObjectHash(kind string, content []byte) string {
	payload := append([]byte(fmt.Sprintf("%s %d\x00", kind, len(content))), content...)
	sum := sha1.Sum(payload) // #nosec G401 -- fixture computes Git SHA-1 object identities.
	return hex.EncodeToString(sum[:])
}

func changeRawTree(mode, name, objectID string) []byte {
	raw, _ := hex.DecodeString(objectID)
	return append([]byte(mode+" "+name+"\x00"), raw...)
}

func changeDiagnosisFacts(incidentPublicID string, cycleNo uint64, evidenceIDs []string) []agent.EvidenceFact {
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
			IncidentID: incidentPublicID, CycleNo: cycleNo, Type: factType, SourceSystem: source,
			CollectionPath: collection, CorroborationGroup: fmt.Sprintf("group-%02d", index+1),
			Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
			ClaimUse: "allowed", CollectionStatus: agent.CollectionAvailable, Direct: index == 0,
		})
	}
	return facts
}

func changeTestPlan() remediation.RemediationPlan {
	now := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	post := []byte("apiVersion: apps/v1\nkind: Deployment\n")
	return remediation.RemediationPlan{
		ID: 21, PublicID: "22222222-2222-4222-8222-222222222222", IncidentID: 11,
		IncidentPublicID: "11111111-1111-4111-8111-111111111111", CycleNo: 2,
		PlanVersion: 1, RowVersion: 3, OperationType: remediation.OperationRestoreRequiredEnv,
		TargetRepository: "acme/gitops", TargetBaseBranch: "main", TargetBaseRevision: strings.Repeat("a", 40),
		TargetPath: "apps/demo.yaml", BaseBlobSHA: strings.Repeat("b", 40),
		ExpectedBeforeHash: strings.Repeat("1", 64), ExpectedPostImageHash: remediation.HashBytes(post),
		ExpectedTreeHash: strings.Repeat("c", 40), PostImage: post,
		CanonicalPlanHash: strings.Repeat("2", 64), PolicySnapshotHash: strings.Repeat("3", 64),
		EvidenceBindings: []remediation.EvidenceBinding{{ID: "33333333-3333-4333-8333-333333333333", ContentHash: strings.Repeat("4", 64)}},
		CreatedAt:        now, ExpiresAt: now.Add(30 * time.Minute),
	}
}
