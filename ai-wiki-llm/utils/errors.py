from typing import Any, Optional


class ServiceError(Exception):
    """业务异常基类，所有服务层自定义异常继承自此类。

    约束：捕获 LLM/向量库异常后转换为本类，对外友好返回错误信息。
    """

    def __init__(self, code: int, message: str, cause: Optional[Exception] = None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.cause = cause  # 原始异常，仅用于日志，不对外暴露

    def __str__(self) -> str:
        return f"[{self.code}] {self.message}"


class LLMError(ServiceError):
    """LLM 调用异常（超时、限流、网络错误等）"""

    def __init__(self, message: str = "LLM调用失败", cause: Optional[Exception] = None):
        super().__init__(code=5001, message=message, cause=cause)


class EmbeddingError(ServiceError):
    """向量化异常"""

    def __init__(self, message: str = "向量化失败", cause: Optional[Exception] = None):
        super().__init__(code=5002, message=message, cause=cause)


class VectorStoreError(ServiceError):
    """向量库操作异常"""

    def __init__(self, message: str = "向量库操作失败", cause: Optional[Exception] = None):
        super().__init__(code=5003, message=message, cause=cause)


class ParamError(ServiceError):
    """参数校验异常"""

    def __init__(self, message: str = "参数错误", cause: Optional[Exception] = None):
        super().__init__(code=4001, message=message, cause=cause)


def error_dict(error: ServiceError, data: Any = None) -> dict:
    """将业务异常转换为标准返回结构 dict。"""
    return {"code": error.code, "message": error.message, "data": data}