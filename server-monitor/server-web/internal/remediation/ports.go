package remediation

import (
	"context"
	"time"
)

type Repository interface {
	CreatePlan(context.Context, *RemediationPlan) error
	GetPlan(context.Context, string) (*RemediationPlan, error)
	ListPlans(context.Context, ListFilter) (Page, error)
	ApprovePlan(context.Context, string, uint64, Approval, *ChangeRequest) (*RemediationPlan, *ChangeRequest, error)
	RejectPlan(context.Context, string, uint64, Approval) (*RemediationPlan, error)
	CreateDelivery(context.Context, *ChangeRequest) error
	ClaimDelivery(context.Context, string, time.Time, time.Duration) (*ChangeRequest, *RemediationPlan, error)
	ReleaseDelivery(context.Context, uint64, uint64, string, string) error
	MarkPRCreated(context.Context, uint64, uint64, string, string, int64, string) error
	UpdateCI(context.Context, uint64, uint64, CIStatus) error
}

type GitHubWriter interface {
	ReadBaseFile(context.Context, string, string, string) ([]byte, error)
	DeliverDraftPR(context.Context, DeliveryRequest) (DeliveryResult, error)
	ReadCI(context.Context, string, string) (CIStatus, error)
}

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
