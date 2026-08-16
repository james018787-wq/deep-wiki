-- 仓库访问令牌（HTTPS Bearer，加密存储），支持私有仓库克隆/拉取鉴权
ALTER TABLE code_repo ADD COLUMN auth_token VARCHAR(512) NOT NULL DEFAULT '' COMMENT '仓库访问令牌（加密存储）' AFTER description;