// Package vector 向量库抽象接口层。
// 通过 VectorClient 统一屏蔽底层向量引擎差异（Chroma / Milvus），
// 业务代码只依赖该抽象，不感知底层引擎。
package vector

import (
	"errors"
	"fmt"
	"strings"
)

// VectorClient 向量存储抽象接口。
// 业务代码（doc_service / task_service / search_service）只依赖本接口，
// 通过 NewVectorClient 按 VECTOR_DRIVER 选择具体实现。
type VectorClient interface {
	// UpsertDoc 写入向量。
	// 文档不存在则新增，已存在则覆盖（幂等语义）。
	UpsertDoc(doc *DocVector) error

	// UpdateDoc 更新向量。
	// 语义为更新已有文档向量（Chroma 内部即 upsert；Milvus 按主键 upsert）。
	UpdateDoc(doc *DocVector) error

	// DeleteDoc 删除向量。
	DeleteDoc(docID int64) error

	// SearchQuery 向量相似度检索。
	// queryVector 为经 EmbedText 生成的查询向量，返回按相关性排序的候选 doc_id。
	// filter 非 nil 时在向量侧按元数据过滤（如 repo_id / module），提升函数级检索精度。
	SearchQuery(queryVector []float64, limit int, filter *SearchFilter) ([]int64, error)
}

// Options VectorClient 构建参数。
// 字段由内部/config 配置转换而来，具体映射见 config/config.yaml 与环境变量。
type Options struct {
	Driver string // 向量引擎驱动：chroma / milvus（VECTOR_DRIVER，默认 chroma）

	// ---- Chroma 连接参数 ----
	ChromaURL string // chroma HTTP 地址，如 http://chroma:8000（CHROMA_URL）

	// ---- 通用参数 ----
	Collection   string // 向量集合名（CHROMA_COLLECTION / MILVUS_COLLECTION）
	EmbedBaseURL string // LLM 服务地址（LLM_SERVICE_URL），用于文本转向量 embedding

	// ---- Milvus 连接参数 ----
	MilvusHost     string // Milvus 服务地址（MILVUS_HOST，如 127.0.0.1 或 milvus）
	MilvusPort     int    // Milvus 端口（MILVUS_PORT，默认 19530）
	MilvusDim      int    // embedding 向量维度（MILVUS_DIM，需与 embedding 服务输出一致）
	MilvusUser     string // Milvus 用户名（MILVUS_USER，可选，开启鉴权时必填）
	MilvusPassword string // Milvus 密码（MILVUS_PASSWORD，可选）
}

// NewVectorClient 根据 driver 构建对应向量引擎实现。
// 未知 driver 或关键连接参数缺失时返回错误，由调用方决定降级处理。
func NewVectorClient(opts Options) (VectorClient, error) {
	switch strings.ToLower(opts.Driver) {
	case "", "chroma":
		if opts.ChromaURL == "" || opts.Collection == "" {
			return nil, errors.New("向量引擎 chroma 未配置：需要 CHROMA_URL 与集合名")
		}
		return NewChromaClient(opts), nil
	case "milvus":
		if opts.MilvusHost == "" || opts.MilvusPort <= 0 {
			return nil, errors.New("向量引擎 milvus 未配置：需要 MILVUS_HOST / MILVUS_PORT")
		}
		if opts.MilvusDim <= 0 {
			return nil, errors.New("向量引擎 milvus 未配置：需要 MILVUS_DIM（embedding 维度）")
		}
		return NewMilvusClient(opts), nil
	default:
		return nil, fmt.Errorf("不支持的向量引擎 driver=%q（仅支持 chroma / milvus）", opts.Driver)
	}
}
