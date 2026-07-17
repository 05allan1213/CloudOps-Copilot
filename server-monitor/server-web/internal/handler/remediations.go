package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"server-web/internal/middleware"
	"server-web/internal/remediation"
)

type RemediationApplication interface {
	Enabled() bool
	List(context.Context, remediation.ListFilter) (remediation.Page, error)
	Get(context.Context, string) (*remediation.RemediationPlan, error)
	GetApproval(context.Context, string) (*remediation.Approval, error)
	Approve(context.Context, string, string, string, string, string, uint64) (*remediation.RemediationPlan, *remediation.ChangeRequest, error)
	Reject(context.Context, string, string, string, string, string, uint64) (*remediation.RemediationPlan, error)
}

func (h *Handler) SetRemediation(service RemediationApplication) { h.remediation = service }

type remediationDTO struct {
	ID                 string                    `json:"id"`
	IncidentID         string                    `json:"incident_id"`
	PlanVersion        int                       `json:"plan_version"`
	PlanHash           string                    `json:"plan_hash"`
	Status             remediation.PlanStatus    `json:"status"`
	OperationType      remediation.OperationType `json:"operation_type"`
	TargetRepository   string                    `json:"target_repository"`
	TargetBaseRevision string                    `json:"target_base_revision"`
	TargetPath         string                    `json:"target_path"`
	Parameters         remediation.Parameters    `json:"parameters"`
	EvidenceReferences []string                  `json:"evidence_references"`
	RiskLevel          remediation.RiskLevel     `json:"risk_level"`
	PolicySnapshotHash string                    `json:"policy_snapshot_hash"`
	ExpectedBeforeHash string                    `json:"expected_before_hash"`
	ProposedPatchHash  string                    `json:"proposed_patch_hash"`
	PatchSummary       string                    `json:"patch_summary"`
	RollbackPlan       string                    `json:"rollback_plan"`
	ValidationPlan     string                    `json:"validation_plan"`
	Version            uint64                    `json:"version"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}

type approvalRequest struct {
	PlanHash  string `json:"plan_hash"`
	PatchHash string `json:"patch_hash"`
	Version   uint64 `json:"version"`
}

func (h *Handler) ListRemediations(c *gin.Context) {
	if !h.requireRemediation(c) {
		return
	}
	page, err := strconv.Atoi(defaultQuery(c.Query("page"), "1"))
	if err != nil {
		writeRemediationError(c, remediation.ErrInvalidArgument)
		return
	}
	pageSize, err := strconv.Atoi(defaultQuery(c.Query("page_size"), "20"))
	if err != nil {
		writeRemediationError(c, remediation.ErrInvalidArgument)
		return
	}
	result, err := h.remediation.List(c.Request.Context(), remediation.ListFilter{IncidentPublicID: c.Query("incident_id"), Status: remediation.PlanStatus(c.Query("status")), Page: page, PageSize: pageSize})
	if err != nil {
		writeRemediationError(c, err)
		return
	}
	items := make([]remediationDTO, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toRemediationDTO(&result.Items[i]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": result.Total, "page": result.Page, "page_size": result.PageSize}})
}

func (h *Handler) GetRemediation(c *gin.Context) {
	if !h.requireRemediationID(c) || !h.requireRemediation(c) {
		return
	}
	plan, err := h.remediation.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeRemediationError(c, err)
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: toRemediationDTO(plan)})
}

func (h *Handler) ApproveRemediation(c *gin.Context) { h.decideRemediation(c, true) }

func (h *Handler) RejectRemediation(c *gin.Context) { h.decideRemediation(c, false) }

func (h *Handler) decideRemediation(c *gin.Context, approve bool) {
	if !h.requireRemediationID(c) || !h.requireRemediation(c) {
		return
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request approvalRequest
	if decoder.Decode(&request) != nil || len(request.PlanHash) != 64 || len(request.PatchHash) != 64 || request.Version == 0 {
		writeRemediationError(c, remediation.ErrInvalidArgument)
		return
	}
	actor, role := "", ""
	if value, exists := c.Get(middleware.ContextUsername); exists {
		actor, _ = value.(string)
	}
	if value, exists := c.Get(middleware.ContextRole); exists {
		role, _ = value.(string)
	}
	if strings.TrimSpace(actor) == "" && h.fastDemo != nil && h.fastDemoActor != "" {
		actor, role = h.fastDemoActor, "admin"
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(role) == "" {
		writeRemediationError(c, remediation.ErrForbidden)
		return
	}
	if approve {
		plan, delivery, err := h.remediation.Approve(c.Request.Context(), c.Param("id"), actor, role, request.PlanHash, request.PatchHash, request.Version)
		if err != nil {
			writeRemediationError(c, err)
			return
		}
		c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"plan": toRemediationDTO(plan), "change_request_id": delivery.PublicID, "delivery_status": delivery.Status}})
		return
	}
	plan, err := h.remediation.Reject(c.Request.Context(), c.Param("id"), actor, role, request.PlanHash, request.PatchHash, request.Version)
	if err != nil {
		writeRemediationError(c, err)
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: toRemediationDTO(plan)})
}

func (h *Handler) requireRemediation(c *gin.Context) bool {
	if h.remediation == nil || !h.remediation.Enabled() {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "remediation disabled"})
		return false
	}
	return true
}

func (h *Handler) requireRemediationID(c *gin.Context) bool {
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		writeRemediationError(c, remediation.ErrInvalidArgument)
		return false
	}
	return true
}

func toRemediationDTO(plan *remediation.RemediationPlan) remediationDTO {
	return remediationDTO{ID: plan.PublicID, IncidentID: plan.IncidentPublicID, PlanVersion: plan.PlanVersion, PlanHash: plan.PlanHash, Status: plan.Status, OperationType: plan.OperationType, TargetRepository: plan.TargetRepository, TargetBaseRevision: plan.TargetBaseRevision, TargetPath: plan.TargetPath, Parameters: plan.Parameters, EvidenceReferences: append([]string(nil), plan.EvidenceReferences...), RiskLevel: plan.RiskLevel, PolicySnapshotHash: plan.PolicySnapshotHash, ExpectedBeforeHash: plan.ExpectedBeforeHash, ProposedPatchHash: plan.ProposedPatchHash, PatchSummary: plan.PatchSummary, RollbackPlan: plan.RollbackPlan, ValidationPlan: plan.ValidationPlan, Version: plan.RowVersion, CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt}
}

func writeRemediationError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "remediation request failed"
	switch {
	case errors.Is(err, remediation.ErrInvalidArgument):
		status, message = http.StatusBadRequest, "invalid remediation request"
	case errors.Is(err, remediation.ErrNotFound):
		status, message = http.StatusNotFound, "remediation not found"
	case errors.Is(err, remediation.ErrForbidden):
		status, message = http.StatusForbidden, "remediation forbidden"
	case errors.Is(err, remediation.ErrApprovalMismatch), errors.Is(err, remediation.ErrConflict), errors.Is(err, remediation.ErrInvalidTransition):
		status, message = http.StatusConflict, "remediation approval conflict"
	case errors.Is(err, remediation.ErrPolicyRejected):
		status, message = http.StatusUnprocessableEntity, "remediation policy rejected"
	}
	c.JSON(status, response{Status: "error", Error: message})
}

func defaultQuery(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
