#!/bin/bash
set -e

# ================= 配置区 =================
# 你的 GitHub 仓库
REPO_URL="https://github.com/KyleYu2024/MosCtl.git"
# 默认版本，如果获取不到最新版则回退到此版本
DEFAULT_MOSDNS_VERSION="v5.3.3"
# 脚本版本号
SCRIPT_VERSION="v0.4.1-fix" 
# GitHub 加速代理
GH_PROXY="https://gh-proxy.com/"
# =========================================

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始 MosDNS 全自动部署 (Go Binary 版)...${NC}"

# ================= 1. 基础环境准备 =================
echo -e "${YELLOW}[1/8] 环境准备...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip iptables cron

# 修复 PATH 环境变量，防止找不到命令
if ! grep -q "/usr/local/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/bin
fi

# ================= 1.5 获取最新版本 =================
echo -e "${YELLOW}🔍 正在检查 MosDNS 最新版本...${NC}"
# 尝试通过 API 获取最新 Release Tag
LATEST_TAG=$(curl -sL https://api.github.com/repos/IrineSistiana/mosdns/releases/latest | grep '"tag_name":' | cut -d'"' -f4)

if [ -n "$LATEST_TAG" ]; then
    MOSDNS_VERSION="$LATEST_TAG"
    echo -e "✅ 检测到最新版本: ${GREEN}${MOSDNS_VERSION}${NC}"
else
    MOSDNS_VERSION="$DEFAULT_MOSDNS_VERSION"
    echo -e "${RED}⚠️  无法获取最新版本，将使用稳定版: ${MOSDNS_VERSION}${NC}"
fi

# ================= 2. 清理端口占用 =================
echo -e "${YELLOW}[2/8] 清理 53 端口...${NC}"
# 停止 Ubuntu 默认的 systemd-resolved 防止占用 53 端口
systemctl stop systemd-resolved 2>/dev/null || true
systemctl disable systemd-resolved 2>/dev/null || true
# 重置 resolv.conf 为阿里 DNS，确保下载过程中有网
rm -f /etc/resolv.conf
echo "nameserver 223.5.5.5" > /etc/resolv.conf
# 开启 IP 转发
sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
echo "net.ipv4.ip_forward=1" > /etc/sysctl.d/99-mosdns.conf

# ================= 2.5 开放防火墙端口 =================
echo -e "${YELLOW}[2.5/8] 开放 53 端口防火墙...${NC}"
iptables -I INPUT -p udp --dport 53 -j ACCEPT 2>/dev/null || true
iptables -I INPUT -p tcp --dport 53 -j ACCEPT 2>/dev/null || true
# 尝试保存规则 (适配 debian/ubuntu)
if command -v iptables-save >/dev/null; then
    mkdir -p /etc/iptables
    iptables-save > /etc/iptables/rules.v4
fi

# ================= 3. 安装 MosDNS 主程序 =================
echo -e "${YELLOW}[3/8] 安装 MosDNS 主程序 (${MOSDNS_VERSION})...${NC}"
if [ ! -f "/usr/local/bin/mosdns" ]; then
    cd /tmp
    echo "正在下载内核文件..."
    # 使用 GH_PROXY 加速下载
    wget -q --show-progress -O mosdns.zip "${GH_PROXY}https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
    
    unzip -o mosdns.zip > /dev/null 2>&1
    mv mosdns /usr/local/bin/mosdns
    chmod +x /usr/local/bin/mosdns
    echo -e "✅ 安装完成"
else
    echo "MosDNS 已安装，跳过下载。"
fi

# ================= 4. 安装 Mosctl 管理工具 =================
echo -e "${YELLOW}[4/8] 安装 mosctl 二进制工具...${NC}"
rm -f /usr/local/bin/mosctl

# 下载 Go 编译的 mosctl 二进制文件
wget -q --show-progress -O /usr/local/bin/mosctl "${GH_PROXY}https://github.com/KyleYu2024/mosctl/releases/download/${SCRIPT_VERSION}/mosctl-linux-amd64"
chmod +x /usr/local/bin/mosctl

if [ ! -f "/usr/local/bin/mosctl" ]; then
    echo -e "${RED}❌ mosctl 下载失败，请检查网络或版本号。${NC}"
    # 回滚：如果二进制下载失败，可以考虑提供一个极简版的 shell 脚本或者报错退出
    exit 1
fi
echo -e "✅ mosctl 安装完成"


# ================= 5. 下载初始规则 =================
mkdir -p /etc/mosdns/rules
# 此处保留 [ ! -f ] 判断，防止覆盖用户的自定义规则（仅在首次安装时生效）
download_rule_init() {
    if [ ! -f "$1" ]; then
        echo "初始化下载: ${1##*/}"
        wget -q --show-progress -O "$1" "${GH_PROXY}$2"
    fi
}
download_rule_init "/etc/mosdns/rules/geosite_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
download_rule_init "/etc/mosdns/rules/geoip_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"
download_rule_init "/etc/mosdns/rules/geosite_apple.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"
download_rule_init "/etc/mosdns/rules/geosite_no_cn.txt" "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"

# 确保这些文件存在，避免 mosdns 启动报错
touch /etc/mosdns/rules/{force-cn.txt,force-nocn.txt,hosts.txt,local-ptr.txt,user_iot.txt,local_direct.txt,local_proxy.txt}

# ================= 6. 配置服务文件 (提前创建) =================
echo -e "${YELLOW}[6/8] 配置系统服务...${NC}"

# 救援模式服务
cat > /etc/systemd/system/mosdns-rescue.service <<EOF
[Unit]
Description=MosDNS Rescue Mode
After=network.target
[Service]
Type=oneshot
ExecStart=/usr/local/bin/mosctl rescue enable
EOF

# MosDNS 主服务
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

systemctl daemon-reload

# ================= 6.5 初始化配置 =================
echo -e "${YELLOW}[6.5/8] 初始化配置...${NC}"
/usr/local/bin/mosctl sync || echo -e "${RED}同步配置失败，稍后请手动同步...${NC}"

# ================= 6.8 交互式配置 =================
echo -e "${YELLOW}[6.8/8] 交互式配置向导...${NC}"

if grep -q "local_dns=" /etc/mosdns/config.yaml; then
    echo "⚠️ 检测到配置文件损坏，正在重置..."
    rm -f /etc/mosdns/config.yaml
    /usr/local/bin/mosctl sync
fi

echo -e "请配置 DNS 上游（按回车使用默认值）"

# 读取国内 DNS
echo -n "国内 DNS (默认 udp://119.29.29.29): "
if [ -c /dev/tty ]; then read local_dns < /dev/tty; else read local_dns; fi
local_dns=${local_dns:-"udp://119.29.29.29"}
if [[ "$local_dns" != *"://"* ]]; then local_dns="udp://${local_dns}"; fi

# 读取国外 DNS (必须填写，否则可能无法分流)
echo -n "mihomo或其他代理工具的dns监听地址 (例如 10.10.2.252:53，直接回车不修改): "
if [ -c /dev/tty ]; then read remote_dns < /dev/tty; else read remote_dns; fi
if [[ -n "$remote_dns" ]] && [[ "$remote_dns" != *"://"* ]]; then remote_dns="udp://${remote_dns}"; fi

# 写入配置
sed -i "s|\(.*\)- addr:.*# TAG_LOCAL|\1- addr: \"${local_dns}\" # TAG_LOCAL|" /etc/mosdns/config.yaml
if [ -n "$remote_dns" ]; then
    sed -i "s|\(.*\)- addr:.*# TAG_REMOTE|\1- addr: \"${remote_dns}\" # TAG_REMOTE|" /etc/mosdns/config.yaml
    echo "  - 国外 DNS 已设为: $remote_dns"
else
    echo "  - 国外 DNS 未修改 (保留配置默认值)"
fi

echo "  - 国内 DNS 已设为: $local_dns"

# 自动更新 (每天凌晨 2 点)
if ! crontab -l 2>/dev/null | grep -q "mosctl update"; then
    (crontab -l 2>/dev/null; echo "0 2 * * * /usr/local/bin/mosctl update > /dev/null 2>&1") | crontab -
    echo -e "${GREEN}✅ 已添加自动更新计划任务${NC}"
fi

# ================= 8. 启动验证 =================
echo -e "${YELLOW}[8/8] 启动服务...${NC}"
systemctl daemon-reload
systemctl enable mosdns
systemctl reset-failed mosdns
systemctl restart mosdns

if systemctl is-active --quiet mosdns; then
    echo -e "${GREEN}✅ 部署完成！${NC}"
    echo -e "👉 输入 ${GREEN}mosctl${NC} 即可打开管理菜单"
else
    echo -e "${RED}❌ 启动失败，可能原因：${NC}"
    echo -e "1. 配置文件格式错误"
    echo -e "2. 端口仍被占用"
    echo -e "👉 请输入 ${YELLOW}journalctl -u mosdns -n 20${NC} 查看详情"
fi
