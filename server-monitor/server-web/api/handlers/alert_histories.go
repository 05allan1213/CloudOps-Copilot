package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	appalert "server-web/alert"
	"server-web/model"
	"server-web/webhook"
)

const (
	defaultAlertHistoryPage     = 1
	defaultAlertHistoryPageSize = 20
	maxAlertHistoryPageSize     = 100
)

type alertHistoryQuery struct {
	Status    string
	Severity  string
	AlertName string
	Instance  string
	GroupID   uint64
	Start     *time.Time
	End       *time.Time
	Page      int
	PageSize  int
}

type alertHistoryListResponse struct {
	Items    []model.AlertHistory `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func (h *Handler) ListAlertHistories(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	query, ok := parseAlertHistoryQuery(c)
	if !ok {
		return
	}

	stmt, ok := h.buildAlertHistoryQuery(c, db, query)
	if !ok {
		return
	}

	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "count alert histories failed"})
		return
	}

	var histories []model.AlertHistory
	if err := stmt.
		Order("fired_at DESC").
		Order("id DESC").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&histories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "list alert histories failed"})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: alertHistoryListResponse{
			Items:    histories,
			Total:    total,
			Page:     query.Page,
			PageSize: query.PageSize,
		},
	})
}

func (h *Handler) buildAlertHistoryQuery(c *gin.Context, db *gorm.DB, query alertHistoryQuery) (*gorm.DB, bool) {
	stmt := db.WithContext(c.Request.Context()).Model(&model.AlertHistory{})
	if query.Status != "" {
		stmt = stmt.Where("status = ?", query.Status)
	}
	if query.Severity != "" {
		stmt = stmt.Where("severity = ?", query.Severity)
	}
	if query.AlertName != "" {
		stmt = stmt.Where("alert_name = ?", query.AlertName)
	}
	if query.Instance != "" {
		stmt = stmt.Where("instance = ?", query.Instance)
	}
	if query.Start != nil {
		stmt = stmt.Where("fired_at >= ?", *query.Start)
	}
	if query.End != nil {
		stmt = stmt.Where("fired_at <= ?", *query.End)
	}
	if query.GroupID != 0 {
		instances, ok := h.alertHistoryGroupInstances(c, db, query.GroupID)
		if !ok {
			return nil, false
		}
		if len(instances) == 0 {
			stmt = stmt.Where("1 = 0")
		} else {
			stmt = stmt.Where("instance IN ?", instances)
		}
	}
	return stmt, true
}

func (h *Handler) alertHistoryGroupInstances(c *gin.Context, db *gorm.DB, groupID uint64) ([]string, bool) {
	var group model.HostGroup
	err := db.WithContext(c.Request.Context()).First(&group, groupID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "host group not found"})
		return nil, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query host group failed"})
		return nil, false
	}

	var instances []string
	if err := db.WithContext(c.Request.Context()).
		Model(&model.HostGroupMember{}).
		Where("group_id = ?", groupID).
		Pluck("instance", &instances).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query host group members failed"})
		return nil, false
	}
	return instances, true
}

func parseAlertHistoryQuery(c *gin.Context) (alertHistoryQuery, bool) {
	query := alertHistoryQuery{
		Page:     defaultAlertHistoryPage,
		PageSize: defaultAlertHistoryPageSize,
	}

	status, ok := parseAllowedQuery(c, "status", validAlertEventStatuses)
	if !ok {
		return alertHistoryQuery{}, false
	}
	severity, ok := parseAllowedQuery(c, "severity", validAlertEventSeverities)
	if !ok {
		return alertHistoryQuery{}, false
	}
	query.Status = status
	query.Severity = severity
	query.AlertName = strings.TrimSpace(c.Query("alert_name"))
	query.Instance = strings.TrimSpace(c.Query("instance"))

	groupID, ok := parseOptionalUintQuery(c, "group")
	if !ok {
		return alertHistoryQuery{}, false
	}
	query.GroupID = groupID

	start, ok := parseOptionalTimeQuery(c, "start")
	if !ok {
		return alertHistoryQuery{}, false
	}
	end, ok := parseOptionalTimeQuery(c, "end")
	if !ok {
		return alertHistoryQuery{}, false
	}
	if start != nil && end != nil && start.After(*end) {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "start must be before end"})
		return alertHistoryQuery{}, false
	}
	query.Start = start
	query.End = end

	page, ok := parsePositiveIntQuery(c, "page", defaultAlertHistoryPage, 0)
	if !ok {
		return alertHistoryQuery{}, false
	}
	pageSize, ok := parsePositiveIntQuery(c, "page_size", defaultAlertHistoryPageSize, maxAlertHistoryPageSize)
	if !ok {
		return alertHistoryQuery{}, false
	}
	query.Page = page
	query.PageSize = pageSize

	return query, true
}

func parseAllowedQuery(c *gin.Context, name string, allowed map[string]struct{}) (string, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return "", true
	}
	if _, ok := allowed[value]; !ok {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid " + name})
		return "", false
	}
	return value, true
}

func parseOptionalUintQuery(c *gin.Context, name string) (uint64, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid " + name})
		return 0, false
	}
	return parsed, true
}

func parseOptionalTimeQuery(c *gin.Context, name string) (*time.Time, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid " + name})
		return nil, false
	}
	parsed = parsed.UTC()
	return &parsed, true
}

func parsePositiveIntQuery(c *gin.Context, name string, defaultValue int, maxValue int) (int, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return defaultValue, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid " + name})
		return 0, false
	}
	if maxValue > 0 && parsed > maxValue {
		parsed = maxValue
	}
	return parsed, true
}

func buildAlertHistory(alert webhook.AlertRecord) (model.AlertHistory, error) {
	return appalert.BuildHistory(alert)
}
