package service

import (
	"strings"
	"testing"

	"ai-code-wiki/internal/model"
)

// TestBfsReverseForward 测试双向 BFS 影响传播：
//
//	order.CreateOrder →(intra) order.validateOrder
//	order.CreateOrder →(cross) user.GetUser
//	order.HandlePayCallback →(cross) user.GetUser
//
// 种子=order.CreateOrder 时：
//   - 反向：无上游调用方
//   - 正向：order.validateOrder、user.GetUser
func TestBfsReverseForward(t *testing.T) {
	edges := []*model.FunctionCallEdge{
		{CallerModule: "order", CallerFunc: "CreateOrder", CalleeModule: "order", CalleeFunc: "validateOrder", CallKind: model.CallKindIntraPackage},
		{CallerModule: "order", CallerFunc: "CreateOrder", CalleeModule: "user", CalleeFunc: "GetUser", CallKind: model.CallKindCrossPackage},
		{CallerModule: "order", CallerFunc: "HandlePayCallback", CalleeModule: "user", CalleeFunc: "GetUser", CallKind: model.CallKindCrossPackage},
	}
	g := buildCallGraph(edges)

	reverse := bfs(g, []string{"order.CreateOrder"}, 2, true)
	if len(reverse) != 0 {
		t.Fatalf("反向传播应无结果，got %+v", reverse)
	}

	forward := bfs(g, []string{"order.CreateOrder"}, 2, false)
	if len(forward) != 2 {
		t.Fatalf("正向传播应命中2个下游，got %d: %+v", len(forward), forward)
	}
	got := map[string]*FuncRef{}
	for _, f := range forward {
		got[funcKey(f.Module, f.Func)] = f
	}
	if f := got["order.validateOrder"]; f == nil || f.Depth != 1 {
		t.Errorf("缺少同包下游 order.validateOrder: %+v", forward)
	}
	if f := got["user.GetUser"]; f == nil || f.Depth != 1 {
		t.Errorf("缺少跨包下游 user.GetUser: %+v", forward)
	}
}

// TestBfsReverseChain 测试反向链式传播：
//
//	order.CreateOrder → user.GetUser
//	user.GetUser → user.UpdateUser（同包）
//
// 种子=user.GetUser 时反向应命中 order.CreateOrder。
func TestBfsReverseChain(t *testing.T) {
	edges := []*model.FunctionCallEdge{
		{CallerModule: "order", CallerFunc: "CreateOrder", CalleeModule: "user", CalleeFunc: "GetUser", CallKind: model.CallKindCrossPackage},
		{CallerModule: "user", CallerFunc: "GetUser", CalleeModule: "user", CalleeFunc: "UpdateUser", CallKind: model.CallKindIntraPackage},
	}
	g := buildCallGraph(edges)

	reverse := bfs(g, []string{"user.GetUser"}, 1, true)
	if len(reverse) != 1 {
		t.Fatalf("反向应命中1个上游，got %d: %+v", len(reverse), reverse)
	}
	if reverse[0].Module != "order" || reverse[0].Func != "CreateOrder" {
		t.Errorf("上游应为 order.CreateOrder，got %+v", reverse[0])
	}

	forward := bfs(g, []string{"user.GetUser"}, 1, false)
	if len(forward) != 1 || forward[0].Func != "UpdateUser" {
		t.Errorf("正向应命中 user.UpdateUser，got %+v", forward)
	}
}

// TestBfsDedupAndDepth 测试去重与深度分层：
// 两层反向传播中同一函数被多条路径命中时只出现一次，且按首次命中深度归层。
func TestBfsDedupAndDepth(t *testing.T) {
	edges := []*model.FunctionCallEdge{
		{CallerModule: "order", CallerFunc: "A", CalleeModule: "user", CalleeFunc: "T", CallKind: model.CallKindCrossPackage},
		{CallerModule: "order", CallerFunc: "B", CalleeModule: "user", CalleeFunc: "T", CallKind: model.CallKindCrossPackage},
		{CallerModule: "order", CallerFunc: "C", CalleeModule: "order", CalleeFunc: "A", CallKind: model.CallKindIntraPackage},
		{CallerModule: "order", CallerFunc: "C", CalleeModule: "order", CalleeFunc: "B", CallKind: model.CallKindIntraPackage},
	}
	g := buildCallGraph(edges)

	// 种子 user.T，深度2：深度1=A,B；深度2=C
	reverse := bfs(g, []string{"user.T"}, 2, true)
	if len(reverse) != 3 {
		t.Fatalf("应命中3个上游，got %d: %+v", len(reverse), reverse)
	}
	depthMap := map[string]int{}
	for _, f := range reverse {
		depthMap[f.Func] = f.Depth
	}
	if depthMap["A"] != 1 || depthMap["B"] != 1 || depthMap["C"] != 2 {
		t.Errorf("深度分层错误: %+v", depthMap)
	}
}

// TestFuncKey 模块.函数 键生成。
func TestFuncKey(t *testing.T) {
	if funcKey("order", "CreateOrder") != "order.CreateOrder" {
		t.Fatal("funcKey 拼接错误")
	}
}

// TestParseImpactDesignJSON LLM 输出容错解析（代码块包裹、前后多余文本）。
func TestParseImpactDesignJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string // 期望 change_summary 前缀
	}{
		{name: "纯净JSON", raw: `{"change_summary":"订单模块改造","business_impact":"影响下单流程","attention":"回归下单"}`,
			want: "订单模块改造"},
		{name: "markdown代码块", raw: "```json\n{\"change_summary\":\"支付回调改动\",\"business_impact\":\"影响支付\",\"attention\":\"回归支付\"}\n```",
			want: "支付回调改动"},
		{name: "前后多余文本", raw: "好的，分析如下：\n{\"change_summary\":\"用户服务重构\",\"business_impact\":\"影响用户查询\",\"attention\":\"回归用户\"}\n以上。",
			want: "用户服务重构"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseImpactDesignJSON(tc.raw)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if !strings.HasPrefix(got.ChangeSummary, tc.want) {
				t.Errorf("change_summary 不匹配: got %q want 前缀 %q", got.ChangeSummary, tc.want)
			}
		})
	}

	if _, err := parseImpactDesignJSON("没有 JSON 的输出"); err == nil {
		t.Fatal("无 JSON 时应报错")
	}
}

// TestBuildImpactUserPrompt 影响清单 Prompt 组装包含变更与上游。
func TestBuildImpactUserPrompt(t *testing.T) {
	result := &ImpactAnalyzeResult{
		Changed: []*FuncRef{{Module: "user", Func: "GetUser", Summary: "按ID查用户"}},
		Reverse: []*FuncRef{{Module: "order", Func: "CreateOrder", Depth: 1, Edge: "order.CreateOrder 调用了 user.GetUser", Summary: "创建订单"}},
	}
	p := buildImpactUserPrompt(result)
	if !strings.Contains(p, "user.GetUser") || !strings.Contains(p, "order.CreateOrder") ||
		!strings.Contains(p, "直接修改的函数") || !strings.Contains(p, "上游调用方") {
		t.Fatalf("Prompt 组装缺失关键内容: %s", p)
	}
}

// TestMergeFuncSeeds 多轮追问会话种子合并：按 模块.函数 去重。
func TestMergeFuncSeeds(t *testing.T) {
	prev := []*FuncRef{{Module: "order", Func: "CreateOrder", File: "order/order.go"}}
	next := []*FuncRef{{Module: "order", Func: "CreateOrder"}, {Module: "user", Func: "GetUser"}}
	merged := mergeFuncSeeds(prev, next)
	if len(merged) != 2 {
		t.Fatalf("合并后应去重为2个种子，got %d: %+v", len(merged), merged)
	}
	if merged[0].File != "order/order.go" {
		t.Errorf("应保留首次出现的文件信息，got %+v", merged[0])
	}
}

// TestFuncsFromDocs 文档列表映射为变更种子（去重+上限）。
func TestFuncsFromDocs(t *testing.T) {
	docs := []*model.CodeFunctionDoc{
		{ModuleName: "order", FuncName: "CreateOrder", FilePath: "order/order.go", Summary: "创建订单"},
		{ModuleName: "order", FuncName: "CreateOrder", FilePath: "order/other.go", Summary: "重复"},
		{ModuleName: "user", FuncName: "GetUser", FilePath: "user/user.go", Summary: "查用户"},
	}
	seeds := funcsFromDocs(docs, 1)
	if len(seeds) != 1 {
		t.Fatalf("limit=1 时应仅1个种子，got %d", len(seeds))
	}
	seeds = funcsFromDocs(docs, 0)
	if len(seeds) != 2 {
		t.Fatalf("去重后应2个种子，got %d: %+v", len(seeds), seeds)
	}
}

// TestPropagateDirection 方向过滤：upstream 只返回上游，downstream 只返回下游。
func TestPropagateDirection(t *testing.T) {
	edges := []*model.FunctionCallEdge{
		{CallerModule: "order", CallerFunc: "CreateOrder", CalleeModule: "user", CalleeFunc: "GetUser", CallKind: model.CallKindCrossPackage},
	}
	g := buildCallGraph(edges)

	up := bfs(g, []string{"user.GetUser"}, 2, true)
	if len(up) != 1 || up[0].Func != "CreateOrder" {
		t.Errorf("上游应命中 CreateOrder，got %+v", up)
	}
	down := bfs(g, []string{"order.CreateOrder"}, 2, false)
	if len(down) != 1 || down[0].Func != "GetUser" {
		t.Errorf("下游应命中 GetUser，got %+v", down)
	}
}

// TestParseFuncChangesJSON 函数个性化变更记录 JSON 解析。
func TestParseFuncChangesJSON(t *testing.T) {
	raw := "```json\n{\"entries\":[{\"module\":\"order\",\"func\":\"CreateOrder\",\"change_summary\":\"新增用户校验\",\"business_impact\":\"影响下单\",\"attention\":\"回归下单\"}]}\n```"
	entries, err := parseFuncChangesJSON(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(entries) != 1 || entries[0].Func != "CreateOrder" || entries[0].ChangeSummary != "新增用户校验" {
		t.Fatalf("解析结果错误: %+v", entries)
	}
	if _, err := parseFuncChangesJSON(`{"entries":[]}`); err == nil {
		t.Fatal("空 entries 应报错")
	}
}

// TestLocalFuncChanges 本地兜底拼接：每个被修改函数生成互不相同的记录，含上下游信息。
func TestLocalFuncChanges(t *testing.T) {
	result := &ImpactAnalyzeResult{
		Changed: []*FuncRef{{Module: "order", Func: "CreateOrder", Summary: "创建订单"}, {Module: "user", Func: "GetUser"}},
		Reverse: []*FuncRef{{Module: "order", Func: "CreateOrder", Depth: 1, Edge: "order.CreateOrder 调用了 user.GetUser"}},
		Forward: []*FuncRef{{Module: "user", Func: "GetUser", Depth: 1, Edge: "order.CreateOrder 调用了 user.GetUser"}},
	}
	out := localFuncChanges(result)
	if len(out) != 2 {
		t.Fatalf("应生成2条记录，got %d", len(out))
	}
	if !strings.Contains(out[0].ChangeSummary, "order.CreateOrder") {
		t.Errorf("CreateOrder 记录应含自身，got %+v", out[0])
	}
	if !strings.Contains(out[1].BusinessImpact, "order.CreateOrder") {
		t.Errorf("GetUser 记录应含上游 order.CreateOrder，got %+v", out[1])
	}
	if out[0].ChangeSummary == out[1].ChangeSummary {
		t.Error("两条记录不应相同")
	}
}

// TestBuildFuncChangePrompt 函数个性化 Prompt 组装。
func TestBuildFuncChangePrompt(t *testing.T) {
	result := &ImpactAnalyzeResult{
		Changed: []*FuncRef{{Module: "user", Func: "GetUser", File: "user/user.go", Summary: "按ID查用户"}},
	}
	p := buildFuncChangePrompt(result)
	if !strings.Contains(p, "user.GetUser") || !strings.Contains(p, "user/user.go") {
		t.Fatalf("Prompt 缺失函数信息: %s", p)
	}
}
