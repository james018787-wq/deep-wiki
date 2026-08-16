package astgo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// FuncItem 单个 Go 函数的最小解析切片。
type FuncItem struct {
	FuncName     string            // 函数名称
	Code         string            // 函数源码片段（完整函数体）
	Callee       []string          // 调用函数列表（SelectorExpr 形式，如 http.Get、user.GetUser）
	CalleeSimple []string          // 简单标识符调用列表（同包函数调用，如 ValidateOrder）
	PackageName  string            // 函数所在包名（package xxx）
	Imports      map[string]string // 文件导入表：导入别名 -> 导入路径
}

// ParseGoFile 解析 Go 源码字符串，提取全部函数声明。
//
// 规则：
//  1. 使用 go/ast 解析，遍历提取全部 FuncDecl（含方法）。
//  2. 在函数体内遍历 CallExpr，收集函数调用：
//     - SelectorExpr 形式（pkg.Func()、a.b.Func()）存入 Callee；
//     - 简单标识符形式（ValidateOrder()，同包函数）存入 CalleeSimple。
//  3. 记录函数所在包名与文件导入表，供调用边分类（同包/跨包）使用。
func ParseGoFile(fileContent string) ([]FuncItem, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", fileContent, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析go源码失败: %w", err)
	}

	imports := fileImports(file)
	var items []FuncItem
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		simple, selector := collectCallees(fn.Body)
		items = append(items, FuncItem{
			FuncName:     fn.Name.Name,
			Code:         extractSource(fset, fileContent, fn),
			Callee:       selector,
			CalleeSimple: simple,
			PackageName:  file.Name.Name,
			Imports:      imports,
		})
	}
	return items, nil
}

// fileImports 提取文件导入表：别名（未显式别名时取路径末段）-> 导入路径。
func fileImports(file *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		path := strings.Trim(imp.Path.Value, `"`)
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
			if name == "_" || name == "." {
				continue
			}
		}
		if name == "" {
			segs := strings.Split(path, "/")
			name = segs[len(segs)-1]
		}
		imports[name] = path
	}
	return imports
}

// collectCallees 遍历函数体，分离收集函数调用：
//   - simple: CallExpr.Fun 为 *ast.Ident（同包函数，如 ValidateOrder()）
//   - selector: CallExpr.Fun 为 *ast.SelectorExpr（pkg.Func()、a.b.Func()）
func collectCallees(body *ast.BlockStmt) (simple, selector []string) {
	if body == nil {
		return nil, nil
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			// 排除包级调用如 make/len/append 等内建函数（无法作为业务调用边）
			if !IsBuiltin(f.Name) {
				simple = append(simple, f.Name)
			}
		case *ast.SelectorExpr:
			selector = append(selector, exprString(f))
		}
		return true
	})
	return simple, selector
}

// IsBuiltin 判断是否为 Go 内建函数（无法解析为业务函数调用边）。
func IsBuiltin(name string) bool {
	switch name {
	case "make", "len", "cap", "append", "copy", "delete", "new", "panic", "recover",
		"close", "complex", "real", "imag", "min", "max", "clear", "print", "println":
		return true
	}
	return false
}

// extractSource 根据位置信息从原始源码中截取函数源码片段。
func extractSource(fset *token.FileSet, fileContent string, fn *ast.FuncDecl) string {
	start := fset.Position(fn.Pos()).Offset
	end := fset.Position(fn.End()).Offset
	if start >= 0 && end > start && end <= len(fileContent) {
		return fileContent[start:end]
	}
	return ""
}

// exprString 还原表达式源码字符串，支持多级 SelectorExpr 与泛型索引。
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.IndexExpr: // 泛型调用，如 pkg.Func[int]()
		return exprString(v.X)
	case *ast.Ident:
		return v.Name
	default:
		return ""
	}
}
