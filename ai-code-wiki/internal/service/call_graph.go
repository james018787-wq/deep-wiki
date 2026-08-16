package service

import (
	"errors"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/common"

	"gorm.io/gorm"
)

// ============ 函数调用图（D3 可视化） ============

// GraphNode 调用图节点。
type GraphNode struct {
	ID     string `json:"id"`     // module.func
	Module string `json:"module"` // 业务模块
	Func   string `json:"func"`   // 函数名
	File   string `json:"file"`   // 文件路径
	Kind   string `json:"kind"`   // 节点类型：self/changed/reverse/forward/caller/callee
	Depth  int    `json:"depth"`  // 影响传播深度（仅影响分析图使用）
	DocID  int64  `json:"doc_id"` // 命中 Wiki 文档时的主键（可跳转源码）
}

// GraphLink 调用边（有向：Source 调用 Target）。
type GraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// CallGraphData 调用图数据（供前端 D3 force 图渲染）。
type CallGraphData struct {
	Nodes []*GraphNode `json:"nodes"`
	Links []*GraphLink `json:"links"`
}

// ============ 影响分析增强 ============

// APISchemaChange 接口/API 签名变更（破坏性变更检测）。
type APISchemaChange struct {
	Module     string `json:"module"`      // 业务模块
	Func       string `json:"func"`        // 函数名
	File       string `json:"file"`        // 文件路径
	ChangeType string `json:"change_type"` // 变更类型：added / modified（签名变化）/ removed
	Old        string `json:"old"`         // 旧签名（modified 时展示）
	New        string `json:"new"`         // 新签名（modified 时展示）
}

// DBSchemaChange 数据库表结构变更。
type DBSchemaChange struct {
	File            string   `json:"file"`             // 变更 SQL 文件
	ChangeType      string   `json:"change_type"`      // create / alter / drop / rename
	Tables          []string `json:"tables"`           // 涉及的表名
	AffectedModules []string `json:"affected_modules"` // 业务文档中引用到该表的模块（best-effort）
}

// TestFileRef 建议回归测试文件（引用受影响函数的测试用例）。
type TestFileRef struct {
	File  string   `json:"file"`  // 测试文件路径
	Funcs []string `json:"funcs"` // 命中的受影响函数
}

// GetDocGraph 查询单篇文档对应函数的调用图（上游调用方 + 下游被调用方 + 自身）。
// 供文档详情页 D3 可视化。
func (s *DocService) GetDocGraph(docID int64) (*CallGraphData, error) {
	doc, err := s.docRepo.GetByID(docID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "文档不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询文档失败", err)
	}

	selfKey := funcKey(doc.ModuleName, doc.FuncName)
	g := &CallGraphData{
		Nodes: []*GraphNode{{
			ID: selfKey, Module: doc.ModuleName, Func: doc.FuncName,
			File: doc.FilePath, Kind: "self", DocID: doc.ID,
		}},
	}

	callers, err := s.callEdge.ListByCallee(doc.RepoID, doc.ModuleName, doc.FuncName)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询上游调用方失败", err)
	}
	callees, err := s.callEdge.ListByCaller(doc.RepoID, doc.ModuleName, doc.FuncName)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询下游被调用方失败", err)
	}

	seen := map[string]struct{}{selfKey: {}}
	addNode := func(module, fn, file, kind string) {
		key := funcKey(module, fn)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		g.Nodes = append(g.Nodes, &GraphNode{
			ID: key, Module: module, Func: fn, File: file, Kind: kind,
		})
	}
	for _, e := range callers {
		addNode(e.CallerModule, e.CallerFunc, e.CallerFile, "caller")
		g.Links = append(g.Links, &GraphLink{Source: funcKey(e.CallerModule, e.CallerFunc), Target: selfKey})
	}
	for _, e := range callees {
		addNode(e.CalleeModule, e.CalleeFunc, e.CalleeFile, "callee")
		g.Links = append(g.Links, &GraphLink{Source: selfKey, Target: funcKey(e.CalleeModule, e.CalleeFunc)})
	}
	return g, nil
}

// buildImpactGraph 从影响分析结果 + 全量调用边构建 D3 图数据。
// 仅保留两端都在受影响函数集合内的边（直接修改 / 上游 / 下游）。
func buildImpactGraph(result *ImpactAnalyzeResult, edges []*model.FunctionCallEdge, docMap map[string]*model.CodeFunctionDoc) *CallGraphData {
	involved := make(map[string]*GraphNode)
	add := func(module, fn, file, kind string, depth int) {
		key := funcKey(module, fn)
		if _, ok := involved[key]; ok {
			return
		}
		node := &GraphNode{ID: key, Module: module, Func: fn, File: file, Kind: kind, Depth: depth}
		if d, ok := docMap[key]; ok {
			node.DocID = d.ID
		}
		involved[key] = node
	}
	for _, f := range result.Changed {
		add(f.Module, f.Func, f.File, "changed", f.Depth)
	}
	for _, f := range result.Reverse {
		add(f.Module, f.Func, f.File, "reverse", f.Depth)
	}
	for _, f := range result.Forward {
		add(f.Module, f.Func, f.File, "forward", f.Depth)
	}

	g := &CallGraphData{Nodes: make([]*GraphNode, 0, len(involved))}
	for _, n := range involved {
		g.Nodes = append(g.Nodes, n)
	}
	for _, e := range edges {
		from := funcKey(e.CallerModule, e.CallerFunc)
		to := funcKey(e.CalleeModule, e.CalleeFunc)
		if _, ok := involved[from]; !ok {
			continue
		}
		if _, ok := involved[to]; !ok {
			continue
		}
		g.Links = append(g.Links, &GraphLink{Source: from, Target: to})
	}
	return g
}
