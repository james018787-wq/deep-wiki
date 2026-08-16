-- 函数声明起始行号（答案引用定位 / 源码跳转定位）
ALTER TABLE code_function_doc ADD COLUMN func_line INT NOT NULL DEFAULT 0 COMMENT '函数声明起始行号（答案引用定位）' AFTER func_name;