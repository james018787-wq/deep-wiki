-- 增量迁移：登录鉴权（user / user_token）
-- 已运行过 init.sql 的环境执行本脚本；新环境 init.sql 已包含，无需重复执行。
CREATE TABLE IF NOT EXISTS `user` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `username` varchar(64) NOT NULL COMMENT '登录名（唯一）',
  `password_hash` varchar(128) NOT NULL COMMENT 'bcrypt 密码哈希',
  `nickname` varchar(64) DEFAULT '' COMMENT '显示名',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '状态：1正常 0停用',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint NOT NULL DEFAULT 0 COMMENT '逻辑删除标记',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='系统登录用户';

CREATE TABLE IF NOT EXISTS `user_token` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `token` varchar(64) NOT NULL COMMENT '登录令牌（随机 hex）',
  `user_id` bigint NOT NULL COMMENT '所属用户id',
  `expire_at` datetime NOT NULL COMMENT '过期时间',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_token` (`token`),
  KEY `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='登录令牌表（支持多实例 + 主动登出失效）';