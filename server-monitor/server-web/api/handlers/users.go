package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	authpkg "server-web/auth"
)

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (h *Handler) Register(c *gin.Context) {
	if h.authService == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "auth service unavailable",
		})
		return
	}

	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "invalid register request",
		})
		return
	}

	identity, err := h.authService.Register(c.Request.Context(), req.Username, req.Password, req.Role)
	if errors.Is(err, authpkg.ErrUsernameInvalid) || errors.Is(err, authpkg.ErrPasswordTooShort) || errors.Is(err, authpkg.ErrRoleInvalid) {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}
	if errors.Is(err, authpkg.ErrUserExists) {
		c.JSON(http.StatusConflict, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  "register failed",
		})
		return
	}

	c.JSON(http.StatusCreated, response{
		Status: "success",
		Data: authUserResponse{
			ID:       identity.ID,
			Username: identity.Username,
			Role:     identity.Role,
		},
	})
}

func (h *Handler) ListUsers(c *gin.Context) {
	if h.authService == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "auth service unavailable",
		})
		return
	}

	users, err := h.authService.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  "list users failed",
		})
		return
	}

	items := make([]authUserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, authUserResponse{
			ID:       u.ID,
			Username: u.Username,
			Role:     u.Role,
		})
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   items,
	})
}

func (h *Handler) DeleteUser(c *gin.Context) {
	if h.authService == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "auth service unavailable",
		})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "invalid user id",
		})
		return
	}

	userID, _ := c.Get("user_id")
	if uid, ok := userID.(uint64); ok && uid == id {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "cannot delete yourself",
		})
		return
	}

	if err := h.authService.DeleteUser(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  "delete user failed",
		})
		return
	}

	c.Status(http.StatusNoContent)
}
