#!/bin/bash
set -e

# ================= 配置区 =================
REPO_URL="https://github.com/KyleYu2024/mosctl.git"
DEFAULT_MOSDNS_VERSION="v5.3.3"
SCRIPT_VERSION="v1.0.9"
GH_PROXY="https://gh-proxy.com/"
# =========================================

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始 MosDNS 全自动部署 (${SCRIPT_VERSION} 修复版)...${NC}"

# 1. 基础环境
echo -e "${YELLOW}[1/8] 环境准备...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip iptables cron

# 修复 PATH
if ! grep -q "/usr/local/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/bin
fi

# ================= 1.5 获取最新版本 =================
echo -e "${YELLOW}🔍 正在检查 MosDNS 最新版本...${NC}"
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
    # 增加 || true 防止 git 失败导致脚本退出
    git clone --depth 1 "\${GH_PROXY}\${REPO_URL}" "\$TEMP_DIR" >/dev/null 2>&1 || true
    
    if [ -f "\$TEMP_DIR/templates/config.yaml" ]; then
        echo "⚙️  应用新配置..."
        local old_ttl=""
        local old_local_dns=""
        local old_remote_dns=""

        if [ -f "/etc/mosdns/config.yaml" ]; then
            cp /etc/mosdns/config.yaml /etc/mosdns/config.yaml.bak
            old_ttl=\$(grep "lazy_cache_ttl:" /etc/mosdns/config.yaml | awk '{print \$2}')
            old_local_dns=\$(grep "# TAG_LOCAL" /etc/mosdns/config.yaml | cut -d '"' -f 2)
            old_remote_dns=\$(grep "# TAG_REMOTE" /etc/mosdns/config.yaml | cut -d '"' -f 2)
        fi
        
        mkdir -p /etc/mosdns
        cp "\$TEMP_DIR/templates/config.yaml" /etc/mosdns/config.yaml
        rm -rf "\$TEMP_DIR"

        if [ -n "\$old_ttl" ]; then
            sed -i "s/lazy_cache_ttl: [0-9]*/lazy_cache_ttl: \${old_ttl}/" /etc/mosdns/config.yaml
        fi
        if [ -n "\$old_local_dns" ]; then
            sed -i "s|\(.*\)- addr:.*# TAG_LOCAL|\1- addr: \"\${old_local_dns}\" # TAG_LOCAL|" /etc/mosdns/config.yaml
        fi
        if [ -n "\$old_remote_dns" ]; then
            sed -i "s|\(.*\)- addr:.*# TAG_REMOTE|\1- addr: \"\${old_remote_dns}\" # TAG_REMOTE|" /etc/mosdns/config.yaml
        fi

        if systemctl list-units --full -all | grep -q "mosdns.service"; then
            echo "🔄 重启服务..."
            systemctl reset-failed mosdns 2>/dev/null
            if systemctl restart mosdns; then
                echo -e "\${GREEN}✅ 同步成功！(配置已保留)\${PLAIN}"
            else
                echo -e "\${RED}❌ 启动失败！自动回滚...\${PLAIN}"
                if [ -f "/etc/mosdns/config.yaml.bak" ]; then
                    mv /etc/mosdns/config.yaml.bak /etc/mosdns/config.yaml
                    systemctl restart mosdns
                fi
            fi
        else
            echo -e "\${GREEN}✅ 初始配置已写入。 (等待服务启动)\${PLAIN}"
        fi
    else
        echo -e "\${RED}❌ 拉取失败，跳过同步步骤${PLAIN}"
        rm -rf "\$TEMP_DIR"
        return 1
    fi
}

change_upstream() {
    local type=\$1
    local tag_marker=\$2
    local default_proto=\$3
    echo -e "\n\${YELLOW}📝 修改 [\$type] DNS 上游\${PLAIN}"
    grep "\$tag_marker" \$CONFIG_FILE | grep -v "grep"
    read -p "地址: " new_ip
    if [ -z "\$new_ip" ]; then echo "已取消"; return; fi
    if [[ -n "\$default_proto" ]] && [[ "\$new_ip" != *"://"* ]]; then new_ip="\${default_proto}://\${new_ip}"; fi
    sed -i "s|\(.*\)- addr:.*\$tag_marker|\1- addr: \"\$new_ip\" \$tag_marker|" \$CONFIG_FILE
    systemctl restart mosdns && echo -e "\${GREEN}✅ 修改成功！\${PLAIN}"
}

change_cache_ttl() {
    local new_ttl=\$1
    if [ -z "\$new_ttl" ]; then
        echo -e "\n\${YELLOW}⏱️  修改 DNS 缓存时间 (TTL)\${PLAIN}"
        echo "当前配置: \$(grep "lazy_cache_ttl" \$CONFIG_FILE | awk '{print \$2}') 秒"
        read -p "请输入新的缓存时间 (秒): " new_ttl
    fi
    if [[ ! "\$new_ttl" =~ ^[0-9]+$ ]]; then echo -e "\${RED}❌ 错误：TTL 必须是数字\${PLAIN}"; return 1; fi
    echo "修改缓存时间为: \${new_ttl} 秒"
    sed -i "s/lazy_cache_ttl: [0-9]*/lazy_cache_ttl: \${new_ttl}/" \$CONFIG_FILE
    systemctl restart mosdns && echo -e "\${GREEN}✅ 缓存时间已修改！\${PLAIN}"
}

run_test() {
    echo -e "\n\${YELLOW}🩺 正在进行 DNS 解析诊断 (检查 IP 类型)...\${PLAIN}"
    check_domain() {
        local domain=\$1
        local label=\$2
        echo -n "  Testing \$label (\$domain) ... "
        local start_time=\$(date +%s%3N)
        local result=\$(nslookup "\$domain" 127.0.0.1 2>&1)
        local exit_code=\$?
        local duration=\$((\$(date +%s%3N) - start_time))

        if [ \$exit_code -eq 0 ]; then
            local ip=\$(echo "\$result" | grep "Address:" | grep -v "#53" | grep -v "127.0.0.1" | grep -v "::1" | awk '{print \$2}' | head -n 1)
            if [ -z "\$ip" ]; then ip=\$(echo "\$result" | tail -n 2 | grep -E -o "([0-9]{1,3}[\.]){3}[0-9]{1,3}" | head -n 1); fi
            echo -e "\${GREEN}✅ Pass (\${duration}ms)\${NC} -> IP: \${YELLOW}\${ip}\${NC}"
        else
            echo -e "\${RED}❌ Failed (Timeout)\${NC}"
        fi
    }
    check_domain "www.baidu.com" "🇨🇳 国内"
    check_domain "www.google.com" "🌍 国外"
    echo ""
}

edit_rule() {
    local file=\$1
    echo "路径: \$file"
    read -p "按回车键开始编辑..."
    nano "\$file"
    systemctl restart mosdns && echo -e "\${GREEN}✅ 规则已应用。\${PLAIN}"
}

flush_cache() {
    rm -f "\$CACHE_FILE"
    systemctl restart mosdns && echo -e "\${GREEN}✅ 缓存已清空！\${PLAIN}"
}

rules_menu() {
    clear
    echo "  1. 🏠 自定义 Hosts"
    echo "  2. 🇨🇳 强制走国内"
    echo "  3. 🌍 强制走国外"
    read -p "请选择: " sub_choice
    case "\$sub_choice" in
        1) edit_rule "/etc/mosdns/rules/hosts.txt" ;;
        2) edit_rule "/etc/mosdns/rules/force-cn.txt" ;;
        3) edit_rule "/etc/mosdns/rules/force-nocn.txt" ;;
    esac
}

# ⚠️ 这个函数就是之前缺失的，现在补上了！
config_menu() {
    clear
    echo -e "\${GREEN}=====================================\${PLAIN}"
    echo -e "\${GREEN}    ⚙️  修改 DNS 上游配置     \${PLAIN}"
    echo -e "\${GREEN}=====================================\${PLAIN}"
    echo -e "  1. 🇨🇳 修改国内 DNS (默认补全 udp://)"
    echo -e "  2. 🌍 修改国外 DNS (不强制补全)"
    echo -e "  0. 🔙 返回主菜单"
    echo -e "\${GREEN}=====================================\${PLAIN}"
    read -p "请选择: " sub_choice
    case "\$sub_choice" in
        1) change_upstream "国内" "# TAG_LOCAL" "udp" ;;
        2) change_upstream "国外" "# TAG_REMOTE" "" ;;
        0) return ;;
        *) echo -e "\${RED}无效\${PLAIN}" ;;
    esac
}

update_geo_rules() {
    echo -e "\${YELLOW}⬇️  正在更新 GeoSite/GeoIP...\${PLAIN}"
    mkdir -p /etc/mosdns/rules
    dl() { if [ ! -f "\$1" ]; then wget -q --show-progress -O "\$1" "\${GH_PROXY}\$2"; fi; }
    dl "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
    dl "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
    dl "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
    dl "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
    systemctl restart mosdns
    echo -e "\${GREEN}✅ 规则更新完毕！\${PLAIN}"
}

view_logs() {
    tail -n 50 -f "\$LOG_FILE"
}

uninstall_mosdns() {
    read -p "确定卸载吗？(y/n): " confirm
    if [ "\$confirm" == "y" ]; then
        systemctl stop mosdns
        systemctl disable mosdns
        rm -f /etc/systemd/system/mosdns*
        systemctl daemon-reload
        rm -rf /etc/mosdns /usr/local/bin/mosdns /var/log/mosdns.log
        crontab -l 2>/dev/null | grep -v "mosctl update" | crontab -
        echo "nameserver 223.5.5.5" > /etc/resolv.conf
        rm -f /usr/local/bin/mosctl
        echo -e "\${GREEN}✅ 卸载完成。\${PLAIN}"
        exit 0
    fi
}

show_menu() {
    clear
    local status_raw=\$(systemctl is-active mosdns 2>/dev/null)
    local status_text=""
    if [ "\$status_raw" == "active" ]; then status_text="\${GREEN}🟢 运行中\${PLAIN}"; else status_text="\${RED}🔴 未运行\${PLAIN}"; fi

    echo -e "\${GREEN}=====================================\${PLAIN}"
    echo -e "\${GREEN}      MosDNS 管理面板 (\${SCRIPT_VER})      \${PLAIN}"
    echo -e "\${GREEN}=====================================\${PLAIN}"
    echo -e " 内核版本: \${GREEN}\${KERNEL_VERSION}\${PLAIN} | 状态: \$status_text"
    echo -e "\${GREEN}=====================================\${PLAIN}"
    echo -e "   1. 🔄  同步配置 (Git Pull)"
    echo -e "   2. ⚙️   修改上游 DNS"
    echo -e "   3. 📝  管理自定义规则"
    echo -e "   4. ⬇️   更新 Geo 数据"
    echo -e "   5. 🚑  开启救援模式"
    echo -e "   6. ♻️   关闭救援模式"
    echo -e "   7. 📊  查看运行日志"
    echo -e "   8. 🧹  清空 DNS 缓存"
    echo -e "   9. ▶️   重启服务"
    echo -e "  10. 🩺  DNS 解析测试"
    echo -e "  11. ⏱️   设置缓存 TTL"
    echo -e "  12. 🗑️   彻底卸载"
    echo -e "   0. 🚪  退出"
    echo -e "\${GREEN}=====================================\${PLAIN}"
    echo
    read -p "请选择: " choice

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
        10) run_test; read -p "按回车继续..." ;;
        11) change_cache_ttl ;;
        12) uninstall_mosdns ;;
        0) exit 0 ;;
        *) echo -e "\${RED}无效\${PLAIN}" ;;
    esac
    if [[ "\$choice" != "7" && "\$choice" != "10" ]]; then read -p "按回车键返回..."; show_menu; fi
}

if [ \$# -gt 0 ]; then
    case "\$1" in
        rescue)
            if [ "\$2" == "enable" ]; then rescue_enable; elif [ "\$2" == "disable" ]; then rescue_disable; else echo "Usage: mosctl rescue {enable|disable}"; fi ;;
        sync) sync_config ;;
        update) update_geo_rules ;;
        flush) flush_cache ;;
        cache-ttl) change_cache_ttl "\$2" ;;
        test) run_test ;;
        version) echo "${KERNEL_VERSION}" ;;
        *) echo "Usage: mosctl [rescue|sync|update|flush|cache-ttl|test|version]" ;;
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
    if [ ! -f "$1" ]; then
        echo "Downloading $1..."
        wget -q --show-progress -O "$1" "${GH_PROXY}$2"
    fi
}
download_rule "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
download_rule "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
download_rule "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
download_rule "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"
touch /etc/mosdns/rules/{force-cn.txt,force-nocn.txt,hosts.txt,local-ptr.txt}

# 6. 初次配置 (允许失败，不中断)
echo -e "${YELLOW}[6/8] 初始化配置...${NC}"
/usr/local/bin/mosctl sync || echo -e "${RED}同步配置失败，稍后请手动同步...${NC}"

# ================= 交互式配置环节 (修复版) =================
# 即使 mosctl sync 失败，也要让用户配置，避免服务无法启动
echo -e "${YELLOW}[6.5/8] 交互式配置向导...${NC}"

# 强制提示，不跳过
echo -e "请配置 DNS 上游（按回车使用默认值）"

echo -n "国内 DNS (默认 udp://119.29.29.29): "
read local_dns
local_dns=${local_dns:-"udp://119.29.29.29"}
if [[ "$local_dns" != *"://"* ]]; then local_dns="udp://${local_dns}"; fi

echo -n "国外 DNS (默认 10.10.2.252:53): "
read remote_dns
remote_dns=${remote_dns:-"10.10.2.252:53"}

# 写入配置文件
mkdir -p /etc/mosdns
# 确保文件存在（如果 sync 失败）
if [ ! -f /etc/mosdns/config.yaml ]; then
    echo "log: {level: info, file: '/var/log/mosdns.log'}" > /etc/mosdns/config.yaml
    echo "plugins: []" >> /etc/mosdns/config.yaml
    echo "# TAG_LOCAL" >> /etc/mosdns/config.yaml
    echo "# TAG_REMOTE" >> /etc/mosdns/config.yaml
    echo -e "${RED}⚠️  注意：配置文件是从空生成的，请务必执行 'mosctl sync' 修复！${NC}"
fi

sed -i "s|\(.*\)- addr:.*# TAG_LOCAL|\1- addr: \"${local_dns}\" # TAG_LOCAL|" /etc/mosdns/config.yaml
sed -i "s|\(.*\)- addr:.*# TAG_REMOTE|\1- addr: \"${remote_dns}\" # TAG_REMOTE|" /etc/mosdns/config.yaml

echo "  - 国内 DNS: $local_dns"
echo "  - 国外 DNS: $remote_dns"
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
StartLimitInterval=0
Type=simple
ExecStartPre=-/usr/local/bin/mosctl rescue disable silent
ExecStart=/usr/local/bin/mosdns start -d /etc/mosdns
Restart=on-failure
RestartSec=3s
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
EOF

# 7.5 配置自动更新 (Crontab)
echo -e "${YELLOW}[7.5/8] 配置自动更新任务 (每天凌晨 2 点)...${NC}"
if ! crontab -l 2>/dev/null | grep -q "mosctl update"; then
    (crontab -l 2>/dev/null; echo "0 2 * * * /usr/local/bin/mosctl update > /dev/null 2>&1") | crontab -
    echo -e "${GREEN}✅ 已添加自动更新计划任务${NC}"
else
    echo "计划任务已存在，跳过。"
fi

# 8. 启动
echo -e "${YELLOW}[8/8] 启动服务...${NC}"
systemctl daemon-reload
systemctl enable mosdns
systemctl reset-failed mosdns
systemctl restart mosdns

if systemctl is-active --quiet mosdns; then
    echo -e "${GREEN}✅ 部署完成！(${SCRIPT_VERSION})${NC}"
    echo -e "👉 输入 ${GREEN}mosctl${NC} 即可打开管理菜单"
else
    echo -e "${RED}❌ 启动失败，请检查日志${NC}"
fi
