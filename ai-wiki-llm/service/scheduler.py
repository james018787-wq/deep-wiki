"""多模型调度器：优先低价，失败自动降级；Redis 分布式熔断/限流（fail-open）。

语义与旧 Go 侧实现保持一致：
  - force_model 非空 → 直连指定模型（不降级/不熔断/不限流）；
  - 候选按平均单价升序（低价优先）；
  - 可重试错误（429/5xx/超时/网络）→ 记失败计数并降级切换；
  - 鉴权失败（401/403）→ 直接标记模型不可用并切换；
  - 业务错误（400/404/上下文超限/解析）→ 不切换，直接返回；
  - 全部尝试仍失败 → 抛 LLMError（“所有模型服务暂时不可用”）；
  - Redis 不可用时熔断/限流 fail-open（记录告警，不阻断调用）。
"""

import threading
import time
import uuid
from typing import Optional

from openai import (
    APIConnectionError,
    APIStatusError,
    APITimeoutError,
    OpenAI,
    RateLimitError,
)

from config import settings
from service.model_pool import ModelItem, ModelPool
from utils.errors import LLMError
from utils.logging import logger

# ============ Redis key 与 Lua 脚本 ============

CIRCUIT_KEY_FMT = "ai_code_wiki:model:circuit:%s"
CIRCUIT_COUNT_KEY_FMT = "ai_code_wiki:model:circuit:count:%s"
RPM_KEY_FMT = "ai_code_wiki:model:rpm:%s"
TPM_KEY_FMT = "ai_code_wiki:model:tpm:%s"

# 连续失败计数 + 达标熔断（原子）
CIRCUIT_LUA = """
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
if n >= tonumber(ARGV[2]) then
  redis.call('SET', KEYS[2], '1', 'EX', ARGV[3])
  redis.call('DEL', KEYS[1])
  return 1
end
return 0
"""

# RPM/TPM 滑动窗口限流（原子）
RATE_LIMIT_LUA = """
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', tonumber(ARGV[1]) - tonumber(ARGV[2]))
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[3]) then return 0 end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[6])
redis.call('EXPIRE', KEYS[1], ARGV[7])

redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', tonumber(ARGV[1]) - tonumber(ARGV[2]))
local vals = redis.call('ZRANGE', KEYS[2], 0, -1)
local sum = 0
for _, v in ipairs(vals) do
  local sep = string.find(v, ':')
  if sep then sum = sum + tonumber(string.sub(v, 1, sep - 1)) end
end
if sum + tonumber(ARGV[4]) > tonumber(ARGV[5]) then return 0 end
redis.call('ZADD', KEYS[2], ARGV[1], ARGV[4] .. ':' .. ARGV[6])
redis.call('EXPIRE', KEYS[2], ARGV[7])
return 1
"""


def _redis_client():
    """按配置构建 Redis 客户端；未配置时返回 None（熔断/限流降级 fail-open）。"""
    cfg = settings.redis
    if cfg.addr:
        parts = cfg.addr.rsplit(":", 1)
        host, port = parts[0], int(parts[1]) if len(parts) == 2 else 6379
    elif cfg.host:
        host, port = cfg.host, cfg.port
    else:
        return None
    import redis as redis_lib

    return redis_lib.Redis(
        host=host,
        port=port,
        password=cfg.password or None,
        db=cfg.db,
        decode_responses=True,
        socket_timeout=2,
        socket_connect_timeout=2,
    )


class CircuitBreaker:
    """模型级熔断器（Redis 分布式状态）。

    规则：
      - 同一模型连续 circuit_failure_threshold 次可重试错误 → 熔断 circuit_ttl 秒，到期自动恢复；
      - 鉴权失败直接写入熔断标记；
      - 调用成功清空连续失败计数；
      - Redis 不可用时 fail-open。
    """

    def __init__(self, client, ttl_sec: int, threshold: int) -> None:
        self._r = client
        self._ttl = max(ttl_sec, 1)
        self._threshold = max(threshold, 1)

    def is_open(self, model: str) -> bool:
        if self._r is None:
            return False
        try:
            return bool(self._r.exists(CIRCUIT_KEY_FMT % model))
        except Exception as e:  # noqa: BLE001
            logger.warning("[scheduler] 熔断状态查询失败，fail-open model=%s err=%s", model, e)
            return False

    def record_failure(self, model: str) -> None:
        if self._r is None:
            return
        try:
            self._r.eval(
                CIRCUIT_LUA,
                2,
                CIRCUIT_COUNT_KEY_FMT % model,
                CIRCUIT_KEY_FMT % model,
                self._ttl,
                self._threshold,
                self._ttl,
            )
        except Exception as e:  # noqa: BLE001
            logger.warning("[scheduler] 熔断计数失败 model=%s err=%s", model, e)

    def record_success(self, model: str) -> None:
        if self._r is None:
            return
        try:
            self._r.delete(CIRCUIT_COUNT_KEY_FMT % model)
        except Exception as e:  # noqa: BLE001
            logger.warning("[scheduler] 熔断计数清空失败 model=%s err=%s", model, e)

    def mark_unavailable(self, model: str) -> None:
        if self._r is None:
            return
        try:
            self._r.set(CIRCUIT_KEY_FMT % model, "1", ex=self._ttl)
        except Exception as e:  # noqa: BLE001
            logger.warning("[scheduler] 标记模型不可用失败 model=%s err=%s", model, e)


class RateLimiter:
    """模型级 RPM/TPM 分布式限流（Redis ZSET 滑动窗口）。

    命中限流时 allow 返回 False（调度器跳过该模型并降级切换）；Redis 不可用 fail-open。
    """

    def __init__(self, client, window_sec: int) -> None:
        self._r = client
        self._window = max(window_sec, 1)

    def allow(self, model: str, rpm: int, tpm: int, tokens: int) -> bool:
        if self._r is None or (rpm <= 0 and tpm <= 0):
            return True
        now = int(time.time())
        member = f"{time.time_ns()}-{uuid.uuid4().hex}"
        try:
            n = self._r.eval(
                RATE_LIMIT_LUA,
                2,
                RPM_KEY_FMT % model,
                TPM_KEY_FMT % model,
                now,
                self._window,
                rpm,
                tokens,
                tpm,
                member,
                self._window * 2 + 60,
            )
            return n == 1
        except Exception as e:  # noqa: BLE001
            logger.warning("[scheduler] 限流判断失败，fail-open model=%s err=%s", model, e)
            return True


class _ErrorKind:
    """调用错误分类（决定是否降级切换）。"""

    RETRIABLE = "retriable"  # 429 / 5xx / 超时 / 网络
    AUTH = "auth"  # 401 / 403
    BUSINESS = "business"  # 400 / 404 / 上下文超限 / 解析


def _classify(exc: Exception) -> str:
    if isinstance(exc, RateLimitError):
        return _ErrorKind.RETRIABLE
    if isinstance(exc, APITimeoutError):
        return _ErrorKind.RETRIABLE
    if isinstance(exc, APIConnectionError):
        return _ErrorKind.RETRIABLE
    if isinstance(exc, APIStatusError):
        code = exc.status_code
        if code in (401, 403):
            return _ErrorKind.AUTH
        if code >= 500:
            return _ErrorKind.RETRIABLE
        return _ErrorKind.BUSINESS  # 400 / 404 等
    return _ErrorKind.RETRIABLE  # 其它异常按可重试处理


class Scheduler:
    """多模型调度器：低价优先，失败自动降级切换。"""

    def __init__(self, pool: ModelPool) -> None:
        self._pool = pool
        self._clients: dict[str, OpenAI] = {}
        self._client_lock = threading.Lock()
        self._r = _redis_client()
        if self._r is not None:
            try:
                self._r.ping()
            except Exception as e:  # noqa: BLE001
                logger.warning("[scheduler] Redis 不可用，熔断/限流降级 fail-open: %s", e)
        global_cfg = pool.global_config()
        self._cb = CircuitBreaker(self._r, global_cfg.circuit_ttl_sec, global_cfg.circuit_failure_threshold)
        self._rl = RateLimiter(self._r, global_cfg.ratelimit_window_sec)

    def _client(self, m: ModelItem) -> OpenAI:
        with self._client_lock:
            if m.name not in self._clients:
                self._clients[m.name] = OpenAI(
                    api_key=m.api_key or None,
                    base_url=m.base_url,
                    timeout=settings.llm.llm_timeout,
                    max_retries=0,  # 降级切换由调度器控制，客户端不重试
                )
            return self._clients[m.name]

    def chat(
        self,
        system: str,
        user: str,
        force_model: str = "",
        force_high_quality: bool = False,
        estimated_tokens: int = 0,
    ) -> dict:
        """按调度策略调用模型，返回 {answer, used_model, switch_count, input_token, output_token, cost, retried_models}。

        Raises:
            LLMError: 模型池未配置 / 指定模型不存在 / 所有模型调用失败。
        """
        # 1. force_model 直连
        if force_model:
            m = self._pool.get(force_model)
            if m is None:
                raise LLMError(message=f"指定模型不存在或未启用: {force_model}")
            content, usage = self._call(m, system, user)
            return self._result(m, content, usage, 0, [])

        # 2. 候选（低价优先）
        candidates = self._pool.candidates(force_high_quality, estimated_tokens)
        if not candidates:
            raise LLMError(message="模型池未配置或无可用的候选模型")

        max_switch = self._pool.global_config().max_retry_switch
        retried: list[str] = []
        switched = 0
        for m in candidates:
            if switched > max_switch:
                break
            if self._cb.is_open(m.name):
                retried.append(m.name)
                continue
            if not self._rl.allow(m.name, m.rpm, m.tpm, estimated_tokens):
                retried.append(m.name)
                continue

            try:
                content, usage = self._call(m, system, user)
            except Exception as e:  # noqa: BLE001
                kind = _classify(e)
                if kind == _ErrorKind.BUSINESS:
                    # 业务错误：不切换模型，直接返回
                    raise LLMError(message=f"模型调用被拒绝: {e}", cause=e) from e
                if kind == _ErrorKind.AUTH:
                    self._cb.mark_unavailable(m.name)
                else:
                    self._cb.record_failure(m.name)
                retried.append(m.name)
                switched += 1
                logger.warning("[scheduler] 模型调用失败，切换下一档 model=%s err=%s", m.name, e)
                continue

            self._cb.record_success(m.name)
            return self._result(m, content, usage, switched, retried)

        raise LLMError(message="所有模型服务暂时不可用，请稍后重试")

    def _call(self, m: ModelItem, system: str, user: str):
        """调用单个模型，返回 (content, usage)。"""
        client = self._client(m)
        messages = []
        if system:
            messages.append({"role": "system", "content": system})
        messages.append({"role": "user", "content": user})
        resp = client.chat.completions.create(
            model=m.name,
            messages=messages,
            temperature=0.2,
        )
        content = resp.choices[0].message.content if resp.choices else ""
        if not content:
            raise LLMError(message="模型返回空回答")
        usage = (getattr(resp.usage, "prompt_tokens", 0) or 0, getattr(resp.usage, "completion_tokens", 0) or 0)
        return content, usage

    @staticmethod
    def _result(m: ModelItem, content: str, usage: tuple[int, int], switched: int, retried: list[str]) -> dict:
        prompt_tokens, completion_tokens = usage
        cost = (prompt_tokens * m.input_price + completion_tokens * m.output_price) / 1000
        return {
            "answer": content,
            "used_model": m.name,
            "switch_count": switched,
            "input_token": prompt_tokens,
            "output_token": completion_tokens,
            "cost": round(cost, 6),
            "retried_models": retried,
        }


# 全局调度器单例（main.py 使用）
def build_scheduler() -> Scheduler:
    pool = ModelPool(settings.model_pool.file)
    return Scheduler(pool)