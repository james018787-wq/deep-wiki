// Package taskqueue 异步任务队列抽象接口与实现（内存 / RabbitMQ）。
//
// 设计要点：
//   - 队列统一投递 TaskMessage（可序列化），业务提交方与消费方不感知底层引擎；
//   - 接口 SubmitTask / ConsumeTask，实现选择见 New（TASK_QUEUE_DRIVER=memory/rabbitmq）；
//   - 消费端配合 Consumer 使用：注册任务处理器，失败自动重试，超过最大重试次数回调标记失败。
package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskType 任务类型标识，Consumer 按类型分发到对应处理器。
type TaskType string

// 内置任务类型（与业务对应）。
const (
	// TaskTypePipeline 代码解析任务（触发后执行增量解析流水线）。
	TaskTypePipeline TaskType = "pipeline"
	// TaskTypeVectorSync 向量同步任务（文档内容转向量写入向量库）。
	TaskTypeVectorSync TaskType = "vector_sync"
)

// TaskMessage 队列消息载体（必须可 JSON 序列化，保证跨引擎传输）。
type TaskMessage struct {
	Type       TaskType        `json:"type"`        // 任务类型
	Payload    json.RawMessage `json:"payload"`     // 业务载荷（由各处理器解析）
	RetryCount int             `json:"retry_count"` // 已重试次数（消费失败后自增）
}

// TaskQueue 异步任务队列抽象接口。
type TaskQueue interface {
	// SubmitTask 投递任务到队列，立即返回（异步执行）。
	SubmitTask(task *TaskMessage) error

	// ConsumeTask 阻塞消费一条任务，返回任务消息。
	// ctx 取消（服务关闭）时返回 ctx.Err()。
	ConsumeTask(ctx context.Context) (*TaskMessage, error)

	// Close 关闭队列，释放连接资源。
	Close() error
}

// Options 队列构建参数。
type Options struct {
	Driver      string // 队列驱动：memory / rabbitmq（TASK_QUEUE_DRIVER）
	RabbitMQURL string // RabbitMQ 连接地址（amqp://user:pass@host:port/，RABBITMQ_URL）
	QueueName   string // 队列名（TASK_QUEUE_NAME，默认 ai-code-wiki-task）
}

// New 根据驱动构建任务队列实例。
// 默认 memory（开发环境）；rabbitmq 连接失败时返回错误（生产环境 fail-fast）。
func New(opts Options) (TaskQueue, error) {
	switch opts.Driver {
	case "", "memory":
		return NewMemoryQueue(), nil
	case "rabbitmq":
		if opts.RabbitMQURL == "" {
			return nil, fmt.Errorf("RabbitMQ 未配置：需要 RABBITMQ_URL")
		}
		return NewRabbitMQ(opts.RabbitMQURL, opts.QueueName)
	default:
		return nil, fmt.Errorf("不支持的任务队列驱动 %q（仅支持 memory / rabbitmq）", opts.Driver)
	}
}
