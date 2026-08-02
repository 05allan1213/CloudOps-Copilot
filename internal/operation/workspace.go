package operation

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

type AuthorityReader interface {
	OperationPlans(context.Context, int) ([]agent.OperationPlan, error)
	ActionCards(context.Context, int) ([]agent.ActionCard, error)
}

type WorkspaceService struct {
	repository *Repository
	authority  AuthorityReader
}

func NewWorkspaceService(repository *Repository, authority AuthorityReader) (*WorkspaceService, error) {
	if repository == nil || authority == nil {
		return nil, ErrInvalidArgument
	}
	return &WorkspaceService{repository: repository, authority: authority}, nil
}

func (s *WorkspaceService) EnqueueActionCard(ctx context.Context, id string, request ExecuteRequest) (Execution, error) {
	return s.repository.EnqueueActionCard(ctx, id, request)
}

func (s *WorkspaceService) EnqueueOperationPlan(ctx context.Context, id string, request ExecuteRequest) (Execution, error) {
	return s.repository.EnqueueOperationPlan(ctx, id, request)
}

func (s *WorkspaceService) Execution(ctx context.Context, id string) (Execution, error) {
	return s.repository.Execution(ctx, id)
}

func (s *WorkspaceService) Executions(ctx context.Context, limit int) ([]Execution, error) {
	return s.repository.Executions(ctx, limit)
}

type DevOpsWorkspace struct {
	OperationPlans      []agent.OperationPlan `json:"operation_plans"`
	ActionCards         []agent.ActionCard    `json:"action_cards"`
	Executions          []Execution           `json:"executions"`
	ChangeFreezes       []ChangeFreezeState   `json:"change_freezes"`
	ChangeCandidates    []ChangeCandidate     `json:"change_candidates"`
	DeploymentBaselines []DeploymentBaseline  `json:"deployment_baselines"`
	Deliveries          []DeliveryProjection  `json:"deliveries"`
	Providers           []ProviderBranch      `json:"providers"`
	CollectedAt         time.Time             `json:"collected_at"`
}

type ChangeCandidate struct {
	ID                 string          `json:"id"`
	IncidentID         string          `json:"incident_id"`
	RunID              string          `json:"run_id"`
	Cycle              uint64          `json:"cycle"`
	ChangeRef          string          `json:"change_ref"`
	SourceType         string          `json:"source_type"`
	Repository         string          `json:"repository"`
	CommitSHA          string          `json:"commit_sha"`
	GitOpsRevision     string          `json:"gitops_revision"`
	ImageDigest        string          `json:"image_digest"`
	TargetPath         string          `json:"target_path"`
	Category           string          `json:"category"`
	SupportingEvidence json.RawMessage `json:"supporting_evidence"`
	ContentHash        string          `json:"content_hash"`
	ChangeTime         time.Time       `json:"change_time"`
	CreatedAt          time.Time       `json:"created_at"`
}

type DeploymentBaseline struct {
	ID                        string     `json:"id"`
	TargetIdentityHash        string     `json:"target_identity_hash"`
	Cluster                   string     `json:"cluster"`
	Environment               string     `json:"environment"`
	Namespace                 string     `json:"namespace"`
	WorkloadKind              string     `json:"workload_kind"`
	WorkloadName              string     `json:"workload_name"`
	ContainerName             string     `json:"container_name"`
	Repository                string     `json:"repository"`
	BaseBranch                string     `json:"base_branch"`
	TargetPath                string     `json:"target_path"`
	SourceRevision            string     `json:"source_revision"`
	ImageDigest               string     `json:"image_digest"`
	GitOpsRevision            string     `json:"gitops_revision"`
	ConfigHash                string     `json:"config_hash"`
	VerificationPolicyVersion string     `json:"verification_policy_version"`
	VerificationHash          string     `json:"verification_hash"`
	Status                    string     `json:"status"`
	RowVersion                uint64     `json:"row_version"`
	VerifiedAt                time.Time  `json:"verified_at"`
	SupersededAt              *time.Time `json:"superseded_at,omitempty"`
}

type DeliveryProjection struct {
	ID                  string     `json:"id"`
	IncidentID          string     `json:"incident_id"`
	Repository          string     `json:"repository"`
	BaseRevision        string     `json:"base_revision"`
	HeadBranch          string     `json:"head_branch"`
	CommitSHA           string     `json:"commit_sha"`
	PullRequestNumber   int64      `json:"pull_request_number"`
	PullRequestURL      string     `json:"pull_request_url"`
	PullRequestState    string     `json:"pull_request_state"`
	CIStatus            string     `json:"ci_status"`
	MergedCommitSHA     string     `json:"merged_commit_sha"`
	TargetRevision      string     `json:"target_revision"`
	ArgoApplication     string     `json:"argo_application"`
	ArgoSyncStatus      string     `json:"argo_sync_status"`
	ArgoOperationPhase  string     `json:"argo_operation_phase"`
	ArgoHealthStatus    string     `json:"argo_health_status"`
	RolloutRevision     string     `json:"rollout_revision"`
	DesiredReplicas     int32      `json:"desired_replicas"`
	UpdatedReplicas     int32      `json:"updated_replicas"`
	AvailableReplicas   int32      `json:"available_replicas"`
	UnavailableReplicas int32      `json:"unavailable_replicas"`
	Status              string     `json:"status"`
	LastObservedAt      *time.Time `json:"last_observed_at,omitempty"`
}

type ProviderBranch struct {
	Provider                string     `json:"provider"`
	Role                    string     `json:"role"`
	Enabled                 bool       `json:"enabled"`
	State                   string     `json:"state"`
	Detail                  string     `json:"detail"`
	ConfigurationRevisionID string     `json:"configuration_revision_id"`
	CheckedAt               *time.Time `json:"checked_at,omitempty"`
}

func (s *WorkspaceService) Workspace(ctx context.Context, limit int) (DevOpsWorkspace, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	plans, err := s.authority.OperationPlans(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	cards, err := s.authority.ActionCards(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	executions, err := s.repository.Executions(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	changeFreezes, err := s.repository.changeFreezes(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	candidates, err := s.repository.changeCandidates(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	baselines, err := s.repository.deploymentBaselines(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	deliveries, err := s.repository.deliveries(ctx, limit)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	providers, err := s.repository.providerBranches(ctx)
	if err != nil {
		return DevOpsWorkspace{}, err
	}
	return DevOpsWorkspace{
		OperationPlans: plans, ActionCards: cards, Executions: executions,
		ChangeFreezes:    changeFreezes,
		ChangeCandidates: candidates, DeploymentBaselines: baselines, Deliveries: deliveries,
		Providers: providers, CollectedAt: s.repository.now().UTC(),
	}, nil
}

func (r *Repository) changeFreezes(ctx context.Context, limit int) ([]ChangeFreezeState, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT card.target_json,freeze.enabled,freeze.reason,
freeze.row_version,freeze.updated_at
FROM operation_change_freezes freeze
JOIN operation_executions execution ON execution.id=freeze.updated_by_execution_id
JOIN agent_action_cards card ON card.id=execution.action_card_id
ORDER BY freeze.updated_at DESC,freeze.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ChangeFreezeState, 0)
	for rows.Next() {
		var item ChangeFreezeState
		var targetJSON json.RawMessage
		var updatedAt time.Time
		if err = rows.Scan(&targetJSON, &item.Enabled, &item.Reason, &item.RowVersion, &updatedAt); err != nil {
			return nil, err
		}
		if err = decodeExact(targetJSON, &item.Target); err != nil {
			return nil, err
		}
		if err = validateTarget(item.Target); err != nil {
			return nil, err
		}
		value := updatedAt.UTC()
		item.UpdatedAt = &value
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) changeCandidates(ctx context.Context, limit int) ([]ChangeCandidate, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT candidate.public_id,incident.public_id,run.public_id,candidate.cycle_no,
candidate.change_ref,candidate.source_type,candidate.repository,candidate.commit_sha,candidate.gitops_revision,
candidate.image_digest,candidate.target_path,candidate.category,candidate.supporting_evidence_json,
candidate.content_hash,candidate.change_time,candidate.created_at
FROM change_candidates candidate JOIN incidents incident ON incident.id=candidate.incident_id
JOIN agent_runs run ON run.id=candidate.agent_run_id
ORDER BY candidate.change_time DESC,candidate.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ChangeCandidate, 0)
	for rows.Next() {
		var item ChangeCandidate
		if err = rows.Scan(&item.ID, &item.IncidentID, &item.RunID, &item.Cycle, &item.ChangeRef,
			&item.SourceType, &item.Repository, &item.CommitSHA, &item.GitOpsRevision, &item.ImageDigest,
			&item.TargetPath, &item.Category, &item.SupportingEvidence, &item.ContentHash,
			&item.ChangeTime, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ChangeTime, item.CreatedAt = item.ChangeTime.UTC(), item.CreatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) deploymentBaselines(ctx context.Context, limit int) ([]DeploymentBaseline, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT public_id,target_identity_hash,cluster,environment,namespace,
workload_kind,workload_name,container_name,repository,base_branch,target_path,source_revision,image_digest,
gitops_revision,config_hash,verification_policy_version,verification_hash,status,row_version,verified_at,superseded_at
FROM deployment_baselines ORDER BY verified_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]DeploymentBaseline, 0)
	for rows.Next() {
		var item DeploymentBaseline
		var superseded sql.NullTime
		if err = rows.Scan(&item.ID, &item.TargetIdentityHash, &item.Cluster, &item.Environment,
			&item.Namespace, &item.WorkloadKind, &item.WorkloadName, &item.ContainerName,
			&item.Repository, &item.BaseBranch, &item.TargetPath, &item.SourceRevision,
			&item.ImageDigest, &item.GitOpsRevision, &item.ConfigHash, &item.VerificationPolicyVersion,
			&item.VerificationHash, &item.Status, &item.RowVersion, &item.VerifiedAt, &superseded); err != nil {
			return nil, err
		}
		item.VerifiedAt = item.VerifiedAt.UTC()
		if superseded.Valid {
			value := superseded.Time.UTC()
			item.SupersededAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) deliveries(ctx context.Context, limit int) ([]DeliveryProjection, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT request.public_id,incident.public_id,request.repository,
request.base_revision,request.head_branch,request.commit_sha,request.pr_number,request.pr_url,request.pr_state,
request.ci_status,request.merged_commit_sha,request.target_revision,request.argocd_application,
request.argocd_sync_status,request.argocd_operation_phase,request.argocd_health_status,
request.rollout_revision,request.desired_replicas,request.updated_replicas,request.available_replicas,
request.unavailable_replicas,request.status,request.last_observed_at
FROM change_requests request JOIN incidents incident ON incident.id=request.incident_id
ORDER BY request.created_at DESC,request.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]DeliveryProjection, 0)
	for rows.Next() {
		var item DeliveryProjection
		var observed sql.NullTime
		if err = rows.Scan(&item.ID, &item.IncidentID, &item.Repository, &item.BaseRevision,
			&item.HeadBranch, &item.CommitSHA, &item.PullRequestNumber, &item.PullRequestURL,
			&item.PullRequestState, &item.CIStatus, &item.MergedCommitSHA, &item.TargetRevision,
			&item.ArgoApplication, &item.ArgoSyncStatus, &item.ArgoOperationPhase,
			&item.ArgoHealthStatus, &item.RolloutRevision, &item.DesiredReplicas,
			&item.UpdatedReplicas, &item.AvailableReplicas, &item.UnavailableReplicas,
			&item.Status, &observed); err != nil {
			return nil, err
		}
		if observed.Valid {
			value := observed.Time.UTC()
			item.LastObservedAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) providerBranches(ctx context.Context) ([]ProviderBranch, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT config.provider,config.enabled,
COALESCE(health.state,IF(config.enabled,'unavailable','disabled')),
COALESCE(health.detail,IF(config.enabled,'No current provider observation','Provider disabled')),
revision.public_id,health.checked_at
FROM active_configuration active JOIN configuration_revisions revision ON revision.id=active.configuration_revision_id
JOIN provider_configurations config ON config.configuration_revision_id=revision.id
LEFT JOIN provider_health health ON health.configuration_revision_id=revision.id AND health.provider=config.provider
WHERE active.singleton_id=1 AND config.provider IN ('kubernetes','github','argocd')
ORDER BY FIELD(config.provider,'kubernetes','github','argocd')`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ProviderBranch, 0, 3)
	for rows.Next() {
		var item ProviderBranch
		var checked sql.NullTime
		if err = rows.Scan(&item.Provider, &item.Enabled, &item.State, &item.Detail,
			&item.ConfigurationRevisionID, &checked); err != nil {
			return nil, err
		}
		item.Role = "optional"
		if item.Provider == "kubernetes" {
			item.Role = "core"
		}
		if checked.Valid {
			value := checked.Time.UTC()
			item.CheckedAt = &value
		}
		result = append(result, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, provider := range []string{"kubernetes", "github", "argocd"} {
		found := false
		for _, item := range result {
			found = found || item.Provider == provider
		}
		if !found {
			role := "optional"
			if provider == "kubernetes" {
				role = "core"
			}
			result = append(result, ProviderBranch{Provider: provider, Role: role, State: "not_configured", Detail: "Provider is not configured"})
		}
	}
	return result, nil
}
