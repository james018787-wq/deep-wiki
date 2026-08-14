package filefilter

import "testing"

func TestShouldSkip(t *testing.T) {
	f := New(Config{})

	cases := []struct {
		name string
		path string
		skip bool
	}{
		{"Go 业务文件", "internal/service/doc.go", false},
		{"PHP 业务文件", "app/controller/Order.php", false},
		{"test 测试文件", "pkg/user/user_test.go", true},
		{"test 测试文件（子目录）", "internal/service/xxx_test.go", true},
		{"vendor 目录", "vendor/golang.org/x/net/http.go", true},
		{"node_modules 目录", "web/node_modules/foo/index.js", true},
		{"mock 目录", "test/mock/handler_mock.go", true},
		{"fixture 目录", "test/fixture/data.go", true},
		{"非业务代码后缀", "README.md", true},
		{"无后缀文件", "Dockerfile", true},
		{"资源文件", "web/static/app.js", true},
		{"空路径", "", true},
	}

	for _, c := range cases {
		got, reason := f.ShouldSkip(c.path)
		if got != c.skip {
			t.Errorf("%s: ShouldSkip(%q) = %v(%q), want %v", c.name, c.path, got, reason, c.skip)
		}
	}
}

func TestShouldSkipCustomConfig(t *testing.T) {
	f := New(Config{
		IgnoreDirs:   []string{"third_party", "generated"},
		IgnoreFileRe: []string{`_test\.go$`, `^test/`},
		AllowExts:    []string{"go", "php", "py"},
	})

	cases := []struct {
		path string
		skip bool
	}{
		{"third_party/gen/gen.go", true},
		{"internal/foo/generated/xxx.go", true},
		{"test/setup.go", true},
		{"internal/foo/helper_test.go", true},
		{"scripts/deploy.py", false},
		{"app/controller/Order.php", false},
		{"app/controller/Order.java", true},
	}

	for _, c := range cases {
		got, _ := f.ShouldSkip(c.path)
		if got != c.skip {
			t.Errorf("ShouldSkip(%q) = %v, want %v", c.path, got, c.skip)
		}
	}
}

func TestShouldSkipReason(t *testing.T) {
	f := New(Config{})

	if skip, reason := f.ShouldSkip("vendor/foo/bar.go"); !skip || reason == "" {
		t.Errorf("vendor 文件应跳过且给出原因: skip=%v reason=%q", skip, reason)
	}
	if skip, reason := f.ShouldSkip("pkg/user/user_test.go"); !skip || reason == "" {
		t.Errorf("test 文件应跳过且给出原因: skip=%v reason=%q", skip, reason)
	}
	if skip, reason := f.ShouldSkip("README.md"); !skip || reason == "" {
		t.Errorf("非业务后缀应跳过且给出原因: skip=%v reason=%q", skip, reason)
	}
}

func TestSplitList(t *testing.T) {
	if got := SplitList("vendor,node_modules, mock"); len(got) != 3 || got[2] != "mock" {
		t.Errorf("SplitList 解析失败: %v", got)
	}
	if got := SplitList(""); len(got) != 0 {
		t.Errorf("空串应返回空列表: %v", got)
	}
	if got := SplitList("a,,b"); len(got) != 2 {
		t.Errorf("空项应被忽略: %v", got)
	}
}

func TestNewCustomWithCommaExt(t *testing.T) {
	f := New(Config{AllowExts: []string{".Go"}})
	if _, ok := f.allowExts["go"]; !ok {
		t.Errorf("带点后缀应归一化处理: %v", f.allowExts)
	}
}
