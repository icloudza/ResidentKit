package v1

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type DashboardHandler struct {
	dashboard *service.DashboardService
}

func NewDashboardHandler(d *service.DashboardService) *DashboardHandler {
	return &DashboardHandler{dashboard: d}
}

// Summary 总览
//
//	@Summary	总览：设备、事件、指令与网关的 24 小时统计
//	@Tags		总览
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.Summary}
//	@Router		/dashboard/summary [get]
func (h *DashboardHandler) Summary(c *gin.Context) {
	s, err := h.dashboard.Summary(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, s)
}

// EventTypes 事件类型
//
//	@Summary	各事件类型的数量（since 支持 1h、24h、7d，默认 24h）
//	@Tags		事件
//	@Security	Bearer
//	@Param		since	query		string	false	"时间范围"
//	@Success	200		{object}	response.Body{data=[]dto.TypeCount}
//	@Router		/events/types [get]
func (h *DashboardHandler) EventTypes(c *gin.Context) {
	list, err := h.dashboard.EventTypes(c, sinceParam(c, 24*time.Hour))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// sinceParam 解析 ?since=1h|24h|7d|30d，非法值用默认。
func sinceParam(c *gin.Context, def time.Duration) time.Time {
	d := def
	switch c.Query("since") {
	case "1h":
		d = time.Hour
	case "24h":
		d = 24 * time.Hour
	case "7d":
		d = 7 * 24 * time.Hour
	case "30d":
		d = 30 * 24 * time.Hour
	case "all":
		return time.Time{}
	}
	if d == 0 {
		return time.Time{} // 未指定且无默认：不限
	}
	return time.Now().UTC().Add(-d)
}

// DeviceActivity 设备活动概览
//
//	@Summary	设备活动概览：指令与事件按时间分桶
//	@Tags		设备
//	@Security	Bearer
//	@Param		id		path		int		true	"设备 ID"
//	@Param		range	query		string	false	"24h（默认，按小时）| 7d | 30d（按天）"
//	@Param		tz		query		int		false	"浏览器时区相对 UTC 的分钟数，如东八区 480"
//	@Success	200		{object}	response.Body{data=dto.DeviceActivity}
//	@Router		/devices/{id}/activity [get]
func (h *DashboardHandler) DeviceActivity(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	tz, _ := strconv.Atoi(c.Query("tz"))
	a, err := h.dashboard.DeviceActivity(c, id, c.Query("range"), tz)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, a)
}
