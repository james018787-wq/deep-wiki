# ============ 前端构建阶段（Vue3 + Vite） ============
FROM node:18-alpine AS web

WORKDIR /web

# 国内网络使用 npmmirror 加速依赖下载
ARG NPM_REGISTRY=https://registry.npmmirror.com
ENV NPM_CONFIG_REGISTRY=$NPM_REGISTRY

# 先拷贝依赖清单，利用 docker 层缓存加速构建
COPY web/package*.json ./
RUN npm install --no-audit --no-fund

# 拷贝源码并构建
COPY web/ .
RUN npm run build

# ============ Go 构建阶段 ============
FROM golang:1.22-alpine AS builder

WORKDIR /app

# 国内网络使用 goproxy.cn 加速依赖下载（构建时可用 --build-arg GOPROXY=... 覆盖）
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY

# 先拷贝依赖清单，利用 docker 层缓存加速构建
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ai-code-wiki ./cmd

# ============ 运行阶段 ============
FROM alpine:3.20

# git：pkg/git 通过命令行封装调用 git
# tzdata：时区支持
RUN apk add --no-cache ca-certificates git tzdata && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

WORKDIR /app

# 拷贝二进制与配置文件
COPY --from=builder /app/ai-code-wiki .
COPY --from=builder /app/config/config.yaml ./config/config.yaml
# 前端 SPA 构建产物（Vue3 + Vite），由 gin 根路径 + history fallback 挂载
COPY --from=web /web/dist ./web/dist

# 配置读取支持环境变量覆盖（DB_HOST/DB_PORT 等），默认走 config.yaml
EXPOSE 8080

CMD ["./ai-code-wiki"]