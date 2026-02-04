#!/bin/bash

# ==========================================
# MosDNS 一键部署脚本 (高阶防劫持版)
# 适用环境: Debian/Ubuntu LXC
# ==========================================

# 设置变量
MOSDNS_VERSION="v5.3.3"
REPO_URL="https://github.com/KyleYu2023/MosDNS-Web.git" # 请确认这是你的正确仓库地址
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/mosdns"
RULES_DIR="${CONFIG_DIR}/rules"

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 错误处理
set -e
trap 'echo -e "${RED}❌ 脚本执行出错，请检查上方报错信息。${NC}"' ERR

echo -e "${GREEN}🚀 开始部署 MosDNS 环境...${NC}"

# 1. 系统基础环境准备
echo -e "${YELLOW}Step 1: 安装基础依赖并清理 53 端口...${NC}"
apt update
apt install -y curl wget git nano net-tools dnsutils unzip

# 关闭 systemd-resolved 防止占用 53 端口
if systemctl is-active --quiet systemd-resolved; then
    echo "关闭 systemd-resolved..."
    systemctl stop systemd-resolved
    systemctl disable systemd-resolved
fi

# 重置 resolv.conf (临时使用阿里DNS，确保后续下载顺畅)
rm -f /etc/resolv.conf
echo "nameserver 223.5.5.5" > /etc/resolv.conf

# 检查端口
if netstat -tunlp | grep -q ":53 "; then
    echo -e "${RED}⚠️ 检测到 53 端口仍被占用，请手动排查！${NC}"
    netstat -tunlp | grep ":53 "
    exit 1
fi

# 2. 安装 MosDNS 主程序
echo -e "${YELLOW}Step 2: 下载并安装 MosDNS ${MOSDNS_VERSION}...${NC}"
cd /tmp
wget -O mosdns.zip "https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
unzip -o mosdns.zip
mv mosdns ${INSTALL_DIR}/mosdns
chmod +x ${INSTALL_DIR}/mosdns
rm -f mosdns.zip
echo -e "MosDNS 版本: $(${INSTALL_DIR}/mosdns version)"

# 3. 克隆配置仓库 (Mosctl)
echo -e "${YELLOW}Step 3: 克隆个人配置仓库...${NC}"
cd ~
if [ -d "mosctl" ]; then
    echo "检测到旧仓库，正在清理..."
    rm -rf mosctl
fi
git clone ${REPO_URL} mosctl

# 设置 mosctl 软链接
chmod +x ~/mosctl/mosctl
ln -sf ~/mosctl/mosctl ${INSTALL_DIR}/mosctl

# 4. 初始化规则文件 (下载 Loyalsoldier 规则)
echo -e "${YELLOW}Step 4: 下载规则文件 (这是关键一步)...${NC}"
mkdir -p ${RULES_DIR}

echo "⬇️  下载 GeoSite CN..."
wget -q --show-progress -O ${RULES_DIR}/geosite_cn.txt https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt

echo "⬇️  下载 GeoIP CN..."
wget -q --show-progress -O ${RULES_DIR}/geoip_cn.txt https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt

echo "⬇️  下载 Apple CN..."
wget -q --show-progress -O ${RULES_DIR}/geosite_apple.txt https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt

echo "⬇️  下载 国外域名列表 (已修正文件名)..."
wget -q --show-progress -O ${RULES_DIR}/geosite_no_cn.txt https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt

# 创建空文件防止报错
echo "📄 创建必要的空文件..."
touch ${RULES_DIR}/force-cn.txt
touch ${RULES_DIR}/force-nocn.txt
touch ${RULES_DIR}/hosts.txt
touch ${RULES_DIR}/local-ptr.txt

# 5. 应用配置文件
echo -e "${YELLOW}Step 5: 应用最新的 Config...${NC}"
# 直接从克隆下来的仓库复制，比 mosctl sync 更适合初始化
cp ~/mosctl/templates/config.yaml ${CONFIG_DIR}/config.yaml

# 6. 配置 Systemd 服务
echo -e "${YELLOW}Step 6: 配置 Systemd 服务...${NC}"
cat > /etc/systemd/system/mosdns.service <<EOF
[Unit]
Description=MosDNS Service
Documentation=https://github.com/IrineSistiana/mosdns
After=network.target

[Service]
Type=simple
#ExecStartPre=/usr/local/bin/mosctl rescue disable
ExecStart=/usr/local/bin/mosdns start -d /etc/mosdns
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# 7. 启动服务与验证
echo -e "${YELLOW}Step 7: 启动服务并验证...${NC}"
systemctl daemon-reload
systemctl enable mosdns
systemctl restart mosdns

# 等待几秒让服务完全启动
sleep 3

# 检查状态
if systemctl is-active --quiet mosdns; then
    echo -e "${GREEN}✅ MosDNS 服务启动成功！${NC}"
else
    echo -e "${RED}❌ 服务启动失败，请查看 journalctl -u mosdns${NC}"
    exit 1
fi

# 8. 功能测试
echo -e "${YELLOW}正在进行解析测试...${NC}"

echo -n "测试百度 (应为公网 IP): "
BAIDU_IP=$(nslookup www.baidu.com 127.0.0.1 | grep 'Address:' | tail -n1 | awk '{print $2}')
echo "${BAIDU_IP}"

echo -n "测试 Google (应为 FakeIP 198.18.x.x): "
GOOGLE_IP=$(nslookup google.com 127.0.0.1 | grep 'Address:' | tail -n1 | awk '{print $2}')
echo "${GOOGLE_IP}"

echo -e "${GREEN}🎉 部署完成！享受你的完美 DNS 吧！${NC}"
