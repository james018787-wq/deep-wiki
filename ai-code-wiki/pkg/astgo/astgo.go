// Package astgo Go 语言 AST 解析工具。
// 用于从 Go 源码中提取函数级信息，最小切片单元为单个函数。
package astgo

// FuncInfo Go 函数 AST 解析结果。
type FuncInfo struct {
	FilePath     string   // 源码文件路径
	FuncName     string   // 函数名称
	SourceCode   string   // 函数源码片段（完整函数体）
	Params       []string // 入参列表
	Returns      []string // 返回值列表
	CalledFuncs  []string // 调用函数列表
	RelyModules  []string // 依赖模块列表（跨模块调用）
}

// ParseFile 解析单个 Go 文件，提取全部函数信息。
func ParseFile(filePath string) ([]*FuncInfo, error) {
	// TODO(骨架)：基于 go/ast + go/parser 实现函数提取，后续实现。
	_ = filePath
	return nil, nil
}

// ParseDir 解析目录下所有 Go 文件。
func ParseDir(dir string) ([]*FuncInfo, error) {
	// TODO(骨架)：遍历目录调用 ParseFile 聚合结果，后续实现。
	_ = dir
	return nil, nil
}