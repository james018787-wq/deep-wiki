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

// RepoService 代码仓库注册业务逻辑（多仓库支持）。
type RepoService struct {
	repoRepo *repo.CodeRepoRepo
}

// NewRepoService 构建仓库注册服务。
func NewRepoService(db *gorm.DB) *RepoService {
	return &RepoService{repoRepo: repo.NewCodeRepoRepo(db)}
}

// RegisterRepoReq 注册代码仓库入参。
type RegisterRepoReq struct {
	RepoName      string `json:"repo_name" binding:"required"` // 仓库名（全局唯一，克隆目录/{repo_name}）
	RepoURL       string `json:"repo_url" binding:"required"`  // 克隆地址（https/ssh 均可）
	DefaultBranch string `json:"default_branch"`               // 默认分支（默认 main，用于增量 diff 基线）
	Description   string `json:"description"`                  // 仓库说明
}

// Register 注册代码仓库（幂等）：按仓库名不存在则创建，已存在则返回现有记录。
func (s *RepoService) Register(ctx context.Context, req *RegisterRepoReq) (*model.CodeRepo, error) {
	_ = ctx
	name := strings.TrimSpace(req.RepoName)
	url := strings.TrimSpace(req.RepoURL)
	if name == "" || url == "" {
		return nil, common.NewError(common.CodeBadRequest, "仓库名与克隆地址不能为空")
	}
	branch := strings.TrimSpace(req.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	info, err := s.repoRepo.EnsureRepo(name, url, branch, strings.TrimSpace(req.Description))
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "注册仓库失败", err)
	}
	return info, nil
}

// List 获取全部未删除仓库（含停用，按 id 升序），供仓库管理页展示与启停操作。
// 文档/任务页等业务选择器只取启用仓库，由前端按 status 过滤。
func (s *RepoService) List(ctx context.Context) ([]*model.CodeRepo, error) {
	_ = ctx
	list, err := s.repoRepo.ListManaged()
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询仓库列表失败", err)
	}
	return list, nil
}

// SetStatusReq 启停仓库入参。
type SetStatusReq struct {
	Status int8 `json:"status" binding:"required"` // 1=启用 2=停用
}

// SetStatus 启用/停用仓库。
// 停用后无法触发解析任务，webhook 回调按仓库路由时也会被拒绝。
func (s *RepoService) SetStatus(ctx context.Context, repoID int64, req *SetStatusReq) error {
	_ = ctx
	if req.Status != common.RepoStatusEnabled && req.Status != common.RepoStatusDisabled {
		return common.NewError(common.CodeBadRequest, "status 仅支持 1=启用 / 2=停用")
	}
	if _, err := s.repoRepo.GetByID(repoID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewError(common.CodeNotFound, "仓库不存在")
		}
		return common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}
	if err := s.repoRepo.UpdateFields(repoID, map[string]any{"status": req.Status}); err != nil {
		return common.WrapError(common.CodeInternalError, "更新仓库状态失败", err)
	}
	return nil
}