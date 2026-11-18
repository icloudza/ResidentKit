package v1

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type EventHandler struct {
	events *service.EventService
}

func NewEventHandler(e *service.EventService) *EventHandler { return &EventHandler{events: e} }

// List 事件列表
//
//	@Summary	设备上报的事件
//	@Tags		事件
//	@Security	Bearer
//	@Param		device_id	query		int		false	"设备 ID"
//	@Param		type		query		string	false	"事件类型"
//	@Param		since		query		string	false	"时间范围：1h、24h、7d、30d，默认不限"
//	@Param		level		query		string	false	"级别，逗号分隔，如 warning,error"
//	@Param		page		query		int		false	"页码，从 1 开始"
//	@Param		page_size	query		int		false	"每页条数，默认 20，最大 200"
//	@Success	200			{object}	response.Body{data=dto.EventPage}
//	@Router		/events [get]
func (h *EventHandler) List(c *gin.Context) {
	f := repository.EventFilter{
		ListQuery: listQuery(c),
		DeviceID:  queryUint(c, "device_id"), Type: c.Query("type"),
		Since:  sinceParam(c, 0),
		Levels: levelsParam(c.Query("level")),
	}
	page, err := h.events.List(c, f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, page)
}

// levelsParam 解析 ?level=warning,error，忽略未知值。
func levelsParam(v string) []string {
	var out []string
	for _, l := range strings.Split(v, ",") {
		l = strings.TrimSpace(l)
		if l != "" && model.ValidLevel(l) == l {
			out = append(out, l)
		}
	}
	return out
}
