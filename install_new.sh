#!/bin/bash
set -e

# ================= 配置区 =================
REPO_URL="https://github.com/KyleYu2024/mosctl.git"
MOSDNS_VERSION="v5.3.3"
#GH_PROXY="https://ghproxy.net/"
# =========================================

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始 MosDNS 全自动部署 (Rescue版)...${NC}"

# 1. 基础环境与 PATH 修复
echo -e "${YELLOW}[1/8] 环境准备 & PATH 修复...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip iptables

# 永久修复 PATH 问题
if ! grep -q "/usr/local/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/bin
    echo "✅ PATH 已修正"
fi

# 2. 清理端口
echo -e "${YELLOW}[2/8] 清理 53 端口...${NC}"
systemctl stop systemd-resolved 2>/dev/null || true
systemctl disable systemd-resolved 2>/dev/null || true
rm -f /etc/resolv.conf
echo "nameserver 223.5.5.5" > /etc/resolv.conf
# 开启转发
sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
echo "net.ipv4.ip_forward=1" > /etc/sysctl.d/99-mosdns.conf

# 3. 安装 MosDNS
echo -e "${YELLOW}[3/8] 安装 MosDNS 主程序...${NC}"
cd /tmp
wget -q -O mosdns.zip "${GH_PROXY}https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
unzip -o mosdns.zip
mv mosdns /usr/local/bin/mosdns
chmod +x /usr/local/bin/mosdns

# 4. 生成 Mosctl 管理工具 (含 Rescue 逻辑)
echo -e "${YELLOW}[4/8] 生成 mosctl 管理脚本...${NC}"
cat > /usr/local/bin/mosctl <<'EOF'
#!/bin/bash
RESCUE_DNS="223.5.5.5"

rescue_enable() {
    if iptables -t nat -C PREROUTING -p udp --dport 53 -j DNAT --to-destination $RESCUE_DNS 2>/dev/null; then
        echo "⚠️  救援模式已在运行中。"
        return
    fi
    echo "🚑 正在启用救援模式 (转发 -> $RESCUE_DNS)..."
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
    iptables -t nat -A PREROUTING -p udp --dport 53 -j DNAT --to-destination $RESCUE_DNS
    iptables -t nat -A PREROUTING -p tcp --dport 53 -j DNAT --to-destination $RESCUE_DNS
    iptables -t nat -A POSTROUTING -p udp -d $RESCUE_DNS --dport 53 -j MASQUERADE
    iptables -t nat -A POSTROUTING -p tcp -d $RESCUE_DNS --dport 53 -j MASQUERADE
    echo "✅ 救援模式已开启！"
}

rescue_disable() {
    if [ "$1" != "silent" ]; then echo "♻️  正在关闭救援模式..."; fi
    iptables -t nat -D PREROUTING -p udp --dport 53 -j DNAT --to-destination $RESCUE_DNS 2>/dev/null || true
    iptables -t nat -D PREROUTING -p tcp --dport 53 -j DNAT --to-destination $RESCUE_DNS 2>/dev/null || true
    iptables -t nat -D POSTROUTING -p udp -d $RESCUE_DNS --dport 53 -j MASQUERADE 2>/dev/null || true
    iptables -t nat -D POSTROUTING -p tcp -d $RESCUE_DNS --dport 53 -j MASQUERADE 2>/dev/null || true
}

case "$1" in
    rescue)
        if [ "$2" == "enable" ]; then rescue_enable; elif [ "$2" == "disable" ]; then rescue_disable; else echo "Usage: mosctl rescue {enable|disable}"; fi ;;
    sync) echo "⚠️  CLI Sync 暂未集成" ;;
    *) echo "MosDNS CLI Tools"; echo "Usage: mosctl rescue {enable|disable}" ;;
esac
EOF
chmod +x /usr/local/bin/mosctl

# 5. 下载规则
echo -e "${YELLOW}[5/8] 下载规则文件...${NC}"
mkdir -p /etc/mosdns/rules
wget -q -O /etc/mosdns/rules/geosite_cn.txt "${GH_PROXY}https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
wget -q -O /etc/mosdns/rules/geoip_cn.txt "${GH_PROXY}https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
wget -q -O /etc/mosdns/rules/geosite_apple.txt "${GH_PROXY}https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
wget -q -O /etc/mosdns/rules/geosite_no_cn.txt "${GH_PROXY}https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
touch /etc/mosdns/rules/{force-cn.txt,force-nocn.txt,hosts.txt,local-ptr.txt}

# 6. 拉取配置
echo -e "${YELLOW}[6/8] 拉取 Config...${NC}"
cd ~ && rm -rf mosctl
git clone "${GH_PROXY}${REPO_URL}" mosctl || { echo -e "${RED}克隆失败${NC}"; exit 1; }
cp ~/mosctl/templates/config.yaml /etc/mosdns/config.yaml

# 7. 配置 Systemd (含 Rescue 联动)
echo -e "${YELLOW}[7/8] 配置服务 (OnFailure)...${NC}"
cat > /etc/systemd/system/mosdns-rescue.service <<EOF
[Unit]
Description=MosDNS Rescue Mode
After=network.target
[Service]
Type=oneshot
ExecStart=/usr/local/bin/mosctl rescue enable
EOF

cat > /etc/systemd/system/mosdns.service <<EOF
[Unit]
Description=MosDNS Service
After=network.target
OnFailure=mosdns-rescue.service
[Service]
Type=simple
ExecStartPre=-/usr/local/bin/mosctl rescue disable silent
ExecStart=/usr/local/bin/mosdns start -d /etc/mosdns
Restart=on-failure
RestartSec=5s
StartLimitInterval=60
StartLimitBurst=3
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
EOF

# 8. 启动
echo -e "${YELLOW}[8/8] 启动服务...${NC}"
systemctl daemon-reload
systemctl enable mosdns
systemctl restart mosdns
sleep 2

if systemctl is-active --quiet mosdns; then
    echo -e "${GREEN}✅ 部署成功！Rescue 机制已就绪。${NC}"
    echo "Mosctl 命令已可用 (尝试: mosctl rescue enable)"
else
    echo -e "${RED}❌ 启动失败，请检查日志${NC}"
fi
