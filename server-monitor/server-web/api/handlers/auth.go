package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	authpkg "server-web/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authUserResponse struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// @Summary      用户登录
// @Description  使用用户名和密码登录，返回 JWT 令牌
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body  loginRequest  true  "登录请求"
// @Success      200  {object}  response
// @Failure      400  {object}  response
// @Failure      401  {object}  response
// @Failure      503  {object}  response
// @Router       /auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	if h.authService == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "auth service unavailable",
		})
		return
	}

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "invalid login request",
		})
		return
	}

	result, err := h.authService.Login(c.Request.Context(), req.Username, req.Password)
	if errors.Is(err, authpkg.ErrInvalidCredentials) {
		c.JSON(http.StatusUnauthorized, response{
			Status: "error",
			Error:  "invalid username or password",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  "login failed",
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: gin.H{
			"token":      result.Token,
			"expires_at": result.ExpiresAt,
			"user": authUserResponse{
				ID:       result.User.ID,
				Username: result.User.Username,
				Role:     result.User.Role,
			},
		},
	})
}

// @Summary      获取当前用户信息
// @Description  根据 Bearer Token 获取当前登录用户信息
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response
// @Failure      401  {object}  response
// @Failure      503  {object}  response
// @Router       /auth/me [get]
func (h *Handler) Me(c *gin.Context) {
	if h.authService == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "auth service unavailable",
		})
		return
	}

	identity, err := h.authService.AuthenticateBearer(c.GetHeader("Authorization"))
	if errors.Is(err, authpkg.ErrBearerTokenMissing) {
		c.JSON(http.StatusUnauthorized, response{
			Status: "error",
			Error:  "authorization header required",
		})
		return
	}
	if errors.Is(err, authpkg.ErrInvalidToken) || errors.Is(err, authpkg.ErrExpiredToken) {
		c.JSON(http.StatusUnauthorized, response{
			Status: "error",
			Error:  "invalid or expired token",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusUnauthorized, response{
			Status: "error",
			Error:  "invalid or expired token",
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: authUserResponse{
			ID:       identity.ID,
			Username: identity.Username,
			Role:     identity.Role,
		},
	})
}
