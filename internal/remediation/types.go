package remediation

import (
	"encoding/json"
	"time"
)

const (
	MaxPlannerJSONBytes = 16 * 1024
	MaxPlanJSONBytes    = 64 * 1024
)

type PlanStatus string

const (
	PlanDraft            PlanStatus = "draft"
	PlanAwaitingApproval PlanStatus = "awaiting_approval"
	PlanApproved         PlanStatus = "approved"
	PlanDeliveryPending  PlanStatus = "delivery_pending"
	PlanDelivering       PlanStatus = "delivering"
	PlanPRCreated        PlanStatus = "pr_created"
	PlanCIPending        PlanStatus = "ci_pending"
	PlanCIPassed         PlanStatus = "ci_passed"
	PlanCIFailed         PlanStatus = "ci_failed"
	PlanPolicyRejected   PlanStatus = "policy_rejected"
	PlanRejected         PlanStatus = "rejected"
	PlanCancelled        PlanStatus = "cancelled"
	PlanSuperseded       PlanStatus = "superseded"
)

type OperationType string

const (
	OperationRollbackImage      OperationType = "rollback_image"
	OperationSetReplicas        OperationType = "set_replicas"
	OperationRestoreRequiredEnv OperationType = "restore_required_env"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type TargetResource struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Container  string `json:"container,omitempty"`
}

type ProposedValue struct {
	ImageDigest string `json:"image_digest,omitempty"`
	Replicas    *int   `json:"replicas,omitempty"`
}

// PlannerOutput is the entire model authority surface. Delivery coordinates,
// credentials, approval and policy fields intentionally cannot be represented.
type PlannerOutput struct {
	OperationType  OperationType  `json:"operation_type"`
	TargetResource TargetResource `json:"target_resource"`
	ProposedValue  ProposedValue  `json:"proposed_value"`
	EvidenceIDs    []string       `json:"evidence_ids"`
}

type Parameters struct {
	Target        TargetResource `json:"target_resource"`
	ProposedValue ProposedValue  `json:"proposed_value"`
}

type RemediationPlan struct {
	ID                 uint64
	PublicID           string
	IncidentID         uint64
	IncidentPublicID   string
	PlanVersion        int
	PlanHash           string
	Status             PlanStatus
	OperationType      OperationType
	TargetRepository   string
	TargetBaseRevision string
	TargetPath         string
	Parameters         Parameters
	EvidenceReferences []string
	RiskLevel          RiskLevel
	PolicySnapshotHash string
	ExpectedBeforeHash string
	ProposedPatchHash  string
	PatchSummary       string
	RollbackPlan       string
	ValidationPlan     string
	RowVersion         uint64
	CreatedAt          time.Time
	UpdatedAt          time.Time

	// V3 immutable approval bindings. Legacy rows leave these fields empty
	// until the release-A conversion/cutover flow classifies them.
	CycleNo                 uint64
	IncidentVersion         uint64
	CreatedByAgentRunID     string
	DiagnosisHash           string
	HashSchemaVersion       int
	CanonicalPlanHash       string
	LastKnownGoodRevision   string
	TargetBaseBranch        string
	BaseBlobSHA             string
	FileMode                string
	TargetFieldRef          string
	ExpectedPostImageHash   string
	ExpectedTreeHash        string
	CanonicalChangeManifest json.RawMessage
	BoundedDiff             string
	PostImage               []byte
	PolicyVersion           string
	PolicySnapshot          json.RawMessage
	VerificationPlan        json.RawMessage
	VerificationPlanHash    string
	EvidenceBindings        []EvidenceBinding
	EvidenceSetHash         string
	ExpiresAt               time.Time
}

type Decision string

const (
	DecisionApproved Decision = "approved"
	DecisionRejected Decision = "rejected"
)

type Approval struct {
	ID                uint64
	PublicID          string
	PlanID            uint64
	Decision          Decision
	Actor             string
	ApprovedPlanHash  string
	ApprovedPatchHash string
	CreatedAt         time.Time

	ActorProvider             string
	Role                      string
	Reason                    string
	RequestID                 string
	RequestAuthenticatedAt    time.Time
	ExpiresAt                 time.Time
	ApprovedHashSchemaVersion int
	ApprovedBaseSHA           string
	ApprovedPostImageHash     string
	ApprovedTreeHash          string
	ApprovedPolicyHash        string
	ApprovedVerificationHash  string
	ApprovedEvidenceSetHash   string
}

type EvidenceBinding struct {
	ID          string `json:"id"`
	ContentHash string `json:"content_hash"`
}

type ChangeRequestStatus string

const (
	DeliveryPending          ChangeRequestStatus = "pending"
	DeliveryDelivering       ChangeRequestStatus = "delivering"
	DeliveryPRCreated        ChangeRequestStatus = "pr_created"
	DeliveryCIPending        ChangeRequestStatus = "ci_pending"
	DeliveryCIPassed         ChangeRequestStatus = "ci_passed"
	DeliveryMergePending     ChangeRequestStatus = "merge_pending"
	DeliveryMerged           ChangeRequestStatus = "merged"
	DeliveryArgoPending      ChangeRequestStatus = "argocd_pending"
	DeliverySyncing          ChangeRequestStatus = "syncing"
	DeliverySynced           ChangeRequestStatus = "synced"
	DeliveryRolloutPending   ChangeRequestStatus = "rollout_pending"
	DeliveryDelivered        ChangeRequestStatus = "delivered"
	DeliveryCIFailed         ChangeRequestStatus = "ci_failed"
	DeliveryPRClosed         ChangeRequestStatus = "pr_closed"
	DeliveryMergeTimeout     ChangeRequestStatus = "merge_timeout"
	DeliveryRevisionMismatch ChangeRequestStatus = "revision_mismatch"
	DeliveryArgoFailed       ChangeRequestStatus = "argocd_failed"
	DeliveryArgoTimeout      ChangeRequestStatus = "argocd_timeout"
	DeliveryRolloutFailed    ChangeRequestStatus = "rollout_failed"
	DeliveryCancelled        ChangeRequestStatus = "delivery_cancelled"
	DeliveryFailed           ChangeRequestStatus = "failed"
)

type CIStatus string

const (
	CIPending   CIStatus = "pending"
	CIPassing   CIStatus = "passing"
	CIFailing   CIStatus = "failing"
	CICancelled CIStatus = "cancelled"
)

type ChangeRequest struct {
	ID                   uint64
	PublicID             string
	PlanID               uint64
	Repository           string
	BaseRevision         string
	HeadBranch           string
	CommitSHA            string
	PRNumber             int64
	PRURL                string
	Status               ChangeRequestStatus
	CIStatus             CIStatus
	IdempotencyKey       string
	LeaseOwner           string
	LeaseExpiresAt       *time.Time
	HeartbeatAt          *time.Time
	Attempts             int
	FailureCode          string
	PRState              string
	MergedCommitSHA      string
	TargetRevision       string
	ArgoCDApplication    string
	ArgoCDProject        string
	DetectedRevision     string
	ArgoCDSyncStatus     string
	ArgoCDOperationPhase string
	ArgoCDHealthStatus   string
	ResourceHealth       json.RawMessage
	SyncStartedAt        *time.Time
	SyncCompletedAt      *time.Time
	Cluster              string
	Environment          string
	Namespace            string
	WorkloadKind         string
	WorkloadName         string
	DeploymentGeneration int64
	ObservedGeneration   int64
	RolloutRevision      string
	DesiredReplicas      int32
	UpdatedReplicas      int32
	AvailableReplicas    int32
	UnavailableReplicas  int32
	DeliveryStartedAt    *time.Time
	DeliveryDeadlineAt   *time.Time
	DeliveryCompletedAt  *time.Time
	NextPollAt           *time.Time
	LastObservedAt       *time.Time
	FailureReason        string
	RowVersion           uint64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ControlledExecutionResult is the bounded observation persisted by the
// disposable fast-demo executor after an approved Kubernetes mutation.
type ControlledExecutionResult struct {
	Revision             string
	Cluster              string
	Environment          string
	Namespace            string
	WorkloadName         string
	DeploymentGeneration int64
	ObservedGeneration   int64
	RolloutRevision      string
	DesiredReplicas      int32
	UpdatedReplicas      int32
	AvailableReplicas    int32
	UnavailableReplicas  int32
	ObservedAt           time.Time
}

type Page struct {
	Items    []RemediationPlan
	Total    int64
	Page     int
	PageSize int
}

type ListFilter struct {
	IncidentPublicID string
	Status           PlanStatus
	Page             int
	PageSize         int
}

type EvidenceFact struct {
	PublicID         string
	IncidentID       uint64
	Valid            bool
	Truncated        bool
	ConfirmedChange  bool
	RegistryVerified bool
	DeployedDigests  []string
	Facts            json.RawMessage
}
