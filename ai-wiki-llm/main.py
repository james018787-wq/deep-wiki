"""ai-wiki-llm 入口：FastAPI 应用初始化与路由注册。

作用：提供 LLM 调用、文档生成、向量嵌入能力，
对外提供 HTTP 接口供 Golang 服务远程调用。

约束：向量库（Chroma/Milvus）的存取统一由 Golang 侧
pkg/vector.VectorClient 负责，本服务不做向量库读写。
"""

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from config import settings
from schema.models import (
    ApiResponse,
    ChatRequest,
    ChatResponse,
    EmbeddingRequest,
    EmbeddingResponse,
    GenerateDiffLogRequest,
    GenerateDiffLogResponse,
    GenerateDocRequest,
    GenerateDocResponse,
)
from service.doc_generator import DocGenerator
from service.embed_service import EmbedService
from service.scheduler import Scheduler, build_scheduler
from utils.errors import ServiceError
from utils.logging import logger

# ============ 依赖初始化 ============
embed_service = EmbedService()
# 多模型调度器（优先低价，失败自动降级；Redis 分布式熔断/限流）
scheduler: Scheduler = build_scheduler()
# 文档生成统一走调度器（与 /api/chat 共用模型池，返回 token/cost 供消耗统计）
doc_generator = DocGenerator(scheduler)

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


# ============ 接口：通用对话 ============
@app.post("/api/chat", response_model=ApiResponse)
def chat(req: ChatRequest) -> ApiResponse:
    """通用大模型对话（RAG 问答/需求分析）。

    经多模型调度器：优先低价，失败自动降级切换下一档；
    支持 force_model / force_high_quality / estimated_tokens 覆盖。
    返回回答与调度元信息（used_model / switch_count / cost 等）。
    """
    data = scheduler.chat(
        system=req.system,
        user=req.user,
        force_model=req.force_model,
        force_high_quality=req.force_high_quality,
        estimated_tokens=req.estimated_tokens,
    )
    return ApiResponse(code=0, message="success", data=ChatResponse(**data).model_dump())


# ============ 接口：模型池与用量 ============
@app.get("/api/models", response_model=ApiResponse)
def list_models() -> ApiResponse:
    """返回模型池配置（脱敏，不含 api_key）与全局调度参数。

    供 Go 侧 /model/list 转发展示当前系统配置了哪些模型。
    """
    return ApiResponse(code=0, message="success", data=scheduler.pool_snapshot())


@app.get("/api/models/status", response_model=ApiResponse)
def list_models_status() -> ApiResponse:
    """返回各模型运行状态（熔断中 / 限流用量 / 连续失败 / 累计降级次数）。

    供 Go 侧 /model/status 转发，前端「模型与用量」页展示调度健康度。
    """
    return ApiResponse(code=0, message="success", data=scheduler.pool_status())


# ============ 健康检查 ============
@app.get("/health")
def health() -> dict:
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    logger.info("ai-wiki-llm 启动: %s:%s", settings.server.host, settings.server.port)
    uvicorn.run("main:app", host=settings.server.host, port=settings.server.port, reload=False)