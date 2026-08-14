package astgo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// FuncItem 单个 Go 函数的最小解析切片。
type FuncItem struct {
	FuncName string   // 函数名称
	Code     string   // 函数源码片段（完整函数体）
	Callee   []string // 调用函数列表（SelectorExpr 形式，如 http.Get）
}

// ParseGoFile 解析 Go 源码字符串，提取全部函数声明。
//
// 规则：
//  1. 使用 go/ast 解析，遍历提取全部 FuncDecl（含方法）。
//  2. 在函数体内遍历 CallExpr，仅收集 SelectorExpr 形式的函数调用
//     （如 pkg.Func()、a.b.Func()），存入 Callee。
func ParseGoFile(fileContent string) ([]FuncItem, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", fileContent, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析go源码失败: %w", err)
	}

	var items []FuncItem
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		items = append(items, FuncItem{
			FuncName: fn.Name.Name,
			Code:     extractSource(fset, fileContent, fn),
			Callee:   collectCallee(fn.Body),
		})
	}
	return items, nil
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

// collectCallee 遍历函数体，收集 SelectorExpr 形式的函数调用。
func collectCallee(body *ast.BlockStmt) []string {
	if body == nil {
		return nil
	}
	var callee []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			callee = append(callee, exprString(sel))
		}
		return true
	})
	return callee
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