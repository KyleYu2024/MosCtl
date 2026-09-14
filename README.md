
## docker部署
推荐用macvlan，可使用以下一键脚本来配置和宿主机通信
```bash
bash <(wget -qO- https://ghproxy.net/https://raw.githubusercontent.com/KyleYu2024/Script/main/macvlan_setup.sh)
```
```yaml
services:
  mosctl:
    image: ghcr.io/kyleyu2024/mosctl:latest
    container_name: mosctl
    restart: always
    ports:
      - "53:53/udp"
      - "53:53/tcp"
      - "9090:9090/tcp" # Web 规则管理
    environment:
      REMOTE_UPSTREAM: "udp://10.10.1.202:53" #国外上游dns（代理软件的dns监听地址）
      USERNAME: "admin" # Web 登录用户名
      PASSWORD: "change-me" # 请替换为强密码
      WEB_LISTEN: ":9090" # 可选，默认 :9090
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

## Web 规则管理

容器同时配置 `USERNAME` 和 `PASSWORD` 后，访问 `http://<NAS-IP>:9090` 即可管理以下本地规则：

- `force-cn.txt`：强制国内解析
- `force-nocn.txt`：强制国外解析
- `user_iot.txt`：IoT 设备 IP/CIDR
- `hosts.txt`：自定义 Hosts 映射

“保存并应用”会在原子写入后触发 MosDNS 热重载。如未配置上述两个登录变量，Web 管理页不会启动。

Web 登录状态默认保留 30 天。会话签名密钥会以 `0600` 权限保存在挂载目录的 `/etc/mosdns/.web_session_key`，因此更新或重建容器不会导致登录失效。修改 `USERNAME` 或 `PASSWORD` 会自动使旧会话失效。

### PWA 安装

Web 管理页已支持 PWA，可从 Chrome、Edge 或 Safari 的“安装应用 / 添加到主屏幕”入口安装。安装后会使用独立窗口、专用图标、深色主题和设备安全区。

PWA 在 NAS 局域网地址上使用时需要通过 HTTPS 反向代理访问；浏览器只对 HTTPS 和 `localhost` 允许 Service Worker。离线时可打开已缓存的应用界面，但登录、读取和保存规则仍需要连接 MosCtl 服务，规则 API 和登录数据不会进入离线缓存。
