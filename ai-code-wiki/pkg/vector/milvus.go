package vector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// MilvusClient 基于 Milvus 的 VectorClient 实现。
//
// 连接参数（环境变量，见 internal/config/config.go ApplyEnv）：
//   - MILVUS_HOST       Milvus 服务地址，如 127.0.0.1 或 milvus（docker-compose 服务名）
//   - MILVUS_PORT       Milvus 端口，默认 19530
//   - MILVUS_COLLECTION 集合名，默认 code_doc
//   - MILVUS_DIM        embedding 向量维度，需与向量化服务输出维度一致（如 OpenAI text-embedding-3-small 为 1536）
//   - MILVUS_USER       用户名（可选，服务端开启鉴权时必填）
//   - MILVUS_PASSWORD   密码（可选）
//
// 设计说明：
//   - 采用惰性连接：首次调用时才建立与 Milvus 的连接，避免引擎不可用时拖垮服务启动。
//   - 集合不存在时自动创建（FLAT 索引 + L2 距离），并对 embedding 字段建索引、加载集合。
//   - 写入前通过 LLM 服务 EmbedText 将文本转向量，再落库。
type MilvusClient struct {
	address  string
	user     string
	passwd   string
	coll     string
	dim      int
	embedURL string

	mu        sync.Mutex
	cli       client.Client
	connected bool
}

// 集合字段与长度常量（与 code_function_doc 表字段对应）。
const (
	milvusFieldDocID      = "doc_id"
	milvusFieldRepoID     = "repo_id"
	milvusFieldRepoName   = "repo_name"
	milvusFieldModuleName = "module_name"
	milvusFieldFilePath   = "file_path"
	milvusFieldFuncName   = "func_name"
	milvusFieldContent    = "content"
	milvusFieldEmbedding  = "embedding"

	milvusModuleMaxLen   = 64   // module_name VarChar 长度
	milvusRepoMaxLen     = 64   // repo_name VarChar 长度
	milvusFilePathMaxLen = 512  // file_path VarChar 长度
	milvusFuncMaxLen     = 128  // func_name VarChar 长度
	milvusContentMaxLen  = 6000 // content VarChar 长度（超长截断，全文以 MySQL 为准）

	milvusOpTimeout      = 30 * time.Second // 单次操作超时
	milvusConnectTimeout = 15 * time.Second // 连接超时
)

// NewMilvusClient 构建 Milvus 实现（惰性连接，不立即建连）。
func NewMilvusClient(opts Options) *MilvusClient {
	return &MilvusClient{
		address:  fmt.Sprintf("%s:%d", opts.MilvusHost, opts.MilvusPort),
		user:     opts.MilvusUser,
		passwd:   opts.MilvusPassword,
		coll:     opts.Collection,
		dim:      opts.MilvusDim,
		embedURL: strings.TrimRight(opts.EmbedBaseURL, "/"),
	}
}

// connect 建立（或复用）与 Milvus 的连接。
func (m *MilvusClient) connect() (client.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connected && m.cli != nil {
		return m.cli, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), milvusConnectTimeout)
	defer cancel()
	cli, err := client.NewClient(ctx, client.Config{
		Address:  m.address,
		Username: m.user,
		Password: m.passwd,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 Milvus 失败（%s）: %w", m.address, err)
	}
	m.cli = cli
	m.connected = true
	return cli, nil
}

// milvusExpr 构建 Milvus 检索过滤表达式（按 repo_id / module_name）。
func milvusExpr(filter *SearchFilter) string {
	if filter == nil {
		return ""
	}
	var conds []string
	if filter.RepoID > 0 {
		conds = append(conds, fmt.Sprintf("%s == %d", milvusFieldRepoID, filter.RepoID))
	}
	if m := strings.TrimSpace(filter.Module); m != "" {
		conds = append(conds, fmt.Sprintf("%s == %q", milvusFieldModuleName, m))
	}
	if len(conds) == 0 {
		return ""
	}
	return strings.Join(conds, " && ")
}

// ensureReady 建立连接并确保集合存在（不存在则自动创建 + 建索引 + 加载）。
func (m *MilvusClient) ensureReady() (client.Client, error) {
	cli, err := m.connect()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), milvusOpTimeout)
	defer cancel()

	ok, err := cli.HasCollection(ctx, m.coll)
	if err != nil {
		return nil, fmt.Errorf("检查 Milvus 集合失败: %w", err)
	}
	if !ok {
		if err := m.createCollection(ctx, cli); err != nil {
			return nil, err
		}
	}
	return cli, nil
}

// createCollection 创建集合：FLAT 索引 + L2 距离，并加载到内存。
func (m *MilvusClient) createCollection(ctx context.Context, cli client.Client) error {
	schema := entity.NewSchema().
		WithName(m.coll).
		WithDescription("ai-code-wiki 函数业务文档向量").
		WithField(entity.NewField().
			WithName(milvusFieldDocID).
			WithDataType(entity.FieldTypeInt64).
			WithIsPrimaryKey(true).
			WithDescription("code_function_doc.doc_id")).
		WithField(entity.NewField().
			WithName(milvusFieldRepoName).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(milvusRepoMaxLen).
			WithDescription("所属仓库")).
		WithField(entity.NewField().
			WithName(milvusFieldRepoID).
			WithDataType(entity.FieldTypeInt64).
			WithDescription("所属仓库id（检索过滤）")).
		WithField(entity.NewField().
			WithName(milvusFieldModuleName).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(milvusModuleMaxLen).
			WithDescription("所属模块")).
		WithField(entity.NewField().
			WithName(milvusFieldFilePath).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(milvusFilePathMaxLen).
			WithDescription("文件路径")).
		WithField(entity.NewField().
			WithName(milvusFieldFuncName).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(milvusFuncMaxLen).
			WithDescription("函数名")).
		WithField(entity.NewField().
			WithName(milvusFieldContent).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(milvusContentMaxLen).
			WithDescription("向量化文本内容（截断存储）")).
		WithField(entity.NewField().
			WithName(milvusFieldEmbedding).
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(m.dim)).
			WithDescription("文本 embedding 向量"))

	if err := cli.CreateCollection(ctx, schema, 1); err != nil {
		return fmt.Errorf("创建 Milvus 集合 %s 失败: %w", m.coll, err)
	}

	// embedding 字段建 FLAT 索引（L2 距离），与检索距离度量保持一致
	idx := entity.NewGenericIndex("idx_embedding_flat", entity.Flat, map[string]string{
		"metric_type": string(entity.L2),
	})
	if err := cli.CreateIndex(ctx, m.coll, milvusFieldEmbedding, idx, true); err != nil {
		return fmt.Errorf("创建 Milvus 向量索引失败: %w", err)
	}

	// 加载集合到内存，否则检索不可用
	if err := cli.LoadCollection(ctx, m.coll, true); err != nil {
		return fmt.Errorf("加载 Milvus 集合失败: %w", err)
	}
	return nil
}

// UpsertDoc 写入向量：文本转向量后按主键 upsert（新增或覆盖）。
func (m *MilvusClient) UpsertDoc(doc *DocVector) error {
	return m.write(doc)
}

// UpdateDoc 更新向量：按主键 upsert 覆盖（Milvus 无独立 update，PK 存在即覆盖）。
func (m *MilvusClient) UpdateDoc(doc *DocVector) error {
	return m.write(doc)
}

// write 文本转向量后写入 Milvus。
func (m *MilvusClient) write(doc *DocVector) error {
	if doc == nil || doc.DocID <= 0 {
		return errors.New("向量文档非法：doc_id 不能为空")
	}
	vec, err := EmbedText(m.embedURL, doc.Content)
	if err != nil {
		return fmt.Errorf("向量化失败: %w", err)
	}
	if len(vec) != m.dim {
		return fmt.Errorf("向量维度不匹配：内容向量 %d 维，MILVUS_DIM 配置 %d 维", len(vec), m.dim)
	}

	cli, err := m.ensureReady()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), milvusOpTimeout)
	defer cancel()

	columns := []entity.Column{
		entity.NewColumnInt64(milvusFieldDocID, []int64{doc.DocID}),
		entity.NewColumnInt64(milvusFieldRepoID, []int64{doc.RepoID}),
		entity.NewColumnVarChar(milvusFieldRepoName, []string{doc.RepoName}),
		entity.NewColumnVarChar(milvusFieldModuleName, []string{doc.ModuleName}),
		entity.NewColumnVarChar(milvusFieldFilePath, []string{doc.FilePath}),
		entity.NewColumnVarChar(milvusFieldFuncName, []string{doc.FuncName}),
		entity.NewColumnVarChar(milvusFieldContent, []string{truncateRune(doc.Content, milvusContentMaxLen)}),
		entity.NewColumnFloatVector(milvusFieldEmbedding, m.dim, [][]float32{toFloat32s(vec)}),
	}
	if _, err := cli.Upsert(ctx, m.coll, "", columns...); err != nil {
		return fmt.Errorf("写入 Milvus 失败 doc_id=%d: %w", doc.DocID, err)
	}
	return nil
}

// DeleteDoc 按主键 doc_id 删除向量。
func (m *MilvusClient) DeleteDoc(docID int64) error {
	if docID <= 0 {
		return errors.New("向量删除非法：doc_id 不能为空")
	}
	cli, err := m.ensureReady()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), milvusOpTimeout)
	defer cancel()

	pks := entity.NewColumnInt64(milvusFieldDocID, []int64{docID})
	if err := cli.DeleteByPks(ctx, m.coll, "", pks); err != nil {
		return fmt.Errorf("删除 Milvus 向量失败 doc_id=%d: %w", docID, err)
	}
	return nil
}

// SearchQuery 向量相似度检索，返回按距离升序的候选 doc_id 列表。
func (m *MilvusClient) SearchQuery(queryVector []float64, limit int, filter *SearchFilter) ([]int64, error) {
	if len(queryVector) == 0 {
		return nil, errors.New("查询向量为空")
	}
	if len(queryVector) != m.dim {
		return nil, fmt.Errorf("查询向量维度 %d 与 MILVUS_DIM %d 不匹配", len(queryVector), m.dim)
	}
	cli, err := m.ensureReady()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), milvusOpTimeout)
	defer cancel()

	sp, err := entity.NewIndexFlatSearchParam()
	if err != nil {
		return nil, fmt.Errorf("构建检索参数失败: %w", err)
	}

	vectors := []entity.Vector{
		entity.FloatVector(toFloat32s(queryVector)),
	}
	results, err := cli.Search(ctx, m.coll, nil, milvusExpr(filter), []string{milvusFieldDocID},
		vectors, milvusFieldEmbedding, entity.L2, limit, sp)
	if err != nil {
		return nil, fmt.Errorf("Milvus 向量检索失败: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}

	// 从检索结果中提取 doc_id 列（通过 Column 接口通用取值，兼容不同实现）
	var docIDs []int64
	for _, col := range results[0].Fields {
		if col.Name() != milvusFieldDocID {
			continue
		}
		for i := 0; i < col.Len(); i++ {
			v, err := col.Get(i)
			if err != nil {
				continue
			}
			if id, ok := v.(int64); ok && id > 0 {
				docIDs = append(docIDs, id)
			}
		}
	}
	return docIDs, nil
}

// toFloat32s 将 []float64 转为 []float32（Milvus 向量为 float32）。
func toFloat32s(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}

// truncateRune 按字符（rune）截断字符串，超长追加省略号。
func truncateRune(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
