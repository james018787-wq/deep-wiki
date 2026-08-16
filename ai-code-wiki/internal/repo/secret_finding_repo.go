package repo

import (
	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// SecretFindingRepo 代码安全扫描发现仓库。
type SecretFindingRepo struct {
	*BaseRepo[model.CodeSecretFinding]
}

// NewSecretFindingRepo 构建安全扫描发现仓库。
func NewSecretFindingRepo(db *gorm.DB) *SecretFindingRepo {
	return &SecretFindingRepo{BaseRepo: &BaseRepo[model.CodeSecretFinding]{DB: db}}
}

// ListByRepo 按仓库分页查询发现（支持状态/风险过滤），时间倒序。
func (r *SecretFindingRepo) ListByRepo(repoID int64, status, risk string, page, pageSize int) ([]*model.CodeSecretFinding, int64, error) {
	query := withNotDeleted(r.DB).Model(&model.CodeSecretFinding{})
	query = query.Where("repo_id = ?", repoID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if risk != "" {
		query = query.Where("risk_level = ?", risk)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.CodeSecretFinding
	if err := query.Order("risk_level DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListOpenByRepo 查询某仓库全部 open 状态发现（用于增量扫描的陈旧标记）。
func (r *SecretFindingRepo) ListOpenByRepo(repoID int64) ([]*model.CodeSecretFinding, error) {
	var list []*model.CodeSecretFinding
	err := withNotDeleted(r.DB).
		Where("repo_id = ? AND status = ?", repoID, "open").
		Find(&list).Error
	return list, err
}

// GetBySignature 按 仓库+文件+行号+类型 查询 open 状态发现（扫描去重/更新）。
func (r *SecretFindingRepo) GetBySignature(repoID int64, filePath string, line int, secretType string) (*model.CodeSecretFinding, error) {
	var m model.CodeSecretFinding
	err := withNotDeleted(r.DB).
		Where("repo_id = ? AND file_path = ? AND line = ? AND secret_type = ? AND status = ?",
			repoID, filePath, line, secretType, "open").
		First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkFixed 将未再命中的 open 发现标记为已修复。
func (r *SecretFindingRepo) MarkFixed(repoID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.DB.Model(&model.CodeSecretFinding{}).
		Where("repo_id = ? AND id IN ?", repoID, ids).
		Update("status", "fixed").Error
}

// CountByRepo 统计某仓库发现（按状态/风险可选）。
func (r *SecretFindingRepo) CountByRepo(repoID int64) (total, high, medium, low int64, err error) {
	base := withNotDeleted(r.DB).Model(&model.CodeSecretFinding{}).Where("repo_id = ?", repoID)
	if err = base.Count(&total).Error; err != nil {
		return
	}
	for _, risk := range []string{"high", "medium", "low"} {
		var n int64
		if err = base.Session(&gorm.Session{}).Where("risk_level = ?", risk).Count(&n).Error; err != nil {
			return
		}
		switch risk {
		case "high":
			high = n
		case "medium":
			medium = n
		case "low":
			low = n
		}
	}
	return
}
