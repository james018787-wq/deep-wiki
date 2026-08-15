package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/taskqueue"
	"ai-code-wiki/pkg/vector"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DocService 业务文档相关逻辑：检索、详情、人工校正、重置、迭代记录。
type DocService struct {
	db         *gorm.DB
	docRepo    *repo.CodeFunctionDocRepo
	modifyLog  *repo.DocModifyLogRepo
	changeLog  *repo.CodeChangeLogRepo
	moduleRepo *repo.BusinessModuleRepo
	vc         vector.VectorClient // 向量存储抽象（业务不感知 chroma/milvus）
	queue      taskqueue.TaskQueue // 异步任务队列（提交入口，消费由独立 Worker 完成）
}

// NewDocService 构建文档服务。
// vc 为 nil 时跳过向量同步（向量引擎未配置/初始化失败场景）。
func NewDocService(db *gorm.DB, vc vector.VectorClient, queue taskqueue.TaskQueue) *DocService {
	return &DocService{
		db:         db,
		docRepo:    newDocRepo(db),
		modifyLog:  repo.NewDocModifyLogRepo(db),
		changeLog:  repo.NewCodeChangeLogRepo(db),
		moduleRepo: repo.NewBusinessModuleRepo(db),
		vc:         vc,
		queue:      queue,
	}
}

// ListModules 获取所有业务模块。
func (s *DocService) ListModules(ctx context.Context) ([]*model.BusinessModule, error) {
	_ = ctx
	modules, err := s.moduleRepo.ListAll()
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询业务模块失败", err)
	}
	return modules, nil
}

// ListDocs 分页查询函数文档列表，可按模块筛选（前端文档列表页使用）。
func (s *DocService) ListDocs(ctx context.Context, module string, page, pageSize int) (*common.PageResult, error) {
	_ = ctx
	list, total, err := s.docRepo.ListByModule(strings.TrimSpace(module), page, pageSize)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询文档列表失败", err)
	}
	return &common.PageResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetDoc 获取文档详情。
// 文档不存在时返回业务错误 CodeNotFound。
func (s *DocService) GetDoc(ctx context.Context, docID int64) (*model.CodeFunctionDoc, error) {
	_ = ctx
	doc, err := s.docRepo.GetByID(docID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "文档不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询文档失败", err)
	}
	return doc, nil
}

// EditDocReq 人工校正文档入参。
type EditDocReq struct {
	Summary     string `json:"summary"`                     // 业务摘要
	InputDesc   string `json:"input_desc"`                  // 入参说明
	OutputDesc  string `json:"output_desc"`                 // 返回值说明
	ProcessFlow string `json:"process_flow"`                // 业务执行流程
	RiskPoint   string `json:"risk_point"`                  // 业务风险点
	Operator    string `json:"operator" binding:"required"` // 操作人
	Remark      string `json:"remark"`                      // 备注
}

// EditDoc 人工校正业务文档。
//
// 业务规则（严格遵守）：
//  1. 整个编辑过程使用 GORM 事务包裹，保证文档更新与日志写入的原子性。
//  2. 修改前把完整旧文档快照存入 doc_modify_log，before_content 存旧文档 JSON。
//  3. 更新 CodeFunctionDoc 字段，content_source 置为 2（人工校正），
//     记录操作人 last_edit_user 与操作时间 last_edit_time。
//  4. origin_auto_doc 永久保留首次 AI 生成内容，禁止覆盖。
//  5. 事务提交之后【异步调用向量抽象 VectorClient.UpsertDoc】同步向量库，
//     保证向量检索使用最新校正内容。
//
// 校验：文档不存在返回业务错误。
func (s *DocService) EditDoc(ctx context.Context, docID int64, req *EditDocReq) error {
	_ = ctx
	if strings.TrimSpace(req.Operator) == "" {
		return common.NewError(common.CodeBadRequest, "操作人不能为空")
	}

	var updated *model.CodeFunctionDoc
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 锁定查询文档（行级锁防并发编辑），排除已删除
		doc, err := s.lockDoc(tx, docID)
		if err != nil {
			return err
		}

		// 2. 修改前完整旧文档快照写入 doc_modify_log（operate_type=1）
		beforeJSON, err := json.Marshal(doc)
		if err != nil {
			return common.WrapError(common.CodeInternalError, "文档快照序列化失败", err)
		}

		// 3. 更新生效字段（不触碰 origin_auto_doc）
		// 仅更新请求中提供的非空字段，未提供的字段保留原值（支持部分编辑）
		now := time.Now()
		if req.Summary != "" {
			doc.Summary = req.Summary
		}
		if req.InputDesc != "" {
			doc.InputDesc = req.InputDesc
		}
		if req.OutputDesc != "" {
			doc.OutputDesc = req.OutputDesc
		}
		if req.ProcessFlow != "" {
			doc.ProcessFlow = req.ProcessFlow
		}
		if req.RiskPoint != "" {
			doc.RiskPoint = req.RiskPoint
		}
		doc.ContentSource = common.ContentSourceManual // 2=人工校正
		doc.LastEditUser = req.Operator
		doc.LastEditTime = &now
		if err := tx.Save(doc).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "更新文档失败", err)
		}

		// 4. 写入修改后快照与操作日志
		afterJSON, err := json.Marshal(doc)
		if err != nil {
			return common.WrapError(common.CodeInternalError, "文档快照序列化失败", err)
		}
		logRecord := &model.DocModifyLog{
			DocID:         docID,
			OperateType:   common.DocOperateEdit,
			BeforeContent: string(beforeJSON),
			AfterContent:  string(afterJSON),
			Operator:      req.Operator,
			Remark:        req.Remark,
		}
		if err := tx.Create(logRecord).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "写入校正日志失败", err)
		}

		updated = doc
		return nil
	})
	if err != nil {
		return err
	}

	// 5. 事务提交后异步同步向量库（best-effort，失败不阻塞主流程）
	s.syncVectorAsync(updated)
	return nil
}

// ResetDoc 将文档重置为原始 AI 版本。
//
// 业务规则（严格遵守）：
//  1. 整个重置过程使用 GORM 事务包裹，保证文档更新与日志写入的原子性。
//  2. 使用 origin_auto_doc 恢复原始 AI 生成内容，origin_auto_doc 禁止被覆盖。
//  3. content_source 恢复为 1（AI自动生成）。
//  4. 写入重置类型操作日志（operate_type=2）。
//  5. 事务提交之后【异步调用向量抽象 VectorClient.UpsertDoc】同步向量库。
//
// 校验：文档不存在、origin_auto_doc 为空均返回业务错误。
func (s *DocService) ResetDoc(ctx context.Context, docID int64, operator string) error {
	_ = ctx
	if strings.TrimSpace(operator) == "" {
		return common.NewError(common.CodeBadRequest, "操作人不能为空")
	}

	var updated *model.CodeFunctionDoc
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 锁定查询文档
		doc, err := s.lockDoc(tx, docID)
		if err != nil {
			return err
		}

		// 2. 校验原始 AI 文档必须存在
		if strings.TrimSpace(doc.OriginAutoDoc) == "" {
			return common.NewError(common.CodeInvalidState, "原始AI文档为空，无法重置")
		}

		// 3. 修改前完整旧文档快照写入 doc_modify_log（operate_type=2）
		beforeJSON, err := json.Marshal(doc)
		if err != nil {
			return common.WrapError(common.CodeInternalError, "文档快照序列化失败", err)
		}

		// 4. 用 origin_auto_doc 恢复原始 AI 生成内容，origin_auto_doc 自身不被覆盖
		if err := restoreFromOriginDoc(doc); err != nil {
			return err
		}

		// 5. content_source 恢复为 1，清空人工校正痕迹
		doc.ContentSource = common.ContentSourceAuto
		doc.LastEditUser = ""
		doc.LastEditTime = nil
		if err := tx.Save(doc).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "重置文档失败", err)
		}

		// 6. 写入重置类型操作日志
		afterJSON, err := json.Marshal(doc)
		if err != nil {
			return common.WrapError(common.CodeInternalError, "文档快照序列化失败", err)
		}
		logRecord := &model.DocModifyLog{
			DocID:         docID,
			OperateType:   common.DocOperateReset,
			BeforeContent: string(beforeJSON),
			AfterContent:  string(afterJSON),
			Operator:      operator,
			Remark:        "重置回原始AI自动生成版本",
		}
		if err := tx.Create(logRecord).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "写入重置日志失败", err)
		}

		updated = doc
		return nil
	})
	if err != nil {
		return err
	}

	// 7. 事务提交后异步同步向量库
	s.syncVectorAsync(updated)
	return nil
}

// lockDoc 行级锁查询文档，排除已删除记录。
// 文档不存在时返回业务错误（CodeNotFound）。
func (s *DocService) lockDoc(tx *gorm.DB, docID int64) (*model.CodeFunctionDoc, error) {
	var doc model.CodeFunctionDoc
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND is_deleted = ?", docID, common.NotDeleted).
		First(&doc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "文档不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询文档失败", err)
	}
	return &doc, nil
}

// restoreFromOriginDoc 用 origin_auto_doc 解析并恢复原始 AI 文档字段。
// origin_auto_doc 为首次 AI 生成的标准化文档 JSON。
func restoreFromOriginDoc(doc *model.CodeFunctionDoc) error {
	type originDoc struct {
		Summary     string `json:"summary"`
		InputDesc   string `json:"input_desc"`
		OutputDesc  string `json:"output_desc"`
		ProcessFlow string `json:"process_flow"`
		RelyModules string `json:"rely_modules"`
		RiskPoint   string `json:"risk_point"`
	}
	var origin originDoc
	if err := json.Unmarshal([]byte(doc.OriginAutoDoc), &origin); err != nil {
		return common.WrapError(common.CodeInvalidState, "原始AI文档格式异常，无法重置", err)
	}
	doc.Summary = origin.Summary
	doc.InputDesc = origin.InputDesc
	doc.OutputDesc = origin.OutputDesc
	doc.ProcessFlow = origin.ProcessFlow
	doc.RelyModules = origin.RelyModules
	doc.RiskPoint = origin.RiskPoint
	return nil
}

// syncVectorAsync 事务提交后投递向量同步任务到队列。
// 最小切片单元=单个函数，投递失败仅记录日志，不阻塞主流程。
// 消费由独立 Worker 完成（替换直接 goroutine）。
func (s *DocService) syncVectorAsync(doc *model.CodeFunctionDoc) {
	if doc == nil || s.vc == nil {
		return
	}
	msg, err := buildVectorSyncMessage(doc)
	if err != nil {
		logger.Warn(context.Background(), "构建向量同步任务失败 doc_id=%d err=%v", doc.ID, err)
		return
	}
	if err := s.queue.SubmitTask(msg); err != nil {
		logger.Warn(context.Background(), "向量同步任务投递失败 doc_id=%d err=%v", doc.ID, err)
	}
}

// ListModifiedDocs 查询所有人工校正文档。
func (s *DocService) ListModifiedDocs(ctx context.Context, page, pageSize int) (*common.PageResult, error) {
	_ = ctx
	list, total, err := s.docRepo.ListManualModified(page, pageSize)
	if err != nil {
		return nil, err
	}
	return &common.PageResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// ListChangeLogs 查看文档迭代变更记录。
func (s *DocService) ListChangeLogs(ctx context.Context, docID int64) ([]*model.CodeChangeLog, error) {
	_ = ctx
	logs, err := s.changeLog.ListByDocID(docID)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询文档变更记录失败", err)
	}
	return logs, nil
}

// ============ 文档历史版本（doc_modify_log 快照） ============

// DocHistoryItem 文档历史修改记录列表项（不含快照正文，避免列表过大）。
type DocHistoryItem struct {
	LogID       int64     `json:"log_id"`       // 历史记录 ID
	OperateType int8      `json:"operate_type"` // 操作类型：1编辑 2重置
	OperateName string    `json:"operate_name"` // 操作类型中文描述
	Operator    string    `json:"operator"`     // 操作人
	OperateTime time.Time `json:"operate_time"` // 修改时间
	Remark      string    `json:"remark"`       // 备注
}

// DocHistoryDetail 单条历史快照详情（含修改前后完整原始 JSON）。
type DocHistoryDetail struct {
	DocHistoryItem
	Before map[string]any `json:"before"` // 修改前完整文档快照（原始 JSON）
	After  map[string]any `json:"after"`  // 修改后完整文档快照（原始 JSON）
}

// ListDocHistory 查看文档全部修改记录（基于 doc_modify_log，时间倒序）。
func (s *DocService) ListDocHistory(ctx context.Context, docID int64) ([]*DocHistoryItem, error) {
	_ = ctx
	logs, err := s.modifyLog.ListByDocID(docID)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询文档修改记录失败", err)
	}
	list := make([]*DocHistoryItem, 0, len(logs))
	for _, log := range logs {
		list = append(list, &DocHistoryItem{
			LogID:       log.ID,
			OperateType: log.OperateType,
			OperateName: operateName(log.OperateType),
			Operator:    log.Operator,
			OperateTime: log.OperateTime,
			Remark:      log.Remark,
		})
	}
	return list, nil
}

// GetDocHistoryDetail 获取某一条历史快照详情（含修改前后原始 JSON）。
// 记录不存在或不属于该文档时返回 CodeNotFound。
func (s *DocService) GetDocHistoryDetail(ctx context.Context, docID, logID int64) (*DocHistoryDetail, error) {
	_ = ctx
	log, err := s.modifyLog.GetByLogID(logID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "历史记录不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询历史记录失败", err)
	}
	if log.DocID != docID {
		return nil, common.NewError(common.CodeNotFound, "历史记录不存在")
	}
	before, err := parseSnapshotJSON(log.BeforeContent)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "解析修改前快照失败", err)
	}
	after, err := parseSnapshotJSON(log.AfterContent)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "解析修改后快照失败", err)
	}
	return &DocHistoryDetail{
		DocHistoryItem: DocHistoryItem{
			LogID:       log.ID,
			OperateType: log.OperateType,
			OperateName: operateName(log.OperateType),
			Operator:    log.Operator,
			OperateTime: log.OperateTime,
			Remark:      log.Remark,
		},
		Before: before,
		After:  after,
	}, nil
}

// parseSnapshotJSON 解析快照原始 JSON（以 map 形式返回，不丢失字段）。
func parseSnapshotJSON(content string) (map[string]any, error) {
	if content == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// operateName 文档操作类型中文描述。
func operateName(t int8) string {
	switch t {
	case common.DocOperateEdit:
		return "编辑文档"
	case common.DocOperateReset:
		return "重置回AI原始版本"
	default:
		return "未知操作"
	}
}

// ============ 源码变更处理策略 ============
// 说明：人工校正文档（content_source=2）在自动解析任务中的不覆盖逻辑在
// TaskService.processFunc 内实现（仅更新 source_code + 置 source_code_changed=1），
// 不再单独维护本处逻辑，避免重复/遗漏。
