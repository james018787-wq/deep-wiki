# ============ 运行阶段 ============
FROM python:3.11-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    TZ=Asia/Shanghai

WORKDIR /app

# 国内网络使用腾讯 PyPI 镜像加速依赖安装（构建时可用 --build-arg PIP_INDEX_URL=... 覆盖）
ARG PIP_INDEX_URL=https://mirrors.cloud.tencent.com/pypi/simple
ENV PIP_INDEX_URL=$PIP_INDEX_URL

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