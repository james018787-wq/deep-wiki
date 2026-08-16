package repo

import (
	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// CallEdgeRepo 函数级调用边仓库。
type CallEdgeRepo struct {
	*BaseRepo[model.FunctionCallEdge]
}

// NewCallEdgeRepo 构建调用边仓库。
func NewCallEdgeRepo(db *gorm.DB) *CallEdgeRepo {
	return &CallEdgeRepo{BaseRepo: &BaseRepo[model.FunctionCallEdge]{DB: db}}
}

// ReplaceEdgesForFile 重建某个仓库单个文件的全部调用边（先物理删除该文件旧边，再批量插入新边）。
// 流水线按变更文件增量重建，保证图谱随每次迭代刷新。
func (r *CallEdgeRepo) ReplaceEdgesForFile(repoID int64, callerFile string, edges []*model.FunctionCallEdge) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("repo_id = ? AND caller_file = ?", repoID, callerFile).
			Delete(&model.FunctionCallEdge{}).Error; err != nil {
			return err
		}
		if len(edges) == 0 {
			return nil
		}
		return tx.CreateInBatches(edges, 500).Error
	})
}

// ListByCallee 反向查询：谁调用了指定函数（callee_module + callee_func 定位）。
// 迭代影响分析的核心查询：输入被改函数，输出全部调用方。
func (r *CallEdgeRepo) ListByCallee(repoID int64, calleeModule, calleeFunc string) ([]*model.FunctionCallEdge, error) {
	var list []*model.FunctionCallEdge
	err := r.DB.Where("repo_id = ? AND callee_module = ? AND callee_func = ? AND is_deleted = ?",
		repoID, calleeModule, calleeFunc, 0).Find(&list).Error
	return list, err
}

// ListByCaller 正向查询：指定函数调用了谁（caller_module + caller_func 定位）。
func (r *CallEdgeRepo) ListByCaller(repoID int64, callerModule, callerFunc string) ([]*model.FunctionCallEdge, error) {
	var list []*model.FunctionCallEdge
	err := r.DB.Where("repo_id = ? AND caller_module = ? AND caller_func = ? AND is_deleted = ?",
		repoID, callerModule, callerFunc, 0).Find(&list).Error
	return list, err
}

// ListByRepo 获取某仓库全部调用边（用于影响传播的全量遍历与图谱重建）。
func (r *CallEdgeRepo) ListByRepo(repoID int64) ([]*model.FunctionCallEdge, error) {
	var list []*model.FunctionCallEdge
	err := r.DB.Where("repo_id = ? AND is_deleted = ?", repoID, 0).Find(&list).Error
	return list, err
}
