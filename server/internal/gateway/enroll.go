package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

const EnrollPath = "/enroll"

// 设备等待批准时每 5 秒问一次，每分钟 30 次足够。
var enrollLimiter = limiter.New(memory.NewStore(), limiter.Rate{Period: time.Minute, Limit: 30})

// serveEnroll 设备接入申请：POST /enroll，无需令牌，靠设备签名与管理员批准。
// 签名或格式错误计入封禁，与 WebSocket 鉴权失败一样。
func (h *Hub) serveEnroll(w http.ResponseWriter, r *http.Request, ip string) {
	if h.svc.Enroll == nil {
		http.NotFound(w, r)
		return
	}
	if lc, err := enrollLimiter.Get(r.Context(), ip); err == nil && lc.Reached {
		writeEnroll(w, nil, errcode.TooManyRequests)
		return
	}
	var req dto.EnrollRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		h.deny(ip, "enroll: bad body")
		writeEnroll(w, nil, service.ErrBadEnroll)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.svc.Enroll.Request(ctx, ip, req)
	if errors.Is(err, service.ErrBadEnroll) {
		h.deny(ip, "enroll: bad signature")
	} else if err != nil && errcode.From(err).Code == errcode.Internal.Code {
		h.log.Error("enroll", zap.Error(err))
	}
	writeEnroll(w, resp, err)
}

// writeEnroll 与控制台 API 相同的响应格式 {code, msg, data}。
func writeEnroll(w http.ResponseWriter, data *dto.EnrollResponse, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	body := map[string]any{"code": 0, "msg": "ok", "data": data}
	if err != nil {
		e := errcode.From(err)
		w.WriteHeader(e.Status)
		body = map[string]any{"code": e.Code, "msg": e.Message, "data": nil}
	}
	_ = json.NewEncoder(w).Encode(body)
}
