package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"server-web/model"
	promclient "server-web/prometheus"
)

type hostGroupRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Instances   []string `json:"instances"`
}

type hostGroupMemberRequest struct {
	Instance string `json:"instance"`
}

type hostGroupMemberResponse struct {
	ID        uint64 `json:"id,omitempty"`
	GroupID   uint64 `json:"group_id,omitempty"`
	Instance  string `json:"instance"`
	CreatedAt string `json:"created_at,omitempty"`
}

type hostGroupResponse struct {
	ID          uint64                    `json:"id"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	MemberCount int                       `json:"member_count"`
	Members     []hostGroupMemberResponse `json:"members,omitempty"`
	CreatedAt   string                    `json:"created_at"`
	UpdatedAt   string                    `json:"updated_at"`
}

func (h *Handler) ListHostGroups(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}

	var groups []model.HostGroup
	if err := db.WithContext(c.Request.Context()).Preload("Members").Order("id ASC").Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  "list host groups failed",
		})
		return
	}

	result := make([]hostGroupResponse, 0, len(groups))
	for _, group := range groups {
		result = append(result, buildHostGroupResponse(group, false))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: result})
}

func (h *Handler) GetHostGroup(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	group, ok := h.findHostGroup(c, db, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: buildHostGroupResponse(group, true)})
}

func (h *Handler) CreateHostGroup(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}

	var req hostGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid host group request"})
		return
	}
	name, description, instances, ok := normalizeHostGroupRequest(c, req)
	if !ok {
		return
	}

	group := model.HostGroup{Name: name, Description: description}
	err := db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&group).Error; err != nil {
			return err
		}
		members := buildHostGroupMembers(group.ID, instances)
		if len(members) == 0 {
			return nil
		}
		return tx.Create(&members).Error
	})
	if err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "create host group failed"})
		return
	}

	created, ok := h.findHostGroup(c, db, group.ID)
	if !ok {
		return
	}
	c.JSON(http.StatusCreated, response{Status: "success", Data: buildHostGroupResponse(created, true)})
}

func (h *Handler) UpdateHostGroup(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	var req hostGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid host group request"})
		return
	}
	name, description, _, ok := normalizeHostGroupRequest(c, req)
	if !ok {
		return
	}

	group, ok := h.findHostGroup(c, db, id)
	if !ok {
		return
	}
	group.Name = name
	group.Description = description
	if err := db.WithContext(c.Request.Context()).Save(&group).Error; err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "update host group failed"})
		return
	}

	updated, ok := h.findHostGroup(c, db, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: buildHostGroupResponse(updated, true)})
}

func (h *Handler) DeleteHostGroup(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	if _, ok := h.findHostGroup(c, db, id); !ok {
		return
	}
	if err := db.WithContext(c.Request.Context()).Delete(&model.HostGroup{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "delete host group failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) AddHostGroupMember(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	var req hostGroupMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid host group member request"})
		return
	}
	instance, ok := normalizeInstance(c, req.Instance)
	if !ok {
		return
	}
	if _, ok := h.findHostGroup(c, db, id); !ok {
		return
	}

	member := model.HostGroupMember{GroupID: id, Instance: instance}
	if err := db.WithContext(c.Request.Context()).Create(&member).Error; err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "add host group member failed"})
		return
	}
	c.JSON(http.StatusCreated, response{Status: "success", Data: buildHostGroupMemberResponse(member)})
}

func (h *Handler) DeleteHostGroupMember(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	var req hostGroupMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid host group member request"})
		return
	}
	instance, ok := normalizeInstance(c, req.Instance)
	if !ok {
		return
	}
	if _, ok := h.findHostGroup(c, db, id); !ok {
		return
	}

	result := db.WithContext(c.Request.Context()).
		Where("group_id = ? AND instance = ?", id, instance).
		Delete(&model.HostGroupMember{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "delete host group member failed"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "host group member not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) parseHostGroupFilter(c *gin.Context) (map[string]struct{}, bool, bool) {
	raw := strings.TrimSpace(c.Query("group"))
	if raw == "" {
		return nil, false, true
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "database unavailable"})
		return nil, false, false
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid group"})
		return nil, false, false
	}

	var group model.HostGroup
	err = h.db.WithContext(c.Request.Context()).First(&group, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "host group not found"})
		return nil, false, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query host group failed"})
		return nil, false, false
	}

	var instances []string
	if err := h.db.WithContext(c.Request.Context()).
		Model(&model.HostGroupMember{}).
		Where("group_id = ?", id).
		Pluck("instance", &instances).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query host group members failed"})
		return nil, false, false
	}

	allowed := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		allowed[instance] = struct{}{}
	}
	return allowed, true, true
}

func (h *Handler) requireDB(c *gin.Context) (*gorm.DB, bool) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "database unavailable"})
		return nil, false
	}
	return h.db, true
}

func (h *Handler) findHostGroup(c *gin.Context, db *gorm.DB, id uint64) (model.HostGroup, bool) {
	var group model.HostGroup
	err := db.WithContext(c.Request.Context()).Preload("Members").First(&group, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "host group not found"})
		return model.HostGroup{}, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query host group failed"})
		return model.HostGroup{}, false
	}
	return group, true
}

func normalizeHostGroupRequest(c *gin.Context, req hostGroupRequest) (string, string, []string, bool) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "name must be 1-128 characters"})
		return "", "", nil, false
	}
	description := strings.TrimSpace(req.Description)
	if len(description) > 512 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "description must be at most 512 characters"})
		return "", "", nil, false
	}

	instances, ok := normalizeInstances(c, req.Instances)
	return name, description, instances, ok
}

func normalizeInstances(c *gin.Context, raw []string) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	seen := make(map[string]struct{}, len(raw))
	instances := make([]string, 0, len(raw))
	for _, item := range raw {
		instance, ok := normalizeInstance(c, item)
		if !ok {
			return nil, false
		}
		if _, exists := seen[instance]; exists {
			continue
		}
		seen[instance] = struct{}{}
		instances = append(instances, instance)
	}
	return instances, true
}

func normalizeInstance(c *gin.Context, raw string) (string, bool) {
	instance := strings.TrimSpace(raw)
	if instance == "" || len(instance) > 256 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "instance must be 1-256 characters"})
		return "", false
	}
	return instance, true
}

func buildHostGroupMembers(groupID uint64, instances []string) []model.HostGroupMember {
	members := make([]model.HostGroupMember, 0, len(instances))
	for _, instance := range instances {
		members = append(members, model.HostGroupMember{GroupID: groupID, Instance: instance})
	}
	return members
}

func buildHostGroupResponse(group model.HostGroup, includeMembers bool) hostGroupResponse {
	result := hostGroupResponse{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
		MemberCount: len(group.Members),
		CreatedAt:   group.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   group.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if includeMembers {
		result.Members = make([]hostGroupMemberResponse, 0, len(group.Members))
		for _, member := range group.Members {
			result.Members = append(result.Members, buildHostGroupMemberResponse(member))
		}
	}
	return result
}

func buildHostGroupMemberResponse(member model.HostGroupMember) hostGroupMemberResponse {
	return hostGroupMemberResponse{
		ID:        member.ID,
		GroupID:   member.GroupID,
		Instance:  member.Instance,
		CreatedAt: member.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func parseIDParam(c *gin.Context, name string) (uint64, bool) {
	raw := strings.TrimSpace(c.Param(name))
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid id"})
		return 0, false
	}
	return id, true
}

func filterHostsByInstances(hosts []promclient.Host, allowed map[string]struct{}) []promclient.Host {
	if allowed == nil {
		return hosts
	}
	filtered := make([]promclient.Host, 0, len(hosts))
	for _, host := range hosts {
		if _, ok := allowed[host.Instance]; ok {
			filtered = append(filtered, host)
		}
	}
	return filtered
}
