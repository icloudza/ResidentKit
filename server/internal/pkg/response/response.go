// Package response 输出统一响应 {code, msg, data}。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
)

type Body struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Msg: "ok", Data: data})
}

// Error 输出错误；内部原因只记日志。
func Error(c *gin.Context, err error) {
	e := errcode.From(err)
	if e.Status >= 500 {
		if log, ok := c.Get("logger"); ok {
			log.(*zap.Logger).Error("request failed", zap.Error(err))
		}
	}
	c.AbortWithStatusJSON(e.Status, Body{Code: e.Code, Msg: e.Message})
}

// ErrorData 输出错误并附带数据（如开放 API 指令超时时返回指令 ID，便于之后查询）。
func ErrorData(c *gin.Context, err error, data any) {
	e := errcode.From(err)
	c.AbortWithStatusJSON(e.Status, Body{Code: e.Code, Msg: e.Message, Data: data})
}

// Status 以指定的 HTTP 状态码输出成功响应（如 202 Accepted）。
func Status(c *gin.Context, status int, data any) {
	c.JSON(status, Body{Code: 0, Msg: "ok", Data: data})
}
