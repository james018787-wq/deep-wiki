package model

import "time"

// 调用边类型：同包调用 / 跨包调用。
const (
	CallKindIntraPackage int8 = 1 // 同包调用（简单标识符，CalleeFunc 为函数名）
	CallKindCrossPackage int8 = 2 // 跨包调用（SelectorExpr，CalleeFunc 为限定名，如 user.GetUser）
)

// FunctionCallEdge 函数级调用边（迭代影响分析的地基）。
// 由 AST 自动解析生成，按仓库隔离；每次任务针对变更文件重建（先删后插）。
// 通过 caller 正查（A 调用了谁）与 callee 反查（谁调用了 B）支撑影响传播。
type FunctionCallEdge struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RepoID       int64     `gorm:"column:repo_id;not null" json:"repo_id"` // 仓库ID（知识库隔离）
	CallerModule string    `gorm:"column:caller_module;size:64;not null" json:"caller_module"`
	CallerFile   string    `gorm:"column:caller_file;size:512;not null" json:"caller_file"`
	CallerFunc   string    `gorm:"column:caller_func;size:128;not null" json:"caller_func"`
	CalleeModule string    `gorm:"column:callee_module;size:64;not null" json:"callee_module"`
	CalleeFile   string    `gorm:"column:callee_file;size:512;default:''" json:"callee_file"` // 同包调用在解析阶段通常未知，留空
	CalleeFunc   string    `gorm:"column:callee_func;size:128;not null" json:"callee_func"`
	CallKind     int8      `gorm:"column:call_kind;default:1" json:"call_kind"` // 1同包 2跨包
	CreateTime   time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime   time.Time `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted    int8      `gorm:"column:is_deleted;default:0" json:"is_deleted"` // 逻辑删除标记
}

// TableName 指定表名。
func (FunctionCallEdge) TableName() string {
	return "function_call_edge"
}
