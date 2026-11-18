package v1

import (
	"errors"
	"io"
	"mime/multipart"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

// UploadMaxBytes 上传请求体上限（安装包上限 64 MB 加表单开销）。
const UploadMaxBytes = 66 << 20

type ReleaseHandler struct {
	ota         *service.OTAService
	repoEnabled bool
}

func NewReleaseHandler(o *service.OTAService, repoEnabled bool) *ReleaseHandler {
	return &ReleaseHandler{ota: o, repoEnabled: repoEnabled}
}

// Config OTA 配置
//
//	@Summary	是否已配置发布公钥
//	@Tags		发布
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.OTAConfig}
//	@Router		/releases/config [get]
func (h *ReleaseHandler) Config(c *gin.Context) {
	cfg := h.ota.Config()
	cfg.RepoEnabled = h.repoEnabled
	response.OK(c, cfg)
}

// List 版本列表
//
//	@Summary	全部版本，新到旧，附各更新状态的设备数
//	@Tags		发布
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=[]dto.Release}
//	@Router		/releases [get]
func (h *ReleaseHandler) List(c *gin.Context) {
	list, err := h.ota.List(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// Get 版本详情
//
//	@Summary	版本详情：灰度计划与每台设备的更新状态
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"版本 ID"
//	@Success	200	{object}	response.Body{data=dto.ReleaseDetail}
//	@Router		/releases/{id} [get]
func (h *ReleaseHandler) Get(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	d, err := h.ota.Detail(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, d)
}

// Upload 上传版本
//
//	@Summary	上传发布包（rk-release sign 生成的 .rkrel.zip，需再次输入密码）
//	@Description	multipart 字段顺序：password、totp_code、notes，最后是文件 bundle。
//	@Tags		发布
//	@Security	Bearer
//	@Accept		multipart/form-data
//	@Param		password	formData	string	true	"当前密码"
//	@Param		totp_code	formData	string	false	"两步验证码"
//	@Param		notes		formData	string	false	"更新说明"
//	@Param		repo		formData	string	false	"1 表示同步到软件源（源里改为提供这个版本）"
//	@Param		bundle		formData	file	true	"发布包 .rkrel.zip"
//	@Success	200			{object}	response.Body{data=dto.Release}
//	@Router		/releases [post]
func (h *ReleaseHandler) Upload(c *gin.Context) {
	mr, err := c.Request.MultipartReader()
	if err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	var password, totpCode, notes, repo string
	small := func(p *multipart.Part, n int64) (string, error) {
		b, err := io.ReadAll(io.LimitReader(p, n+1))
		if err != nil || int64(len(b)) > n {
			return "", errors.New("too large")
		}
		return string(b), nil
	}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			response.Error(c, errcode.InvalidParams.WithMessage("上传中断"))
			return
		}
		switch p.FormName() {
		case "password":
			password, err = small(p, 128)
		case "totp_code":
			totpCode, err = small(p, 6)
		case "notes":
			notes, err = small(p, 4000)
		case "repo":
			repo, err = small(p, 8)
		case "bundle":
			// 文件必须放在最后：先验证身份，不合格的不接收文件。
			a := middleware.ActorFrom(c)
			if err := h.ota.CheckUploader(c, a, password, totpCode); err != nil {
				response.Error(c, err)
				return
			}
			r, err := h.ota.Upload(c, a, notes, repo == "1" || repo == "true", p)
			if err != nil {
				response.Error(c, err)
				return
			}
			response.OK(c, r)
			return
		}
		if err != nil {
			response.Error(c, errcode.InvalidParams.WithMessage("字段 %s 过长", p.FormName()))
			return
		}
	}
	response.Error(c, errcode.InvalidParams.WithMessage("缺少发布包"))
}

// Withdraw 撤回版本
//
//	@Summary	撤回版本：停止相关灰度计划
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"版本 ID"
//	@Success	200	{object}	response.Body
//	@Router		/releases/{id}/withdraw [post]
func (h *ReleaseHandler) Withdraw(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.ota.Withdraw(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// CreateRollout 创建灰度计划
//
//	@Summary	创建灰度计划：全部设备，或指定设备（先试几台）
//	@Tags		发布
//	@Security	Bearer
//	@Param		id		path		int							true	"版本 ID"
//	@Param		body	body		dto.CreateRolloutRequest	true	"目标"
//	@Success	200		{object}	response.Body{data=dto.Rollout}
//	@Router		/releases/{id}/rollouts [post]
func (h *ReleaseHandler) CreateRollout(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.CreateRolloutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	ro, err := h.ota.CreateRollout(c, middleware.ActorFrom(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, ro)
}

// ChangeRollout 暂停 / 继续 / 取消灰度计划
//
//	@Summary	暂停 / 继续 / 取消灰度计划
//	@Tags		发布
//	@Security	Bearer
//	@Param		id		path		int		true	"计划 ID"
//	@Param		action	path		string	true	"pause | resume | cancel"
//	@Success	200		{object}	response.Body
//	@Router		/rollouts/{id}/{action} [post]
func (h *ReleaseHandler) ChangeRollout(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.ota.ChangeRollout(c, middleware.ActorFrom(c), id, c.Param("action")); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Retry 重试设备更新
//
//	@Summary	重试一台设备失败的更新
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"更新记录 ID"
//	@Success	200	{object}	response.Body
//	@Router		/device-updates/{id}/retry [post]
func (h *ReleaseHandler) Retry(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.ota.Retry(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// DeviceUpdate 设备更新信息
//
//	@Summary	设备当前版本、可更新到的版本与更新历史
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"设备 ID"
//	@Success	200	{object}	response.Body{data=dto.DeviceUpdateInfo}
//	@Router		/devices/{id}/update [get]
func (h *ReleaseHandler) DeviceUpdate(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	info, err := h.ota.DeviceInfo(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, info)
}

// SetRepo 同步到软件源
//
//	@Summary	同步到软件源或从源里移除（源里提供已同步、未撤回的最新版本）
//	@Tags		发布
//	@Security	Bearer
//	@Param		id		path		int							true	"版本 ID"
//	@Param		body	body		dto.SetReleaseRepoRequest	true	"是否同步"
//	@Success	200		{object}	response.Body
//	@Router		/releases/{id}/repo [post]
func (h *ReleaseHandler) SetRepo(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.SetReleaseRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	if err := h.ota.SetInRepo(c, middleware.ActorFrom(c), id, req.InRepo); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}
