
## MosCtl全自动部署与管理工具

一个专为 LXC/Linux 环境设计的 MosDNS 部署脚本，集成了交互式菜单、规则管理和故障救援模式。

## ✨ 特性

- **一键部署**：全自动安装环境、MosDNS 核心及管理工具。
- **交互式菜单**：小白友好的 CLI 菜单 (`mosctl`)，无需记忆复杂命令。
- **智能配置**：安装向导自动引导设置国内/国外 DNS 上游。
- **救援模式**：DNS 挂了？一键切换到救援 DNS (223.5.5.5)，防止断网。
- **规则管理**：内置编辑器，轻松修改 Hosts、强制国内/国外域名列表。

## 🚀 快速开始

在你的 LXC 或 Linux 服务器终端执行以下命令即可安装：

```bash
bash <(wget -qO- https://raw.githubusercontent.com/KyleYu2024/mosctl/main/install_cli.sh)
```

## 🛠️ 使用说明
安装完成后，直接输入 mosctl 可唤出管理菜单

## 📸 界面预览
```text
=====================================
         MosDNS 管理面板 [0.4.7]
=====================================
 状态: 🟢 运行中 | 核心: v5.3.4-xxxx
=====================================
 [1] 服务管理 (启动/停止/重启)
 [2] 参数设置 (上游/缓存/TTL)
 [3] 规则管理 (直连/代理/IoT)
 [4] 更新 Geo 数据库
 [5] 救援模式管理
 [6] 查看运行日志
 [7] DNS 解析测试
 [8] 彻底卸载脚本
 [0] 退出程序
=====================================
```

## docker部署
推荐用macvlan，可使用以下一键脚本来配置和宿主机通信
```bash
bash <(wget -qO- https://ghproxy.net/https://raw.githubusercontent.com/KyleYu2024/Script/main/macvlan_setup.sh)
```
```yaml
services:
  mosctl:
    image: kyleyu2024/mosctl:latest
    container_name: mosctl
    restart: always
    ports:
      - "53:53/udp"
      - "53:53/tcp"
    environment:
      LOCAL_UPSTREAM: "udp://223.5.5.5" #国内上游dns
      REMOTE_UPSTREAM: "udp://10.10.1.202:53" #国外上游dns
      TZ: "Asia/Shanghai"
    volumes:
      - ./data:/etc/mosdns
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    networks:
      my_macvlan:
        ipv4_address: 10.10.1.201
networks:
  my_macvlan:
    external: true
    name: macvlan
```
