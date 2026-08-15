# ============ 构建阶段 ============
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
# 极简前端静态页面（Vue3 CDN，无构建），由 gin /webstatic 挂载
COPY --from=builder /app/webstatic ./webstatic

# 配置读取支持环境变量覆盖（DB_HOST/DB_PORT 等），默认走 config.yaml
EXPOSE 8080

CMD ["./ai-code-wiki"]