# MosCtl

MosCtl 管理 MosDNS 进程与分流规则，提供 Web 登录、规则编辑、运行日志、DNS 测试和 REMOTE 上游设置。推荐在 Debian/Ubuntu 的 LXC 中使用 Linux 二进制部署。

## 获取安装包

在 GitHub 的 **Actions → Build Linux binaries → Run workflow** 中选择 `main`，运行完成后下载对应架构的 Artifact，解压外层 ZIP 后可得到 `mosctl-linux-amd64.tar.gz` 或 `mosctl-linux-arm64.tar.gz` 与 SHA256 校验文件。

- Intel/AMD x86_64：`amd64`
- ARM aarch64：`arm64`

安装包包含 MosCtl 二进制、默认配置和 GeoIP/GeoSite 规则。MosDNS 内核需要单独安装。

也可使用 Go 1.23 或更高版本从源码编译：

```bash
git clone --branch main https://github.com/KyleYu2024/MosCtl.git
cd MosCtl
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o mosctl ./cmd/mosctl
```

源码安装时，将 `templates/config.yaml` 与 `rules/` 一起放入下一节的模板目录。

## Debian/Ubuntu LXC 二进制安装

以下命令以 root 执行。先安装必要工具：

```bash
apt-get update
apt-get install -y ca-certificates curl unzip dnsutils tar
```

将下载的安装包放到 LXC 的 `/tmp`。先在校验文件所在目录运行 `sha256sum -c checksums-amd64.txt`，再解压安装（ARM 使用对应文件名）：

```bash
mkdir -p /tmp/mosctl-package
tar -xzf /tmp/mosctl-linux-amd64.tar.gz -C /tmp/mosctl-package
install -m 0755 /tmp/mosctl-package/mosctl /usr/local/bin/mosctl
mkdir -p /usr/share/mosdns/rules /etc/mosdns/rules
install -m 0644 /tmp/mosctl-package/config.yaml /usr/share/mosdns/config.yaml
cp -a /tmp/mosctl-package/rules/. /usr/share/mosdns/rules/
```

安装 MosDNS v5 内核。以下为 x86_64 示例（ARM 将 `amd64` 换为 `arm64`）：

```bash
mkdir -p /tmp/mosdns-package
curl -fL https://github.com/IrineSistiana/mosdns/releases/download/v5.3.4/mosdns-linux-amd64.zip -o /tmp/mosdns-package/mosdns.zip
unzip -o /tmp/mosdns-package/mosdns.zip -d /tmp/mosdns-package
install -m 0755 /tmp/mosdns-package/mosdns /usr/local/bin/mosdns
/usr/local/bin/mosdns version
```

MosCtl 第一次启动时，会从 `/usr/share/mosdns` 初始化 `/etc/mosdns`；之后不会覆盖已有配置。确认本机没有其他服务占用 UDP/TCP 53 端口。

## 设置登录与 systemd 服务

使用编辑器创建 `/etc/mosctl.env`，替换登录密码和代理 DNS 地址：

```ini
USERNAME=admin
PASSWORD=replace-with-a-strong-password
WEB_LISTEN=:9090
REMOTE_UPSTREAM=udp://192.168.1.2:53
TZ=Asia/Shanghai
```

设置权限：

```bash
chmod 600 /etc/mosctl.env
```

创建 `/etc/systemd/system/mosctl.service`：

```ini
[Unit]
Description=MosCtl and MosDNS
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/mosctl.env
ExecStart=/usr/local/bin/mosctl
Restart=on-failure
RestartSec=3
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```

启用服务：

```bash
systemctl daemon-reload
systemctl enable --now mosctl
systemctl status mosctl --no-pager
```

访问 `http://<LXC-IP>:9090` 登录。MosCtl 管理并启动 MosDNS，不需要另外启动 `mosdns.service`。

## Web 页面

- `/rules`：强制国内、强制国外、IoT、Hosts 规则；直接编辑文本并保存应用。
- `/stats`：今日国内/国外查询量、分流占比、小时趋势与按查询次数排序的域名榜单，支持分流筛选。
- `/logs`：最近 256KB 控制台日志，默认每 3 秒刷新；服务重启后重新记录。
- `/tests`：百度、谷歌及自定义域名测试，展示地址、耗时和规则匹配参考。
- `/settings`：REMOTE 国外 DNS 上游，支持 UDP、TCP、TLS、HTTPS。

**Web 保存的 REMOTE 是最终配置。** 保存后会标记配置为 `WEB_MANAGED`，立即请求重载；此后重启服务也不会被 `REMOTE_UPSTREAM` 环境变量覆盖。该环境变量只在尚未由 Web 接管时用于初始配置。无需修改环境变量即可继续通过 Web 管理。

国内上游默认并发使用阿里与腾讯 DNS；GeoIP/GeoSite 规则每天 02:30 检查更新。DNS 测试统一查询本机 `127.0.0.1:53`。规则匹配提示并非实际请求追踪，缓存、自定义配置、客户端 IP 规则可能影响实际路径。Fake-IP 结果不代表网页 HTTP 连通性。

登录有效期为 30 天，签名密钥保存在 `/etc/mosdns/.web_session_key`（权限 0600）。保留该目录，重启或升级后登录状态仍有效；修改账号密码会使旧会话失效。

数据页统计真实 DNS 请求（A 和 AAAA 分别计次），不是网页访问次数。启动时为标准分流序列加入 MosDNS `query_summary`，将日志等级设为 `info`，查询摘要在后台消费，不展示到运行日志。国内/国外按实际执行的分流序列计数，缓存命中也统计；Hosts、拒绝等计入其他。自定义配置不包含标准序列或将日志写入文件时会明确报告统计未启用。

今日统计每 30 秒保存到 `/etc/mosdns/query_stats.json`（权限 0600），正常退出时保存，重启保留；每日重置。最多统计 10,000 个域名/分流组合，超过上限的查询仍计入总量，未纳入排行次数会在数据页显示。数据保存在本机，不上传到 GitHub；磁盘满等写入失败时重启可能丢失未保存统计。

## 升级二进制

下载并校验新安装包，只替换 MosCtl；不要覆盖 `/etc/mosdns`：

```bash
cp -p /usr/local/bin/mosctl /usr/local/bin/mosctl.backup
install -m 0755 /tmp/mosctl-package/mosctl /usr/local/bin/mosctl.new
mv /usr/local/bin/mosctl.new /usr/local/bin/mosctl
systemctl restart mosctl
systemctl status mosctl --no-pager
```

若需要回滚，将备份复制为新文件后原子替换并重启。更新 MosDNS 内核需单独替换 `/usr/local/bin/mosdns`，建议先验证与现有配置兼容。

## PWA

支持安装到主屏幕、独立窗口、深色主题及安全区。LXC 局域网部署需通过 HTTPS 反向代理使用 PWA；`localhost` 可使用 HTTP。离线只缓存页面和图标，登录、规则、日志与设置 API 不会缓存。

## 可选容器与内核同步

仓库保留 Dockerfile 和 `Sync MosDNS kernel` 工作流，定时检查上游 amd64 digest，有变化时测试并发布 `ghcr.io/kyleyu2024/mosctl:latest`。这不会自动升级 LXC 的二进制。详见 [内核同步说明](docs/AUTO_UPDATE.md)。
