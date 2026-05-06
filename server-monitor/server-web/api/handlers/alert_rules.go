package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"server-web/model"
)

const maxAlertRuleExprLength = 2048

type AlertRuleSyncConfig struct {
	Enabled    bool
	FilePath   string
	Promtool   string
	ReloadURL  string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type alertRuleSyncResponse struct {
	Success    bool   `json:"success"`
	Enabled    bool   `json:"enabled"`
	RuleCount  int    `json:"rule_count"`
	FilePath   string `json:"file_path,omitempty"`
	SyncedAt   string `json:"synced_at,omitempty"`
	ReloadURL  string `json:"reload_url,omitempty"`
	Promtool   string `json:"promtool,omitempty"`
	Error      string `json:"error,omitempty"`
	Restored   bool   `json:"restored,omitempty"`
	Reloaded   bool   `json:"reloaded"`
	Validated  bool   `json:"validated"`
	RenderedTo string `json:"rendered_to,omitempty"`
}

var forbiddenAlertRuleExprTerms = []string{
	"admin_api",
	"scrape_interval",
	"scrape_duration",
}

type alertRuleRequest struct {
	Name        string `json:"name"`
	Expr        string `json:"expr"`
	Duration    string `json:"duration"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

func NewAlertRuleSyncConfig(enabled bool, filePath, promtool, reloadURL string, timeout time.Duration) AlertRuleSyncConfig {
	return AlertRuleSyncConfig{
		Enabled:   enabled,
		FilePath:  strings.TrimSpace(filePath),
		Promtool:  strings.TrimSpace(promtool),
		ReloadURL: strings.TrimSpace(reloadURL),
		Timeout:   timeout,
	}
}

func (h *Handler) ListAlertRules(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}

	var rules []model.AlertRule
	if err := db.WithContext(c.Request.Context()).Order("id ASC").Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "list alert rules failed"})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: rules})
}

func (h *Handler) GetAlertRule(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	rule, ok := h.findAlertRule(c, db, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: rule})
}

func (h *Handler) CreateAlertRule(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}

	var req alertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid alert rule request"})
		return
	}
	rule, ok := normalizeAlertRuleRequest(c, req)
	if !ok {
		return
	}

	if err := db.WithContext(c.Request.Context()).Create(&rule).Error; err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "create alert rule failed"})
		return
	}
	c.JSON(http.StatusCreated, response{Status: "success", Data: rule})
}

func (h *Handler) UpdateAlertRule(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	var req alertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid alert rule request"})
		return
	}
	updated, ok := normalizeAlertRuleRequest(c, req)
	if !ok {
		return
	}

	rule, ok := h.findAlertRule(c, db, id)
	if !ok {
		return
	}
	rule.Name = updated.Name
	rule.Expr = updated.Expr
	rule.Duration = updated.Duration
	rule.Severity = updated.Severity
	rule.Summary = updated.Summary
	rule.Description = updated.Description
	rule.Enabled = updated.Enabled

	if err := db.WithContext(c.Request.Context()).Save(&rule).Error; err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "update alert rule failed"})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: rule})
}

func (h *Handler) DeleteAlertRule(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	if _, ok := h.findAlertRule(c, db, id); !ok {
		return
	}
	if err := db.WithContext(c.Request.Context()).Delete(&model.AlertRule{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "delete alert rule failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) SyncAlertRules(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	if !h.ruleSync.Enabled {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "alert rule sync is disabled"})
		return
	}
	if strings.TrimSpace(h.ruleSync.FilePath) == "" {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "alert rule sync is not configured"})
		return
	}

	var rules []model.AlertRule
	if err := db.WithContext(c.Request.Context()).
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "list enabled alert rules failed"})
		return
	}

	timeout := h.ruleSync.Timeout
	if timeout <= 0 {
		timeout = h.requestTimeout
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	result, err := h.syncAlertRules(ctx, rules)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		c.JSON(http.StatusBadGateway, response{Status: "error", Error: "sync alert rules failed", Data: result})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: result})
}

func (h *Handler) syncAlertRules(ctx context.Context, rules []model.AlertRule) (alertRuleSyncResponse, error) {
	cfg := h.ruleSync
	cfg.FilePath = strings.TrimSpace(cfg.FilePath)
	cfg.Promtool = strings.TrimSpace(cfg.Promtool)
	cfg.ReloadURL = strings.TrimSpace(cfg.ReloadURL)
	if cfg.Promtool == "" {
		cfg.Promtool = "promtool"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	rendered, err := renderAlertRulesYAML(rules)
	result := alertRuleSyncResponse{
		Enabled:   cfg.Enabled,
		RuleCount: len(rules),
		FilePath:  cfg.FilePath,
		ReloadURL: cfg.ReloadURL,
		Promtool:  cfg.Promtool,
	}
	if err != nil {
		return result, err
	}

	tmpFile, err := writeTempAlertRulesFile(cfg.FilePath, rendered)
	if err != nil {
		return result, err
	}
	result.RenderedTo = tmpFile
	defer os.Remove(tmpFile)

	if err := runPromtoolCheck(ctx, cfg.Promtool, tmpFile); err != nil {
		return result, err
	}
	result.Validated = true

	previous, hadPrevious, err := readPreviousRulesFile(cfg.FilePath)
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(cfg.FilePath, []byte(rendered), 0644); err != nil {
		return result, fmt.Errorf("write alert rules file: %w", err)
	}

	if cfg.ReloadURL != "" {
		if err := reloadPrometheus(ctx, cfg.HTTPClient, cfg.ReloadURL); err != nil {
			result.Restored = restoreRulesFile(cfg.FilePath, previous, hadPrevious)
			return result, fmt.Errorf("reload prometheus: %w", err)
		}
		result.Reloaded = true
	}

	result.Success = true
	result.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	return result, nil
}

func (h *Handler) findAlertRule(c *gin.Context, db *gorm.DB, id uint64) (model.AlertRule, bool) {
	var rule model.AlertRule
	err := db.WithContext(c.Request.Context()).First(&rule, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "alert rule not found"})
		return model.AlertRule{}, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query alert rule failed"})
		return model.AlertRule{}, false
	}
	return rule, true
}

func normalizeAlertRuleRequest(c *gin.Context, req alertRuleRequest) (model.AlertRule, bool) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "name must be 1-128 characters"})
		return model.AlertRule{}, false
	}

	expr := strings.TrimSpace(req.Expr)
	if err := validateAlertRuleExpr(expr); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: err.Error()})
		return model.AlertRule{}, false
	}

	duration := strings.TrimSpace(req.Duration)
	if duration == "" {
		duration = "2m"
	}
	if err := validateAlertRuleDuration(duration); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: err.Error()})
		return model.AlertRule{}, false
	}

	severity := strings.TrimSpace(req.Severity)
	if severity == "" {
		severity = "warning"
	}
	if _, ok := validAlertEventSeverities[severity]; !ok {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid severity"})
		return model.AlertRule{}, false
	}

	summary := strings.TrimSpace(req.Summary)
	if len(summary) > 512 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "summary must be at most 512 characters"})
		return model.AlertRule{}, false
	}
	description := strings.TrimSpace(req.Description)

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	return model.AlertRule{
		Name:        name,
		Expr:        expr,
		Duration:    duration,
		Severity:    severity,
		Summary:     summary,
		Description: description,
		Enabled:     enabled,
	}, true
}

func validateAlertRuleExpr(expr string) error {
	if expr == "" {
		return errors.New("expr is required")
	}
	if len(expr) > maxAlertRuleExprLength {
		return errors.New("expr too long")
	}

	lower := strings.ToLower(expr)
	for _, term := range forbiddenAlertRuleExprTerms {
		if strings.Contains(lower, term) {
			return errors.New("expr contains forbidden reference")
		}
	}
	if containsPromQLSubquery(expr) {
		return errors.New("expr subquery is not allowed")
	}
	return nil
}

func validateAlertRuleDuration(duration string) error {
	if duration == "" {
		return errors.New("duration must be a positive prometheus duration")
	}
	for i := 0; i < len(duration); {
		start := i
		for i < len(duration) && duration[i] >= '0' && duration[i] <= '9' {
			i++
		}
		if start == i {
			return errors.New("duration must be a positive prometheus duration")
		}
		if !hasNonZeroDigit(duration[start:i]) {
			return errors.New("duration must be a positive prometheus duration")
		}

		unitStart := i
		if i < len(duration) && duration[i] == 'm' {
			i++
			if i < len(duration) && duration[i] == 's' {
				i++
			}
		} else if i < len(duration) && strings.ContainsRune("smhdwy", rune(duration[i])) {
			i++
		}
		if unitStart == i {
			return errors.New("duration must be a positive prometheus duration")
		}
	}
	return nil
}

func hasNonZeroDigit(value string) bool {
	for _, ch := range value {
		if ch >= '1' && ch <= '9' {
			return true
		}
	}
	return false
}

func renderAlertRulesYAML(rules []model.AlertRule) (string, error) {
	var builder strings.Builder
	builder.WriteString("groups:\n")
	builder.WriteString("  - name: custom_alerts\n")

	count := 0
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if err := validateAlertRuleForRender(rule); err != nil {
			return "", err
		}
		if count == 0 {
			builder.WriteString("    rules:\n")
		}
		count++
		builder.WriteString("      - alert: ")
		builder.WriteString(yamlString(rule.Name))
		builder.WriteString("\n")
		builder.WriteString("        expr: ")
		builder.WriteString(yamlString(rule.Expr))
		builder.WriteString("\n")
		builder.WriteString("        for: ")
		builder.WriteString(yamlString(rule.Duration))
		builder.WriteString("\n")
		builder.WriteString("        labels:\n")
		builder.WriteString("          severity: ")
		builder.WriteString(yamlString(rule.Severity))
		builder.WriteString("\n")
		builder.WriteString("        annotations:\n")
		builder.WriteString("          summary: ")
		builder.WriteString(yamlString(rule.Summary))
		builder.WriteString("\n")
		builder.WriteString("          description: ")
		builder.WriteString(yamlString(rule.Description))
		builder.WriteString("\n")
	}
	if count == 0 {
		builder.WriteString("    rules: []\n")
	}
	return builder.String(), nil
}

func validateAlertRuleForRender(rule model.AlertRule) error {
	if rule.Name == "" || len(rule.Name) > 128 {
		return fmt.Errorf("invalid alert rule name: %d", rule.ID)
	}
	if err := validateAlertRuleExpr(rule.Expr); err != nil {
		return fmt.Errorf("invalid alert rule expr %q: %w", rule.Name, err)
	}
	if err := validateAlertRuleDuration(rule.Duration); err != nil {
		return fmt.Errorf("invalid alert rule duration %q: %w", rule.Name, err)
	}
	if _, ok := validAlertEventSeverities[rule.Severity]; !ok {
		return fmt.Errorf("invalid alert rule severity %q", rule.Name)
	}
	if len(rule.Summary) > 512 {
		return fmt.Errorf("invalid alert rule summary %q", rule.Name)
	}
	return nil
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func writeTempAlertRulesFile(targetPath string, content string) (string, error) {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".custom-alerts-*.yml")
	if err != nil {
		return "", fmt.Errorf("create temporary rules file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("write temporary rules file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("close temporary rules file: %w", err)
	}
	return tmpName, nil
}

func runPromtoolCheck(ctx context.Context, promtool string, rulesFile string) error {
	cmd := exec.CommandContext(ctx, promtool, "check", "rules", rulesFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("promtool check rules failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func readPreviousRulesFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read current alert rules file: %w", err)
	}
	return data, true, nil
}

func restoreRulesFile(path string, previous []byte, hadPrevious bool) bool {
	if hadPrevious {
		return os.WriteFile(path, previous, 0644) == nil
	}
	err := os.Remove(path)
	return err == nil || errors.Is(err, os.ErrNotExist)
}

func reloadPrometheus(ctx context.Context, client *http.Client, reloadURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reloadURL, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func containsPromQLSubquery(expr string) bool {
	for start := strings.Index(expr, "["); start >= 0; {
		end := strings.Index(expr[start:], "]")
		if end < 0 {
			return false
		}
		inside := expr[start+1 : start+end]
		if strings.Contains(inside, ":") {
			return true
		}
		next := start + end + 1
		if next >= len(expr) {
			return false
		}
		remaining := expr[next:]
		offset := strings.Index(remaining, "[")
		if offset < 0 {
			return false
		}
		start = next + offset
	}
	return false
}
