package gateway

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

const OTAPrefix = "/ota/"

// serveOTA 设备下载安装包，设备令牌鉴权：
//
//	GET /ota/<release_id>        要安装的新版本；该设备须有这个版本进行中的更新
//	GET /ota/version/<x.y.z>     设备当前版本的安装包，供首次 OTA 前缓存为回滚包；
//	                             签名清单放在 X-RK-Size / X-RK-SHA256 / X-RK-Signature 头里，设备按发布签名校验
//
// 鉴权失败与 WebSocket 一样计入封禁。
func (h *Hub) serveOTA(w http.ResponseWriter, r *http.Request, ip string) {
	if h.svc.OTA == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, OTAPrefix)
	version, byVersion := strings.CutPrefix(rest, "version/")
	var id uint64
	if !byVersion {
		var err error
		if id, err = strconv.ParseUint(rest, 10, 64); err != nil || id == 0 {
			h.deny(ip, "bad ota path")
			http.NotFound(w, r)
			return
		}
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		h.deny(ip, "ota: no token")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tok, err := h.svc.Devices.Authenticate(ctx, token)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrBadDeviceToken), errors.Is(err, service.ErrTokenUnknown):
			h.deny(ip, "ota: bad token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case errors.Is(err, service.ErrTokenRevoked):
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	var f *os.File
	var rel *model.Release
	if byVersion {
		f, rel, err = h.svc.OTA.OpenVersion(ctx, tok, version)
	} else {
		f, rel, err = h.svc.OTA.OpenDownload(ctx, tok, id)
	}
	if err != nil {
		if errors.Is(err, errcode.Forbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
		} else if errors.Is(err, errcode.NotFound) {
			http.NotFound(w, r)
		} else {
			h.log.Error("ota open", zap.Error(err))
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	defer f.Close()
	h.log.Info("ota download", zap.String("ip", ip), zap.Uint64("device", *tok.DeviceID),
		zap.String("version", rel.Version), zap.Bool("rollback_copy", byVersion))
	hd := w.Header()
	hd.Set("Content-Type", "application/vnd.debian.binary-package")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-RK-Size", strconv.FormatInt(rel.Size, 10))
	hd.Set("X-RK-SHA256", rel.SHA256)
	hd.Set("X-RK-Signature", rel.Signature)
	http.ServeContent(w, r, rel.FileName, rel.CreatedAt, f)
}
