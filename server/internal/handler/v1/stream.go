package v1

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/icloudza/ResidentKit/server/internal/service"
)

type StreamHandler struct {
	bus *service.Bus
}

func NewStreamHandler(bus *service.Bus) *StreamHandler { return &StreamHandler{bus: bus} }

// Stream 实时推送
//
//	@Summary	SSE 实时推送：设备上下线、新事件、指令状态
//	@Tags		总览
//	@Security	Bearer
//	@Produce	text/event-stream
//	@Router		/stream [get]
func (h *StreamHandler) Stream(c *gin.Context) {
	ch, cancel := h.bus.Subscribe()
	defer cancel()
	c.Header("Content-Type", "text/event-stream")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Cache-Control", "no-cache")
	// 立即发出响应头和一条注释：否则要等到第一条事件或保活才返回，控制台会一直显示「连接中」。
	c.Status(http.StatusOK)
	_, _ = io.WriteString(c.Writer, ": connected\n\n")
	c.Writer.Flush()
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	c.Stream(func(w io.Writer) bool {
		select {
		case <-c.Request.Context().Done():
			return false
		case <-keepalive.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			return true
		case m, ok := <-ch:
			if !ok {
				return false
			}
			b, _ := json.Marshal(m.Data)
			_, _ = io.WriteString(w, "event: "+m.Topic+"\ndata: "+string(b)+"\n\n")
			return true
		}
	})
}
