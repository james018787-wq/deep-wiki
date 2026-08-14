// Package git Git 工具封装，负责代码仓库克隆、更新与变更文件获取。
package git

import "os/exec"

// Client Git 操作客户端。
type Client struct {
	repoURL string // 仓库地址
	workDir string // 本地工作目录（克隆/拉取目标）
}

// NewClient 构建 Git 客户端。
func NewClient(repoURL, workDir string) *Client {
	return &Client{repoURL: repoURL, workDir: workDir}
}

// CloneOrPull 仓库不存在则克隆，已存在则拉取最新代码。
func (c *Client) CloneOrPull(branch string) error {
	// TODO(骨架)：调用 git clone / git pull，后续实现。
	_ = branch
	return nil
}

// DiffChangedFiles 获取指定分支相对基分支（main）的变更文件列表。
// 返回值为相对路径列表，供增量解析使用（仅处理 git diff 变更文件）。
func (c *Client) DiffChangedFiles(branch string) ([]string, error) {
	// TODO(骨架)：执行 git diff --name-only，后续实现。
	_ = branch
	return nil, nil
}

// CheckoutBranch 切换分支。
func (c *Client) CheckoutBranch(branch string) error {
	// TODO(骨架)：后续实现。
	_ = branch
	return nil
}

// run 执行 git 命令（通用封装）。
func (c *Client) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}