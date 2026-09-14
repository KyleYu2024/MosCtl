# syntax=docker/dockerfile:1

ARG MOSDNS_IMAGE=irinesistiana/mosdns:latest
ARG MOSDNS_PLATFORM=linux/amd64

# 阶段 1: 构建 MosCtl
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY . .
RUN go mod tidy && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -ldflags="-s -w" -o mosctl ./cmd/mosctl

# 阶段 2: 锁定的 MosDNS 上游（可由 CI 传入 digest）
FROM --platform=${MOSDNS_PLATFORM} ${MOSDNS_IMAGE} AS mosdns-source

# 阶段 3: 运行
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata binutils && \
    mkdir -p /etc/mosdns/rules /usr/share/mosdns/rules /var/log

# 预置核心
COPY --from=builder /app/mosctl /usr/local/bin/mosctl
COPY --from=mosdns-source /usr/bin/mosdns /usr/local/bin/mosdns
COPY templates/config.yaml /usr/share/mosdns/config.yaml
COPY rules/ /usr/share/mosdns/rules/

RUN chmod +x /usr/local/bin/mosctl /usr/local/bin/mosdns

# 暴露 DNS 与 Web 管理端口
EXPOSE 53/udp 53/tcp
EXPOSE 9090/tcp

# 强制注入 Docker 模式标识
ENV MOSCTL_MODE=docker

ENTRYPOINT ["/usr/local/bin/mosctl"]
