package v1

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type InvocationHandler struct {
	invocations *service.InvocationService
}

func NewInvocationHandler(i *service.InvocationService) *InvocationHandler {
	return &InvocationHandler{invocations: i}
}

// List 指令记录
//
//	@Summary	指令记录（device_id 为空则全部）
//	@Tags		指令
//	@Security	Bearer
//	@Param		device_id	query		int	false	"设备 ID"
//	@Param		status		query		string	false	"状态，逗号分隔，如 failed,lost"
//	@Param		since		query		string	false	"1h | 24h | 7d | 30d | all（默认全部）"
//	@Param		page		query		int	false	"页码，从 1 开始"
//	@Param		page_size	query		int	false	"每页条数，默认 20，最大 200"
//	@Success	200			{object}	response.Body{data=dto.InvocationPage}
//	@Router		/invocations [get]
func (h *InvocationHandler) List(c *gin.Context) {
	f := repository.InvocationFilter{ListQuery: listQuery(c), DeviceID: queryUint(c, "device_id"), Since: sinceParam(c, 0)}
	for _, st := range strings.Split(c.Query("status"), ",") {
		if st = strings.TrimSpace(st); st != "" {
			f.Statuses = append(f.Statuses, st)
		}
	}
	page, err := h.invocations.List(c, f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, page)
}

// Get 单条指令
//
//	@Summary	单条指令
//	@Tags		指令
//	@Security	Bearer
//	@Param		id	path		int	true	"指令 ID"
//	@Success	200	{object}	response.Body{data=dto.Invocation}
//	@Router		/invocations/{id} [get]
func (h *InvocationHandler) Get(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	inv, err := h.invocations.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, inv)
}
