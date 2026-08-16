package repo

import (
	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// BusinessModuleRepo 业务模块表仓库。
type BusinessModuleRepo struct {
	*BaseRepo[model.BusinessModule]
}

// NewBusinessModuleRepo 构建业务模块仓库。
func NewBusinessModuleRepo(db *gorm.DB) *BusinessModuleRepo {
	return &BusinessModuleRepo{BaseRepo: &BaseRepo[model.BusinessModule]{DB: db}}
}

// GetByModuleName 按仓库+模块名称查询。
func (r *BusinessModuleRepo) GetByModuleName(repoID int64, moduleName string) (*model.BusinessModule, error) {
	return r.GetByWhere(map[string]any{"repo_id": repoID, "module_name": moduleName})
}

// ListAll 获取指定仓库所有未删除模块。
func (r *BusinessModuleRepo) ListAll(repoID int64) ([]*model.BusinessModule, error) {
	var list []*model.BusinessModule
	err := withNotDeleted(r.DB).Where("repo_id = ?", repoID).Order("id asc").Find(&list).Error
	return list, err
}

// EnsureModule 确保业务模块存在（不存在则创建），返回模块记录（幂等，按仓库隔离）。
func (r *BusinessModuleRepo) EnsureModule(repoID int64, moduleName, desc string) (*model.BusinessModule, error) {
	module := &model.BusinessModule{RepoID: repoID, ModuleName: moduleName, Desc: desc}
	if err := r.DB.Where("repo_id = ? AND module_name = ?", repoID, moduleName).
		Attrs(model.BusinessModule{Desc: desc}).
		FirstOrCreate(module).Error; err != nil {
		return nil, err
	}
	return module, nil
}