#!/bin/bash
set -e

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}>>> 开始安装 MosCtl & MosDNS 环境...${NC}"

# 1. 检查 Root 权限
if [ "$(id -u)" != "0" ]; then
   echo -e "${RED}错误: 必须使用 Root 权限运行此脚本${NC}"
   exit 1
fi

# 2. 创建目录结构
echo ">>> 创建目录 /etc/mosdns..."
mkdir -p /etc/mosdns/rules
mkdir -p /var/log

# 3. 解决 PVE LXC 重启覆盖 resolv.conf 的问题
echo ">>> 配置 PVE LXC DNS 保护..."
touch /etc/.pve-ignore.resolv.conf

# 4. 创建空的本地规则文件 (如果不存在)
touch /etc/mosdns/rules/force-cn.txt
touch /etc/mosdns/rules/force-nocn.txt
touch /etc/mosdns/rules/hosts.txt
touch /etc/mosdns/rules/user_iot.txt

# 5. 安装 Systemd 服务
echo ">>> 安装 Systemd 服务..."
if [ -f "templates/mosdns.service" ]; then
    cp templates/mosdns.service /etc/systemd/system/
    cp templates/mosdns-rescue.service /etc/systemd/system/
    systemctl daemon-reload
else
    echo -e "${RED}警告: 未找到服务模板文件，跳过 Systemd 配置${NC}"
fi

# 6. 设置日志轮转
echo ">>> 配置日志轮转 (Logrotate)..."
cat > /etc/logrotate.d/mosdns <<LOG
/var/log/mosdns.log {
    daily
    rotate 7
    compress
    missingok
    notifempty
    copytruncate
}
LOG

echo -e "${GREEN}>>> 基础环境安装完成！${NC}"
echo "下一步: 请确保 mosdns 和 mosctl 二进制文件已放入 /usr/local/bin/"
