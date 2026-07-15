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
	OperationRollbackImage OperationType = "rollback_image"
	OperationSetReplicas   OperationType = "set_replicas"
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
}

type ChangeRequestStatus string

const (
	DeliveryPending    ChangeRequestStatus = "pending"
	DeliveryDelivering ChangeRequestStatus = "delivering"
	DeliveryPRCreated  ChangeRequestStatus = "pr_created"
	DeliveryFailed     ChangeRequestStatus = "failed"
)

type CIStatus string

const (
	CIPending   CIStatus = "pending"
	CIPassing   CIStatus = "passing"
	CIFailing   CIStatus = "failing"
	CICancelled CIStatus = "cancelled"
)

type ChangeRequest struct {
	ID             uint64
	PublicID       string
	PlanID         uint64
	Repository     string
	BaseRevision   string
	HeadBranch     string
	CommitSHA      string
	PRNumber       int64
	PRURL          string
	Status         ChangeRequestStatus
	CIStatus       CIStatus
	IdempotencyKey string
	LeaseOwner     string
	LeaseExpiresAt *time.Time
	HeartbeatAt    *time.Time
	Attempts       int
	FailureCode    string
	RowVersion     uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
