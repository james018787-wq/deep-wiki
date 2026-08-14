package vector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// UpdateDocEmbedding 调用向量化服务接口，同步更新单篇文档向量。
//
// 业务规则：人工编辑/重置文档事务提交后【异步调用】本函数，
// 保证向量检索始终使用最新校正内容。
//
// baseURL 为向量化服务（Python LLM 微服务）基础地址，
// 如 http://ai-wiki-llm:9000，接口路径为 /api/vector/upsert_doc。
func UpdateDocEmbedding(baseURL string, doc *DocVector) error {
	if baseURL == "" {
		return ErrNotImplemented
	}
	url := strings.TrimRight(baseURL, "/") + "/api/vector/upsert_doc"

	// 组装请求体：metadata 使用 map，便于向量库按模块过滤
	body, err := json.Marshal(map[string]any{
		"doc_id":      doc.DocID,
		"module_name": doc.ModuleName,
		"file_path":   doc.FilePath,
		"func_name":   doc.FuncName,
		"content":     doc.Content,
		"metadata": map[string]any{
			"module_name": doc.ModuleName,
			"func_name":   doc.FuncName,
			"file_path":   doc.FilePath,
		},
	})
	if err != nil {
		return fmt.Errorf("向量化请求序列化失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构建向量化请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 简单 http 调用：10s 超时，避免阻塞业务主流程
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用向量化服务失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("向量化服务返回异常状态码: %d", resp.StatusCode)
	}
	return nil
}