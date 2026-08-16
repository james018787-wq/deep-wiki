package model

import "time"

// User 系统登录用户。
type User struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"column:username;size:64;not null;uniqueIndex:idx_username" json:"username"` // 登录名（唯一）
	PasswordHash string    `gorm:"column:password_hash;size:128;not null" json:"-"`                           // bcrypt 哈希，不对外输出
	Nickname     string    `gorm:"column:nickname;size:64;default:''" json:"nickname"`                        // 显示名
	Status       int8      `gorm:"column:status;default:1" json:"status"`                                     // 状态：1正常 0停用
	CreateTime   time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime   time.Time `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted    int8      `gorm:"column:is_deleted;default:0" json:"is_deleted"` // 逻辑删除标记
}

// TableName 指定表名。
func (User) TableName() string { return "user" }

// UserToken 登录令牌（Bearer token，随机 32 位 hex，MySQL 存储支持多实例 + 主动失效）。
type UserToken struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Token      string    `gorm:"column:token;size:64;not null;uniqueIndex:idx_token" json:"token"`
	UserID     int64     `gorm:"column:user_id;not null;index:idx_user_id" json:"user_id"`
	ExpireAt   time.Time `gorm:"column:expire_at;not null" json:"expire_at"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
}

// TableName 指定表名。
func (UserToken) TableName() string { return "user_token" }
