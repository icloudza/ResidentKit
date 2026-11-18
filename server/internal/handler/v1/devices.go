package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type DeviceHandler struct {
	devices     *service.DeviceService
	invocations *service.InvocationService
	config      *service.ConfigService
}

func NewDeviceHandler(d *service.DeviceService, i *service.InvocationService, c *service.ConfigService) *DeviceHandler {
	return &DeviceHandler{devices: d, invocations: i, config: c}
}

// List 设备列表
//
//	@Summary	全部设备，在线的在前
//	@Tags		设备
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=[]dto.Device}
//	@Router		/devices [get]
func (h *DeviceHandler) List(c *gin.Context) {
	list, err := h.devices.List(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// Get 设备详情
//
//	@Summary	设备详情
//	@Tags		设备
//	@Security	Bearer
//	@Param		id	path		int	true	"设备 ID"
//	@Success	200	{object}	response.Body{data=dto.Device}
//	@Router		/devices/{id} [get]
func (h *DeviceHandler) Get(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	d, err := h.devices.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.DeviceFrom(d))
}

// Update 修改设备别名
//
//	@Summary	修改设备别名
//	@Tags		设备
//	@Security	Bearer
//	@Param		id		path		int						true	"设备 ID"
//	@Param		body	body		dto.UpdateDeviceRequest	true	"别名"
//	@Success	200		{object}	response.Body
//	@Router		/devices/{id} [patch]
func (h *DeviceHandler) Update(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	if err := h.devices.SetAlias(c, middleware.ActorFrom(c), id, req.Alias); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Commands 设备开放的命令及说明
//
//	@Summary	设备开放的远程命令（名称、图标、参数与返回字段取自设备上报的说明，不含系统内部命令）
//	@Tags		设备
//	@Security	Bearer
//	@Param		id	path		int	true	"设备 ID"
//	@Success	200	{object}	response.Body{data=[]dto.CommandSpec}
//	@Router		/devices/{id}/commands [get]
func (h *DeviceHandler) Commands(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	d, err := h.devices.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, service.DeviceCommands(d))
}

// Delete 删除设备
//
//	@Summary	删除离线设备：吊销其令牌，并删除它的指令、事件、配置与更新记录
//	@Tags		设备
//	@Security	Bearer
//	@Param		id	path		int	true	"设备 ID"
//	@Success	200	{object}	response.Body
//	@Router		/devices/{id} [delete]
func (h *DeviceHandler) Delete(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.devices.Delete(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Invoke 下发远程指令
//
//	@Summary	下发远程指令；设备离线时排队，可选同步等待结果
//	@Tags		指令
//	@Security	Bearer
//	@Param		id		path		int					true	"设备 ID"
//	@Param		body	body		dto.InvokeRequest	true	"指令"
//	@Success	200		{object}	response.Body{data=dto.Invocation}
//	@Router		/devices/{id}/invocations [post]
func (h *DeviceHandler) Invoke(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.InvokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	inv, err := h.invocations.Create(c, middleware.ActorFrom(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, inv)
}

// GetConfig 设备配置
//
//	@Summary	设备的可配置项、期望配置与同步状态
//	@Tags		设备
//	@Security	Bearer
//	@Param		id	path		int	true	"设备 ID"
//	@Success	200	{object}	response.Body{data=dto.DeviceConfig}
//	@Router		/devices/{id}/config [get]
func (h *DeviceHandler) GetConfig(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	cfg, err := h.config.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, cfg)
}

// UpdateConfig 修改设备配置
//
//	@Summary	保存设备配置（整体替换，未给出的项恢复默认）并立即下发
//	@Tags		设备
//	@Security	Bearer
//	@Param		id		path		int						true	"设备 ID"
//	@Param		body	body		dto.UpdateConfigRequest	true	"{模块: {key: 值}}"
//	@Success	200		{object}	response.Body{data=dto.DeviceConfig}
//	@Router		/devices/{id}/config [put]
func (h *DeviceHandler) UpdateConfig(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	cfg, err := h.config.Update(c, middleware.ActorFrom(c), id, req.Config)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, cfg)
}
