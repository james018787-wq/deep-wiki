package repo

import (
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
		"repo_id":       repoID,
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