package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"server-web/internal/agent"
	"server-web/internal/change"
	domain "server-web/internal/incident"
	"server-web/internal/middleware"
	"server-web/internal/remediation"
	"server-web/internal/verification"
)

const (
	workbenchMaxPageSize = 50
	workbenchMaxSnapshot = 200
)

type workbenchSectionSummary struct {
	Availability string `json:"availability"`
	Status       string `json:"status"`
	ID           string `json:"id,omitempty"`
}

type workbenchSummaryDTO struct {
	Investigation workbenchSectionSummary `json:"investigation"`
	Approval      workbenchSectionSummary `json:"approval"`
	Delivery      workbenchSectionSummary `json:"delivery"`
	Verification  workbenchSectionSummary `json:"verification"`
	Postmortem    workbenchSectionSummary `json:"postmortem"`
}

type workbenchIncidentDTO struct {
	ID                string              `json:"id"`
	Title             string              `json:"title"`
	Severity          domain.Severity     `json:"severity"`
	Status            domain.Status       `json:"status"`
	Cluster           string              `json:"cluster"`
	Service           string              `json:"service"`
	Environment       string              `json:"environment"`
	Namespace         string              `json:"namespace"`
	WorkloadKind      string              `json:"workload_kind"`
	WorkloadName      string              `json:"workload_name"`
	TriggeringSummary string              `json:"triggering_summary"`
	FirstSeenAt       time.Time           `json:"first_seen_at"`
	LastSeenAt        time.Time           `json:"last_seen_at"`
	ResolvedAt        *time.Time          `json:"resolved_at,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
	Summary           workbenchSummaryDTO `json:"summary"`
}

type workbenchSignalDTO struct {
	Source     string              `json:"source"`
	Status     domain.SignalStatus `json:"status"`
	Severity   domain.Severity     `json:"severity"`
	Category   string              `json:"category"`
	Summary    string              `json:"summary"`
	OccurredAt time.Time           `json:"occurred_at"`
	ReceivedAt time.Time           `json:"received_at"`
}

type workbenchTimelineDTO struct {
	Key        string           `json:"key"`
	EventType  domain.EventType `json:"event_type"`
	ActorType  domain.ActorType `json:"actor_type"`
	Summary    string           `json:"summary"`
	OccurredAt time.Time        `json:"occurred_at"`
}

type workbenchEvidenceDTO struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Source        string    `json:"source"`
	ResourceRef   string    `json:"resource_ref,omitempty"`
	Summary       string    `json:"summary"`
	State         string    `json:"state"`
	DataFreshness string    `json:"data_freshness"`
	RelatedClaim  string    `json:"related_claim"`
	Truncated     bool      `json:"truncated"`
	CollectedAt   time.Time `json:"collected_at"`
}

type workbenchAgentRunDTO struct {
	ID                string           `json:"id"`
	Attempt           int              `json:"attempt"`
	Status            agent.RunStatus  `json:"status"`
	UsedSteps         int              `json:"used_steps"`
	MaxSteps          int              `json:"max_steps"`
	UsedToolCalls     int              `json:"used_tool_calls"`
	MaxToolCalls      int              `json:"max_tool_calls"`
	UsedEvidenceItems int              `json:"used_evidence_items"`
	MaxEvidenceItems  int              `json:"max_evidence_items"`
	TerminationReason agent.ErrorCode  `json:"termination_reason,omitempty"`
	FailureSummary    string           `json:"failure_summary,omitempty"`
	Diagnosis         *agent.Diagnosis `json:"diagnosis,omitempty"`
	StartedAt         *time.Time       `json:"started_at,omitempty"`
	FinishedAt        *time.Time       `json:"finished_at,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
}

type workbenchAgentStepDTO struct {
	ID            string           `json:"id"`
	Sequence      int              `json:"sequence"`
	Type          agent.Node       `json:"type"`
	Status        agent.StepStatus `json:"status"`
	Summary       string           `json:"summary"`
	TypedTool     string           `json:"typed_tool,omitempty"`
	EvidenceID    string           `json:"evidence_id,omitempty"`
	RetryCount    int              `json:"retry_count"`
	DurationMS    int64            `json:"duration_ms"`
	FailureReason agent.ErrorCode  `json:"failure_reason,omitempty"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	FinishedAt    *time.Time       `json:"finished_at,omitempty"`
}

type workbenchAgentEvidenceDTO struct {
	ID            string    `json:"id"`
	TypedTool     string    `json:"typed_tool"`
	ResourceScope string    `json:"resource_scope,omitempty"`
	Summary       string    `json:"summary"`
	State         string    `json:"state"`
	Truncated     bool      `json:"truncated"`
	CollectedAt   time.Time `json:"collected_at"`
}

type workbenchRemediationDTO struct {
	ID                string                     `json:"id"`
	Status            remediation.PlanStatus     `json:"status"`
	OperationType     remediation.OperationType  `json:"operation_type"`
	Target            remediation.TargetResource `json:"target"`
	ProposedValue     remediation.ProposedValue  `json:"proposed_value"`
	RiskLevel         remediation.RiskLevel      `json:"risk_level"`
	PatchSummary      string                     `json:"patch_summary"`
	RollbackPlan      string                     `json:"rollback_plan"`
	ValidationPlan    string                     `json:"validation_plan"`
	ApprovalActor     string                     `json:"approval_actor"`
	ApprovalDecidedAt *time.Time                 `json:"approval_decided_at,omitempty"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

type workbenchDeliveryDTO struct {
	ID                   string     `json:"id"`
	Status               string     `json:"status"`
	CIStatus             string     `json:"ci_status"`
	PRState              string     `json:"pr_state"`
	PullRequest          int64      `json:"pull_request"`
	PullRequestURL       string     `json:"pull_request_url"`
	HeadCommitSHA        string     `json:"head_commit_sha"`
	MergedCommitSHA      string     `json:"merged_commit_sha"`
	TargetRevision       string     `json:"target_revision"`
	ArgoApplication      string     `json:"argocd_application"`
	DetectedRevision     string     `json:"detected_revision"`
	ArgoSyncStatus       string     `json:"argocd_sync_status"`
	ArgoOperationPhase   string     `json:"argocd_operation_phase"`
	ArgoHealthStatus     string     `json:"argocd_health_status"`
	ImageDigest          string     `json:"image_digest,omitempty"`
	ProvenanceStatus     string     `json:"provenance_status"`
	Attempts             int        `json:"attempts"`
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
	UpdatedAt            time.Time  `json:"updated_at"`
}

type workbenchVerificationCheckDTO struct {
	ID                       string                   `json:"id"`
	Type                     verification.CheckType   `json:"type"`
	TemplateID               string                   `json:"template_id,omitempty"`
	Required                 bool                     `json:"required"`
	Status                   verification.CheckStatus `json:"status"`
	Comparison               verification.Comparison  `json:"comparison,omitempty"`
	Threshold                float64                  `json:"threshold,omitempty"`
	Observed                 map[string]any           `json:"observed,omitempty"`
	StabilityWindowSeconds   int64                    `json:"stability_window_seconds"`
	StabilityProgressSeconds int64                    `json:"stability_progress_seconds"`
	TimeoutSeconds           int64                    `json:"timeout_seconds"`
	AttemptCount             int                      `json:"attempt_count"`
	FailureReason            string                   `json:"failure_reason,omitempty"`
	FirstCheckedAt           *time.Time               `json:"first_checked_at,omitempty"`
	LastCheckedAt            *time.Time               `json:"last_checked_at,omitempty"`
	PassedAt                 *time.Time               `json:"passed_at,omitempty"`
}

func (h *Handler) ListWorkbenchIncidents(c *gin.Context) {
	if !h.requireIncidentService(c) {
		return
	}
	filter, err := parseIncidentFilter(c)
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	if filter.PageSize > workbenchMaxPageSize {
		filter.PageSize = workbenchMaxPageSize
	}
	page, err := h.incidentService.ListIncidents(c.Request.Context(), filter)
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	items := make([]workbenchIncidentDTO, 0, len(page.Items))
	for index := range page.Items {
		items = append(items, h.toWorkbenchIncident(c, &page.Items[index]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": page.Total, "page": page.Page, "page_size": page.PageSize}})
}

func (h *Handler) GetWorkbenchIncident(c *gin.Context) {
	item, ok := h.loadWorkbenchIncident(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: h.toWorkbenchIncident(c, item)})
}

func (h *Handler) ListWorkbenchSignals(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	page, pageSize, ok := parseWorkbenchPage(c)
	if !ok {
		return
	}
	items, err := h.incidentService.ListSignals(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	dtos := make([]workbenchSignalDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, workbenchSignalDTO{Source: item.Source, Status: item.Status, Severity: item.Severity, Category: item.Category, Summary: item.Summary, OccurredAt: item.OccurredAt, ReceivedAt: item.ReceivedAt})
	}
	writeWorkbenchPage(c, dtos, page, pageSize)
}

func (h *Handler) ListWorkbenchTimeline(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	page, pageSize, ok := parseWorkbenchPage(c)
	if !ok {
		return
	}
	items, err := h.incidentService.ListTimeline(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	dtos := make([]workbenchTimelineDTO, 0, len(items))
	sort.SliceStable(items, func(left, right int) bool {
		if !items[left].OccurredAt.Equal(items[right].OccurredAt) {
			return items[left].OccurredAt.Before(items[right].OccurredAt)
		}
		if !items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].CreatedAt.Before(items[right].CreatedAt)
		}
		leftKey := fmt.Sprintf("%s|%s|%s", items[left].EventType, items[left].ActorType, items[left].Summary)
		rightKey := fmt.Sprintf("%s|%s|%s", items[right].EventType, items[right].ActorType, items[right].Summary)
		return leftKey < rightKey
	})
	duplicateOrdinal := make(map[string]int)
	for _, item := range items {
		base := fmt.Sprintf("%s|%s|%s|%s", item.EventType, item.ActorType, item.OccurredAt.UTC().Format(time.RFC3339Nano), item.Summary)
		duplicateOrdinal[base]++
		dtos = append(dtos, workbenchTimelineDTO{Key: stableWorkbenchKey(c.Param("id"), base, duplicateOrdinal[base]), EventType: item.EventType, ActorType: item.ActorType, Summary: item.Summary, OccurredAt: item.OccurredAt})
	}
	writeWorkbenchPage(c, dtos, page, pageSize)
}

func (h *Handler) ListWorkbenchEvidence(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	page, pageSize, ok := parseWorkbenchPage(c)
	if !ok {
		return
	}
	items, err := h.incidentService.ListEvidence(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	dtos := make([]workbenchEvidenceDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, workbenchEvidenceDTO{ID: item.PublicID, Type: item.Type, Source: item.Source, ResourceRef: item.ResourceRef, Summary: item.Summary, State: evidenceState(item.Valid, item.Truncated), DataFreshness: "unknown", RelatedClaim: "Unknown", Truncated: item.Truncated, CollectedAt: item.CollectedAt})
	}
	writeWorkbenchPage(c, dtos, page, pageSize)
}

func (h *Handler) GetWorkbenchInvestigation(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	if h.agentRuntime == nil {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "incident investigation unavailable"})
		return
	}
	runs, err := h.agentRuntime.ListRuns(c.Request.Context(), c.Param("id"), 1, 20)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	runDTOs := make([]workbenchAgentRunDTO, 0, len(runs.Items))
	for index := range runs.Items {
		runDTOs = append(runDTOs, toWorkbenchAgentRun(&runs.Items[index]))
	}
	steps := []workbenchAgentStepDTO{}
	evidence := []workbenchAgentEvidenceDTO{}
	if len(runs.Items) > 0 {
		latest := runs.Items[0]
		stepItems, stepErr := h.agentRuntime.ListSteps(c.Request.Context(), latest.PublicID, 100)
		if stepErr != nil {
			writeAgentError(c, stepErr)
			return
		}
		for _, item := range stepItems {
			steps = append(steps, workbenchAgentStepDTO{ID: item.PublicID, Sequence: item.Sequence, Type: item.Node, Status: item.Status, Summary: item.ResultSummary, TypedTool: item.SelectedTool, EvidenceID: item.EvidencePublicID, RetryCount: item.RetryCount, DurationMS: item.DurationMS, FailureReason: item.ErrorCode, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt})
		}
		evidenceItems, evidenceErr := h.agentRuntime.ListEvidence(c.Request.Context(), latest.PublicID, 100)
		if evidenceErr != nil {
			writeAgentError(c, evidenceErr)
			return
		}
		for _, item := range evidenceItems {
			evidence = append(evidence, workbenchAgentEvidenceDTO{ID: item.PublicID, TypedTool: item.ToolName, ResourceScope: item.ResourceScope, Summary: item.Summary, State: evidenceState(item.Valid, item.Truncated), Truncated: item.Truncated, CollectedAt: item.CollectedAt})
		}
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"runs": runDTOs, "steps": steps, "evidence": evidence}})
}

func (h *Handler) GetWorkbenchRemediation(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	if !isWorkbenchAdmin(c) {
		c.JSON(http.StatusForbidden, response{Status: "error", Error: "remediation details forbidden"})
		return
	}
	if h.remediation == nil || !h.remediation.Enabled() {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "remediation unavailable"})
		return
	}
	page, err := h.remediation.List(c.Request.Context(), remediation.ListFilter{IncidentPublicID: c.Param("id"), Page: 1, PageSize: 1})
	if err != nil {
		writeRemediationError(c, err)
		return
	}
	if len(page.Items) == 0 {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "remediation not found"})
		return
	}
	item := page.Items[0]
	dto := workbenchRemediationDTO{ID: item.PublicID, Status: item.Status, OperationType: item.OperationType, Target: item.Parameters.Target, ProposedValue: item.Parameters.ProposedValue, RiskLevel: item.RiskLevel, PatchSummary: item.PatchSummary, RollbackPlan: item.RollbackPlan, ValidationPlan: item.ValidationPlan, ApprovalActor: "Unknown", CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if approval, approvalErr := h.remediation.GetApproval(c.Request.Context(), item.PublicID); approvalErr == nil {
		dto.ApprovalActor = approval.Actor
		dto.ApprovalDecidedAt = &approval.CreatedAt
	} else if !errors.Is(approvalErr, remediation.ErrNotFound) {
		writeRemediationError(c, approvalErr)
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: dto})
}

func (h *Handler) GetWorkbenchDelivery(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
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
	provenance, digest := "unverified", ""
	if h.changeService != nil {
		if contextResult, contextErr := h.changeService.GetContext(c.Request.Context(), c.Param("id")); contextErr == nil {
			digest = contextResult.ImageResolution.Digest
			switch contextResult.ImageResolution.Status {
			case change.ImageConfirmed:
				provenance = "verified"
			case change.ImageConflict:
				provenance = "conflict"
			}
		}
	}
	dto := workbenchDeliveryDTO{ID: item.PublicID, Status: item.Status, CIStatus: item.CIStatus, PRState: item.PRState, PullRequest: item.PRNumber, PullRequestURL: item.PRURL, HeadCommitSHA: item.HeadCommitSHA, MergedCommitSHA: item.MergedCommitSHA, TargetRevision: item.TargetRevision, ArgoApplication: item.ArgoApplication, DetectedRevision: item.DetectedRevision, ArgoSyncStatus: item.ArgoSyncStatus, ArgoOperationPhase: item.ArgoOperationPhase, ArgoHealthStatus: item.ArgoHealthStatus, ImageDigest: digest, ProvenanceStatus: provenance, Attempts: item.Attempt, DeploymentGeneration: item.DeploymentGeneration, ObservedGeneration: item.ObservedGeneration, DesiredReplicas: item.DesiredReplicas, UpdatedReplicas: item.UpdatedReplicas, AvailableReplicas: item.AvailableReplicas, UnavailableReplicas: item.UnavailableReplicas, StartedAt: item.DeliveryStartedAt, DeadlineAt: item.DeliveryDeadlineAt, CompletedAt: item.DeliveryCompletedAt, FailureReason: item.FailureReason, UpdatedAt: item.UpdatedAt}
	c.JSON(http.StatusOK, response{Status: "success", Data: dto})
}

func (h *Handler) ListWorkbenchVerifications(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	if h.deliveryVerification == nil || !h.deliveryVerification.VerificationEnabled() {
		writeDeliveryVerificationError(c, verification.ErrUnavailable)
		return
	}
	page, pageSize, ok := parseWorkbenchPage(c)
	if !ok {
		return
	}
	result, err := h.deliveryVerification.ListRuns(c.Request.Context(), c.Param("id"), page, pageSize)
	if err != nil {
		writeDeliveryVerificationError(c, err)
		return
	}
	items := make([]verificationRunDTO, 0, len(result.Items))
	for index := range result.Items {
		items = append(items, toVerificationRunDTO(&result.Items[index]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": result.Total, "page": result.Page, "page_size": result.PageSize}})
}

func (h *Handler) GetWorkbenchVerification(c *gin.Context) {
	if _, ok := h.loadWorkbenchIncident(c); !ok {
		return
	}
	if !validPublicID(c.Param("verification_id")) {
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
	now := time.Now().UTC()
	checkDTOs := make([]workbenchVerificationCheckDTO, 0, len(checks))
	for index := range checks {
		item := &checks[index]
		progress := int64(0)
		if item.ConsecutiveSuccessSince != nil {
			progress = max(0, min(int64(item.StabilityWindow.Seconds()), int64(now.Sub(item.ConsecutiveSuccessSince.UTC()).Seconds())))
		}
		checkDTOs = append(checkDTOs, workbenchVerificationCheckDTO{ID: item.PublicID, Type: item.Type, TemplateID: item.TemplateID, Required: item.Required, Status: item.Status, Comparison: item.Comparison, Threshold: item.Threshold, Observed: safeObserved(item.Observed), StabilityWindowSeconds: int64(item.StabilityWindow.Seconds()), StabilityProgressSeconds: progress, TimeoutSeconds: int64(item.Timeout.Seconds()), AttemptCount: item.AttemptCount, FailureReason: item.FailureReason, FirstCheckedAt: item.FirstCheckedAt, LastCheckedAt: item.LastCheckedAt, PassedAt: item.PassedAt})
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"verification": toVerificationRunDTO(run), "checks": checkDTOs}})
}

func (h *Handler) WorkbenchRealtime(c *gin.Context) {
	incident, ok := h.loadWorkbenchIncident(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	lastSequence := int64(0)
	writeEvent := func(w io.Writer) bool {
		item, err := h.incidentService.GetIncident(c.Request.Context(), incident.PublicID)
		if err != nil {
			return false
		}
		// Unix microseconds preserve the database timestamp precision while remaining
		// within JavaScript's safe integer range for client-side sequence checks.
		sequence := item.UpdatedAt.UTC().UnixMicro()
		timeline, timelineErr := h.incidentService.ListTimeline(c.Request.Context(), incident.PublicID)
		if timelineErr == nil {
			for index := range timeline {
				createdSequence := timeline[index].CreatedAt.UTC().UnixMicro()
				if createdSequence > sequence {
					sequence = createdSequence
				}
			}
		}
		if sequence <= lastSequence {
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			return true
		}
		lastSequence = sequence
		payload, _ := json.Marshal(gin.H{"incident_id": incident.PublicID, "sequence": sequence, "kind": "refresh"})
		_, _ = fmt.Fprintf(w, "id: %d\nevent: incident_refresh\ndata: %s\n\n", sequence, payload)
		return true
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	c.Stream(func(w io.Writer) bool {
		if lastSequence == 0 {
			return writeEvent(w)
		}
		select {
		case <-c.Request.Context().Done():
			return false
		case <-ticker.C:
			return writeEvent(w)
		}
	})
}

func (h *Handler) loadWorkbenchIncident(c *gin.Context) (*domain.Incident, bool) {
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		writeIncidentError(c, domain.ErrInvalidArgument)
		return nil, false
	}
	return h.loadIncident(c)
}

func (h *Handler) toWorkbenchIncident(c *gin.Context, item *domain.Incident) workbenchIncidentDTO {
	return workbenchIncidentDTO{ID: item.PublicID, Title: workbenchTitle(item), Severity: item.Severity, Status: item.Status, Cluster: item.Cluster, Service: item.ServiceName, Environment: item.Environment, Namespace: item.Namespace, WorkloadKind: item.TargetKind, WorkloadName: item.TargetName, TriggeringSummary: item.Summary, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, ResolvedAt: item.ResolvedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Summary: h.workbenchSummary(c, item.PublicID)}
}

func (h *Handler) workbenchSummary(c *gin.Context, incidentID string) workbenchSummaryDTO {
	result := workbenchSummaryDTO{
		Investigation: workbenchSectionSummary{Availability: "unavailable", Status: "unknown"},
		Approval:      workbenchSectionSummary{Availability: "unavailable", Status: "unknown"},
		Delivery:      workbenchSectionSummary{Availability: "unavailable", Status: "unknown"},
		Verification:  workbenchSectionSummary{Availability: "unavailable", Status: "unknown"},
		Postmortem:    workbenchSectionSummary{Availability: "available", Status: "not_generated"},
	}
	if h.agentRuntime != nil {
		if page, err := h.agentRuntime.ListRuns(c.Request.Context(), incidentID, 1, 1); err == nil {
			result.Investigation = workbenchSectionSummary{Availability: "available", Status: "not_started"}
			if len(page.Items) > 0 {
				result.Investigation.Status, result.Investigation.ID = string(page.Items[0].Status), page.Items[0].PublicID
			}
		}
	}
	if !isWorkbenchAdmin(c) {
		result.Approval = workbenchSectionSummary{Availability: "forbidden", Status: "restricted"}
	} else if h.remediation != nil && h.remediation.Enabled() {
		if page, err := h.remediation.List(c.Request.Context(), remediation.ListFilter{IncidentPublicID: incidentID, Page: 1, PageSize: 1}); err == nil {
			result.Approval = workbenchSectionSummary{Availability: "available", Status: "not_started"}
			if len(page.Items) > 0 {
				result.Approval.Status, result.Approval.ID = string(page.Items[0].Status), page.Items[0].PublicID
			}
		}
	}
	if h.deliveryVerification != nil && h.deliveryVerification.DeliveryEnabled() {
		result.Delivery = workbenchSectionSummary{Availability: "available", Status: "not_started"}
		if item, err := h.deliveryVerification.GetDelivery(c.Request.Context(), incidentID); err == nil {
			result.Delivery.Status, result.Delivery.ID = item.Status, item.PublicID
		} else if !errors.Is(err, verification.ErrNotFound) {
			result.Delivery = workbenchSectionSummary{Availability: "unavailable", Status: "unknown"}
		}
	}
	if h.deliveryVerification != nil && h.deliveryVerification.VerificationEnabled() {
		result.Verification = workbenchSectionSummary{Availability: "available", Status: "not_started"}
		if page, err := h.deliveryVerification.ListRuns(c.Request.Context(), incidentID, 1, 1); err == nil && len(page.Items) > 0 {
			result.Verification.Status, result.Verification.ID = string(page.Items[0].Status), page.Items[0].PublicID
		}
		if item, err := h.deliveryVerification.GetPostmortem(c.Request.Context(), incidentID); err == nil {
			result.Postmortem.Status, result.Postmortem.ID = "generated", item.PublicID
		} else if !errors.Is(err, verification.ErrNotFound) {
			result.Postmortem = workbenchSectionSummary{Availability: "unavailable", Status: "unknown"}
		}
	}
	return result
}

func toWorkbenchAgentRun(item *agent.Run) workbenchAgentRunDTO {
	var diagnosis *agent.Diagnosis
	if len(item.FinalDiagnosis) > 0 {
		var decoded agent.Diagnosis
		if json.Unmarshal(item.FinalDiagnosis, &decoded) == nil {
			diagnosis = &decoded
		}
	}
	return workbenchAgentRunDTO{ID: item.PublicID, Attempt: item.Attempt, Status: item.Status, UsedSteps: item.Usage.Steps, MaxSteps: item.Limits.MaxSteps, UsedToolCalls: item.Usage.ToolCalls, MaxToolCalls: item.Limits.MaxToolCalls, UsedEvidenceItems: item.Usage.Evidence, MaxEvidenceItems: item.Limits.MaxEvidenceItems, TerminationReason: item.FailureCode, FailureSummary: item.FailureSummary, Diagnosis: diagnosis, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, CreatedAt: item.CreatedAt}
}

func safeObserved(raw json.RawMessage) map[string]any {
	var source map[string]any
	if json.Unmarshal(raw, &source) != nil {
		return nil
	}
	allowed := map[string]struct{}{"status": {}, "value": {}, "numerator": {}, "denominator": {}, "sample_count": {}, "series_count": {}, "matched_count": {}, "first_seen": {}, "last_seen": {}, "sampled_at": {}, "reason_code": {}, "ready": {}, "replicas": {}, "revision": {}, "sync_status": {}, "health_status": {}}
	result := make(map[string]any)
	for key, value := range source {
		if _, ok := allowed[key]; ok {
			switch typed := value.(type) {
			case string:
				result[key] = truncateWorkbenchText(typed, 256)
			case float64, bool, nil:
				result[key] = typed
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func truncateWorkbenchText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func parseWorkbenchPage(c *gin.Context) (int, int, bool) {
	page, pageSize := 1, 20
	var err error
	if raw := c.Query("page"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err == nil && c.Query("page_size") != "" {
		pageSize, err = strconv.Atoi(c.Query("page_size"))
	}
	if err != nil || page < 1 || pageSize < 1 || pageSize > workbenchMaxPageSize {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid page or page_size"})
		return 0, 0, false
	}
	return page, pageSize, true
}

func writeWorkbenchPage[T any](c *gin.Context, items []T, page, pageSize int) {
	if len(items) > workbenchMaxSnapshot {
		items = items[:workbenchMaxSnapshot]
	}
	total := len(items)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items[start:end], "total": total, "page": page, "page_size": pageSize, "bounded": true}})
}

func stableWorkbenchKey(incidentID, base string, ordinal int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", incidentID, base, ordinal)))
	return hex.EncodeToString(sum[:16])
}

func evidenceState(valid, truncated bool) string {
	if !valid {
		return "malformed"
	}
	if truncated {
		return "partial"
	}
	return "available"
}

func workbenchTitle(item *domain.Incident) string {
	if title := strings.TrimSpace(item.Summary); title != "" {
		return title
	}
	parts := make([]string, 0, 2)
	if item.ServiceName != "" && item.ServiceName != "unknown" {
		parts = append(parts, item.ServiceName)
	}
	if item.TargetName != "" && item.TargetName != "unknown" {
		parts = append(parts, item.TargetName)
	}
	if len(parts) == 0 {
		return "Incident"
	}
	return strings.Join(parts, " / ")
}

func isWorkbenchAdmin(c *gin.Context) bool {
	value, exists := c.Get(middleware.ContextRole)
	if !exists {
		return true
	}
	role, _ := value.(string)
	return role == "admin"
}
