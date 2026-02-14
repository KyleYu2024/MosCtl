# Build stage for mosctl
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o mosctl ./cmd/mosctl

# Final stage
FROM alpine:latest

# 1. 解决下载失败：拷贝根证书
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# 2. 解决时间不对：拷贝时区文件（如果 builder 有的话）并设置环境变量
ENV TZ=Asia/Shanghai

# Prepare directories
RUN mkdir -p /etc/mosdns/rules

# Copy binaries
COPY mosdns /usr/local/bin/
COPY --from=builder /app/mosctl /usr/local/bin/

# Copy default config template
COPY templates/config.yaml /etc/mosdns/config.yaml.template

# Create placeholder files (不再 touch cache.dump，让 mosctl 逻辑处理)
RUN touch /etc/mosdns/rules/local_direct.txt \
    /etc/mosdns/rules/local_proxy.txt \
    /etc/mosdns/rules/user_iot.txt \
    /etc/mosdns/rules/hosts.txt \
    /etc/mosdns/rules/geosite_cn.txt \
    /etc/mosdns/rules/geoip_cn.txt

# Expose DNS port
EXPOSE 53/udp 53/tcp 8080/tcp

# Entrypoint
ENTRYPOINT ["mosctl"]
