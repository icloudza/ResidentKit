package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type TokenHandler struct {
	devices *service.DeviceService
}

func NewTokenHandler(d *service.DeviceService) *TokenHandler { return &TokenHandler{devices: d} }

// List 设备令牌列表
//
//	@Summary	设备令牌（设备申请接入、管理员同意后自动签发）
//	@Tags		令牌
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=[]dto.DeviceToken}
//	@Router		/tokens [get]
func (h *TokenHandler) List(c *gin.Context) {
	list, err := h.devices.ListTokens(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// Revoke 吊销设备令牌
//
//	@Summary	吊销设备令牌（已连接的设备在下次重连时被拒）
//	@Tags		令牌
//	@Security	Bearer
//	@Param		id	path		int	true	"令牌 ID"
//	@Success	200	{object}	response.Body
//	@Router		/tokens/{id}/revoke [post]
func (h *TokenHandler) Revoke(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.devices.RevokeToken(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Delete 删除设备令牌
//
//	@Summary	删除已吊销的设备令牌（未吊销的须先吊销）
//	@Tags		令牌
//	@Security	Bearer
//	@Param		id	path		int	true	"令牌 ID"
//	@Success	200	{object}	response.Body
//	@Router		/tokens/{id} [delete]
func (h *TokenHandler) Delete(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.devices.DeleteToken(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}
