// Package vector 向量库通用接口，屏蔽底层引擎差异。
// 支撑跨模块 RAG 检索与文档内容同步。
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

// Client 向量库通用接口。
type Client interface {
	// Upsert 写入/更新向量文档。
	// 人工修改/重置文档后必须同步调用，保证检索使用最新校正内容。
	Upsert(doc *DocVector) error

	// Delete 删除向量文档。
	Delete(docID int64) error

	// Search 向量相似度检索，返回按相关性排序的文档。
	Search(query string, module string, limit int) ([]*DocVector, error)
}

// New 根据配置创建向量库客户端实例。
// 具体引擎（redis / milvus / faiss 等）后续按需实现。
func New(engine, host string, port int, collection string) (Client, error) {
	// TODO(骨架)：按 engine 分发到对应实现，当前返回未实现错误。
	_ = engine
	_ = host
	_ = port
	_ = collection
	return nil, ErrNotImplemented
}
