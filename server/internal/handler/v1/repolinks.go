package v1

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

// RepoLinkHandler 软件源安装链接（控制台「发布」页）。
type RepoLinkHandler struct {
	repo    *service.RepoService
	baseURL string // gateway.public_url
}

func NewRepoLinkHandler(r *service.RepoService, publicURL string) *RepoLinkHandler {
	return &RepoLinkHandler{repo: r, baseURL: strings.TrimRight(publicURL, "/")}
}

// Overview 软件源概况
//
//	@Summary	软件源是否开启、当前提供的版本与全部安装链接
//	@Tags		发布
//	@Security	Bearer
//	@Success	200	{object}	response.Body{data=dto.RepoOverview}
//	@Router		/repo-links [get]
func (h *RepoLinkHandler) Overview(c *gin.Context) {
	out, err := h.repo.Overview(c, h.baseURL)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

// Create 生成安装链接
//
//	@Summary	生成安装链接（需再次输入密码，完整地址只返回这一次）
//	@Tags		发布
//	@Security	Bearer
//	@Param		body	body		dto.CreateRepoLinkRequest	true	"名称、有效期、下载次数上限与当前密码"
//	@Success	200		{object}	response.Body{data=dto.IssuedRepoLink}
//	@Router		/repo-links [post]
func (h *RepoLinkHandler) Create(c *gin.Context) {
	var req dto.CreateRepoLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	l, err := h.repo.Create(c, middleware.ActorFrom(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, l)
}

// Revoke 吊销安装链接
//
//	@Summary	吊销安装链接（立即失效，已添加该源的 Sileo 刷新会失败）
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"链接 ID"
//	@Success	200	{object}	response.Body
//	@Router		/repo-links/{id}/revoke [post]
func (h *RepoLinkHandler) Revoke(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.repo.Revoke(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Downloads 下载记录
//
//	@Summary	经该链接下载安装包的记录（最近 100 条）
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"链接 ID"
//	@Success	200	{object}	response.Body{data=[]dto.RepoDownload}
//	@Router		/repo-links/{id}/downloads [get]
func (h *RepoLinkHandler) Downloads(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	list, err := h.repo.Downloads(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, list)
}

// Reveal 查看链接地址
//
//	@Summary	再次查看安装链接的地址（旧版本生成的链接无法查看）
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"链接 ID"
//	@Success	200	{object}	response.Body{data=dto.RepoLinkPath}
//	@Router		/repo-links/{id}/reveal [get]
func (h *RepoLinkHandler) Reveal(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, err := h.repo.Reveal(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, p)
}

// Update 修改安装链接
//
//	@Summary	修改名称、有效期、下载次数上限（过期或用完的链接改后可重新生效）
//	@Tags		发布
//	@Security	Bearer
//	@Param		id		path		int							true	"链接 ID"
//	@Param		body	body		dto.UpdateRepoLinkRequest	true	"名称、有效期（不填不变）、下载次数上限"
//	@Success	200		{object}	response.Body{data=dto.RepoLink}
//	@Router		/repo-links/{id} [put]
func (h *RepoLinkHandler) Update(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateRepoLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.InvalidParams)
		return
	}
	l, err := h.repo.Update(c, middleware.ActorFrom(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, l)
}

// Delete 删除安装链接
//
//	@Summary	删除已吊销的安装链接及其下载记录
//	@Tags		发布
//	@Security	Bearer
//	@Param		id	path		int	true	"链接 ID"
//	@Success	200	{object}	response.Body
//	@Router		/repo-links/{id} [delete]
func (h *RepoLinkHandler) Delete(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.repo.Delete(c, middleware.ActorFrom(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}
