#!/bin/bash
set -e

# ================= 配置区 =================
REPO_URL="https://github.com/KyleYu2024/mosctl.git"
MOSDNS_VERSION="v5.3.3"
GH_PROXY="" 
# =========================================

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始 MosDNS 全自动部署 (v3.2 状态监控版)...${NC}"

# 1. 基础环境与日志修复
echo -e "${YELLOW}[1/8] 环境准备 & 修复日志系统...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip iptables

# 修复 PATH
if ! grep -q "/usr/local/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/bin
fi

# 修复 Journald 日志
mkdir -p /var/log/journal
if [ -f /etc/systemd/journald.conf ]; then
    sed -i 's/^#Storage=.*/Storage=persistent/' /etc/systemd/journald.conf
    sed -i 's/^Storage=.*/Storage=persistent/' /etc/systemd/journald.conf
    # 允许重启失败
    systemctl restart systemd-journald || echo -e "${YELLOW}⚠️  日志服务重启失败 (LXC限制)，跳过...${NC}"
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
    wget -q -O mosdns.zip "${GH_PROXY}https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
    unzip -o mosdns.zip
    mv mosdns /usr/local/bin/mosdns
    chmod +x /usr/local/bin/mosdns
else
    echo "MosDNS 已安装，跳过下载。"
fi

# 4. 生成 Mosctl 管理工具
echo -e "${YELLOW}[4/8] 生成 mosctl (v3.2)...${NC}"
cat > /usr/local/bin/mosctl <<EOF
#!/bin/bash
# MosDNS 管理工具 v3.2
RESCUE_DNS="223.5.5.5"
REPO_URL="${REPO_URL}"
GH_PROXY="${GH_PROXY}"
CONFIG_FILE="/etc/mosdns/config.yaml"
VERSION="${MOSDNS_VERSION}"

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
PLAIN='\033[0m'

# --- 核心功能 ---

rescue_enable() {
    if iptables -t nat -C PREROUTING -p udp --dport 53 -j DNAT --to-destination \$RESCUE_DNS 2>/dev/null; then
        echo -e "\${YELLOW}⚠️  救援模式已在运行中。\${PLAIN}"
        return
    fi
    echo -e "\${RED}🚑 正在启用救援模式 (转发 -> \$RESCUE_DNS)...\${PLAIN}"
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
    iptables -t nat -A PREROUTING -p udp --dport 53 -j DNAT --to-destination \$RESCUE_DNS
    iptables -t nat -A PREROUTING -p tcp --dport 53 -j DNAT --to-destination \$RESCUE_DNS
    iptables -t nat -A POSTROUTING -p udp -d \$RESCUE_DNS --dport 53 -j MASQUERADE
    iptables -t nat -A POSTROUTING -p tcp -d \$RESCUE_DNS --dport 53 -j MASQUERADE
    echo -e "\${GREEN}✅ 救援模式已开启！\${PLAIN}"
}

rescue_disable() {
    if [ "\$1" != "silent" ]; then echo -e "\${GREEN}♻️  正在关闭救援模式...\${PLAIN}"; fi
    iptables -t nat -D PREROUTING -p udp --dport 53 -j DNAT --to-destination \$RESCUE_DNS 2>/dev/null || true
    iptables -t nat -D PREROUTING -p tcp --dport 53 -j DNAT --to-destination \$RESCUE_DNS 2>/dev/null || true
    iptables -t nat -D POSTROUTING -p udp -d \$RESCUE_DNS --dport 53 -j MASQUERADE 2>/dev/null || true
    iptables -t nat -D POSTROUTING -p tcp -d \$RESCUE_DNS --dport 53 -j MASQUERADE 2>/dev/null || true
}

sync_config() {
    echo -e "\${YELLOW}☁️  正在从 GitHub 拉取最新配置...\${PLAIN}"
    TEMP_DIR=\$(mktemp -d)
    git clone --depth 1 "\${GH_PROXY}\${REPO_URL}" "\$TEMP_DIR" >/dev/null 2>&1
    
    if [ -f "\$TEMP_DIR/templates/config.yaml" ]; then
        echo "⚙️  应用新配置..."
        cp /etc/mosdns/config.yaml /etc/mosdns/config.yaml.bak
        cp "\$TEMP_DIR/templates/config.yaml" /etc/mosdns/config.yaml
        echo "🔄 重启服务..."
        if systemctl restart mosdns; then
            echo -e "\${GREEN}✅ 同步成功！\${PLAIN}"
            rm -rf "\$TEMP_DIR"
        else
            echo -e "\${RED}❌ 启动失败！自动回滚...\${PLAIN}"
            mv /etc/mosdns/config.yaml.bak /etc/mosdns/config.yaml
            systemctl restart mosdns
            rm -rf "\$TEMP_DIR"
        fi
    else
        echo -e "\${RED}❌ 拉取失败\${PLAIN}"
        rm -rf "\$TEMP_DIR"
    fi
}

change_upstream() {
    local type=\$1
    local tag_marker=\$2
    local default_proto=\$3
    
    echo -e "\n\${YELLOW}📝 修改 [\$type] DNS 上游\${PLAIN}"
    echo "当前配置行:"
    grep "\$tag_marker" \$CONFIG_FILE | grep -v "grep"
    echo
    echo -e "请输入新的地址 (例如: \${GREEN}223.5.5.5\${PLAIN} 或 \${GREEN}10.0.0.1:53\${PLAIN})"
    read -p "地址: " new_ip
    
    if [ -z "\$new_ip" ]; then echo "已取消"; return; fi
    
    if [[ "\$new_ip" != *"://"* ]]; then
        new_ip="\${default_proto}://\${new_ip}"
    fi
    
    echo "正在将上游修改为: \$new_ip"
    sed -i "s|\(.*\)- addr:.*\$tag_marker|\1- addr: \"\$new_ip\" \$tag_marker|" \$CONFIG_FILE
    
    echo "🔄 重启服务生效..."
    if systemctl restart mosdns; then
        echo -e "\${GREEN}✅ 修改成功！\${PLAIN}"
    else
        echo -e "\${RED}❌ 修改失败，请检查输入格式。\${PLAIN}"
    fi
}

config_menu() {
    clear
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "\${GREEN}    📝 修改 DNS 上游配置     \${PLAIN}"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "  1. 🇨🇳 修改国内 DNS (默认 UDP)"
    echo -e "  2. 🌍 修改国外 DNS (默认 TLS)"
    echo -e "  0. 🔙 返回主菜单"
    echo -e "\${GREEN}==============================\${PLAIN}"
    read -p "请选择 [0-2]: " sub_choice
    case "\$sub_choice" in
        1) change_upstream "国内" "# TAG_LOCAL" "udp" ;;
        2) change_upstream "国外" "# TAG_REMOTE" "tls" ;;
        0) return ;;
        *) echo -e "\${RED}无效选择\${PLAIN}" ;;
    esac
}

update_rules() {
    echo -e "\${YELLOW}⬇️  正在更新 GeoSite/GeoIP 规则数据库...\${PLAIN}"
    mkdir -p /etc/mosdns/rules
    dl() { wget -q -O "\$1" "\${GH_PROXY}\$2" && echo "  - \$1 更新成功"; }
    dl "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
    dl "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
    dl "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
    dl "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
    systemctl restart mosdns
    echo -e "\${GREEN}✅ 规则更新完毕！\${PLAIN}"
}

uninstall_mosdns() {
    echo -e "\${RED}⚠️  高危操作：即将彻底卸载 MosDNS！\${PLAIN}"
    read -p "确定要继续吗？(y/n): " confirm
    if [ "\$confirm" == "y" ]; then
        systemctl stop mosdns
        systemctl disable mosdns
        rm -f /etc/systemd/system/mosdns.service
        rm -f /etc/systemd/system/mosdns-rescue.service
        systemctl daemon-reload
        rm -rf /etc/mosdns
        rm -f /usr/local/bin/mosdns
        echo "nameserver 223.5.5.5" > /etc/resolv.conf
        echo -e "\${GREEN}✅ 卸载完成。\${PLAIN}"
        rm -f /usr/local/bin/mosctl
        exit 0
    fi
}

show_menu() {
    clear
    # 获取动态状态
    local status_raw=\$(systemctl is-active mosdns 2>/dev/null)
    local status_text=""
    if [ "\$status_raw" == "active" ]; then
        status_text="\${GREEN}🟢 运行中 (Active)\${PLAIN}"
    else
        status_text="\${RED}🔴 未运行 (\$status_raw)\${PLAIN}"
    fi

    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "\${GREEN}   MosDNS 管理面板 (v3.2)   \${PLAIN}"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e " Mos版本: \${GREEN}\${VERSION}\${PLAIN}"
    echo -e " 状态: \$status_text"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "  1. 🔄  同步配置 (Git Pull)"
    echo -e "  2. 📝  修改上游 DNS (填空模式)"
    echo -e "  3. ⬇️   更新规则 (Geo/IP库)"
    echo -e "  4. 🚑  开启救援模式 (Rescue)"
    echo -e "  5. ♻️   关闭救援模式 (Normal)"
    echo -e "  6. 📊  查看运行日志"
    echo -e "  7. ▶️   重启服务"
    echo -e "  8. 🗑️   卸载 MosDNS"
    echo -e "  0. 🚪  退出"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo
    read -p "请选择操作 [0-8]: " choice

    case "\$choice" in
        1) sync_config ;;
        2) config_menu ;;
        3) update_rules ;;
        4) rescue_enable ;;
        5) rescue_disable ;;
        6) journalctl -u mosdns -n 50 -f ;;
        7) systemctl restart mosdns && echo -e "\${GREEN}已重启\${PLAIN}" ;;
        8) uninstall_mosdns ;;
        0) exit 0 ;;
        *) echo -e "\${RED}无效选择\${PLAIN}" ;;
    esac
    
    if [ "\$choice" != "6" ] && [ "\$choice" != "0" ] && [ "\$choice" != "8" ] && [ "\$choice" != "2" ]; then
        echo
        read -p "按回车键返回主菜单..."
        show_menu
    elif [ "\$choice" == "2" ]; then
        show_menu
    fi
}

if [ \$# -gt 0 ]; then
    case "\$1" in
        rescue)
            if [ "\$2" == "enable" ]; then rescue_enable; elif [ "\$2" == "disable" ]; then rescue_disable; else echo "Usage: mosctl rescue {enable|disable}"; fi ;;
        sync) sync_config ;;
        update) update_rules ;;
        *) echo "Usage: mosctl [rescue|sync|update]" ;;
    esac
else
    show_menu
fi
EOF
chmod +x /usr/local/bin/mosctl

# 5. 下载规则
echo -e "${YELLOW}[5/8] 检查/下载规则文件...${NC}"
mkdir -p /etc/mosdns/rules
download_rule() {
    if [ ! -f "$1" ] || [ ! -s "$1" ]; then
        echo "Downloading $1..."
        wget -q -O "$1" "${GH_PROXY}$2"
    fi
}
download_rule "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
download_rule "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
download_rule "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
download_rule "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
touch /etc/mosdns/rules/{force-cn.txt,force-nocn.txt,hosts.txt,local-ptr.txt}

# 6. 初次配置
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
    echo -e "${GREEN}✅ 部署完成！(v3.2)${NC}"
    echo -e "👉 输入 ${GREEN}mosctl${NC} 即可打开管理菜单"
else
    echo -e "${RED}❌ 启动失败，请检查日志${NC}"
fi