package vector

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ChromaClient 基于 Chroma 的 VectorClient 实现。
// 写入/更新复用原有向量化服务（Python LLM 微服务）的 upsert 逻辑，
// 检索复用原有 chroma HTTP 查询逻辑，删除按约定（id=doc_id 字符串）调用 chroma HTTP delete 接口。
type ChromaClient struct {
	chromaURL    string // chroma HTTP 地址
	collection   string // 集合名
	embedBaseURL string // LLM 服务地址（embedding + chroma upsert）
}

// NewChromaClient 构建 Chroma 实现。
func NewChromaClient(opts Options) *ChromaClient {
	return &ChromaClient{
		chromaURL:    strings.TrimRight(opts.ChromaURL, "/"),
		collection:   opts.Collection,
		embedBaseURL: strings.TrimRight(opts.EmbedBaseURL, "/"),
	}
}

// UpsertDoc 写入/覆盖向量，复用原有 pkg/vector.UpdateDocEmbedding 逻辑。
func (c *ChromaClient) UpsertDoc(doc *DocVector) error {
	return UpdateDocEmbedding(c.embedBaseURL, doc)
}

// UpdateDoc 更新向量，Chroma upsert 语义天然覆盖新增与更新。
func (c *ChromaClient) UpdateDoc(doc *DocVector) error {
	return UpdateDocEmbedding(c.embedBaseURL, doc)
}

// DeleteDoc 按 doc_id 删除向量记录。
// chroma delete 接口：POST /api/v1/collections/{collection}/delete，body {ids:[doc_id]}。
func (c *ChromaClient) DeleteDoc(docID int64) error {
	if c.chromaURL == "" || c.collection == "" {
		return fmt.Errorf("向量库地址或集合未配置")
	}
	apiURL := c.chromaURL + "/api/v1/collections/" + url.PathEscape(c.collection) + "/delete"

	body, err := json.Marshal(map[string]any{
		"ids": []string{strconv.FormatInt(docID, 10)},
	})
	if err != nil {
		return fmt.Errorf("向量删除请求序列化失败: %w", err)
	}

	resp, err := httpPost(apiURL, body, 15*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SearchQuery 向量相似度检索，复用原有 pkg/vector.QuerySimilar 逻辑。
func (c *ChromaClient) SearchQuery(queryVector []float64, limit int) ([]int64, error) {
	return QuerySimilar(c.chromaURL, c.collection, queryVector, limit)
}