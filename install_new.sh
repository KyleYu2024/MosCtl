#!/bin/bash
set -e

# ================= 配置区 =================
REPO_URL="https://github.com/KyleYu2024/mosctl.git"
DEFAULT_MOSDNS_VERSION="v5.3.3"
SCRIPT_VERSION="v0.3.3"
# 【改动】采用更稳定的 gh-proxy.com 加速源
GH_PROXY="https://gh-proxy.com/"
# =========================================

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始 MosDNS 全自动部署 (${SCRIPT_VERSION})...${NC}"

# 1. 基础环境
echo -e "${YELLOW}[1/8] 环境准备...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip iptables

# 修复 PATH
if ! grep -q "/usr/local/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/bin
fi

# ================= 1.5 获取最新版本 =================
echo -e "${YELLOW}🔍 正在检查 MosDNS 最新版本...${NC}"
# 尝试获取最新版本，如果失败则使用默认
LATEST_TAG=$(curl -sL https://api.github.com/repos/IrineSistiana/mosdns/releases/latest | grep '"tag_name":' | cut -d'"' -f4)

if [ -n "$LATEST_TAG" ]; then
    MOSDNS_VERSION="$LATEST_TAG"
    echo -e "✅ 检测到最新版本: ${GREEN}${MOSDNS_VERSION}${NC}"
else
    MOSDNS_VERSION="$DEFAULT_MOSDNS_VERSION"
    echo -e "${RED}⚠️  无法获取最新版本，将使用稳定版: ${MOSDNS_VERSION}${NC}"
fi
# ===================================================

# 2. 清理端口
echo -e "${YELLOW}[2/8] 清理 53 端口...${NC}"
systemctl stop systemd-resolved 2>/dev/null || true
systemctl disable systemd-resolved 2>/dev/null || true
rm -f /etc/resolv.conf
echo "nameserver 223.5.5.5" > /etc/resolv.conf
sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
echo "net.ipv4.ip_forward=1" > /etc/sysctl.d/99-mosdns.conf

# 3. 安装 MosDNS
echo -e "${YELLOW}[3/8] 安装 MosDNS 主程序 (${MOSDNS_VERSION})...${NC}"
if [ ! -f "/usr/local/bin/mosdns" ]; then
    cd /tmp
    echo "正在下载内核文件..."
    # 下载内核也走代理
    wget -q --show-progress -O mosdns.zip "${GH_PROXY}https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
    
    unzip -o mosdns.zip > /dev/null 2>&1
    mv mosdns /usr/local/bin/mosdns
    chmod +x /usr/local/bin/mosdns
    echo -e "✅ 安装完成"
else
    echo "MosDNS 已安装，跳过下载。"
fi

# 4. 生成 Mosctl 管理工具
echo -e "${YELLOW}[4/8] 生成 mosctl (${SCRIPT_VERSION})...${NC}"
cat > /usr/local/bin/mosctl <<EOF
#!/bin/bash
# MosDNS 管理工具 ${SCRIPT_VERSION}
RESCUE_DNS="223.5.5.5"
REPO_URL="${REPO_URL}"
GH_PROXY="${GH_PROXY}"
CONFIG_FILE="/etc/mosdns/config.yaml"
KERNEL_VERSION="${MOSDNS_VERSION}"
SCRIPT_VER="${SCRIPT_VERSION}"
LOG_FILE="/var/log/mosdns.log"
CACHE_FILE="/etc/mosdns/cache.dump"

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
PLAIN='\033[0m'

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
    # 同步配置走代理
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
    
    if [[ -n "\$default_proto" ]] && [[ "\$new_ip" != *"://"* ]]; then
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

edit_rule() {
    local file=\$1
    local desc=\$2
    echo -e "\n\${YELLOW}📝 编辑 \$desc\${PLAIN}"
    echo "路径: \$file"
    echo "按 Ctrl+O 保存，Ctrl+X 退出。"
    read -p "按回车键开始编辑..."
    nano "\$file"
    systemctl restart mosdns
    echo -e "\${GREEN}✅ 规则已应用。\${PLAIN}"
}

flush_cache() {
    echo -e "\n\${YELLOW}🧹 正在清空 DNS 缓存...\${PLAIN}"
    if [ -f "\$CACHE_FILE" ]; then
        rm -f "\$CACHE_FILE"
        systemctl restart mosdns
        echo -e "\${GREEN}✅ 缓存已清空并重建！\${PLAIN}"
    else
        systemctl restart mosdns
        echo -e "\${GREEN}✅ 缓存文件不存在，已重启服务。\${PLAIN}"
    fi
}

rules_menu() {
    clear
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "\${GREEN}    📝 管理自定义规则列表    \${PLAIN}"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "  1. 🏠 自定义 Hosts (hosts.txt)"
    echo -e "  2. 🇨🇳 强制走国内 (force-cn.txt)"
    echo -e "  3. 🌍 强制走国外 (force-nocn.txt)"
    echo -e "  0. 🔙 返回主菜单"
    echo -e "\${GREEN}==============================\${PLAIN}"
    read -p "请选择: " sub_choice
    case "\$sub_choice" in
        1) edit_rule "/etc/mosdns/rules/hosts.txt" "自定义 Hosts" ;;
        2) edit_rule "/etc/mosdns/rules/force-cn.txt" "强制国内" ;;
        3) edit_rule "/etc/mosdns/rules/force-nocn.txt" "强制国外" ;;
        0) return ;;
        *) echo -e "\${RED}无效\${PLAIN}" ;;
    esac
}

config_menu() {
    clear
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "\${GREEN}    ⚙️  修改 DNS 上游配置     \${PLAIN}"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "  1. 🇨🇳 修改国内 DNS (默认补全 udp://)"
    echo -e "  2. 🌍 修改国外 DNS (不强制补全)"
    echo -e "  0. 🔙 返回主菜单"
    echo -e "\${GREEN}==============================\${PLAIN}"
    read -p "请选择: " sub_choice
    case "\$sub_choice" in
        1) change_upstream "国内" "# TAG_LOCAL" "udp" ;;
        2) change_upstream "国外" "# TAG_REMOTE" "" ;;
        0) return ;;
        *) echo -e "\${RED}无效\${PLAIN}" ;;
    esac
}

update_geo_rules() {
    echo -e "\${YELLOW}⬇️  正在更新 GeoSite/GeoIP 规则数据库...\${PLAIN}"
    mkdir -p /etc/mosdns/rules
    dl() { 
        echo -e "  ☁️  正在下载 \$1 ..."
        # 更新规则也走代理，并显示进度条
        wget -q --show-progress -O "\$1" "\${GH_PROXY}\$2"
        if [ \$? -eq 0 ]; then
             echo -e "  ✅ \$1 更新成功"
        else
             echo -e "  ❌ \$1 下载失败"
        fi
    }
    dl "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
    dl "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
    dl "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
    dl "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
    systemctl restart mosdns
    echo -e "\${GREEN}✅ 规则更新完毕！\${PLAIN}"
}

view_logs() {
    if [ -f "\$LOG_FILE" ]; then
        tail -n 50 -f "\$LOG_FILE"
    else
        echo -e "\${RED}❌ 未找到日志文件: \$LOG_FILE\${PLAIN}"
        echo "尝试使用 journalctl..."
        journalctl -u mosdns -n 50 -f
    fi
}

uninstall_mosdns() {
    echo -e "\${RED}⚠️  高危操作：此操作将删除 MosDNS 服务、所有配置文件及 mosctl 工具。\${PLAIN}"
    read -p "确定要彻底卸载吗？(y/n): " confirm
    if [ "\$confirm" == "y" ]; then
        systemctl stop mosdns
        systemctl disable mosdns
        rm -f /etc/systemd/system/mosdns.service
        rm -f /etc/systemd/system/mosdns-rescue.service
        systemctl daemon-reload
        rm -rf /etc/mosdns
        rm -f /usr/local/bin/mosdns
        rm -f /var/log/mosdns.log
        echo "nameserver 223.5.5.5" > /etc/resolv.conf
        echo -e "\${GREEN}✅ 卸载完成。再见！\${PLAIN}"
        rm -f /usr/local/bin/mosctl
        exit 0
    fi
}

show_menu() {
    clear
    local status_raw=\$(systemctl is-active mosdns 2>/dev/null)
    local status_text=""
    if [ "\$status_raw" == "active" ]; then status_text="\${GREEN}🟢 运行中\${PLAIN}"; else status_text="\${RED}🔴 未运行\${PLAIN}"; fi

    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "\${GREEN}   MosDNS 管理面板 (\${SCRIPT_VER})   \${PLAIN}"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e " 内核版本: \${GREEN}\${KERNEL_VERSION}\${PLAIN} | 状态: \$status_text"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo -e "  1. 🔄  同步配置 (Git Pull)"
    echo -e "  2. ⚙️   修改上游 DNS"
    echo -e "  3. 📝  管理自定义规则"
    echo -e "  4. ⬇️   更新 Geo 数据"
    echo -e "  5. 🚑  开启救援模式"
    echo -e "  6. ♻️   关闭救援模式"
    echo -e "  7. 📊  查看运行日志"
    echo -e "  8. 🧹  清空 DNS 缓存"
    echo -e "  9. ▶️   重启服务"
    echo -e "  10.🗑️   彻底卸载"
    echo -e "  0. 🚪  退出"
    echo -e "\${GREEN}==============================\${PLAIN}"
    echo
    read -p "请选择 [0-10]: " choice

    case "\$choice" in
        1) sync_config ;;
        2) config_menu ;;
        3) rules_menu ;;
        4) update_geo_rules ;;
        5) rescue_enable ;;
        6) rescue_disable ;;
        7) view_logs ;;
        8) flush_cache ;;
        9) systemctl restart mosdns && echo -e "\${GREEN}已重启\${PLAIN}" ;;
        10) uninstall_mosdns ;;
        0) exit 0 ;;
        *) echo -e "\${RED}无效\${PLAIN}" ;;
    esac
    
    if [ "\$choice" != "7" ] && [ "\$choice" != "0" ] && [ "\$choice" != "10" ] && [ "\$choice" != "2" ] && [ "\$choice" != "3" ]; then
        echo; read -p "按回车键返回..." ; show_menu
    elif [ "\$choice" == "2" ] || [ "\$choice" == "3" ]; then
        show_menu
    fi
}

if [ \$# -gt 0 ]; then
    case "\$1" in
        rescue)
            if [ "\$2" == "enable" ]; then rescue_enable; elif [ "\$2" == "disable" ]; then rescue_disable; else echo "Usage: mosctl rescue {enable|disable}"; fi ;;
        sync) sync_config ;;
        update) update_geo_rules ;;
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
        # 初次下载也走代理
        wget -q --show-progress -O "$1" "${GH_PROXY}$2"
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

# ================= 交互式配置环节 =================
echo -e "${YELLOW}[6.5/8] 交互式配置向导...${NC}"
if [ -c /dev/tty ]; then
    read -p "是否现在配置上游 DNS？(y/n) [y]: " config_confirm < /dev/tty
else
    config_confirm="n"
fi
config_confirm=${config_confirm:-y}

if [[ "$config_confirm" == "y" ]]; then
    # 1. 国内
    read -p "请输入国内 DNS (回车默认 udp://119.29.29.29): " local_dns < /dev/tty
    local_dns=${local_dns:-"udp://119.29.29.29"}
    if [[ "$local_dns" != *"://"* ]]; then local_dns="udp://${local_dns}"; fi
    sed -i "s|\(.*\)- addr:.*# TAG_LOCAL|\1- addr: \"${local_dns}\" # TAG_LOCAL|" /etc/mosdns/config.yaml
    echo "  - 国内 DNS 已设置为: $local_dns"

    # 2. 国外
    read -p "请输入国外 DNS (回车默认 10.10.2.252:53): " remote_dns < /dev/tty
    remote_dns=${remote_dns:-"10.10.2.252:53"}
    sed -i "s|\(.*\)- addr:.*# TAG_REMOTE|\1- addr: \"${remote_dns}\" # TAG_REMOTE|" /etc/mosdns/config.yaml
    echo "  - 国外 DNS 已设置为: $remote_dns"
fi
# =================================================

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
    echo -e "${GREEN}✅ 部署完成！(${SCRIPT_VERSION})${NC}"
    echo -e "👉 输入 ${GREEN}mosctl${NC} 即可打开管理菜单"
else
    echo -e "${RED}❌ 启动失败，请检查日志${NC}"
fi