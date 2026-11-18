package v1

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/config"
	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type EnrollHandler struct {
	enroll *service.EnrollService
	info   dto.EnrollTicketOverview // 二维码要用的服务器信息（不含接入码列表）
}

// NewEnrollHandler pin 为网关自己做 TLS 时的证书指纹（经反代时传空）。
func NewEnrollHandler(e *service.EnrollService, cfg *config.Config, pin string) *EnrollHandler {
	return &EnrollHandler{enroll: e, info: dto.EnrollTicketOverview{
		AppScheme: cfg.Enroll.AppScheme, ServerURL: deviceURL(cfg.Gateway.PublicURL, cfg.Gateway.Path),
		Path: cfg.Gateway.Path, Pin: pin}}
}

// deviceURL 由对外地址推出设备连接地址：https://x → wss://x/rk。
func deviceURL(publicURL, path string) string {
	u := strings.TrimRight(publicURL, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://") + path
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://") + path
	}
	return ""
}

// Tickets 接入码
//
//	@Summary	「添加设备」：二维码所需的服务器信息与全部接入码
//	@Tags		设备
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.EnrollTicketOverview}
//	@Router		/enroll-tickets [get]
func (h *EnrollHandler) Tickets(c *gin.Context) {
	list, err := h.enroll.Tickets(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := h.info
	out.Tickets = list
	response.OK(c, out)
}

// CreateTicket 生成接入码
//
//	@Summary	生成接入码（明文只返回这一次）；设备扫码后带着它申请接入会直接批准
//	@Tags		设备
//	@Security	Bearer
//	@Param		body	body		dto.CreateEnrollTicketRequest	true	"名称、有效期（分钟）、可用次数"
//	@Success	200		{object}	response.Body{data=dto.IssuedEnrollTicket}
//	@Router		/enroll-tickets [post]
func (h *EnrollHandler) CreateTicket(c *gin.Context) {
	var req dto.CreateEnrollTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	t, err := h.enroll.CreateTicket(c, middleware.ActorFrom(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, t)
}

// RevokeTicket 吊销接入码
//
//	@Summary	吊销接入码（立即失效，已接入的设备不受影响）
//	@Tags		设备
//	@Security	Bearer
//	@Param		id	path		int	true	"接入码 ID"
//	@Success	200	{object}	response.Body
//	@Router		/enroll-tickets/{id}/revoke [post]
func (h *EnrollHandler) RevokeTicket(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.enroll.RevokeTicket(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Pending 待批准的接入申请
//
//	@Summary	待批准的设备接入申请
//	@Tags		设备
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=[]dto.Enrollment}
//	@Router		/enrollments [get]
func (h *EnrollHandler) Pending(c *gin.Context) {
	list, err := h.enroll.Pending(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// Decide 同意或拒绝接入
//
//	@Summary	同意（approve）或拒绝（reject）设备接入
//	@Tags		设备
//	@Security	Bearer
//	@Param		id		path		int		true	"申请 ID"
//	@Param		action	path		string	true	"approve | reject"
//	@Success	200		{object}	response.Body
//	@Router		/enrollments/{id}/{action} [post]
func (h *EnrollHandler) Decide(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var approve bool
	switch c.Param("action") {
	case "approve":
		approve = true
	case "reject":
	default:
		response.Error(c, errcode.InvalidParams)
		return
	}
	if err := h.enroll.Decide(c, middleware.ActorFrom(c), id, approve); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}
