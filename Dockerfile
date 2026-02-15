# Build stage for mosctl
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o mosctl ./cmd/mosctl

# Final stage
FROM alpine:latest

# Environment
ENV TZ=Asia/Shanghai

# Install dependencies
RUN apk add --no-cache bind-tools ca-certificates

# Prepare directories
RUN mkdir -p /etc/mosdns/rules

# Copy binaries
COPY mosdns /usr/local/bin/
COPY --from=builder /app/mosctl /usr/local/bin/

# Copy default config template
RUN mkdir -p /etc/mosctl
COPY templates/config.yaml /etc/mosctl/config.yaml.template

# Create placeholder files
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
