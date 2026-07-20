package apiv3

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) listRemediationPlans(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	cursor, afterID, limit, err := parseListOptions(c.Request)
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_CURSOR", "cursor and limit parameters are invalid")
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{
		Kind: QueryRemediationPlans, IncidentID: id, Cursor: cursor, AfterID: afterID, Limit: limit,
	})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	if len(result.RemediationPlans) > limit {
		result.RemediationPlans = result.RemediationPlans[:limit]
	}
	if err := validateRemediationPlanViews(result.RemediationPlans); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the bounded Workbench contract")
		return
	}
	if err := validateWorkbenchNextCursor(result.NextCursor); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public cursor contract")
		return
	}
	h.writeJSON(c, http.StatusOK, remediationPlanPageResponse{
		Items: nonNilRemediationPlans(result.RemediationPlans), NextCursor: result.NextCursor,
	})
}

func (h *Handler) getDelivery(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{Kind: QueryDelivery, IncidentID: id, Limit: 1})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	if result.Delivery == nil {
		h.writeProblem(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource was not found")
		return
	}
	if err := validateDeliveryView(result.Delivery); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the bounded Workbench contract")
		return
	}
	h.writeJSON(c, http.StatusOK, deliveryResponse{Resource: *result.Delivery})
}

func (h *Handler) listVerifications(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	cursor, afterID, limit, err := parseListOptions(c.Request)
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_CURSOR", "cursor and limit parameters are invalid")
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{
		Kind: QueryVerifications, IncidentID: id, Cursor: cursor, AfterID: afterID, Limit: limit,
	})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	if len(result.Verifications) > limit {
		result.Verifications = result.Verifications[:limit]
	}
	if err := validateVerificationRunViews(result.Verifications); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the bounded Workbench contract")
		return
	}
	if err := validateWorkbenchNextCursor(result.NextCursor); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public cursor contract")
		return
	}
	h.writeJSON(c, http.StatusOK, verificationRunPageResponse{
		Items: nonNilVerificationRuns(result.Verifications), NextCursor: result.NextCursor,
	})
}
