// Package apperr 定义全站统一的业务错误类型与错误码。
//
// 契约见 docs/design/api.md:
//   - 成功响应: { "code": 0, "message": "ok", "data": ... }
//   - 失败响应: { "code": <业务码>, "message": <文案>, "data": null }
//
// 业务码段约定:
//   - 0      业务成功
//   - 400xx  参数 / 业务规则拒绝 -> HTTP 400
//   - 401xx  未认证、token 过期 / 无效 -> HTTP 401
//   - 403xx  已认证但权限不足 -> HTTP 403
//   - 404xx  资源不存在 -> HTTP 404
//   - 50000  系统错误(对外不暴露细节)-> HTTP 500
package apperr

import (
	"errors"
	"net/http"
)

// 通用业务码。
const (
	CodeOK           = 0     // 业务成功
	CodeBiz          = 40000 // 业务规则拒绝
	CodeParam        = 40001 // 参数绑定 / 校验失败
	CodeUnauthorized = 40100 // 未认证
	CodeForbidden    = 40300 // 权限不足
	CodeNotFound     = 40400 // 资源不存在
	CodeSystem       = 50000 // 系统错误
)

// Error 是携带业务码的统一错误类型。
// controller / 中间件通过它序列化出标准失败响应。
type Error struct {
	Code    int
	Message string
	// Err 保留原始错误(可选),只用于日志,不对外暴露。
	Err error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

// New 构造一个带业务码的错误。
func New(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap 构造一个带业务码且保留底层错误的错误。
func Wrap(code int, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

// BadRequest 构造参数 / 业务规则错误(400xx)。
func BadRequest(message string) *Error {
	return New(CodeParam, message)
}

// Biz 构造通用业务规则拒绝错误(40000)。
func Biz(message string) *Error {
	return New(CodeBiz, message)
}

// Unauthorized 构造未认证错误(401xx)。
func Unauthorized(message string) *Error {
	return New(CodeUnauthorized, message)
}

// Forbidden 构造权限不足错误(403xx)。
func Forbidden(message string) *Error {
	return New(CodeForbidden, message)
}

// NotFound 构造资源不存在错误(404xx)。
func NotFound(message string) *Error {
	return New(CodeNotFound, message)
}

// SystemError 构造系统错误(50000),对外文案统一,不暴露细节。
func SystemError(err error) *Error {
	if err == nil {
		return New(CodeSystem, "系统错误，请联系工作人员")
	}
	return Wrap(CodeSystem, "系统错误，请联系工作人员", err)
}

// From 把任意 error 归一化为 *Error:
//   - 已经是 *Error 则原样返回;
//   - 其他 error 视为系统错误(50000),原始错误只进日志。
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return SystemError(err)
}

// HTTPStatus 根据业务码推导 HTTP 状态码。
func HTTPStatus(code int) int {
	switch {
	case code == CodeOK:
		return http.StatusOK
	case code >= 40000 && code < 40100:
		return http.StatusBadRequest
	case code >= 40100 && code < 40200:
		return http.StatusUnauthorized
	case code >= 40300 && code < 40400:
		return http.StatusForbidden
	case code >= 40400 && code < 40500:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
