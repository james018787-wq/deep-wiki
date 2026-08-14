// Package taskqueue 异步任务队列抽象接口。
// 当前实现：SubmitAsyncTask 直接以 goroutine 本地执行（MVP）。
// 生产环境可替换为 RabbitMQ / Kafka 等消息队列实现：
// 实现同一 TaskQueue 接口（提交时序列化任务投递到 MQ，由消费者进程执行），
// 调用方代码无需改动。
package taskqueue

// TaskQueue 异步任务队列抽象接口。
type TaskQueue interface {
	// SubmitAsyncTask 提交一个异步任务，立即返回。
	SubmitAsyncTask(task func())
}

// localQueue 本地 goroutine 队列（当前默认实现）。
type localQueue struct{}

// SubmitAsyncTask 直接以 goroutine 执行任务。
//
// TODO(生产环境替换)：
//   - RabbitMQ：任务序列化为消息投递到队列/交换机，消费者 worker 执行，支持 ACK/重试；
//   - Kafka：任务作为事件投递到 topic，消费者组并行消费，支持分区扩容；
//   - 替换后调用方无需改动，仅需换掉 Default 指向的实现。
func (q *localQueue) SubmitAsyncTask(task func()) {
	if task == nil {
		return
	}
	go task()
}

// Default 默认异步任务队列实例（本地 goroutine 实现）。
var Default TaskQueue = &localQueue{}

// Submit 便捷方法：提交异步任务到默认队列。
func Submit(task func()) {
	Default.SubmitAsyncTask(task)
}