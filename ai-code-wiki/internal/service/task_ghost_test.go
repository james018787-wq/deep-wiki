package service

import (
	"context"
	"testing"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/taskqueue"

	"gorm.io/gorm"
)

// TestCleanupGhostDocs 幽灵文档清理：函数从代码删除后，对应文档自动下线。
// 断言：逻辑删除 + 删除操作日志（operate_type=3）+ 投递向量删除任务。
func TestCleanupGhostDocs(t *testing.T) {
	db := newTestDB(t)
	queue := taskqueue.NewMemoryQueue()
	ts := NewTaskService(db, &config.Config{}, nil, queue, nil)

	repoInfo := &model.CodeRepo{ID: 1, RepoName: "testrepo"}
	file := "order/service.go"

	// 幂等：先清空该文件的残留文档（防止中断运行遗留记录撞唯一键 idx_file_func）
	db.Unscoped().Where("repo_id = ? AND file_path = ?", 1, file).Delete(&model.CodeFunctionDoc{})

	// 种子：文件中 3 个函数文档，其中 2 个在当前代码中已不存在（含 1 个人工校正）
	var seedIDs []int64
	docs := []*model.CodeFunctionDoc{
		{RepoID: 1, ModuleName: "order", FilePath: file, FuncName: "Keep", Summary: "保留", ContentSource: common.ContentSourceAuto},
		{RepoID: 1, ModuleName: "order", FilePath: file, FuncName: "Gone", Summary: "已删除", ContentSource: common.ContentSourceAuto},
		{RepoID: 1, ModuleName: "order", FilePath: file, FuncName: "GoneToo", Summary: "已删除2", ContentSource: common.ContentSourceManual},
	}
	for _, d := range docs {
		if err := db.Create(d).Error; err != nil {
			t.Fatalf("seed 文档失败: %v", err)
		}
		seedIDs = append(seedIDs, d.ID)
	}

	// 当前代码仅剩 Keep 与新增 NewFunc
	funcs := []fileFunc{{Name: "Keep", StartLine: 3}, {Name: "NewFunc", StartLine: 10}}
	if err := ts.cleanupGhostDocs(repoInfo, file, funcs); err != nil {
		t.Fatalf("幽灵清理失败: %v", err)
	}

	// 断言 1：Keep 保留，Gone/GoneToo 被逻辑删除
	var keep model.CodeFunctionDoc
	if err := db.Where("repo_id = ? AND func_name = ? AND is_deleted = ?", 1, "Keep", 0).First(&keep).Error; err != nil {
		t.Fatalf("Keep 应保留: %v", err)
	}
	var goneCount int64
	db.Model(&model.CodeFunctionDoc{}).Where("repo_id = ? AND func_name IN ? AND is_deleted = ?", 1, []string{"Gone", "GoneToo"}, 1).Count(&goneCount)
	if goneCount != 2 {
		t.Fatalf("Gone/GoneToo 应被逻辑删除, got count=%d", goneCount)
	}

	// 断言 2：仅本次种子文档产生 2 条删除操作日志
	var logCount int64
	db.Model(&model.DocModifyLog{}).Where("operate_type = ? AND doc_id IN ?", common.DocOperateDelete, seedIDs).Count(&logCount)
	if logCount != 2 {
		t.Fatalf("期望 2 条删除日志, got %d", logCount)
	}

	// 断言 3：投递向量删除任务
	msg, err := queue.ConsumeTask(context.Background())
	if err != nil {
		t.Fatalf("应投递向量删除任务: %v", err)
	}
	if msg.Type != taskqueue.TaskTypeVectorDelete {
		t.Fatalf("期望 vector_delete 任务, got %s", msg.Type)
	}

	cleanupGhostTestData(t, db, file)
}

// TestCleanupFileDocs 文件整体删除：该文件全部文档下线。
func TestCleanupFileDocs(t *testing.T) {
	db := newTestDB(t)
	queue := taskqueue.NewMemoryQueue()
	ts := NewTaskService(db, &config.Config{}, nil, queue, nil)

	repoInfo := &model.CodeRepo{ID: 1, RepoName: "testrepo"}
	file := "order/removed.go"
	for _, fn := range []string{"A", "B"} {
		if err := db.Create(&model.CodeFunctionDoc{
			RepoID: 1, ModuleName: "order", FilePath: file, FuncName: fn,
			ContentSource: common.ContentSourceAuto,
		}).Error; err != nil {
			t.Fatalf("seed 文档失败: %v", err)
		}
	}

	if err := ts.cleanupFileDocs(repoInfo, file); err != nil {
		t.Fatalf("清理文件文档失败: %v", err)
	}
	var alive int64
	db.Model(&model.CodeFunctionDoc{}).Where("repo_id = ? AND file_path = ? AND is_deleted = ?", 1, file, 0).Count(&alive)
	if alive != 0 {
		t.Fatalf("文件文档应全部下线, 存活=%d", alive)
	}

	cleanupGhostTestData(t, db, file)
}

// cleanupGhostTestData 物理清理测试文档与日志（避免污染测试库）。
func cleanupGhostTestData(t *testing.T, db *gorm.DB, file string) {
	t.Helper()
	var ids []int64
	db.Model(&model.CodeFunctionDoc{}).Where("file_path = ?", file).Pluck("id", &ids)
	if err := db.Unscoped().Where("file_path = ?", file).Delete(&model.CodeFunctionDoc{}).Error; err != nil {
		t.Fatalf("清理测试文档失败: %v", err)
	}
	if len(ids) > 0 {
		if err := db.Unscoped().Where("doc_id IN ?", ids).Delete(&model.DocModifyLog{}).Error; err != nil {
			t.Fatalf("清理测试日志失败: %v", err)
		}
	}
}
