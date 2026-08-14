// Package filefilter 解析流水线文件过滤规则。
//
// 用于在增量解析流水线中过滤非业务代码文件，命中规则的文件直接跳过
// AST 解析、不生成文档。规则支持三类：
//   - 允许后缀（非业务代码后缀跳过）；
//   - 忽略目录（路径任意层级命中即跳过，如 vendor / node_modules）；
//   - 忽略文件正则（匹配相对路径，如 _test.go）。
//
// 规则通过环境变量配置（见 internal/config，FILTER_* 前缀），默认值覆盖
// 常见测试 / 第三方依赖 / 测试数据目录。
package filefilter

import (
	"fmt"
	"regexp"
	"strings"
)

// 默认规则：忽略第三方依赖与测试数据目录。
var defaultIgnoreDirs = []string{"vendor", "node_modules", "mock", "fixture"}

// 默认规则：忽略测试文件（Go 单测 *_test.go）。
var defaultIgnoreFileRes = []string{`_test\.go$`}

// 默认规则：允许解析的业务代码后缀（不含点，小写）。
var defaultAllowExts = []string{"go", "php"}

// Config 过滤规则配置（由环境变量解析而来，字段为空时回退到默认值）。
type Config struct {
	IgnoreDirs   []string // 忽略目录名（FILTER_IGNORE_DIRS，逗号分隔）
	IgnoreFileRe []string // 忽略文件正则（FILTER_IGNORE_FILE_REGEX，逗号分隔，匹配相对路径）
	AllowExts    []string // 允许的代码文件后缀（FILTER_ALLOW_EXTS，逗号分隔，不含点）
}

// FileFilter 解析流水线文件过滤器。
type FileFilter struct {
	ignoreDirs map[string]struct{}
	fileRes    []*regexp.Regexp
	allowExts  map[string]struct{}
}

// SplitList 逗号分隔字符串转列表（去空白，空串返回 nil）。
func SplitList(s string) []string {
	parts := strings.Split(s, ",")
	list := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			list = append(list, p)
		}
	}
	return list
}

// New 构建过滤器，空配置项回退到默认规则。
func New(cfg Config) *FileFilter {
	f := &FileFilter{
		ignoreDirs: make(map[string]struct{}),
		allowExts:  make(map[string]struct{}),
	}

	dirs := cfg.IgnoreDirs
	if len(dirs) == 0 {
		dirs = defaultIgnoreDirs
	}
	for _, d := range dirs {
		if d = strings.Trim(d, " /"); d != "" {
			f.ignoreDirs[d] = struct{}{}
		}
	}

	res := cfg.IgnoreFileRe
	if len(res) == 0 {
		res = defaultIgnoreFileRes
	}
	for _, re := range res {
		if re = strings.TrimSpace(re); re == "" {
			continue
		}
		if r, err := regexp.Compile(re); err == nil {
			f.fileRes = append(f.fileRes, r)
		}
	}

	exts := cfg.AllowExts
	if len(exts) == 0 {
		exts = defaultAllowExts
	}
	for _, e := range exts {
		if e = strings.TrimPrefix(strings.TrimSpace(e), "."); e != "" {
			f.allowExts[strings.ToLower(e)] = struct{}{}
		}
	}
	return f
}

// ShouldSkip 判断文件是否应被跳过（命中任一规则即跳过）。
// 跳过时返回 (true, 原因)；未命中返回 (false, "")。
func (f *FileFilter) ShouldSkip(path string) (bool, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return true, "空路径"
	}

	// 1. 非业务代码后缀
	ext := extOf(path)
	if _, ok := f.allowExts[ext]; !ok {
		return true, fmt.Sprintf("非业务代码后缀 .%s", ext)
	}

	// 2. 忽略目录（路径任意层级命中即跳过，git diff 路径统一为 / 分隔）
	for _, seg := range strings.Split(path, "/") {
		if _, ok := f.ignoreDirs[seg]; ok {
			return true, fmt.Sprintf("依赖/测试数据目录 %s", seg)
		}
	}

	// 3. 忽略文件正则（匹配相对路径）
	for _, re := range f.fileRes {
		if re.MatchString(path) {
			return true, fmt.Sprintf("匹配忽略正则 %s", re.String())
		}
	}

	return false, ""
}

// extOf 提取文件小写后缀（不含点），无后缀返回空串。
func extOf(path string) string {
	i := strings.LastIndex(path, ".")
	if i < 0 || i == len(path)-1 {
		return ""
	}
	return strings.ToLower(path[i+1:])
}
