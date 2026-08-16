package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/git"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/secretscan"

	"gorm.io/gorm"
)

// ScanSummary 安全扫描汇总（扫描文件数 + 各级别发现数）。
type ScanSummary struct {
	RepoID       int64 `json:"repo_id"`
	ScannedFiles int   `json:"scanned_files"`
	Total        int64 `json:"total"`
	High         int64 `json:"high"`
	Medium       int64 `json:"medium"`
	Low          int64 `json:"low"`
}

// SecretScanService 代码安全扫描：正则检测硬编码密钥/密码，自动生成漏洞记录。
// 支持按仓库全量扫描与解析任务增量扫描（变更文件）。
type SecretScanService struct {
	db       *gorm.DB
	repoRepo *repo.CodeRepoRepo
	findRepo *repo.SecretFindingRepo
	gitCfg   *config.GitConfig
}

// NewSecretScanService 构建安全扫描服务。
func NewSecretScanService(db *gorm.DB, cfg *config.Config) *SecretScanService {
	return &SecretScanService{
		db:       db,
		repoRepo: repo.NewCodeRepoRepo(db),
		findRepo: repo.NewSecretFindingRepo(db),
		gitCfg:   &cfg.Git,
	}
}

// ScanRepo 对仓库做全量安全扫描（拉取默认分支后扫描全部跟踪文件）。
func (s *SecretScanService) ScanRepo(ctx context.Context, repoID int64) (*ScanSummary, error) {
	repoInfo, err := s.repoRepo.GetByID(repoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "仓库不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}
	if strings.TrimSpace(repoInfo.RepoURL) == "" {
		return nil, common.NewError(common.CodeInternalError, "仓库克隆地址未配置")
	}
	cloneDir := strings.TrimRight(s.gitCfg.CloneDir, "/") + "/" + repoInfo.RepoName
	if err := git.CloneOrPull(repoInfo.RepoURL, repoInfo.DefaultBranch, cloneDir, repoInfo.AuthToken); err != nil {
		return nil, common.WrapError(common.CodeInternalError, "拉取代码失败", err)
	}
	files, err := git.ListTrackedFiles(cloneDir)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "列出仓库文件失败", err)
	}
	return s.scanFiles(ctx, repoInfo, cloneDir, files)
}

// ScanFiles 增量扫描指定文件集合（解析任务对变更文件调用，best-effort）。
func (s *SecretScanService) ScanFiles(ctx context.Context, repoInfo *model.CodeRepo, cloneDir string, files []string) (*ScanSummary, error) {
	return s.scanFiles(ctx, repoInfo, cloneDir, files)
}

// scanFiles 核心：扫描文件集合，upsert 发现，陈旧 open 发现标记为已修复。
func (s *SecretScanService) scanFiles(ctx context.Context, repoInfo *model.CodeRepo, cloneDir string, files []string) (*ScanSummary, error) {
	repoID := repoInfo.ID
	currentIDs := make(map[int64]struct{})
	scanned := 0

	for _, file := range files {
		if !scannableFile(file) {
			continue
		}
		content, ok := s.readFileLimited(cloneDir, file)
		if !ok {
			continue
		}
		scanned++
		for _, f := range secretscan.Scan(content, file) {
			// 按 文件+行+类型 幂等 upsert
			existing, err := s.findRepo.GetBySignature(repoID, file, f.Line, f.Type)
			if err == nil {
				currentIDs[existing.ID] = struct{}{}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				logger.Warn(ctx, "查询安全发现失败 file=%s: %v", file, err)
				continue
			}
			finding := &model.CodeSecretFinding{
				RepoID:      repoID,
				FilePath:    file,
				Line:        f.Line,
				SecretType:  f.Type,
				RiskLevel:   f.Risk,
				SecretValue: f.Secret,
				Snippet:     f.Snippet,
				Recommend:   secretscan.Recommendation(f.Type),
				Status:      "open",
			}
			if err := s.findRepo.Create(finding); err != nil {
				logger.Warn(ctx, "写入安全发现失败 file=%s: %v", file, err)
				continue
			}
			currentIDs[finding.ID] = struct{}{}
		}
	}

	// 陈旧处理：上次 open、本次未再命中的发现 → 已修复
	open, err := s.findRepo.ListOpenByRepo(repoID)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询历史发现失败", err)
	}
	var staleIDs []int64
	for _, o := range open {
		if _, ok := currentIDs[o.ID]; !ok {
			staleIDs = append(staleIDs, o.ID)
		}
	}
	if err := s.findRepo.MarkFixed(repoID, staleIDs); err != nil {
		return nil, common.WrapError(common.CodeInternalError, "标记修复失败", err)
	}

	total, high, medium, low, err := s.findRepo.CountByRepo(repoID)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "统计发现失败", err)
	}
	return &ScanSummary{RepoID: repoID, ScannedFiles: scanned, Total: total, High: high, Medium: medium, Low: low}, nil
}

// List 分页查询发现列表（状态/风险过滤）。
func (s *SecretScanService) List(ctx context.Context, repoID int64, status, risk string, page, pageSize int) ([]*model.CodeSecretFinding, int64, error) {
	_ = ctx
	list, total, err := s.findRepo.ListByRepo(repoID, strings.TrimSpace(status), strings.TrimSpace(risk), page, pageSize)
	if err != nil {
		return nil, 0, common.WrapError(common.CodeInternalError, "查询安全发现失败", err)
	}
	return list, total, nil
}

// UpdateStatus 更新发现状态（open / fixed / false_positive）。
func (s *SecretScanService) UpdateStatus(ctx context.Context, id int64, status string) error {
	_ = ctx
	status = strings.TrimSpace(status)
	switch status {
	case "open", "fixed", "false_positive":
	default:
		return common.NewError(common.CodeBadRequest, "status 仅支持 open/fixed/false_positive")
	}
	if _, err := s.findRepo.GetByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewError(common.CodeNotFound, "发现记录不存在")
		}
		return common.WrapError(common.CodeInternalError, "查询发现失败", err)
	}
	if err := s.findRepo.UpdateFields(id, map[string]any{"status": status}); err != nil {
		return common.WrapError(common.CodeInternalError, "更新发现状态失败", err)
	}
	return nil
}

// readFileLimited 读取文件内容（跳过超大文件，返回 ok=false 表示跳过）。
func (s *SecretScanService) readFileLimited(cloneDir, file string) (string, bool) {
	full := filepath.Join(cloneDir, file)
	info, err := os.Stat(full)
	if err != nil {
		return "", false
	}
	if info.Size() > 2*1024*1024 { // >2MB 跳过，避免大文件拖慢扫描
		return "", false
	}
	content, err := git.ReadFile(cloneDir, file)
	if err != nil {
		return "", false
	}
	if strings.ContainsRune(content, '\x00') { // 二进制文件跳过
		return "", false
	}
	return content, true
}

// scannableFile 判断文件是否值得扫描（跳过二进制扩展名与常见产物目录）。
var skipExts = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".ico": {}, ".svg": {}, ".webp": {}, ".bmp": {},
	".pdf": {}, ".zip": {}, ".gz": {}, ".tar": {}, ".jar": {}, ".war": {}, ".class": {}, ".so": {},
	".dll": {}, ".exe": {}, ".bin": {}, ".woff": {}, ".woff2": {}, ".ttf": {}, ".eot": {}, ".mp4": {}, ".mp3": {},
	".lock": {}, ".sum": {}, ".min.js": {}, ".min.css": {}, ".map": {},
}

func scannableFile(file string) bool {
	lower := strings.ToLower(file)
	ext := ""
	if i := strings.LastIndex(lower, "."); i >= 0 {
		ext = lower[i:]
	}
	if _, skip := skipExts[ext]; skip {
		return false
	}
	// 产物目录跳过（node_modules 等一般不提交，兜底）
	for _, d := range []string{"node_modules/", "vendor/", "dist/", "build/", ".git/"} {
		if strings.HasPrefix(lower, d) || strings.Contains(lower, "/"+d) {
			return false
		}
	}
	return true
}
