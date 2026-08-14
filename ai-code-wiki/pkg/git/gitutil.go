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
func CloneOrPull(repoURL, branch, localDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), clonePullTimeout)
	defer cancel()

	if _, err := os.Stat(localDir); os.IsNotExist(err) {
		// 目录不存在：克隆仓库（只拉取指定分支）
		if err := os.MkdirAll(filepath.Dir(localDir), 0o755); err != nil {
			return fmt.Errorf("创建仓库父目录失败: %w", err)
		}
		args := []string{"clone", "--branch", branch, "--single-branch", repoURL, localDir}
		out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// 目录已存在：拉取最新代码
	cmd := exec.CommandContext(ctx, "git", "pull", "origin", branch)
	cmd.Dir = localDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull 失败: %v, 输出: %s", err, strings.TrimSpace(string(out)))
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