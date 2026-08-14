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
	"ai-code-wiki/pkg/git"
	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/taskqueue"
	"ai-code-wiki/pkg/vector"
	"ai-code-wiki/pkg/webhook"

	"gorm.io/gorm"
)

// TaskService 代码解析任务业务逻辑。
type TaskService struct {
	db         *gorm.DB
	taskRepo   *repo.TaskRecordRepo
	docRepo    *repo.CodeFunctionDocRepo
	gitCfg     *config.GitConfig    // git 仓库配置
	llmBaseURL string               // Python LLM 服务地址（LLM_SERVICE_URL）
	vc         vector.VectorClient  // 向量存储抽象（业务不感知 chroma/milvus）
	queue      taskqueue.TaskQueue  // 异步任务队列（当前默认 goroutine 本地实现，可替换为 MQ）
}

// NewTaskService 构建任务服务。
// vc 为 nil 时跳过向量同步（向量引擎未配置/初始化失败场景）。
func NewTaskService(db *gorm.DB, cfg *config.Config, vc vector.VectorClient) *TaskService {
	return &TaskService{
		db:         db,
		taskRepo:   newTaskRepo(db),
		docRepo:    newDocRepo(db),
		gitCfg:     &cfg.Git,
		llmBaseURL: cfg.LLM.BaseURL,
		vc:         vc,
		queue:      taskqueue.Default,
	}
}

// TriggerTaskReq 触发代码解析任务入参（CI 回调）。
type TriggerTaskReq struct {
	TaskID string `json:"task_id" binding:"required"` // 任务唯一标识
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
//  接收task -> git拉取代码 -> git diff获取变更文件 -> 过滤.go/.php文件
//  -> 按扩展名解析（.go走go ast，.php走简易正则）提取函数 -> 调用Python LLM服务生成业务文档。
func (s *TaskService) TriggerTask(ctx context.Context, req *TriggerTaskReq) (*model.TaskRecord, error) {
	_ = ctx

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
		Branch: req.Branch,
		Status: common.TaskStatusPending,
	}
	if err := s.taskRepo.Create(record); err != nil {
		if isDuplicateKeyError(err) {
			return nil, common.NewError(common.CodeConflict, "任务已存在，请勿重复触发")
		}
		return nil, common.WrapError(common.CodeInternalError, "创建任务失败", err)
	}

	// 3. 异步队列提交任务流水线，不阻塞主 HTTP 接口
	//（当前为 goroutine 本地执行，生产环境可替换为 RabbitMQ/Kafka）
	s.queue.SubmitAsyncTask(func() {
		s.runPipeline(record)
	})
	return record, nil
}

// runPipeline 任务流水线执行入口：负责状态流转与整体错误兜底。
func (s *TaskService) runPipeline(record *model.TaskRecord) {
	ctx := context.Background()

	if err := s.MarkRunning(record.TaskID); err != nil {
		logger.Warn(ctx, "任务 %s 标记执行中失败: %v", record.TaskID, err)
	}

	if err := s.process(ctx, record); err != nil {
		_ = s.MarkFailed(record.TaskID, err.Error())
		logger.Error(ctx, "任务 %s 执行失败: %v", record.TaskID, err)
		return
	}
	_ = s.MarkSuccess(record.TaskID)
	logger.Info(ctx, "任务 %s 执行成功", record.TaskID)
}

// HandleGitPush 处理代码托管平台 webhook 分支 push 回调。
//
// 业务规则：
//  1. 校验入参（分支/仓库），tag 推送与分支删除在前置 handler 已过滤，这里再兜底；
//  2. 以「仓库+branch+after commit」生成幂等 task_id：同一次 push 重复回调
//     （平台重试）直接返回已存在任务，不重复触发；
//  3. 落库 task_record（待执行）后投递异步任务队列，执行增量解析流水线。
//
// 说明：仓库地址以服务配置 git.repo_url 为准（单仓库部署约定）；
// 回调中的仓库地址用于校验与日志，不一致时仅告警不中断。
func (s *TaskService) HandleGitPush(ctx context.Context, event *webhook.PushEvent) (*model.TaskRecord, error) {
	if event == nil || event.IsTag || event.IsDelete {
		return nil, common.NewError(common.CodeBadRequest, "仅支持分支 push 事件")
	}
	if strings.TrimSpace(event.Branch) == "" {
		return nil, common.NewError(common.CodeBadRequest, "推送分支为空")
	}
	if strings.TrimSpace(s.gitCfg.RepoURL) == "" {
		return nil, common.NewError(common.CodeInvalidState, "git 仓库未配置，无法执行解析任务")
	}

	// 仓库一致性校验：仅告警，仍按配置仓库解析
	if event.RepoURL != "" && !sameRepo(s.gitCfg.RepoURL, event.RepoURL) {
		logger.Warn(ctx, "webhook 仓库 %s 与配置仓库 %s 不一致，仍按配置仓库执行解析", event.RepoURL, s.gitCfg.RepoURL)
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

	// 投递任务队列，异步执行增量解析流水线，不阻塞 webhook 响应
	s.queue.SubmitAsyncTask(func() {
		s.runPipeline(record)
	})
	logger.Info(ctx, "webhook 触发解析任务成功 task_id=%s branch=%s", taskID, event.Branch)
	return record, nil
}

// genWebhookTaskID 生成幂等任务 id：仓库+branch+after commit 的 sha1 摘要。
func genWebhookTaskID(event *webhook.PushEvent) string {
	sum := sha1.Sum([]byte(event.RepoURL + "|" + event.Branch + "|" + event.AfterCommit))
	return "webhook-" + hex.EncodeToString(sum[:])[:16]
}

// sameRepo 判断两个仓库地址是否指向同一仓库（忽略 http/https 协议与末尾 .git）。
func sameRepo(a, b string) bool {
	norm := func(s string) string {
		s = strings.TrimRight(strings.TrimSpace(s), "/")
		s = strings.TrimSuffix(s, ".git")
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		return s
	}
	return norm(a) == norm(b)
}

// process 核心流水线：git拉取 -> diff -> 过滤.go -> AST解析 -> LLM生成文档。
func (s *TaskService) process(ctx context.Context, task *model.TaskRecord) error {
	// 1. git 拉取/更新代码
	if strings.TrimSpace(s.gitCfg.RepoURL) == "" {
		return fmt.Errorf("git仓库地址未配置")
	}
	if err := git.CloneOrPull(s.gitCfg.RepoURL, task.Branch, s.gitCfg.CloneDir); err != nil {
		return fmt.Errorf("拉取代码失败: %w", err)
	}

	// 2. 获取 diff 变更文件（任务分支 对比 默认分支）
	baseRef := "origin/" + s.gitCfg.DefaultBranch
	branchRef := "origin/" + task.Branch
	files, err := git.GetDiffFiles(s.gitCfg.CloneDir, baseRef, branchRef)
	if err != nil {
		return fmt.Errorf("获取diff变更文件失败: %w", err)
	}

	// 3. 过滤 .go / .php 文件
	var codeFiles []string
	for _, f := range files {
		if strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".php") {
			codeFiles = append(codeFiles, f)
		}
	}
	if len(codeFiles) == 0 {
		logger.Info(ctx, "任务 %s 无 .go/.php 文件变更", task.TaskID)
		return nil
	}

	// 4-5. 解析 + LLM生成文档（单文件失败不终止整体任务）
	ok := 0
	for _, file := range codeFiles {
		if err := s.processFile(file); err != nil {
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

// fileFunc 解析出的通用函数单元（函数名 + 源码片段）。
type fileFunc struct {
	Name string // 函数名称
	Code string // 函数源码片段
}

// processFile 解析单个代码文件，逐个函数生成文档。
// 按文件后缀选择解析器：.go 走 go ast 解析，.php 走简易正则解析。
func (s *TaskService) processFile(file string) error {
	// 读取仓库内文件内容
	content, err := git.ReadFile(s.gitCfg.CloneDir, file)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 按扩展名分派解析器
	var funcs []fileFunc
	if strings.HasSuffix(file, ".php") {
		// PHP：简易正则解析（不引入重型解析库）
		items, err := astphp.ParsePHPFile(content)
		if err != nil {
			return fmt.Errorf("PHP解析失败: %w", err)
		}
		for _, it := range items {
			funcs = append(funcs, fileFunc{Name: it.FuncName, Code: it.Code})
		}
	} else {
		// Go：原 ast 解析逻辑（不改动）
		items, err := astgo.ParseGoFile(content)
		if err != nil {
			return fmt.Errorf("AST解析失败: %w", err)
		}
		for i := range items {
			funcs = append(funcs, fileFunc{Name: items[i].FuncName, Code: items[i].Code})
		}
	}
	if len(funcs) == 0 {
		return fmt.Errorf("文件内未发现函数")
	}

	// 逐个函数生成文档（单函数失败仅记录日志，继续处理）
	ok := 0
	for _, fn := range funcs {
		if err := s.processFunc(file, fn.Name, fn.Code); err != nil {
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

// processFunc 单个函数：调用 LLM 生成文档，并按业务规则落库。
//
// 业务规则（严格遵守）：
//  1. 函数已存在且 content_source=2（人工校正）：不覆盖当前生效文档，
//     仅更新 source_code 并置 source_code_changed=1 待复核。
//  2. origin_auto_doc 只在【首次创建】时写入，任何情况（源码变更、重新解析）禁止覆盖，
//     保证"重置回 AI 原始版本"始终可追溯到首次生成内容。
//  3. 无人工校正标记的函数：覆盖写入文档并同步向量库。
func (s *TaskService) processFunc(file, funcName, code string) error {
	if strings.TrimSpace(funcName) == "" {
		return fmt.Errorf("函数名为空，跳过")
	}

	// 推导模块名（取文件路径首段目录）
	moduleName := moduleNameFromPath(file)

	// 调用 Python LLM 服务生成标准化业务文档
	data, rawJSON, err := s.generateDoc(moduleName, file, code)
	if err != nil {
		return err
	}

	// 查询函数是否已有文档
	existing, exists := (*model.CodeFunctionDoc)(nil), false
	if doc, err := s.docRepo.GetByFileFunc(file, funcName); err == nil {
		existing, exists = doc, true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("查询已有文档失败: %w", err)
	}

	// 规则1：人工校正文档不覆盖，仅更新源码与待复核标记，禁止触碰 origin_auto_doc 与生效文档
	if exists && existing.ContentSource == common.ContentSourceManual {
		if err := s.docRepo.UpdateFields(existing.ID, map[string]any{
			"source_code":         code,
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
		ModuleName:        moduleName,
		FilePath:          file,
		FuncName:          funcName,
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

	client := &http.Client{Timeout: 60 * time.Second}
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

// syncVector 同步向量库（best-effort，失败仅记录日志）。
// 通过异步任务队列执行，避免阻塞主流程。
func (s *TaskService) syncVector(doc *model.CodeFunctionDoc) {
	if doc == nil || doc.ID <= 0 || s.vc == nil {
		return
	}
	s.queue.SubmitAsyncTask(func() {
		content := strings.Join([]string{doc.Summary, doc.ProcessFlow, doc.RiskPoint}, "\n")
		dv := &vector.DocVector{
			DocID:      doc.ID,
			ModuleName: doc.ModuleName,
			FilePath:   doc.FilePath,
			FuncName:   doc.FuncName,
			Content:    content,
		}
		if err := s.vc.UpsertDoc(dv); err != nil {
			logger.Warn(context.Background(), "同步向量失败 doc_id=%d err=%v", doc.ID, err)
		}
	})
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