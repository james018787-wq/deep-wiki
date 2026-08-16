// Package vector 向量存储抽象接口与文档模型。
// 支撑跨模块 RAG 检索与文档内容同步，屏蔽底层引擎（Chroma / Milvus）差异。
//
// 业务代码只依赖 vector_interface.go 中的 VectorClient 抽象接口，
// 通过 NewVectorClient 按 VECTOR_DRIVER 选择具体实现。
package vector

// DocVector 向量文档模型。
// 最小切片单元为单个函数文档。
type DocVector struct {
	DocID      int64    // 关联 code_function_doc 主键
	RepoID     int64    // 所属仓库id（向量侧过滤，检索按仓库隔离）
	RepoName   string   // 所属仓库名（多仓库检索隔离用）
	ModuleName string   // 所属模块
	FilePath   string   // 文件路径
	FuncName   string   // 函数名称
	FuncLine   int      // 函数起始行号
	Content    string   // 向量化文本内容（摘要+流程+风险点+入参+出参等）
	Metadata   []string // 附加元数据（模块、标签等，用于过滤）
}

// SearchFilter 向量检索过滤条件（函数级精度：按仓库/模块在向量侧收敛候选）。
type SearchFilter struct {
	RepoID int64  // >0 时仅检索该仓库（元数据 repo_id 过滤）
	Module string // 非空时仅检索该模块（元数据 module_name 过滤）
}
