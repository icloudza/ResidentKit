// Package errcode 定义各层共用的业务错误。
//
// service 返回 *Error 表示可告知客户端的错误；其他错误一律视为内部错误，
// 原文只记日志，不返回给客户端。
package errcode

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Code    int    // 对外稳定的错误码
	Status  int    // HTTP 状态码
	Message string // 可展示给客户端
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%d %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%d %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// Is 按错误码比较，WithMessage / Wrap 得到的副本仍与原错误相等。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// WithMessage 返回替换了提示文案的副本。
func (e *Error) WithMessage(format string, a ...any) *Error {
	c := *e
	c.Message = fmt.Sprintf(format, a...)
	return &c
}

// Wrap 返回附带内部原因的副本（只记日志）。
func (e *Error) Wrap(cause error) *Error {
	c := *e
	c.cause = cause
	return &c
}

func newErr(code, status int, msg string) *Error {
	return &Error{Code: code, Status: status, Message: msg}
}

var (
	OK = newErr(0, http.StatusOK, "ok")

	Internal        = newErr(10000, http.StatusInternalServerError, "服务器内部错误")
	InvalidParams   = newErr(10001, http.StatusBadRequest, "参数错误")
	NotFound        = newErr(10002, http.StatusNotFound, "资源不存在")
	TooManyRequests = newErr(10003, http.StatusTooManyRequests, "请求过于频繁")
	Conflict        = newErr(10004, http.StatusConflict, "资源冲突")

	Unauthorized   = newErr(20001, http.StatusUnauthorized, "未登录或登录已过期")
	BadCredentials = newErr(20002, http.StatusUnauthorized, "用户名或密码错误")
	AccountLocked  = newErr(20003, http.StatusForbidden, "账号已临时锁定")
	TOTPRequired   = newErr(20004, http.StatusUnauthorized, "需要两步验证码")
	TOTPInvalid    = newErr(20005, http.StatusUnauthorized, "两步验证码错误")
	Forbidden      = newErr(20006, http.StatusForbidden, "无权执行此操作")

	DeviceOffline       = newErr(30001, http.StatusConflict, "设备不在线")
	CommandNotPermitted = newErr(30002, http.StatusForbidden, "设备未开放此远程命令")
	InvocationTimeout   = newErr(30003, http.StatusGatewayTimeout, "等待设备响应超时")
	BadEnrollment       = newErr(30004, http.StatusBadRequest, "接入请求无效")
	CommandFailed       = newErr(30005, http.StatusUnprocessableEntity, "设备执行失败")
	ResultUnknown       = newErr(30006, http.StatusBadGateway, "结果未知")
)

// From 提取 *Error，否则包装为 Internal。
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal.Wrap(err)
}
