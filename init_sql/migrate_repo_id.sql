-- ai-code-wiki 多仓库支持（P0）迁移脚本
-- 适用：已有运行中数据库（已存在 6 张业务表且有数据）。
-- 步骤：建 code_repo 表 -> 注册 testrepo(id=1) -> 各表加 repo_id -> 存量数据回填 repo_id=1 -> 唯一索引改复合。
-- 执行前请备份：docker exec ai-code-wiki-mysql mysqldump -uroot -pWiki@2026 ai_code_wiki > /tmp/backup.sql

-- 1. 代码仓库注册表
CREATE TABLE IF NOT EXISTS `code_repo` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `repo_name` varchar(64) NOT NULL COMMENT '仓库名（全局唯一，克隆目录/{repo_name}）',
  `repo_url` varchar(512) NOT NULL COMMENT '克隆地址',
  `default_branch` varchar(128) NOT NULL DEFAULT 'main' COMMENT '默认分支（增量diff基线）',
  `description` varchar(512) DEFAULT '' COMMENT '仓库说明',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1启用 2停用',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_repo_name` (`repo_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='代码仓库注册表';

-- 2. 注册存量仓库 testrepo（幂等；repo_url 按实际克隆地址调整）
INSERT INTO `code_repo` (`repo_name`, `repo_url`, `default_branch`, `description`, `status`)
SELECT 'testrepo', '/app/testrepo', 'main', '本地测试仓库', 1
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `code_repo` WHERE `repo_name` = 'testrepo');

-- 3. 各业务表加 repo_id 列
ALTER TABLE `business_module`    ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;
ALTER TABLE `code_function_doc`  ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;
ALTER TABLE `module_relation`    ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;
ALTER TABLE `doc_modify_log`     ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;
ALTER TABLE `relation_modify_log` ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;
ALTER TABLE `code_change_log`    ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;
ALTER TABLE `task_record`        ADD COLUMN `repo_id` bigint NOT NULL DEFAULT 0 COMMENT '所属仓库id' AFTER `id`;

-- 4. 存量数据回填到 testrepo
UPDATE `business_module`    SET `repo_id` = 1;
UPDATE `code_function_doc`  SET `repo_id` = 1;
UPDATE `module_relation`    SET `repo_id` = 1;
UPDATE `doc_modify_log`     SET `repo_id` = 1;
UPDATE `relation_modify_log` SET `repo_id` = 1;
UPDATE `code_change_log`    SET `repo_id` = 1;
UPDATE `task_record`        SET `repo_id` = 1;

-- 5. 唯一索引改复合（先删旧、再加新），保证按仓库隔离不撞键
ALTER TABLE `business_module`
  DROP INDEX `idx_module_name`,
  ADD UNIQUE INDEX `idx_module_name` (`repo_id`, `module_name`);

ALTER TABLE `code_function_doc`
  DROP INDEX `idx_file_func`,
  ADD UNIQUE INDEX `idx_file_func` (`repo_id`, `file_path`, `func_name`, `is_deleted`);

ALTER TABLE `module_relation`
  DROP INDEX `idx_module_relation`,
  ADD UNIQUE INDEX `idx_module_relation` (`repo_id`, `source_module`, `target_module`, `relation_type`, `is_deleted`);

-- 6. 其余表加 repo_id 检索索引
ALTER TABLE `doc_modify_log`      ADD KEY `idx_repo_id` (`repo_id`);
ALTER TABLE `relation_modify_log` ADD KEY `idx_repo_id` (`repo_id`);
ALTER TABLE `code_change_log`     ADD KEY `idx_repo_id` (`repo_id`);
ALTER TABLE `task_record`         ADD KEY `idx_repo_id` (`repo_id`);