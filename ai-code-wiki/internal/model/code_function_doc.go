package model

import "time"

// CodeFunctionDoc 函数业务文档主表，对应 code_function_doc。
// 最小切片单元为单个函数，一条记录对应一个函数。
type CodeFunctionDoc struct {
	ID               int64      `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RepoID           int64      `gorm:"column:repo_id;not null;uniqueIndex:idx_file_func" json:"repo_id"`                    // 所属仓库id
	ModuleName       string     `gorm:"column:module_name;size:64;not null" json:"module_name"`                              // 所属业务模块
	FilePath         string     `gorm:"column:file_path;size:512;not null;uniqueIndex:idx_file_func" json:"file_path"`       // 源码文件路径
	FuncName         string     `gorm:"column:func_name;size:128;not null;uniqueIndex:idx_file_func" json:"func_name"`       // 函数名称
	SourceCode       string     `gorm:"column:source_code;type:text" json:"source_code"`                                               // 函数源码片段
	Summary          string     `gorm:"column:summary;type:text" json:"summary"`                                                       // 一句话业务摘要
	InputDesc        string     `gorm:"column:input_desc;type:text" json:"input_desc"`                                                 // 入参说明
	OutputDesc       string     `gorm:"column:output_desc;type:text" json:"output_desc"`                                               // 返回值说明
	ProcessFlow      string     `gorm:"column:process_flow;type:text" json:"process_flow"`                                             // 业务执行流程
	RelyModules      string     `gorm:"column:rely_modules;type:text" json:"rely_modules"`                                             // 依赖模块json数组
	RiskPoint        string     `gorm:"column:risk_point;type:text" json:"risk_point"`                                                 // 业务风险点
	OriginAutoDoc    string     `gorm:"column:origin_auto_doc;type:text" json:"origin_auto_doc"`                                       // 原始AI自动生成文档，永久保存，不可覆盖
	ContentSource    int8       `gorm:"column:content_source;not null;default:1" json:"content_source"`                               // 内容来源：1=AI自动生成 2=人工校正
	SourceCodeChanged int8      `gorm:"column:source_code_changed;default:0" json:"source_code_changed"`                               // 源码已更新、文档待复核标记
	LastEditUser     string     `gorm:"column:last_edit_user;size:64;default:''" json:"last_edit_user"`                               // 最后操作人
	LastEditTime     *time.Time `gorm:"column:last_edit_time" json:"last_edit_time"`                                                   // 人工校正时间
	CreateTime       time.Time  `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime       time.Time  `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted        int8       `gorm:"column:is_deleted;default:0;uniqueIndex:idx_file_func" json:"is_deleted"` // 逻辑删除标记
}

// TableName 指定表名。
func (CodeFunctionDoc) TableName() string {
	return "code_function_doc"
}
