package repo

import (
	"strings"

	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// CodeRepoRepo 代码仓库注册表仓库。
type CodeRepoRepo struct {
	*BaseRepo[model.CodeRepo]
}

// NewCodeRepoRepo 构建仓库注册表仓库。
func NewCodeRepoRepo(db *gorm.DB) *CodeRepoRepo {
	return &CodeRepoRepo{BaseRepo: &BaseRepo[model.CodeRepo]{DB: db}}
}

// EnsureRepo 按仓库名注册：不存在则创建（幂等），已存在则返回现有记录。
func (r *CodeRepoRepo) EnsureRepo(repoName, repoURL, defaultBranch, desc string) (*model.CodeRepo, error) {
	repo := &model.CodeRepo{RepoName: repoName}
	if err := r.DB.Where("repo_name = ?", repoName).
		Attrs(model.CodeRepo{RepoURL: repoURL, DefaultBranch: defaultBranch, Description: desc}).
		FirstOrCreate(repo).Error; err != nil {
		return nil, err
	}
	return repo, nil
}

// GetByRepoName 按仓库名查询（排除已删除）。
func (r *CodeRepoRepo) GetByRepoName(repoName string) (*model.CodeRepo, error) {
	return r.GetByWhere(map[string]any{"repo_name": repoName})
}

// GetByRepoURL 按仓库地址查询（归一化匹配，排除已删除）。
// 用于 webhook 回调按项目仓库路由到对应注册仓库。
func (r *CodeRepoRepo) GetByRepoURL(repoURL string) (*model.CodeRepo, error) {
	var list []*model.CodeRepo
	if err := r.DB.Where("is_deleted = ?", 0).Find(&list).Error; err != nil {
		return nil, err
	}
	for _, m := range list {
		if normalizeRepoURL(m.RepoURL) == normalizeRepoURL(repoURL) {
			return m, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// ListManaged 获取全部未删除仓库（含停用，按 id 升序），供仓库管理页启停操作使用。
// 业务选择器（文档/任务）仅需启用仓库，由前端按 status 过滤。
func (r *CodeRepoRepo) ListManaged() ([]*model.CodeRepo, error) {
	var list []*model.CodeRepo
	err := r.DB.Where("is_deleted = ?", 0).Order("id asc").Find(&list).Error
	return list, err
}

// normalizeRepoURL 归一化仓库地址（忽略协议与末尾 .git），用于跨仓库匹配。
func normalizeRepoURL(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "git@")
	s = strings.Replace(s, ":", "/", 1) // git@host:path → host/path
	return s
}