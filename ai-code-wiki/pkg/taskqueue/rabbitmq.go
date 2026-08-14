package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// defaultQueueName RabbitMQ 默认队列名。
const defaultQueueName = "ai-code-wiki-task"

// rabbitMQQueue RabbitMQ 队列实现（生产环境）。
// 消息持久化（Persistent）+ 手动 ACK；消费失败返回错误后由上层重试。
type rabbitMQQueue struct {
	conn *amqp.Connection
	ch   *amqp.Channel
	name string
	msgs <-chan amqp.Delivery
}

// NewRabbitMQ 构建 RabbitMQ 队列（持久化队列声明 + 开启消费通道）。
// rabbitmqURL 形如 amqp://user:pass@host:port/。
func NewRabbitMQ(rabbitmqURL, queueName string) (TaskQueue, error) {
	if strings.TrimSpace(queueName) == "" {
		queueName = defaultQueueName
	}

	conn, err := amqp.DialConfig(rabbitmqURL, amqp.Config{
		Heartbeat: 10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("创建 RabbitMQ 通道失败: %w", err)
	}

	// 声明持久化队列（durable=true），保证重启后队列与消息不丢
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("声明 RabbitMQ 队列 %s 失败: %w", queueName, err)
	}

	// 开启消费通道：autoAck=false，由 ConsumeTask 手动 ACK
	msgs, err := ch.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("开启 RabbitMQ 消费失败: %w", err)
	}

	return &rabbitMQQueue{conn: conn, ch: ch, name: queueName, msgs: msgs}, nil
}

// SubmitTask 投递任务到 RabbitMQ 队列（消息持久化）。
func (q *rabbitMQQueue) SubmitTask(task *TaskMessage) error {
	if task == nil {
		return fmt.Errorf("任务为空")
	}
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("任务序列化失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return q.ch.PublishWithContext(ctx, "", q.name, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent, // 持久化消息
		Body:         body,
	})
}

// ConsumeTask 阻塞消费一条任务，成功消费后手动 ACK。
func (q *rabbitMQQueue) ConsumeTask(ctx context.Context) (*TaskMessage, error) {
	select {
	case d, ok := <-q.msgs:
		if !ok {
			return nil, fmt.Errorf("RabbitMQ 消费通道已关闭")
		}
		var task TaskMessage
		if err := json.Unmarshal(d.Body, &task); err != nil {
			_ = d.Nack(false, false) // 反序列化失败：直接丢弃，避免死循环
			return nil, fmt.Errorf("任务反序列化失败: %w", err)
		}
		_ = d.Ack(false)
		return &task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close 关闭 RabbitMQ 连接。
func (q *rabbitMQQueue) Close() error {
	if q.conn != nil {
		return q.conn.Close()
	}
	return nil
}
