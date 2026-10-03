# MosCtl

MosCtl 管理 MosDNS 进程与分流规则，提供 Web 登录、规则编辑、运行日志、DNS 测试和 REMOTE 上游设置。推荐在 Debian/Ubuntu 的 LXC 中使用 Linux 二进制部署。

## 一键安装

在 Debian/Ubuntu（含 LXC）中以 **root** 执行：

```bash
bash <(wget -qO- https://ghproxy.net/https://raw.githubusercontent.com/KyleYu2024/MosCtl/main/scripts/install.sh)
```

脚本自动识别 amd64/arm64，下载并校验最新 Release 的 MosCtl 安装包，安装 MosDNS v5 内核，配置 systemd 并启动服务。脚本和安装包默认通过 `ghproxy.net` 下载，方便国内环境使用；本机需安装 `wget`，且 53 端口未被其他服务占用。

完成后访问 `http://<本机IP>:9090`，用户名为 `admin`，随机密码会在安装结束时显示（保存在 `/etc/mosctl.env`）。登录后在设置页修改 REMOTE 国外 DNS 上游。

再次执行同一条命令即可升级 MosCtl，保留已有配置、账号、规则、统计数据及 MosDNS 内核。在命令前加 `VERSION=vX.Y.Z` 可指定版本；加 `GITHUB_PROXY=https://其他代理` 可切换安装包下载代理，`GITHUB_PROXY= bash ...` 使用直连。MosDNS 内核通过 Web 设置页更新。

安装包由 `vX.Y.Z` tag 自动发布到 GitHub Releases；首次使用需先发布一个版本。脚本见 [scripts/install.sh](scripts/install.sh)。

## Web 页面

- `/`、`/stats`：默认首页，展示今日国内/国外查询量、分流占比、小时趋势与按查询次数排序的域名榜单，支持分流筛选。
- `/rules`：强制国内、强制国外、IoT、Hosts 规则；直接编辑文本并保存应用。
- `/logs`：最近 256KB 控制台日志，默认每 3 秒刷新；服务重启后重新记录。
- `/tests`：百度、谷歌及自定义域名测试，展示地址、耗时和规则匹配参考。
- `/settings`：REMOTE 国外 DNS 上游，支持 UDP、TCP、TLS、HTTPS。

**Web 保存的 REMOTE 是最终配置。** 保存后会标记配置为 `WEB_MANAGED`，立即请求重载；此后重启服务也不会被 `REMOTE_UPSTREAM` 环境变量覆盖。该环境变量只在尚未由 Web 接管时用于初始配置。无需修改环境变量即可继续通过 Web 管理。

国内上游默认并发使用阿里与腾讯 DNS；GeoIP/GeoSite 规则每天 02:30 检查更新。DNS 测试统一查询本机 `127.0.0.1:53`。规则匹配提示并非实际请求追踪，缓存、自定义配置、客户端 IP 规则可能影响实际路径。Fake-IP 结果不代表网页 HTTP 连通性。

登录有效期为 30 天，签名密钥保存在 `/etc/mosdns/.web_session_key`（权限 0600）。保留该目录，重启或升级后登录状态仍有效；修改账号密码会使旧会话失效。

数据页统计真实 DNS 请求（A 和 AAAA 分别计次），不是网页访问次数。启动时为标准分流序列加入 MosDNS `query_summary`，将日志等级设为 `info`，查询摘要在后台消费，不展示到运行日志。国内/国外按实际执行的分流序列计数，缓存命中也统计；Hosts、拒绝等计入其他。自定义配置不包含标准序列或将日志写入文件时会明确报告统计未启用。

今日统计每 30 秒保存到 `/etc/mosdns/query_stats.json`（权限 0600），正常退出时保存，重启保留；每日重置。最多统计 10,000 个域名/分流组合，超过上限的查询仍计入总量，未纳入排行次数会在数据页显示。数据保存在本机，不上传到 GitHub；磁盘满等写入失败时重启可能丢失未保存统计。

## Web 内核更新

设置页的「MosDNS 内核」显示当前版本，并可检查官方 GitHub 稳定版本。Linux amd64/arm64 原生进程管理部署（包括 LXC，服务需以 root 运行）支持在线安装和回滚；Docker 部署请更新镜像。

发现更高版本后显示更新按钮。安装包必须具有官方发布 API 提供的 SHA-256 摘要，下载校验及版本验证通过后才替换二进制。更新保留配置、规则和统计数据，重启后连续检查国内、国外 DNS 解析；失败自动恢复旧内核。更新过程中只允许一个内核操作，页面显示后台进度。

成功更新后保留一个可回滚版本。回滚也会检查 DNS，并将切换前的内核保留为下一次回滚备份。更新或回滚会短暂中断 DNS；若 MosCtl 在替换过程中退出，下次启动会优先恢复未完成的操作。

## PWA

支持安装到主屏幕、独立窗口、深色主题及安全区。LXC 局域网部署需通过 HTTPS 反向代理使用 PWA；`localhost` 可使用 HTTP。离线只缓存页面和图标，登录、规则、日志与设置 API 不会缓存。

## 可选容器与内核同步

仓库保留 Dockerfile 和 `Sync MosDNS kernel` 工作流，定时检查上游 amd64 digest，有变化时测试并发布 `ghcr.io/kyleyu2024/mosctl:latest`。这不会自动升级 LXC 的二进制。详见 [内核同步说明](docs/AUTO_UPDATE.md)。
