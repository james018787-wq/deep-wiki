"""向量库通用接口封装。

约束：屏蔽底层引擎差异，方便后期从 Chroma 切换到 Milvus。
当前使用 Chroma，实现与引擎无关的 store_vectors / search_similar 能力。

运行模式：
  1. HTTP 模式：配置 CHROMA_URL（docker-compose 连接 chroma 容器）。
  2. 持久化模式：本地开发，使用 persist_directory 嵌入式落盘。
"""

from typing import Optional
from urllib.parse import urlparse

import chromadb
from langchain_chroma import Chroma
from langchain_core.embeddings import Embeddings

from config import settings
from utils.errors import VectorStoreError
from utils.logging import logger


class VectorStore:
    """向量库通用接口（当前基于 Chroma 实现）。"""

    def __init__(self, embeddings: Embeddings):
        self._collection_name = settings.vector.chroma_collection
        try:
            self._store = self._build_store(embeddings)
        except Exception as e:  # noqa: BLE001
            raise VectorStoreError(f"初始化向量库失败: {e}", cause=e) from e
        logger.info("向量库已初始化: engine=%s collection=%s",
                    settings.vector.engine, self._collection_name)

    def _build_store(self, embeddings: Embeddings) -> Chroma:
        """按配置构建 Chroma 客户端：优先 HTTP 模式，其次嵌入式持久化。"""
        host, port = self._resolve_chroma_address()
        if host:
            # HTTP 模式：连接 chroma 容器（docker-compose 部署）
            client = chromadb.HttpClient(host=host, port=port)
            logger.info("Chroma 使用 HTTP 模式: %s:%d", host, port)
            return Chroma(
                client=client,
                collection_name=self._collection_name,
                embedding_function=embeddings,
            )
        # 嵌入式持久化模式：本地开发使用
        logger.info("Chroma 使用持久化模式: %s", settings.vector.chroma_persist_dir)
        return Chroma(
            collection_name=self._collection_name,
            embedding_function=embeddings,
            persist_directory=settings.vector.chroma_persist_dir,
        )

    def _resolve_chroma_address(self) -> tuple[str, int]:
        """解析 Chroma 地址：优先 CHROMA_URL，其次 CHROMA_HOST:CHROMA_PORT。"""
        url = settings.vector.chroma_url
        if url:
            parsed = urlparse(url)
            host = parsed.hostname or ""
            port = parsed.port or settings.vector.chroma_port
            return host, port
        return settings.vector.chroma_host, settings.vector.chroma_port

    def upsert_documents(self, texts: list[str], metadatas: list[dict], ids: list[str]) -> None:
        """写入/更新向量文档。

        Args:
            texts: 待向量化文本（单个函数文档为最小单元）
            metadatas: 元数据列表（module_name/func_name 等，用于过滤）
            ids: 文档id列表（与 Go 侧 code_function_doc.id 对应）
        """
        try:
            self._store.add_texts(texts=texts, metadatas=metadatas, ids=ids)
        except Exception as e:  # noqa: BLE001
            raise VectorStoreError(f"写入向量失败: {e}", cause=e) from e

    def search(self, query: str, top_k: int = 5, filter_meta: Optional[dict] = None) -> list[dict]:
        """相似度检索，返回按相关性排序的文档。

        Returns:
            [{"id": str, "text": str, "score": float, "metadata": dict}, ...]
        """
        try:
            results = self._store.similarity_search_with_relevance_scores(
                query=query, k=top_k, filter=filter_meta
            )
        except Exception as e:  # noqa: BLE001
            raise VectorStoreError(f"向量检索失败: {e}", cause=e) from e

        docs = []
        for doc, score in results:
            docs.append({
                "id": doc.metadata.get("doc_id", ""),
                "text": doc.page_content,
                "score": float(score),
                "metadata": doc.metadata,
            })
        return docs

    def delete_document(self, doc_id: str) -> None:
        """删除指定向量文档（人工校正/重置后同步更新索引）。"""
        try:
            self._store.delete(ids=[doc_id])
        except Exception as e:  # noqa: BLE001
            raise VectorStoreError(f"删除向量失败: {e}", cause=e) from e


class _MilvusVectorStore(VectorStore):
    """Milvus 实现占位，后期切换时补充 pymilvus 逻辑。"""

    pass  # TODO: 实现 Milvus 版 upsert/search