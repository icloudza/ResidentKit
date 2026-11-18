package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	openapi "github.com/icloudza/ResidentKit/server/internal/handler/open"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type APIKeyHandler struct {
	keys  *service.APIKeyService
	stats repository.APIStatsRepository
	info  dto.OpenAPIInfo
	open  http.Handler // 开放 API 的处理器，「在线调试」在进程内调用它
	// catalog 全部设备的命令目录；spec 由目录生成的 OpenAPI 规范。
	catalog func(ctx context.Context) ([]dto.CommandSpec, error)
	spec    func(cat []dto.CommandSpec) []byte
}

func NewAPIKeyHandler(k *service.APIKeyService, stats repository.APIStatsRepository, info dto.OpenAPIInfo, open http.Handler,
	catalog func(ctx context.Context) ([]dto.CommandSpec, error), spec func([]dto.CommandSpec) []byte) *APIKeyHandler {
	return &APIKeyHandler{keys: k, stats: stats, info: info, open: open, catalog: catalog, spec: spec}
}

// Commands 命令目录
//
//	@Summary	全部设备开放给开放 API 的命令（每个命令一个接口），含参数与返回字段
//	@Tags		开放 API
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=[]dto.CommandSpec}
//	@Router		/api-keys/commands [get]
func (h *APIKeyHandler) Commands(c *gin.Context) {
	cat, err := h.catalog(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, cat)
}

// Info 开放 API 接入信息
//
//	@Summary	开放 API 的地址与服务器证书指纹
//	@Tags		开放 API
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.OpenAPIInfo}
//	@Router		/api-keys/info [get]
func (h *APIKeyHandler) Info(c *gin.Context) { response.OK(c, h.info) }

// List API 密钥列表
//
//	@Summary	API 密钥列表
//	@Tags		开放 API
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=[]dto.APIKey}
//	@Router		/api-keys [get]
func (h *APIKeyHandler) List(c *gin.Context) {
	list, err := h.keys.List(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// Create 创建 API 密钥
//
//	@Summary	创建 API 密钥（需再次输入密码，明文只返回这一次）
//	@Tags		开放 API
//	@Security	Bearer
//	@Param		body	body		dto.CreateAPIKeyRequest	true	"名称、权限、范围"
//	@Success	200		{object}	response.Body{data=dto.IssuedAPIKey}
//	@Router		/api-keys [post]
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req dto.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	k, err := h.keys.Create(c, middleware.ActorFrom(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, k)
}

// Revoke 吊销 API 密钥
//
//	@Summary	吊销 API 密钥（立即生效）
//	@Tags		开放 API
//	@Security	Bearer
//	@Param		id	path		int	true	"密钥 ID"
//	@Success	200	{object}	response.Body
//	@Router		/api-keys/{id}/revoke [post]
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.keys.Revoke(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Delete 删除 API 密钥
//
//	@Summary	删除已吊销的 API 密钥
//	@Tags		开放 API
//	@Security	Bearer
//	@Param		id	path		int	true	"密钥 ID"
//	@Success	200	{object}	response.Body
//	@Router		/api-keys/{id} [delete]
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.keys.Delete(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Update 编辑 API 密钥
//
//	@Summary	编辑密钥的名称、权限、设备范围、IP 白名单与有效期（需再次输入密码，立即生效）
//	@Tags		开放 API
//	@Security	Bearer
//	@Param		id		path		int							true	"密钥 ID"
//	@Param		body	body		dto.UpdateAPIKeyRequest	true	"新的设置"
//	@Success	200		{object}	response.Body{data=dto.APIKey}
//	@Router		/api-keys/{id} [put]
func (h *APIKeyHandler) Update(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	k, err := h.keys.Update(c, middleware.ActorFrom(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, k)
}

// Stats 开放 API 概况
//
//	@Summary	近 24 小时调用量、成功率、耗时、走势、热门命令与每个密钥的走势
//	@Tags		开放 API
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.APIStats}
//	@Router		/api-keys/stats [get]
func (h *APIKeyHandler) Stats(c *gin.Context) {
	st, err := h.keys.Stats(c, h.stats)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, st)
}

// Calls 开放 API 调用记录
//
//	@Summary	调用记录（新到旧），可按密钥、设备、状态、时间筛选
//	@Tags		开放 API
//	@Security	Bearer
//	@Param		key_id		query		int		false	"密钥 ID"
//	@Param		device_id	query		int		false	"设备 ID"
//	@Param		status		query		string	false	"逗号分隔：pending,sent,succeeded,failed,expired,lost"
//	@Param		since		query		string	false	"1h | 24h | 7d | 30d | all，默认 24h"
//	@Param		page		query		int		false	"页码"
//	@Param		page_size	query		int		false	"每页条数"
//	@Success	200			{object}	response.Body{data=dto.APICallPage}
//	@Router		/api-calls [get]
func (h *APIKeyHandler) Calls(c *gin.Context) {
	f := repository.APICallFilter{ListQuery: listQuery(c), KeyID: queryUint(c, "key_id"), DeviceID: queryUint(c, "device_id"),
		Since: sinceParam(c, 24*time.Hour)}
	for _, st := range strings.Split(c.Query("status"), ",") {
		if st = strings.TrimSpace(st); st != "" {
			f.Statuses = append(f.Statuses, st)
		}
	}
	page, err := h.keys.Calls(c, h.stats, f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, page)
}

// Spec OpenAPI 规范
//
//	@Summary	开放 API 的 OpenAPI 3 规范（与 /open/v1/openapi.json 相同），供控制台下载
//	@Tags		开放 API
//	@Security	Bearer
//	@Produce	json
//	@Router		/api-keys/openapi.json [get]
func (h *APIKeyHandler) Spec(c *gin.Context) {
	cat, err := h.catalog(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="residentkit-openapi.json"`)
	c.Data(http.StatusOK, "application/json", h.spec(cat))
}

// Try 在线调试
//
//	@Summary	用给定的密钥调用一次开放 API（服务端在进程内转发，结果原样返回）
//	@Tags		开放 API
//	@Security	Bearer
//	@Param		body	body		dto.APITryRequest	true	"密钥、方法、路径与请求体"
//	@Success	200		{object}	response.Body{data=dto.APITryResponse}
//	@Router		/api-keys/try [post]
func (h *APIKeyHandler) Try(c *gin.Context) {
	var req dto.APITryRequest
	if err := c.ShouldBindJSON(&req); err != nil || !strings.HasPrefix(req.Path, "/open/v1/") || strings.Contains(req.Path, "..") {
		response.Error(c, errcode.InvalidParams.WithMessage("路径无效"))
		return
	}
	if req.Body != nil && !json.Valid(req.Body) {
		response.Error(c, errcode.InvalidParams.WithMessage("请求体不是有效的 JSON"))
		return
	}
	if req.KeyID == 0 && strings.TrimSpace(req.Key) == "" {
		response.Error(c, errcode.InvalidParams.WithMessage("请选择密钥"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	if req.KeyID != 0 {
		p, err := h.keys.PrincipalForKey(ctx, req.KeyID)
		if err != nil {
			response.Error(c, err)
			return
		}
		ctx = openapi.WithPrincipal(ctx, p)
	}
	var body *bytes.Reader
	if req.Method == http.MethodPost {
		body = bytes.NewReader(req.Body)
	} else {
		body = bytes.NewReader(nil)
	}
	r, err := http.NewRequestWithContext(ctx, req.Method, "http://open"+req.Path, body)
	if err != nil {
		response.Error(c, errcode.InvalidParams.WithMessage("路径无效"))
		return
	}
	r.RemoteAddr = c.Request.RemoteAddr
	if req.KeyID == 0 {
		r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.Key))
	}
	r.Header.Set("Content-Type", "application/json")
	if req.IdemKey != "" {
		r.Header.Set("Idempotency-Key", req.IdemKey)
	}
	rec := httptest.NewRecorder()
	start := time.Now()
	h.open.ServeHTTP(rec, r)
	out := dto.APITryResponse{Status: rec.Code, DurationMs: float64(time.Since(start).Microseconds()) / 1000, Body: rec.Body.Bytes()}
	if !json.Valid(out.Body) {
		out.Body, _ = json.Marshal(rec.Body.String())
	}
	response.OK(c, out)
}
