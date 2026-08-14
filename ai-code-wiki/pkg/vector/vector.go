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
	ModuleName string   // 所属模块
	FilePath   string   // 文件路径
	FuncName   string   // 函数名称
	Content    string   // 向量化文本内容（摘要+流程+风险点等）
	Metadata   []string // 附加元数据（模块、标签等，用于过滤）
}
