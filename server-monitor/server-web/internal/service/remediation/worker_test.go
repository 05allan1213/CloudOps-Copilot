package remediationservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"server-web/internal/remediation"
)

type workerRepository struct {
	delivery *remediation.ChangeRequest
	plan     *remediation.RemediationPlan
	released string
	marked   int
}

func (*workerRepository) CreatePlan(context.Context, *remediation.RemediationPlan) error { return nil }
func (*workerRepository) GetPlan(context.Context, string) (*remediation.RemediationPlan, error) {
	return nil, remediation.ErrNotFound
}
func (*workerRepository) GetApproval(context.Context, string) (*remediation.Approval, error) {
	return nil, remediation.ErrNotFound
}
func (*workerRepository) ListPlans(context.Context, remediation.ListFilter) (remediation.Page, error) {
	return remediation.Page{}, nil
}
func (*workerRepository) ApprovePlan(context.Context, string, uint64, remediation.Approval, *remediation.ChangeRequest) (*remediation.RemediationPlan, *remediation.ChangeRequest, error) {
	return nil, nil, nil
}
func (*workerRepository) RejectPlan(context.Context, string, uint64, remediation.Approval) (*remediation.RemediationPlan, error) {
	return nil, nil
}
func (*workerRepository) CreateDelivery(context.Context, *remediation.ChangeRequest) error {
	return nil
}
func (r *workerRepository) ClaimDelivery(context.Context, string, time.Time, time.Duration) (*remediation.ChangeRequest, *remediation.RemediationPlan, error) {
	return r.delivery, r.plan, nil
}
func (r *workerRepository) ReleaseDelivery(_ context.Context, _, _ uint64, _, failure string) error {
	r.released = failure
	return nil
}
func (r *workerRepository) MarkPRCreated(context.Context, uint64, uint64, string, string, int64, string) error {
	r.marked++
	return nil
}
func (*workerRepository) UpdateCI(context.Context, uint64, uint64, remediation.CIStatus) error {
	return nil
}

type workerGitHub struct {
	base         []byte
	deliveryCall int
}

func (g *workerGitHub) ReadBaseFile(context.Context, string, string, string) ([]byte, error) {
	return g.base, nil
}
func (g *workerGitHub) DeliverDraftPR(context.Context, remediation.DeliveryRequest) (remediation.DeliveryResult, error) {
	g.deliveryCall++
	return remediation.DeliveryResult{CommitSHA: strings.Repeat("c", 40), PRNumber: 2, PRURL: "https://github.example/pr/2"}, nil
}
func (*workerGitHub) ReadCI(context.Context, string, string) (remediation.CIStatus, error) {
	return remediation.CIPending, nil
}

func TestWorkerRejectsBaseAndPatchDriftBeforeWrite(t *testing.T) {
	replicas := 4
	base := []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: prod\nspec:\n  replicas: 3\n")
	params := remediation.Parameters{Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api"}, ProposedValue: remediation.ProposedValue{Replicas: &replicas}}
	patch, err := remediation.RenderPatch(base, remediation.OperationSetReplicas, params)
	if err != nil {
		t.Fatal(err)
	}
	plan := &remediation.RemediationPlan{PublicID: "22222222-2222-4222-8222-222222222222", IncidentPublicID: "11111111-1111-4111-8111-111111111111", TargetRepository: "acme/gitops", TargetBaseRevision: strings.Repeat("a", 40), TargetPath: "apps/api.yaml", OperationType: remediation.OperationSetReplicas, Parameters: params, ExpectedBeforeHash: patch.BeforeHash, ProposedPatchHash: strings.Repeat("f", 64)}
	plan.PlanHash, _ = remediation.ComputePlanHash(*plan)
	delivery := &remediation.ChangeRequest{ID: 1, RowVersion: 2, Repository: "acme/gitops", HeadBranch: "cloudops/incident-11111111-1111-4111-8111-111111111111/remediation-22222222-2222-4222-8222-222222222222"}
	repo := &workerRepository{delivery: delivery, plan: plan}
	github := &workerGitHub{base: base}
	worker, err := NewWorker(WorkerConfig{Enabled: true, Owner: "worker", PollInterval: time.Second, Lease: 10 * time.Second, Repository: repo, GitHub: github, BaseBranches: map[string]string{"acme/gitops": "main"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = worker.RunOnce(context.Background())
	if !errors.Is(err, remediation.ErrDrift) || github.deliveryCall != 0 || repo.released != "approved_content_drift" || repo.marked != 0 {
		t.Fatalf("drift did not fail before write err=%v calls=%d release=%s", err, github.deliveryCall, repo.released)
	}
}

func TestNonAdminCannotApprove(t *testing.T) {
	service := &Service{cfg: Config{Enabled: true}}
	if _, _, err := service.Approve(context.Background(), "plan", "user", "viewer", strings.Repeat("a", 64), strings.Repeat("b", 64), 1); !errors.Is(err, remediation.ErrForbidden) {
		t.Fatalf("non-admin approval err=%v", err)
	}
}
