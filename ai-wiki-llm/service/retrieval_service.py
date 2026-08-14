"""RAG 基础能力：向量检索与简易重排。"""

import json
from typing import Optional

from schema.models import CandidateDoc, RerankItem
from service.vector_store import VectorStore
from utils.logging import logger


class RetrievalService:
    """RAG 检索服务：向量检索 + 简易相关性重排。

    约束：
      1. 重排为简易实现（关键词重叠 + 原始得分加权），后续可接入
         Cohere/BGE-Rerank 等专用重排模型。
      2. 最小切片单元=单个函数文档。
    """

    def __init__(self, store: VectorStore) -> None:
        self._store = store

    def search(self, query: str, module: Optional[str] = None, top_k: int = 5) -> list[dict]:
        """向量相似度检索。

        Args:
            query: 自然语言查询
            module: 模块过滤（可选）
            top_k: 返回条数
        """
        filter_meta = None
        if module:
            filter_meta = {"module_name": module}
        results = self._store.search(query, top_k=top_k, filter_meta=filter_meta)
        logger.info("RAG检索完成: query=%s hits=%d", query, len(results))
        return results

    def upsert_doc(self, doc_id: str, content: str, metadata: dict) -> None:
        """写入/更新单篇文档向量。

        Args:
            doc_id: 文档id（对应 Go 侧 code_function_doc.id）
            content: 文档内容（摘要+流程+风险点等）
            metadata: 元数据（module_name/func_name 等）
        """
        self._store.upsert_documents(
            texts=[content],
            metadatas=[metadata],
            ids=[doc_id],
        )

    def delete_doc(self, doc_id: str) -> None:
        """删除文档向量。"""
        self._store.delete_document(doc_id)

    def rerank(self, query: str, candidates: list[CandidateDoc]) -> list[RerankItem]:
        """候选文档重排（简易实现）。

        策略：结合原始得分与查询-文档关键词重叠度综合打分。

        Args:
            query: 用户查询
            candidates: 候选文档列表
        """
        query_terms = self._tokenize(query)
        scored: list[RerankItem] = []
        for c in candidates:
            overlap = self._overlap_score(query_terms, c.content)
            final_score = c.score * 0.6 + overlap * 0.4
            scored.append(RerankItem(
                doc_id=c.doc_id,
                module_name=c.module_name,
                func_name=c.func_name,
                content=c.content,
                score=round(final_score, 4),
            ))
        # 按综合得分降序
        scored.sort(key=lambda x: x.score, reverse=True)
        return scored

    @staticmethod
    def _tokenize(text: str) -> set[str]:
        """简易分词：中英文混合处理。"""
        tokens = set()
        for seg in text.replace("，", " ").replace("。", " ").replace(",", " ").split():
            seg = seg.strip().lower()
            if seg:
                tokens.add(seg)
        return tokens

    @staticmethod
    def _overlap_score(query_terms: set[str], content: str) -> float:
        """查询与文档内容的关键词重叠得分（0~1）。"""
        if not query_terms:
            return 0.0
        content_lower = content.lower()
        hit = sum(1 for t in query_terms if t in content_lower)
        return hit / len(query_terms)