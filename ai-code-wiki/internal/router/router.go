// Package router 注册所有 HTTP 路由。
package router

import (
	"ai-code-wiki/internal/handler"
	"ai-code-wiki/internal/middleware"

	"github.com/gin-gonic/gin"
)

// Register 注册全部路由，统一前缀 /api/v1。
func Register(r *gin.Engine, h *handler.Handler) {
	// 全局中间件：请求追踪（request_id）+ 异常恢复
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())

	// 健康检查（跳过鉴权，不挂载在 /api/v1 分组下；探测 mysql/llm 连通性）
	r.GET("/health", h.Health)

	// 兜底：404 / 405 统一 JSON 返回
	r.NoRoute(middleware.NotFoundHandler)
	r.NoMethod(middleware.NoMethodHandler)

	api := r.Group("/api/v1")
	// 简易 API 密钥鉴权：环境变量 API_SECRET_KEY 为空时自动关闭
	api.Use(middleware.APIAuth())
	{
		// ========== 代码解析任务 ==========
		// 触发代码解析任务（CI 回调）
		api.POST("/task/trigger", h.Task.Trigger)
		// 查询任务状态
		api.GET("/task/status", h.Task.Status)
		// 任务列表（分页，按时间倒序）
		api.GET("/task/list", h.Task.List)

		// ========== 业务文档 ==========
		// 自然语言查询业务（跨模块 RAG 检索）
		api.POST("/doc/search", h.Doc.Search)
		// 获取所有业务模块
		api.GET("/doc/module/list", h.Doc.ListModules)
		// 获取文档详情
		api.GET("/doc/:doc_id", h.Doc.GetDoc)
		// 人工校正业务文档
		api.PUT("/doc/:doc_id/edit", h.Doc.EditDoc)
		// 文档重置为原始 AI 版本
		api.POST("/doc/:doc_id/reset", h.Doc.ResetDoc)
		// 查询所有人工校正文档
		api.GET("/doc/modified/list", h.Doc.ListModifiedDocs)
		// 查看文档迭代变更记录
		api.GET("/doc/changelog", h.Doc.ListChangeLogs)

		// ========== 模块依赖知识图谱 ==========
		// 查询模块上下游依赖
		api.GET("/relation/list", h.Relation.ListRelations)
		// 手动新增模块依赖关系
		api.POST("/relation/add", h.Relation.AddRelation)
		// 删除模块依赖
		api.DELETE("/relation", h.Relation.DeleteRelation)

		// ========== 需求分析 ==========
		// 新产品需求分析
		api.POST("/requirement/analyze", h.Requirement.Analyze)
	}
}