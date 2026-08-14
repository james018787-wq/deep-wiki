"""重试与超时封装。

约束：
  1. LLM/向量库等外部调用统一走 retry_call，保证超时与重试策略一致。
  2. 仅对可重试异常（网络、限流、超时）重试，业务异常直接抛出。
"""

import time
from typing import Any, Callable

from tenacity import (
    retry,
    retry_if_exception_type,
    stop_after_attempt,
    wait_exponential,
)

from utils.errors import ServiceError
from utils.logging import logger


def default_should_retry(exception: BaseException) -> bool:
    """是否应重试：网络类/限流/超时异常可重试，业务异常不重试。"""
    if isinstance(exception, ServiceError):
        return False
    return True


def with_retry(
    fn: Callable[..., Any],
    *,
    max_retries: int = 3,
    timeout: float = 60.0,
    retry_wait: float = 1.0,
) -> Any:
    """带超时与指数退避重试的调用封装。

    Args:
        fn: 待执行函数
        max_retries: 最大重试次数（含首次）
        timeout: 单次调用超时（秒）
        retry_wait: 首次重试等待（秒），随后指数退避
    """
    r = retry(
        retry=retry_if_exception_type((TimeoutError, ConnectionError, OSError)),
        stop=stop_after_attempt(max_retries),
        wait=wait_exponential(multiplier=retry_wait, min=retry_wait, max=10.0),
        before_sleep=lambda s: logger.warning(
            "调用失败，即将重试(第%s次): %s", s.attempt_number, s.outcome.exception()
        ),
        reraise=True,
    )

    # tenacity 装饰函数签名需透传 timeout
    import functools

    @functools.wraps(fn)
    def _wrapped(*args, **kwargs):
        kwargs.setdefault("timeout", timeout)
        return r(fn)(*args, **kwargs)

    return _wrapped()


class TimeoutGuard:
    """同步超时守卫：对不支持 timeout 参数的调用做简单超时控制。

    说明：当前保留为通用超时封装，后续可替换为 asyncio.wait_for 异步版。
    """

    def __init__(self, seconds: float):
        self.seconds = seconds

    def run(self, fn: Callable[..., Any], *args, **kwargs) -> Any:
        start = time.monotonic()
        result = fn(*args, **kwargs)
        elapsed = time.monotonic() - start
        if elapsed > self.seconds:
            raise TimeoutError(f"调用超时，耗时 {elapsed:.2f}s，上限 {self.seconds}s")
        return result