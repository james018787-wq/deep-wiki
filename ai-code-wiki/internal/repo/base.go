package repo

import (
	"gorm.io/gorm"
)

// BaseRepo 通用基础仓库模板，封装常见单表 CRUD 操作。
// 各业务仓库可通过内嵌本结构体复用通用能力。
type BaseRepo[T any] struct {
	DB *gorm.DB
}

// Create 新增记录。
func (r *BaseRepo[T]) Create(m *T) error {
	return r.DB.Create(m).Error
}

// Update 按主键更新非零字段。
func (r *BaseRepo[T]) Update(m *T) error {
	return r.DB.Model(m).Updates(m).Error
}

// UpdateFields 按主键更新指定字段（map 方式，可更新零值字段）。
func (r *BaseRepo[T]) UpdateFields(id int64, fields map[string]any) error {
	return r.DB.Model(new(T)).Where("id = ?", id).Updates(fields).Error
}

// Delete 逻辑删除（is_deleted = 1）。
func (r *BaseRepo[T]) Delete(id int64) error {
	return r.DB.Model(new(T)).Where("id = ?", id).Update("is_deleted", 1).Error
}

// HardDelete 物理删除。
func (r *BaseRepo[T]) HardDelete(id int64) error {
	return r.DB.Where("id = ?", id).Delete(new(T)).Error
}

// GetByID 按主键查询单条记录（排除已删除）。
func (r *BaseRepo[T]) GetByID(id int64) (*T, error) {
	var m T
	if err := withNotDeleted(r.DB).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetByWhere 按条件查询单条记录（排除已删除）。
func (r *BaseRepo[T]) GetByWhere(where map[string]any) (*T, error) {
	var m T
	if err := withNotDeleted(r.DB).Where(where).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByWhere 按条件查询列表（排除已删除，支持排序与分页）。
func (r *BaseRepo[T]) ListByWhere(where map[string]any, order string, page, pageSize int) ([]*T, int64, error) {
	var list []*T
	var total int64
	query := withNotDeleted(r.DB).Model(new(T)).Where(where)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		query = query.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	if order != "" {
		query = query.Order(order)
	}
	if err := query.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// CountByWhere 按条件统计数量（排除已删除）。
func (r *BaseRepo[T]) CountByWhere(where map[string]any) (int64, error) {
	var total int64
	err := withNotDeleted(r.DB).Model(new(T)).Where(where).Count(&total).Error
	return total, err
}