from typing import Any, Optional

from pydantic import BaseModel, Field


# ============ 统一响应模型 ============
class ApiResponse(BaseModel):
    """标准接口返回结构：code=0 成功，非0失败"""

    code: int = 0
    message: str = "success"
    data: Optional[Any] = None


# ============ 文档生成 ============
class GenerateDocRequest(BaseModel):
    """根据代码生成标准化业务文档请求"""

    module_name: str = Field(..., description="所属业务模块")
    file_path: str = Field(..., description="源码文件路径")
    code_content: str = Field(..., description="函数源码片段（最小切片单元=单个函数）")


class GenerateDocResponse(BaseModel):
    """标准化业务文档响应（对应 Go 侧 code_function_doc 结构）"""

    module_name: str = Field(..., description="所属业务模块")
    file_path: str = Field(..., description="源码文件路径")
    func_name: str = Field("", description="函数名称")
    summary: str = Field("", description="一句话业务摘要")
    input_desc: str = Field("", description="入参说明")
    output_desc: str = Field("", description="返回值说明")
    process_flow: str = Field("", description="业务执行流程")
    rely_modules: str = Field("[]", description="依赖模块json数组")
    risk_point: str = Field("", description="业务风险点")


# ============ 代码变更摘要 ============
class GenerateDiffLogRequest(BaseModel):
    """代码 diff 生成变更摘要请求"""

    old_code: str = Field(..., description="变更前代码")
    new_code: str = Field(..., description="变更后代码")


class GenerateDiffLogResponse(BaseModel):
    """代码变更摘要响应"""

    change_summary: str = Field("", description="代码变更摘要")
    business_impact: str = Field("", description="业务影响范围")
    attention: str = Field("", description="上线注意事项")


# ============ 向量嵌入 ============
class EmbeddingRequest(BaseModel):
    """文本向量化请求"""

    text: str = Field(..., min_length=1, description="待向量化文本")


class EmbeddingResponse(BaseModel):
    """文本向量化响应"""

    vector: list[float] = Field(..., description="向量数组")
    dimension: int = Field(..., description="向量维度")
    model: str = Field("", description="嵌入模型名称")


# ============ RAG 重排 ============
class CandidateDoc(BaseModel):
    """候选文档条目"""

    doc_id: int = Field(0, description="文档id")
    module_name: str = Field("", description="所属模块")
    func_name: str = Field("", description="函数名称")
    content: str = Field("", description="文档内容")
    score: float = Field(0.0, description="原始检索得分")


class RerankRequest(BaseModel):
    """候选文档重排请求"""

    query: str = Field(..., min_length=1, description="用户查询")
    candidates: list[CandidateDoc] = Field(..., description="候选文档列表")


class RerankItem(BaseModel):
    """重排结果条目"""

    doc_id: int = Field(0, description="文档id")
    module_name: str = Field("", description="所属模块")
    func_name: str = Field("", description="函数名称")
    content: str = Field("", description="文档内容")
    score: float = Field(0.0, description="重排后得分")


class RerankResponse(BaseModel):
    """重排结果响应"""

    items: list[RerankItem] = Field(..., description="按相关性降序的文档列表")


# ============ 向量文档同步 ============
class UpsertDocRequest(BaseModel):
    """向量文档写入/更新请求（Go 侧人工校正/重置后异步调用）"""

    doc_id: int = Field(..., description="关联 code_function_doc 主键")
    module_name: str = Field("", description="所属模块")
    file_path: str = Field("", description="文件路径")
    func_name: str = Field("", description="函数名称")
    content: str = Field("", description="向量化文本内容")
    metadata: dict = Field(default_factory=dict, description="附加元数据")


# ============ 通用对话 ============
class ChatRequest(BaseModel):
    """通用对话请求（RAG 问答等）"""

    system: str = Field("", description="系统提示词")
    user: str = Field(..., min_length=1, description="用户内容（上下文+问题）")