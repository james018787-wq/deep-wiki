-- LLM 调用消耗明细（token/金额）统计
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
