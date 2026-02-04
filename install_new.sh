#!/bin/bash
set -e

# ================= 配置区 =================
# ✅ 已修正: 用户名改为 KyleYu2024
REPO_URL="https://github.com/KyleYu2024/MosDNS-Web.git" 
MOSDNS_VERSION="v5.3.3"
# =========================================

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}🚀 开始本地部署流程 (User: KyleYu2024)...${NC}"

# 1. 基础环境
echo -e "${YELLOW}[1/7] 安装依赖...${NC}"
apt update && apt install -y curl wget git nano net-tools dnsutils unzip

# 2. 清理端口
echo -e "${YELLOW}[2/7] 清理 53 端口...${NC}"
systemctl stop systemd-resolved 2>/dev/null || true
systemctl disable systemd-resolved 2>/dev/null || true
rm -f /etc/resolv.conf
echo "nameserver 223.5.5.5" > /etc/resolv.conf

# 3. 安装 MosDNS
echo -e "${YELLOW}[3/7] 安装 MosDNS 主程序...${NC}"
cd /tmp
wget -q -O mosdns.zip "https://github.com/IrineSistiana/mosdns/releases/download/${MOSDNS_VERSION}/mosdns-linux-amd64.zip"
unzip -o mosdns.zip
mv mosdns /usr/local/bin/mosdns
chmod +x /usr/local/bin/mosdns

# 4. 下载规则
echo -e "${YELLOW}[4/7] 下载规则文件...${NC}"
mkdir -p /etc/mosdns/rules
echo "Downloading GeoSite CN..."
wget -q -O /etc/mosdns/rules/geosite_cn.txt https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt
echo "Downloading GeoIP CN..."
wget -q -O /etc/mosdns/rules/geoip_cn.txt https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt
echo "Downloading Apple..."
wget -q -O /etc/mosdns/rules/geosite_apple.txt https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt
echo "Downloading No-CN List..."
wget -q -O /etc/mosdns/rules/geosite_no_cn.txt https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt

# 补全空文件
touch /etc/mosdns/rules/force-cn.txt
touch /etc/mosdns/rules/force-nocn.txt
touch /etc/mosdns/rules/hosts.txt
touch /etc/mosdns/rules/local-ptr.txt

# 5. 拉取你的配置
echo -e "${YELLOW}[5/7] 拉取最新配置 (KyleYu2024)...${NC}"
cd ~
rm -rf mosctl
# 尝试克隆
git clone ${REPO_URL} mosctl || { echo -e "${RED}克隆失败！请检查 GitHub 上是否已存在 MosDNS-Web 仓库。${NC}"; exit 1; }

# 应用配置
cp ~/mosctl/templates/config.yaml /etc/mosdns/config.yaml

# 6. 设置服务
echo -e "${YELLOW}[6/7] 配置 Systemd...${NC}"
cat > /etc/systemd/system/mosdns.service <<EOF
[Unit]
Description=MosDNS Service
After=network.target
[Service]
Type=simple
ExecStart=/usr/local/bin/mosdns start -d /etc/mosdns
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
EOF

# 7. 启动
echo -e "${YELLOW}[7/7] 启动服务...${NC}"
systemctl daemon-reload
systemctl enable mosdns
systemctl restart mosdns
sleep 2

# 状态检查
if systemctl is-active --quiet mosdns; then
    echo -e "${GREEN}✅ 部署成功！${NC}"
    echo "测试百度:"
    nslookup www.baidu.com 127.0.0.1
    echo "测试谷歌:"
    nslookup google.com 127.0.0.1
else
    echo -e "${RED}❌ 启动失败，请运行 systemctl status mosdns 查看原因${NC}"
fi
