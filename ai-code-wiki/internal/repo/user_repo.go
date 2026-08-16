package repo

import (
	"time"

	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// UserRepo 用户与登录令牌数据访问。
type UserRepo struct {
	db *gorm.DB
}

// NewUserRepo 构建用户数据访问层。
func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

// CreateUser 创建用户。
func (r *UserRepo) CreateUser(u *model.User) error {
	return r.db.Create(u).Error
}

// GetByUsername 按用户名查询（含已删除校验：逻辑删除的不返回）。
func (r *UserRepo) GetByUsername(username string) (*model.User, error) {
	var u model.User
	if err := r.db.Where("username = ? AND is_deleted = 0", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateToken 写入登录令牌。
func (r *UserRepo) CreateToken(t *model.UserToken) error {
	return r.db.Create(t).Error
}

// GetTokenByValue 按令牌值查询有效令牌（附带用户信息，join user 校验用户状态）。
func (r *UserRepo) GetTokenByValue(token string) (*model.UserToken, *model.User, error) {
	var t model.UserToken
	if err := r.db.Where("token = ? AND expire_at > ?", token, time.Now()).First(&t).Error; err != nil {
		return nil, nil, err
	}
	var u model.User
	if err := r.db.Where("id = ? AND is_deleted = 0 AND status = 1", t.UserID).First(&u).Error; err != nil {
		return nil, nil, err
	}
	return &t, &u, nil
}

// DeleteToken 使令牌失效（登出）。
func (r *UserRepo) DeleteToken(token string) error {
	return r.db.Where("token = ?", token).Delete(&model.UserToken{}).Error
}

// CleanupExpiredTokens 清理过期令牌（空闲回收，返回删除条数）。
func (r *UserRepo) CleanupExpiredTokens(limit int) (int64, error) {
	res := r.db.Where("expire_at <= ?", time.Now()).Limit(limit).Delete(&model.UserToken{})
	return res.RowsAffected, res.Error
}
