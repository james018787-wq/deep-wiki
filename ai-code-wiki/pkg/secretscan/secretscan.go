// Package secretscan 代码安全扫描：正则检测硬编码密钥/密码/令牌等敏感信息。
// 纯函数实现（输入源码文本，输出发现列表），不依赖外部服务，便于测试与复用。
package secretscan

import (
	"regexp"
	"strings"
)

// Finding 单条敏感信息发现。
type Finding struct {
	File    string // 文件路径
	Line    int    // 命中行号（1基）
	Type    string // 类型（Pattern.Type）
	Risk    string // 风险等级：high/medium/low
	Secret  string // 命中的敏感值（脱敏，见 Mask）
	Snippet string // 所在行文本（脱敏）
}

// Pattern 敏感信息匹配规则。
type Pattern struct {
	Type      string // 类型标识（入库用）
	Risk      string // 风险等级
	Re        *regexp.Regexp
	Recommend string // 修复建议（入库用）
}

// Patterns 全部检测规则（按优先级排序：先高风险的强特征，避免一般规则抢占）。
var Patterns = []Pattern{
	{
		Type: "private_key", Risk: "high",
		Re:        regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
		Recommend: "私钥泄露=账号被接管。立即吊销该密钥，改用环境变量或密钥管理服务注入，禁止入库。",
	},
	{
		Type: "aws_access_key", Risk: "high",
		Re:        regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),
		Recommend: "AWS 访问密钥泄露可能导致云资源被滥用/产生巨额账单。立即轮换该密钥，改用 IAM Role / 密钥管理。",
	},
	{
		Type: "github_token", Risk: "high",
		Re:        regexp.MustCompile(`\b(ghp_|gho_|github_pat_)[A-Za-z0-9_]{20,}\b`),
		Recommend: "GitHub 令牌泄露=仓库被越权访问。立即在 GitHub 撤销该令牌，改用 GitHub App / 环境变量注入。",
	},
	{
		Type: "gitlab_token", Risk: "high",
		Re:        regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{15,}\b`),
		Recommend: "GitLab 令牌泄露。立即在 GitLab 撤销该令牌，改用 CI 变量 / 环境变量注入。",
	},
	{
		Type: "openai_key", Risk: "high",
		Re:        regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{15,}\b`),
		Recommend: "AI 平台密钥泄露可能被滥用产生费用。立即撤销并轮换，改用环境变量注入。",
	},
	{
		Type: "conn_string", Risk: "high",
		Re:        regexp.MustCompile(`(?i)\b(mysql|postgres|postgresql|mongodb|redis|amqp|mssql)://[^\s'"@]+:[^\s'"@]+@`),
		Recommend: "数据库/消息连接串含明文口令。立即改口令并改用密钥管理注入，禁止硬编码。",
	},
	{
		Type: "password", Risk: "medium",
		Re:        regexp.MustCompile(`(?i)\b\w*(password|passwd|pwd)\s*["']?\s*[:=]+\s*['"][^'"]{4,}['"]`),
		Recommend: "硬编码明文密码。改用环境变量 / 密钥管理服务注入，并轮换该密码。",
	},
	{
		Type: "stripe_key", Risk: "high",
		Re:        regexp.MustCompile(`\bsk_(live|test)_[A-Za-z0-9]{20,}\b`),
		Recommend: "Stripe 密钥泄露可能被用于盗刷/篡改支付。立即在 Stripe Dashboard 撤销并轮换，改用环境变量注入。",
	},
	{
		Type: "secret", Risk: "medium",
		Re:        regexp.MustCompile(`(?i)\b(secret|client_?secret|app_?secret)\s*["\']?\s*[:=]+\s*['"][^'"]{6,}['"]`),
		Recommend: "硬编码密钥/客户端密钥。改用环境变量注入，并轮换该密钥。",
	},
	{
		Type: "api_key", Risk: "medium",
		Re:        regexp.MustCompile(`(?i)\b(api_?key|access_?token|auth_?token|api_token)\s*["\']?\s*[:=]+\s*['"][A-Za-z0-9_\-\.]{8,}['"]`),
		Recommend: "硬编码 API 密钥/令牌。改用环境变量注入，并轮换该密钥。",
	},
	{
		Type: "jwt_token", Risk: "medium",
		Re:        regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{5,}\b`),
		Recommend: "硬编码 JWT。立即使该令牌失效，改用密钥管理注入。",
	},
	{
		Type: "cookie_session", Risk: "low",
		Re:        regexp.MustCompile(`(?i)\b(session_?secret|cookie_?secret|csrf_?secret)\s*["\']?\s*[:=]+\s*['"][^'"]{6,}['"]`),
		Recommend: "会话/CSRF 密钥建议独立配置并经环境变量注入。",
	},
}

// Mask 脱敏：保留前 6 位与末 2 位，中间以 **** 代替（足够识别，不泄露）。
func Mask(s string) string {
	r := []rune(s)
	if len(r) <= 8 {
		return "****"
	}
	return string(r[:6]) + "****" + string(r[len(r)-2:])
}

// Scan 扫描单个文件内容，返回全部敏感信息发现（含行号与脱敏行文本）。
func Scan(content, file string) []*Finding {
	var out []*Finding
	lines := strings.Split(content, "\n")
	for _, p := range Patterns {
		for _, m := range p.Re.FindAllString(content, -1) {
			line := lineOf(content, m)
			snippet := ""
			if line > 0 && line <= len(lines) {
				snippet = strings.TrimSpace(lines[line-1])
				snippet = p.Re.ReplaceAllString(snippet, Mask(m)) // 行文本同样脱敏
			}
			out = append(out, &Finding{
				File:    file,
				Line:    line,
				Type:    p.Type,
				Risk:    p.Risk,
				Secret:  Mask(m),
				Snippet: snippet,
			})
		}
	}
	return out
}

// Recommendation 返回某类型规则对应的修复建议。
func Recommendation(typ string) string {
	for _, p := range Patterns {
		if p.Type == typ {
			return p.Recommend
		}
	}
	return ""
}

// lineOf 计算首个匹配位置所在行号（1基）。
func lineOf(content, match string) int {
	idx := strings.Index(content, match)
	if idx < 0 {
		return 0
	}
	return 1 + strings.Count(content[:idx], "\n")
}
