"""向量化服务：文本嵌入与向量接口。"""

from langchain_core.embeddings import Embeddings
from langchain_openai import OpenAIEmbeddings

from config import settings
from utils.errors import EmbeddingError
from utils.logging import logger


class EmbedService:
    """文本向量化服务。

    约束：
      1. 提供统一 embedding 实例，供向量库复用。
      2. 捕获向量化异常，友好返回。
    """

    def __init__(self) -> None:
        try:
            # TODO: 后续支持切换嵌入模型/供应商（如本地 bge、Claude 等）
            self._embeddings: Embeddings = OpenAIEmbeddings(
                model=os_getenv("EMBEDDING_MODEL", "text-embedding-3-small"),
                api_key=settings.llm.openai_api_key,
                base_url=settings.llm.openai_base_url,
            )
        except Exception as e:  # noqa: BLE001
            raise EmbeddingError(message=f"初始化嵌入模型失败: {e}", cause=e) from e

    def embed_text(self, text: str) -> list[float]:
        """单条文本向量化，返回向量数组。

        Args:
            text: 待向量化文本
        """
        if not text or not text.strip():
            raise EmbeddingError(message="向量化文本不能为空")
        try:
            vector = self._embeddings.embed_query(text)
            logger.info("向量化完成: 文本长度=%d 维度=%d", len(text), len(vector))
            return vector
        except Exception as e:  # noqa: BLE001
            logger.error("向量化失败: %s", e)
            raise EmbeddingError(message=f"向量化失败: {e}", cause=e) from e

    def embed_documents(self, texts: list[str]) -> list[list[float]]:
        """批量文本向量化。"""
        try:
            return self._embeddings.embed_documents(texts)
        except Exception as e:  # noqa: BLE001
            logger.error("批量向量化失败: %s", e)
            raise EmbeddingError(message=f"批量向量化失败: {e}", cause=e) from e

    @property
    def embedding_model(self) -> Embeddings:
        """返回底层嵌入模型实例（供向量库构建使用）。"""
        return self._embeddings


def os_getenv(key: str, default: str = "") -> str:
    """轻量读取环境变量，避免顶部循环依赖 config。"""
    import os

    return os.getenv(key, default)