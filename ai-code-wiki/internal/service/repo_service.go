package service

import (
	"context"
	"errors"
	"strings"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/git"

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
	AuthToken     string `json:"auth_token"`                   // 私有仓库访问令牌（HTTPS Bearer，加密存储）
}

// Register 注册代码仓库（幂等）：按仓库名不存在则创建，已存在则返回现有记录。
// 注册前对克隆地址做可用性校验（git ls-remote：地址可达 + 默认分支存在），失败即报错。
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

	// 仓库可用性校验：地址可达 + 默认分支存在（重注册未带令牌时用已有令牌校验）
	effectiveToken := strings.TrimSpace(req.AuthToken)
	if effectiveToken == "" {
		if existing, err := s.repoRepo.GetByRepoName(name); err == nil && existing != nil {
			effectiveToken = existing.AuthToken
		}
	}
	if err := verifyRepoUsable(url, branch, effectiveToken); err != nil {
		return nil, err
	}

	info, err := s.repoRepo.EnsureRepo(name, url, branch, strings.TrimSpace(req.Description))
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "注册仓库失败", err)
	}
	// 仅当提交了新令牌才更新（避免幂等重注册覆盖已有令牌）。
	if strings.TrimSpace(req.AuthToken) != "" {
		if err := s.repoRepo.SetAuthToken(info.ID, strings.TrimSpace(req.AuthToken)); err != nil {
			return nil, common.WrapError(common.CodeInternalError, "保存仓库令牌失败", err)
		}
	}
	return info, nil
}

// verifyRepoUsable 校验仓库地址可用（git ls-remote：地址可达 + 分支存在）。
func verifyRepoUsable(url, branch, token string) error {
	if err := git.LsRemote(url, branch, token); err != nil {
		return common.NewError(common.CodeBadRequest, "仓库可用性校验失败（地址不可达、分支不存在或令牌无效）: "+err.Error())
	}
	return nil
}

// UpdateRepoReq 编辑仓库入参。
type UpdateRepoReq struct {
	RepoName      string `json:"repo_name" binding:"required"` // 仓库名（全局唯一）
	RepoURL       string `json:"repo_url" binding:"required"`  // 克隆地址
	DefaultBranch string `json:"default_branch"`               // 默认分支（diff 基线）
	Description   string `json:"description"`                  // 仓库说明
}

// Update 编辑仓库基本信息。
// 注意：修改 repo_name 会改变克隆目录（{GIT_CLONE_DIR}/{repo_name}），下次解析将重新克隆。
func (s *RepoService) Update(ctx context.Context, repoID int64, req *UpdateRepoReq) (*model.CodeRepo, error) {
	_ = ctx
	name := strings.TrimSpace(req.RepoName)
	url := strings.TrimSpace(req.RepoURL)
	if name == "" || url == "" {
		return nil, common.NewError(common.CodeBadRequest, "仓库名与克隆地址不能为空")
	}
	existing, err := s.repoRepo.GetByID(repoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "仓库不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}
	// 仓库名唯一性校验（改名时不能与其它仓库冲突）
	if name != existing.RepoName {
		if other, err := s.repoRepo.GetByRepoName(name); err == nil {
			if other != nil && other.ID != repoID {
				return nil, common.NewError(common.CodeBadRequest, "仓库名已存在")
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.WrapError(common.CodeInternalError, "校验仓库名失败", err)
		}
	}
	branch := strings.TrimSpace(req.DefaultBranch)
	if branch == "" {
		branch = existing.DefaultBranch
		if branch == "" {
			branch = "main"
		}
	}
	// 仓库可用性校验（新地址/分支必须可达，用当前令牌）
	if err := verifyRepoUsable(url, branch, existing.AuthToken); err != nil {
		return nil, err
	}
	if err := s.repoRepo.UpdateFields(repoID, map[string]any{
		"repo_name":      name,
		"repo_url":       url,
		"default_branch": branch,
		"description":    strings.TrimSpace(req.Description),
	}); err != nil {
		return nil, common.WrapError(common.CodeInternalError, "更新仓库失败", err)
	}
	return s.repoRepo.GetByID(repoID)
}

// List 获取全部未删除仓库（含停用，按 id 升序），供仓库管理页展示与启停操作。
// 文档/任务页等业务选择器只取启用仓库，由前端按 status 过滤。
func (s *RepoService) List(ctx context.Context) ([]*model.CodeRepo, error) {
	_ = ctx
	list, err := s.repoRepo.ListManaged()
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询仓库列表失败", err)
	}
	for _, m := range list {
		m.HasToken = m.AuthToken != "" // 密文非空即视为已配置，且不外泄明文
	}
	return list, nil
}

// ClearToken 清除仓库访问令牌（撤销鉴权）。
func (s *RepoService) ClearToken(ctx context.Context, repoID int64) error {
	_ = ctx
	if _, err := s.repoRepo.GetByID(repoID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewError(common.CodeNotFound, "仓库不存在")
		}
		return common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}
	if err := s.repoRepo.SetAuthToken(repoID, ""); err != nil {
		return common.WrapError(common.CodeInternalError, "清除仓库令牌失败", err)
	}
	return nil
}

// SetToken 设置/更新仓库访问令牌（加密存储）。
func (s *RepoService) SetToken(ctx context.Context, repoID int64, token string) error {
	_ = ctx
	token = strings.TrimSpace(token)
	if token == "" {
		return common.NewError(common.CodeBadRequest, "访问令牌不能为空")
	}
	if _, err := s.repoRepo.GetByID(repoID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewError(common.CodeNotFound, "仓库不存在")
		}
		return common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}
	if err := s.repoRepo.SetAuthToken(repoID, token); err != nil {
		return common.WrapError(common.CodeInternalError, "保存仓库令牌失败", err)
	}
	return nil
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
