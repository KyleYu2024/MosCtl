#!/bin/bash
set -e

# ================= 配置区 =================
REPO_URL="https://github.com/KyleYu2024/mosctl.git"
MOSDNS_VERSION="v5.3.3"

# [用户修改] 国内加速效果不佳，已禁用。如需启用，取消下一行的注释即可。
# GH_PROXY="https://ghproxy.net/"
GH_PROXY="" 
# =========================================

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始 MosDNS 全自动部署 (v2.1 直连版)...${NC}"

# 1. 基础环境与 PATH 修复
echo -e "${YELLOW}[1/8] 环境准备 & PATH 修复...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip iptables

if ! grep -q "/usr/local/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/bin
fi

# 2. 清理端口
echo -e "${YELLOW}[2/8] 清理 53 端口...${NC}"
systemctl stop systemd-resolved 2>/dev/null || true
systemctl disable systemd-resolved 2>/dev/null || true
rm -f /etc/resolv.conf
echo "nameserver 223.5.5.5" > /etc/resolv.conf
sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
echo "net.ipv4.ip_forward=1" > /etc/sysctl.d/99-mosdns.conf

# 3. 安装 MosDNS
echo -e "${YELLOW}[3/8] 安装 MosDNS 主程序...${NC}"
if [ ! -f "/usr/local/bin/mosdns" ]; then
    cd /tmp
    # 如果 GH_PROXY 为空，这里就是直连 GitHub
    wget -q -O mosdns.zip "${GH_PROXY}https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
    unzip -o mosdns.zip
    mv mosdns /usr/local/bin/mosdns
    chmod +x /usr/local/bin/mosdns
else
    echo "MosDNS 已安装，跳过下载。"
fi

# 4. 生成 Mosctl 管理工具 (集成 Sync 和 Rescue)
echo -e "${YELLOW}[4/8] 生成 mosctl (v2.1)...${NC}"
cat > /usr/local/bin/mosctl <<EOF
#!/bin/bash
# 配置
RESCUE_DNS="223.5.5.5"
REPO_URL="${REPO_URL}"
GH_PROXY="${GH_PROXY}"

# --- 功能: 救援模式 ---
rescue_enable() {
    if iptables -t nat -C PREROUTING -p udp --dport 53 -j DNAT --to-destination \$RESCUE_DNS 2>/dev/null; then
        echo "⚠️  救援模式已在运行中。"
        return
    fi
    echo "🚑 正在启用救援模式 (转发 -> \$RESCUE_DNS)..."
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
    iptables -t nat -A PREROUTING -p udp --dport 53 -j DNAT --to-destination \$RESCUE_DNS
    iptables -t nat -A PREROUTING -p tcp --dport 53 -j DNAT --to-destination \$RESCUE_DNS
    iptables -t nat -A POSTROUTING -p udp -d \$RESCUE_DNS --dport 53 -j MASQUERADE
    iptables -t nat -A POSTROUTING -p tcp -d \$RESCUE_DNS --dport 53 -j MASQUERADE
    echo "✅ 救援模式已开启！"
}

rescue_disable() {
    if [ "\$1" != "silent" ]; then echo "♻️  正在关闭救援模式..."; fi
    iptables -t nat -D PREROUTING -p udp --dport 53 -j DNAT --to-destination \$RESCUE_DNS 2>/dev/null || true
    iptables -t nat -D PREROUTING -p tcp --dport 53 -j DNAT --to-destination \$RESCUE_DNS 2>/dev/null || true
    iptables -t nat -D POSTROUTING -p udp -d \$RESCUE_DNS --dport 53 -j MASQUERADE 2>/dev/null || true
    iptables -t nat -D POSTROUTING -p tcp -d \$RESCUE_DNS --dport 53 -j MASQUERADE 2>/dev/null || true
}

# --- 功能: 同步配置 ---
sync_config() {
    echo "☁️  正在从 GitHub 拉取最新配置..."
    TEMP_DIR=\$(mktemp -d)
    
    # 使用 git clone 拉取 (如果不加代理，国内可能较慢)
    # 如果你在 LXC 已经配了系统代理，这里会自动走系统代理
    git clone --depth 1 "\${GH_PROXY}\${REPO_URL}" "\$TEMP_DIR" >/dev/null 2>&1
    
    if [ -f "\$TEMP_DIR/templates/config.yaml" ]; then
        echo "⚙️  发现新配置，正在应用..."
        cp /etc/mosdns/config.yaml /etc/mosdns/config.yaml.bak
        cp "\$TEMP_DIR/templates/config.yaml" /etc/mosdns/config.yaml
        
        echo "🔄 重启服务..."
        if systemctl restart mosdns; then
            echo "✅ 同步完成！服务运行正常。"
            rm -rf "\$TEMP_DIR"
        else
            echo "❌ 配置有误，服务启动失败！正在自动回滚..."
            mv /etc/mosdns/config.yaml.bak /etc/mosdns/config.yaml
            systemctl restart mosdns
            echo "⚠️  已回滚到上一个版本。"
            rm -rf "\$TEMP_DIR"
            exit 1
        fi
    else
        echo "❌ 拉取失败：仓库中未找到 templates/config.yaml"
        rm -rf "\$TEMP_DIR"
        exit 1
    fi
}

# --- 路由 ---
case "\$1" in
    rescue)
        if [ "\$2" == "enable" ]; then rescue_enable; elif [ "\$2" == "disable" ]; then rescue_disable; else echo "Usage: mosctl rescue {enable|disable}"; fi ;;
    sync)
        sync_config ;;
    restart)
        systemctl restart mosdns && echo "✅ 服务已重启" ;;
    log)
        journalctl -u mosdns -n 50 -f ;;
    *)
        echo "MosDNS CLI Tools (v2.1)"
        echo "Commands:"
        echo "  mosctl sync             同步 GitHub 最新配置"
        echo "  mosctl rescue enable    开启救援模式 (转发到阿里DNS)"
        echo "  mosctl rescue disable   关闭救援模式"
        echo "  mosctl restart          重启服务"
        echo "  mosctl log              查看日志"
        ;;
esac
EOF
chmod +x /usr/local/bin/mosctl

# 5. 下载规则 (直连 GitHub)
echo -e "${YELLOW}[5/8] 检查/下载规则文件...${NC}"
mkdir -p /etc/mosdns/rules
download_rule() {
    if [ ! -f "$1" ] || [ ! -s "$1" ]; then
        echo "Downloading $1..."
        # GH_PROXY 为空时，这里就是直连
        wget -q -O "$1" "${GH_PROXY}$2"
    fi
}
download_rule "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
download_rule "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
download_rule "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
download_rule "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
touch /etc/mosdns/rules/{force-cn.txt,force-nocn.txt,hosts.txt,local-ptr.txt}

# 6. 初次拉取配置
echo -e "${YELLOW}[6/8] 初始化配置...${NC}"
/usr/local/bin/mosctl sync

# 7. 配置 Systemd
echo -e "${YELLOW}[7/8] 配置服务...${NC}"
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

if systemctl is-active --quiet mosdns; then
    echo -e "${GREEN}✅ 部署完成！(v2.1 直连版)${NC}"
    echo "试一试: mosctl sync"
else
    echo -e "${RED}❌ 启动失败，请检查日志${NC}"
fi
