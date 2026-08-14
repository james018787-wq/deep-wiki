// Package service 业务逻辑层，编排 repo 与 pkg 工具，实现核心业务规则。
package service

import (
	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/repo"

	"gorm.io/gorm"
)

// Service 聚合所有业务服务实例，供 handler 层调用。
type Service struct {
	db         *gorm.DB
	llmBaseURL string

	Task        *TaskService
	TaskQuery   *TaskQueryService
	Doc         *DocService
	Search      *SearchService
	Relation    *RelationService
	Requirement *RequirementService
}

// NewService 构建业务服务聚合对象，注入依赖。
// cfg 提供向量化服务（Python LLM 微服务）地址等外部依赖配置。
func NewService(db *gorm.DB, cfg *config.Config) *Service {
	// 需求分析服务依赖检索服务，先构建检索服务
	searchSvc := NewSearchService(db, cfg)
	return &Service{
		db:          db,
		llmBaseURL:  cfg.LLM.BaseURL,
		Task:        NewTaskService(db, cfg),
		TaskQuery:   NewTaskQueryService(db),
		Doc:         NewDocService(db, cfg.LLM.BaseURL),
		Search:      searchSvc,
		Relation:    NewRelationService(db),
		Requirement: NewRequirementService(searchSvc, cfg),
	}
}

// 各服务依赖的仓库统一在此构建，避免重复初始化。
func newDocRepo(db *gorm.DB) *repo.CodeFunctionDocRepo {
	return repo.NewCodeFunctionDocRepo(db)
}

func newTaskRepo(db *gorm.DB) *repo.TaskRecordRepo {
	return repo.NewTaskRecordRepo(db)
}

func newRelationRepo(db *gorm.DB) *repo.ModuleRelationRepo {
	return repo.NewModuleRelationRepo(db)
}