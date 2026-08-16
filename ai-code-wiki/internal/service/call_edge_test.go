package service

import (
	"strings"
	"testing"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/astgo"
)

// buildGoItems 便捷构建 astgo.FuncItem（默认无导入）。
func buildGoItems(funcName string, simple, selector []string, imports map[string]string) astgo.FuncItem {
	return astgo.FuncItem{FuncName: funcName, Callee: selector, CalleeSimple: simple, Imports: imports}
}

// TestExtractCallEdges 表格驱动测试：调用边提取（同包/跨包/标准库跳过/方法调用跳过）。
func TestExtractCallEdges(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		items []astgo.FuncItem
		want  []*model.FunctionCallEdge
	}{
		{
			name: "跨包调用（命中导入）",
			file: "order/order.go",
			items: []astgo.FuncItem{
				buildGoItems("CreateOrder", nil, []string{"user.GetUser", "fmt.Println"}, map[string]string{"user": "user", "fmt": "fmt"}),
			},
			want: []*model.FunctionCallEdge{
				{RepoID: 1, CallerModule: "order", CallerFile: "order/order.go", CallerFunc: "CreateOrder", CalleeModule: "user", CalleeFunc: "GetUser", CallKind: model.CallKindCrossPackage},
			},
		},
		{
			name: "同包调用",
			file: "order/order.go",
			items: []astgo.FuncItem{
				buildGoItems("CreateOrder", []string{"ValidateOrder"}, nil, nil),
			},
			want: []*model.FunctionCallEdge{
				{RepoID: 1, CallerModule: "order", CallerFile: "order/order.go", CallerFunc: "CreateOrder", CalleeModule: "order", CalleeFunc: "ValidateOrder", CallKind: model.CallKindIntraPackage},
			},
		},
		{
			name: "标准库与局部变量方法调用跳过",
			file: "order/order.go",
			items: []astgo.FuncItem{
				buildGoItems("Pay", []string{"println"}, []string{"fmt.Println", "svc.Save()"}, map[string]string{"fmt": "fmt"}),
			},
			want: nil,
		},
		{
			name: "第三方库跨包调用保留",
			file: "order/order.go",
			items: []astgo.FuncItem{
				buildGoItems("Send", nil, []string{"notify.Notify"}, map[string]string{"notify": "github.com/foo/notify"}),
			},
			want: []*model.FunctionCallEdge{
				{RepoID: 1, CallerModule: "order", CallerFile: "order/order.go", CallerFunc: "Send", CalleeModule: "notify", CalleeFunc: "Notify", CallKind: model.CallKindCrossPackage},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractCallEdges(1, tc.file, tc.items)
			if len(got) != len(tc.want) {
				t.Fatalf("边数量不匹配: got %d want %d\n got=%+v", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				w := tc.want[i]
				g := got[i]
				if g.CallerFunc != w.CallerFunc || g.CallerModule != w.CallerModule ||
					g.CalleeFunc != w.CalleeFunc || g.CalleeModule != w.CalleeModule ||
					g.CallKind != w.CallKind {
					t.Errorf("第%d条边不匹配:\n got=%+v\nwant=%+v", i, g, w)
				}
			}
		})
	}
}

// TestImportModule 导入路径末段推导模块名。
func TestImportModule(t *testing.T) {
	if got := importModule("github.com/foo/order"); got != "order" {
		t.Errorf("importModule(github.com/foo/order) = %q, want order", got)
	}
	if got := importModule("user"); got != "user" {
		t.Errorf("importModule(user) = %q, want user", got)
	}
	if got := importModule("order/service"); got != "service" {
		t.Errorf("importModule(order/service) = %q, want service", got)
	}
}

// TestIsStdlibImport 标准库识别。
func TestIsStdlibImport(t *testing.T) {
	if !isStdlibImport("fmt") || !isStdlibImport("net/http") || isStdlibImport("github.com/foo/bar") {
		t.Fatal("isStdlibImport 判断错误")
	}
}

// TestSplitSelector 限定名拆分。
func TestSplitSelector(t *testing.T) {
	alias, rest := splitSelector("user.GetUser")
	if alias != "user" || rest != "GetUser" {
		t.Fatalf("splitSelector(user.GetUser) = %q,%q", alias, rest)
	}
	alias, rest = splitSelector("a.b.Func")
	if alias != "a" || rest != "b.Func" {
		t.Fatalf("splitSelector(a.b.Func) = %q,%q", alias, rest)
	}
	if !strings.Contains("x", "x") {
		t.Fatal("unreachable")
	}
}
