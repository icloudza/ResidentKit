package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

const (
	helloTimeout  = 15 * time.Second // 升级后必须在此时间内发 hello
	writeTimeout  = 10 * time.Second
	pingInterval  = 30 * time.Second // 服务端主动 ping，用于测延迟
	sendQueueSize = 64
	dbTimeout     = 5 * time.Second
)

// conn 一个设备连接：一个读协程、一个写协程。
type conn struct {
	hub    *Hub
	ws     *websocket.Conn
	ip     string
	token  *model.DeviceToken
	device *model.Device
	log    *zap.Logger

	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once

	pingMu     sync.Mutex
	pingID     string
	pingSentAt time.Time
}

func newConn(h *Hub, ws *websocket.Conn, ip string, tok *model.DeviceToken) *conn {
	ws.SetReadLimit(h.cfg.MaxMessageBytes)
	return &conn{hub: h, ws: ws, ip: ip, token: tok, log: h.log.With(zap.String("ip", ip)),
		out: make(chan []byte, sendQueueSize), done: make(chan struct{})}
}

func (c *conn) run() {
	defer c.cleanup()
	go c.writeLoop()

	// 第一条必须是 hello。
	_ = c.ws.SetReadDeadline(time.Now().Add(helloTimeout))
	var first Message
	if err := c.ws.ReadJSON(&first); err != nil || first.Type != TypeHello {
		if errors.Is(err, websocket.ErrReadLimit) {
			// 模块与命令说明太多，hello 超过单条消息上限：设备会一直连不上，需调大 gateway.max_message_bytes。
			c.log.Error("hello exceeds gateway.max_message_bytes", zap.Int64("limit", c.hub.cfg.MaxMessageBytes))
			return
		}
		c.log.Info("no hello", zap.Error(err))
		return
	}
	var hp helloPayload
	_ = json.Unmarshal(first.Payload, &hp)
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	d, err := c.hub.onHello(ctx, c, hp)
	cancel()
	if err != nil {
		c.log.Warn("hello rejected", zap.Error(err))
		if err == service.ErrTokenBoundElse {
			c.hub.deny(c.ip, "token bound to another device")
		}
		return
	}
	c.device = d
	c.log = c.log.With(zap.Uint64("device", d.ID))
	c.hub.register(c)
	c.log.Info("device online")
	c.send(Message{ID: first.ID, Type: TypeAck})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c.hub.svc.Invocations.DeliverPending(ctx, d.ID)
	}()
	go c.pingLoop()
	if c.hub.svc.OTA != nil {
		c.hub.svc.OTA.OnDeviceOnline(d)
	}
	if c.hub.svc.Config != nil {
		c.hub.svc.Config.OnDeviceOnline(d)
	}

	for {
		_ = c.ws.SetReadDeadline(time.Now().Add(c.hub.cfg.IdleTimeout))
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var m Message
		if json.Unmarshal(data, &m) != nil || m.Type == "" {
			continue
		}
		c.handle(m)
	}
}

func (c *conn) handle(m Message) {
	switch m.Type {
	case TypePing:
		c.send(Message{ID: m.ID, Type: TypePong})
		c.onMetrics(m.Payload)
	case TypePong:
		c.onPong(m.ID)
	case TypeAck, TypeHello:
		// 设备不会对服务端消息回 ack；重复 hello 忽略。
	case TypeResult:
		r := service.InvokeResult{UID: m.ID, Payload: m.Payload}
		if m.Error != nil {
			r.HasError, r.ErrorCode, r.ErrorMessage = true, m.Error.Code, m.Error.Message
		}
		ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
		c.hub.svc.Invocations.OnResult(ctx, c.device.ID, r)
		cancel()
	default:
		if m.ID == "" || len(m.ID) > 64 {
			return
		}
		// 先落库成功再 ack；落库失败不回 ack，设备会重发。
		ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
		err := c.hub.svc.Events.Ingest(ctx, c.device.ID, m.ID, m.Type, m.Level, m.Payload, m.TS)
		cancel()
		if err != nil {
			c.log.Error("ingest event", zap.Error(err))
			return
		}
		c.send(Message{ID: m.ID, Type: TypeAck})
	}
}

func (c *conn) pingLoop() {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			id := newID()
			c.pingMu.Lock()
			c.pingID, c.pingSentAt = id, time.Now()
			c.pingMu.Unlock()
			c.send(Message{ID: id, Type: TypePing, TS: nowTS()})
		}
	}
}

func (c *conn) onPong(id string) {
	c.pingMu.Lock()
	ok := id != "" && id == c.pingID
	rtt := time.Since(c.pingSentAt)
	c.pingID = ""
	c.pingMu.Unlock()
	if ok {
		c.hub.rtts.add(float64(rtt.Milliseconds()))
		ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
		c.hub.svc.Devices.Touch(ctx, c.device.ID, rtt)
		cancel()
	}
}

// onMetrics 设备心跳附带的资源指标：{"metrics": {...}}。
func (c *conn) onMetrics(payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}
	var p struct {
		Metrics json.RawMessage `json:"metrics"`
	}
	if json.Unmarshal(payload, &p) != nil || len(p.Metrics) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	c.hub.svc.Devices.UpdateMetrics(ctx, c.device.ID, p.Metrics)
	cancel()
}

// send 非阻塞入队；队列满说明对端过慢，直接断开。
func (c *conn) send(m Message) bool {
	b, err := json.Marshal(m)
	if err != nil {
		return false
	}
	select {
	case <-c.done:
		return false
	case c.out <- b:
		return true
	default:
		c.close("send queue full")
		return false
	}
}

// sendWait 阻塞入队，最多等 wait；用于指令下发：队列暂时满时等写协程腾出位置，而不是断开设备。
func (c *conn) sendWait(m Message, wait time.Duration) bool {
	b, err := json.Marshal(m)
	if err != nil {
		return false
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-c.done:
		return false
	case c.out <- b:
		return true
	case <-t.C:
		return false
	}
}

func (c *conn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				c.close("write failed")
				return
			}
		}
	}
}

func (c *conn) close(reason string) {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), time.Now().Add(time.Second))
		_ = c.ws.Close()
	})
}

func (c *conn) cleanup() {
	c.close("closed")
	c.hub.release(c.ip)
	if c.device == nil || !c.hub.unregister(c) {
		return
	}
	c.log.Info("device offline")
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	c.hub.svc.Devices.OnDisconnect(ctx, c.device.ID)
	c.hub.svc.Invocations.OnDisconnect(ctx, c.device.ID)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
