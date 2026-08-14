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


class VectorConfig:
    """向量库配置：当前使用 Chroma，预留 Milvus 切换能力"""

    engine: str = os.getenv("VECTOR_ENGINE", "chroma")  # chroma / milvus
    # docker-compose 场景直接使用 CHROMA_URL（如 http://chroma:8000）
    chroma_url: str = os.getenv("CHROMA_URL", "")
    chroma_host: str = os.getenv("CHROMA_HOST", "")
    chroma_port: int = int(os.getenv("CHROMA_PORT", "8000"))
    chroma_persist_dir: str = os.getenv("CHROMA_PERSIST_DIR", "./data/chroma")
    chroma_collection: str = os.getenv("CHROMA_COLLECTION", "code_doc")

    # Milvus 连接参数（切换引擎时使用）
    milvus_host: str = os.getenv("MILVUS_HOST", "localhost")
    milvus_port: int = int(os.getenv("MILVUS_PORT", "19530"))


class ServerConfig:
    """服务运行配置"""

    host: str = os.getenv("SERVER_HOST", "0.0.0.0")
    # 默认 9000，与 docker-compose 中 ai-wiki-llm 服务对外端口保持一致
    port: int = int(os.getenv("SERVER_PORT", "9000"))


class Settings:
    """全局配置聚合"""

    llm: LLMConfig = LLMConfig()
    vector: VectorConfig = VectorConfig()
    server: ServerConfig = ServerConfig()


settings = Settings()
