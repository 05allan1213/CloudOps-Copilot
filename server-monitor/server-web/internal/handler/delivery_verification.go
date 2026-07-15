package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"server-web/internal/verification"
)

type DeliveryVerificationApplication interface {
	DeliveryEnabled() bool
	VerificationEnabled() bool
	GetDelivery(context.Context, string) (*verification.Delivery, error)
	ListRuns(context.Context, string, int, int) (verification.RunPage, error)
	GetRun(context.Context, string, string) (*verification.Run, []verification.Check, error)
}

func (h *Handler) SetDeliveryVerification(service DeliveryVerificationApplication) {
	h.deliveryVerification = service
}

type deliveryDTO struct {
	ID                   string     `json:"id"`
	IncidentID           string     `json:"incident_id"`
	RemediationPlanID    string     `json:"remediation_plan_id"`
	Repository           string     `json:"repository"`
	PullRequest          int64      `json:"pull_request"`
	PullRequestURL       string     `json:"pull_request_url"`
	Status               string     `json:"status"`
	CIStatus             string     `json:"ci_status"`
	PRState              string     `json:"pr_state"`
	HeadCommitSHA        string     `json:"head_commit_sha"`
	MergedCommitSHA      string     `json:"merged_commit_sha"`
	TargetRevision       string     `json:"target_revision"`
	ArgoApplication      string     `json:"argocd_application"`
	DetectedRevision     string     `json:"detected_revision"`
	ArgoSyncStatus       string     `json:"argocd_sync_status"`
	ArgoOperationPhase   string     `json:"argocd_operation_phase"`
	ArgoHealthStatus     string     `json:"argocd_health_status"`
	Cluster              string     `json:"cluster"`
	Environment          string     `json:"environment"`
	Namespace            string     `json:"namespace"`
	WorkloadKind         string     `json:"workload_kind"`
	WorkloadName         string     `json:"workload_name"`
	DeploymentGeneration int64      `json:"deployment_generation"`
	ObservedGeneration   int64      `json:"observed_generation"`
	DesiredReplicas      int32      `json:"desired_replicas"`
	UpdatedReplicas      int32      `json:"updated_replicas"`
	AvailableReplicas    int32      `json:"available_replicas"`
	UnavailableReplicas  int32      `json:"unavailable_replicas"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	DeadlineAt           *time.Time `json:"deadline_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	FailureReason        string     `json:"failure_reason,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type verificationRunDTO struct {
	ID             string                 `json:"id"`
	IncidentID     string                 `json:"incident_id"`
	Status         verification.RunStatus `json:"status"`
	TargetRevision string                 `json:"target_revision"`
	Attempt        int                    `json:"attempt"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	DeadlineAt     time.Time              `json:"deadline_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	ResultSummary  string                 `json:"result_summary,omitempty"`
	FailureReason  string                 `json:"failure_reason,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type verificationCheckDTO struct {
	ID              string                   `json:"id"`
	Type            verification.CheckType   `json:"type"`
	Status          verification.CheckStatus `json:"status"`
	Required        bool                     `json:"required"`
	Subject         verification.Subject     `json:"subject"`
	Expected        any                      `json:"expected"`
	Observed        any                      `json:"observed,omitempty"`
	SourceReference string                   `json:"source_reference,omitempty"`
	FirstCheckedAt  *time.Time               `json:"first_checked_at,omitempty"`
	LastCheckedAt   *time.Time               `json:"last_checked_at,omitempty"`
	PassedAt        *time.Time               `json:"passed_at,omitempty"`
	AttemptCount    int                      `json:"attempt_count"`
	FailureReason   string                   `json:"failure_reason,omitempty"`
}

func (h *Handler) GetIncidentDelivery(c *gin.Context) {
	if !validPublicID(c.Param("id")) {
		writeDeliveryVerificationError(c, verification.ErrInvalidArgument)
		return
	}
	if h.deliveryVerification == nil || !h.deliveryVerification.DeliveryEnabled() {
		writeDeliveryVerificationError(c, verification.ErrUnavailable)
		return
	}
	item, err := h.deliveryVerification.GetDelivery(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDeliveryVerificationError(c, err)
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: toDeliveryDTO(item)})
}

func (h *Handler) ListIncidentVerifications(c *gin.Context) {
	if !validPublicID(c.Param("id")) {
		writeDeliveryVerificationError(c, verification.ErrInvalidArgument)
		return
	}
	if h.deliveryVerification == nil || !h.deliveryVerification.VerificationEnabled() {
		writeDeliveryVerificationError(c, verification.ErrUnavailable)
		return
	}
	page, err := strconv.Atoi(defaultQuery(c.Query("page"), "1"))
	if err != nil {
		writeDeliveryVerificationError(c, verification.ErrInvalidArgument)
		return
	}
	pageSize, err := strconv.Atoi(defaultQuery(c.Query("page_size"), "20"))
	if err != nil {
		writeDeliveryVerificationError(c, verification.ErrInvalidArgument)
		return
	}
	result, err := h.deliveryVerification.ListRuns(c.Request.Context(), c.Param("id"), page, pageSize)
	if err != nil {
		writeDeliveryVerificationError(c, err)
		return
	}
	items := make([]verificationRunDTO, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toVerificationRunDTO(&result.Items[i]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": result.Total, "page": result.Page, "page_size": result.PageSize}})
}

func (h *Handler) GetIncidentVerification(c *gin.Context) {
	if !validPublicID(c.Param("id")) || !validPublicID(c.Param("verification_id")) {
		writeDeliveryVerificationError(c, verification.ErrInvalidArgument)
		return
	}
	if h.deliveryVerification == nil || !h.deliveryVerification.VerificationEnabled() {
		writeDeliveryVerificationError(c, verification.ErrUnavailable)
		return
	}
	run, checks, err := h.deliveryVerification.GetRun(c.Request.Context(), c.Param("id"), c.Param("verification_id"))
	if err != nil {
		writeDeliveryVerificationError(c, err)
		return
	}
	checkDTOs := make([]verificationCheckDTO, 0, len(checks))
	for i := range checks {
		checkDTOs = append(checkDTOs, toVerificationCheckDTO(&checks[i]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"verification": toVerificationRunDTO(run), "checks": checkDTOs}})
}

func toDeliveryDTO(item *verification.Delivery) deliveryDTO {
	return deliveryDTO{ID: item.PublicID, IncidentID: item.IncidentPublicID, RemediationPlanID: item.RemediationPlanPublicID, Repository: item.Repository, PullRequest: item.PRNumber, PullRequestURL: item.PRURL, Status: item.Status, CIStatus: item.CIStatus, PRState: item.PRState, HeadCommitSHA: item.HeadCommitSHA, MergedCommitSHA: item.MergedCommitSHA, TargetRevision: item.TargetRevision, ArgoApplication: item.ArgoApplication, DetectedRevision: item.DetectedRevision, ArgoSyncStatus: item.ArgoSyncStatus, ArgoOperationPhase: item.ArgoOperationPhase, ArgoHealthStatus: item.ArgoHealthStatus, Cluster: item.Cluster, Environment: item.Environment, Namespace: item.Namespace, WorkloadKind: item.WorkloadKind, WorkloadName: item.WorkloadName, DeploymentGeneration: item.DeploymentGeneration, ObservedGeneration: item.ObservedGeneration, DesiredReplicas: item.DesiredReplicas, UpdatedReplicas: item.UpdatedReplicas, AvailableReplicas: item.AvailableReplicas, UnavailableReplicas: item.UnavailableReplicas, StartedAt: item.DeliveryStartedAt, DeadlineAt: item.DeliveryDeadlineAt, CompletedAt: item.DeliveryCompletedAt, FailureReason: item.FailureReason, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
func toVerificationRunDTO(item *verification.Run) verificationRunDTO {
	return verificationRunDTO{ID: item.PublicID, IncidentID: item.IncidentPublicID, Status: item.Status, TargetRevision: item.TargetRevision, Attempt: item.Attempt, StartedAt: item.StartedAt, DeadlineAt: item.DeadlineAt, CompletedAt: item.CompletedAt, ResultSummary: item.ResultSummary, FailureReason: item.FailureReason, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
func toVerificationCheckDTO(item *verification.Check) verificationCheckDTO {
	var expected, observed any
	_ = json.Unmarshal(item.Expected, &expected)
	_ = json.Unmarshal(item.Observed, &observed)
	return verificationCheckDTO{ID: item.PublicID, Type: item.Type, Status: item.Status, Required: item.Required, Subject: item.Subject, Expected: expected, Observed: observed, SourceReference: item.SourceReference, FirstCheckedAt: item.FirstCheckedAt, LastCheckedAt: item.LastCheckedAt, PassedAt: item.PassedAt, AttemptCount: item.AttemptCount, FailureReason: item.FailureReason}
}
func validPublicID(value string) bool { _, err := uuid.Parse(value); return err == nil }
func writeDeliveryVerificationError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "delivery verification request failed"
	switch {
	case errors.Is(err, verification.ErrInvalidArgument):
		status, message = http.StatusBadRequest, "invalid delivery verification request"
	case errors.Is(err, verification.ErrNotFound):
		status, message = http.StatusNotFound, "delivery verification not found"
	case errors.Is(err, verification.ErrConflict), errors.Is(err, verification.ErrInvalidTransition):
		status, message = http.StatusConflict, "delivery verification conflict"
	case errors.Is(err, verification.ErrNotAllowed):
		status, message = http.StatusForbidden, "delivery verification forbidden"
	case errors.Is(err, verification.ErrUnavailable):
		status, message = http.StatusServiceUnavailable, "delivery verification unavailable"
	}
	c.JSON(status, response{Status: "error", Error: message})
}
