// Package common 存放通用工具、统一错误码与常量定义。
package common

// 业务常量定义区（按模块分节组织，便于扩展）。

// ============ 内容来源 content_source ============
const (
	ContentSourceAuto   int8 = 1 // 1=AI自动生成
	ContentSourceManual int8 = 2 // 2=人工校正
)

// ============ 源码变更复核标记 source_code_changed ============
const (
	SourceCodeUnchanged int8 = 0 // 源码未变更
	SourceCodeChanged   int8 = 1 // 源码已更新，文档待复核
)

// ============ 模块依赖关系类型 relation_type ============
const (
	RelationSyncCall int8 = 1 // 1=同步调用
	RelationAsyncMQ  int8 = 2 // 2=异步MQ事件
)

// ============ 依赖关系来源 source ============
const (
	RelationSourceAST   int8 = 1 // 1=AST自动识别
	RelationSourceManual int8 = 2 // 2=人工手动添加
)

// ============ 文档操作类型 operate_type ============
const (
	DocOperateEdit   int8 = 1 // 1=编辑文档
	DocOperateReset  int8 = 2 // 2=重置回AI原始版本
)

// ============ 依赖关系操作类型 operate_type ============
const (
	RelationOperateAdd    int8 = 1 // 1=新增
	RelationOperateEdit   int8 = 2 // 2=编辑
	RelationOperateDelete int8 = 3 // 3=删除
)

// ============ 代码解析任务状态 status ============
const (
	TaskStatusPending  int8 = 0 // 0=待执行
	TaskStatusRunning  int8 = 1 // 1=执行中
	TaskStatusSuccess  int8 = 2 // 2=成功
	TaskStatusFailed   int8 = 3 // 3=失败
)

// ============ 逻辑删除 is_deleted ============
const (
	NotDeleted int8 = 0 // 未删除
	Deleted    int8 = 1 // 已删除
)

// ============ 其他常量 ============
const (
	// 默认分页大小
	DefaultPageSize = 20
	// 最大分页大小
	MaxPageSize = 100
)