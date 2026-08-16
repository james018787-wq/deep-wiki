package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 说明：
// 本测试使用本地临时 git 仓库作为"远程"，仅走 file 协议，不拉取真实远程仓库（mock 方式）。
// 测试依赖系统 git 命令，环境缺失时自动跳过。

// requireGit 检测 git 命令，缺失则跳过。
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("环境无 git 命令，跳过: %v", err)
	}
}

// gitRun 在指定目录执行 git 命令。
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v 失败: %v, 输出: %s", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// initLocalRepo 初始化一个含 1 个提交的本地仓库，返回目录与首个 commit。
func initLocalRepo(t *testing.T) (repoDir, firstCommit string) {
	t.Helper()
	repoDir = t.TempDir()
	gitRun(t, repoDir, "init", "-b", "main")
	gitRun(t, repoDir, "config", "user.email", "test@example.com")
	gitRun(t, repoDir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "init")
	firstCommit = gitRun(t, repoDir, "rev-parse", "HEAD")
	return repoDir, firstCommit
}

// TestCloneOrPull 表格驱动：克隆新目录 / 已有目录拉取。
func TestCloneOrPull(t *testing.T) {
	requireGit(t)
	t.Parallel()

	cases := []struct {
		name      string // 用例名
		prepare   func(t *testing.T, src, dst string) // 用例前置（新建提交等）
		check     func(t *testing.T, dst string)      // 结果校验
		createDst bool                                // 目标目录是否预先存在
	}{
		{
			name: "目录不存在时克隆",
			prepare: func(t *testing.T, src, dst string) {
			},
			check: func(t *testing.T, dst string) {
				if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
					t.Fatalf("克隆后应存在 a.txt: %v", err)
				}
			},
		},
		{
			name: "目录已存在时拉取",
			prepare: func(t *testing.T, src, dst string) {
				// 上游新增提交
				if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("v2\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				gitRun(t, src, "add", ".")
				gitRun(t, src, "commit", "-m", "add b.txt")
			},
			check: func(t *testing.T, dst string) {
				if _, err := os.Stat(filepath.Join(dst, "b.txt")); err != nil {
					t.Fatalf("拉取后应存在 b.txt: %v", err)
				}
			},
			createDst: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, _ := initLocalRepo(t)
			base := t.TempDir()
			dst := filepath.Join(base, "repo")
			if tc.createDst {
				// 先克隆一次，使目标目录已存在
				if err := CloneOrPull(src, "main", dst, ""); err != nil {
					t.Fatalf("首次克隆失败: %v", err)
				}
			}
			tc.prepare(t, src, dst)
			if err := CloneOrPull(src, "main", dst, ""); err != nil {
				t.Fatalf("CloneOrPull 失败: %v", err)
			}
			tc.check(t, dst)
		})
	}
}

// TestGetDiffFiles 验证两个 commit 之间变更文件列表。
func TestGetDiffFiles(t *testing.T) {
	requireGit(t)
	t.Parallel()

	repo, first := initLocalRepo(t)
	// 修改 a.txt + 新增 b.txt
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("v1-modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "modify a, add b")
	second := gitRun(t, repo, "rev-parse", "HEAD")

	files, err := GetDiffFiles(repo, first, second)
	if err != nil {
		t.Fatalf("GetDiffFiles 失败: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("期望 2 个变更文件, got %v", files)
	}
	wantSet := map[string]bool{"a.txt": false, "b.txt": false}
	for _, f := range files {
		if _, ok := wantSet[f]; !ok {
			t.Fatalf("意外文件: %s", f)
		}
		wantSet[f] = true
	}
	for f, seen := range wantSet {
		if !seen {
			t.Fatalf("缺少变更文件: %s", f)
		}
	}
}

// TestReadFile 表格驱动：正常读取 / 路径穿越 / 文件不存在。
func TestReadFile(t *testing.T) {
	requireGit(t)
	t.Parallel()

	repo, _ := initLocalRepo(t)

	cases := []struct {
		name    string // 用例名
		relPath string // 相对路径
		want    string // 期望内容
		wantErr bool   // 期望报错
	}{
		{name: "正常读取", relPath: "a.txt", want: "v1\n"},
		{name: "路径穿越", relPath: "../secret.txt", wantErr: true},
		{name: "文件不存在", relPath: "nope.txt", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadFile(repo, tc.relPath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际成功: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("读取失败: %v", err)
			}
			if got != tc.want {
				t.Fatalf("内容不匹配: got %q want %q", got, tc.want)
			}
		})
	}
}
