package remediationservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"server-web/internal/change"
	"server-web/internal/incident"
	"server-web/internal/remediation"
)

var exactRevision = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)

type IncidentReader interface {
	GetByPublicID(context.Context, string) (*incident.Incident, error)
}

type EvidenceReader interface {
	ListByIncident(context.Context, uint64, int) ([]incident.EvidenceItem, error)
}

type Mapping struct {
	Repository string
	Path       string
	BaseBranch string
}

type Observer interface {
	ObserveRemediationPlan(status string)
	ObserveChangeRequestDelivery(result string)
}

type Config struct {
	Enabled    bool
	Repository remediation.Repository
	Incidents  IncidentReader
	Evidence   EvidenceReader
	Changes    change.Repository
	BaseReader remediation.GitHubWriter
	Mappings   map[string]Mapping
	Policy     remediation.PolicyConfig
	Observer   Observer
	Now        func() time.Time
}

type Service struct{ cfg Config }

func New(cfg Config) (*Service, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Enabled && (cfg.Repository == nil || cfg.Incidents == nil || cfg.Evidence == nil || cfg.Changes == nil || cfg.BaseReader == nil || len(cfg.Mappings) == 0) {
		return nil, fmt.Errorf("%w: remediation dependencies", remediation.ErrInvalidArgument)
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) Enabled() bool { return s != nil && s.cfg.Enabled }

func (s *Service) CreatePlan(ctx context.Context, incidentPublicID string, plannerJSON []byte) (*remediation.RemediationPlan, remediation.PolicyDecision, error) {
	if !s.Enabled() {
		return nil, remediation.PolicyDecision{}, remediation.ErrForbidden
	}
	output, err := remediation.DecodePlannerOutput(plannerJSON)
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	item, err := s.cfg.Incidents.GetByPublicID(ctx, incidentPublicID)
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	if item.Status != incident.StatusDiagnosisCompleted && item.Status != incident.StatusPlanningRemediation {
		return nil, remediation.PolicyDecision{}, remediation.ErrInvalidTransition
	}
	mapping, ok := s.cfg.Mappings[item.ServiceName]
	if !ok {
		return nil, remediation.PolicyDecision{}, remediation.ErrPolicyRejected
	}
	changeItem, err := s.confirmedChange(ctx, incidentPublicID, mapping)
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	base, err := s.cfg.BaseReader.ReadBaseFile(ctx, mapping.Repository, changeItem.CommitSHA, mapping.Path)
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	parameters := remediation.Parameters{Target: output.TargetResource, ProposedValue: output.ProposedValue}
	patch, err := remediation.RenderPatch(base, output.OperationType, parameters)
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	evidence, err := s.loadEvidence(ctx, item.ID, output.EvidenceIDs)
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	decision, err := remediation.EvaluatePolicy(s.cfg.Policy, remediation.PolicyInput{IncidentID: item.ID, Repository: mapping.Repository, Path: mapping.Path, Operation: output.OperationType, Parameters: parameters, Evidence: evidence, Patch: patch})
	if err != nil {
		return nil, remediation.PolicyDecision{}, err
	}
	page, err := s.cfg.Repository.ListPlans(ctx, remediation.ListFilter{IncidentPublicID: incidentPublicID, Page: 1, PageSize: 100})
	if err != nil && !errors.Is(err, remediation.ErrNotFound) {
		return nil, decision, err
	}
	plan := &remediation.RemediationPlan{PublicID: uuid.NewString(), IncidentID: item.ID, IncidentPublicID: item.PublicID, PlanVersion: nextPlanVersion(page.Items), Status: remediation.PlanAwaitingApproval, OperationType: output.OperationType, TargetRepository: mapping.Repository, TargetBaseRevision: strings.ToLower(changeItem.CommitSHA), TargetPath: mapping.Path, Parameters: parameters, EvidenceReferences: append([]string(nil), output.EvidenceIDs...), RiskLevel: decision.RiskLevel, PolicySnapshotHash: decision.PolicySnapshotHash, ExpectedBeforeHash: patch.BeforeHash, ProposedPatchHash: patch.PatchHash, PatchSummary: patch.Summary, RollbackPlan: rollbackText(output.OperationType), ValidationPlan: "Review Draft PR checks and the exact approved diff; Phase 4 performs no merge, sync, or recovery verification.", RowVersion: 1, CreatedAt: s.cfg.Now().UTC(), UpdatedAt: s.cfg.Now().UTC()}
	if !decision.Allowed {
		plan.Status = remediation.PlanPolicyRejected
	}
	plan.PlanHash, err = remediation.ComputePlanHash(*plan)
	if err != nil {
		return nil, decision, err
	}
	if err := s.cfg.Repository.CreatePlan(ctx, plan); err != nil {
		return nil, decision, err
	}
	if s.cfg.Observer != nil {
		s.cfg.Observer.ObserveRemediationPlan(string(plan.Status))
	}
	if !decision.Allowed {
		return plan, decision, remediation.ErrPolicyRejected
	}
	return plan, decision, nil
}

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

func (s *Service) confirmedChange(ctx context.Context, incidentID string, mapping Mapping) (*change.Change, error) {
	page, err := s.cfg.Changes.ListByIncident(ctx, incidentID, change.ListFilter{Status: change.StatusMatched, Category: change.CategoryConfirmed, Page: 1, PageSize: 50})
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		item := &page.Items[i]
		if item.Repository == mapping.Repository && item.GitOpsPath == mapping.Path && exactRevision.MatchString(item.CommitSHA) && !item.Truncated && !item.Degraded {
			return item, nil
		}
	}
	return nil, remediation.ErrPolicyRejected
}

func (s *Service) loadEvidence(ctx context.Context, incidentID uint64, ids []string) ([]remediation.EvidenceFact, error) {
	items, err := s.cfg.Evidence.ListByIncident(ctx, incidentID, 100)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]incident.EvidenceItem, len(items))
	for _, item := range items {
		byID[item.PublicID] = item
	}
	result := make([]remediation.EvidenceFact, 0, len(ids))
	for _, id := range ids {
		item, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: evidence does not belong to incident", remediation.ErrPolicyRejected)
		}
		confirmed, registry, digests := inspectEvidence(item.Facts)
		result = append(result, remediation.EvidenceFact{PublicID: item.PublicID, IncidentID: item.IncidentID, Valid: item.Valid, Truncated: item.Truncated, ConfirmedChange: confirmed, RegistryVerified: registry, DeployedDigests: digests, Facts: item.Facts})
	}
	return result, nil
}

func inspectEvidence(payload json.RawMessage) (bool, bool, []string) {
	var root any
	if json.Unmarshal(payload, &root) != nil {
		return false, false, nil
	}
	confirmed := false
	registry := false
	digests := make([]string, 0, 2)
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if typed["status"] == "matched" && typed["category"] == "confirmed_match" {
				confirmed = true
			}
			if typed["status"] == "confirmed" && typed["valid"] == true && typed["truncated"] == false && typed["degraded"] == false {
				if metadata, ok := typed["registry_metadata"].(map[string]any); ok && metadata["integrity_status"] == "verified" && metadata["valid"] == true && metadata["truncated"] == false && metadata["degraded"] == false {
					registry = true
					if digest, ok := typed["digest"].(string); ok {
						digests = append(digests, strings.ToLower(digest))
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(root)
	slices.Sort(digests)
	return confirmed, registry, slices.Compact(digests)
}

func nextPlanVersion(plans []remediation.RemediationPlan) int {
	maxVersion := 0
	for _, plan := range plans {
		if plan.PlanVersion > maxVersion {
			maxVersion = plan.PlanVersion
		}
	}
	return maxVersion + 1
}

func rollbackText(operation remediation.OperationType) string {
	if operation == remediation.OperationRollbackImage {
		return "Close the Draft PR or revert its single commit; no Kubernetes or Argo CD action is performed by Phase 4."
	}
	return "Close the Draft PR or revert its single commit to restore the approved base replica count."
}
