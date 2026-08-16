-- ai-code-wiki 函数级调用边（M1 迭代影响分析地基）迁移脚本
-- 适用：P0 多仓库支持已上线的运行中数据库。
-- 执行前请备份：docker exec ai-code-wiki-mysql mysqldump -uroot -pWiki@2026 ai_code_wiki > /tmp/backup.sql

CREATE TABLE IF NOT EXISTS `function_call_edge` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL COMMENT '所属仓库id',
  `caller_module` varchar(64) NOT NULL COMMENT '调用方模块',
  `caller_file` varchar(512) NOT NULL COMMENT '调用方文件',
  `caller_func` varchar(128) NOT NULL COMMENT '调用方函数',
  `callee_module` varchar(64) NOT NULL COMMENT '被调模块',
  `callee_file` varchar(512) DEFAULT '' COMMENT '被调文件（同包调用解析阶段未知）',
  `callee_func` varchar(128) NOT NULL COMMENT '被调函数（跨包为限定名，如 user.GetUser）',
  `call_kind` tinyint NOT NULL DEFAULT 1 COMMENT '1同包调用 2跨包调用',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_edge_caller` (`repo_id`,`caller_file`(191),`caller_func`,`callee_module`,`callee_func`,`call_kind`,`is_deleted`),
  KEY `idx_edge_callee` (`repo_id`,`callee_module`,`callee_func`),
  KEY `idx_edge_file` (`repo_id`,`caller_file`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='函数级调用边（迭代影响分析地基，AST自动解析）';