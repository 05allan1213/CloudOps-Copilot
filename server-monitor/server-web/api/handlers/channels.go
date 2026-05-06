package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"server-web/model"
)

const (
	notificationChannelTypeWebhook = "webhook"
	notificationChannelTestTimeout = 10 * time.Second
	notificationChannelMaxBody     = 1024
)

type notificationChannelRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled"`
}

type notificationChannelTestResponse struct {
	Success    bool  `json:"success"`
	LatencyMS  int64 `json:"latency_ms"`
	StatusCode int   `json:"status_code,omitempty"`
}

func (h *Handler) ListNotificationChannels(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}

	var channels []model.NotificationChannel
	if err := db.WithContext(c.Request.Context()).Order("id ASC").Find(&channels).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "list notification channels failed"})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: channels})
}

func (h *Handler) GetNotificationChannel(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	channel, ok := h.findNotificationChannel(c, db, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: channel})
}

func (h *Handler) CreateNotificationChannel(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}

	var req notificationChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid notification channel request"})
		return
	}
	channel, ok := normalizeNotificationChannelRequest(c, req)
	if !ok {
		return
	}

	if err := db.WithContext(c.Request.Context()).Create(&channel).Error; err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "create notification channel failed"})
		return
	}
	c.JSON(http.StatusCreated, response{Status: "success", Data: channel})
}

func (h *Handler) UpdateNotificationChannel(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	var req notificationChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid notification channel request"})
		return
	}
	updated, ok := normalizeNotificationChannelRequest(c, req)
	if !ok {
		return
	}

	channel, ok := h.findNotificationChannel(c, db, id)
	if !ok {
		return
	}
	channel.Name = updated.Name
	channel.Type = updated.Type
	channel.URL = updated.URL
	channel.Enabled = updated.Enabled

	if err := db.WithContext(c.Request.Context()).Save(&channel).Error; err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: "update notification channel failed"})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: channel})
}

func (h *Handler) DeleteNotificationChannel(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	if _, ok := h.findNotificationChannel(c, db, id); !ok {
		return
	}
	if err := db.WithContext(c.Request.Context()).Delete(&model.NotificationChannel{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "delete notification channel failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) TestNotificationChannel(c *gin.Context) {
	db, ok := h.requireDB(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	channel, ok := h.findNotificationChannel(c, db, id)
	if !ok {
		return
	}
	if channel.Type != notificationChannelTypeWebhook {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "unsupported notification channel type"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), notificationChannelTestTimeout)
	defer cancel()
	result, err := testNotificationChannel(ctx, channel, newSafeNotificationHTTPClient(notificationChannelTestTimeout))
	if err != nil {
		c.JSON(http.StatusBadGateway, response{Status: "error", Error: "test notification channel failed", Data: gin.H{
			"success": false,
			"error":   err.Error(),
		}})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: result})
}

func (h *Handler) findNotificationChannel(c *gin.Context, db *gorm.DB, id uint64) (model.NotificationChannel, bool) {
	var channel model.NotificationChannel
	err := db.WithContext(c.Request.Context()).First(&channel, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, response{Status: "error", Error: "notification channel not found"})
		return model.NotificationChannel{}, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "query notification channel failed"})
		return model.NotificationChannel{}, false
	}
	return channel, true
}

func normalizeNotificationChannelRequest(c *gin.Context, req notificationChannelRequest) (model.NotificationChannel, bool) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "name must be 1-128 characters"})
		return model.NotificationChannel{}, false
	}

	channelType := strings.TrimSpace(req.Type)
	if channelType == "" {
		channelType = notificationChannelTypeWebhook
	}
	if channelType != notificationChannelTypeWebhook {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "unsupported notification channel type"})
		return model.NotificationChannel{}, false
	}

	webhookURL := strings.TrimSpace(req.URL)
	if webhookURL == "" || len(webhookURL) > 512 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "url must be 1-512 characters"})
		return model.NotificationChannel{}, false
	}
	if err := validateNotificationChannelURL(webhookURL); err != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: err.Error()})
		return model.NotificationChannel{}, false
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	return model.NotificationChannel{
		Name:    name,
		Type:    channelType,
		URL:     webhookURL,
		Enabled: enabled,
	}, true
}

func testNotificationChannel(ctx context.Context, channel model.NotificationChannel, client *http.Client) (notificationChannelTestResponse, error) {
	if err := validateNotificationChannelURL(channel.URL); err != nil {
		return notificationChannelTestResponse{}, err
	}
	if client == nil {
		client = newSafeNotificationHTTPClient(notificationChannelTestTimeout)
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.URL, strings.NewReader(`{"type":"test"}`))
	if err != nil {
		return notificationChannelTestResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return notificationChannelTestResponse{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, notificationChannelMaxBody))

	result := notificationChannelTestResponse{
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
		LatencyMS:  time.Since(start).Milliseconds(),
		StatusCode: resp.StatusCode,
	}
	if !result.Success {
		return result, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return result, nil
}

func newSafeNotificationHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ip, err := resolvePublicIP(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return validateNotificationChannelURL(req.URL.String())
		},
	}
}

func resolvePublicIP(ctx context.Context, host string) (net.IP, error) {
	host = strings.TrimSpace(host)
	if err := validateNotificationHost(host); err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if isRestrictedIP(addr.IP) {
			return nil, errors.New("url resolves to restricted address")
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("url host did not resolve")
	}
	return addrs[0].IP, nil
}

func validateNotificationChannelURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return errors.New("url host is required")
	}
	if parsed.User != nil {
		return errors.New("url user info is not allowed")
	}
	return validateNotificationHost(parsed.Hostname())
}

func validateNotificationHost(host string) error {
	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if normalized == "" {
		return errors.New("url host is required")
	}
	if normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") || normalized == "0" {
		return errors.New("url host is restricted")
	}
	if ip := net.ParseIP(normalized); ip != nil && isRestrictedIP(ip) {
		return errors.New("url host is restricted")
	}
	return nil
}

func isRestrictedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}
