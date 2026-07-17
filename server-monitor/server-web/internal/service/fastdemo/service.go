// Package fastdemo implements the explicitly enabled disposable V2 demo path.
package fastdemo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"server-web/internal/agent"
	"server-web/internal/copilot/action"
	"server-web/internal/incident"
	"server-web/internal/infra/incidentmysql"
	"server-web/internal/remediation"
	"server-web/internal/verification"
)

type Config struct {
	Revision         string
	Cluster          string
	Environment      string
	Namespace        string
	Workload         string
	RecoveryReplicas int
	Executor         action.K8sExecutor
	Rollout          verification.RolloutReader
	Incidents        *incidentmysql.Store
	Remediations     *incidentmysql.RemediationRepository
	Verifications    *incidentmysql.VerificationRepository
	Now              func() time.Time
}

type Service struct{ cfg Config }

func New(cfg Config) (*Service, error) {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if len(cfg.Revision) != 40 || cfg.Cluster == "" || cfg.Environment == "" || cfg.Namespace == "" || cfg.Workload == "" || cfg.RecoveryReplicas < 1 || cfg.Executor == nil || cfg.Rollout == nil || cfg.Incidents == nil || cfg.Remediations == nil || cfg.Verifications == nil {
		return nil, fmt.Errorf("fast demo: invalid configuration")
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) Enabled() bool             { return s != nil }
func (s *Service) DeliveryEnabled() bool     { return s != nil }
func (s *Service) VerificationEnabled() bool { return s != nil }

func (s *Service) CreatePlan(ctx context.Context, incidentID string) (*remediation.RemediationPlan, error) {
	item, err := s.cfg.Incidents.GetByPublicID(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	if item.Status != incident.StatusDiagnosisCompleted || item.Namespace != s.cfg.Namespace || item.TargetName != s.cfg.Workload {
		return nil, remediation.ErrInvalidTransition
	}
	existing, err := s.cfg.Remediations.ListPlans(ctx, remediation.ListFilter{IncidentPublicID: incidentID, Page: 1, PageSize: 1})
	if err == nil && len(existing.Items) > 0 {
		return &existing.Items[0], nil
	}
	runs, err := s.cfg.Incidents.ListRunsByIncident(ctx, incidentID, 1, 20)
	if err != nil {
		return nil, err
	}
	var completed *agent.Run
	for index := range runs.Items {
		if runs.Items[index].Status == agent.RunCompleted {
			completed = &runs.Items[index]
			break
		}
	}
	if completed == nil {
		return nil, fmt.Errorf("fast demo: completed AgentRun required")
	}
	evidence, err := s.cfg.Incidents.ListEvidence(ctx, completed.PublicID, 20)
	if err != nil {
		return nil, err
	}
	evidenceIDs := make([]string, 0, len(evidence))
	for _, record := range evidence {
		if record.Valid && !record.Truncated {
			evidenceIDs = append(evidenceIDs, record.PublicID)
		}
	}
	if len(evidenceIDs) == 0 {
		return nil, fmt.Errorf("fast demo: valid persisted Evidence required")
	}
	observation, err := s.cfg.Rollout.ObserveDeployment(ctx, s.cfg.Cluster, s.cfg.Namespace, s.cfg.Workload)
	if err != nil {
		return nil, err
	}
	replicas := s.cfg.RecoveryReplicas
	now := s.cfg.Now()
	plan := &remediation.RemediationPlan{
		PublicID: uuid.NewString(), IncidentID: item.ID, IncidentPublicID: item.PublicID,
		PlanVersion: 1, Status: remediation.PlanAwaitingApproval,
		OperationType:    remediation.OperationSetReplicas,
		TargetRepository: "controlled-direct/cloudops-demo", TargetBaseRevision: strings.ToLower(s.cfg.Revision),
		TargetPath: s.cfg.Namespace + "/Deployment/" + s.cfg.Workload,
		Parameters: remediation.Parameters{
			Target:        remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: s.cfg.Namespace, Name: s.cfg.Workload},
			ProposedValue: remediation.ProposedValue{Replicas: &replicas},
		},
		EvidenceReferences: evidenceIDs, RiskLevel: remediation.RiskLow,
		PolicySnapshotHash: hashValue(map[string]any{"mode": "controlled_direct", "namespace": s.cfg.Namespace, "workload": s.cfg.Workload, "max_replicas": s.cfg.RecoveryReplicas}),
		ExpectedBeforeHash: hashValue(map[string]any{"replicas": observation.DesiredReplicas, "generation": observation.Generation}),
		ProposedPatchHash:  hashValue(map[string]any{"replicas": replicas}),
		PatchSummary:       fmt.Sprintf("Restore %s/Deployment/%s to %d replicas using the controlled direct demo executor.", s.cfg.Namespace, s.cfg.Workload, replicas),
		RollbackPlan:       fmt.Sprintf("Restore the prior replica count %d through the same approved controlled executor.", observation.DesiredReplicas),
		ValidationPlan:     "Require Kubernetes rollout readiness and the persisted Alertmanager resolved Signal; GitOps/Argo checks are not asserted in controlled-direct demo mode.",
		RowVersion:         1, CreatedAt: now, UpdatedAt: now,
	}
	plan.PlanHash, err = remediation.ComputePlanHash(*plan)
	if err != nil {
		return nil, err
	}
	if err := s.cfg.Remediations.CreatePlan(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *Service) List(ctx context.Context, filter remediation.ListFilter) (remediation.Page, error) {
	return s.cfg.Remediations.ListPlans(ctx, filter)
}

func (s *Service) Get(ctx context.Context, publicID string) (*remediation.RemediationPlan, error) {
	return s.cfg.Remediations.GetPlan(ctx, publicID)
}

func (s *Service) Approve(ctx context.Context, publicID, actor, role, planHash, patchHash string, expectedVersion uint64) (*remediation.RemediationPlan, *remediation.ChangeRequest, error) {
	if role != "admin" || strings.TrimSpace(actor) == "" {
		return nil, nil, remediation.ErrForbidden
	}
	plan, err := s.cfg.Remediations.GetPlan(ctx, publicID)
	if err != nil {
		return nil, nil, err
	}
	if plan.PlanHash != planHash || plan.ProposedPatchHash != patchHash {
		return nil, nil, remediation.ErrApprovalMismatch
	}
	idempotency, _ := remediation.CanonicalHash(struct{ PlanID, PlanHash, PatchHash string }{plan.PublicID, plan.PlanHash, plan.ProposedPatchHash})
	delivery := &remediation.ChangeRequest{PublicID: uuid.NewString(), Repository: plan.TargetRepository, BaseRevision: plan.TargetBaseRevision, HeadBranch: "controlled-direct/" + plan.PublicID, Status: remediation.DeliveryPending, CIStatus: remediation.CIPending, IdempotencyKey: idempotency, RowVersion: 1, CreatedAt: s.cfg.Now(), UpdatedAt: s.cfg.Now()}
	approval := remediation.Approval{PublicID: uuid.NewString(), Decision: remediation.DecisionApproved, Actor: actor, ApprovedPlanHash: planHash, ApprovedPatchHash: patchHash, CreatedAt: s.cfg.Now()}
	return s.cfg.Remediations.ApprovePlan(ctx, publicID, expectedVersion, approval, delivery)
}

func (s *Service) Reject(ctx context.Context, publicID, actor, role, planHash, patchHash string, expectedVersion uint64) (*remediation.RemediationPlan, error) {
	if role != "admin" || strings.TrimSpace(actor) == "" {
		return nil, remediation.ErrForbidden
	}
	approval := remediation.Approval{PublicID: uuid.NewString(), Decision: remediation.DecisionRejected, Actor: actor, ApprovedPlanHash: planHash, ApprovedPatchHash: patchHash, CreatedAt: s.cfg.Now()}
	return s.cfg.Remediations.RejectPlan(ctx, publicID, expectedVersion, approval)
}

func (s *Service) Execute(ctx context.Context, planID string) (*verification.Run, error) {
	delivery, plan, err := s.cfg.Remediations.ClaimDelivery(ctx, "fast-demo-controlled-executor", s.cfg.Now(), 30*time.Second)
	if err != nil {
		return nil, err
	}
	if plan.PublicID != planID {
		_ = s.cfg.Remediations.ReleaseDelivery(ctx, delivery.ID, delivery.RowVersion, delivery.LeaseOwner, "unexpected_plan")
		return nil, remediation.ErrConflict
	}
	computed, err := remediation.ComputePlanHash(*plan)
	if err != nil || computed != plan.PlanHash || plan.Parameters.ProposedValue.Replicas == nil {
		return nil, remediation.ErrApprovalMismatch
	}
	if _, err := s.cfg.Executor.ScaleDeployment(ctx, s.cfg.Namespace, s.cfg.Workload, int32(*plan.Parameters.ProposedValue.Replicas)); err != nil {
		_ = s.cfg.Remediations.ReleaseDelivery(context.WithoutCancel(ctx), delivery.ID, delivery.RowVersion, delivery.LeaseOwner, "controlled_execution_failed")
		return nil, err
	}
	deadline := time.Now().Add(90 * time.Second)
	var rollout verification.RolloutObservation
	for {
		rollout, err = s.cfg.Rollout.ObserveDeployment(ctx, s.cfg.Cluster, s.cfg.Namespace, s.cfg.Workload)
		if err == nil && rollout.ObservedGeneration >= rollout.Generation && rollout.UpdatedReplicas == rollout.DesiredReplicas && rollout.AvailableReplicas == rollout.DesiredReplicas && rollout.UnavailableReplicas == 0 && rollout.Progressing && rollout.Available && rollout.PodsReady == rollout.PodsTotal {
			break
		}
		if time.Now().After(deadline) {
			_ = s.cfg.Remediations.ReleaseDelivery(context.WithoutCancel(ctx), delivery.ID, delivery.RowVersion, delivery.LeaseOwner, "rollout_timeout")
			return nil, fmt.Errorf("fast demo: workload rollout timeout")
		}
		time.Sleep(time.Second)
	}
	result := remediation.ControlledExecutionResult{Revision: s.cfg.Revision, Cluster: s.cfg.Cluster, Environment: s.cfg.Environment, Namespace: s.cfg.Namespace, WorkloadName: s.cfg.Workload, DeploymentGeneration: rollout.Generation, ObservedGeneration: rollout.ObservedGeneration, RolloutRevision: rollout.RolloutRevision, DesiredReplicas: rollout.DesiredReplicas, UpdatedReplicas: rollout.UpdatedReplicas, AvailableReplicas: rollout.AvailableReplicas, UnavailableReplicas: rollout.UnavailableReplicas, ObservedAt: s.cfg.Now()}
	if err := s.cfg.Remediations.CompleteControlledExecution(ctx, delivery, result); err != nil {
		return nil, err
	}
	observedDelivery, err := s.cfg.Verifications.GetDeliveryByIncident(ctx, plan.IncidentPublicID)
	if err != nil {
		return nil, err
	}
	subject := verification.Subject{Repository: observedDelivery.Repository, Revision: observedDelivery.TargetRevision, Cluster: observedDelivery.Cluster, Environment: observedDelivery.Environment, Namespace: observedDelivery.Namespace, Service: observedDelivery.ServiceName, WorkloadKind: observedDelivery.WorkloadKind, WorkloadName: observedDelivery.WorkloadName, AlertFingerprint: observedDelivery.IncidentFingerprint}
	verificationPlan, err := verification.CompileControlledDirectPlan(subject, verification.CompilerConfig{PollInterval: time.Second, StabilityWindow: time.Second, Timeout: 5 * time.Minute, AlertLookback: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	return s.cfg.Verifications.CreateRun(ctx, observedDelivery, verificationPlan, s.cfg.Now())
}

func (s *Service) Verify(ctx context.Context, incidentID string) (*verification.Run, error) {
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		run, err := s.verifyOnce(ctx, incidentID)
		if err != nil && !errors.Is(err, verification.ErrNotFound) {
			return nil, err
		}
		if run != nil && verification.TerminalRun(run.Status) {
			return run, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, fmt.Errorf("fast demo: verification did not reach a terminal result")
}

func (s *Service) verifyOnce(ctx context.Context, incidentID string) (*verification.Run, error) {
	run, err := s.cfg.Verifications.ClaimRun(ctx, "fast-demo-verification-worker", s.cfg.Now(), 10*time.Second)
	if errors.Is(err, verification.ErrNotFound) {
		page, listErr := s.cfg.Verifications.ListRuns(ctx, incidentID, 1, 1)
		if listErr != nil || len(page.Items) == 0 {
			return nil, err
		}
		return &page.Items[0], nil
	}
	if err != nil {
		return nil, err
	}
	now := s.cfg.Now()
	if !now.Before(run.DeadlineAt) {
		if err := s.cfg.Verifications.TimeoutRun(ctx, run, now); err != nil {
			return nil, err
		}
		return s.cfg.Verifications.GetRun(ctx, incidentID, run.PublicID)
	}
	checks, err := s.cfg.Verifications.ListChecks(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	if status, _, terminal := verification.Aggregate(checks); terminal && status != verification.RunRunning {
		return s.cfg.Verifications.AggregateRun(ctx, run, s.cfg.Now())
	}
	var selected *verification.Check
	for index := range checks {
		if verification.TerminalCheck(checks[index].Status) {
			continue
		}
		if checks[index].LastCheckedAt == nil || !now.Before(checks[index].LastCheckedAt.Add(checks[index].PollInterval)) {
			selected = &checks[index]
			break
		}
	}
	if selected == nil {
		return run, s.cfg.Verifications.ReleaseRun(ctx, run, now)
	}
	sample := verification.Sample{Status: verification.SamplePending, Observed: json.RawMessage(`{}`)}
	switch selected.Type {
	case verification.CheckDeploymentRollout, verification.CheckWorkloadReady:
		rollout, observeErr := s.cfg.Rollout.ObserveDeployment(ctx, selected.Subject.Cluster, selected.Subject.Namespace, selected.Subject.WorkloadName)
		if observeErr != nil {
			sample = verification.Sample{Status: verification.SampleUnavailable, Observed: json.RawMessage(`{"available":false}`), ReasonCode: "kubernetes_unavailable"}
			break
		}
		observed, _ := json.Marshal(map[string]any{"generation": rollout.Generation, "observed_generation": rollout.ObservedGeneration, "desired": rollout.DesiredReplicas, "updated": rollout.UpdatedReplicas, "available": rollout.AvailableReplicas, "unavailable": rollout.UnavailableReplicas, "pods_ready": rollout.PodsReady, "pods_total": rollout.PodsTotal})
		sample.Observed, sample.SourceReference = observed, "kubernetes:"+selected.Subject.Namespace+"/Deployment/"+selected.Subject.WorkloadName
		if selected.Type == verification.CheckDeploymentRollout && rollout.ObservedGeneration >= rollout.Generation && rollout.UpdatedReplicas == rollout.DesiredReplicas && rollout.UnavailableReplicas == 0 && rollout.Progressing {
			sample.Status = verification.SamplePassed
		}
		if selected.Type == verification.CheckWorkloadReady && rollout.AvailableReplicas == rollout.DesiredReplicas && rollout.PodsReady == rollout.PodsTotal && rollout.Available {
			sample.Status = verification.SamplePassed
		}
	case verification.CheckAlertResolved:
		resolved, occurredAt, readErr := s.cfg.Verifications.ResolvedSignal(ctx, run.IncidentID, selected.Subject.AlertFingerprint, now.Add(-selected.Lookback))
		if readErr != nil {
			return nil, readErr
		}
		observed, _ := json.Marshal(map[string]any{"resolved": resolved, "fingerprint": selected.Subject.AlertFingerprint, "occurred_at": occurredAt})
		sample.Observed, sample.SourceReference = observed, "incident_signal:"+selected.Subject.AlertFingerprint
		if resolved {
			sample.Status = verification.SamplePassed
		}
	default:
		sample = verification.Sample{Status: verification.SampleInvalid, Observed: json.RawMessage(`{"bounded":true}`), ReasonCode: "unsupported_fast_demo_check"}
	}
	if err := s.cfg.Verifications.PersistCheckSample(ctx, run, selected, sample, now, now.Add(selected.PollInterval)); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) GetDelivery(ctx context.Context, incidentID string) (*verification.Delivery, error) {
	return s.cfg.Verifications.GetDeliveryByIncident(ctx, incidentID)
}

func (s *Service) ListRuns(ctx context.Context, incidentID string, page, pageSize int) (verification.RunPage, error) {
	return s.cfg.Verifications.ListRuns(ctx, incidentID, page, pageSize)
}

func (s *Service) GetRun(ctx context.Context, incidentID, runID string) (*verification.Run, []verification.Check, error) {
	run, err := s.cfg.Verifications.GetRun(ctx, incidentID, runID)
	if err != nil {
		return nil, nil, err
	}
	checks, err := s.cfg.Verifications.ListRunChecks(ctx, incidentID, runID)
	return run, checks, err
}

func (s *Service) GetPostmortem(ctx context.Context, incidentID string) (*verification.Postmortem, error) {
	return s.cfg.Verifications.GetPostmortem(ctx, incidentID)
}

func hashValue(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
