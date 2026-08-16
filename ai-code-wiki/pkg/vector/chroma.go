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
// 写入/更新：文本经向量化服务（/api/embedding/text）转为向量后，直连 chroma upsert；
// 检索复用 chroma HTTP query 逻辑；删除按约定（id=doc_id 字符串）调用 chroma HTTP delete。
// 向量库存取完全由本实现负责，不经 Python 侧向量库接口。
type ChromaClient struct {
	chromaURL    string // chroma HTTP 地址
	collection   string // 集合名
	embedBaseURL string // 向量化服务地址（embedding）
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
// 集合不存在时自动创建（get_or_create 语义），避免全新部署因缺少集合导致向量读写全部失败。
func (c *ChromaClient) resolveCollectionID() (string, error) {
	if c.collectionID != "" {
		return c.collectionID, nil
	}
	if c.chromaURL == "" || c.collection == "" {
		return "", fmt.Errorf("向量库地址或集合未配置")
	}
	id, err := c.getCollectionID()
	if err == nil {
		c.collectionID = id
		return id, nil
	}
	// 集合不存在（Chroma 对缺失集合 GET 返回 404 或 500）→ 自动创建后重查
	if cerr := c.createCollection(); cerr != nil {
		return "", fmt.Errorf("集合 %s 不存在且自动创建失败: %v（GET 错误: %v）", c.collection, cerr, err)
	}
	id, err = c.getCollectionID()
	if err != nil {
		return "", fmt.Errorf("自动创建集合后仍无法解析集合ID: %w", err)
	}
	c.collectionID = id
	return id, nil
}

// getCollectionID 按名称查询集合 UUID。
func (c *ChromaClient) getCollectionID() (string, error) {
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
	return col.ID, nil
}

// createCollection 创建集合（幂等：已存在时 Chroma 返回错误但忽略，语义同 get_or_create）。
// 集合维度无需在建集时指定，Chroma 在首次写入向量时按 embedding 维度自动固定。
func (c *ChromaClient) createCollection() error {
	apiURL := c.chromaURL + "/api/v1/collections"
	body, err := json.Marshal(map[string]any{"name": c.collection})
	if err != nil {
		return fmt.Errorf("创建集合请求序列化失败: %w", err)
	}
	resp, err := httpPost(apiURL, body, 15*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// UpsertDoc 写入/覆盖向量：文本向量化后直连 chroma upsert（幂等，新增或覆盖）。
func (c *ChromaClient) UpsertDoc(doc *DocVector) error {
	return c.write(doc)
}

// UpdateDoc 更新向量：chroma upsert 语义天然覆盖新增与更新。
func (c *ChromaClient) UpdateDoc(doc *DocVector) error {
	return c.write(doc)
}

// write 文本转向量后按 doc_id 写入/覆盖 chroma 记录。
//
// chroma upsert 接口：POST /api/v1/collections/{collectionID}/upsert
// 约定：向量记录 id 即 code_function_doc.doc_id（字符串形式）。
func (c *ChromaClient) write(doc *DocVector) error {
	if doc == nil || doc.DocID <= 0 {
		return fmt.Errorf("向量文档非法：doc_id 不能为空")
	}
	vec, err := EmbedText(c.embedBaseURL, doc.Content)
	if err != nil {
		return fmt.Errorf("向量化失败: %w", err)
	}
	colID, err := c.resolveCollectionID()
	if err != nil {
		return err
	}

	apiURL := c.chromaURL + "/api/v1/collections/" + url.PathEscape(colID) + "/upsert"
	meta := map[string]any{
		"module_name": doc.ModuleName,
		"file_path":   doc.FilePath,
		"func_name":   doc.FuncName,
		"repo_id":     doc.RepoID,
		"func_line":   doc.FuncLine,
	}
	if doc.RepoName != "" {
		meta["repo_name"] = doc.RepoName
	}
	body, err := json.Marshal(map[string]any{
		"ids":        []string{strconv.FormatInt(doc.DocID, 10)},
		"embeddings": [][]float64{vec},
		"documents":  []string{doc.Content},
		"metadatas":  []map[string]any{meta},
	})
	if err != nil {
		return fmt.Errorf("向量写入请求序列化失败: %w", err)
	}

	resp, err := httpPost(apiURL, body, 15*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
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
func (c *ChromaClient) SearchQuery(queryVector []float64, limit int, filter *SearchFilter) ([]int64, error) {
	colID, err := c.resolveCollectionID()
	if err != nil {
		return nil, err
	}
	return QuerySimilar(c.chromaURL, colID, queryVector, limit, filter)
}
