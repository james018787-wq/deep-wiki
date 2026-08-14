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