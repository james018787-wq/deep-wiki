// Package repo 数据访问层，负责与 MySQL 交互。
package repo

import (
	"fmt"
	"time"

	"ai-code-wiki/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// MySQL 驱动级超时配置（外部调用兜底，避免数据库不可用时请求永久挂起）。
// timeout=建连超时；read/writeTimeout=单次 I/O 读写超时。与健康检查、LLM/向量库超时配套。
const (
	dbDialTimeout  = 5 * time.Second
	dbReadTimeout  = 30 * time.Second
	dbWriteTimeout = 30 * time.Second
)

// InitDB 初始化全局 GORM 实例。
func InitDB(cfg *config.MySQLConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local&timeout=%s&readTimeout=%s&writeTimeout=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Charset,
		dbDialTimeout, dbReadTimeout, dbWriteTimeout)

	engine, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := engine.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	db = engine
	return engine, nil
}

// GetDB 获取全局 GORM 实例。
func GetDB() *gorm.DB {
	return db
}

// withNotDeleted 注入逻辑删除过滤条件（is_deleted = 0）。
func withNotDeleted(d *gorm.DB) *gorm.DB {
	return d.Where("is_deleted = ?", 0)
}