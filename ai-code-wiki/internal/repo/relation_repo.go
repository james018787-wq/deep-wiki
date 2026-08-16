package repo

import (
	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// ModuleRelationRepo 模块依赖知识图谱表仓库。
type ModuleRelationRepo struct {
	*BaseRepo[model.ModuleRelation]
}

// NewModuleRelationRepo 构建模块依赖仓库。
func NewModuleRelationRepo(db *gorm.DB) *ModuleRelationRepo {
	return &ModuleRelationRepo{BaseRepo: &BaseRepo[model.ModuleRelation]{DB: db}}
}

// ListByModule 查询某仓库某模块的上下游依赖关系。
// direction=out 查询源模块指向哪些模块；direction=in 查询哪些模块依赖源模块。
func (r *ModuleRelationRepo) ListByModule(repoID int64, module string, direction string) ([]*model.ModuleRelation, error) {
	var list []*model.ModuleRelation
	query := withNotDeleted(r.DB).Where("repo_id = ?", repoID)
	if direction == "in" {
		query = query.Where("target_module = ?", module)
	} else {
		query = query.Where("source_module = ?", module)
	}
	err := query.Order("relation_type asc, id asc").Find(&list).Error
	return list, err
}

// GetByRelation 按 (仓库, 源模块, 目标模块, 关系类型) 查询唯一关系。
func (r *ModuleRelationRepo) GetByRelation(repoID int64, sourceModule, targetModule string, relationType int8) (*model.ModuleRelation, error) {
	return r.GetByWhere(map[string]any{
		"repo_id":       repoID,
		"source_module": sourceModule,
		"target_module": targetModule,
		"relation_type": relationType,
	})
}

// ListRelationsByModules 查询与给定模块集合相关的依赖关系（源或目标命中），限定仓库。
// 业务规则：合并 AST 自动识别(source=1) 与 人工添加(source=2) 的关系，
// 查询时【不过滤 source 字段】，保证人工新增依赖不被丢弃。
func (r *ModuleRelationRepo) ListRelationsByModules(repoID int64, modules []string) ([]*model.ModuleRelation, error) {
	var list []*model.ModuleRelation
	query := withNotDeleted(r.DB).Where("repo_id = ?", repoID)
	if len(modules) > 0 {
		query = query.Where("source_module IN ? OR target_module IN ?", modules, modules)
	}
	err := query.Order("relation_type asc, id asc").Find(&list).Error
	return list, err
}