package astphp

import (
	"strings"
	"testing"
)

// names 提取全部函数名，便于断言。
func names(items []FuncItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.FuncName)
	}
	return out
}

// TestParsePHPFile 表格驱动：验证函数名提取、源码片段、边界规避。
func TestParsePHPFile(t *testing.T) {
	cases := []struct {
		name   string   // 用例名
		src    string   // PHP 源码
		want   []string // 期望函数名（有序）
		codeIn string   // 期望首个函数片段包含的内容（空则跳过断言）
	}{
		{
			name: "简单函数",
			src: `<?php
function foo() {
    return 1;
}
`,
			want:   []string{"foo"},
			codeIn: "function foo() {\n    return 1;\n}",
		},
		{
			name: "多行参数函数定义",
			src: `<?php
function calc(
    $a,
    $b
) {
    return $a + $b;
}
`,
			want:   []string{"calc"},
			codeIn: "function calc(",
		},
		{
			name: "注释内函数不提取-行注释",
			src: `<?php
// function hidden() { return 1; }
function visible() { return 2; }
`,
			want:   []string{"visible"},
			codeIn: "",
		},
		{
			name: "注释内函数不提取-块注释",
			src: `<?php
/*
 * function hidden2() { return 1; }
 */
function visible2() { return 2; }
`,
			want:   []string{"visible2"},
			codeIn: "",
		},
		{
			name: "注释内函数不提取-hash注释",
			src: `<?php
# function hidden3() { return 1; }
function visible3() { return 2; }
`,
			want:   []string{"visible3"},
			codeIn: "",
		},
		{
			name: "字符串内函数文本不提取",
			src: `<?php
$s = "function fake() { return 1; }";
function real() { return $s; }
`,
			want:   []string{"real"},
			codeIn: "",
		},
		{
			name: "类方法",
			src: `<?php
class Order {
    public function create() {
        return $this->save();
    }
}
`,
			want:   []string{"create"},
			codeIn: "function create()",
		},
		{
			name: "引用返回函数",
			src: `<?php
function &getRef() {
    return $x;
}
`,
			want:   []string{"getRef"},
			codeIn: "function &getRef()",
		},
		{
			name: "带返回类型函数",
			src: `<?php
function typed(): array {
    return [];
}
`,
			want:   []string{"typed"},
			codeIn: "function typed(): array {",
		},
		{
			name: "匿名函数不提取",
			src: `<?php
$r = array_map(function ($x) { return $x * 2; }, [1, 2]);
function named() { return $r; }
`,
			want:   []string{"named"},
			codeIn: "",
		},
		{
			name: "函数体内字符串含花括号不影响配平",
			src: `<?php
function withStr() {
    $s = "{not a real brace}";
    return $s;
}
`,
			want:   []string{"withStr"},
			codeIn: "function withStr() {\n    $s = \"{not a real brace}\";\n    return $s;\n}",
		},
		{
			name: "默认数组参数",
			src: `<?php
function def($arr = [1, 2]) {
    return $arr;
}
`,
			want:   []string{"def"},
			codeIn: "function def($arr = [1, 2]) {",
		},
		{
			name: "多个函数依次提取",
			src: `<?php
function a() {
}
function b() {
    return 2;
}
`,
			want:   []string{"a", "b"},
			codeIn: "",
		},
		{
			name: "空源码",
			src:  "",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePHPFile(tc.src)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			gotNames := names(got)
			if len(gotNames) != len(tc.want) {
				t.Fatalf("函数数量不匹配: got %v want %v", gotNames, tc.want)
			}
			for i := range tc.want {
				if gotNames[i] != tc.want[i] {
					t.Fatalf("函数名不匹配: got %v want %v", gotNames, tc.want)
				}
			}
			if tc.codeIn != "" {
				if len(got) == 0 || !strings.Contains(got[0].Code, tc.codeIn) {
					t.Fatalf("函数片段不匹配:\n got: %q\nwant contain: %q", got[0].Code, tc.codeIn)
				}
			}
		})
	}
}

// TestParsePHPFileStartLine 验证 PHP 函数起始行号捕获（答案引用定位）。
func TestParsePHPFileStartLine(t *testing.T) {
	src := `<?php

// 注释行
function foo() {
    return 1;
}

function bar() { return 2; }
`
	items, err := ParsePHPFile(src)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 个函数, got %d", len(items))
	}
	if items[0].FuncName != "foo" || items[0].StartLine != 4 {
		t.Fatalf("foo 行号不匹配: got %+v", items[0])
	}
	if items[1].FuncName != "bar" || items[1].StartLine != 8 {
		t.Fatalf("bar 行号不匹配: got %+v", items[1])
	}
}
