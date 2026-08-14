import logging
import os
import sys

# 全局日志配置：结构化输出，便于检索定位。
# LOG_DIR 环境变量（docker-compose 挂载 ./ai-wiki-llm/logs）非空时同时写文件日志。
def setup_logging(level: int = logging.INFO) -> logging.Logger:
    logger = logging.getLogger("ai-wiki-llm")
    if logger.handlers:
        return logger
    logger.setLevel(level)

    formatter = logging.Formatter(
        "%(asctime)s | %(levelname)s | %(name)s | %(message)s"
    )

    # 控制台输出
    console = logging.StreamHandler(sys.stdout)
    console.setFormatter(formatter)
    logger.addHandler(console)

    # 文件输出（可选）
    log_dir = os.getenv("LOG_DIR", "")
    if log_dir:
        os.makedirs(log_dir, exist_ok=True)
        file_handler = logging.FileHandler(
            os.path.join(log_dir, "ai-wiki-llm.log"), encoding="utf-8"
        )
        file_handler.setFormatter(formatter)
        logger.addHandler(file_handler)

    return logger


logger = setup_logging()