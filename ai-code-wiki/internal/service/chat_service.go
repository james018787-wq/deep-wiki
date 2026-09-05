package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/pkg/chatstore"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"

	"gorm.io/gorm"
)

// genSessionID 生成随机会话id（16 字节 hex，32 字符）。
func genSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405.000")
	}
	return hex.EncodeToString(buf)
}

// truncateRunes 按 rune 截断字符串，超长追加省略号。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// 对话上下文窗口控制常量。
const (
	chatWindowMsgs    = 12 // 滑动窗口：每次带入最近 12 条消息（约 6 轮问答）
	chatRollupTrigger = 20 // 消息总数超过该值时触发滚动摘要并裁剪
	chatRollupKeep    = 12 // 触发后保留的消息数（与窗口一致）
	chatTitleMaxRunes = 20 // 会话标题截断长度
)

// ChatService 基于 Redis 会话记忆的多轮问答服务。
// 每轮：检索相关文档 + 历史窗口/滚动摘要组装上下文 -> LLM 回答 -> 落库记忆。
type ChatService struct {
	db          *gorm.DB
	search      *SearchService
	chatStore   chatstore.Store
	llmBaseURL  string
	chatTimeout time.Duration
}

// NewChatService 构建多轮问答服务。
func NewChatService(db *gorm.DB, cfg *config.Config, search *SearchService, store chatstore.Store) *ChatService {
	return &ChatService{
		db:          db,
		search:      search,
		chatStore:   store,
		llmBaseURL:  cfg.LLM.BaseURL,
		chatTimeout: llmCallTimeout(cfg.LLM.Timeout, defaultLLMTimeoutSec),
	}
}

// ChatAskReq 多轮问答入参。
type ChatAskReq struct {
	RepoID           int64  `json:"repo_id" binding:"required"` // 所属仓库id（按库隔离检索）
	SessionID        string `json:"session_id"`                 // 会话id，为空则新建会话
	Query            string `json:"query" binding:"required"`   // 用户输入
	ForceModel       string `json:"force_model"`                // 可选：强制指定模型
	ForceHighQuality bool   `json:"force_high_quality"`         // 可选：仅用高配模型
}

// ChatAskResult 多轮问答返回。
type ChatAskResult struct {
	SessionID     string              `json:"session_id"`
	Answer        string              `json:"answer"`
	ReferenceList []ReferenceDoc      `json:"reference_list"`
	UsedModel     string              `json:"used_model"`
	Cost          float64             `json:"cost"`
	History       []chatstore.Message `json:"history"` // 最近若干条，供前端渲染
}

// Ask 多轮问答主流程。
func (s *ChatService) Ask(ctx context.Context, req *ChatAskReq) (*ChatAskResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, common.NewError(common.CodeBadRequest, "请输入问题")
	}

	now := chatstore.NowUnix()
	meta, err := s.loadOrCreateSession(req, query, now)
	if err != nil {
		return nil, err
	}

	// step1: RAG 检索相关文档（跨模块扩充），无结果也继续（可基于历史对话作答）
	recalled, err := s.search.RetrieveRelatedDocs(ctx, req.RepoID, query)
	if err != nil {
		return nil, err
	}
	contextPrompt := buildContextPrompt(recalled)

	// step2: 读取会话记忆（滚动摘要 + 最近窗口）
	recent, err := s.chatStore.Messages(meta.SessionID, chatWindowMsgs)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "读取会话历史失败", err)
	}

	// step3: 组装带记忆的 prompt 并调用 LLM
	system := "你是一名代码知识库智能助手。请优先依据「相关业务文档」回答；" +
		"同时结合「历史对话」理解上下文与用户的追问（如'那这个呢''具体怎么做'），" +
		"回答要准确、具体、简洁；文档不足以回答时如实说明，不要编造。"
	user := buildChatUserPrompt(meta.Summary, recent, contextPrompt, query)

	estimated := estimateTokens(user)
	sched, err := chatLLM(ctx, s.llmBaseURL, s.chatTimeout, system, user,
		req.ForceModel, req.ForceHighQuality, estimated, UsageScenarioChat)
	if err != nil {
		return nil, err
	}

	// step4: 落库本轮记忆（用户问 + 助手答）
	if err := s.chatStore.AppendMessage(meta.SessionID, chatstore.Message{Role: "user", Content: query, Ts: now}); err != nil {
		logger.Error(ctx, "[chat] 保存用户消息失败: %v", err)
	}
	if err := s.chatStore.AppendMessage(meta.SessionID, chatstore.Message{Role: "assistant", Content: sched.Answer, Ts: now}); err != nil {
		logger.Error(ctx, "[chat] 保存助手消息失败: %v", err)
	}

	// step5: 窗口溢出时滚动摘要（best-effort，失败保留旧摘要）
	s.rollupIfNeeded(ctx, meta.SessionID, meta.Summary)

	// 更新元信息：消息数 + 最近活跃时间
	cnt, _ := s.chatStore.Count(meta.SessionID)
	meta.MessageCount = cnt
	meta.UpdatedAt = now
	_ = s.chatStore.SaveMeta(meta)

	logger.Info(ctx, "[chat] 多轮问答完成 session=%s repo_id=%d used_model=%s cost=%.6f context_docs=%d history_msgs=%d",
		meta.SessionID, req.RepoID, sched.UsedModel, sched.Cost, len(recalled), len(recent))

	return &ChatAskResult{
		SessionID:     meta.SessionID,
		Answer:        sched.Answer,
		ReferenceList: toReferenceList(recalled),
		UsedModel:     sched.UsedModel,
		Cost:          sched.Cost,
		History:       recent,
	}, nil
}

// ListSessions 列出指定仓库的会话（按更新时间倒序）。
func (s *ChatService) ListSessions(ctx context.Context, repoID int64) ([]*chatstore.SessionMeta, error) {
	list, err := s.chatStore.ListSessions(repoID)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "读取会话列表失败", err)
	}
	return list, nil
}

// History 返回会话全部消息（时间正序）。
func (s *ChatService) History(ctx context.Context, sessionID string) ([]chatstore.Message, error) {
	msgs, err := s.chatStore.Messages(sessionID, 0)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "读取会话历史失败", err)
	}
	return msgs, nil
}

// DeleteSession 删除指定会话（元信息 + 全部消息，幂等）。
func (s *ChatService) DeleteSession(ctx context.Context, sessionID string) error {
	_ = ctx
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return common.NewError(common.CodeBadRequest, "session_id 不能为空")
	}
	if err := s.chatStore.DeleteSession(sessionID); err != nil {
		return common.WrapError(common.CodeInternalError, "删除会话失败", err)
	}
	logger.Info(ctx, "[chat] 会话已删除 session=%s", sessionID)
	return nil
}

// loadOrCreateSession 读取已有会话或新建会话。
func (s *ChatService) loadOrCreateSession(req *ChatAskReq, query string, now int64) (*chatstore.SessionMeta, error) {
	if req.SessionID != "" {
		m, err := s.chatStore.GetMeta(req.SessionID)
		if err != nil {
			return nil, common.WrapError(common.CodeInternalError, "读取会话失败", err)
		}
		if m != nil {
			if m.RepoID != req.RepoID {
				return nil, common.NewError(common.CodeBadRequest, "会话不属于当前仓库，请切换仓库或新建会话")
			}
			return m, nil
		}
	}
	sid := req.SessionID
	if sid == "" {
		sid = genSessionID()
	}
	meta := &chatstore.SessionMeta{
		SessionID: sid,
		RepoID:    req.RepoID,
		Title:     truncateRunes(query, chatTitleMaxRunes),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.chatStore.SaveMeta(meta); err != nil {
		return nil, common.WrapError(common.CodeInternalError, "创建会话失败", err)
	}
	return meta, nil
}

// rollupIfNeeded 消息数超阈值时，把窗口之外更早的对话压进滚动摘要并裁剪窗口。
func (s *ChatService) rollupIfNeeded(ctx context.Context, sessionID, oldSummary string) {
	cnt, err := s.chatStore.Count(sessionID)
	if err != nil || cnt <= chatRollupTrigger {
		return
	}
	all, err := s.chatStore.Messages(sessionID, 0)
	if err != nil {
		return
	}
	overflow := all[:len(all)-chatRollupKeep]
	if len(overflow) == 0 {
		return
	}
	var sb strings.Builder
	for _, m := range overflow {
		sb.WriteString(m.Role + ": " + m.Content + "\n")
	}
	newSummary := oldSummary
	if res, err := chatLLM(ctx, s.llmBaseURL, s.chatTimeout, "你是对话摘要助手，把对话压缩为一段要点，保留关键事实（函数名、模块、结论），控制在 200 字内。",
		"既有摘要：\n"+oldSummary+"\n\n待压缩的新对话：\n"+sb.String(), "", false, estimateTokens(sb.String()), UsageScenarioRollup); err == nil {
		newSummary = strings.TrimSpace(res.Answer)
	}
	_ = s.chatStore.SaveMeta(&chatstore.SessionMeta{SessionID: sessionID, Summary: newSummary})
	_ = s.chatStore.Trim(sessionID, chatRollupKeep)
}

// buildChatUserPrompt 组装带记忆的用户 prompt。
func buildChatUserPrompt(summary string, recent []chatstore.Message, contextPrompt, query string) string {
	var sb strings.Builder
	sb.WriteString("[历史对话摘要]\n")
	if strings.TrimSpace(summary) == "" {
		sb.WriteString("（无）\n")
	} else {
		sb.WriteString(summary + "\n")
	}
	if len(recent) > 0 {
		sb.WriteString("\n[最近对话]\n")
		for _, m := range recent {
			sb.WriteString(m.Role + ": " + truncate(m.Content, 300) + "\n")
		}
	}
	if strings.TrimSpace(contextPrompt) != "" {
		sb.WriteString("\n" + contextPrompt + "\n")
	}
	sb.WriteString("\n[当前问题]\n" + query + "\n")
	return sb.String()
}
