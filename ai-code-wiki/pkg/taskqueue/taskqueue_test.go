package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryQueueSubmitConsume(t *testing.T) {
	q := NewMemoryQueue()
	defer func() { _ = q.Close() }()

	payload := []byte(`{"task_id":"t-1"}`)
	for i := 0; i < 3; i++ {
		if err := q.SubmitTask(&TaskMessage{Type: TaskTypePipeline, Payload: payload}); err != nil {
			t.Fatalf("SubmitTask 失败: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		msg, err := q.ConsumeTask(ctx)
		if err != nil {
			t.Fatalf("ConsumeTask 失败: %v", err)
		}
		if msg.Type != TaskTypePipeline {
			t.Fatalf("任务类型不符: got %s want %s", msg.Type, TaskTypePipeline)
		}
		var p struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			t.Fatalf("载荷反序列化失败: %v", err)
		}
		if p.TaskID != "t-1" {
			t.Fatalf("任务 ID 不符: got %s", p.TaskID)
		}
	}
}

func TestMemoryQueueConsumeCancel(t *testing.T) {
	q := NewMemoryQueue()
	defer func() { _ = q.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := q.ConsumeTask(ctx); err == nil {
		t.Fatal("空队列应返回 ctx 取消错误")
	}
}

// TestConsumerRetryFlow 验证：处理器失败后自动重投重试，超过上限触发 OnMaxRetry。
func TestConsumerRetryFlow(t *testing.T) {
	q := NewMemoryQueue()
	defer func() { _ = q.Close() }()

	var executed int32
	var retried int32
	var maxRetried int32

	c := NewConsumer(q, 3) // 最多重试 3 次（首次失败 + 3 次重试 = 共执行 4 次）
	c.RegisterHandler(TaskTypePipeline, func(ctx context.Context, msg *TaskMessage) error {
		atomic.AddInt32(&executed, 1)
		return errors.New("模拟失败")
	})
	c.SetOnRetry(func(ctx context.Context, msg *TaskMessage, err error) {
		atomic.AddInt32(&retried, 1)
		if int32(msg.RetryCount) != atomic.LoadInt32(&retried) {
			t.Errorf("重试计数不符: msg=%d retried=%d", msg.RetryCount, retried)
		}
	})
	c.SetOnMaxRetry(func(ctx context.Context, msg *TaskMessage, err error) {
		atomic.AddInt32(&maxRetried, 1)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c.Start(ctx, 1)

	if err := q.SubmitTask(&TaskMessage{Type: TaskTypePipeline, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("SubmitTask 失败: %v", err)
	}

	// 等待重试耗尽
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&maxRetried) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	c.Stop()

	if got := atomic.LoadInt32(&executed); got != 4 {
		t.Errorf("执行次数不符: got %d want 4", got)
	}
	if got := atomic.LoadInt32(&retried); got != 3 {
		t.Errorf("重试次数不符: got %d want 3", got)
	}
	if got := atomic.LoadInt32(&maxRetried); got != 1 {
		t.Errorf("OnMaxRetry 次数不符: got %d want 1", got)
	}
}

// TestConsumerSuccessNoRetry 验证：处理器成功时不再重投。
func TestConsumerSuccessNoRetry(t *testing.T) {
	q := NewMemoryQueue()
	defer func() { _ = q.Close() }()

	var executed int32
	c := NewConsumer(q, 3)
	c.RegisterHandler(TaskTypePipeline, func(ctx context.Context, msg *TaskMessage) error {
		atomic.AddInt32(&executed, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	c.Start(ctx, 1)

	if err := q.SubmitTask(&TaskMessage{Type: TaskTypePipeline, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("SubmitTask 失败: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for atomic.LoadInt32(&executed) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	c.Stop()

	if got := atomic.LoadInt32(&executed); got != 1 {
		t.Errorf("执行次数不符: got %d want 1", got)
	}
}

// TestConsumerUnregisteredType 验证：未注册的任务类型被静默丢弃（不重试）。
func TestConsumerUnregisteredType(t *testing.T) {
	q := NewMemoryQueue()
	defer func() { _ = q.Close() }()

	c := NewConsumer(q, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	c.Start(ctx, 1)

	if err := q.SubmitTask(&TaskMessage{Type: "unknown_type", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("SubmitTask 失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	c.Stop()
}
