package remediation

import (
	"context"
	"database/sql"
	"time"
)

type Repository interface {
	CreatePlan(context.Context, *RemediationPlan) error
	GetPlan(context.Context, string) (*RemediationPlan, error)
	GetApproval(context.Context, string) (*Approval, error)
	ListPlans(context.Context, ListFilter) (Page, error)
	ApprovePlan(context.Context, string, uint64, Approval, *ChangeRequest) (*RemediationPlan, *ChangeRequest, error)
	RejectPlan(context.Context, string, uint64, Approval) (*RemediationPlan, error)
	CreateDelivery(context.Context, *ChangeRequest) error
	ClaimDelivery(context.Context, string, time.Time, time.Duration) (*ChangeRequest, *RemediationPlan, error)
	ReleaseDelivery(context.Context, uint64, uint64, string, string) error
	MarkPRCreated(context.Context, uint64, uint64, string, string, int64, string) error
	UpdateCI(context.Context, uint64, uint64, CIStatus) error
}

// PersistenceTX is the transaction surface shared with the durable async task
// runner. V3 domain effects must be written through the runner-owned
// transaction so task completion and business state cannot diverge.
type PersistenceTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// V3Repository is intentionally separate from the compatibility Repository.
// In particular, V3 decisions are never stored as legacy approvals.
type V3Repository interface {
	CreatePlan(context.Context, *RemediationPlan) error
	CreatePlanIn(context.Context, PersistenceTX, *RemediationPlan) error
	GetPlan(context.Context, string) (*RemediationPlan, error)
	ResolveAgentRunID(context.Context, PersistenceTX, string, uint64, uint64) (uint64, error)
	RecordDecision(context.Context, string, uint64, *Approval) error
	RecordDecisionIn(context.Context, PersistenceTX, string, uint64, *Approval) error
	GetDecision(context.Context, string) (*Approval, error)
}

type GitHubWriter interface {
	ReadBaseFile(context.Context, string, string, string) ([]byte, error)
	DeliverDraftPR(context.Context, DeliveryRequest) (DeliveryResult, error)
	ReadCI(context.Context, string, string) (CIStatus, error)
}

// PhasedGitHubWriter is the V3 write surface. Each Ensure method reconciles
// first and performs at most one mutating GitHub request.
type PhasedGitHubWriter interface {
	ReconcileDraftPR(context.Context, PhasedDeliveryRequest) (WriteObservation, error)
	EnsureBranch(context.Context, PhasedDeliveryRequest) (WriteObservation, error)
	EnsureCommit(context.Context, PhasedDeliveryRequest) (WriteObservation, error)
	EnsureDraftPR(context.Context, PhasedDeliveryRequest) (WriteObservation, error)
}

type WritePhase string

const (
	WritePhaseEnsureBranch  WritePhase = "ensure_branch"
	WritePhaseEnsureCommit  WritePhase = "ensure_commit"
	WritePhaseEnsureDraftPR WritePhase = "ensure_draft_pr"
	WritePhaseComplete      WritePhase = "complete"
)

type DeliveryRequest struct {
	Repository   string
	BaseRevision string
	BaseBranch   string
	Path         string
	Content      []byte
	Branch       string
	CommitTitle  string
	PRTitle      string
	PRBody       string
	Marker       string
}

type DeliveryResult struct {
	CommitSHA string
	PRNumber  int64
	PRURL     string
}

type PhasedDeliveryRequest struct {
	DeliveryRequest
	BaseBlobSHA           string
	ExpectedBeforeHash    string
	ExpectedPostImageHash string
	ExpectedTreeHash      string
	LogicalOperationKey   string
}

type WriteObservation struct {
	Phase      WritePhase
	BaseSHA    string
	BranchSHA  string
	CommitSHA  string
	TreeSHA    string
	PRNumber   int64
	PRURL      string
	Reconciled bool
}
