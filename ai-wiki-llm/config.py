# ai-wiki-llm 服务配置（支持环境变量覆盖）
import os

from dotenv import load_dotenv

load_dotenv()


class LLMConfig:
    """大模型配置：支持切换 GPT4o / Claude"""

    # 模型供应商: openai / anthropic
    provider: str = os.getenv("LLM_PROVIDER", "openai")

    # OpenAI(GPT4o)
    openai_api_key: str = os.getenv("OPENAI_API_KEY", "")
    openai_base_url: str = os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
    # 兼容 docker-compose 中的 LLM_MODEL 环境变量
    openai_model: str = os.getenv("OPENAI_MODEL", os.getenv("LLM_MODEL", "gpt-4o"))

    # Anthropic(Claude)
    anthropic_api_key: str = os.getenv("ANTHROPIC_API_KEY", "")
    anthropic_base_url: str = os.getenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
    anthropic_model: str = os.getenv("ANTHROPIC_MODEL", "claude-3-5-sonnet-20240620")

    # 请求超时（秒）
    llm_timeout: float = float(os.getenv("LLM_TIMEOUT", "60"))

    # 重试配置
    llm_max_retries: int = int(os.getenv("LLM_MAX_RETRIES", "3"))
    llm_retry_wait: float = float(os.getenv("LLM_RETRY_WAIT", "1.0"))


class EmbeddingConfig:
    """向量化（embedding）配置：独立于对话 LLM。

    对话走 DeepSeek 等，而 DeepSeek 不提供 embedding，故此处单独配置
    一家 OpenAI 兼容协议的 embedding 供应商（如硅基流动 SiliconFlow）。
    """

    # OpenAI 兼容端点（硅基流动: https://api.siliconflow.cn/v1）
    base_url: str = os.getenv("EMBEDDING_BASE_URL", "https://api.siliconflow.cn/v1")
    api_key: str = os.getenv("EMBEDDING_API_KEY", "")
    # 模型名（硅基流动: BAAI/bge-large-zh-v1.5，维度 1024）
    model: str = os.getenv("EMBEDDING_MODEL", "BAAI/bge-large-zh-v1.5")


class RedisConfig:
    """Redis 配置：多模型调度的分布式熔断/限流状态存储。

    未配置（addr 与 host 均为空）时不启用熔断/限流（fail-open）。
    """

    addr: str = os.getenv("REDIS_ADDR", "")  # 形如 redis:6379（与 REDIS_HOST+PORT 二选一）
    host: str = os.getenv("REDIS_HOST", "")
    port: int = int(os.getenv("REDIS_PORT", "6379"))
    password: str = os.getenv("REDIS_PASSWORD", "")
    db: int = int(os.getenv("REDIS_DB", "0"))


class ModelPoolConfig:
    """多模型池配置（model_pool.yaml，支持热重载与 ${ENV} 密钥占位）。"""

    # 配置文件路径（相对工作目录；可由 MODEL_POOL_FILE 覆盖）
    file: str = os.getenv("MODEL_POOL_FILE", "model_pool.yaml")


class ServerConfig:
    """服务运行配置"""

    host: str = os.getenv("SERVER_HOST", "0.0.0.0")
    # 默认 9000，与 docker-compose 中 ai-wiki-llm 服务对外端口保持一致
    port: int = int(os.getenv("SERVER_PORT", "9000"))


class Settings:
    """全局配置聚合"""

    llm: LLMConfig = LLMConfig()
    embedding: EmbeddingConfig = EmbeddingConfig()
    redis: RedisConfig = RedisConfig()
    model_pool: ModelPoolConfig = ModelPoolConfig()
    server: ServerConfig = ServerConfig()


settings = Settings()
