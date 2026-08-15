"""向量化服务：文本嵌入与向量接口。"""

from langchain_core.embeddings import Embeddings
from openai import OpenAI

from config import settings
from utils.errors import EmbeddingError
from utils.logging import logger


class OpenAICompatEmbeddings(Embeddings):
    """基于 OpenAI 客户端直连的 embedding 实现。

    不用 langchain 的 OpenAIEmbeddings：其 tiktoken 分词会把文本转成
    token ID 数组作为 input 发送，部分 OpenAI 兼容供应商（如硅基流动）
    会拒绝该参数（400 invalid）。此处直接用 openai 客户端以原始文本调用。
    """

    def __init__(self, client: OpenAI, model: str) -> None:
        self._client = client
        self._model = model

    def embed_query(self, text: str) -> list[float]:
        resp = self._client.embeddings.create(model=self._model, input=text)
        return resp.data[0].embedding

    def embed_documents(self, texts: list[str]) -> list[list[float]]:
        resp = self._client.embeddings.create(model=self._model, input=list(texts))
        return [d.embedding for d in resp.data]


class EmbedService:
    """文本向量化服务。

    约束：
      1. 提供统一 embedding 实例，供向量库复用。
      2. 捕获向量化异常，友好返回。
    """

    def __init__(self) -> None:
        try:
            # 独立 embedding 配置（与对话 LLM 解耦，见 config.EmbeddingConfig）
            client = OpenAI(
                api_key=settings.embedding.api_key,
                base_url=settings.embedding.base_url,
                timeout=float(settings.llm.llm_timeout),
                max_retries=int(settings.llm.llm_max_retries),
            )
            self._embeddings: Embeddings = OpenAICompatEmbeddings(client, settings.embedding.model)
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