-- ai-code-wiki 数据库初始化脚本
-- MySQL8.0 首次启动时由 docker-entrypoint-initdb.d 自动导入
-- 字符集 utf8mb4，与 docker-compose 中 mysql 服务配置保持一致

-- 代码仓库注册表（多仓库支持：仓库按 repo_name 唯一注册，业务表通过 repo_id 隔离）
CREATE TABLE IF NOT EXISTS `code_repo` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_name` varchar(64) NOT NULL COMMENT '仓库名（全局唯一，克隆目录/{repo_name}）',
  `repo_url` varchar(512) NOT NULL COMMENT '克隆地址',
  `default_branch` varchar(128) NOT NULL DEFAULT 'main' COMMENT '默认分支（增量diff基线）',
  `description` varchar(512) DEFAULT '' COMMENT '仓库说明',
  `auth_token` varchar(512) NOT NULL DEFAULT '' COMMENT '仓库访问令牌（AES-GCM 加密存储，私有仓库 HTTPS 鉴权）',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1启用 2停用',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_repo_name` (`repo_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='代码仓库注册表';

CREATE TABLE IF NOT EXISTS `business_module` (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `module_name` varchar(64) NOT NULL COMMENT '业务模块名称',
  `desc` varchar(512) DEFAULT '' COMMENT '模块说明',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_module_name` (`repo_id`,`module_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='业务模块表';

CREATE TABLE IF NOT EXISTS `code_function_doc` (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `module_name` varchar(64) NOT NULL COMMENT '所属业务模块',
  `file_path` varchar(512) NOT NULL COMMENT '源码文件路径',
  `func_name` varchar(128) NOT NULL COMMENT '函数名称',
  `func_line` int NOT NULL DEFAULT 0 COMMENT '函数声明起始行号（答案引用定位）',
  `source_code` text COMMENT '函数源码片段',
  `summary` text COMMENT '一句话业务摘要',
  `input_desc` text COMMENT '入参说明',
  `output_desc` text COMMENT '返回值说明',
  `process_flow` text COMMENT '业务执行流程',
  `rely_modules` text COMMENT '依赖模块json数组',
  `risk_point` text COMMENT '业务风险点',
  `origin_auto_doc` text COMMENT '原始AI自动生成文档，永久保存，不可覆盖',
  `content_source` tinyint NOT NULL DEFAULT 1 COMMENT '1=AI自动生成 2=人工校正',
  `source_code_changed` tinyint DEFAULT 0 COMMENT '源码已更新，文档待复核标记',
  `last_edit_user` varchar(64) DEFAULT '' COMMENT '最后操作人',
  `last_edit_time` datetime(3) DEFAULT NULL COMMENT '人工校正时间',
  `create_time` datetime(3) DEFAULT CURRENT_TIMESTAMP(3),
  `update_time` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_file_func` (`repo_id`,`file_path`,`func_name`,`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='函数业务文档主表';

CREATE TABLE IF NOT EXISTS `module_relation` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `source_module` varchar(64) NOT NULL COMMENT '源模块',
  `target_module` varchar(64) NOT NULL COMMENT '被依赖模块',
  `relation_type` tinyint NOT NULL COMMENT '1同步调用 2异步MQ事件',
  `source` tinyint NOT NULL DEFAULT 1 COMMENT '1=AST自动识别 2=人工手动添加',
  `creator` varchar(64) DEFAULT '' COMMENT '创建人',
  `remark` varchar(512) DEFAULT '' COMMENT '备注说明',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_module_relation` (`repo_id`,`source_module`,`target_module`,`relation_type`,`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模块依赖知识图谱表';

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

CREATE TABLE IF NOT EXISTS `doc_modify_log` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `doc_id` bigint NOT NULL COMMENT '关联code_function_doc主键',
  `operate_type` tinyint NOT NULL COMMENT '1编辑文档 2重置回AI原始版本',
  `before_content` text COMMENT '修改前完整文档JSON',
  `after_content` text COMMENT '修改后完整文档JSON',
  `operator` varchar(64) NOT NULL COMMENT '操作人',
  `operate_time` datetime(3) DEFAULT CURRENT_TIMESTAMP(3),
  `remark` varchar(512) DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `idx_doc_id` (`doc_id`),
  KEY `idx_repo_id` (`repo_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='文档人工校正日志';

CREATE TABLE IF NOT EXISTS `relation_modify_log` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `source_module` varchar(64) NOT NULL,
  `target_module` varchar(64) NOT NULL,
  `operate_type` tinyint NOT NULL COMMENT '1新增 2编辑 3删除',
  `operator` varchar(64) NOT NULL,
  `operate_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `remark` varchar(512) DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `idx_repo_id` (`repo_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模块依赖关系操作日志';

CREATE TABLE IF NOT EXISTS `code_change_log` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `doc_id` bigint NOT NULL COMMENT '关联文档id',
  `version` varchar(128) DEFAULT '' COMMENT '发布版本',
  `change_summary` text COMMENT '代码变更摘要',
  `business_impact` text COMMENT '业务影响范围',
  `attention` text COMMENT '上线注意事项',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_doc_id` (`doc_id`),
  KEY `idx_repo_id` (`repo_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='代码迭代变更历史记录';

CREATE TABLE IF NOT EXISTS `task_record` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id',
  `task_id` varchar(64) NOT NULL COMMENT '任务唯一标识',
  `branch` varchar(128) NOT NULL COMMENT '代码分支',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0待执行 1执行中 2成功 3失败',
  `retry_count` int NOT NULL DEFAULT 0 COMMENT '失败重试次数（队列消费失败重新投递时自增）',
  `err_msg` text COMMENT '错误信息',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `finish_time` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_task_id` (`task_id`),
  KEY `idx_repo_id` (`repo_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='代码解析任务记录表';

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
CREATE TABLE IF NOT EXISTS `llm_usage` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `model_name` varchar(64) NOT NULL COMMENT '实际使用模型',
  `scenario` varchar(32) NOT NULL COMMENT '调用场景：doc/chat/search/impact/design/func_change/rollup/requirement',
  `input_tokens` int NOT NULL DEFAULT 0 COMMENT '输入 token 数',
  `output_tokens` int NOT NULL DEFAULT 0 COMMENT '输出 token 数',
  `cost` decimal(12,6) NOT NULL DEFAULT 0 COMMENT '本次调用估算成本（元）',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_model_time` (`model_name`,`create_time`),
  KEY `idx_scenario` (`scenario`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='LLM 调用消耗明细（token/金额）';
