package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/config"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

// Services 网关依赖的业务接口。
type Services struct {
	Devices     *service.DeviceService
	Events      *service.EventService
	Invocations *service.InvocationService
	OTA         *service.OTAService
	Config      *service.ConfigService
	Enroll      *service.EnrollService
}

// Hub 管理所有设备连接，并实现 service.Sender。
type Hub struct {
	cfg  config.GatewayConfig
	svc  Services
	log  *zap.Logger
	bans *banList
	prox proxies

	upgrader websocket.Upgrader

	denies hourly // 鉴权失败
	rtts   hourly // 心跳延迟（毫秒）

	open http.Handler // 开放 API（/open/v1），装配后设置
	repo http.Handler // 软件源（/repo/），开启时装配

	mu    sync.Mutex
	conns map[uint64]*conn // 设备 id → 当前连接
	perIP map[string]int
	total int
}

func NewHub(cfg config.GatewayConfig, svc Services, log *zap.Logger) *Hub {
	h := &Hub{cfg: cfg, svc: svc, log: log.Named("gateway"),
		bans:  newBanList(cfg.BanAfter, cfg.BanWindow, cfg.BanFor),
		prox:  parseProxies(cfg.TrustedProxies),
		conns: map[uint64]*conn{}, perIP: map[string]int{}}
	h.upgrader = websocket.Upgrader{
		HandshakeTimeout: cfg.HandshakeTimeout,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
		// 设备客户端不带 Origin；带 Origin 的是浏览器，一律拒绝，杜绝跨站使用。
		CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
	}
	return h
}

// ServeHTTP 处理 WebSocket 升级：限流 → 鉴权 → 升级 → 等待 hello。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := h.prox.realIP(r)
	// 后续环节（接入申请、开放 API 的 ClientIP）都读 RemoteAddr，这里统一换成真实 IP。
	r.RemoteAddr = net.JoinHostPort(ip, "0")
	// 开放 API：不占设备连接名额、不限流（调用方可大量并发），只受封禁约束；设读写截止时间防慢速连接。
	if strings.HasPrefix(r.URL.Path, "/open/") && h.open != nil {
		if h.bans.isBanned(ip) {
			closeHijacked(w)
			return
		}
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(30 * time.Second))
		_ = rc.SetWriteDeadline(time.Now().Add(75 * time.Second)) // 同步等待结果最长 30 秒
		h.open.ServeHTTP(w, r)
		return
	}
	// 软件源：未开启时整个路径 404；受封禁约束，下载大文件放宽写截止时间。
	if strings.HasPrefix(r.URL.Path, "/repo/") {
		if h.repo == nil {
			http.NotFound(w, r)
			return
		}
		if h.bans.isBanned(ip) {
			closeHijacked(w)
			return
		}
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(30 * time.Second))
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute))
		h.repo.ServeHTTP(w, r)
		return
	}
	enroll := r.URL.Path == EnrollPath && r.Method == http.MethodPost
	ota := strings.HasPrefix(r.URL.Path, OTAPrefix) && r.Method == http.MethodGet
	if enroll || ota {
		// 普通 HTTP 请求：同样受封禁、连接数上限约束，并设读写截止时间，防止慢速连接占住资源。
		if h.bans.isBanned(ip) {
			closeHijacked(w)
			return
		}
		if !h.acquire(ip) {
			http.Error(w, "too many connections", http.StatusServiceUnavailable)
			return
		}
		defer h.release(ip)
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(15 * time.Second))
		if enroll {
			_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
			h.serveEnroll(w, r, ip)
		} else {
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute)) // 安装包上限 64 MB
			h.serveOTA(w, r, ip)
		}
		return
	}
	if r.URL.Path != h.cfg.Path || r.Method != http.MethodGet {
		h.deny(ip, "bad path")
		http.NotFound(w, r)
		return
	}
	if h.bans.isBanned(ip) {
		closeHijacked(w)
		return
	}
	if !h.acquire(ip) {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	released := false
	release := func() {
		if !released {
			released = true
			h.release(ip)
		}
	}
	defer release()

	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		h.deny(ip, "no token")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tok, err := h.svc.Devices.Authenticate(r.Context(), token)
	if errors.Is(err, service.ErrTokenRevoked) {
		h.closeRevoked(w, r, ip)
		return
	}
	if errors.Is(err, service.ErrTokenUnknown) {
		h.deny(ip, "unknown token") // 计入封禁：设备收到关闭码后不会再用它，反复出现的只会是猜令牌的
		h.closeRevoked(w, r, ip)
		return
	}
	if err != nil {
		if errors.Is(err, service.ErrBadDeviceToken) {
			h.deny(ip, "bad token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		} else {
			h.log.Error("authenticate", zap.Error(err))
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade 已写出错误响应
	}
	c := newConn(h, ws, ip, tok)
	released = true // 由连接关闭时释放
	go c.run()
}

// SetRepo 装配软件源处理器（repo.enabled 时）。
func (h *Hub) SetRepo(handler http.Handler) { h.repo = handler }

// SetOpenAPI 装配开放 API 的处理器。
func (h *Hub) SetOpenAPI(handler http.Handler) { h.open = handler }

// DenyIP 外部（开放 API）鉴权失败时计入封禁。
func (h *Hub) DenyIP(ip, reason string) { h.deny(ip, reason) }

// CloseTokenRevoked WebSocket 关闭码：令牌已吊销，设备应丢弃令牌并重新申请接入（daemon net.link 对应处理）。
const CloseTokenRevoked = 4401

// closeRevoked 已吊销的令牌：完成握手后立即以 CloseTokenRevoked 关闭，告诉设备不要再用它重试。
// 不计入封禁——这是我们自己的设备，同一出口 IP 下的其他设备不能被连坐。
func (h *Hub) closeRevoked(w http.ResponseWriter, r *http.Request, ip string) {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.log.Info("revoked token, closing", zap.String("ip", ip))
	_ = ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(CloseTokenRevoked, "token revoked"), time.Now().Add(5*time.Second))
	_ = ws.Close()
}

// hasDevicesFrom 该 IP 当前是否有已认证的设备在线（同一出口 IP 下的合法设备）。
func (h *Hub) hasDevicesFrom(ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.conns {
		if c.ip == ip {
			return true
		}
	}
	return false
}

func (h *Hub) deny(ip, reason string) {
	h.denies.add(1)
	// 已有设备在线的 IP 不封禁（多台设备共用一个出口 IP 时，一台出错不能让全部断线），只记日志。
	if h.hasDevicesFrom(ip) {
		h.log.Warn("denied (not banned: devices online from this ip)", zap.String("ip", ip), zap.String("reason", reason))
		return
	}
	if h.bans.fail(ip) {
		h.log.Warn("ip banned", zap.String("ip", ip), zap.String("reason", reason))
	} else {
		h.log.Info("denied", zap.String("ip", ip), zap.String("reason", reason))
	}
}

func (h *Hub) acquire(ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total >= h.cfg.MaxConnections || h.perIP[ip] >= h.cfg.MaxPerIP {
		return false
	}
	h.total++
	h.perIP[ip]++
	return true
}

func (h *Hub) release(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.total--
	if h.perIP[ip]--; h.perIP[ip] <= 0 {
		delete(h.perIP, ip)
	}
}

// register 设备 hello 成功后登记；同一设备的旧连接被替换并关闭。
func (h *Hub) register(c *conn) {
	h.mu.Lock()
	old := h.conns[c.device.ID]
	h.conns[c.device.ID] = c
	h.mu.Unlock()
	if old != nil {
		old.close("replaced by a new connection")
	}
}

// unregister 返回 true 表示 c 仍是该设备的当前连接（即设备真正离线）。
func (h *Hub) unregister(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.device == nil || h.conns[c.device.ID] != c {
		return false
	}
	delete(h.conns, c.device.ID)
	return true
}

// SendInvoke 实现 service.Sender。
func (h *Hub) SendInvoke(deviceID uint64, uid, module, command string, args json.RawMessage) bool {
	h.mu.Lock()
	c := h.conns[deviceID]
	h.mu.Unlock()
	if c == nil {
		return false
	}
	p, _ := json.Marshal(invokePayload{Module: module, Command: command, Arguments: args})
	return c.sendWait(Message{ID: uid, Type: TypeInvoke, Payload: p, TS: nowTS()}, 5*time.Second)
}

// Stats 当前连接数与封禁 IP 数。
func (h *Hub) Stats() (connections, banned int) {
	h.mu.Lock()
	connections = len(h.conns)
	h.mu.Unlock()
	return connections, h.bans.count()
}

// Telemetry 连接数、封禁数与 24 小时走势（实现 service.GatewayTelemetry）。
func (h *Hub) Telemetry() (connections, banned int, denyHourly []int64, rttHourlyMs []float64) {
	connections, banned = h.Stats()
	denyHourly, _ = h.denies.series()
	_, rttHourlyMs = h.rtts.series()
	return
}

// Housekeep 定期清理封禁表。
func (h *Hub) Housekeep() { h.bans.gc() }

// Shutdown 关闭所有连接（进程退出时）。
func (h *Hub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	list := make([]*conn, 0, len(h.conns))
	for _, c := range h.conns {
		list = append(list, c)
	}
	h.mu.Unlock()
	for _, c := range list {
		c.close("server shutting down")
	}
}

// onHello 由连接调用：登记设备、补发离线指令。
func (h *Hub) onHello(ctx context.Context, c *conn, p helloPayload) (*model.Device, error) {
	d, err := h.svc.Devices.OnHello(ctx, c.token, service.HelloInfo{
		DeviceUID: p.Device, Name: p.Name, Version: p.Version, Network: p.Network,
		IP: c.ip, RemoteCommands: p.RemoteCommands, ConfigVersion: p.ConfigVersion, ConfigSchema: p.ConfigSchema,
		CommandSchema: p.CommandSchema, Info: p.Info,
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// closeHijacked 被封禁的 IP 直接断开，不回任何内容。
func closeHijacked(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if c, _, err := hj.Hijack(); err == nil {
			_ = c.Close()
			return
		}
	}
	w.WriteHeader(http.StatusForbidden)
}

func nowTS() float64 { return float64(time.Now().UnixNano()) / 1e9 }
