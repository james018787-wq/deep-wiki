"""ai-wiki-llm 入口：FastAPI 应用初始化与路由注册。

作用：提供 LLM 调用、文档生成、向量嵌入、RAG 辅助能力，
对外提供 HTTP 接口供 Golang 服务远程调用。
"""

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from config import settings
from schema.models import (
    ApiResponse,
    EmbeddingRequest,
    EmbeddingResponse,
    GenerateDiffLogRequest,
    GenerateDiffLogResponse,
    GenerateDocRequest,
    GenerateDocResponse,
    RerankRequest,
    RerankResponse,
    UpsertDocRequest,
    ChatRequest,
)
from service.doc_generator import DocGenerator
from service.embed_service import EmbedService
from service.llm_service import LLMService
from service.retrieval_service import RetrievalService
from service.vector_store import VectorStore
from utils.errors import ServiceError
from utils.logging import logger

# ============ 依赖初始化 ============
llm_service = LLMService()
embed_service = EmbedService()
doc_generator = DocGenerator(llm_service)
vector_store = VectorStore(embed_service.embedding_model)
retrieval_service = RetrievalService(vector_store)

# ============ 应用 ============
app = FastAPI(
    title="ai-wiki-llm",
    description="AI代码知识库 LLM 微服务：文档生成 / 向量嵌入 / RAG辅助",
    version="1.0.0",
)


# ============ 全局异常处理 ============
@app.exception_handler(ServiceError)
async def service_error_handler(_: Request, exc: ServiceError) -> JSONResponse:
    """业务异常统一返回友好 JSON。"""
    logger.error("业务异常: %s", exc)
    return JSONResponse(status_code=200, content={"code": exc.code, "message": exc.message, "data": None})


@app.exception_handler(RequestValidationError)
async def validation_error_handler(_: Request, exc: RequestValidationError) -> JSONResponse:
    """参数校验异常统一返回。"""
    logger.warning("参数校验失败: %s", exc.errors())
    return JSONResponse(status_code=200, content={"code": 4001, "message": f"参数错误: {exc.errors()}", "data": None})


# ============ 接口：文档生成 ============
@app.post("/api/generate/doc", response_model=ApiResponse)
def generate_doc(req: GenerateDocRequest) -> ApiResponse:
    """根据代码生成标准化业务文档 JSON。"""
    data = doc_generator.generate_doc(req.module_name, req.file_path, req.code_content)
    return ApiResponse(code=0, message="success", data=data)


# ============ 接口：代码变更摘要 ============
@app.post("/api/generate/diff_log", response_model=ApiResponse)
def generate_diff_log(req: GenerateDiffLogRequest) -> ApiResponse:
    """代码 diff 生成变更摘要。"""
    data = doc_generator.generate_diff_log(req.old_code, req.new_code)
    return ApiResponse(code=0, message="success", data=data)


# ============ 接口：文本向量化 ============
@app.post("/api/embedding/text", response_model=ApiResponse)
def embedding_text(req: EmbeddingRequest) -> ApiResponse:
    """文本字符串向量化，返回向量数组。"""
    vector = embed_service.embed_text(req.text)
    return ApiResponse(
        code=0,
        message="success",
        data=EmbeddingResponse(vector=vector, dimension=len(vector), model="text-embedding-3-small").model_dump(),
    )


# ============ 接口：RAG 重排 ============
@app.post("/api/rag/rerank", response_model=ApiResponse)
def rag_rerank(req: RerankRequest) -> ApiResponse:
    """候选文档列表 + 用户 query，返回重排后的文档（简易实现）。"""
    items = retrieval_service.rerank(req.query, req.candidates)
    return ApiResponse(code=0, message="success", data=RerankResponse(items=items).model_dump())


# ============ 接口：向量文档同步 ============
@app.post("/api/vector/upsert_doc", response_model=ApiResponse)
def vector_upsert_doc(req: UpsertDocRequest) -> ApiResponse:
    """写入/更新单篇文档向量（Go 侧人工校正/重置后异步调用）。

    约束：保证向量检索使用最新校正内容。
    """
    retrieval_service.upsert_doc(
        doc_id=str(req.doc_id),
        content=req.content,
        metadata={"module_name": req.module_name, "func_name": req.func_name, "file_path": req.file_path, **req.metadata},
    )
    return ApiResponse(code=0, message="success", data=None)


# ============ 接口：通用对话 ============
@app.post("/api/chat", response_model=ApiResponse)
def chat(req: ChatRequest) -> ApiResponse:
    """通用大模型对话（Go 侧 RAG 问答：传入上下文+用户问题，返回回答）。"""
    answer = llm_service.chat(req.system, req.user)
    return ApiResponse(code=0, message="success", data={"answer": answer})


# ============ 健康检查 ============
@app.get("/health")
def health() -> dict:
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    logger.info("ai-wiki-llm 启动: %s:%s", settings.server.host, settings.server.port)
    uvicorn.run("main:app", host=settings.server.host, port=settings.server.port, reload=False)