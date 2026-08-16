// Package handler HTTP 接口层，负责参数绑定、调用 service 并输出统一响应。
package handler

import (
	"ai-code-wiki/internal/service"
)

// Handler 聚合所有 HTTP 处理器，注入业务服务。
type Handler struct {
	Service     *service.Service
	Task        *TaskHandler
	Doc         *DocHandler
	Relation    *RelationHandler
	Requirement *RequirementHandler
	Webhook     *WebhookHandler
	Report      *ReportHandler
	Repo        *RepoHandler
}

// NewHandler 构建处理器聚合对象。
func NewHandler(svc *service.Service) *Handler {
	h := &Handler{Service: svc}
	h.Task = NewTaskHandler(svc)
	h.Doc = NewDocHandler(svc)
	h.Relation = NewRelationHandler(svc)
	h.Requirement = NewRequirementHandler(svc)
	h.Webhook = NewWebhookHandler(svc)
	h.Report = NewReportHandler(svc)
	h.Repo = NewRepoHandler(svc)
	return h
}
