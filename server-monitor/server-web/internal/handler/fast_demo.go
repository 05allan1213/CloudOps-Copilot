package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"server-web/internal/remediation"
	"server-web/internal/verification"
)

type FastDemoApplication interface {
	CreatePlan(context.Context, string) (*remediation.RemediationPlan, error)
	Execute(context.Context, string) (*verification.Run, error)
	Verify(context.Context, string) (*verification.Run, error)
}

func (h *Handler) SetFastDemo(service FastDemoApplication, actor string) {
	h.fastDemo = service
	h.fastDemoActor = strings.TrimSpace(actor)
}

func (h *Handler) CreateFastDemoPlan(c *gin.Context) {
	if h.fastDemo == nil || uuid.Validate(c.Param("id")) != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid fast demo request"})
		return
	}
	plan, err := h.fastDemo.CreatePlan(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeRemediationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, response{Status: "success", Data: toRemediationDTO(plan)})
}

func (h *Handler) ExecuteFastDemo(c *gin.Context) {
	if h.fastDemo == nil || uuid.Validate(c.Param("id")) != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid fast demo request"})
		return
	}
	run, err := h.fastDemo.Execute(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, response{Status: "success", Data: toVerificationRunDTO(run)})
}

func (h *Handler) VerifyFastDemo(c *gin.Context) {
	if h.fastDemo == nil || uuid.Validate(c.Param("id")) != nil {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid fast demo request"})
		return
	}
	run, err := h.fastDemo.Verify(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusConflict, response{Status: "error", Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: toVerificationRunDTO(run)})
}
