// Package router 注册所有 HTTP 路由。
package router

import (
	"net/http"

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

	// ========== Webhook 回调 ==========
	// GitLab/Gitee 分支 push 回调，跳过 X-Api-Secret 鉴权，使用 WEBHOOK_SECRET 自有签名鉴权
	r.POST("/api/v1/webhook/git_push", h.Webhook.GitPush)

	// 兜底：404 / 405 统一 JSON 返回
	r.NoRoute(middleware.NotFoundHandler)
	r.NoMethod(middleware.NoMethodHandler)

	api := r.Group("/api/v1")
	// 简易 API 密钥鉴权：环境变量 API_SECRET_KEY 为空时自动关闭
	api.Use(middleware.APIAuth())
	{
		// ========== 代码仓库注册（多仓库支持） ==========
		// 注册代码仓库（幂等）
		api.POST("/repo/register", h.Repo.Register)
		// 获取所有启用仓库
		api.GET("/repo/list", h.Repo.List)
		// 启用/停用仓库
		api.PUT("/repo/:repo_id/status", h.Repo.SetStatus)

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
		// 分页查询函数文档列表（前端列表页使用，支持模块筛选）
		api.GET("/doc/list", h.Doc.List)
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
		// 查看文档全部修改记录（doc_modify_log 历史版本列表）
		api.GET("/doc/:doc_id/history", h.Doc.ListDocHistory)
		// 获取某一条历史快照详情（含修改前后原始 JSON）
		api.GET("/doc/:doc_id/history/:log_id", h.Doc.GetDocHistoryDetail)

		// ========== 模块依赖知识图谱 ==========
		// 查询模块上下游依赖
		api.GET("/relation/list", h.Relation.ListRelations)
		// 手动新增模块依赖关系
		api.POST("/relation/add", h.Relation.AddRelation)
		// 删除模块依赖
		api.DELETE("/relation", h.Relation.DeleteRelation)

		// ========== 迭代影响分析 ==========
		// 迭代影响分析（分支 diff 或显式函数 → 上游/下游影响点）
		api.POST("/impact/analyze", h.Impact.Analyze)

		// ========== 需求分析 ==========
		// 新产品需求分析
		api.POST("/requirement/analyze", h.Requirement.Analyze)

		// ========== 知识库统计 ==========
		// 基础统计（文档/校正/待复核/模块数量）
		api.GET("/report/basic", h.Report.Basic)
	}

	// ========== 极简前端静态页面（原生 HTML + Vue3 CDN，无构建） ==========
	// 挂载 ./webstatic 目录，不经过 /api/v1 鉴权分组；
	// 加 NoCache 中间件避免浏览器强缓存导致页面/脚本不一致（前端 bind mount 实时开发）。
	web := r.Group("/webstatic", middleware.NoCache())
	web.Static("", "./webstatic")
	// 首页默认跳转文档列表页
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/webstatic/docs.html")
	})
}
