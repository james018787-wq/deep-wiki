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
	collectionID string // 集合 UUID（懒解析缓存，Chroma 0.5.x 起查询/删除按 UUID）
}

// NewChromaClient 构建 Chroma 实现。
func NewChromaClient(opts Options) *ChromaClient {
	return &ChromaClient{
		chromaURL:    strings.TrimRight(opts.ChromaURL, "/"),
		collection:   opts.Collection,
		embedBaseURL: strings.TrimRight(opts.EmbedBaseURL, "/"),
	}
}

// resolveCollectionID 按名称解析集合 UUID（Chroma 0.5.x 起 REST API 使用 UUID 而非名称）。
// 接口：GET /api/v1/collections/{name}，返回含 id 的集合信息；结果缓存复用。
func (c *ChromaClient) resolveCollectionID() (string, error) {
	if c.collectionID != "" {
		return c.collectionID, nil
	}
	if c.chromaURL == "" || c.collection == "" {
		return "", fmt.Errorf("向量库地址或集合未配置")
	}
	apiURL := c.chromaURL + "/api/v1/collections/" + url.PathEscape(c.collection)
	resp, err := httpGet(apiURL, 15*time.Second)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var col struct {
		ID string `json:"id"`
	}
	if err := decodeResp(resp, &col); err != nil {
		return "", err
	}
	if col.ID == "" {
		return "", fmt.Errorf("向量集合未找到: %s", c.collection)
	}
	c.collectionID = col.ID
	return col.ID, nil
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
// chroma delete 接口：POST /api/v1/collections/{uuid}/delete，body {ids:[doc_id]}。
func (c *ChromaClient) DeleteDoc(docID int64) error {
	colID, err := c.resolveCollectionID()
	if err != nil {
		return err
	}
	apiURL := c.chromaURL + "/api/v1/collections/" + url.PathEscape(colID) + "/delete"

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
	colID, err := c.resolveCollectionID()
	if err != nil {
		return nil, err
	}
	return QuerySimilar(c.chromaURL, colID, queryVector, limit)
}