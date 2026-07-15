package remediationservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"server-web/internal/remediation"
)

type WorkerConfig struct {
	Enabled      bool
	Owner        string
	PollInterval time.Duration
	Lease        time.Duration
	Repository   remediation.Repository
	GitHub       remediation.GitHubWriter
	BaseBranches map[string]string
	Observer     Observer
}

type Worker struct{ cfg WorkerConfig }

func NewWorker(cfg WorkerConfig) (*Worker, error) {
	if !cfg.Enabled {
		return &Worker{cfg: cfg}, nil
	}
	if strings.TrimSpace(cfg.Owner) == "" || cfg.PollInterval <= 0 || cfg.Lease <= cfg.PollInterval || cfg.Repository == nil || cfg.GitHub == nil {
		return nil, remediation.ErrInvalidArgument
	}
	return &Worker{cfg: cfg}, nil
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || !w.cfg.Enabled {
		return
	}
	go func() {
		ticker := time.NewTicker(w.cfg.PollInterval)
		defer ticker.Stop()
		for {
			if _, err := w.RunOnce(ctx); err != nil && !errors.Is(err, remediation.ErrNotFound) && w.cfg.Observer != nil {
				w.cfg.Observer.ObserveChangeRequestDelivery("failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *Worker) RunOnce(ctx context.Context) (*remediation.ChangeRequest, error) {
	if w == nil || !w.cfg.Enabled {
		return nil, remediation.ErrForbidden
	}
	delivery, plan, err := w.cfg.Repository.ClaimDelivery(ctx, w.cfg.Owner, time.Now().UTC(), w.cfg.Lease)
	if err != nil {
		return nil, err
	}
	fail := func(code string, cause error) (*remediation.ChangeRequest, error) {
		_ = w.cfg.Repository.ReleaseDelivery(context.WithoutCancel(ctx), delivery.ID, delivery.RowVersion, w.cfg.Owner, code)
		return delivery, cause
	}
	planHash, err := remediation.ComputePlanHash(*plan)
	if err != nil || planHash != plan.PlanHash {
		return fail("approved_plan_hash_drift", remediation.ErrDrift)
	}
	base, err := w.cfg.GitHub.ReadBaseFile(ctx, plan.TargetRepository, plan.TargetBaseRevision, plan.TargetPath)
	if err != nil {
		return fail("base_read_failed", err)
	}
	patch, err := remediation.RenderPatch(base, plan.OperationType, plan.Parameters)
	if err != nil {
		return fail("patch_rebuild_failed", err)
	}
	if patch.BeforeHash != plan.ExpectedBeforeHash || patch.PatchHash != plan.ProposedPatchHash {
		return fail("approved_content_drift", remediation.ErrDrift)
	}
	baseBranch, ok := w.cfg.BaseBranches[plan.TargetRepository]
	if !ok || strings.TrimSpace(baseBranch) == "" {
		return fail("base_branch_denied", remediation.ErrForbidden)
	}
	marker := "<!-- cloudops-remediation:" + plan.PublicID + ":" + plan.ProposedPatchHash + " -->"
	body := fmt.Sprintf("Incident ID: %s\n\nPlan ID: %s\n\nPatch Summary: %s\n\nRisk: %s\n\nRollback Plan: %s\n\n%s", plan.IncidentPublicID, plan.PublicID, plan.PatchSummary, plan.RiskLevel, plan.RollbackPlan, marker)
	result, err := w.cfg.GitHub.DeliverDraftPR(ctx, remediation.DeliveryRequest{Repository: plan.TargetRepository, BaseRevision: plan.TargetBaseRevision, BaseBranch: baseBranch, Path: plan.TargetPath, Content: patch.Content, Branch: delivery.HeadBranch, CommitTitle: "cloudops: approved remediation " + plan.PublicID, PRTitle: "[Draft] CloudOps remediation for incident " + plan.IncidentPublicID, PRBody: body, Marker: marker})
	if err != nil {
		return fail("github_delivery_failed", err)
	}
	if err := w.cfg.Repository.MarkPRCreated(ctx, delivery.ID, delivery.RowVersion, w.cfg.Owner, result.CommitSHA, result.PRNumber, result.PRURL); err != nil {
		return delivery, err
	}
	delivery.CommitSHA, delivery.PRNumber, delivery.PRURL, delivery.Status, delivery.RowVersion = result.CommitSHA, result.PRNumber, result.PRURL, remediation.DeliveryPRCreated, delivery.RowVersion+1
	ci, err := w.cfg.GitHub.ReadCI(ctx, delivery.Repository, result.CommitSHA)
	if err == nil {
		if updateErr := w.cfg.Repository.UpdateCI(ctx, delivery.ID, delivery.RowVersion, ci); updateErr == nil {
			delivery.CIStatus, delivery.RowVersion = ci, delivery.RowVersion+1
		}
	}
	if w.cfg.Observer != nil {
		w.cfg.Observer.ObserveChangeRequestDelivery("pr_created")
	}
	return delivery, nil
}
