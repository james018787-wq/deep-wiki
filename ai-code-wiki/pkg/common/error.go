package common

import (
	"net/http"
)

// 统一业务错误码定义。
// 约定：0 表示成功，非 0 表示业务失败。
const (
	CodeSuccess        = 0      // 成功
	CodeBadRequest     = 10001  // 请求参数错误
	CodeUnauthorized   = 10002  // 未授权
	CodeForbidden      = 10003  // 无权限
	CodeNotFound       = 10004  // 资源不存在
	CodeConflict       = 10005  // 数据冲突（如唯一键冲突）
	CodeInternalError  = 10006  // 系统内部错误
	CodeUpstreamError  = 10007  // 上游依赖调用失败（git/AST/LLM/向量库）
	CodeInvalidState   = 10008  // 业务状态不允许（如重复解析等）
)

// AppError 业务错误结构体，贯穿 handler/service/repo 三层。
type AppError struct {
	Code    int    // 业务错误码
	Message string // 错误描述
	Cause   error  // 底层根因（可选，用于日志）
}

// Error 实现 error 接口。
func (e *AppError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap 返回底层根因，便于 errors.Is / errors.As 使用。
func (e *AppError) Unwrap() error {
	return e.Cause
}

// NewError 构建业务错误。
func NewError(code int, msg string) *AppError {
	return &AppError{Code: code, Message: msg}
}

// WrapError 包装底层错误并转换为业务错误。
func WrapError(code int, msg string, err error) *AppError {
	return &AppError{Code: code, Message: msg, Cause: err}
}

// HTTPStatus 将业务错误码映射为 HTTP 状态码。
func (e *AppError) HTTPStatus() int {
	switch e.Code {
	case CodeBadRequest:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeSuccess:
		return http.StatusOK
	default:
		return http.StatusInternalServerError
	}
}