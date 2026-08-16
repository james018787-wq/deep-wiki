// Package router 注册所有 HTTP 路由。
package router

import (
	"net/http"
	"strings"

	"ai-code-wiki/internal/handler"
	"ai-code-wiki/internal/middleware"
	"ai-code-wiki/internal/service"

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

	// ========== 登录（公开，不经过鉴权中间件） ==========
	r.POST("/api/v1/auth/login", h.Auth.Login)

	// 兜底：404 / 405 统一 JSON 返回
	// 前端 SPA history 模式 fallback：非 /api、非 /assets 的 GET 请求回退到 index.html，
	// 保证刷新/直达深层路由不 404。
	r.NoRoute(middleware.NoCache(), func(c *gin.Context) {
		if c.Request.Method == http.MethodGet &&
			!strings.HasPrefix(c.Request.URL.Path, "/api/") &&
			!strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.File("./web/dist/index.html")
			return
		}
		middleware.NotFoundHandler(c)
	})
	r.NoMethod(middleware.NoMethodHandler)

	// ========== 前端 SPA（Vue3 + Vite 构建产物） ==========
	// /assets 下为构建后的静态资源；其余非 /api 的 GET 请求统一回退到 index.html
	// （vue-router history 模式，刷新/直达深层路由不 404）。
	spa := r.Group("", middleware.NoCache())
	spa.Static("/assets", "./web/dist/assets")
	// 首页加载 SPA 入口（路由守卫决定跳登录页或文档列表）
	r.GET("/", middleware.NoCache(), func(c *gin.Context) {
		c.File("./web/dist/index.html")
	})

	api := r.Group("/api/v1")
	// 统一鉴权：Bearer Token（用户登录）或 X-Api-Secret（server-to-server）
	var auth *service.AuthService
	if h.Service != nil {
		auth = h.Service.Auth
	}
	api.Use(middleware.AuthGuard(auth))
	{
		// 当前登录用户 / 登出
		api.GET("/auth/me", h.Auth.Me)
		api.POST("/auth/logout", h.Auth.Logout)
		// ========== 代码仓库注册（多仓库支持） ==========
		// 注册代码仓库（幂等）
		api.POST("/repo/register", h.Repo.Register)
		// 获取所有启用仓库
		api.GET("/repo/list", h.Repo.List)
		// 启用/停用仓库
		api.PUT("/repo/:repo_id/status", h.Repo.SetStatus)
		// 编辑仓库基本信息
		api.PUT("/repo/:repo_id", h.Repo.Update)
		api.PUT("/repo/:repo_id/token", h.Repo.SetToken)
		api.DELETE("/repo/:repo_id/token", h.Repo.ClearToken)

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
		// 读取文档对应源码文件内容（查看源码）
		api.GET("/doc/:doc_id/source", h.Doc.GetDocSource)
		// 查询文档对应函数的调用图（D3 可视化）
		api.GET("/doc/:doc_id/graph", h.Doc.GetDocGraph)
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

		// ========== 多轮对话（Redis 会话记忆） ==========
		// 多轮问答：先问模块逻辑，可连续追问（带会话记忆）
		api.POST("/chat/ask", h.Chat.Ask)
		// 会话列表
		api.GET("/chat/sessions", h.Chat.ListSessions)
		// 会话历史消息
		api.GET("/chat/history", h.Chat.History)
		// 删除会话（元信息 + 消息）
		api.DELETE("/chat/session", h.Chat.DeleteSession)

		// ========== 需求分析 ==========
		// 新产品需求分析
		api.POST("/requirement/analyze", h.Requirement.Analyze)

		// ========== 知识库统计 ==========
		// 基础统计（文档/校正/待复核/模块数量）
		api.GET("/report/basic", h.Report.Basic)

		// ========== 代码安全扫描 ==========
		// 触发仓库全量安全扫描
		api.POST("/security/scan", h.Security.Scan)
		// 分页查询安全发现
		api.GET("/security/list", h.Security.List)
		// 更新安全发现状态（open/fixed/false_positive）
		api.PUT("/security/:id/status", h.Security.UpdateStatus)
	}
}
