package remediation

import (
	"context"
	"database/sql"
)

// PersistenceTX is the transaction surface shared with the durable async task
// runner. Domain effects must be written through the runner-owned
// transaction so task completion and business state cannot diverge.
type PersistenceTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Repository persists immutable plans and owner decisions.
type Repository interface {
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

// ReconciledGitHubWriter is the bounded write surface. Each Ensure method reconciles
// first and performs at most one mutating GitHub request.
type ReconciledGitHubWriter interface {
	ReconcileDraftPR(context.Context, ChangeWriteRequest) (WriteObservation, error)
	EnsureBranch(context.Context, ChangeWriteRequest) (WriteObservation, error)
	EnsureCommit(context.Context, ChangeWriteRequest) (WriteObservation, error)
	EnsureDraftPR(context.Context, ChangeWriteRequest) (WriteObservation, error)
}

type OperationStep string

const (
	OperationStepEnsureBranch  OperationStep = "ensure_branch"
	OperationStepEnsureCommit  OperationStep = "ensure_commit"
	OperationStepEnsureDraftPR OperationStep = "ensure_draft_pr"
	OperationStepComplete      OperationStep = "complete"
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

type ChangeWriteRequest struct {
	DeliveryRequest
	BaseBlobSHA           string
	ExpectedBeforeHash    string
	ExpectedPostImageHash string
	ExpectedTreeHash      string
	LogicalOperationKey   string
}

type WriteObservation struct {
	Step       OperationStep
	BaseSHA    string
	BranchSHA  string
	CommitSHA  string
	TreeSHA    string
	PRNumber   int64
	PRURL      string
	Reconciled bool
}
