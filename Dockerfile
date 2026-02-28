# 阶段 1: 构建
FROM --platform=linux/amd64 golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && \
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o mosctl ./cmd/mosctl

# 阶段 2: 运行
FROM --platform=linux/amd64 alpine:3.21
RUN apk add --no-cache ca-certificates tzdata binutils && \
    mkdir -p /etc/mosdns/rules /usr/share/mosdns/rules /var/log

# 预置核心
COPY --from=builder /app/mosctl /usr/local/bin/mosctl
COPY --from=irinesistiana/mosdns:latest /usr/bin/mosdns /usr/local/bin/mosdns
COPY templates/config.yaml /usr/share/mosdns/config.yaml
COPY rules/ /usr/share/mosdns/rules/

RUN chmod +x /usr/local/bin/mosctl /usr/local/bin/mosdns

# 暴露 DNS 端口
EXPOSE 53/udp 53/tcp

# 强制注入 Docker 模式标识
ENV MOSCTL_MODE=docker

ENTRYPOINT ["/usr/local/bin/mosctl"]
