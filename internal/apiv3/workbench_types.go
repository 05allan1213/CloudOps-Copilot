package apiv3

import (
	"encoding/json"
	"time"
)

// RemediationPlanView is the complete bounded Workbench projection of an
// immutable V3 remediation plan. Identifier-bearing fields are public UUIDs;
// the internal plan, Incident, and AgentRun numeric keys never cross the port.
type RemediationPlanView struct {
	ID                       string                   `json:"id"`
	Kind                     string                   `json:"kind"`
	Cycle                    uint64                   `json:"cycle"`
	Status                   string                   `json:"status"`
	Version                  uint64                   `json:"version"`
	PlanVersion              uint64                   `json:"plan_version"`
	PlanContentSchemaVersion uint64                   `json:"plan_content_schema_version"`
	IncidentVersion          uint64                   `json:"incident_version"`
	CreatedByAgentRunID      string                   `json:"created_by_agent_run_id"`
	OperationType            string                   `json:"operation_type"`
	RiskLevel                string                   `json:"risk_level"`
	PatchSummary             string                   `json:"patch_summary"`
	RollbackPlan             string                   `json:"rollback_plan"`
	ValidationPlan           string                   `json:"validation_plan"`
	Target                   RemediationTargetView    `json:"target"`
	HashSchemaVersion        uint64                   `json:"hash_schema_version"`
	DiagnosisHash            string                   `json:"diagnosis_hash"`
	CanonicalPlanHash        string                   `json:"canonical_plan_hash"`
	ExpectedBeforeHash       string                   `json:"expected_before_hash"`
	ExpectedPostImageHash    string                   `json:"expected_post_image_hash"`
	ExpectedTreeHash         string                   `json:"expected_tree_hash"`
	ProposedPatchHash        string                   `json:"proposed_patch_hash"`
	CanonicalManifest        json.RawMessage          `json:"canonical_manifest"`
	BoundedDiff              string                   `json:"bounded_diff"`
	PolicyVersion            string                   `json:"policy_version"`
	PolicyHash               string                   `json:"policy_hash"`
	PolicySnapshot           json.RawMessage          `json:"policy_snapshot"`
	VerificationPlan         json.RawMessage          `json:"verification_plan"`
	VerificationPlanHash     string                   `json:"verification_plan_hash"`
	EvidenceBindings         []EvidenceBindingView    `json:"evidence_bindings"`
	EvidenceSetHash          string                   `json:"evidence_set_hash"`
	ExpiresAt                time.Time                `json:"expires_at"`
	Decision                 *RemediationDecisionView `json:"decision,omitempty"`
	CreatedAt                time.Time                `json:"created_at"`
	UpdatedAt                time.Time                `json:"updated_at"`
}

type RemediationTargetView struct {
	Repository            string                        `json:"repository"`
	BaseBranch            string                        `json:"base_branch"`
	BaseRevision          string                        `json:"base_revision"`
	LastKnownGoodRevision string                        `json:"last_known_good_revision"`
	BaseBlobSHA           string                        `json:"base_blob_sha"`
	FileMode              string                        `json:"file_mode"`
	Path                  string                        `json:"path"`
	FieldRef              string                        `json:"field_ref"`
	Resource              RemediationTargetResourceView `json:"resource"`
}

type RemediationTargetResourceView struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Container  string `json:"container,omitempty"`
}

type EvidenceBindingView struct {
	ID          string `json:"id"`
	ContentHash string `json:"content_hash"`
}

type RemediationDecisionView struct {
	ID                        string                       `json:"id"`
	DecisionSchemaVersion     uint64                       `json:"decision_schema_version"`
	PlanVersion               uint64                       `json:"plan_version"`
	Decision                  string                       `json:"decision"`
	Actor                     RemediationDecisionActorView `json:"actor"`
	Reason                    string                       `json:"reason"`
	RequestID                 string                       `json:"request_id"`
	RequestAuthenticatedAt    time.Time                    `json:"request_authenticated_at"`
	ExpiresAt                 time.Time                    `json:"expires_at"`
	ApprovedHashSchemaVersion uint64                       `json:"approved_hash_schema_version"`
	ApprovedPlanHash          string                       `json:"approved_plan_hash"`
	ApprovedBaseSHA           string                       `json:"approved_base_sha"`
	ApprovedPostImageHash     string                       `json:"approved_post_image_hash"`
	ApprovedTreeHash          string                       `json:"approved_tree_hash"`
	ApprovedPatchHash         string                       `json:"approved_patch_hash"`
	ApprovedPolicyHash        string                       `json:"approved_policy_hash"`
	ApprovedVerificationHash  string                       `json:"approved_verification_hash"`
	ApprovedEvidenceSetHash   string                       `json:"approved_evidence_set_hash"`
	CreatedAt                 time.Time                    `json:"created_at"`
}

type RemediationDecisionActorView struct {
	Provider string `json:"provider"`
	Login    string `json:"login"`
	Role     string `json:"role"`
}

// DeliveryView exposes only persisted delivery facts. Scheduling, lease,
// fencing, write markers, and provider credentials are intentionally absent.
type DeliveryView struct {
	ID                   string          `json:"id"`
	Kind                 string          `json:"kind"`
	Cycle                uint64          `json:"cycle"`
	Status               string          `json:"status"`
	Version              uint64          `json:"version"`
	RemediationPlanID    string          `json:"remediation_plan_id"`
	Repository           string          `json:"repository"`
	BaseRevision         string          `json:"base_revision"`
	HeadBranch           string          `json:"head_branch"`
	CommitSHA            string          `json:"commit_sha,omitempty"`
	PRNumber             int64           `json:"pr_number,omitempty"`
	PRURL                string          `json:"pr_url,omitempty"`
	PRState              string          `json:"pr_state,omitempty"`
	CIStatus             string          `json:"ci_status"`
	MergedCommitSHA      string          `json:"merged_commit_sha,omitempty"`
	TargetRevision       string          `json:"target_revision,omitempty"`
	DetectedRevision     string          `json:"detected_revision,omitempty"`
	ArgoApplication      string          `json:"argocd_application,omitempty"`
	ArgoProject          string          `json:"argocd_project,omitempty"`
	ArgoSyncStatus       string          `json:"argocd_sync_status,omitempty"`
	ArgoOperationPhase   string          `json:"argocd_operation_phase,omitempty"`
	ArgoHealthStatus     string          `json:"argocd_health_status,omitempty"`
	ResourceHealth       json.RawMessage `json:"resource_health,omitempty"`
	Cluster              string          `json:"cluster,omitempty"`
	Environment          string          `json:"environment,omitempty"`
	Namespace            string          `json:"namespace,omitempty"`
	WorkloadKind         string          `json:"workload_kind,omitempty"`
	WorkloadName         string          `json:"workload_name,omitempty"`
	DeploymentGeneration int64           `json:"deployment_generation,omitempty"`
	ObservedGeneration   int64           `json:"observed_generation,omitempty"`
	RolloutRevision      string          `json:"rollout_revision,omitempty"`
	DesiredReplicas      int64           `json:"desired_replicas"`
	UpdatedReplicas      int64           `json:"updated_replicas"`
	AvailableReplicas    int64           `json:"available_replicas"`
	UnavailableReplicas  int64           `json:"unavailable_replicas"`
	SyncStartedAt        *time.Time      `json:"sync_started_at,omitempty"`
	SyncCompletedAt      *time.Time      `json:"sync_completed_at,omitempty"`
	DeliveryStartedAt    *time.Time      `json:"delivery_started_at,omitempty"`
	DeliveryDeadlineAt   *time.Time      `json:"delivery_deadline_at,omitempty"`
	DeliveryCompletedAt  *time.Time      `json:"delivery_completed_at,omitempty"`
	LastObservedAt       *time.Time      `json:"last_observed_at,omitempty"`
	FailureCode          string          `json:"failure_code,omitempty"`
	FailureReason        string          `json:"failure_reason,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// VerificationRunView nests the bounded durable Check and Sample facts for a
// current-cycle Run. All relation fields are joined to public UUIDs.
type VerificationRunView struct {
	ID                string                       `json:"id"`
	Kind              string                       `json:"kind"`
	Cycle             uint64                       `json:"cycle"`
	Status            string                       `json:"status"`
	Version           uint64                       `json:"version"`
	TriggerType       string                       `json:"trigger_type"`
	RemediationPlanID string                       `json:"remediation_plan_id,omitempty"`
	ChangeRequestID   string                       `json:"change_request_id,omitempty"`
	TriggerSignalID   string                       `json:"trigger_signal_id,omitempty"`
	Attempt           uint64                       `json:"attempt"`
	Profile           VerificationProfileView      `json:"profile"`
	Revisions         VerificationRevisionsView    `json:"revisions"`
	StartedAt         *time.Time                   `json:"started_at,omitempty"`
	DeadlineAt        time.Time                    `json:"deadline_at"`
	CompletedAt       *time.Time                   `json:"completed_at,omitempty"`
	CommonWindow      VerificationCommonWindowView `json:"common_window"`
	ResultSummary     string                       `json:"result_summary,omitempty"`
	FailureReason     string                       `json:"failure_reason,omitempty"`
	Checks            []VerificationCheckView      `json:"checks"`
	CreatedAt         time.Time                    `json:"created_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`
}

type VerificationProfileView struct {
	ID              string `json:"id"`
	Version         uint64 `json:"version"`
	Hash            string `json:"hash"`
	ContractVersion uint64 `json:"contract_version"`
}

type VerificationRevisionsView struct {
	TargetRevision string `json:"target_revision"`
	SourceRevision string `json:"source_revision"`
	ImageDigest    string `json:"image_digest"`
	GitOpsRevision string `json:"gitops_revision"`
}

type VerificationCommonWindowView struct {
	StabilityWindowMS uint64     `json:"stability_window_ms"`
	SuccessSince      *time.Time `json:"success_since,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type VerificationCheckView struct {
	ID                      string                   `json:"id"`
	SpecSchemaVersion       uint64                   `json:"spec_schema_version"`
	Type                    string                   `json:"type"`
	Status                  string                   `json:"status"`
	Required                bool                     `json:"required"`
	ProfileID               string                   `json:"profile_id"`
	TemplateID              string                   `json:"template_id"`
	TemplateVersion         string                   `json:"template_version"`
	Subject                 VerificationSubjectView  `json:"subject"`
	Expected                json.RawMessage          `json:"expected"`
	Observed                json.RawMessage          `json:"observed,omitempty"`
	Comparison              string                   `json:"comparison,omitempty"`
	Threshold               *float64                 `json:"threshold,omitempty"`
	SourceReference         string                   `json:"source_reference,omitempty"`
	SourceIdentity          string                   `json:"source_identity"`
	LookbackMS              uint64                   `json:"lookback_ms"`
	InitialDelayMS          uint64                   `json:"initial_delay_ms"`
	StabilityWindowMS       uint64                   `json:"stability_window_ms"`
	TimeoutMS               uint64                   `json:"timeout_ms"`
	PollIntervalMS          uint64                   `json:"poll_interval_ms"`
	MinSamples              uint64                   `json:"min_samples"`
	SampleUnit              string                   `json:"sample_unit"`
	FailureMode             string                   `json:"failure_mode"`
	FirstCheckedAt          *time.Time               `json:"first_checked_at,omitempty"`
	LastCheckedAt           *time.Time               `json:"last_checked_at,omitempty"`
	PassedAt                *time.Time               `json:"passed_at,omitempty"`
	ConsecutiveSuccessSince *time.Time               `json:"consecutive_success_since,omitempty"`
	AttemptCount            uint64                   `json:"attempt_count"`
	FailureReason           string                   `json:"failure_reason,omitempty"`
	Samples                 []VerificationSampleView `json:"samples"`
	CreatedAt               time.Time                `json:"created_at"`
	UpdatedAt               time.Time                `json:"updated_at"`
}

type VerificationSubjectView struct {
	Repository       string `json:"repository,omitempty"`
	PullRequest      int64  `json:"pull_request,omitempty"`
	Revision         string `json:"revision"`
	ArgoApplication  string `json:"argocd_application,omitempty"`
	ArgoProject      string `json:"argocd_project,omitempty"`
	Cluster          string `json:"cluster,omitempty"`
	Environment      string `json:"environment,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	Service          string `json:"service,omitempty"`
	WorkloadKind     string `json:"workload_kind,omitempty"`
	WorkloadName     string `json:"workload_name,omitempty"`
	AlertFingerprint string `json:"alert_fingerprint,omitempty"`
}

type VerificationSampleView struct {
	ID              string          `json:"id"`
	SchemaVersion   uint64          `json:"schema_version"`
	Sequence        uint64          `json:"sequence"`
	Status          string          `json:"status"`
	Observed        json.RawMessage `json:"observed"`
	SourceReference string          `json:"source_reference,omitempty"`
	ReasonCode      string          `json:"reason_code,omitempty"`
	WindowStartAt   *time.Time      `json:"window_start_at,omitempty"`
	WindowEndAt     *time.Time      `json:"window_end_at,omitempty"`
	SampledAt       time.Time       `json:"sampled_at"`
	ContentHash     string          `json:"content_hash"`
	CreatedAt       time.Time       `json:"created_at"`
}

type remediationPlanPageResponse struct {
	Items      []RemediationPlanView `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

type deliveryResponse struct {
	Resource DeliveryView `json:"resource"`
}

type verificationRunPageResponse struct {
	Items      []VerificationRunView `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}
