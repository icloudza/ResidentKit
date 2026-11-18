package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type AuditHandler struct {
	audit *service.AuditService
}

func NewAuditHandler(a *service.AuditService) *AuditHandler { return &AuditHandler{audit: a} }

// List 审计日志
//
//	@Summary	审计日志
//	@Tags		审计
//	@Security	Bearer
//	@Param		page		query		int	false	"页码，从 1 开始"
//	@Param		page_size	query		int	false	"每页条数，默认 20，最大 200"
//	@Success	200		{object}	response.Body{data=dto.AuditLogPage}
//	@Router		/audit-logs [get]
func (h *AuditHandler) List(c *gin.Context) {
	q := listQuery(c)
	page, err := h.audit.List(c, q)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, page)
}
