package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/taskqueue"
	"ai-code-wiki/pkg/vector"

	"gorm.io/gorm"
)

// ============ 队列任务载荷（提交方与消费方共用） ============

// pipelineTaskPayload 代码解析任务载荷。
type pipelineTaskPayload struct {
	TaskID string `json:"task_id"` // 关联 task_record.task_id
}

// vectorSyncTaskPayload 向量同步任务载荷（最小切片单元=单个函数文档）。
type vectorSyncTaskPayload struct {
	DocID      int64  `json:"doc_id"`
	ModuleName string `json:"module_name"`
	FilePath   string `json:"file_path"`
	FuncName   string `json:"func_name"`
	Content    string `json:"content"`
}

// buildPipelineMessage 构建代码解析任务队列消息。
func buildPipelineMessage(taskID string) (*taskqueue.TaskMessage, error) {
	payload, err := json.Marshal(pipelineTaskPayload{TaskID: taskID})
	if err != nil {
		return nil, fmt.Errorf("解析任务载荷序列化失败: %w", err)
	}
	return &taskqueue.TaskMessage{Type: taskqueue.TaskTypePipeline, Payload: payload}, nil
}

// buildVectorSyncMessage 构建向量同步任务队列消息。
func buildVectorSyncMessage(doc *model.CodeFunctionDoc) (*taskqueue.TaskMessage, error) {
	content := strings.Join([]string{doc.Summary, doc.ProcessFlow, doc.RiskPoint}, "\n")
	payload, err := json.Marshal(vectorSyncTaskPayload{
		DocID:      doc.ID,
		ModuleName: doc.ModuleName,
		FilePath:   doc.FilePath,
		FuncName:   doc.FuncName,
		Content:    content,
	})
	if err != nil {
		return nil, fmt.Errorf("向量任务载荷序列化失败: %w", err)
	}
	return &taskqueue.TaskMessage{Type: taskqueue.TaskTypeVectorSync, Payload: payload}, nil
}

// ============ 任务消费 Worker（独立后台协程） ============

// TaskWorker 队列消费 Worker：注册业务任务处理器并启动独立后台协程。
// 解析任务 / 向量更新任务统一在这里消费，失败自动重试并记录到 task_record。
type TaskWorker struct {
	queue       taskqueue.TaskQueue
	consumer    *taskqueue.Consumer
	taskSvc     *TaskService
	vc          vector.VectorClient
	maxRetry    int
	concurrency int
}

// NewTaskWorker 构建消费 Worker。
func NewTaskWorker(queue taskqueue.TaskQueue, taskSvc *TaskService, vc vector.VectorClient, maxRetry, concurrency int) *TaskWorker {
	if maxRetry < 0 {
		maxRetry = 0
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return &TaskWorker{
		queue:       queue,
		taskSvc:     taskSvc,
		vc:          vc,
		maxRetry:    maxRetry,
		concurrency: concurrency,
	}
}

// Start 启动消费协程（阻塞在 ConsumeTask），ctx 取消时协程退出。
func (w *TaskWorker) Start(ctx context.Context) {
	w.consumer = taskqueue.NewConsumer(w.queue, w.maxRetry)
	w.consumer.SetOnRetry(w.onRetry)
	w.consumer.SetOnMaxRetry(w.onMaxRetry)
	w.consumer.RegisterHandler(taskqueue.TaskTypePipeline, w.handlePipeline)
	w.consumer.RegisterHandler(taskqueue.TaskTypeVectorSync, w.handleVectorSync)
	w.consumer.Start(ctx, w.concurrency)
	logger.Info(ctx, "任务消费协程启动 concurrency=%d maxRetry=%d", w.concurrency, w.maxRetry)
}

// Stop 停止消费协程并关闭队列连接。
func (w *TaskWorker) Stop() {
	if w.consumer != nil {
		w.consumer.Stop()
	}
	if w.queue != nil {
		_ = w.queue.Close()
	}
}

// handlePipeline 消费代码解析任务：按 task_id 读取记录并执行增量解析流水线。
func (w *TaskWorker) handlePipeline(ctx context.Context, msg *taskqueue.TaskMessage) error {
	var p pipelineTaskPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return fmt.Errorf("解析任务载荷失败: %w", err)
	}
	if p.TaskID == "" {
		return fmt.Errorf("任务载荷缺少 task_id")
	}
	record, err := w.taskSvc.taskRepo.GetByTaskID(p.TaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // 任务记录已删除，忽略
		}
		return fmt.Errorf("查询任务 %s 失败: %w", p.TaskID, err)
	}
	return w.taskSvc.runPipeline(record)
}

// handleVectorSync 消费向量同步任务：将文档内容转向量写入向量库。
func (w *TaskWorker) handleVectorSync(ctx context.Context, msg *taskqueue.TaskMessage) error {
	var p vectorSyncTaskPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return fmt.Errorf("解析向量任务载荷失败: %w", err)
	}
	if p.DocID <= 0 {
		return fmt.Errorf("向量任务载荷缺少 doc_id")
	}
	if w.vc == nil {
		logger.Warn(ctx, "向量引擎未配置，跳过向量同步 doc_id=%d", p.DocID)
		return nil
	}
	dv := &vector.DocVector{
		DocID:      p.DocID,
		ModuleName: p.ModuleName,
		FilePath:   p.FilePath,
		FuncName:   p.FuncName,
		Content:    p.Content,
	}
	if err := w.vc.UpsertDoc(dv); err != nil {
		return fmt.Errorf("向量同步失败 doc_id=%d: %w", p.DocID, err)
	}
	return nil
}

// onRetry 进入重试前回调：解析任务记录失败重试（retry_count+1，状态置回待执行）。
func (w *TaskWorker) onRetry(ctx context.Context, msg *taskqueue.TaskMessage, err error) {
	if msg.Type == taskqueue.TaskTypePipeline {
		var p pipelineTaskPayload
		if json.Unmarshal(msg.Payload, &p) == nil && p.TaskID != "" {
			if err2 := w.taskSvc.taskRepo.IncrementRetry(p.TaskID, msg.RetryCount); err2 != nil {
				logger.Warn(ctx, "记录任务重试失败 task_id=%s err=%v", p.TaskID, err2)
			}
		}
	}
	logger.Warn(ctx, "任务失败进入重试 type=%s retry=%d/%d err=%v", msg.Type, msg.RetryCount, w.maxRetry, err)
}

// onMaxRetry 重试耗尽回调：解析任务标记失败。
func (w *TaskWorker) onMaxRetry(ctx context.Context, msg *taskqueue.TaskMessage, err error) {
	if msg.Type == taskqueue.TaskTypePipeline {
		var p pipelineTaskPayload
		if json.Unmarshal(msg.Payload, &p) == nil && p.TaskID != "" {
			_ = w.taskSvc.MarkFailed(p.TaskID, "任务重试次数耗尽: "+err.Error())
		}
	}
	logger.Error(ctx, "任务重试次数耗尽 type=%s retry=%d err=%v", msg.Type, msg.RetryCount, err)
}
