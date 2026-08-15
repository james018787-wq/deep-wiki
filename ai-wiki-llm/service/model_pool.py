"""多模型池配置：加载 model_pool.yaml，支持 ${ENV} 密钥占位与 mtime 热重载。

与 Go 侧旧实现的语义保持一致：
  - 优先低价（按平均单价升序）调度；
  - global 缺省参数回填默认值；
  - 密钥占位符 ${ENV} 在加载时替换，密钥不入库；
  - 配置文件修改后约 5s 自动热重载，解析失败保留旧配置。
"""

import os
import threading
import time
from typing import Optional

import yaml

from utils.logging import logger


def _expand_env(value: str) -> str:
    """解析 ${ENV_VAR} 占位符为环境变量值；未定义时留空并告警。"""
    if "${" not in value:
        return value
    out = value
    for _ in range(5):
        changed = False
        start = out.find("${")
        if start < 0:
            break
        end = out.find("}", start)
        if end < 0:
            break
        key = out[start + 2:end]
        v = os.getenv(key, "")
        if not v:
            logger.warning("环境变量 %s 未配置，对应密钥为空", key)
        out = out[:start] + v + out[end + 1:]
        changed = True
        if not changed:
            break
    return out


class ModelItem:
    """单个模型配置项（对应 model_pool.yaml 的 model_pool 元素）。"""

    def __init__(self, cfg: dict) -> None:
        self.name: str = cfg.get("name", "")
        self.provider: str = cfg.get("provider", "")
        self.api_key: str = _expand_env(cfg.get("api_key", ""))
        self.base_url: str = cfg.get("base_url", "")
        self.input_price: float = float(cfg.get("input_price", 0) or 0)
        self.output_price: float = float(cfg.get("output_price", 0) or 0)
        self.max_context: int = int(cfg.get("max_context", 0) or 0)
        self.rpm: int = int(cfg.get("rpm", 0) or 0)
        self.tpm: int = int(cfg.get("tpm", 0) or 0)
        self.enable: bool = bool(cfg.get("enable", False))

    @property
    def avg_price(self) -> float:
        """平均单价（调度排序依据）。"""
        return (self.input_price + self.output_price) / 2


class GlobalConfig:
    """全局调度参数（对应 model_pool.yaml 的 global 段）。"""

    def __init__(self, cfg: Optional[dict] = None) -> None:
        cfg = cfg or {}
        self.max_retry_switch: int = int(cfg.get("max_retry_switch") or 2)
        self.circuit_ttl_sec: int = int(cfg.get("circuit_ttl_sec") or 30)
        self.circuit_failure_threshold: int = int(cfg.get("circuit_failure_threshold") or 3)
        self.ratelimit_window_sec: int = int(cfg.get("ratelimit_window_sec") or 60)
        self.high_quality_price_threshold: float = float(
            cfg.get("high_quality_price_threshold") or 0.2
        )


class ModelPool:
    """模型池：加载 model_pool.yaml 并后台热重载。

    文件缺失/解析失败时仅告警并保留空池（对话返回“模型池未配置”），不阻断服务启动。
    """

    def __init__(self, path: str) -> None:
        self._path = path
        self._lock = threading.RLock()
        self._global = GlobalConfig()
        self._models: list[ModelItem] = []
        self._mtime: float = 0.0
        self.reload()
        self._watcher = threading.Thread(target=self._watch, daemon=True, name="model-pool-watch")
        self._watcher.start()

    def _watch(self) -> None:
        while True:
            time.sleep(5)
            try:
                mtime = os.path.getmtime(self._path)
            except OSError:
                continue  # 文件暂不存在，等待下次
            with self._lock:
                changed = mtime != self._mtime
            if changed:
                logger.info("[model_pool] %s 变更，热重载模型池", self._path)
                self.reload()

    def reload(self) -> None:
        try:
            data, mtime = self._load_file()
        except Exception as e:  # noqa: BLE001
            logger.error("[model_pool] 加载模型配置失败，保留旧配置: %s", e)
            return
        models = [ModelItem(c) for c in data.get("model_pool", []) if isinstance(c, dict)]
        global_cfg = GlobalConfig(data.get("global") or {})
        with self._lock:
            self._models = models
            self._global = global_cfg
            self._mtime = mtime
        logger.info("[model_pool] 模型池加载完成，共 %d 个模型", len(models))

    def _load_file(self):
        """读取并解析 model_pool.yaml，返回 (data, mtime)。"""
        with open(self._path, "r", encoding="utf-8") as f:
            data = yaml.safe_load(f) or {}
        return data, os.path.getmtime(self._path)

    def global_config(self) -> GlobalConfig:
        with self._lock:
            return self._global

    def get(self, name: str) -> Optional[ModelItem]:
        """按名称查询启用模型；未启用返回 None。"""
        with self._lock:
            for m in self._models:
                if m.name == name and m.enable:
                    return m
        return None

    def candidates(self, force_high_quality: bool, estimated_tokens: int) -> list[ModelItem]:
        """按调度规则过滤并按平均单价升序返回候选模型。

        过滤规则：
          - enable=false 排除；
          - 预估上下文超过 max_context 的模型排除；
          - force_high_quality=true 时，过滤平均单价低于阈值的低价模型。
        """
        with self._lock:
            threshold = self._global.high_quality_price_threshold
            models = list(self._models)
        list_ = []
        for m in models:
            if not m.enable:
                continue
            if estimated_tokens > 0 and m.max_context > 0 and m.max_context < estimated_tokens:
                continue
            if force_high_quality and m.avg_price < threshold:
                continue
            list_.append(m)
        list_.sort(key=lambda m: m.avg_price)
        return list_