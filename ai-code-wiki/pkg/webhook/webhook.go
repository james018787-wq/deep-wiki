// Package webhook 代码托管平台（GitLab / Gitee）webhook 回调解析与签名校验。
// 业务只依赖本包的标准化事件结构 PushEvent，不感知具体平台 payload 差异。
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Provider 代码托管平台。
type Provider string

const (
	ProviderGitLab Provider = "gitlab"
	ProviderGitee  Provider = "gitee"
)

// zeroCommit 分支删除时平台推送的 after 全零 commit id。
const zeroCommit = "0000000000000000000000000000000000000000"

// PushEvent 标准化后的代码推送事件（分支 push）。
type PushEvent struct {
	Provider     Provider // 来源平台：gitlab / gitee
	RepoURL      string   // 仓库 HTTP 克隆地址（git_http_url）
	Branch       string   // 推送分支（已去除 refs/heads/ 前缀）
	BeforeCommit string   // push 前 commit id
	AfterCommit  string   // push 后 commit id（当前 HEAD）
	IsTag        bool     // tag 推送（按约定过滤，不触发解析）
	IsDelete     bool     // 分支删除推送（after 全零，不触发解析）
}

// gitPushPayload GitLab / Gitee 分支 push payload 公共字段。
// 两个平台字段高度一致，共用一份结构解析。
type gitPushPayload struct {
	ObjectKind string `json:"object_kind"` // GitLab：push / tag_push
	HookName   string `json:"hook_name"`   // Gitee：push_hooks / tag_push_hooks
	Ref        string `json:"ref"`         // refs/heads/{branch} 或 refs/tags/{tag}
	Before     string `json:"before"`      // 前 commit id
	After      string `json:"after"`       // 后 commit id（删除分支时为全零）
	Repository struct {
		URL        string `json:"url"`
		GitHTTPURL string `json:"git_http_url"`
		GitSSHURL  string `json:"git_ssh_url"`
		HTMLURL    string `json:"html_url"`
	} `json:"repository"`
	Project struct {
		GitHTTPURL string `json:"git_http_url"`
		GitSSHURL  string `json:"git_ssh_url"`
		HTMLURL    string `json:"html_url"`
	} `json:"project"`
}

// VerifyToken 校验平台 Token 请求头（GitLab X-Gitlab-Token / Gitee X-Gitee-Token）。
// 使用常量时间比较，避免时序侧信道。
func VerifyToken(provided, secret string) bool {
	if secret == "" {
		return true // 未配置密钥：跳过鉴权（开发环境）
	}
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

// VerifyGiteeSignature 校验 Gitee HMAC-SHA256 签名。
//
// Gitee 签名规则：
//   - X-Gitee-Timestamp：请求时间戳；
//   - X-Gitee-Signature：hex(HMAC-SHA256(key=WEBHOOK_SECRET, data=timestamp + "\n" + WEBHOOK_SECRET))。
func VerifyGiteeSignature(secret, timestamp, signature string) bool {
	if secret == "" {
		return true
	}
	if timestamp == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "\n" + secret))
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(expected)), []byte(strings.ToLower(signature))) == 1
}

// ParsePush 解析 GitLab / Gitee push 回调 payload，提取仓库地址、分支、前后 commit id。
//
// 识别规则：
//   - GitLab：object_kind=push（分支） / tag_push（tag）；
//   - Gitee：hook_name=push_hooks（分支） / tag_push_hooks（tag）。
//
// tag 推送与分支删除会标记在返回值上（IsTag / IsDelete），由调用方决定跳过。
func ParsePush(body []byte) (*PushEvent, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("请求体为空")
	}
	var p gitPushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("payload JSON 解析失败: %w", err)
	}

	ev := &PushEvent{}
	switch p.ObjectKind {
	case "push":
		ev.Provider = ProviderGitLab
	case "tag_push":
		ev.Provider = ProviderGitLab
		ev.IsTag = true
	}
	if ev.Provider == "" {
		switch p.HookName {
		case "push_hooks":
			ev.Provider = ProviderGitee
		case "tag_push_hooks":
			ev.Provider = ProviderGitee
			ev.IsTag = true
		}
	}
	if ev.Provider == "" {
		return nil, fmt.Errorf("未知的推送事件（object_kind=%q hook_name=%q）", p.ObjectKind, p.HookName)
	}

	// 仓库地址：优先取 git_http_url，兜底其他字段
	ev.RepoURL = firstNonEmpty(
		p.Repository.GitHTTPURL,
		p.Project.GitHTTPURL,
		p.Repository.URL,
		p.Repository.GitSSHURL,
		p.Project.GitSSHURL,
		p.Repository.HTMLURL,
		p.Project.HTMLURL,
	)

	// ref 解析：refs/heads/{branch} 为分支推送，refs/tags/{tag} 为 tag 推送
	if p.Ref != "" && !ev.IsTag {
		if strings.HasPrefix(p.Ref, "refs/tags/") {
			ev.IsTag = true
		} else {
			ev.Branch = strings.TrimPrefix(p.Ref, "refs/heads/")
		}
	}

	ev.BeforeCommit = p.Before
	ev.AfterCommit = p.After

	// 分支删除：after 为全零 commit id
	if !ev.IsTag && ev.AfterCommit == zeroCommit {
		ev.IsDelete = true
	}

	if ev.RepoURL == "" {
		return nil, fmt.Errorf("未解析到仓库地址")
	}
	if !ev.IsTag && !ev.IsDelete && ev.Branch == "" {
		return nil, fmt.Errorf("未解析到推送分支（ref=%q）", p.Ref)
	}
	return ev, nil
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}