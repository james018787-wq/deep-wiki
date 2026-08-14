package common

import "github.com/gin-gonic/gin"

// Response 统一接口返回结构体。
// 约定字段：code=0 成功；msg 为提示信息；data 为业务数据。
// 统一返回格式：{"code":0,"msg":"ok","data":xxx}
// data 字段恒存在（无业务数据时为 null），保证返回结构体统一，便于前端统一处理。
type Response struct {
	Code int  `json:"code"`
	Msg  string `json:"msg"`
	Data any  `json:"data"`
}

// Success 返回成功响应。
func Success(c *gin.Context, data any) {
	c.JSON(200, Response{Code: CodeSuccess, Msg: "ok", Data: data})
}

// Fail 返回失败响应（自定义错误码）。
func Fail(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, Response{Code: code, Msg: msg})
}

// FailWithAppError 返回业务错误响应，HTTP状态码由错误码自动映射。
func FailWithAppError(c *gin.Context, err *AppError) {
	Fail(c, err.HTTPStatus(), err.Code, err.Message)
}

// PageResult 通用分页数据结构。
type PageResult struct {
	List     any   `json:"list"`      // 当前页数据
	Total    int64 `json:"total"`     // 总记录数
	Page     int   `json:"page"`      // 当前页码
	PageSize int   `json:"page_size"` // 每页大小
}