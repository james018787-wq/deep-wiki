package repo

import (
	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// DocModifyLogRepo 文档人工校正日志仓库。
type DocModifyLogRepo struct {
	*BaseRepo[model.DocModifyLog]
}

// NewDocModifyLogRepo 构建文档日志仓库。
func NewDocModifyLogRepo(db *gorm.DB) *DocModifyLogRepo {
	return &DocModifyLogRepo{BaseRepo: &BaseRepo[model.DocModifyLog]{DB: db}}
}

// ListByDocID 查询某文档的全部操作日志（时间倒序）。
func (r *DocModifyLogRepo) ListByDocID(docID int64) ([]*model.DocModifyLog, error) {
	var list []*model.DocModifyLog
	err := r.DB.Where("doc_id = ?", docID).Order("id desc").Find(&list).Error
	return list, err
}

// RelationModifyLogRepo 模块依赖关系操作日志仓库。
type RelationModifyLogRepo struct {
	*BaseRepo[model.RelationModifyLog]
}

// NewRelationModifyLogRepo 构建依赖日志仓库。
func NewRelationModifyLogRepo(db *gorm.DB) *RelationModifyLogRepo {
	return &RelationModifyLogRepo{BaseRepo: &BaseRepo[model.RelationModifyLog]{DB: db}}
}

// CodeChangeLogRepo 代码迭代变更历史记录仓库。
type CodeChangeLogRepo struct {
	*BaseRepo[model.CodeChangeLog]
}

// NewCodeChangeLogRepo 构建变更历史仓库。
func NewCodeChangeLogRepo(db *gorm.DB) *CodeChangeLogRepo {
	return &CodeChangeLogRepo{BaseRepo: &BaseRepo[model.CodeChangeLog]{DB: db}}
}

// ListByDocID 查询某文档的迭代变更记录（时间倒序）。
func (r *CodeChangeLogRepo) ListByDocID(docID int64) ([]*model.CodeChangeLog, error) {
	var list []*model.CodeChangeLog
	err := r.DB.Where("doc_id = ?", docID).Order("id desc").Find(&list).Error
	return list, err
}