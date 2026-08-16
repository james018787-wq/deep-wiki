package service

import (
	"context"
	"errors"
	"strings"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"

	"gorm.io/gorm"
)

// RelationService 模块依赖知识图谱业务逻辑。
type RelationService struct {
	relationRepo *repo.ModuleRelationRepo
	repoRepo     *repo.CodeRepoRepo // 代码仓库注册表（校验 repo_id 有效性）
	db           *gorm.DB
}

// NewRelationService 构建依赖关系服务。
func NewRelationService(db *gorm.DB) *RelationService {
	return &RelationService{
		relationRepo: newRelationRepo(db),
		repoRepo:     repo.NewCodeRepoRepo(db),
		db:           db,
	}
}

// ListRelationReq 查询模块依赖入参。
type ListRelationReq struct {
	RepoID    int64  `json:"repo_id" binding:"required"` // 所属仓库id
	Module    string `json:"module"`                     // 模块名称
	Direction string `json:"direction"`                  // out=下游(out) / in=上游(in)
}

// ListRelations 查询模块上下游依赖。
//
// 业务规则（严格遵守）：
//  1. direction 仅支持 out/in；
//  2. 查询模块依赖 = AST 自动识别关系 ∪ 人工添加关系
//     （当前实现直接从 module_relation 表查询，该表已聚合两类来源）。
func (s *RelationService) ListRelations(ctx context.Context, req *ListRelationReq) ([]*model.ModuleRelation, error) {
	_ = ctx
	if req.RepoID <= 0 {
		return nil, common.NewError(common.CodeBadRequest, "仓库不能为空")
	}
	module := strings.TrimSpace(req.Module)
	if module == "" {
		return nil, common.NewError(common.CodeBadRequest, "模块名称不能为空")
	}
	direction := req.Direction
	if direction == "" {
		direction = "out"
	}
	if direction != "out" && direction != "in" {
		return nil, common.NewError(common.CodeBadRequest, "direction 仅支持 out/in")
	}
	list, err := s.relationRepo.ListByModule(req.RepoID, module, direction)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询模块依赖失败", err)
	}
	return list, nil
}

// AddRelationReq 手动新增模块依赖关系入参。
type AddRelationReq struct {
	RepoID       int64  `json:"repo_id" binding:"required"` // 所属仓库id
	SourceModule string `json:"source_module" binding:"required"`
	TargetModule string `json:"target_module" binding:"required"`
	RelationType int8   `json:"relation_type" binding:"required"` // 1=同步调用 2=异步MQ
	Creator      string `json:"creator" binding:"required"`
	Remark       string `json:"remark"`
}

// AddRelation 手动新增模块依赖关系。
//
// 业务规则（严格遵守）：
//  1. source 固定为 2（人工手动添加）。
//  2. source_module 与 target_module 不能为空、不能相同，relation_type 仅支持 1/2。
//  3. 已存在的依赖关系（含人工/AST）不允许重复新增，返回 CodeConflict。
//  4. 新增关系与 relation_modify_log（operate_type=1）操作日志在同一事务内原子写入。
//  5. 自动任务不会清理人工新增依赖。
func (s *RelationService) AddRelation(ctx context.Context, req *AddRelationReq) error {
	_ = ctx
	source := strings.TrimSpace(req.SourceModule)
	target := strings.TrimSpace(req.TargetModule)
	if source == "" || target == "" {
		return common.NewError(common.CodeBadRequest, "源模块与目标模块不能为空")
	}
	if source == target {
		return common.NewError(common.CodeBadRequest, "源模块与目标模块不能相同")
	}
	if req.RelationType != common.RelationSyncCall && req.RelationType != common.RelationAsyncMQ {
		return common.NewError(common.CodeBadRequest, "relation_type 仅支持 1/2")
	}
	if strings.TrimSpace(req.Creator) == "" {
		return common.NewError(common.CodeBadRequest, "创建人不能为空")
	}

	// 重复校验：该依赖关系（含 AST/人工来源）已存在时禁止重复新增
	existing, err := s.relationRepo.GetByRelation(req.RepoID, source, target, req.RelationType)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return common.WrapError(common.CodeInternalError, "查询依赖关系失败", err)
	}
	if existing != nil {
		return common.NewError(common.CodeConflict, "该依赖关系已存在")
	}

	// 新增关系 + 操作日志：事务内原子写入，保证"同时写日志"
	err = s.db.Transaction(func(tx *gorm.DB) error {
		rel := &model.ModuleRelation{
			RepoID:       req.RepoID,
			SourceModule: source,
			TargetModule: target,
			RelationType: req.RelationType,
			Source:       common.RelationSourceManual,
			Creator:      req.Creator,
			Remark:       req.Remark,
		}
		if err := tx.Create(rel).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "新增依赖关系失败", err)
		}
		logRecord := &model.RelationModifyLog{
			RepoID:       req.RepoID,
			SourceModule: source,
			TargetModule: target,
			OperateType:  common.RelationOperateAdd,
			Operator:     req.Creator,
			Remark:       req.Remark,
		}
		if err := tx.Create(logRecord).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "写入操作日志失败", err)
		}
		return nil
	})
	return err
}

// DeleteRelationReq 删除模块依赖入参。
type DeleteRelationReq struct {
	RepoID       int64  `json:"repo_id" binding:"required"` // 所属仓库id
	SourceModule string `json:"source_module" binding:"required"`
	TargetModule string `json:"target_module" binding:"required"`
	RelationType int8   `json:"relation_type" binding:"required"` // 1=同步调用 2=异步MQ
	Operator     string `json:"operator" binding:"required"`
	Remark       string `json:"remark"`
}

// DeleteRelation 删除模块依赖。
//
// 业务规则（严格遵守）：
//  1. 按 (源模块, 目标模块, 关系类型) 定位唯一关系，不存在返回 CodeNotFound。
//  2. 删除采取逻辑删除（is_deleted=1），保留审计痕迹。
//  3. 删除与 relation_modify_log（operate_type=3）操作日志在同一事务内原子写入。
//  4. 人工删除的依赖在后续自动任务中【不会被 AST 重新添加】的标记逻辑待完善。
func (s *RelationService) DeleteRelation(ctx context.Context, req *DeleteRelationReq) error {
	_ = ctx
	source := strings.TrimSpace(req.SourceModule)
	target := strings.TrimSpace(req.TargetModule)
	if source == "" || target == "" {
		return common.NewError(common.CodeBadRequest, "源模块与目标模块不能为空")
	}
	if req.RelationType != common.RelationSyncCall && req.RelationType != common.RelationAsyncMQ {
		return common.NewError(common.CodeBadRequest, "relation_type 仅支持 1/2")
	}
	if strings.TrimSpace(req.Operator) == "" {
		return common.NewError(common.CodeBadRequest, "操作人不能为空")
	}

	// 定位待删除关系
	rel, err := s.relationRepo.GetByRelation(req.RepoID, source, target, req.RelationType)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewError(common.CodeNotFound, "依赖关系不存在")
		}
		return common.WrapError(common.CodeInternalError, "查询依赖关系失败", err)
	}

	// 逻辑删除 + 操作日志：事务内原子写入
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ModuleRelation{}).
			Where("id = ?", rel.ID).
			Update("is_deleted", common.Deleted).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "删除依赖关系失败", err)
		}
		logRecord := &model.RelationModifyLog{
			RepoID:       req.RepoID,
			SourceModule: source,
			TargetModule: target,
			OperateType:  common.RelationOperateDelete,
			Operator:     req.Operator,
			Remark:       req.Remark,
		}
		if err := tx.Create(logRecord).Error; err != nil {
			return common.WrapError(common.CodeInternalError, "写入操作日志失败", err)
		}
		return nil
	})
	return err
}