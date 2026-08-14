package handler

import (
	"io"
	"net/http"
	"os"

	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/webhook"

	"github.com/gin-gonic/gin"
)

// maxWebhookBody webhook 请求体大小上限（2MB），防止超大 payload 打满内存。
const maxWebhookBody = 2 << 20

// WebhookHandler 代码托管平台（GitLab / Gitee）webhook 接收处理器。
type WebhookHandler struct {
	svc *service.Service
}

// NewWebhookHandler 构建 webhook 处理器。
func NewWebhookHandler(svc *service.Service) *WebhookHandler {
	return &WebhookHandler{svc: svc}
}

// GitPush 接收 GitLab / Gitee 分支 push 回调，自动触发解析任务。
//
//	POST /api/v1/webhook/git_push
//
// 鉴权：使用 webhook 自有签名（WEBHOOK_SECRET），不经过 /api/v1 的 X-Api-Secret 鉴权；
// 签名校验失败返回 403。处理流程：原始日志 -> 签名校验 -> payload 解析 ->
// tag/分支删除过滤 -> 投递任务队列执行增量解析流水线。
func (h *WebhookHandler) GitPush(c *gin.Context) {
	ctx := c.Request.Context()

	// 1. 读取原始请求体（限制大小）
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, common.CodeBadRequest, "读取 webhook 请求体失败")
		return
	}

	// 2. 记录 webhook 原始日志（便于排查推送回调）
	logger.Info(ctx, "[webhook] 收到推送回调 raw_body=%s", string(body))

	// 3. 签名校验（WEBHOOK_SECRET 未配置时跳过，仅限开发环境）
	if !verifyWebhookSignature(c) {
		common.Fail(c, http.StatusForbidden, common.CodeForbidden, "webhook 签名校验失败")
		return
	}

	// 4. 解析 payload，提取仓库地址、分支、前后 commit id
	event, err := webhook.ParsePush(body)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, common.CodeBadRequest, "webhook payload 解析失败: "+err.Error())
		return
	}
	logger.Info(ctx, "[webhook] 解析成功 provider=%s repo=%s branch=%s before=%s after=%s tag=%v delete=%v",
		event.Provider, event.RepoURL, event.Branch, event.BeforeCommit, event.AfterCommit, event.IsTag, event.IsDelete)

	// 5. 过滤 tag 推送与分支删除，只处理分支 push 事件
	if event.IsTag || event.IsDelete {
		logger.Info(ctx, "[webhook] 忽略非分支 push 事件（tag=%v delete=%v）", event.IsTag, event.IsDelete)
		common.Success(c, gin.H{"skipped": true, "reason": "仅处理分支 push 事件"})
		return
	}

	// 6. 投递任务队列，执行增量解析流水线
	record, err := h.svc.Task.HandleGitPush(ctx, event)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, record)
}

// verifyWebhookSignature 校验 webhook 签名（WEBHOOK_SECRET 环境变量）。
// 支持：GitLab X-Gitlab-Token、Gitee X-Gitee-Token、Gitee X-Gitee-Signature（HMAC-SHA256）。
func verifyWebhookSignature(c *gin.Context) bool {
	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		return true // 未配置密钥：跳过鉴权（开发环境）
	}
	switch {
	case c.GetHeader("X-Gitlab-Token") != "":
		return webhook.VerifyToken(c.GetHeader("X-Gitlab-Token"), secret)
	case c.GetHeader("X-Gitee-Token") != "":
		return webhook.VerifyToken(c.GetHeader("X-Gitee-Token"), secret)
	case c.GetHeader("X-Gitee-Signature") != "":
		return webhook.VerifyGiteeSignature(secret, c.GetHeader("X-Gitee-Timestamp"), c.GetHeader("X-Gitee-Signature"))
	default:
		return false
	}
}