arkdown
# MosCtl - MosDNS 全自动部署与管理工具

一个专为 LXC/Linux 环境设计的 MosDNS 部署脚本，集成了交互式菜单、配置同步 (GitOps)、规则管理和故障救援模式。

## ✨ 特性

- **一键部署**：全自动安装环境、MosDNS 核心及管理工具。
- **交互式菜单**：小白友好的 CLI 菜单 (`mosctl`)，无需记忆复杂命令。
- **GitOps 同步**：支持从 GitHub 拉取最新配置，实现配置的版本控制。
- **智能配置**：安装向导自动引导设置国内/国外 DNS 上游。
- **救援模式**：DNS 挂了？一键切换到救援 DNS (223.5.5.5)，防止断网。
- **规则管理**：内置编辑器，轻松修改 Hosts、强制国内/国外域名列表。

## 🚀 快速开始

在你的 LXC 或 Linux 服务器终端执行以下命令即可安装：

```bash
wget -qO- [https://raw.githubusercontent.com/KyleYu2024/mosctl/main/install_new.sh](https://raw.githubusercontent.com/KyleYu2024/mosctl/main/install_new.sh) | bash
🛠️ 使用说明
安装完成后，直接输入 mosctl 唤出管理菜单：

Plaintext
==============================
   MosDNS 管理面板 (v3.6)   
==============================
 内核版本: v5.3.3 | 状态: 🟢 运行中
==============================
  1. 🔄  同步配置 (Git Pull)      # 拉取云端最新 config.yaml
  2. ⚙️   修改上游 DNS             # 修改国内/国外 DNS IP
  3. 📝  管理自定义规则           # 编辑 Hosts 或强制分流列表
  4. ⬇️   更新 Geo 数据            # 更新 GeoSite/GeoIP 数据库
  5. 🚑  开启救援模式             # 紧急切换 DNS 转发
  6. ♻️   关闭救援模式             # 恢复 MosDNS 接管
  7. 📊  查看运行日志             # 实时查看查询日志
  8. ▶️   重启服务
  9. 🗑️   彻底卸载
📂 目录结构
/etc/mosdns/config.yaml: 主配置文件

/etc/mosdns/rules/: 规则文件目录 (hosts.txt, force-cn.txt 等)

/usr/local/bin/mosctl: 管理脚本

/usr/local/bin/mosdns: 核心程序

⚠️ 注意事项
本脚本专为 LXC 容器 优化，同时也兼容标准 Debian/Ubuntu 系统。

修改配置建议通过 mosctl 菜单或在本地修改 Git 仓库后推送同步。


**提交 README:**
```bash
