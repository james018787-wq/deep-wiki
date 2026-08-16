package service

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/astgo"
	"ai-code-wiki/pkg/astphp"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/filefilter"
	"ai-code-wiki/pkg/git"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/taskqueue"
	"ai-code-wiki/pkg/vector"
	"ai-code-wiki/pkg/webhook"

	"gorm.io/gorm"
)

// TaskService 代码解析任务业务逻辑。
type TaskService struct {
	db           *gorm.DB
	taskRepo     *repo.TaskRecordRepo
	docRepo      *repo.CodeFunctionDocRepo
	moduleRepo   *repo.BusinessModuleRepo
	repoRepo     *repo.CodeRepoRepo       // 代码仓库注册表
	callEdgeRepo *repo.CallEdgeRepo       // 函数级调用边（迭代影响分析地基）
	relationRepo *repo.ModuleRelationRepo // 模块依赖图谱（跨包调用聚合 source=1）
	gitCfg       *config.GitConfig        // git 克隆目录根配置（每个仓库独立子目录）
	llmBaseURL   string                   // Python LLM 服务地址（LLM_SERVICE_URL）
	llmTimeout   time.Duration            // LLM 生成文档调用超时（LLM_TIMEOUT，默认 60s）
	maxCalls     int                      // 单次解析任务 LLM 生成调用预算上限（0=不限）
	vc           vector.VectorClient      // 向量存储抽象（业务不感知 chroma/milvus）
	queue        taskqueue.TaskQueue      // 异步任务队列（提交入口，消费由独立 Worker 完成）
	fileFilter   *filefilter.FileFilter   // 文件过滤规则（跳过测试/依赖/非业务代码）
}

// pipelineStats 单次解析任务的成本统计（缓存命中 / LLM 调用 / 预算跳过）。
type pipelineStats struct {
	total    int // 处理函数总数
	cacheHit int // 源码未变更、跳过 LLM 生成的函数数
	llmCalls int // 实际 LLM 生成调用次数
	skipped  int // 超预算跳过的函数数
}

// NewTaskService 构建任务服务。
// vc 为 nil 时跳过向量同步（向量引擎未配置/初始化失败场景）。
func NewTaskService(db *gorm.DB, cfg *config.Config, vc vector.VectorClient, queue taskqueue.TaskQueue) *TaskService {
	return &TaskService{
		db:           db,
		taskRepo:     newTaskRepo(db),
		docRepo:      newDocRepo(db),
		moduleRepo:   repo.NewBusinessModuleRepo(db),
		repoRepo:     repo.NewCodeRepoRepo(db),
		callEdgeRepo: repo.NewCallEdgeRepo(db),
		relationRepo: repo.NewModuleRelationRepo(db),
		gitCfg:       &cfg.Git,
		llmBaseURL:   cfg.LLM.BaseURL,
		llmTimeout:   llmCallTimeout(cfg.LLM.Timeout, defaultLLMTimeoutSec),
		maxCalls:     cfg.LLM.MaxCallsPerTask,
		vc:           vc,
		queue:        queue,
		fileFilter: filefilter.New(filefilter.Config{
			IgnoreDirs:   filefilter.SplitList(cfg.Filter.IgnoreDirs),
			IgnoreFileRe: filefilter.SplitList(cfg.Filter.IgnoreFileRe),
			AllowExts:    filefilter.SplitList(cfg.Filter.AllowExts),
		}),
	}
}

// TriggerTaskReq 触发代码解析任务入参（CI 回调）。
type TriggerTaskReq struct {
	TaskID string `json:"task_id" binding:"required"` // 任务唯一标识
	RepoID int64  `json:"repo_id" binding:"required"` // 所属仓库id（code_repo 主键）
	Branch string `json:"branch" binding:"required"`  // 代码分支
}

// TriggerTask 触发代码解析任务。
//
// 业务规则（严格遵守）：
//  1. 记录任务到 task_record（初始状态=待执行）。
//  2. 同一 task_id 重复触发时返回冲突错误，避免重复执行。
//  3. 任务在后台 goroutine 执行，不阻塞主 HTTP 接口。
//
// 任务流水线：
//
//	接收task -> git拉取代码 -> git diff获取变更文件 -> 过滤业务代码文件（跳过测试/依赖/非业务后缀）
//	-> 按扩展名解析（.go走go ast，.php走简易正则）提取函数 -> 调用Python LLM服务生成业务文档。
func (s *TaskService) TriggerTask(ctx context.Context, req *TriggerTaskReq) (*model.TaskRecord, error) {
	_ = ctx

	// 0. 校验仓库存在且启用
	repoInfo, err := s.repoRepo.GetByID(req.RepoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeBadRequest, "仓库不存在，请先注册仓库")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}
	if repoInfo.Status != common.RepoStatusEnabled {
		return nil, common.NewError(common.CodeInvalidState, "仓库已停用，无法触发任务")
	}

	// 1. 校验 task_id 唯一性，避免重复触发
	var cnt int64
	if err := s.db.Model(&model.TaskRecord{}).Where("task_id = ?", req.TaskID).Count(&cnt).Error; err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询任务失败", err)
	}
	if cnt > 0 {
		return nil, common.NewError(common.CodeConflict, "任务已存在，请勿重复触发")
	}

	// 2. 落库任务记录（待执行）
	// 并发触发时依赖唯一索引 idx_task_id 兜底：命中唯一键冲突返回 CodeConflict，而非 500。
	record := &model.TaskRecord{
		TaskID: req.TaskID,
		RepoID: req.RepoID,
		Branch: req.Branch,
		Status: common.TaskStatusPending,
	}
	if err := s.taskRepo.Create(record); err != nil {
		if isDuplicateKeyError(err) {
			return nil, common.NewError(common.CodeConflict, "任务已存在，请勿重复触发")
		}
		return nil, common.WrapError(common.CodeInternalError, "创建任务失败", err)
	}

	// 3. 投递异步任务队列（替换直接 goroutine），由独立 consumer 后台协程消费执行
	if err := s.submitPipelineTask(req.TaskID); err != nil {
		_ = s.MarkFailed(req.TaskID, "任务投递队列失败: "+err.Error())
		return nil, common.WrapError(common.CodeInternalError, "任务投递队列失败", err)
	}
	return record, nil
}

// runPipeline 任务流水线执行入口：负责状态流转与整体错误兜底。
// 返回 error 时由消费 Worker 决定重试或标记失败（不在此处直接 MarkFailed）。
func (s *TaskService) runPipeline(record *model.TaskRecord) error {
	ctx := context.Background()

	if err := s.MarkRunning(record.TaskID); err != nil {
		logger.Warn(ctx, "任务 %s 标记执行中失败: %v", record.TaskID, err)
	}

	stats := &pipelineStats{}
	if err := s.process(ctx, record, stats); err != nil {
		logger.Error(ctx, "任务 %s 执行失败: %v", record.TaskID, err)
		return err
	}
	if err := s.MarkSuccess(record.TaskID); err != nil {
		logger.Warn(ctx, "任务 %s 标记成功失败: %v", record.TaskID, err)
	}
	// 成本统计：缓存命中率 = 跳过 LLM 生成的函数占比
	hitRate := 0.0
	if stats.total > 0 {
		hitRate = float64(stats.cacheHit) / float64(stats.total) * 100
	}
	logger.Info(ctx, "任务 %s 执行成功，成本统计: 函数总数=%d 缓存命中=%d(命中率=%.1f%%) LLM生成调用=%d 超预算跳过=%d",
		record.TaskID, stats.total, stats.cacheHit, hitRate, stats.llmCalls, stats.skipped)
	return nil
}

// HandleGitPush 处理代码托管平台 webhook 分支 push 回调。
//
// 业务规则：
//  1. 校验入参（分支/仓库），tag 推送与分支删除在前置 handler 已过滤，这里再兜底；
//  2. 按回调仓库地址匹配已注册代码仓库（code_repo），未注册则报错（需先登记仓库）；
//  3. 以「仓库+branch+after commit」生成幂等 task_id：同一次 push 重复回调
//     （平台重试）直接返回已存在任务，不重复触发；
//  4. 落库 task_record（待执行）后投递异步任务队列，执行增量解析流水线。
func (s *TaskService) HandleGitPush(ctx context.Context, event *webhook.PushEvent) (*model.TaskRecord, error) {
	if event == nil || event.IsTag || event.IsDelete {
		return nil, common.NewError(common.CodeBadRequest, "仅支持分支 push 事件")
	}
	if strings.TrimSpace(event.Branch) == "" {
		return nil, common.NewError(common.CodeBadRequest, "推送分支为空")
	}
	if strings.TrimSpace(event.RepoURL) == "" {
		return nil, common.NewError(common.CodeBadRequest, "回调仓库地址为空")
	}

	// 按回调仓库地址匹配已注册仓库（多仓库路由）
	repoInfo, err := s.repoRepo.GetByRepoURL(event.RepoURL)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeInvalidState, "仓库未登记，请先通过 /api/v1/repo/register 注册")
		}
		return nil, common.WrapError(common.CodeInternalError, "匹配仓库失败", err)
	}
	if repoInfo.Status != common.RepoStatusEnabled {
		return nil, common.NewError(common.CodeInvalidState, "仓库已停用，无法触发解析")
	}

	taskID := genWebhookTaskID(event)

	// 幂等：同一次 push 已触发过则直接返回已存在任务
	if existing, err := s.taskRepo.GetByTaskID(taskID); err == nil {
		logger.Info(ctx, "webhook push 已存在任务 %s，跳过重复触发", taskID)
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.WrapError(common.CodeInternalError, "查询任务失败", err)
	}

	record := &model.TaskRecord{
		TaskID: taskID,
		RepoID: repoInfo.ID,
		Branch: event.Branch,
		Status: common.TaskStatusPending,
	}
	if err := s.taskRepo.Create(record); err != nil {
		if isDuplicateKeyError(err) { // 并发触发兜底：命中唯一索引，返回已存在任务
			if existing, e2 := s.taskRepo.GetByTaskID(taskID); e2 == nil {
				return existing, nil
			}
		}
		return nil, common.WrapError(common.CodeInternalError, "创建任务失败", err)
	}

	// 投递任务队列（替换直接 goroutine），异步执行增量解析流水线，不阻塞 webhook 响应
	if err := s.submitPipelineTask(taskID); err != nil {
		_ = s.MarkFailed(taskID, "任务投递队列失败: "+err.Error())
		return nil, common.WrapError(common.CodeInternalError, "任务投递队列失败", err)
	}
	logger.Info(ctx, "webhook 触发解析任务成功 task_id=%s repo=%s branch=%s", taskID, repoInfo.RepoName, event.Branch)
	return record, nil
}

// submitPipelineTask 构建并投递代码解析任务到异步任务队列。
func (s *TaskService) submitPipelineTask(taskID string) error {
	msg, err := buildPipelineMessage(taskID)
	if err != nil {
		return err
	}
	return s.queue.SubmitTask(msg)
}

// genWebhookTaskID 生成幂等任务 id：仓库+branch+after commit 的 sha1 摘要。
func genWebhookTaskID(event *webhook.PushEvent) string {
	sum := sha1.Sum([]byte(event.RepoURL + "|" + event.Branch + "|" + event.AfterCommit))
	return "webhook-" + hex.EncodeToString(sum[:])[:16]
}

// process 核心流水线：git拉取 -> diff -> 过滤业务代码文件 -> AST解析 -> LLM生成文档。
// 文件过滤命中测试文件/依赖目录/非业务后缀等规则时直接跳过，不解析、不生成文档。
func (s *TaskService) process(ctx context.Context, task *model.TaskRecord, stats *pipelineStats) error {
	// 0. 解析任务所属仓库（克隆目录按仓库名隔离：{cloneRoot}/{repo_name}）
	repoInfo, err := s.repoRepo.GetByID(task.RepoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("任务所属仓库不存在 repo_id=%d", task.RepoID)
		}
		return fmt.Errorf("查询任务仓库失败: %w", err)
	}
	cloneDir := s.cloneDirFor(repoInfo)

	// 1. git 拉取/更新代码
	if strings.TrimSpace(repoInfo.RepoURL) == "" {
		return fmt.Errorf("仓库 %s 克隆地址未配置", repoInfo.RepoName)
	}
	if err := git.CloneOrPull(repoInfo.RepoURL, task.Branch, cloneDir, repoInfo.AuthToken); err != nil {
		return fmt.Errorf("拉取代码失败: %w", err)
	}

	// 2. 获取 diff 变更文件（任务分支 对比 默认分支）
	baseRef := "origin/" + repoInfo.DefaultBranch
	branchRef := "origin/" + task.Branch
	files, err := git.GetDiffFiles(cloneDir, baseRef, branchRef)
	if err != nil {
		return fmt.Errorf("获取diff变更文件失败: %w", err)
	}

	// 3. 过滤业务代码文件：扩展名 + 忽略目录 + 忽略文件正则（跳过测试/依赖/非业务代码）
	var codeFiles []string
	for _, f := range files {
		if skip, reason := s.fileFilter.ShouldSkip(f); skip {
			logger.Info(ctx, "任务 %s 跳过文件 %s：%s", task.TaskID, f, reason)
			continue
		}
		codeFiles = append(codeFiles, f)
	}
	if len(codeFiles) == 0 {
		logger.Info(ctx, "任务 %s 无业务代码文件变更（已全部被过滤规则跳过）", task.TaskID)
		return nil
	}

	// 4-5. 解析 + LLM生成文档（单文件失败不终止整体任务）
	ok := 0
	for _, file := range codeFiles {
		if err := s.processFile(cloneDir, repoInfo, file, stats); err != nil {
			logger.Warn(ctx, "任务 %s 处理文件失败 %s: %v", task.TaskID, file, err)
			continue
		}
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("所有代码文件处理均失败")
	}
	return nil
}

// cloneDirFor 计算仓库克隆目录（按仓库名隔离，避免多仓库互相覆盖）。
func (s *TaskService) cloneDirFor(repoInfo *model.CodeRepo) string {
	root := strings.TrimRight(s.gitCfg.CloneDir, "/")
	if root == "" {
		root = "/app/repo_cache"
	}
	return root + "/" + repoInfo.RepoName
}

// fileFunc 解析出的通用函数单元（函数名 + 源码片段）。
type fileFunc struct {
	Name      string // 函数名称
	StartLine int    // 函数声明起始行号（1 基）
	Code      string // 函数源码片段
}

// processFile 解析单个代码文件，逐个函数生成文档。
// 按文件后缀选择解析器：.go 走 go ast 解析，.php 走简易正则解析。
func (s *TaskService) processFile(cloneDir string, repoInfo *model.CodeRepo, file string, stats *pipelineStats) error {
	// 文件整体已删除：清空该文件全部文档（幽灵文档清理）
	if !git.FileExists(cloneDir, file) {
		if err := s.cleanupFileDocs(repoInfo, file); err != nil {
			return fmt.Errorf("清理已删除文件文档失败: %w", err)
		}
		logger.Info(context.Background(), "文件已从代码删除，清理其全部文档 %s", file)
		return nil
	}

	// 读取仓库内文件内容
	content, err := git.ReadFile(cloneDir, file)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 按扩展名分派解析器
	var funcs []fileFunc
	var goItems []astgo.FuncItem
	if strings.HasSuffix(file, ".php") {
		// PHP：简易正则解析（不引入重型解析库）
		items, err := astphp.ParsePHPFile(content)
		if err != nil {
			return fmt.Errorf("PHP解析失败: %w", err)
		}
		for _, it := range items {
			funcs = append(funcs, fileFunc{Name: it.FuncName, StartLine: it.StartLine, Code: it.Code})
		}
	} else {
		// Go：原 ast 解析逻辑（不改动）
		items, err := astgo.ParseGoFile(content)
		if err != nil {
			return fmt.Errorf("AST解析失败: %w", err)
		}
		goItems = items
		for i := range items {
			funcs = append(funcs, fileFunc{Name: items[i].FuncName, StartLine: items[i].StartLine, Code: items[i].Code})
		}
	}

	// 幽灵文档清理：代码中已删除函数对应文档自动下线（best-effort，失败不阻塞主流程）
	if err := s.cleanupGhostDocs(repoInfo, file, funcs); err != nil {
		logger.Warn(context.Background(), "幽灵文档清理失败 %s: %v", file, err)
	}

	if len(funcs) == 0 {
		return fmt.Errorf("文件内未发现函数")
	}

	// 提取并重建该文件的函数级调用边（Go 支持，PHP 重建为空清掉旧边）
	edges := extractCallEdges(repoInfo.ID, file, goItems)
	if err := s.callEdgeRepo.ReplaceEdgesForFile(repoInfo.ID, file, edges); err != nil {
		logger.Warn(context.Background(), "重建调用边失败 %s: %v", file, err)
	}
	// 跨包调用自动聚合模块级依赖关系（source=1，已有关系不重复创建）
	if err := s.syncModuleRelationsFromEdges(repoInfo.ID, edges); err != nil {
		logger.Warn(context.Background(), "聚合模块依赖关系失败 %s: %v", file, err)
	}

	// 逐个函数生成文档（单函数失败仅记录日志，继续处理）
	ok := 0
	for _, fn := range funcs {
		// 预算控制：超过单任务 LLM 调用上限后跳过剩余函数
		if s.maxCalls > 0 && stats.llmCalls >= s.maxCalls {
			stats.skipped++
			logger.Warn(context.Background(), "已达到单任务 LLM 预算上限(%d)，跳过函数 %s.%s", s.maxCalls, file, fn.Name)
			continue
		}
		if err := s.processFunc(repoInfo, file, fn, stats); err != nil {
			logger.Warn(context.Background(), "处理函数失败 %s.%s: %v", file, fn.Name, err)
			continue
		}
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("文件内无函数处理成功")
	}
	return nil
}

// cleanupGhostDocs 幽灵文档清理：库内该文件已存在文档中，凡当前代码已不存在的函数文档，一律下线。
// 下线动作 = 写删除操作日志（operate_type=3）+ 逻辑删除 + 删除向量（异步队列）。
func (s *TaskService) cleanupGhostDocs(repoInfo *model.CodeRepo, file string, funcs []fileFunc) error {
	current := make(map[string]struct{}, len(funcs))
	for _, fn := range funcs {
		if strings.TrimSpace(fn.Name) != "" {
			current[fn.Name] = struct{}{}
		}
	}
	existing, err := s.docRepo.ListByFile(repoInfo.ID, file)
	if err != nil {
		return err
	}
	for _, doc := range existing {
		if _, ok := current[doc.FuncName]; ok {
			continue
		}
		if err := s.removeGhostDoc(doc); err != nil {
			return err
		}
		logger.Info(context.Background(), "幽灵文档下线 doc_id=%d %s.%s 函数已从代码删除", doc.ID, file, doc.FuncName)
	}
	return nil
}

// cleanupFileDocs 文件整体删除时清理其全部文档。
func (s *TaskService) cleanupFileDocs(repoInfo *model.CodeRepo, file string) error {
	existing, err := s.docRepo.ListByFile(repoInfo.ID, file)
	if err != nil {
		return err
	}
	for _, doc := range existing {
		if err := s.removeGhostDoc(doc); err != nil {
			return err
		}
	}
	return nil
}

// removeGhostDoc 下线单篇幽灵文档：写删除日志 + 逻辑删除 + 投递向量删除任务。
func (s *TaskService) removeGhostDoc(doc *model.CodeFunctionDoc) error {
	if err := s.docRepo.RemoveDocWithLog(doc, "system", "函数已从代码中删除，文档自动下线"); err != nil {
		return err
	}
	msg, err := buildVectorDeleteMessage(doc.ID)
	if err != nil {
		return err
	}
	if err := s.queue.SubmitTask(msg); err != nil {
		logger.Warn(context.Background(), "向量删除任务投递失败 doc_id=%d err=%v", doc.ID, err)
	}
	return nil
}

// extractCallEdges 从 Go AST 解析结果提取函数级调用边。
//
// 规则：
//  1. CalleeSimple（简单标识符，如 ValidateOrder()）：同包调用。
//     callee_module=当前文件模块，callee_func=函数名。
//  2. Callee（SelectorExpr，如 user.GetUser()）：取首段为导入别名，
//     命中导入表且非标准库时视为跨包调用：
//     callee_module=导入路径末段（与 moduleNameFromPath 的"首段即模块"约定对应），
//     callee_func=别名后剩余限定名（GetUser 或 b.Func）。
//     未命中导入表（局部变量/结构体方法调用，如 r.Context()）直接跳过，无法解析为业务调用边。
//  3. 标准库（fmt/net/http 等）导入跳过，避免噪音边。
func extractCallEdges(repoID int64, file string, items []astgo.FuncItem) []*model.FunctionCallEdge {
	if len(items) == 0 {
		return nil
	}
	module := moduleNameFromPath(file)
	var edges []*model.FunctionCallEdge
	for i := range items {
		it := &items[i]
		caller := it.FuncName
		for _, callee := range it.CalleeSimple {
			if astgo.IsBuiltin(callee) {
				continue
			}
			edges = append(edges, &model.FunctionCallEdge{
				RepoID:       repoID,
				CallerModule: module,
				CallerFile:   file,
				CallerFunc:   caller,
				CalleeModule: module,
				CalleeFunc:   callee,
				CallKind:     model.CallKindIntraPackage,
			})
		}
		for _, call := range it.Callee {
			alias, rest := splitSelector(call)
			path, ok := it.Imports[alias]
			if !ok || isStdlibImport(path) || rest == "" {
				continue
			}
			edges = append(edges, &model.FunctionCallEdge{
				RepoID:       repoID,
				CallerModule: module,
				CallerFile:   file,
				CallerFunc:   caller,
				CalleeModule: importModule(path),
				CalleeFunc:   rest,
				CallKind:     model.CallKindCrossPackage,
			})
		}
	}
	return edges
}

// splitSelector 将限定调用表达式按首段点拆分为 别名 + 剩余限定名。
// user.GetUser → ("user", "GetUser")；a.b.Func → ("a", "b.Func")。
func splitSelector(s string) (alias, rest string) {
	idx := strings.Index(s, ".")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// isStdlibImport 判断是否为 Go 标准库导入。
// 标准库路径无域名（如 fmt、net/http），但单段路径也可能是仓库本地包（如 user、order），
// 因此采用"常见标准库白名单"判定，避免把仓库内部包误判为标准库而丢弃调用边。
func isStdlibImport(path string) bool {
	return stdlibPackages[path]
}

// stdlibPackages 常见 Go 标准库路径白名单（命中即视为标准库，跳过调用边）。
var stdlibPackages = map[string]bool{
	"fmt": true, "strings": true, "errors": true, "strconv": true, "os": true,
	"io": true, "bytes": true, "bufio": true, "time": true, "context": true,
	"sync": true, "sort": true, "math": true, "rand": true, "regexp": true,
	"log": true, "path": true, "path/filepath": true, "runtime": true,
	"reflect": true, "unicode": true, "flag": true, "hash": true,
	"encoding/json": true, "encoding/xml": true, "encoding/base64": true,
	"encoding/hex": true, "encoding/binary": true, "crypto": true,
	"crypto/sha1": true, "crypto/sha256": true, "crypto/md5": true,
	"crypto/rsa": true, "crypto/aes": true, "crypto/tls": true,
	"net": true, "net/http": true, "net/url": true, "database/sql": true,
	"mime": true, "html": true, "container/list": true, "io/ioutil": true,
	"strings2": false,
}

// importModule 从导入路径推导业务模块名（取路径末段，与仓库内目录结构约定一致）。
// github.com/foo/order → order；user → user。
func importModule(path string) string {
	segs := strings.Split(strings.TrimSuffix(path, "/"), "/")
	return segs[len(segs)-1]
}

// syncModuleRelationsFromEdges 将跨包调用边聚合为模块级依赖关系（自动识别 source=1）。
// 已存在的关系（含人工 source=2）不重复创建，避免覆盖/冲突。
func (s *TaskService) syncModuleRelationsFromEdges(repoID int64, edges []*model.FunctionCallEdge) error {
	seen := make(map[string]struct{})
	for _, e := range edges {
		if e.CallKind != model.CallKindCrossPackage {
			continue
		}
		if e.CallerModule == e.CalleeModule {
			continue
		}
		key := e.CallerModule + ">" + e.CalleeModule
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		existing, err := s.relationRepo.GetByRelation(repoID, e.CallerModule, e.CalleeModule, common.RelationSyncCall)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing != nil {
			continue
		}
		rel := &model.ModuleRelation{
			RepoID:       repoID,
			SourceModule: e.CallerModule,
			TargetModule: e.CalleeModule,
			RelationType: common.RelationSyncCall,
			Source:       common.RelationSourceAST,
		}
		if err := s.relationRepo.Create(rel); err != nil {
			return err
		}
	}
	return nil
}

// processFunc 单个函数：调用 LLM 生成文档，并按业务规则落库。
//
// 业务规则（严格遵守）：
//  0. 成本控制：函数已存在、源码未变更且无待复核标记（AI 自动文档）时，
//     直接缓存命中跳过 LLM 生成（文档仍有效）；超过单任务预算时跳过。
//  1. 函数已存在且 content_source=2（人工校正）：不覆盖当前生效文档，
//     仅更新 source_code 并置 source_code_changed=1 待复核。
//  2. origin_auto_doc 只在【首次创建】时写入，任何情况（源码变更、重新解析）禁止覆盖，
//     保证"重置回 AI 原始版本"始终可追溯到首次生成内容。
//  3. 无人工校正标记的函数：覆盖写入文档并同步向量库。
func (s *TaskService) processFunc(repoInfo *model.CodeRepo, file string, fn fileFunc, stats *pipelineStats) error {
	funcName, code := fn.Name, fn.Code
	if strings.TrimSpace(funcName) == "" {
		return fmt.Errorf("函数名为空，跳过")
	}
	stats.total++

	// 推导模块名（取文件路径首段目录）
	moduleName := moduleNameFromPath(file)

	// 登记业务模块（不存在则创建），保证 /doc/module/list 下拉有数据
	if err := s.ensureModule(repoInfo.ID, moduleName); err != nil {
		logger.Warn(context.Background(), "登记业务模块失败 module=%s: %v", moduleName, err)
	}

	// 查询函数是否已有文档（按仓库隔离），供缓存命中与规则1判断
	existing, exists := (*model.CodeFunctionDoc)(nil), false
	if doc, err := s.docRepo.GetByFileFunc(repoInfo.ID, file, funcName); err == nil {
		existing, exists = doc, true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("查询已有文档失败: %w", err)
	}

	// 规则0：成本控制 - 源码未变更的 AI 自动文档直接缓存命中，跳过 LLM 生成
	if exists && existing.ContentSource == common.ContentSourceAuto &&
		existing.SourceCodeChanged == common.SourceCodeUnchanged &&
		strings.TrimSpace(existing.SourceCode) == strings.TrimSpace(code) {
		stats.cacheHit++
		logger.Info(context.Background(), "函数 %s.%s 源码未变更，缓存命中跳过 LLM 生成", file, funcName)
		return nil
	}

	// 调用 Python LLM 服务生成标准化业务文档
	data, rawJSON, err := s.generateDoc(moduleName, file, code)
	if err != nil {
		return err
	}
	stats.llmCalls++

	// 规则1：人工校正文档不覆盖，仅更新源码与待复核标记，禁止触碰 origin_auto_doc 与生效文档
	if exists && existing.ContentSource == common.ContentSourceManual {
		if err := s.docRepo.UpdateFields(existing.ID, map[string]any{
			"source_code":         code,
			"func_line":           fn.StartLine,
			"source_code_changed": common.SourceCodeChanged, // 标记待复核
		}); err != nil {
			return fmt.Errorf("更新待复核文档失败: %w", err)
		}
		logger.Warn(context.Background(),
			"函数 %s.%s 为人工校正文档，仅置待复核标记，不覆盖生效文档与 origin_auto_doc", file, funcName)
		return nil
	}

	// 规则3：无人工校正标记，直接写入文档并同步向量
	doc := &model.CodeFunctionDoc{
		RepoID:            repoInfo.ID,
		ModuleName:        moduleName,
		FilePath:          file,
		FuncName:          funcName,
		FuncLine:          fn.StartLine,
		SourceCode:        code,
		Summary:           data.Summary,
		InputDesc:         data.InputDesc,
		OutputDesc:        data.OutputDesc,
		ProcessFlow:       data.ProcessFlow,
		RelyModules:       data.RelyModules,
		RiskPoint:         data.RiskPoint,
		OriginAutoDoc:     rawJSON,
		ContentSource:     common.ContentSourceAuto,
		SourceCodeChanged: common.SourceCodeUnchanged,
	}

	if exists {
		// 已存在 AI 文档：覆盖生效字段；origin_auto_doc 只在首次创建时写入，禁止覆盖
		if err := s.docRepo.UpdateFields(existing.ID, map[string]any{
			"module_name":         moduleName,
			"func_line":           fn.StartLine,
			"source_code":         code,
			"summary":             data.Summary,
			"input_desc":          data.InputDesc,
			"output_desc":         data.OutputDesc,
			"process_flow":        data.ProcessFlow,
			"rely_modules":        data.RelyModules,
			"risk_point":          data.RiskPoint,
			"content_source":      common.ContentSourceAuto,
			"source_code_changed": common.SourceCodeUnchanged,
		}); err != nil {
			return fmt.Errorf("更新文档失败: %w", err)
		}
		doc.ID = existing.ID
	} else {
		// 首次创建：写入 origin_auto_doc（此后永不覆盖）
		if err := s.docRepo.Create(doc); err != nil {
			return fmt.Errorf("写入文档失败: %w", err)
		}
	}

	// 同步向量库（保证检索使用最新内容）
	s.syncVector(doc)
	return nil
}

// generateDoc 调用 Python LLM 服务 /api/generate/doc 生成业务文档。
// 返回标准化文档数据与新AI文档完整json（存入 origin_auto_doc）。
func (s *TaskService) generateDoc(moduleName, filePath, codeContent string) (*llmDocData, string, error) {
	if strings.TrimSpace(s.llmBaseURL) == "" {
		return nil, "", fmt.Errorf("LLM服务地址未配置")
	}
	url := strings.TrimRight(s.llmBaseURL, "/") + "/api/generate/doc"

	body, err := json.Marshal(map[string]any{
		"module_name":  moduleName,
		"file_path":    filePath,
		"code_content": codeContent,
	})
	if err != nil {
		return nil, "", fmt.Errorf("生成文档请求序列化失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("构建文档生成请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: s.llmTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("调用LLM服务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("读取LLM响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("LLM服务返回异常状态码: %d, 响应: %s", resp.StatusCode, string(respBody))
	}

	// 解析统一返回结构 {code, message, data}
	var apiResp llmAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, "", fmt.Errorf("解析LLM响应失败: %w", err)
	}
	if apiResp.Code != common.CodeSuccess {
		return nil, "", fmt.Errorf("LLM服务返回业务错误 code=%d msg=%s", apiResp.Code, apiResp.Message)
	}
	if len(apiResp.Data) == 0 {
		return nil, "", fmt.Errorf("LLM返回文档为空")
	}

	// 解析 data 为标准文档字段
	var data llmDocData
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, "", fmt.Errorf("解析文档数据失败: %w", err)
	}
	if strings.TrimSpace(data.Summary) == "" && strings.TrimSpace(data.ProcessFlow) == "" {
		return nil, "", fmt.Errorf("LLM生成文档内容为空")
	}
	return &data, string(apiResp.Data), nil
}

// syncVector 投递向量同步任务到队列（best-effort，投递失败仅记录日志）。
// 消费由独立 Worker 完成，保证检索使用最新校正内容。
func (s *TaskService) syncVector(doc *model.CodeFunctionDoc) {
	if doc == nil || doc.ID <= 0 || s.vc == nil {
		return
	}
	msg, err := buildVectorSyncMessage(doc, s.repoName(doc.RepoID))
	if err != nil {
		logger.Warn(context.Background(), "构建向量同步任务失败 doc_id=%d err=%v", doc.ID, err)
		return
	}
	if err := s.queue.SubmitTask(msg); err != nil {
		logger.Warn(context.Background(), "向量同步任务投递失败 doc_id=%d err=%v", doc.ID, err)
	}
}

// repoName 查询仓库名称（查询失败返回空串，向量元数据缺仓库名不影响功能）。
func (s *TaskService) repoName(repoID int64) string {
	if repoID <= 0 {
		return ""
	}
	r, err := s.repoRepo.GetByID(repoID)
	if err != nil {
		return ""
	}
	return r.RepoName
}

// moduleNameFromPath 从文件路径推导业务模块名（取首段目录）。
func moduleNameFromPath(file string) string {
	parts := strings.Split(file, "/")
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return p
		}
	}
	return "default"
}

// ensureModule 登记业务模块：不存在则创建（幂等，按仓库隔离），模块说明留空。
func (s *TaskService) ensureModule(repoID int64, moduleName string) error {
	if strings.TrimSpace(moduleName) == "" {
		return nil
	}
	_, err := s.moduleRepo.EnsureModule(repoID, moduleName, "")
	return err
}

// isDuplicateKeyError 判断是否为数据库唯一键冲突（并发触发同一任务时命中 idx_task_id）。
// 优先使用 gorm 翻译后的 ErrDuplicatedKey，兼容兜底匹配 "Duplicate entry"。
func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}

// ============ LLM 服务响应结构 ============

// llmAPIResponse Python LLM 服务统一返回结构。
type llmAPIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// llmDocData 标准化业务文档字段（对应 code_function_doc）。
type llmDocData struct {
	FuncName    string `json:"func_name"`
	Summary     string `json:"summary"`
	InputDesc   string `json:"input_desc"`
	OutputDesc  string `json:"output_desc"`
	ProcessFlow string `json:"process_flow"`
	RelyModules string `json:"rely_modules"`
	RiskPoint   string `json:"risk_point"`
}

// ============ 任务状态流转 ============

// MarkRunning 标记任务执行中。
func (s *TaskService) MarkRunning(taskID string) error {
	return s.taskRepo.UpdateStatus(taskID, common.TaskStatusRunning, "")
}

// MarkSuccess 标记任务成功并写入完成时间。
func (s *TaskService) MarkSuccess(taskID string) error {
	now := time.Now()
	return s.taskRepo.DB.Model(&model.TaskRecord{}).
		Where("task_id = ?", taskID).
		Updates(map[string]any{"status": common.TaskStatusSuccess, "finish_time": &now}).Error
}

// MarkFailed 标记任务失败并记录错误信息。
func (s *TaskService) MarkFailed(taskID, errMsg string) error {
	now := time.Now()
	return s.taskRepo.DB.Model(&model.TaskRecord{}).
		Where("task_id = ?", taskID).
		Updates(map[string]any{"status": common.TaskStatusFailed, "err_msg": errMsg, "finish_time": &now}).Error
}
