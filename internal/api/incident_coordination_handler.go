package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *Handler) listIncidentAlertRelations(c *gin.Context) {
	h.listIncidentCoordinationCollection(c, QueryAlertRelations)
}

func (h *Handler) listIncidentTimeline(c *gin.Context) {
	h.listIncidentCoordinationCollection(c, QueryTimeline)
}

func (h *Handler) listIncidentEvidence(c *gin.Context) {
	h.listIncidentCoordinationCollection(c, QueryEvidence)
}

func (h *Handler) listIncidentInvestigations(c *gin.Context) {
	h.listIncidentCoordinationCollection(c, QueryInvestigations)
}

func (h *Handler) listIncidentCoordinationCollection(c *gin.Context, kind QueryKind) {
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
		Kind: kind, IncidentID: id, Cursor: cursor, AfterID: afterID, Limit: limit,
	})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	switch kind {
	case QueryAlertRelations:
		if len(result.AlertRelations) > limit {
			result.AlertRelations = result.AlertRelations[:limit]
		}
		if err := validateIncidentAlertRelations(result.AlertRelations); err != nil {
			h.invalidIncidentCoordinationProjection(c)
			return
		}
		h.writeJSON(c, http.StatusOK, collectionResponse[IncidentAlertRelationView]{
			Items: nonNilAlertRelations(result.AlertRelations), NextCursor: result.NextCursor,
		})
	case QueryTimeline:
		if len(result.Timeline) > limit {
			result.Timeline = result.Timeline[:limit]
		}
		if err := validateIncidentTimeline(result.Timeline); err != nil {
			h.invalidIncidentCoordinationProjection(c)
			return
		}
		h.writeJSON(c, http.StatusOK, collectionResponse[IncidentTimelineEventView]{
			Items: nonNilTimeline(result.Timeline), NextCursor: result.NextCursor,
		})
	case QueryEvidence:
		if len(result.Evidence) > limit {
			result.Evidence = result.Evidence[:limit]
		}
		if err := validateIncidentEvidence(result.Evidence); err != nil {
			h.invalidIncidentCoordinationProjection(c)
			return
		}
		h.writeJSON(c, http.StatusOK, collectionResponse[IncidentEvidenceView]{
			Items: nonNilEvidence(result.Evidence), NextCursor: result.NextCursor,
		})
	case QueryInvestigations:
		if len(result.Investigations) > limit {
			result.Investigations = result.Investigations[:limit]
		}
		if err := validateIncidentInvestigations(result.Investigations); err != nil {
			h.invalidIncidentCoordinationProjection(c)
			return
		}
		h.writeJSON(c, http.StatusOK, collectionResponse[IncidentInvestigationView]{
			Items: nonNilInvestigations(result.Investigations), NextCursor: result.NextCursor,
		})
	default:
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "unsupported Incident coordination projection")
	}
}

func (h *Handler) getIncidentDecision(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{
		Kind: QueryDecision, IncidentID: id, Limit: 1,
	})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	if result.Decision != nil {
		if err := validateIncidentDecision(result.Decision); err != nil {
			h.invalidIncidentCoordinationProjection(c)
			return
		}
	}
	h.writeJSON(c, http.StatusOK, struct {
		Decision *IncidentDecisionView `json:"decision"`
	}{Decision: result.Decision})
}

func (h *Handler) invalidIncidentCoordinationProjection(c *gin.Context) {
	h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "Incident coordination projection violated its typed contract")
}

func validateIncidentAlertRelations(items []IncidentAlertRelationView) error {
	for index := range items {
		item := &items[index]
		id, err := ParsePublicUUID(item.ID)
		if err != nil {
			return err
		}
		alertID, err := ParsePublicUUID(item.AlertID)
		if err != nil || item.Cycle == 0 || !validSeverity(item.Severity) ||
			(item.Status != "firing" && item.Status != "resolved") ||
			len(item.Summary) > 2048 || item.CreatedAt.IsZero() {
			return ErrInvalidArgument
		}
		if err := validateIncidentContextLink(&item.ContextLink, true); err != nil {
			return err
		}
		item.ID = id
		item.AlertID = alertID
	}
	return nil
}

func validateIncidentTimeline(items []IncidentTimelineEventView) error {
	for index := range items {
		item := &items[index]
		id, err := ParsePublicUUID(item.ID)
		if err != nil || item.Cycle == 0 || strings.TrimSpace(item.Type) == "" ||
			len(item.Type) > 64 || len(item.Summary) > 2048 || item.OccurredAt.IsZero() ||
			len(item.Metadata) == 0 || !json.Valid(item.Metadata) {
			return ErrInvalidArgument
		}
		item.ID = id
	}
	return nil
}

func validateIncidentEvidence(items []IncidentEvidenceView) error {
	for index := range items {
		item := &items[index]
		id, err := ParsePublicUUID(item.ID)
		if err != nil || item.Cycle == 0 || strings.TrimSpace(item.Type) == "" ||
			strings.TrimSpace(item.Source) == "" || len(item.Summary) > 4096 ||
			len(item.ResourceRef) > 1024 || item.CollectedAt.IsZero() {
			return ErrInvalidArgument
		}
		if item.ContentHash != "" && validateExpectedHash(item.ContentHash) != nil {
			return ErrInvalidArgument
		}
		for _, raw := range []json.RawMessage{item.TimeRange, item.Provenance} {
			if len(raw) > 0 && !json.Valid(raw) {
				return ErrInvalidArgument
			}
		}
		if err := validateIncidentContextLink(&item.ContextLink, true); err != nil {
			return err
		}
		item.ID = id
	}
	return nil
}

func validateIncidentInvestigations(items []IncidentInvestigationView) error {
	for index := range items {
		item := &items[index]
		id, err := ParsePublicUUID(item.ID)
		if err != nil || item.Cycle == 0 || item.Version == 0 ||
			(item.Status != "pending" && item.Status != "running" && item.Status != "completed" &&
				item.Status != "failed" && item.Status != "cancelled") ||
			len(item.Objective) > 2048 || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() {
			return ErrInvalidArgument
		}
		if err := validateIncidentContextLink(&item.ContextLink, true); err != nil {
			return err
		}
		item.ID = id
	}
	return nil
}

func validateIncidentDecision(item *IncidentDecisionView) error {
	if item == nil || item.Cycle == 0 ||
		(item.Kind != "pending" && item.Kind != "no_change" && item.Kind != "action" && item.Kind != "recovery") ||
		strings.TrimSpace(item.Status) == "" || len(item.Summary) > 2048 {
		return ErrInvalidArgument
	}
	for _, value := range []*string{
		&item.InvestigationID, &item.RemediationPlanID, &item.DecisionID,
		&item.DeliveryID, &item.VerificationID,
	} {
		if *value == "" {
			continue
		}
		id, err := ParsePublicUUID(*value)
		if err != nil {
			return err
		}
		*value = id
	}
	if item.ContextLink != nil {
		if err := validateIncidentContextLink(item.ContextLink, true); err != nil {
			return err
		}
	}
	return nil
}

func validateIncidentContextLink(item *IncidentContextLinkView, allowEmpty bool) error {
	if item == nil {
		return ErrInvalidArgument
	}
	if allowEmpty && item.Workspace == "" && item.Path == "" && item.OperationalScopeID == "" {
		return nil
	}
	paths := map[string]string{
		"monitoring": "/monitoring", "logs": "/logs", "traces": "/traces",
		"agent": "/agent", "alerts": "/alerts", "devops": "/devops",
	}
	prefix, ok := paths[item.Workspace]
	if !ok || item.External || (item.Path != prefix && !strings.HasPrefix(item.Path, prefix+"/")) {
		return ErrInvalidArgument
	}
	scopeID, err := ParsePublicUUID(item.OperationalScopeID)
	if err != nil {
		return err
	}
	if item.Query == nil || len(item.Query) > 16 {
		return ErrInvalidArgument
	}
	for key, value := range item.Query {
		if key == "" || len(key) > 64 || len(value) > 1024 || containsControl(key+value) {
			return ErrInvalidArgument
		}
	}
	item.OperationalScopeID = scopeID
	return nil
}

func nonNilAlertRelations(items []IncidentAlertRelationView) []IncidentAlertRelationView {
	if items == nil {
		return []IncidentAlertRelationView{}
	}
	return items
}

func nonNilTimeline(items []IncidentTimelineEventView) []IncidentTimelineEventView {
	if items == nil {
		return []IncidentTimelineEventView{}
	}
	return items
}

func nonNilEvidence(items []IncidentEvidenceView) []IncidentEvidenceView {
	if items == nil {
		return []IncidentEvidenceView{}
	}
	return items
}

func nonNilInvestigations(items []IncidentInvestigationView) []IncidentInvestigationView {
	if items == nil {
		return []IncidentInvestigationView{}
	}
	return items
}
