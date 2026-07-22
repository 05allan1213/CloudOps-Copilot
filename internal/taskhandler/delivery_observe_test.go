package taskhandler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestDeliveryObserveExactIdentityFlowCreatesVerificationOnlyAfterRollout(t *testing.T) {
	now := time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC)
	snapshot := deliveryTestSnapshot(now)

	prOpen := DeliveryObservation{Kind: DeliveryObservePullRequest, PullRequest: &DeliveryPullRequestObservation{
		State: "open", BaseSHA: snapshot.BaseRevision, HeadSHA: snapshot.Projection.HeadCommitSHA,
		HeadTreeSHA: snapshot.ExpectedTreeSHA, HeadPostImageHash: snapshot.ExpectedPostImageHash,
	}}
	first, err := evaluateDeliveryObservation(snapshot, prOpen, now, 5*time.Second)
	if err != nil || first.FailureCode != "" || first.Projection.V3Status != "pr_open" || first.Projection.Status != "ci_pending" || first.VerificationPlan != nil {
		t.Fatalf("pull request outcome=%+v err=%v", first, err)
	}

	snapshot.Projection = first.Projection
	ci := DeliveryObservation{Kind: DeliveryObserveCI, CI: &DeliveryCIObservation{
		HeadSHA: snapshot.Projection.HeadCommitSHA, HeadTreeSHA: snapshot.ExpectedTreeSHA,
		HeadPostImageHash: snapshot.ExpectedPostImageHash, Status: "completed", Conclusion: "success", RequiredChecksValid: true,
		RequiredCheckName: "render-and-validate", ProducerAppID: 15368, WorkflowID: 41, WorkflowPath: ".github/workflows/render.yaml",
	}}
	second, err := evaluateDeliveryObservation(snapshot, ci, now.Add(5*time.Second), 5*time.Second)
	if err != nil || second.FailureCode != "" || second.Projection.Status != "merge_pending" || second.Projection.CIStatus != "passing" || second.VerificationPlan != nil {
		t.Fatalf("CI outcome=%+v err=%v", second, err)
	}

	snapshot.Projection = second.Projection
	mergedSHA := strings.Repeat("f", 40)
	merged := DeliveryObservation{Kind: DeliveryObservePullRequest, PullRequest: &DeliveryPullRequestObservation{
		State: "closed", Merged: true, BaseSHA: snapshot.BaseRevision, HeadSHA: snapshot.Projection.HeadCommitSHA,
		HeadTreeSHA: snapshot.ExpectedTreeSHA, HeadPostImageHash: snapshot.ExpectedPostImageHash,
		MergeCommitSHA: mergedSHA, MergedTreeSHA: snapshot.ExpectedTreeSHA, MergedPostImageHash: snapshot.ExpectedPostImageHash,
		HumanMerged: true, MergedBy: "incident-operator", MergedByType: "User", MergeMethod: "squash",
	}}
	third, err := evaluateDeliveryObservation(snapshot, merged, now.Add(10*time.Second), 5*time.Second)
	if err != nil || third.FailureCode != "" || third.Projection.V3Status != "merged" || third.Projection.TargetRevision != mergedSHA || third.VerificationPlan != nil {
		t.Fatalf("merge outcome=%+v err=%v", third, err)
	}

	snapshot.Projection = third.Projection
	argo := DeliveryObservation{Kind: DeliveryObserveArgo, Argo: &DeliveryArgoObservation{
		Application: snapshot.ArgoApplication, Project: snapshot.ArgoProject, Repository: snapshot.ArgoRepository,
		Path: snapshot.ArgoPath, TargetRevision: snapshot.BaseBranch, SyncRevision: mergedSHA,
		SyncResultRevision: mergedSHA, SyncStatus: "Synced", OperationPhase: "Succeeded", HealthStatus: "Healthy",
		ResourceHealth: json.RawMessage(`[{"kind":"Deployment","health":"Healthy"}]`),
	}}
	fourth, err := evaluateDeliveryObservation(snapshot, argo, now.Add(15*time.Second), 5*time.Second)
	if err != nil || fourth.FailureCode != "" || fourth.Projection.V3Status != "rolling_out" || fourth.VerificationPlan != nil {
		t.Fatalf("Argo outcome=%+v err=%v", fourth, err)
	}

	snapshot.Projection = fourth.Projection
	rollout := DeliveryObservation{Kind: DeliveryObserveRollout, Rollout: &DeliveryRolloutObservation{
		Cluster: snapshot.Cluster, Environment: snapshot.Environment, Namespace: snapshot.Namespace,
		WorkloadKind: snapshot.WorkloadKind, WorkloadName: snapshot.WorkloadName, Container: snapshot.Container,
		SourceRevision: snapshot.SourceRevision, ImageDigest: snapshot.ImageDigest, GitOpsRevision: mergedSHA,
		Generation: 9, ObservedGeneration: 9, RolloutRevision: "9", DesiredReplicas: 2, UpdatedReplicas: 2,
		ReadyReplicas: 2, AvailableReplicas: 2, PodsReady: 2, PodsTotal: 2, Progressing: true, Available: true,
	}}
	fifth, err := evaluateDeliveryObservation(snapshot, rollout, now.Add(20*time.Second), 5*time.Second)
	if err != nil || fifth.FailureCode != "" || fifth.Projection.V3Status != "delivered" || fifth.Requeue || fifth.VerificationPlan == nil {
		t.Fatalf("rollout outcome=%+v err=%v", fifth, err)
	}
	plan := fifth.VerificationPlan
	if plan.ProfileID != verification.GoldenRequiredEnvProfileID || plan.TriggerType != "post_delivery" ||
		plan.SourceRevision != snapshot.SourceRevision || plan.ImageDigest != snapshot.ImageDigest || plan.GitOpsRevision != mergedSHA ||
		plan.SourceRevision == plan.GitOpsRevision || plan.ImageDigest == plan.GitOpsRevision {
		t.Fatalf("verification identity was collapsed: %+v", plan)
	}
}

func TestDeliveryObserveFailsWhenArgoSupersedesMergedRevision(t *testing.T) {
	now := time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC)
	snapshot := deliveryTestSnapshot(now)
	snapshot.Projection.V3Status = "syncing"
	snapshot.Projection.Status = "syncing"
	snapshot.Projection.MergedCommitSHA = strings.Repeat("f", 40)
	snapshot.Projection.TargetRevision = snapshot.Projection.MergedCommitSHA
	observation := DeliveryObservation{Kind: DeliveryObserveArgo, Argo: &DeliveryArgoObservation{
		Application: snapshot.ArgoApplication, Project: snapshot.ArgoProject, Repository: snapshot.ArgoRepository,
		Path: snapshot.ArgoPath, TargetRevision: snapshot.BaseBranch, SyncRevision: strings.Repeat("e", 40),
		SyncResultRevision: strings.Repeat("e", 40), SyncStatus: "Synced", OperationPhase: "Succeeded",
	}}
	outcome, err := evaluateDeliveryObservation(snapshot, observation, now, 5*time.Second)
	if err != nil || outcome.FailureCode != "revision_superseded" || outcome.Projection.V3Status != "failed" || outcome.VerificationPlan != nil || outcome.Requeue {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

func TestDeliveryObserveRejectsInvalidLeaseBeforeReads(t *testing.T) {
	store := &deliveryTestStore{}
	observer := &deliveryTestObserver{}
	operation := &deliveryObserveOperation{cfg: DeliveryObserveConfig{Store: store, Observer: observer, PollInterval: 5 * time.Second, Now: time.Now}}
	task := asyncjob.Task{
		ID: 91, IncidentID: 11, CycleNo: 2, Queue: asyncjob.QueueObserve, Type: asyncjob.TaskDeliveryObserve,
		SubjectType: "change_request", SubjectID: 21, Transition: "delivery.observe", ExpectedSubjectVersion: 4,
		PayloadSchemaVersion: deliveryObservePayloadSchema, Payload: json.RawMessage(`{"change_request_id":"22222222-2222-4222-8222-222222222222","phase":"observe"}`),
	}
	result := operation.handle(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: 3, Attempt: 1, MaxAttempts: 8}})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "invalid_task_subject" || store.loads != 0 || observer.calls != 0 {
		t.Fatalf("result=%+v loads=%d calls=%d", result, store.loads, observer.calls)
	}
}

func TestLegacyDeliveryObservationRequiresFullRepositoryPRBaseHeadBranchAndMergeIdentity(t *testing.T) {
	base := LegacyDeliveryObserveSnapshot{Repository: "acme/app", PRNumber: 7,
		BaseRevision: strings.Repeat("a", 40), HeadCommitSHA: strings.Repeat("b", 40), HeadBranch: "feature/fix",
		SourcePRState: "open", PRURL: "https://github.com/acme/app/pull/7"}
	observation := DeliveryPullRequestObservation{Repository: base.Repository, PullRequest: base.PRNumber,
		State: "open", BaseSHA: base.BaseRevision, HeadSHA: base.HeadCommitSHA, HeadBranch: base.HeadBranch, URL: base.PRURL}
	if !validateLegacyPullRequestObservation(base, observation, "open") {
		t.Fatal("complete legacy pull request observation was rejected")
	}
	mutations := []func(*DeliveryPullRequestObservation){
		func(value *DeliveryPullRequestObservation) { value.Repository = "acme/other" },
		func(value *DeliveryPullRequestObservation) { value.PullRequest++ },
		func(value *DeliveryPullRequestObservation) { value.BaseSHA = strings.Repeat("c", 40) },
		func(value *DeliveryPullRequestObservation) { value.HeadSHA = strings.Repeat("d", 40) },
		func(value *DeliveryPullRequestObservation) { value.HeadBranch = "feature/other" },
		func(value *DeliveryPullRequestObservation) { value.URL = "https://github.com/acme/app/pull/8" },
	}
	for index, mutate := range mutations {
		changed := observation
		mutate(&changed)
		if validateLegacyPullRequestObservation(base, changed, strings.ToLower(changed.State)) {
			t.Fatalf("legacy identity mutation %d was accepted: %+v", index, changed)
		}
	}
	merged := observation
	merged.State, merged.Merged, merged.MergeCommitSHA = "merged", true, strings.Repeat("e", 40)
	base.SourcePRState = "merged"
	base.SourceMergedSHA = merged.MergeCommitSHA
	if !validateLegacyPullRequestObservation(base, merged, "merged") {
		t.Fatal("complete merged pull request identity was rejected")
	}
	merged.MergeCommitSHA = strings.Repeat("f", 40)
	if validateLegacyPullRequestObservation(base, merged, "merged") {
		t.Fatal("mismatched merge identity was accepted")
	}
}

func TestDeliveryObserveRejectsSupersededApprovalBeforeProviderRead(t *testing.T) {
	store := &deliveryTestStore{loadErr: errApprovedEvidenceSuperseded}
	observer := &deliveryTestObserver{}
	operation := &deliveryObserveOperation{cfg: DeliveryObserveConfig{Store: store, Observer: observer, PollInterval: 5 * time.Second, Now: time.Now}}
	task := asyncjob.Task{
		ID: 92, IncidentID: 11, CycleNo: 2, Queue: asyncjob.QueueObserve, Type: asyncjob.TaskDeliveryObserve,
		SubjectType: "change_request", SubjectID: 21, Transition: "delivery.observe", ExpectedSubjectVersion: 4,
		PayloadSchemaVersion: deliveryObservePayloadSchema, Payload: json.RawMessage(`{"change_request_id":"22222222-2222-4222-8222-222222222222","phase":"observe"}`),
	}
	result := operation.handle(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 8}})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "delivery_preflight_rejected" || store.loads != 1 || observer.calls != 0 {
		t.Fatalf("result=%+v loads=%d calls=%d", result, store.loads, observer.calls)
	}
}

func deliveryTestSnapshot(now time.Time) DeliveryObserveSnapshot {
	deadline := now.Add(20 * time.Minute)
	return DeliveryObserveSnapshot{
		ChangeRequestID: 21, ChangeRequestPublicID: "22222222-2222-4222-8222-222222222222",
		IncidentID: 11, IncidentPublicID: "11111111-1111-4111-8111-111111111111", IncidentFingerprint: "DemoRequiredEnvMissing",
		IncidentVersion: 8, IncidentStatus: "delivering", CycleNo: 2, PlanID: 31, PlanPublicID: "33333333-3333-4333-8333-333333333333",
		PlanVersion: 1, PlanStatus: "approved", PlanV3Status: "consumed", Decision: "approved", DecisionPlanVersion: 1,
		Repository: "acme/gitops", BaseBranch: "main", TargetPath: "apps/demo/deployment.yaml", PRNumber: 42,
		BaseRevision: strings.Repeat("a", 40), LastKnownGoodSHA: strings.Repeat("9", 40),
		ExpectedPostImageHash: strings.Repeat("d", 64), ExpectedTreeSHA: strings.Repeat("c", 40),
		ServiceName: "cloudops-demo", Cluster: "kind-cloudops", Environment: "demo", Namespace: "cloudops-demo",
		WorkloadKind: "Deployment", WorkloadName: "cloudops-demo-workload", Container: "cloudops-demo", ExpectedReplicas: 2,
		AlertNames: []string{"DemoRequiredEnvMissing"}, SourceRevision: strings.Repeat("1", 40),
		ImageDigest: "sha256:" + strings.Repeat("2", 64), BaselineGitOpsSHA: strings.Repeat("9", 40),
		ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo", ArgoRepository: "acme/gitops", ArgoPath: "apps/demo",
		Projection: DeliveryProjection{V3Status: "pr_open", Status: "pr_created", CIStatus: "pending", HeadCommitSHA: strings.Repeat("b", 40), DeliveryDeadlineAt: &deadline},
		RowVersion: 4, Now: now,
	}
}

type deliveryTestStore struct {
	loads   int
	loadErr error
}

func (s *deliveryTestStore) Load(context.Context, asyncjob.Task) (DeliveryObserveSnapshot, error) {
	s.loads++
	return DeliveryObserveSnapshot{}, s.loadErr
}

func (*deliveryTestStore) PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, DeliveryObserveSnapshot, DeliveryObserveOutcome) error {
	return nil
}

type deliveryTestObserver struct{ calls int }

func (o *deliveryTestObserver) Observe(context.Context, DeliveryObserveRequest) (DeliveryObservation, error) {
	o.calls++
	return DeliveryObservation{}, nil
}
