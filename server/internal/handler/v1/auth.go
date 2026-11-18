// Package v1 REST API v1 的处理函数：只做参数绑定、调用 service、输出响应。
//
//	@title			ResidentKit 控制台 API
//	@version		1.0
//	@BasePath		/api/v1
//	@securityDefinitions.apikey	Bearer
//	@in				header
//	@name			Authorization
package v1

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

const (
	refreshCookie = "rk_refresh"
	refreshPath   = "/api/v1/auth"
)

type AuthHandler struct {
	auth         *service.AuthService
	setup        *service.SetupService
	secureCookie bool
}

func NewAuthHandler(auth *service.AuthService, setup *service.SetupService, secureCookie bool) *AuthHandler {
	return &AuthHandler{auth: auth, setup: setup, secureCookie: secureCookie}
}

// SetupStatus 初始化状态
//
//	@Summary	是否需要首次初始化（尚无管理员）
//	@Tags		初始化
//	@Success	200	{object}	response.Body{data=dto.SetupStatus}
//	@Router		/setup/status [get]
func (h *AuthHandler) SetupStatus(c *gin.Context) {
	response.OK(c, dto.SetupStatus{NeedsSetup: h.setup.NeedsSetup()})
}

// Setup 首次初始化
//
//	@Summary	凭初始化码创建第一个管理员并登录（初始化码见服务日志）
//	@Tags		初始化
//	@Param		body	body		dto.SetupRequest	true	"初始化信息"
//	@Success	200		{object}	response.Body{data=dto.TokenResponse}
//	@Router		/setup [post]
func (h *AuthHandler) Setup(c *gin.Context) {
	var req dto.SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	s, err := h.setup.Complete(c, req, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Error(c, err)
		return
	}
	h.setRefresh(c, s.RefreshToken, s.RefreshExpires)
	response.OK(c, s.TokenResponse)
}

// Login 登录
//
//	@Summary	登录，返回访问令牌；刷新令牌写入 HttpOnly Cookie
//	@Tags		认证
//	@Param		body	body		dto.LoginRequest	true	"账号"
//	@Success	200		{object}	response.Body{data=dto.TokenResponse}
//	@Router		/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	s, err := h.auth.Login(c, req, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Error(c, err)
		return
	}
	h.setRefresh(c, s.RefreshToken, s.RefreshExpires)
	response.OK(c, s.TokenResponse)
}

// Refresh 刷新访问令牌
//
//	@Summary	用 Cookie 中的刷新令牌换新的访问令牌（刷新令牌同时轮换）
//	@Tags		认证
//	@Success	200	{object}	response.Body{data=dto.TokenResponse}
//	@Router		/auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	rt, err := c.Cookie(refreshCookie)
	if err != nil || rt == "" {
		response.Error(c, errcode.Unauthorized)
		return
	}
	s, err := h.auth.Refresh(c, rt, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.clearRefresh(c)
		response.Error(c, err)
		return
	}
	h.setRefresh(c, s.RefreshToken, s.RefreshExpires)
	response.OK(c, s.TokenResponse)
}

// Logout 退出登录
//
//	@Summary	作废刷新令牌
//	@Tags		认证
//	@Security	Bearer
//	@Success	200	{object}	response.Body
//	@Router		/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	rt, _ := c.Cookie(refreshCookie)
	h.auth.Logout(c, middleware.ActorFrom(c), rt)
	h.clearRefresh(c)
	response.OK(c, nil)
}

// Me 当前管理员
//
//	@Summary	当前登录的管理员
//	@Tags		认证
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.Admin}
//	@Router		/auth/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	a, err := h.auth.Me(c, middleware.ActorFrom(c).AdminID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, a)
}

// ChangePassword 修改密码
//
//	@Summary	修改密码，其他会话随之失效
//	@Tags		认证
//	@Security	Bearer
//	@Param		body	body		dto.ChangePasswordRequest	true	"新旧密码"
//	@Success	200		{object}	response.Body
//	@Router		/auth/password [post]
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams.WithMessage("新密码至少 8 位"))
		return
	}
	if err := h.auth.ChangePassword(c, middleware.ActorFrom(c), req); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// ChangeUsername 修改用户名
//
//	@Summary	修改用户名（需当前密码，开启两步验证时还需验证码）
//	@Tags		认证
//	@Security	Bearer
//	@Param		body	body		dto.ChangeUsernameRequest	true	"新用户名与当前密码"
//	@Success	200		{object}	response.Body{data=dto.Admin}
//	@Router		/auth/username [put]
func (h *AuthHandler) ChangeUsername(c *gin.Context) {
	var req dto.ChangeUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams.WithMessage("请填写新用户名和当前密码"))
		return
	}
	a, err := h.auth.ChangeUsername(c, middleware.ActorFrom(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, a)
}

// TOTPSetup 生成两步验证密钥
//
//	@Summary	生成两步验证密钥（需再调用 enable 确认）
//	@Tags		认证
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.TOTPSetupResponse}
//	@Router		/auth/totp/setup [post]
func (h *AuthHandler) TOTPSetup(c *gin.Context) {
	r, err := h.auth.TOTPSetup(c, middleware.ActorFrom(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, r)
}

// TOTPEnable 启用两步验证
//
//	@Summary	用验证码确认并启用两步验证
//	@Tags		认证
//	@Security	Bearer
//	@Param		body	body		dto.TOTPCodeRequest	true	"验证码"
//	@Success	200		{object}	response.Body
//	@Router		/auth/totp/enable [post]
func (h *AuthHandler) TOTPEnable(c *gin.Context) {
	var req dto.TOTPCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	if err := h.auth.TOTPEnable(c, middleware.ActorFrom(c), req.Code); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// TOTPDisable 关闭两步验证
//
//	@Summary	用验证码确认并关闭两步验证
//	@Tags		认证
//	@Security	Bearer
//	@Param		body	body		dto.TOTPCodeRequest	true	"验证码"
//	@Success	200		{object}	response.Body
//	@Router		/auth/totp/disable [post]
func (h *AuthHandler) TOTPDisable(c *gin.Context) {
	var req dto.TOTPCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	if err := h.auth.TOTPDisable(c, middleware.ActorFrom(c), req.Code); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *AuthHandler) setRefresh(c *gin.Context, token string, exp time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: refreshCookie, Value: token, Path: refreshPath,
		Expires: exp, HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode})
}

func (h *AuthHandler) clearRefresh(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: refreshCookie, Value: "", Path: refreshPath, MaxAge: -1,
		HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode})
}

// ---- 公共工具 ----

func pathID(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, errcode.InvalidParams)
		return 0, false
	}
	return id, true
}

// listQuery 读取 ?page=&page_size=。
func listQuery(c *gin.Context) repository.ListQuery {
	return repository.ListQuery{Page: queryInt(c, "page"), PageSize: queryInt(c, "page_size")}
}

func queryUint(c *gin.Context, name string) uint64 {
	v, _ := strconv.ParseUint(c.Query(name), 10, 64)
	return v
}

func queryInt(c *gin.Context, name string) int {
	v, _ := strconv.Atoi(c.Query(name))
	return v
}
