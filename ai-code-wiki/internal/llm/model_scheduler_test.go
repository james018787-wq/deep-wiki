package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeClient 可编程的模型调用客户端。
type fakeClient struct {
	mu     sync.Mutex
	fn     func(m *ModelItem) (string, Usage, error)
	calls  atomic.Int32
	models []string // 按调用顺序记录模型名
}

func (f *fakeClient) Chat(_ context.Context, m *ModelItem, _, _ string) (string, Usage, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.models = append(f.models, m.Name)
	fn := f.fn
	f.mu.Unlock()
	content, usage, err := fn(m)
	if err != nil {
		return "", Usage{}, err
	}
	return content, usage, nil
}

func (f *fakeClient) calledModels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.models))
	copy(out, f.models)
	return out
}

func okClient() *fakeClient {
	return &fakeClient{fn: func(m *ModelItem) (string, Usage, error) {
		return "回答内容", Usage{PromptTokens: 100, CompletionTokens: 50}, nil
	}}
}

func failClient(kind ErrorKind) *fakeClient {
	return &fakeClient{fn: func(m *ModelItem) (string, Usage, error) {
		return "", Usage{}, &CallError{Kind: kind, Message: "boom"}
	}}
}

func testPool(models []*ModelItem, g GlobalConfig) *ModelPool {
	g.applyDefaults()
	return &ModelPool{global: g, models: models}
}

func mk(name string, price float64, maxCtx int) *ModelItem {
	return &ModelItem{Name: name, BaseUrl: "http://example.com/v1", InputPrice: price, OutputPrice: price, MaxContext: maxCtx, Enable: true}
}

func newSched(pool *ModelPool, client Client) *Scheduler {
	return NewScheduler(pool, client, nil, nil)
}

func TestForceModel(t *testing.T) {
	pool := testPool([]*ModelItem{mk("cheap", 0.001, 64000), mk("gold", 0.1, 64000)}, defaultGlobal())
	client := okClient()
	s := newSched(pool, client)

	res, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{ForceModel: "gold"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.UsedModelName != "gold" {
		t.Errorf("期望直连 gold，实际 %s", res.UsedModelName)
	}
	if res.SwitchedCount != 0 || res.Content == "" || res.Cost <= 0 {
		t.Errorf("force_model 直连结果异常: %+v", res)
	}
	if got := client.calledModels(); len(got) != 1 || got[0] != "gold" {
		t.Errorf("应只调用 gold，实际 %v", got)
	}
}

func TestForceModelNotExist(t *testing.T) {
	pool := testPool([]*ModelItem{mk("cheap", 0.001, 64000)}, defaultGlobal())
	s := newSched(pool, okClient())
	if _, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{ForceModel: "nope"}); err == nil {
		t.Fatal("指定不存在模型应报错")
	}
}

func TestLowPriceFirst(t *testing.T) {
	pool := testPool([]*ModelItem{
		mk("middle", 0.01, 64000),
		mk("cheap", 0.001, 64000),
		mk("expensive", 0.1, 64000),
	}, defaultGlobal())
	client := okClient()
	s := newSched(pool, client)

	res, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.UsedModelName != "cheap" {
		t.Errorf("应优先选最低价 cheap，实际 %s", res.UsedModelName)
	}
	if got := client.calledModels(); len(got) != 1 || got[0] != "cheap" {
		t.Errorf("应只调用 cheap，实际 %v", got)
	}
}

func TestRetriableDowngrade(t *testing.T) {
	pool := testPool([]*ModelItem{
		mk("cheap", 0.001, 64000),
		mk("middle", 0.01, 64000),
		mk("expensive", 0.1, 64000),
	}, defaultGlobal())
	client := failClient(ErrKindUpstream)
	// cheap 失败，middle 成功
	client.fn = func(m *ModelItem) (string, Usage, error) {
		if m.Name == "cheap" {
			return "", Usage{}, &CallError{Kind: ErrKindUpstream, StatusCode: 500, Message: "上游异常"}
		}
		return "ok", Usage{PromptTokens: 10, CompletionTokens: 5}, nil
	}
	s := newSched(pool, client)

	res, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.UsedModelName != "middle" {
		t.Errorf("应降级到 middle，实际 %s", res.UsedModelName)
	}
	if res.SwitchedCount != 1 {
		t.Errorf("SwitchedCount 应为 1，实际 %d", res.SwitchedCount)
	}
	if got := client.calledModels(); len(got) != 2 || got[0] != "cheap" || got[1] != "middle" {
		t.Errorf("调用顺序异常: %v", got)
	}
	if len(res.RetriedModels) != 1 || res.RetriedModels[0] != "cheap" {
		t.Errorf("RetriedModels 应为 [cheap]，实际 %v", res.RetriedModels)
	}
}

func TestBusinessErrorNoSwitch(t *testing.T) {
	pool := testPool([]*ModelItem{mk("cheap", 0.001, 64000), mk("middle", 0.01, 64000)}, defaultGlobal())
	client := failClient(ErrKindBadRequest)
	s := newSched(pool, client)

	_, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{})
	if err == nil {
		t.Fatal("业务错误应直接返回错误")
	}
	if got := client.calledModels(); len(got) != 1 {
		t.Errorf("业务错误不应切换模型，实际调用 %v", got)
	}
	var ce *CallError
	if !errors.As(err, &ce) || ce.Kind != ErrKindBadRequest {
		t.Errorf("应透传原始业务错误，实际 %v", err)
	}
}

func TestAuthMarkUnavailable(t *testing.T) {
	pool := testPool([]*ModelItem{mk("cheap", 0.001, 64000), mk("middle", 0.01, 64000)}, defaultGlobal())
	client := failClient(ErrKindAuth)
	s := newSched(pool, client)

	_, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{})
	if err != ErrAllFailed {
		t.Fatalf("鉴权失败不应直接返回，应继续切换，实际 err=%v", err)
	}
	if got := client.calledModels(); len(got) != 2 {
		t.Errorf("鉴权失败应切换下一档，实际调用 %v", got)
	}
}

func TestAllFailed(t *testing.T) {
	pool := testPool([]*ModelItem{
		mk("cheap", 0.001, 64000),
		mk("middle", 0.01, 64000),
		mk("expensive", 0.1, 64000),
	}, defaultGlobal())
	client := failClient(ErrKindTimeout)
	s := newSched(pool, client)

	_, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{})
	if err != ErrAllFailed {
		t.Fatalf("全部失败应返回 ErrAllFailed，实际 %v", err)
	}
	if got := client.calledModels(); len(got) != 3 {
		t.Errorf("max_retry_switch=2 时应尝试 3 个模型，实际 %d", len(got))
	}
}

func TestMaxSwitchLimit(t *testing.T) {
	// global.max_retry_switch=1：最多尝试 2 个模型
	pool := testPool([]*ModelItem{
		mk("a", 0.001, 64000),
		mk("b", 0.01, 64000),
		mk("c", 0.1, 64000),
	}, GlobalConfig{MaxRetrySwitch: 1})
	client := failClient(ErrKindNetwork)
	s := newSched(pool, client)

	_, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{})
	if err != ErrAllFailed {
		t.Fatalf("应返回 ErrAllFailed，实际 %v", err)
	}
	if got := client.calledModels(); len(got) != 2 {
		t.Errorf("max_retry_switch=1 时应尝试 2 个模型，实际 %d", len(got))
	}
}

func TestForceHighQuality(t *testing.T) {
	pool := testPool([]*ModelItem{
		mk("cheap", 0.001, 64000), // 低于阈值 0.2，应被过滤
		mk("cheap2", 0.05, 64000), // 低于阈值 0.2，应被过滤
		mk("gold", 0.5, 64000),    // 高于阈值，保留
	}, defaultGlobal())
	client := okClient()
	s := newSched(pool, client)

	res, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{ForceHighQuality: true})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.UsedModelName != "gold" {
		t.Errorf("force_high_quality 应只保留 gold，实际 %s", res.UsedModelName)
	}
	if got := client.calledModels(); len(got) != 1 || got[0] != "gold" {
		t.Errorf("应只调用 gold，实际 %v", got)
	}
}

func TestContextOverflowFilter(t *testing.T) {
	pool := testPool([]*ModelItem{
		mk("small", 0.001, 100),  // max_context=100，预估 500，被过滤
		mk("large", 0.01, 10000), // 保留
	}, defaultGlobal())
	client := okClient()
	s := newSched(pool, client)

	res, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{EstimatedTokenLen: 500})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.UsedModelName != "large" {
		t.Errorf("上下文超限模型应被过滤，实际 %s", res.UsedModelName)
	}
}

func TestNoCandidates(t *testing.T) {
	pool := testPool(nil, defaultGlobal())
	s := newSched(pool, okClient())
	if _, err := s.Chat(context.Background(), "sys", "user", SchedulerOption{}); err != ErrNoModel {
		t.Fatalf("空模型池应返回 ErrNoModel，实际 %v", err)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"hello", (5 + 3) / 4}, // 2
		{"你好", 2},
		{"你好hello", 2 + (5+3)/4}, // 4
	}
	for _, c := range cases {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCandidatesSortedByPrice(t *testing.T) {
	pool := testPool([]*ModelItem{
		mk("c", 0.1, 64000),
		mk("a", 0.001, 64000),
		mk("b", 0.01, 64000),
	}, defaultGlobal())
	cands := pool.Candidates(SchedulerOption{})
	if len(cands) != 3 {
		t.Fatalf("候选数应为 3，实际 %d", len(cands))
	}
	for i, want := range []string{"a", "b", "c"} {
		if cands[i].Name != want {
			t.Errorf("候选第 %d 位应为 %s，实际 %s", i, want, cands[i].Name)
		}
	}
}

func TestEnvExpand(t *testing.T) {
	t.Setenv("LLM_TEST_KEY", "secret")
	if got := expandEnv("${LLM_TEST_KEY}"); got != "secret" {
		t.Errorf("expandEnv 展开失败: %s", got)
	}
	if got := expandEnv("plain"); got != "plain" {
		t.Errorf("无占位符应原样返回: %s", got)
	}
}
