package taskqueue

import (
	"context"
	"sync"
)

// memoryQueueSize 内存队列缓冲大小（开发环境使用，超出后投递方阻塞等待消费）。
const memoryQueueSize = 1024

// memoryQueue 内存队列实现（开发环境）。
// 基于有缓冲 channel，进程内消费，任务不跨实例、不持久化。
type memoryQueue struct {
	ch   chan *TaskMessage
	once sync.Once
}

// NewMemoryQueue 构建内存队列。
func NewMemoryQueue() TaskQueue {
	return &memoryQueue{ch: make(chan *TaskMessage, memoryQueueSize)}
}

// SubmitTask 投递任务到内存队列（channel 满时阻塞等待消费）。
func (q *memoryQueue) SubmitTask(task *TaskMessage) error {
	q.ch <- task
	return nil
}

// ConsumeTask 阻塞消费一条任务。
func (q *memoryQueue) ConsumeTask(ctx context.Context) (*TaskMessage, error) {
	select {
	case task := <-q.ch:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close 关闭队列（不再接收新任务，消费方收到 nil 后退出）。
func (q *memoryQueue) Close() error {
	q.once.Do(func() {
		close(q.ch)
	})
	return nil
}
