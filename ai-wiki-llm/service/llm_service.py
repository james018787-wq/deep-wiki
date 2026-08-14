"""LLM 请求封装：支持切换 GPT4o / Claude，带超时与重试。"""

from abc import ABC, abstractmethod

from langchain_anthropic import ChatAnthropic
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI

from config import settings
from utils.errors import LLMError
from utils.logging import logger
from utils.retry import with_retry


class LLMProvider(ABC):
    """LLM 供应商抽象基类，统一对外提供 chat 能力。"""

    @abstractmethod
    def chat(self, system_prompt: str, user_content: str) -> str:
        """调用模型，返回文本结果。"""
        raise NotImplementedError


class OpenAIProvider(LLMProvider):
    """GPT4o 供应商实现。"""

    def __init__(self) -> None:
        self._model: BaseChatModel = ChatOpenAI(
            model=settings.llm.openai_model,
            api_key=settings.llm.openai_api_key,
            base_url=settings.llm.openai_base_url,
            temperature=0.2,
            max_retries=settings.llm.llm_max_retries,
            request_timeout=settings.llm.llm_timeout,
        )

    def chat(self, system_prompt: str, user_content: str) -> str:
        messages = [
            SystemMessage(content=system_prompt),
            HumanMessage(content=user_content),
        ]
        response = self._model.invoke(messages)
        return response.content


class AnthropicProvider(LLMProvider):
    """Claude 供应商实现。"""

    def __init__(self) -> None:
        self._model: BaseChatModel = ChatAnthropic(
            model=settings.llm.anthropic_model,
            api_key=settings.llm.anthropic_api_key,
            base_url=settings.llm.anthropic_base_url,
            temperature=0.2,
            max_retries=settings.llm.llm_max_retries,
            timeout=settings.llm.llm_timeout,
        )

    def chat(self, system_prompt: str, user_content: str) -> str:
        messages = [
            SystemMessage(content=system_prompt),
            HumanMessage(content=user_content),
        ]
        response = self._model.invoke(messages)
        return response.content


class LLMService:
    """LLM 服务门面：按配置选择供应商，统一处理重试与异常。

    约束：
      1. 模型切换通过环境变量 LLM_PROVIDER=openai|anthropic 控制。
      2. 捕获 LLM 异常，转换后友好返回错误信息。
    """

    def __init__(self) -> None:
        self._provider: LLMProvider = self._build_provider()

    def _build_provider(self) -> LLMProvider:
        provider = settings.llm.provider.lower()
        if provider == "anthropic":
            logger.info("使用 Claude 模型: %s", settings.llm.anthropic_model)
            return AnthropicProvider()
        # 默认使用 OpenAI(GPT4o)
        logger.info("使用 GPT4o 模型: %s", settings.llm.openai_model)
        return OpenAIProvider()

    def chat(self, system_prompt: str, user_content: str) -> str:
        """带超时与重试的模型调用。

        Args:
            system_prompt: 系统提示词
            user_content: 用户内容
        """
        try:
            return with_retry(
                lambda timeout=None: self._provider.chat(system_prompt, user_content),
                max_retries=settings.llm.llm_max_retries,
                timeout=settings.llm.llm_timeout,
                retry_wait=settings.llm.llm_retry_wait,
            )
        except Exception as e:  # noqa: BLE001
            logger.error("LLM调用失败: %s", e)
            raise LLMError(message=f"LLM调用失败: {e}", cause=e) from e