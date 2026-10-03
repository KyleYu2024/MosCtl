#!/usr/bin/env bash
set -euo pipefail

# Debian/Ubuntu systemd 安装；VERSION 可指定 vX.Y.Z，默认最新 Release。
REPO=KyleYu2024/MosCtl
VERSION=${VERSION:-latest}
MOSDNS_VERSION=v5.3.4
# 设置 GITHUB_PROXY=https://其他代理，或 GITHUB_PROXY= 使用直连。
GITHUB_PROXY=${GITHUB_PROXY-https://ghproxy.net}
[[ $(id -u) -eq 0 ]] || { echo '请以 root 执行安装脚本。' >&2; exit 1; }
command -v apt-get >/dev/null || { echo '仅支持 Debian/Ubuntu。' >&2; exit 1; }
command -v systemctl >/dev/null && [[ -d /run/systemd/system ]] || { echo '需要运行中的 systemd。' >&2; exit 1; }
case $(uname -m) in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo '不支持当前 CPU 架构。' >&2; exit 1 ;;
esac
[[ $VERSION == latest || $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'VERSION 应为 latest 或 vX.Y.Z。' >&2; exit 1; }
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl unzip dnsutils tar
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [[ $VERSION == latest ]]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
fetch() {
  local url=$1
  if [[ -n $GITHUB_PROXY ]]; then
    url="${GITHUB_PROXY%/}/$url"
  fi
  curl --fail --location --retry 3 --connect-timeout 15 "$url" -o "$2"
}
package="mosctl-linux-$arch.tar.gz"
fetch "$base/$package" "$work/$package"
fetch "$base/checksums-$arch.txt" "$work/checksums.txt"
# 只校验本次下载的包，不执行校验文件中的其他路径。
checksum=$(awk -v name="$package" '$2 == name {print $1}' "$work/checksums.txt")
[[ $checksum =~ ^[[:xdigit:]]{64}$ ]] || { echo '安装包校验文件无效。' >&2; exit 1; }
(cd "$work"; printf '%s  %s\n' "$checksum" "$package" | sha256sum -c -)
mkdir "$work/package"
tar -xzf "$work/$package" -C "$work/package"
[[ -f "$work/package/mosctl" && -f "$work/package/config.yaml" && -d "$work/package/rules" ]]
mkdir -p /usr/local/bin
if [[ ! -x /usr/local/bin/mosdns ]]; then
  fetch "https://github.com/IrineSistiana/mosdns/releases/download/$MOSDNS_VERSION/mosdns-linux-$arch.zip" "$work/mosdns.zip"
  mkdir "$work/kernel"
  unzip -q "$work/mosdns.zip" -d "$work/kernel"
  "$work/kernel/mosdns" version
  install -m 0755 "$work/kernel/mosdns" /usr/local/bin/mosdns
fi
mkdir -p /usr/local/bin /usr/share/mosdns/rules /etc/mosdns/rules
install -m 0644 "$work/package/config.yaml" /usr/share/mosdns/config.yaml
cp -a "$work/package/rules/." /usr/share/mosdns/rules/
new_login=false
if [[ ! -e /etc/mosctl.env ]]; then
  password=$(od -An -N18 -tx1 /dev/urandom | tr -d ' \n')
  (umask 077; cat > /etc/mosctl.env <<ENV
USERNAME=admin
PASSWORD=$password
WEB_LISTEN=:9090
TZ=Asia/Shanghai
ENV
  )
  new_login=true
fi
if [[ ! -e /etc/systemd/system/mosctl.service ]]; then
  cat > /etc/systemd/system/mosctl.service <<'SERVICE'
[Unit]
Description=MosCtl and MosDNS
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/mosctl.env
ExecStart=/usr/local/bin/mosctl
Restart=on-failure
RestartSec=3
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
SERVICE
fi
if [[ -f /usr/local/bin/mosctl ]]; then
  cp -p /usr/local/bin/mosctl /usr/local/bin/mosctl.backup
fi
install -m 0755 "$work/package/mosctl" /usr/local/bin/mosctl.new
mv /usr/local/bin/mosctl.new /usr/local/bin/mosctl
systemctl daemon-reload
systemctl enable mosctl
systemctl restart mosctl
sleep 3
if ! systemctl is-active --quiet mosctl; then
  echo '服务启动失败，请执行 journalctl -u mosctl -n 50 排查（检查 53 端口是否占用）。' >&2
  exit 1
fi
printf '\n安装完成，访问 http://<本机IP>:9090\n'
if $new_login; then
  printf '用户名：admin\n密码：%s\n' "$password"
else
  echo '沿用 /etc/mosctl.env 中的登录账号和设置。'
fi
echo 'REMOTE 上游可在 Web 设置页修改。再次运行本脚本可升级 MosCtl，保留已有配置和内核。'
