// Package service 业务逻辑层，编排 repo 与 pkg 工具，实现核心业务规则。
package service

import (
	"context"
	"fmt"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/taskqueue"
	"ai-code-wiki/pkg/vector"

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
	Report      *ReportService

	TaskQueue  taskqueue.TaskQueue // 异步任务队列（SubmitTask 提交，worker 消费）
	TaskWorker *TaskWorker         // 独立消费协程 Worker（消费解析/向量任务）
}

// NewService 构建业务服务聚合对象，注入依赖。
// cfg 提供向量化服务（Python LLM 微服务）地址等外部依赖配置。
// 向量引擎按 VECTOR_DRIVER 选择（chroma/milvus），构建失败时降级为 nil
// （向量同步跳过、向量检索返回"未配置"），不阻塞服务启动。
// 任务队列按 TASK_QUEUE_DRIVER 选择（memory/rabbitmq），RabbitMQ 连接失败返回错误。
func NewService(db *gorm.DB, cfg *config.Config) (*Service, error) {
	// 构建向量存储抽象（业务代码只依赖 VectorClient 接口，不感知底层引擎）
	vc, err := vector.NewVectorClient(vector.Options{
		Driver:         cfg.Vector.Engine,
		ChromaURL:      chromaURLFromConfig(&cfg.Vector),
		Collection:     cfg.Vector.Collection,
		EmbedBaseURL:   cfg.LLM.BaseURL,
		MilvusHost:     cfg.Vector.Host,
		MilvusPort:     cfg.Vector.Port,
		MilvusDim:      cfg.Vector.Dim,
		MilvusUser:     cfg.Vector.User,
		MilvusPassword: cfg.Vector.Password,
	})
	if err != nil {
		logger.Warn(context.Background(), "向量引擎初始化失败，向量同步/检索降级跳过: %v", err)
		vc = nil
	}

	// 构建异步任务队列（TASK_QUEUE_DRIVER=memory/rabbitmq，默认 memory）
	queue, err := taskqueue.New(taskqueue.Options{
		Driver:      cfg.TaskQueue.Driver,
		RabbitMQURL: cfg.TaskQueue.RabbitMQURL,
		QueueName:   cfg.TaskQueue.QueueName,
	})
	if err != nil {
		return nil, err
	}

	// 需求分析服务依赖检索服务，先构建检索服务
	searchSvc := NewSearchService(db, cfg, vc)
	taskSvc := NewTaskService(db, cfg, vc, queue)
	s := &Service{
		db:          db,
		llmBaseURL:  cfg.LLM.BaseURL,
		Task:        taskSvc,
		TaskQuery:   NewTaskQueryService(db),
		Doc:         NewDocService(db, vc, queue),
		Search:      searchSvc,
		Relation:    NewRelationService(db),
		Requirement: NewRequirementService(searchSvc, cfg),
		Report:      NewReportService(db),
		TaskQueue:   queue,
		TaskWorker:  NewTaskWorker(queue, taskSvc, vc, cfg.TaskQueue.MaxRetry, cfg.TaskQueue.Concurrency),
	}
	return s, nil
}

// chromaURLFromConfig 由向量配置拼接 chroma HTTP 地址（host 为空时返回空串）。
func chromaURLFromConfig(cfg *config.VectorConfig) string {
	if cfg.Host == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
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
