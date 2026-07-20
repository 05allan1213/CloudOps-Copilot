package taskhandler

import (
	"context"
	"strings"
	"testing"
	"time"

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
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "change_preflight_rejected" || store.created {
		t.Fatalf("result=%+v created=%t", result, store.created)
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
			if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "change_preflight_rejected" || !store.marked {
				t.Fatalf("result=%+v marked=%t", result, store.marked)
			}
		})
	}
}

type changeTestStore struct {
	plan            changePlanSnapshot
	change          changeSnapshot
	loadApprovedErr error
	loadChangeErr   error
	markErr         error
	created         bool
	marked          bool
}

func (s *changeTestStore) LoadApprovedPlan(context.Context, asyncjob.Task, time.Time, string) (changePlanSnapshot, error) {
	return s.plan, s.loadApprovedErr
}
func (s *changeTestStore) CreateChangeRequestIn(context.Context, asyncjob.DBTX, asyncjob.Task, changePlanSnapshot) error {
	s.created = true
	return nil
}
func (s *changeTestStore) LoadChange(context.Context, asyncjob.Task, time.Time, string) (changeSnapshot, error) {
	return s.change, s.loadChangeErr
}
func (s *changeTestStore) MarkWriteIntent(context.Context, asyncjob.Task, changeSnapshot, string) error {
	s.marked = true
	return s.markErr
}
func (*changeTestStore) ApplyObservationIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, remediation.WriteObservation) error {
	return nil
}
func (*changeTestStore) InvalidateIn(context.Context, asyncjob.DBTX, asyncjob.Task, changeSnapshot, string) error {
	return nil
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
