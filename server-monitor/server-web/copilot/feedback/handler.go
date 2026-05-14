package feedback

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"server-web/copilot/diagnosis"
	"server-web/model"
)

type ReportAccessChecker interface {
	GetAccessibleReport(ctx context.Context, id uint64, userID uint64, role string) (model.DiagnosisReport, error)
}

type Handler struct {
	feedbackService  *Service
	reportAccess     ReportAccessChecker
	commentMaxLength int
}

func NewHandler(feedbackService *Service, reportAccess ReportAccessChecker, commentMaxLength int) *Handler {
	if commentMaxLength <= 0 {
		commentMaxLength = 500
	}
	return &Handler{
		feedbackService:  feedbackService,
		reportAccess:     reportAccess,
		commentMaxLength: commentMaxLength,
	}
}

func (h *Handler) Submit(c *gin.Context) {
	idStr := c.Param("id")
	diagnosisID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid diagnosis id"})
		return
	}

	user, ok := diagnosis.UserFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "error": "unauthorized"})
		return
	}

	var req FeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid request body"})
		return
	}

	if !validRatings[req.Rating] {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "rating must be 'useful' or 'not_useful'"})
		return
	}

	req.Comment = strings.TrimSpace(req.Comment)

	if utf8.RuneCountInString(req.Comment) > h.commentMaxLength {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": fmt.Sprintf("comment must be at most %d characters", h.commentMaxLength)})
		return
	}

	report, err := h.reportAccess.GetAccessibleReport(c.Request.Context(), diagnosisID, user.ID, user.Role)
	if err != nil {
		if errors.Is(err, diagnosis.ErrForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "error": "forbidden"})
			return
		}
		if errors.Is(err, diagnosis.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "diagnosis report not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "internal error"})
		return
	}

	if report.Status != diagnosis.StatusCompleted {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": fmt.Sprintf("diagnosis report status is %s, not completed", report.Status)})
		return
	}

	resp, err := h.feedbackService.Submit(c.Request.Context(), diagnosisID, user.ID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to submit feedback"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "data": resp})
}
