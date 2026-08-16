package vector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// EmbedText 调用向量化服务接口，将文本转为向量。
// 接口：POST {baseURL}/api/embedding/text，入参 {text}，返回 data.vector 数组。
func EmbedText(baseURL, text string) ([]float64, error) {
	if baseURL == "" || text == "" {
		return nil, fmt.Errorf("向量化服务地址或文本为空")
	}
	apiURL := strings.TrimRight(baseURL, "/") + "/api/embedding/text"

	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, fmt.Errorf("向量化请求序列化失败: %w", err)
	}

	resp, err := httpPost(apiURL, body, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Vector []float64 `json:"vector"`
		} `json:"data"`
	}
	if err := decodeResp(resp, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("向量化服务返回业务错误 code=%d msg=%s", apiResp.Code, apiResp.Message)
	}
	if len(apiResp.Data.Vector) == 0 {
		return nil, fmt.Errorf("向量化服务返回空向量")
	}
	return apiResp.Data.Vector, nil
}

// QuerySimilar 通过 chroma HTTP 接口按向量检索，返回候选文档 doc_id 列表。
//
// chroma 查询接口：POST /api/v1/collections/{collectionID}/query
// 约定：向量记录 id 即 code_function_doc.doc_id（字符串形式）。
// collectionID 为集合 UUID（Chroma 0.5.x 起 REST API 按 UUID 寻址，由调用方解析）。
// filter 非 nil 时追加 where 条件（repo_id / module_name 元数据过滤），收敛候选范围。
func QuerySimilar(chromaBaseURL, collectionID string, queryVector []float64, limit int, filter *SearchFilter) ([]int64, error) {
	if chromaBaseURL == "" || collectionID == "" {
		return nil, fmt.Errorf("向量库地址或集合未配置")
	}
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("查询向量为空")
	}
	apiURL := strings.TrimRight(chromaBaseURL, "/") + "/api/v1/collections/" + url.PathEscape(collectionID) + "/query"

	body := map[string]any{
		"query_embeddings": [][]float64{queryVector},
		"n_results":        limit,
		"include":          []string{"metadatas"},
	}
	if where := chromaWhere(filter); len(where) > 0 {
		body["where"] = where
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("向量检索请求序列化失败: %w", err)
	}

	resp, err := httpPost(apiURL, payload, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var qResp struct {
		IDs [][]string `json:"ids"`
	}
	if err := decodeResp(resp, &qResp); err != nil {
		return nil, err
	}
	if len(qResp.IDs) == 0 {
		return nil, nil
	}

	// 将字符串 id 转为 int64 doc_id
	var docIDs []int64
	for _, id := range qResp.IDs[0] {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil && n > 0 {
			docIDs = append(docIDs, n)
		}
	}
	return docIDs, nil
}

// chromaWhere 构建 chroma where 过滤条件（多条件用 $and 组合）。
func chromaWhere(filter *SearchFilter) map[string]any {
	if filter == nil {
		return nil
	}
	var conds []map[string]any
	if filter.RepoID > 0 {
		conds = append(conds, map[string]any{"repo_id": filter.RepoID})
	}
	if strings.TrimSpace(filter.Module) != "" {
		conds = append(conds, map[string]any{"module_name": strings.TrimSpace(filter.Module)})
	}
	switch len(conds) {
	case 0:
		return nil
	case 1:
		return conds[0]
	default:
		return map[string]any{"$and": conds}
	}
}

// httpPost 通用 POST 请求，带超时。
func httpPost(apiURL string, body []byte, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("服务返回异常状态码: %d", resp.StatusCode)
	}
	return resp, nil
}

// httpGet 通用 GET 请求，带超时。
func httpGet(apiURL string, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("服务返回异常状态码: %d", resp.StatusCode)
	}
	return resp, nil
}

// decodeResp 读取并解析 JSON 响应。
func decodeResp(resp *http.Response, v any) error {
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return nil
}
