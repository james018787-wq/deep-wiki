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

// GetByModuleName 按模块名称查询。
func (r *BusinessModuleRepo) GetByModuleName(moduleName string) (*model.BusinessModule, error) {
	return r.GetByWhere(map[string]any{"module_name": moduleName})
}

// ListAll 获取所有未删除模块。
func (r *BusinessModuleRepo) ListAll() ([]*model.BusinessModule, error) {
	var list []*model.BusinessModule
	err := withNotDeleted(r.DB).Order("id asc").Find(&list).Error
	return list, err
}

// EnsureModule 确保业务模块存在（不存在则创建），返回模块记录（幂等）。
func (r *BusinessModuleRepo) EnsureModule(moduleName, desc string) (*model.BusinessModule, error) {
	module := &model.BusinessModule{ModuleName: moduleName, Desc: desc}
	if err := r.DB.Where("module_name = ?", moduleName).
		Attrs(model.BusinessModule{Desc: desc}).
		FirstOrCreate(module).Error; err != nil {
		return nil, err
	}
	return module, nil
}