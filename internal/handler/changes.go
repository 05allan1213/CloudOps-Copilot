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

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/service/changeintelligence"
)

// ChangeApplication is the narrow, read-only transport contract for Phase 3.
type ChangeApplication interface {
	Enabled() bool
	ListChanges(context.Context, string, change.ListFilter) (change.Page, error)
	GetContext(context.Context, string) (changeintelligence.Context, error)
}

func (h *Handler) SetChangeIntelligence(service ChangeApplication) { h.changeService = service }

type changeDTO struct {
	ID                     string            `json:"id"`
	SourceType             change.SourceType `json:"source_type"`
	Repository             string            `json:"repository,omitempty"`
	CommitSHA              string            `json:"commit_sha,omitempty"`
	PullRequestNumber      int64             `json:"pull_request_number,omitempty"`
	WorkflowName           string            `json:"workflow_name,omitempty"`
	WorkflowConclusion     string            `json:"workflow_conclusion,omitempty"`
	ImageRepository        string            `json:"image_repository,omitempty"`
	ImageTag               string            `json:"image_tag,omitempty"`
	ImageDigest            string            `json:"image_digest,omitempty"`
	ImageRevision          string            `json:"image_revision,omitempty"`
	ArgoCDApplication      string            `json:"argocd_application,omitempty"`
	ArgoCDTargetRevision   string            `json:"argocd_target_revision,omitempty"`
	ArgoCDDeployedRevision string            `json:"argocd_deployed_revision,omitempty"`
	Environment            string            `json:"environment,omitempty"`
	Cluster                string            `json:"cluster,omitempty"`
	Namespace              string            `json:"namespace,omitempty"`
	ServiceName            string            `json:"service_name,omitempty"`
	WorkloadKind           string            `json:"workload_kind,omitempty"`
	WorkloadName           string            `json:"workload_name,omitempty"`
	GitOpsPath             string            `json:"gitops_path,omitempty"`
	DeployedAt             *time.Time        `json:"deployed_at,omitempty"`
	Status                 change.Status     `json:"status"`
	Category               change.Category   `json:"category"`
	ChangeSummary          string            `json:"change_summary,omitempty"`
	RiskSummary            string            `json:"risk_summary,omitempty"`
	CorrelationScore       int               `json:"correlation_score"`
	CorrelationReasons     []string          `json:"correlation_reasons"`
	Metadata               json.RawMessage   `json:"metadata"`
	Truncated              bool              `json:"truncated"`
	Degraded               bool              `json:"degraded"`
	CreatedAt              time.Time         `json:"created_at"`
}

type changeContextDTO struct {
	Enabled         bool                     `json:"enabled"`
	Status          string                   `json:"status"`
	CurrentRuntime  change.RuntimeContext    `json:"current_runtime"`
	Candidates      []changeDTO              `json:"candidates"`
	Correlation     change.CorrelationResult `json:"correlation"`
	ImageResolution change.ImageResolution   `json:"image_resolution"`
	Unknowns        []string                 `json:"unknowns"`
	Degraded        bool                     `json:"degraded"`
	RefreshedAt     *time.Time               `json:"refreshed_at,omitempty"`
}

// ListIncidentChanges returns only persisted, bounded, sanitized change evidence.
func (h *Handler) ListIncidentChanges(c *gin.Context) {
	if !requireChangeIncidentID(c) {
		return
	}
	if !h.requireChangeIntelligence(c) {
		return
	}
	filter, err := parseChangeFilter(c)
	if err != nil {
		writeChangeError(c, err)
		return
	}
	page, err := h.changeService.ListChanges(c.Request.Context(), c.Param("id"), filter)
	if err != nil {
		writeChangeError(c, err)
		return
	}
	items := make([]changeDTO, 0, len(page.Items))
	for index := range page.Items {
		items = append(items, toChangeDTO(&page.Items[index]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": page.Total, "page": page.Page, "page_size": page.PageSize}})
}

// GetIncidentChangeContext returns the deterministic correlation result. It never refreshes external systems.
func (h *Handler) GetIncidentChangeContext(c *gin.Context) {
	if !requireChangeIncidentID(c) {
		return
	}
	if h.changeService == nil {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "change intelligence is unavailable"})
		return
	}
	result, err := h.changeService.GetContext(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeChangeError(c, err)
		return
	}
	items := make([]changeDTO, 0, len(result.Candidates))
	for index := range result.Candidates {
		items = append(items, toChangeDTO(&result.Candidates[index]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: changeContextDTO{Enabled: result.Enabled, Status: result.Status, CurrentRuntime: result.CurrentRuntime, Candidates: items, Correlation: result.Correlation, ImageResolution: result.ImageResolution, Unknowns: result.Unknowns, Degraded: result.Degraded, RefreshedAt: result.RefreshedAt}})
}

func (h *Handler) requireChangeIntelligence(c *gin.Context) bool {
	if h.changeService != nil && h.changeService.Enabled() {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "change intelligence is disabled"})
	return false
}

func parseChangeFilter(c *gin.Context) (change.ListFilter, error) {
	filter := change.ListFilter{Page: 1, PageSize: 20}
	var err error
	if raw := c.Query("page"); raw != "" {
		filter.Page, err = strconv.Atoi(raw)
	}
	if err == nil && c.Query("page_size") != "" {
		filter.PageSize, err = strconv.Atoi(c.Query("page_size"))
	}
	if err != nil || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return filter, errors.Join(change.ErrInvalidArgument, errors.New("invalid page or page_size"))
	}
	filter.SourceType = change.SourceType(strings.TrimSpace(c.Query("source_type")))
	filter.Status = change.Status(strings.TrimSpace(c.Query("status")))
	filter.Category = change.Category(strings.TrimSpace(c.Query("category")))
	if filter.SourceType != "" && !oneOf(string(filter.SourceType), "github_commit", "github_pull_request", "ci", "image", "argocd") {
		return filter, errors.Join(change.ErrInvalidArgument, errors.New("invalid source_type"))
	}
	if filter.Status != "" && !oneOf(string(filter.Status), "candidate", "matched", "excluded", "unknown") {
		return filter, errors.Join(change.ErrInvalidArgument, errors.New("invalid status"))
	}
	if filter.Category != "" && !oneOf(string(filter.Category), "confirmed_match", "high_confidence", "low_confidence", "excluded", "no_data") {
		return filter, errors.Join(change.ErrInvalidArgument, errors.New("invalid category"))
	}
	return filter, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func toChangeDTO(item *change.Change) changeDTO {
	return changeDTO{ID: item.PublicID, SourceType: item.SourceType, Repository: item.Repository, CommitSHA: item.CommitSHA, PullRequestNumber: item.PullRequestNumber, WorkflowName: item.WorkflowName, WorkflowConclusion: item.WorkflowConclusion, ImageRepository: item.ImageRepository, ImageTag: item.ImageTag, ImageDigest: item.ImageDigest, ImageRevision: item.ImageRevision, ArgoCDApplication: item.ArgoCDApplication, ArgoCDTargetRevision: item.ArgoCDTargetRevision, ArgoCDDeployedRevision: item.ArgoCDDeployedRevision, Environment: item.Environment, Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, WorkloadKind: item.WorkloadKind, WorkloadName: item.WorkloadName, GitOpsPath: item.GitOpsPath, DeployedAt: item.DeployedAt, Status: item.Status, Category: item.Category, ChangeSummary: item.ChangeSummary, RiskSummary: item.RiskSummary, CorrelationScore: item.CorrelationScore, CorrelationReasons: item.CorrelationReasons, Metadata: item.Metadata, Truncated: item.Truncated, Degraded: item.Degraded, CreatedAt: item.CreatedAt}
}

func writeChangeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, change.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: err.Error()})
	case errors.Is(err, change.ErrNotFound), errors.Is(err, domain.ErrNotFound):
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "change context not found"})
	case errors.Is(err, change.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "change intelligence is unavailable"})
	default:
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "change intelligence request failed"})
	}
}

func requireChangeIncidentID(c *gin.Context) bool {
	if _, err := uuid.Parse(c.Param("id")); err == nil {
		return true
	}
	c.JSON(http.StatusBadRequest, response{Status: "error", Error: "incident id must be a valid public UUID"})
	return false
}
