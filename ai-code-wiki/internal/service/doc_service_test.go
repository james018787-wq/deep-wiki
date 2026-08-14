package service

// 本文件包含两类测试：
//
//  1) TestRestoreFromOriginDoc：纯单元测试，无外部依赖，默认运行。
//
//  2) TestDocEdit* / TestDocReset*：依赖 MySQL 的集成测试。
//     外部依赖（mysql）未就绪时自动跳过；本地手动执行：
//         docker compose up -d mysql
//         go test ./internal/service/ -run 'TestDoc(Edit|Reset)' -v
//     向量库 / LLM 服务不在本测试范围内：
//     NewDocService 传入 nil 向量客户端，跳过异步向量同步（外部依赖）。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/taskqueue"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testDSNEnv = "WIKI_TEST_DSN"

// testDSN 读取测试数据库连接串，默认匹配 docker-compose 的本地 MySQL。
func testDSN() string {
	if dsn := os.Getenv(testDSNEnv); dsn != "" {
		return dsn
	}
	return "root:Wiki@2026@tcp(127.0.0.1:3306)/ai_code_wiki?charset=utf8mb4&parseTime=True&loc=Local"
}

// newTestDB 连接本地 MySQL 并准备测试表；连接失败时跳过（外部依赖）。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.Open(testDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("跳过：需要本地 MySQL（手动执行 docker compose up -d mysql）: %v", err)
	}
	if err := db.AutoMigrate(&model.CodeFunctionDoc{}, &model.DocModifyLog{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	return db
}

// seedDoc 插入测试文档并注册清理（连同其操作日志一起物理删除）。
func seedDoc(t *testing.T, db *gorm.DB, doc *model.CodeFunctionDoc) int64 {
	t.Helper()
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("seed 文档失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("doc_id = ?", doc.ID).Delete(&model.DocModifyLog{}).Error
		_ = db.Unscoped().Where("id = ?", doc.ID).Delete(&model.CodeFunctionDoc{}).Error
	})
	return doc.ID
}

// assertAppErr 断言错误为 *common.AppError 且错误码匹配。
func assertAppErr(t *testing.T, err error, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误，实际 nil")
	}
	var ae *common.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("期望 *common.AppError, got %T: %v", err, err)
	}
	if ae.Code != wantCode {
		t.Fatalf("期望错误码 %d, got %d (%s)", wantCode, ae.Code, ae.Message)
	}
}

// TestRestoreFromOriginDoc 表格驱动：用 origin_auto_doc 恢复文档字段，且不被覆盖。
func TestRestoreFromOriginDoc(t *testing.T) {
	validOrigin := `{"summary":"原始摘要","input_desc":"原入参","output_desc":"原出参","process_flow":"原流程","rely_modules":"[\"m\"]","risk_point":"原风险"}`

	cases := []struct {
		name        string // 用例名
		origin      string // origin_auto_doc 内容
		wantSummary string // 恢复后的摘要
		wantErr     bool   // 期望报错
	}{
		{name: "合法JSON恢复字段", origin: validOrigin, wantSummary: "原始摘要"},
		{name: "空origin报错", origin: "", wantErr: true},
		{name: "非法JSON报错", origin: "not-json", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &model.CodeFunctionDoc{
				OriginAutoDoc: tc.origin,
				Summary:       "人工摘要",
				ProcessFlow:   "人工流程",
			}
			err := restoreFromOriginDoc(doc)
			if tc.wantErr {
				if err == nil {
					t.Fatal("期望报错，实际成功")
				}
				return
			}
			if err != nil {
				t.Fatalf("恢复失败: %v", err)
			}
			if doc.Summary != tc.wantSummary {
				t.Fatalf("摘要未恢复: got %q want %q", doc.Summary, tc.wantSummary)
			}
			if doc.ProcessFlow != "原流程" {
				t.Fatalf("流程未恢复: got %q", doc.ProcessFlow)
			}
			// 核心断言：origin_auto_doc 不允许被覆盖
			if doc.OriginAutoDoc != tc.origin {
				t.Fatal("origin_auto_doc 被覆盖")
			}
		})
	}
}

// TestDocEdit 表格驱动：人工校正事务正常落库 + 操作日志生成。
func TestDocEdit(t *testing.T) {
	db := newTestDB(t)
	svc := NewDocService(db, nil, taskqueue.NewMemoryQueue()) // nil 向量客户端：跳过向量同步（外部依赖）

	cases := []struct {
		name        string // 用例名
		seedSummary string // 种子文档摘要
		req         *EditDocReq
		wantSummary string
		wantProcess string
	}{
		{
			name:        "编辑全部业务字段",
			seedSummary: "旧摘要",
			req: &EditDocReq{
				Summary:     "新摘要",
				InputDesc:   "新入参",
				OutputDesc:  "新出参",
				ProcessFlow: "新流程",
				RiskPoint:   "新风险",
				Operator:    "alice",
				Remark:      "人工校正",
			},
			wantSummary: "新摘要",
			wantProcess: "新流程",
		},
		{
			name:        "仅编辑流程不覆盖摘要",
			seedSummary: "保留摘要",
			req: &EditDocReq{
				ProcessFlow: "补充流程",
				Operator:    "bob",
			},
			wantSummary: "保留摘要",
			wantProcess: "补充流程",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed := &model.CodeFunctionDoc{
				ModuleName:    "test",
				FilePath:      "test/" + tc.name + ".go",
				FuncName:      "EditFn",
				Summary:       tc.seedSummary,
				ProcessFlow:   "旧流程",
				OriginAutoDoc: `{"summary":"原始摘要"}`,
				ContentSource: common.ContentSourceAuto,
			}
			docID := seedDoc(t, db, seed)

			if err := svc.EditDoc(context.Background(), docID, tc.req); err != nil {
				t.Fatalf("EditDoc 失败: %v", err)
			}

			// 1. 文档字段落库校验
			var got model.CodeFunctionDoc
			if err := db.Where("id = ?", docID).First(&got).Error; err != nil {
				t.Fatalf("查询文档失败: %v", err)
			}
			if got.Summary != tc.wantSummary {
				t.Errorf("摘要不匹配: got %q want %q", got.Summary, tc.wantSummary)
			}
			if got.ProcessFlow != tc.wantProcess {
				t.Errorf("流程不匹配: got %q want %q", got.ProcessFlow, tc.wantProcess)
			}
			if got.ContentSource != common.ContentSourceManual {
				t.Errorf("content_source 应为人工校正(2), got %d", got.ContentSource)
			}
			if got.LastEditUser != tc.req.Operator {
				t.Errorf("last_edit_user 不匹配: got %q want %q", got.LastEditUser, tc.req.Operator)
			}
			if got.LastEditTime == nil {
				t.Error("last_edit_time 应被记录")
			}
			if got.OriginAutoDoc != seed.OriginAutoDoc {
				t.Error("origin_auto_doc 被覆盖")
			}

			// 2. 操作日志生成校验
			var logs []model.DocModifyLog
			if err := db.Where("doc_id = ?", docID).Order("id asc").Find(&logs).Error; err != nil {
				t.Fatalf("查询日志失败: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("期望 1 条操作日志, got %d", len(logs))
			}
			l := logs[0]
			if l.OperateType != common.DocOperateEdit {
				t.Errorf("operate_type 应为编辑(1), got %d", l.OperateType)
			}
			if l.Operator != tc.req.Operator {
				t.Errorf("日志操作人不匹配: got %q", l.Operator)
			}
			if l.BeforeContent == "" || l.AfterContent == "" {
				t.Error("日志缺少 before/after 快照")
			}
			if l.BeforeContent == l.AfterContent {
				t.Error("修改前后快照应不同")
			}
			var before map[string]any
			if err := json.Unmarshal([]byte(l.BeforeContent), &before); err != nil {
				t.Fatalf("before 快照解析失败: %v", err)
			}
			if before["summary"] != tc.seedSummary {
				t.Errorf("before 快照应包含旧摘要: got %v want %q", before["summary"], tc.seedSummary)
			}
			var after map[string]any
			if err := json.Unmarshal([]byte(l.AfterContent), &after); err != nil {
				t.Fatalf("after 快照解析失败: %v", err)
			}
			if after["summary"] != tc.wantSummary {
				t.Errorf("after 快照应包含新摘要: got %v want %q", after["summary"], tc.wantSummary)
			}
		})
	}
}

// TestDocEditError 表格驱动：编辑的异常分支。
func TestDocEditError(t *testing.T) {
	db := newTestDB(t)
	svc := NewDocService(db, nil, taskqueue.NewMemoryQueue())

	seed := &model.CodeFunctionDoc{
		ModuleName:    "test",
		FilePath:      "test/edit_err.go",
		FuncName:      "ErrFn",
		Summary:       "s",
		ContentSource: common.ContentSourceAuto,
	}
	docID := seedDoc(t, db, seed)

	cases := []struct {
		name     string // 用例名
		docID    int64
		req      *EditDocReq
		wantCode int
	}{
		{name: "操作人为空", docID: docID, req: &EditDocReq{Summary: "s"}, wantCode: common.CodeBadRequest},
		{name: "文档不存在", docID: 999999, req: &EditDocReq{Summary: "s", Operator: "alice"}, wantCode: common.CodeNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.EditDoc(context.Background(), tc.docID, tc.req)
			assertAppErr(t, err, tc.wantCode)
		})
	}
}

// TestDocReset 表格驱动：文档重置恢复原始 AI 内容，origin_auto_doc 不被覆盖。
func TestDocReset(t *testing.T) {
	db := newTestDB(t)
	svc := NewDocService(db, nil, taskqueue.NewMemoryQueue())

	originJSON := `{"summary":"原始摘要","input_desc":"原入参","output_desc":"原出参","process_flow":"原流程","rely_modules":"[\"m\"]","risk_point":"原风险"}`

	cases := []struct {
		name    string                         // 用例名
		prepare func(d *model.CodeFunctionDoc) // 模拟编辑后的文档状态
	}{
		{
			name: "人工编辑后重置",
			prepare: func(d *model.CodeFunctionDoc) {
				d.Summary = "人工摘要"
				d.InputDesc = "人工入参"
				d.ProcessFlow = "人工流程"
				d.RiskPoint = "人工风险"
				d.ContentSource = common.ContentSourceManual
				d.LastEditUser = "alice"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed := &model.CodeFunctionDoc{
				ModuleName:    "test",
				FilePath:      "test/" + tc.name + ".go",
				FuncName:      "ResetFn",
				Summary:       "初始摘要",
				ProcessFlow:   "初始流程",
				OriginAutoDoc: originJSON,
				ContentSource: common.ContentSourceAuto,
			}
			tc.prepare(seed)
			docID := seedDoc(t, db, seed)

			if err := svc.ResetDoc(context.Background(), docID, "bob"); err != nil {
				t.Fatalf("ResetDoc 失败: %v", err)
			}

			// 1. 文档恢复校验
			var got model.CodeFunctionDoc
			if err := db.Where("id = ?", docID).First(&got).Error; err != nil {
				t.Fatalf("查询文档失败: %v", err)
			}
			if got.Summary != "原始摘要" {
				t.Errorf("摘要未恢复: got %q want %q", got.Summary, "原始摘要")
			}
			if got.ProcessFlow != "原流程" {
				t.Errorf("流程未恢复: got %q", got.ProcessFlow)
			}
			if got.ContentSource != common.ContentSourceAuto {
				t.Errorf("content_source 应恢复为 AI(1), got %d", got.ContentSource)
			}
			if got.LastEditUser != "" {
				t.Errorf("last_edit_user 应清空, got %q", got.LastEditUser)
			}
			if got.LastEditTime != nil {
				t.Errorf("last_edit_time 应清空, got %v", got.LastEditTime)
			}
			// 核心断言：origin_auto_doc 永久保留，不允许被覆盖
			if got.OriginAutoDoc != originJSON {
				t.Error("origin_auto_doc 被覆盖")
			}

			// 2. 重置日志校验
			var logs []model.DocModifyLog
			if err := db.Where("doc_id = ?", docID).Order("id asc").Find(&logs).Error; err != nil {
				t.Fatalf("查询日志失败: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("期望 1 条操作日志, got %d", len(logs))
			}
			if logs[0].OperateType != common.DocOperateReset {
				t.Errorf("operate_type 应为重置(2), got %d", logs[0].OperateType)
			}
			if logs[0].Operator != "bob" {
				t.Errorf("日志操作人不匹配: got %q", logs[0].Operator)
			}
		})
	}
}

// TestDocResetError 表格驱动：重置的异常分支。
func TestDocResetError(t *testing.T) {
	db := newTestDB(t)
	svc := NewDocService(db, nil, taskqueue.NewMemoryQueue())

	cases := []struct {
		name     string // 用例名
		seed     *model.CodeFunctionDoc
		operator string
		wantCode int
	}{
		{
			name: "操作人为空",
			seed: &model.CodeFunctionDoc{
				ModuleName: "test", FilePath: "test/r1.go", FuncName: "R1",
				OriginAutoDoc: `{"summary":"s"}`,
			},
			operator: "",
			wantCode: common.CodeBadRequest,
		},
		{
			name: "origin为空无法重置",
			seed: &model.CodeFunctionDoc{
				ModuleName: "test", FilePath: "test/r2.go", FuncName: "R2",
				OriginAutoDoc: "",
			},
			operator: "bob",
			wantCode: common.CodeInvalidState,
		},
		{
			name:     "文档不存在",
			seed:     nil,
			operator: "bob",
			wantCode: common.CodeNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var docID int64
			if tc.seed != nil {
				docID = seedDoc(t, db, tc.seed)
			} else {
				docID = 999999
			}
			err := svc.ResetDoc(context.Background(), docID, tc.operator)
			assertAppErr(t, err, tc.wantCode)
		})
	}
}

// TestDocHistory 验证：编辑/重置会写 doc_modify_log，历史列表与快照详情可查询。
func TestDocHistory(t *testing.T) {
	db := newTestDB(t)
	svc := NewDocService(db, nil, taskqueue.NewMemoryQueue())

	doc := &model.CodeFunctionDoc{
		ModuleName:    "test",
		FilePath:      "test/history.go",
		FuncName:      "HistoryFn",
		Summary:       "初始摘要",
		OriginAutoDoc: `{"summary":"重置后摘要"}`,
	}
	docID := seedDoc(t, db, doc)

	// 1. 编辑一次（写入 operate_type=1）
	if err := svc.EditDoc(context.Background(), docID, &EditDocReq{
		Summary: "编辑后摘要", Operator: "alice", Remark: "第一次编辑",
	}); err != nil {
		t.Fatalf("EditDoc 失败: %v", err)
	}
	// 2. 重置一次（写入 operate_type=2）
	if err := svc.ResetDoc(context.Background(), docID, "bob"); err != nil {
		t.Fatalf("ResetDoc 失败: %v", err)
	}

	// 3. 历史列表：2 条记录，时间倒序（最近在前）
	list, err := svc.ListDocHistory(context.Background(), docID)
	if err != nil {
		t.Fatalf("ListDocHistory 失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("历史记录数量不符: got %d want 2", len(list))
	}
	if list[0].OperateType != common.DocOperateReset || list[0].Operator != "bob" {
		t.Errorf("最近记录应为重置(bob): %+v", list[0])
	}
	if list[0].OperateName == "" {
		t.Errorf("操作类型描述不应为空: %+v", list[0])
	}
	if list[1].OperateType != common.DocOperateEdit || list[1].Operator != "alice" {
		t.Errorf("第一条记录应为编辑(alice): %+v", list[1])
	}

	// 4. 快照详情：取编辑那条，before/after 应为完整原始 JSON
	detail, err := svc.GetDocHistoryDetail(context.Background(), docID, list[1].LogID)
	if err != nil {
		t.Fatalf("GetDocHistoryDetail 失败: %v", err)
	}
	if detail.Before["summary"] != "初始摘要" {
		t.Errorf("before 快照摘要不符: got %v", detail.Before["summary"])
	}
	if detail.After["summary"] != "编辑后摘要" {
		t.Errorf("after 快照摘要不符: got %v", detail.After["summary"])
	}

	// 5. 记录不存在 / 不属于该文档 → CodeNotFound
	_, err = svc.GetDocHistoryDetail(context.Background(), docID, 999999)
	assertAppErr(t, err, common.CodeNotFound)
}
