# ============ 运行阶段 ============
FROM python:3.11-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    TZ=Asia/Shanghai

WORKDIR /app

# 先拷贝依赖清单，利用 docker 层缓存加速构建
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# 拷贝源码
COPY . .

# 日志目录（compose 已挂载 ./ai-wiki-llm/logs）
RUN mkdir -p /app/logs

# 端口：与 docker-compose 中 ai-wiki-llm 服务 9000 保持一致
EXPOSE 9000

CMD ["python", "main.py"]