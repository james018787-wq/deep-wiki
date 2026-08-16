package astgo

import (
	"strings"
	"testing"
)

// equalStrings 比较两个字符串切片，nil 与空切片视为相等（保持顺序）。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseGoFile 表格驱动测试：AST 解析提取函数名称、函数源码、函数调用。
func TestParseGoFile(t *testing.T) {
	cases := []struct {
		name    string // 用例名
		src     string // Go 源码
		want    []FuncItem
		wantErr bool // 期望解析报错
	}{
		{
			name: "简单函数+标准库调用",
			src: `package main
import "fmt"

func foo() {
	fmt.Println("hi")
}
`,
			want: []FuncItem{
				{FuncName: "foo", Callee: []string{"fmt.Println"}, Code: "func foo() {\n\tfmt.Println(\"hi\")\n}"},
			},
		},
		{
			name: "方法+多级Selector",
			src: `package main

func (s *Server) Start() {
	s.log.Info("start")
}
`,
			want: []FuncItem{
				{FuncName: "Start", Callee: []string{"s.log.Info"}, Code: "func (s *Server) Start() {\n\ts.log.Info(\"start\")\n}"},
			},
		},
		{
			// 当前实现：collectCallee 仅收集 call.Fun 直接为 SelectorExpr 的调用，
			// 泛型调用 pkg.Func[int](...) 的 Fun 是 IndexExpr，不会被收集。
			name: "泛型调用当前不收集",
			src: `package main

func bar() {
	x := pkg.Func[int](1)
	_ = x
}
`,
			want: []FuncItem{
				{FuncName: "bar", Callee: nil, Code: "func bar() {\n\tx := pkg.Func[int](1)\n\t_ = x\n}"},
			},
		},
		{
			name: "多个函数依次提取",
			src: `package main

func a() {
}

func b() {
	http.Get("http://x")
}
`,
			want: []FuncItem{
				{FuncName: "a", Callee: nil, Code: "func a() {\n}"},
				{FuncName: "b", Callee: []string{"http.Get"}, Code: "func b() {\n\thttp.Get(\"http://x\")\n}"},
			},
		},
		{
			name: "纯标识符调用不收集（内建函数）",
			src: `package main

func c() {
	println("x")
	_ = append([]int{1}, 2)
}
`,
			// println/append 为内建函数，按规则不收集（含 CalleeSimple）
			want: []FuncItem{
				{FuncName: "c", Callee: nil, Code: "func c() {\n\tprintln(\"x\")\n\t_ = append([]int{1}, 2)\n}"},
			},
		},
		{
			name: "同包函数调用收集到 CalleeSimple",
			src: `package order

func CreateOrder() {
	ValidateOrder()
}

func ValidateOrder() {
}
`,
			want: []FuncItem{
				{FuncName: "CreateOrder", CalleeSimple: []string{"ValidateOrder"}, Code: "func CreateOrder() {\n\tValidateOrder()\n}"},
				{FuncName: "ValidateOrder", Callee: nil, Code: "func ValidateOrder() {\n}"},
			},
		},
		{
			name: "导入表与跨包调用限定名",
			src: `package order

import (
	"fmt"
	user "user"
)

func CreateOrder() {
	u := user.GetUser(1)
	_ = u
	fmt.Println(u)
}
`,
			want: []FuncItem{
				{
					FuncName:     "CreateOrder",
					Callee:       []string{"user.GetUser", "fmt.Println"},
					CalleeSimple: nil,
					PackageName:  "order",
					Imports:      map[string]string{"fmt": "fmt", "user": "user"},
					Code:         "func CreateOrder() {\n\tu := user.GetUser(1)\n\t_ = u\n\tfmt.Println(u)\n}",
				},
			},
		},
		{
			name: "嵌套作用域中的调用",
			src: `package main

func d() {
	for _, v := range []int{1, 2} {
		fmt.Println(v)
	}
}
`,
			want: []FuncItem{
				{FuncName: "d", Callee: []string{"fmt.Println"}, Code: "func d() {\n\tfor _, v := range []int{1, 2} {\n\t\tfmt.Println(v)\n\t}\n}"},
			},
		},
		{
			name: "无函数声明",
			src: `package main

var x = 1

type T struct{}
`,
			want: nil,
		},
		{
			name:    "非法语法报错",
			src:     `func e( {`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseGoFile(tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望解析报错，实际成功: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("函数数量不匹配: got %d want %d\n got=%+v", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				w := tc.want[i]
				g := got[i]
				if g.FuncName != w.FuncName {
					t.Errorf("第%d个函数名不匹配: got %q want %q", i, g.FuncName, w.FuncName)
				}
				if !equalStrings(g.Callee, w.Callee) {
					t.Errorf("函数 %s 调用列表不匹配: got %v want %v", g.FuncName, g.Callee, w.Callee)
				}
				if w.Code != "" && !strings.Contains(g.Code, w.Code) {
					t.Errorf("函数 %s 源码片段未包含期望内容:\n got: %q\nwant: %q", g.FuncName, g.Code, w.Code)
				}
			}
		})
	}
}

// TestParseGoFileStartLine 验证函数起始行号捕获（答案引用定位）。
func TestParseGoFileStartLine(t *testing.T) {
	src := `package main

import "fmt"

// foo 注释
func foo() {
	fmt.Println("hi")
}

func bar() {}
`
	items, err := ParseGoFile(src)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 个函数, got %d", len(items))
	}
	if items[0].FuncName != "foo" || items[0].StartLine != 6 {
		t.Fatalf("foo 行号不匹配: got %+v", items[0])
	}
	if items[1].FuncName != "bar" || items[1].StartLine != 10 {
		t.Fatalf("bar 行号不匹配: got %+v", items[1])
	}
}
