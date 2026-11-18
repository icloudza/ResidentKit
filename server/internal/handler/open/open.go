// Package open 开放 API（/open/v1）：供外部系统用 API 密钥查询设备、下发远程指令、读取事件。
//
// 挂在设备网关的 TLS 端口上（控制台仍只在本机监听）。响应格式与控制台接口相同 {code, msg, data}，
// 并配合标准 HTTP 状态码；接口说明见 docs/open-api.md 与 GET /open/v1/openapi.json。
//
// 命令即接口：每个设备命令一个路径 POST /devices/{id}/commands/{模块}/{命令}，请求体是参数，
// 默认同步返回结果（RPC 语义）；命令的参数与返回字段来自设备上报的说明（模块的 +commandSchema）。
//
// 并发：接口本身无状态，可任意并发调用。同一设备的指令由 InvocationService 控制在途数量、
// 按提交顺序排队下发；带 Idempotency-Key 的重试只执行一次。
package open

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

//go:embed openapi.json
var spec []byte // 基础规范；完整规范由 BuildSpec 加上各设备的命令生成

const ctxPrincipal = "open.principal"

// Deps 开放 API 的依赖。OnAuthFail 由网关提供：无效密钥计入 IP 封禁。
type Deps struct {
	Log         *zap.Logger
	Keys        *service.APIKeyService
	Devices     *service.DeviceService
	Invocations *service.InvocationService
	Events      *service.EventService
	OnAuthFail  func(ip string)
}

var idemRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

func NewRouter(d Deps) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	_ = r.SetTrustedProxies(nil)
	r.Use(middleware.RequestLog(d.Log), middleware.Recover(), middleware.BodyLimit(64<<10, nil),
		func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })

	h := &handler{d: d}
	g := r.Group("/open/v1")
	g.GET("/openapi.json", h.spec)

	a := g.Group("", h.auth)
	a.GET("/devices", h.need(model.ScopeDevicesRead), h.listDevices)
	a.GET("/devices/:id", h.need(model.ScopeDevicesRead), h.getDevice)
	a.GET("/devices/:id/commands", h.need(model.ScopeDevicesRead), h.deviceCommands)
	a.GET("/commands", h.need(model.ScopeDevicesRead), h.catalog)
	// 命令即接口：每个设备命令一个独立路径，请求体就是命令参数。
	a.POST("/devices/:id/commands/:module/:command", h.need(model.ScopeCommandsInvoke), h.call)
	a.GET("/invocations/:id", h.need(model.ScopeCommandsInvoke), h.getInvocation)
	a.GET("/devices/:id/events", h.need(model.ScopeEventsRead), h.listEvents)

	r.NoRoute(func(c *gin.Context) { response.Error(c, errcode.NotFound.WithMessage("接口不存在")) })
	r.NoMethod(func(c *gin.Context) { response.Error(c, errcode.NotFound.WithMessage("不支持该请求方法")) })
	return r
}

type handler struct{ d Deps }

func principal(c *gin.Context) *service.APIPrincipal {
	return c.MustGet(ctxPrincipal).(*service.APIPrincipal)
}

// trustedPrincipal 进程内调用（控制台在线调试）预先放入请求 context 的身份。
// context 只能在进程内设置，外部请求无法伪造。
type trustedPrincipal struct{}

// WithPrincipal 返回带指定身份的 context，供进程内转发的请求跳过密钥校验。
func WithPrincipal(ctx context.Context, p *service.APIPrincipal) context.Context {
	return context.WithValue(ctx, trustedPrincipal{}, p)
}

// auth 校验 Authorization: Bearer rkk_…；无效密钥计入来源 IP 的封禁计数。
func (h *handler) auth(c *gin.Context) {
	if p, ok := c.Request.Context().Value(trustedPrincipal{}).(*service.APIPrincipal); ok && p != nil {
		c.Set(ctxPrincipal, p)
		c.Next()
		return
	}
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok {
		response.Error(c, errcode.Unauthorized.WithMessage("缺少 API 密钥"))
		return
	}
	p, err := h.d.Keys.Authenticate(c, token, c.ClientIP())
	if err != nil {
		if errcode.From(err).Code == errcode.Unauthorized.Code && h.d.OnAuthFail != nil {
			h.d.OnAuthFail(c.ClientIP())
		}
		response.Error(c, err)
		return
	}
	c.Set(ctxPrincipal, p)
	c.Next()
}

func (h *handler) need(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !principal(c).Has(scope) {
			response.Error(c, errcode.Forbidden.WithMessage("密钥没有 %s 权限", scope))
			return
		}
		c.Next()
	}
}

// deviceParam 解析路径中的设备 ID，并确认在密钥范围内（范围外一律 404，不暴露是否存在）。
func (h *handler) deviceParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 || !principal(c).CanAccess(id) {
		response.Error(c, errcode.NotFound.WithMessage("设备不存在"))
		return 0, false
	}
	return id, true
}

func (h *handler) listDevices(c *gin.Context) {
	list, err := h.d.Devices.List(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	p := principal(c)
	out := make([]dto.Device, 0, len(list))
	for _, d := range list {
		if p.CanAccess(d.ID) {
			out = append(out, service.PublicDevice(d))
		}
	}
	response.OK(c, out)
}

func (h *handler) getDevice(c *gin.Context) {
	id, ok := h.deviceParam(c)
	if !ok {
		return
	}
	d, err := h.d.Devices.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, service.PublicDevice(dto.DeviceFrom(d)))
}

// spec OpenAPI 规范：基础部分加上全部设备当前开放的命令（每个命令一个接口）。
func (h *handler) spec(c *gin.Context) {
	devs, err := h.d.Devices.Models(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json", BuildSpec(service.CommandCatalog(devs)))
}

func (h *handler) deviceCommands(c *gin.Context) {
	id, ok := h.deviceParam(c)
	if !ok {
		return
	}
	d, err := h.d.Devices.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, service.DeviceCommands(d))
}

// catalog 密钥范围内全部设备的命令合集。
func (h *handler) catalog(c *gin.Context) {
	devs, err := h.d.Devices.Models(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	p := principal(c)
	var mine = devs[:0]
	for _, d := range devs {
		if p.CanAccess(d.ID) {
			mine = append(mine, d)
		}
	}
	response.OK(c, service.CommandCatalog(mine))
}

// call 调用设备命令，RPC 语义：
//
//	默认同步，等待设备返回（?timeout=1–30 秒，默认 30），成功时 data.result 为设备返回的结果；
//	设备离线默认直接失败（409）；?ttl=秒 表示离线时排队；?async=true 立即返回 202，之后查询。
//	设备执行失败 422、结果未知 502、等待超时 504，均在 data 中带 invocation_id；
//	带 ttl 时设备离线、指令排队中返回 202。
func (h *handler) call(c *gin.Context) {
	id, ok := h.deviceParam(c)
	if !ok {
		return
	}
	module, command := c.Param("module"), c.Param("command")
	d, err := h.d.Devices.Get(c, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	sp := service.FindCommand(d, module, command)
	if sp == nil {
		response.Error(c, errcode.NotFound.WithMessage("设备没有开放命令 %s/%s", module, command))
		return
	}
	var args map[string]any
	if c.Request.ContentLength != 0 {
		if err := json.NewDecoder(c.Request.Body).Decode(&args); err != nil && !errors.Is(err, io.EOF) {
			response.Error(c, errcode.InvalidParams.WithMessage("请求体应为 JSON 对象"))
			return
		}
	}
	if args, err = service.ValidateArgs(sp, args); err != nil {
		response.Error(c, err)
		return
	}
	async := c.Query("async") == "true" || c.Query("async") == "1"
	timeout := 30
	if v, err := strconv.Atoi(c.Query("timeout")); err == nil {
		timeout = min(max(v, 1), 30)
	}
	ttl := -1 // 同步调用：离线直接失败
	if async {
		ttl = 0 // 异步：离线排队，默认 10 分钟
	}
	if v := c.Query("ttl"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 604800 {
			response.Error(c, errcode.InvalidParams.WithMessage("ttl 超出范围"))
			return
		}
		ttl = n
		if n == 0 {
			ttl = -1
		}
	}
	idem := c.GetHeader("Idempotency-Key")
	if idem != "" && !idemRe.MatchString(idem) {
		response.Error(c, errcode.InvalidParams.WithMessage("幂等键格式不正确"))
		return
	}
	wait := timeout
	if async {
		wait = 0
	}
	p := principal(c)
	inv, err := h.d.Invocations.CreateWith(c, p.Actor(c.ClientIP()), id, dto.InvokeRequest{
		Module: module, Command: command, Arguments: args, TTLSeconds: ttl, WaitSeconds: wait,
	}, service.CreateOptions{APIKeyID: p.KeyID, IdemKey: idem, DenyInternal: true})
	if err != nil {
		response.Error(c, err)
		return
	}
	pending := dto.CommandPending{InvocationID: inv.ID, Status: inv.Status}
	switch inv.Status {
	case model.InvSucceeded:
		out := dto.CommandResult{InvocationID: inv.ID, Result: inv.Result}
		if len(out.Result) == 0 {
			out.Result = json.RawMessage("{}")
		}
		if inv.SentAt != nil && inv.FinishedAt != nil {
			out.DurationMs = max(inv.FinishedAt.Sub(*inv.SentAt).Milliseconds(), 1)
		}
		response.OK(c, out)
	case model.InvFailed:
		pending.DeviceError = inv.ErrorCode
		msg := inv.ErrorMessage
		if msg == "" {
			msg = "设备执行失败"
		}
		response.ErrorData(c, errcode.CommandFailed.WithMessage("%s", msg), pending)
	case model.InvLost:
		response.ErrorData(c, errcode.ResultUnknown.WithMessage("结果未知：%s", inv.ErrorMessage), pending)
	case model.InvExpired:
		response.ErrorData(c, errcode.DeviceOffline.WithMessage("设备离线，排队超时未能下发"), pending)
	default: // pending / sent
		// 异步，或同步但带 ttl 且设备离线、指令仍在排队：已受理，上线后执行（202）。
		if async || (ttl > 0 && inv.Status == model.InvPending) {
			response.Status(c, http.StatusAccepted, pending)
			return
		}
		response.ErrorData(c, errcode.InvocationTimeout.WithMessage("设备未在 %d 秒内返回", timeout), pending)
	}
}

// getInvocation 只能查询本密钥发起的指令；?wait=N（0–30 秒）在完成前长轮询。
func (h *handler) getInvocation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, errcode.NotFound.WithMessage("指令不存在"))
		return
	}
	wait, _ := strconv.Atoi(c.Query("wait"))
	wait = min(max(wait, 0), 30)
	inv, err := h.d.Invocations.GetForKey(c, principal(c).KeyID, id, time.Duration(wait)*time.Second)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, inv)
}

func (h *handler) listEvents(c *gin.Context) {
	id, ok := h.deviceParam(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("page_size"))
	f := repository.EventFilter{ListQuery: repository.ListQuery{Page: page, PageSize: size}, DeviceID: id, Type: c.Query("type")}
	for _, l := range strings.Split(c.Query("level"), ",") {
		if l = strings.TrimSpace(l); l != "" && model.ValidLevel(l) == l {
			f.Levels = append(f.Levels, l)
		}
	}
	if s := c.Query("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			response.Error(c, errcode.InvalidParams.WithMessage("since 时间格式不正确"))
			return
		}
		f.Since = t
	}
	out, err := h.d.Events.List(c, f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}
