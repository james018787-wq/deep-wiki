-- 代码安全扫描发现（硬编码密钥/密码）
CREATE TABLE IF NOT EXISTS `code_secret_finding` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL COMMENT '所属仓库id',
  `file_path` varchar(512) NOT NULL COMMENT '文件路径',
  `line` int NOT NULL DEFAULT 0 COMMENT '命中行号（1基）',
  `secret_type` varchar(32) NOT NULL COMMENT '类型：aws_key/github_token/password/...',
  `risk_level` varchar(16) NOT NULL DEFAULT 'medium' COMMENT '风险等级：high/medium/low',
  `secret_value` varchar(256) NOT NULL DEFAULT '' COMMENT '命中的敏感值（脱敏存储）',
  `snippet` text COMMENT '所在行文本（脱敏）',
  `recommendation` text COMMENT '修复建议',
  `status` varchar(16) NOT NULL DEFAULT 'open' COMMENT '状态：open/fixed/false_positive',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_repo` (`repo_id`),
  KEY `idx_repo_status` (`repo_id`,`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='代码安全扫描发现表';