package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// git 外部命令超时控制：
// 仓库克隆/拉取可能因网络慢而耗时较长，diff 查询应较快。
// 超时兜底避免外部 git 命令卡死导致异步任务 worker 永久阻塞。
const (
	clonePullTimeout = 10 * time.Minute
	diffTimeout      = 60 * time.Second
)

// CloneOrPull 克隆或拉取代码仓库（带超时，防止网络异常卡死）。
// 本地目录不存在则执行 git clone；已存在则执行 git pull。
// token 非空时注入 http.extraheader（AUTHORIZATION: Bearer <token>），支持 HTTPS 私有仓库鉴权；
// 令牌通过 GIT_CONFIG_COUNT 环境变量注入，避免出现在进程参数中泄露。
func CloneOrPull(repoURL, branch, localDir, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), clonePullTimeout)
	defer cancel()

	if _, err := os.Stat(localDir); os.IsNotExist(err) {
		// 目录不存在：克隆仓库。
		// 注意：不能使用 --single-branch —— 增量 diff 需要默认分支(origin/{default})
		// 作为对比基线，单分支克隆会导致 origin/{default} 缺失。
		if err := os.MkdirAll(filepath.Dir(localDir), 0o755); err != nil {
			return fmt.Errorf("创建仓库父目录失败: %w", err)
		}
		args := []string{"clone", "--branch", branch, repoURL, localDir}
		cmd := exec.CommandContext(ctx, "git", args...)
		withAuthEnv(cmd, token)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// 目录已存在：拉取最新代码。
	// 先 fetch 全部分支（保证默认分支 ref 可用作 diff 基线），再硬对齐到目标分支，
	// 避免 pull 产生合并提交导致 diff 基线漂移。
	cmd := exec.CommandContext(ctx, "git", "fetch", "--all", "--prune")
	cmd.Dir = localDir
	withAuthEnv(cmd, token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
	}
	reset := exec.CommandContext(ctx, "git", "reset", "--hard", "origin/"+branch)
	reset.Dir = localDir
	if out, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// withAuthEnv 通过 GIT_CONFIG_COUNT 注入 http.extraheader 凭据（不落盘、不进进程参数）。
func withAuthEnv(cmd *exec.Cmd, token string) {
	if token == "" {
		return
	}
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: Bearer "+token,
	)
}

// LsRemote 校验远程仓库可用性：克隆地址可达 + 指定分支存在（git ls-remote，带鉴权与超时）。
// 本地路径/无鉴权仓库亦可校验；token 为空时不注入凭据。
func LsRemote(repoURL, branch, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"ls-remote", "--heads", "--exit-code", repoURL}
	if strings.TrimSpace(branch) != "" {
		args = append(args, strings.TrimSpace(branch))
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	withAuthEnv(cmd, token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git ls-remote 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GetDiffFiles 获取两个 commit 之间变更的文件列表（带超时）。
// 调用 git diff --name-only，返回相对仓库根目录的文件路径列表。
func GetDiffFiles(repoPath, oldCommit, newCommit string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", oldCommit, newCommit)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// FileExists 判断仓库内相对路径文件是否存在（含路径穿越校验）。
// 用于区分"文件整体删除"与普通读取失败，支撑幽灵文档清理。
func FileExists(repoPath, relPath string) bool {
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return false
	}
	full := filepath.Join(root, relPath)
	if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && !info.IsDir()
}

// ReadFileAtCommit 读取仓库内相对路径文件在指定 commit 的内容（如 origin/main），
// 用于对比变更前后函数签名（API 变更检测）。
func ReadFileAtCommit(repoPath, commit, relPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
	defer cancel()

	ref := strings.TrimSpace(commit)
	if ref == "" {
		return "", fmt.Errorf("commit 不能为空")
	}
	if err := validateRelPath(repoPath, relPath); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+relPath)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git show %s:%s 失败: %v", ref, relPath, err)
	}
	return string(out), nil
}

// ListTrackedFiles 列出仓库内被 git 跟踪的文件（排除 .git 内部），支持 glob 匹配相对路径。
// 用于测试用例圈定：扫描 *_test.go 是否引用受影响函数。
func ListTrackedFiles(repoPath string, patterns ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
	defer cancel()

	args := append([]string{"ls-files", "-z"}, patterns...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files 失败: %v", err)
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name = strings.TrimSpace(name); name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// validateRelPath 校验相对路径合法且不越出仓库根目录。
func validateRelPath(repoPath, relPath string) error {
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("解析仓库根目录失败: %w", err)
	}
	full := filepath.Join(root, relPath)
	if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("非法文件路径: %s", relPath)
	}
	return nil
}

// ReadFile 读取仓库内相对路径文件内容。
// 通过路径校验防止越出仓库根目录。
func ReadFile(repoPath, relPath string) (string, error) {
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("解析仓库根目录失败: %w", err)
	}
	full := filepath.Join(root, relPath)

	// 安全校验：解析后路径必须位于仓库根目录内
	if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("非法文件路径: %s", relPath)
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	return string(data), nil
}
