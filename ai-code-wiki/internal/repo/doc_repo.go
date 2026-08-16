package repo

import (
	"encoding/json"
	"fmt"
	"strings"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/common"

	"gorm.io/gorm"
)

// CodeFunctionDocRepo 函数业务文档主表仓库。
type CodeFunctionDocRepo struct {
	*BaseRepo[model.CodeFunctionDoc]
}

// NewCodeFunctionDocRepo 构建函数文档仓库。
func NewCodeFunctionDocRepo(db *gorm.DB) *CodeFunctionDocRepo {
	return &CodeFunctionDocRepo{BaseRepo: &BaseRepo[model.CodeFunctionDoc]{DB: db}}
}

// GetByFileFunc 按仓库+文件路径+函数名查询文档（唯一键 idx_file_func）。
func (r *CodeFunctionDocRepo) GetByFileFunc(repoID int64, filePath, funcName string) (*model.CodeFunctionDoc, error) {
	return r.GetByWhere(map[string]any{
		"repo_id":   repoID,
		"file_path": filePath,
		"func_name": funcName,
	})
}

// ListByFile 按仓库+文件路径列出该文件全部未删除文档（幽灵文档清理：与当前代码函数集合求差）。
func (r *CodeFunctionDocRepo) ListByFile(repoID int64, filePath string) ([]*model.CodeFunctionDoc, error) {
	var list []*model.CodeFunctionDoc
	err := withNotDeleted(r.DB).
		Where("repo_id = ? AND file_path = ?", repoID, filePath).
		Order("id asc").Find(&list).Error
	return list, err
}

// RemoveDocWithLog 幽灵文档下线：事务内写删除操作日志（operate_type=3）+ 逻辑删除。
// 保留 before_content 快照供审计/追溯，AfterContent 为空。
func (r *CodeFunctionDocRepo) RemoveDocWithLog(doc *model.CodeFunctionDoc, operator, remark string) error {
	before, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化文档快照失败: %w", err)
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		logRecord := &model.DocModifyLog{
			RepoID:        doc.RepoID,
			DocID:         doc.ID,
			OperateType:   common.DocOperateDelete,
			BeforeContent: string(before),
			AfterContent:  "",
			Operator:      operator,
			Remark:        remark,
		}
		if err := tx.Create(logRecord).Error; err != nil {
			return err
		}
		return tx.Model(&model.CodeFunctionDoc{}).
			Where("id = ?", doc.ID).Update("is_deleted", 1).Error
	})
}

// ListByModule 按仓库+模块查询文档列表。
func (r *CodeFunctionDocRepo) ListByModule(repoID int64, moduleName string, page, pageSize int) ([]*model.CodeFunctionDoc, int64, error) {
	where := map[string]any{"repo_id": repoID}
	if moduleName != "" {
		where["module_name"] = moduleName
	}
	return r.ListByWhere(where, "id desc", page, pageSize)
}

// ListManualModified 查询指定仓库所有人工校正文档（content_source = 2）。
func (r *CodeFunctionDocRepo) ListManualModified(repoID int64, page, pageSize int) ([]*model.CodeFunctionDoc, int64, error) {
	where := map[string]any{
		"repo_id":        repoID,
		"content_source": common.ContentSourceManual,
	}
	return r.ListByWhere(where, "last_edit_time desc", page, pageSize)
}

// ListPendingReview 查询指定仓库源码已变更待复核文档（source_code_changed = 1）。
func (r *CodeFunctionDocRepo) ListPendingReview(repoID int64) ([]*model.CodeFunctionDoc, error) {
	var list []*model.CodeFunctionDoc
	err := withNotDeleted(r.DB).
		Where("repo_id = ? AND source_code_changed = ?", repoID, common.SourceCodeChanged).
		Find(&list).Error
	return list, err
}

// ListByModules 查询指定仓库下指定模块集合的文档，用于跨模块召回扩充。
// limit 控制返回数量上限（limit<=0 表示不限）。
func (r *CodeFunctionDocRepo) ListByModules(repoID int64, modules []string, limit int) ([]*model.CodeFunctionDoc, error) {
	var list []*model.CodeFunctionDoc
	query := withNotDeleted(r.DB).Where("repo_id = ? AND module_name IN ?", repoID, modules).Order("id desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&list).Error
	return list, err
}

// SearchByKeyword 按关键字模糊检索文档（模块/函数名/文件路径/业务字段），限定仓库。
// 混合检索的关键词通道：与向量召回结果做并集去重，覆盖向量召回不到的关键词命中。
func (r *CodeFunctionDocRepo) SearchByKeyword(repoID int64, keyword string, limit int) ([]*model.CodeFunctionDoc, error) {
	if limit <= 0 {
		limit = 10
	}
	if strings.TrimSpace(keyword) == "" {
		return nil, nil
	}
	like := "%" + strings.TrimSpace(keyword) + "%"
	var list []*model.CodeFunctionDoc
	err := withNotDeleted(r.DB).
		Where("repo_id = ? AND (module_name LIKE ? OR func_name LIKE ? OR file_path LIKE ? OR summary LIKE ? OR input_desc LIKE ? OR output_desc LIKE ? OR process_flow LIKE ? OR risk_point LIKE ?)",
			repoID, like, like, like, like, like, like, like, like).
		Limit(limit).Find(&list).Error
	return list, err
}
