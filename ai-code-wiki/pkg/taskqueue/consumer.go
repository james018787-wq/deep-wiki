package taskqueue

import (
	"context"
	"sync"
	"time"

	"ai-code-wiki/pkg/logger"
)

// TaskHandler 任务处理器：消费并执行一条任务。
// 返回 error 表示执行失败（触发重试），nil 表示成功。
type TaskHandler func(ctx context.Context, msg *TaskMessage) error

// Consumer 后台消费协程，按任务类型分发到注册的处理器。
//
// 重试策略：
//   - 处理器返回错误时自动重新投递任务（RetryCount+1）；
//   - 重试次数达到 maxRetry 时调用 OnMaxRetry 回调（由业务层标记任务失败）；
//   - 每次进入重试前调用 OnRetry 回调（由业务层记录失败重试）。
type Consumer struct {
	queue    TaskQueue
	handlers map[TaskType]TaskHandler
	maxRetry int

	onRetry    func(ctx context.Context, msg *TaskMessage, err error)
	onMaxRetry func(ctx context.Context, msg *TaskMessage, err error)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewConsumer 构建消费者。
func NewConsumer(queue TaskQueue, maxRetry int) *Consumer {
	if maxRetry < 0 {
		maxRetry = 0
	}
	return &Consumer{
		queue:    queue,
		handlers: make(map[TaskType]TaskHandler),
		maxRetry: maxRetry,
	}
}

// RegisterHandler 注册任务类型对应的处理器。
func (c *Consumer) RegisterHandler(t TaskType, h TaskHandler) {
	c.handlers[t] = h
}

// SetOnRetry 设置进入重试前的回调（记录失败重试）。
func (c *Consumer) SetOnRetry(f func(ctx context.Context, msg *TaskMessage, err error)) {
	c.onRetry = f
}

// SetOnMaxRetry 设置重试耗尽回调（标记任务失败）。
func (c *Consumer) SetOnMaxRetry(f func(ctx context.Context, msg *TaskMessage, err error)) {
	c.onMaxRetry = f
}

// Start 启动 concurrency 个后台消费协程，阻塞于 ConsumeTask。
// ctx 取消（服务关闭）时协程退出。
func (c *Consumer) Start(ctx context.Context, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	child, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	for i := 0; i < concurrency; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.consumeLoop(child)
		}()
	}
}

// Stop 停止消费协程（等待当前正在处理的任务结束）。
func (c *Consumer) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

// consumeLoop 单消费协程：循环取任务并执行。
func (c *Consumer) consumeLoop(ctx context.Context) {
	for {
		msg, err := c.queue.ConsumeTask(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // 服务关闭
			}
			logger.Warn(ctx, "消费任务失败，1s 后重试: %v", err)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		c.handle(ctx, msg)
	}
}

// handle 分发任务并处理失败重试。
func (c *Consumer) handle(ctx context.Context, msg *TaskMessage) {
	h, ok := c.handlers[msg.Type]
	if !ok {
		logger.Warn(ctx, "未注册任务处理器 type=%s，任务丢弃", msg.Type)
		return
	}
	if err := h(ctx, msg); err == nil {
		return
	} else {
		c.retry(ctx, msg, err)
	}
}

// retry 失败重试：未达上限重新投递，达上限回调 OnMaxRetry。
func (c *Consumer) retry(ctx context.Context, msg *TaskMessage, err error) {
	if msg.RetryCount < c.maxRetry {
		msg.RetryCount++
		if c.onRetry != nil {
			c.onRetry(ctx, msg, err)
		}
		if err2 := c.queue.SubmitTask(msg); err2 != nil {
			logger.Error(ctx, "任务重新投递失败 type=%s: %v", msg.Type, err2)
			return
		}
		logger.Warn(ctx, "任务执行失败，已重新投递 type=%s retry=%d/%d err=%v",
			msg.Type, msg.RetryCount, c.maxRetry, err)
		return
	}
	// 重试次数耗尽
	if c.onMaxRetry != nil {
		c.onMaxRetry(ctx, msg, err)
	} else {
		logger.Error(ctx, "任务重试次数耗尽 type=%s err=%v", msg.Type, err)
	}
}
