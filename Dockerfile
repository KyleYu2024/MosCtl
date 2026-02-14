# Build stage for mosctl
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o mosctl ./cmd/mosctl

# Final stage
FROM alpine:latest

# 强制设置时区环境变量
ENV TZ=Asia/Shanghai

# Prepare directories
RUN mkdir -p /etc/mosdns/rules

# Copy binaries
COPY mosdns /usr/local/bin/
COPY --from=builder /app/mosctl /usr/local/bin/

# Copy default config template
COPY templates/config.yaml /etc/mosdns/config.yaml.template

# Create placeholder files
RUN touch /etc/mosdns/rules/local_direct.txt \
    /etc/mosdns/rules/local_proxy.txt \
    /etc/mosdns/rules/user_iot.txt \
    /etc/mosdns/rules/hosts.txt

# Expose DNS port
EXPOSE 53/udp 53/tcp 8080/tcp

# Entrypoint
ENTRYPOINT ["mosctl"]
