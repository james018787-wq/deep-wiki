package service

import (
	"context"

	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"

	"gorm.io/gorm"
)

// ReportService 知识库统计业务逻辑，基于现有表聚合查询，无新增数据表。
type ReportService struct {
	docRepo    *repo.CodeFunctionDocRepo
	moduleRepo *repo.BusinessModuleRepo
}

// NewReportService 构建统计服务。
func NewReportService(db *gorm.DB) *ReportService {
	return &ReportService{
		docRepo:    newDocRepo(db),
		moduleRepo: repo.NewBusinessModuleRepo(db),
	}
}

// ReportBasicResult 基础统计结果。
type ReportBasicResult struct {
	TotalDocCount      int64 `json:"total_doc_count"`      // 总函数文档数量（未删除）
	ManualDocCount     int64 `json:"manual_doc_count"`     // 人工校正文档数量（content_source=2）
	AutoDocCount       int64 `json:"auto_doc_count"`       // 自动生成文档数量（content_source=1）
	PendingReviewCount int64 `json:"pending_review_count"` // 待复核文档数量（source_code_changed=1）
	ModuleCount        int64 `json:"module_count"`         // 模块总数量（未删除）
}

// Basic 基础统计：各类文档数量与模块数量。
// 所有统计均基于现有表聚合查询（CountByWhere 自动排除已删除记录），不新增数据表。
func (s *ReportService) Basic(ctx context.Context) (*ReportBasicResult, error) {
	_ = ctx

	total, err := s.docRepo.CountByWhere(map[string]any{})
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "统计文档总数失败", err)
	}
	manual, err := s.docRepo.CountByWhere(map[string]any{"content_source": common.ContentSourceManual})
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "统计人工校正文档失败", err)
	}
	auto, err := s.docRepo.CountByWhere(map[string]any{"content_source": common.ContentSourceAuto})
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "统计自动生成文档失败", err)
	}
	pending, err := s.docRepo.CountByWhere(map[string]any{"source_code_changed": common.SourceCodeChanged})
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "统计待复核文档失败", err)
	}
	modules, err := s.moduleRepo.CountByWhere(map[string]any{})
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "统计模块数量失败", err)
	}

	return &ReportBasicResult{
		TotalDocCount:      total,
		ManualDocCount:     manual,
		AutoDocCount:       auto,
		PendingReviewCount: pending,
		ModuleCount:        modules,
	}, nil
}
