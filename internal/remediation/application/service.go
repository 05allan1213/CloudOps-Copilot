// Package application implements the repository-backed remediation API surface.
package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

type Config struct {
	Enabled    bool
	Repository remediation.Repository
	Now        func() time.Time
}

type Service struct {
	cfg Config
}

func New(cfg Config) (*Service, error) {
	if cfg.Enabled && cfg.Repository == nil {
		return nil, fmt.Errorf("%w: remediation repository", remediation.ErrInvalidArgument)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) Enabled() bool { return s != nil && s.cfg.Enabled }

func (s *Service) List(ctx context.Context, filter remediation.ListFilter) (remediation.Page, error) {
	if !s.Enabled() {
		return remediation.Page{}, remediation.ErrForbidden
	}
	return s.cfg.Repository.ListPlans(ctx, filter)
}

func (s *Service) Get(ctx context.Context, publicID string) (*remediation.RemediationPlan, error) {
	if !s.Enabled() {
		return nil, remediation.ErrForbidden
	}
	return s.cfg.Repository.GetPlan(ctx, publicID)
}

func (s *Service) GetApproval(ctx context.Context, publicID string) (*remediation.Approval, error) {
	if !s.Enabled() {
		return nil, remediation.ErrForbidden
	}
	return s.cfg.Repository.GetApproval(ctx, publicID)
}

func (s *Service) Approve(ctx context.Context, publicID, actor, role, planHash, patchHash string, expectedVersion uint64) (*remediation.RemediationPlan, *remediation.ChangeRequest, error) {
	if !s.Enabled() || role != "admin" || strings.TrimSpace(actor) == "" {
		return nil, nil, remediation.ErrForbidden
	}
	plan, err := s.cfg.Repository.GetPlan(ctx, publicID)
	if err != nil {
		return nil, nil, err
	}
	if plan.PlanHash != planHash || plan.ProposedPatchHash != patchHash {
		return nil, nil, remediation.ErrApprovalMismatch
	}
	branch := "cloudops/incident-" + plan.IncidentPublicID + "/remediation-" + plan.PublicID
	idempotency, _ := remediation.CanonicalHash(struct{ PlanID, PlanHash, PatchHash string }{plan.PublicID, plan.PlanHash, plan.ProposedPatchHash})
	delivery := &remediation.ChangeRequest{PublicID: uuid.NewString(), Repository: plan.TargetRepository, BaseRevision: plan.TargetBaseRevision, HeadBranch: branch, Status: remediation.DeliveryPending, CIStatus: remediation.CIPending, IdempotencyKey: idempotency, RowVersion: 1, CreatedAt: s.cfg.Now().UTC(), UpdatedAt: s.cfg.Now().UTC()}
	approval := remediation.Approval{PublicID: uuid.NewString(), Decision: remediation.DecisionApproved, Actor: actor, ApprovedPlanHash: planHash, ApprovedPatchHash: patchHash, CreatedAt: s.cfg.Now().UTC()}
	return s.cfg.Repository.ApprovePlan(ctx, publicID, expectedVersion, approval, delivery)
}

func (s *Service) Reject(ctx context.Context, publicID, actor, role, planHash, patchHash string, expectedVersion uint64) (*remediation.RemediationPlan, error) {
	if !s.Enabled() || role != "admin" || strings.TrimSpace(actor) == "" {
		return nil, remediation.ErrForbidden
	}
	approval := remediation.Approval{PublicID: uuid.NewString(), Decision: remediation.DecisionRejected, Actor: actor, ApprovedPlanHash: planHash, ApprovedPatchHash: patchHash, CreatedAt: s.cfg.Now().UTC()}
	return s.cfg.Repository.RejectPlan(ctx, publicID, expectedVersion, approval)
}
